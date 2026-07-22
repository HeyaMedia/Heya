package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/scheduler"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/karbowiak/heya/internal/taskdefs"
	"github.com/karbowiak/heya/internal/textembed"
	"github.com/rs/zerolog/log"
)

// TaskStats holds completion counts for a scheduled task.
type TaskStats struct {
	Complete int `json:"complete"`
	Pending  int `json:"pending"`
	Failed   int `json:"failed,omitempty"`
	Total    int `json:"total"`
}

// TaskItem represents a single work item for a scheduled task.
type TaskItem struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	// Error carries the last failure message for items in "failed" status.
	// Empty for complete/pending items. Surfaced in the tasks-page items
	// modal so the user can see why an enrich failed without diving into
	// the logs.
	Error string `json:"error,omitempty"`
}

// TaskItemsResult holds a page of task items together with counts.
type TaskItemsResult struct {
	Items    []TaskItem `json:"items"`
	Total    int        `json:"total"`
	Complete int        `json:"complete"`
	Pending  int        `json:"pending"`
	Failed   int        `json:"failed"`
}

// ListScheduledTasks returns all scheduled tasks from the database.
func (a *App) ListScheduledTasks(ctx context.Context) ([]sqlc.ScheduledTask, error) {
	q := sqlc.New(a.db)
	return q.ListScheduledTasks(ctx)
}

// TaskRuntimeState describes the live state of one scheduled task —
// what's pending/running in River for its kinds. Replaces the old
// in-memory ProgressTracker snapshot now that work happens entirely in
// fanned-out River jobs.
type TaskRuntimeState struct {
	State   string `json:"state"`   // idle | running
	Pending int    `json:"pending"` // count across kinds in available/scheduled/retryable
	Running int    `json:"running"` // count across kinds in state=running
}

// GetAllTaskRuntimeState queries river_job for each scheduled task's
// associated kinds and returns a per-task summary. A task is "running"
// iff at least one of its kinds has a row in available/running/
// retryable/scheduled. Replaces the old GetAllTaskProgress which
// surfaced an in-process ProgressTracker snapshot.
func (a *App) GetAllTaskRuntimeState(ctx context.Context) map[string]TaskRuntimeState {
	defs := taskdefs.Scheduled()
	out := make(map[string]TaskRuntimeState, len(defs))
	// One grouped river_job pass for all definitions — a per-definition
	// CountScheduledTask loop costs a full-table scan per task when a large
	// backlog is parked.
	live, err := queueops.CountLiveByKindAndTask(ctx, a.db)
	if err != nil {
		return out
	}
	for _, def := range defs {
		counts := queueops.RuntimeCountsFor(live, taskdefs.TaskKinds(def.ID), def.ID)
		state := "idle"
		if counts.Pending > 0 || counts.Running > 0 {
			state = "running"
		}
		out[def.ID] = TaskRuntimeState{State: state, Pending: counts.Pending, Running: counts.Running}
	}
	return out
}

// QueryTaskStats gathers completion statistics for each known task type.
func (a *App) QueryTaskStats(ctx context.Context) map[string]TaskStats {
	stats := make(map[string]TaskStats)

	q := sqlc.New(a.db)

	// Eligibility comes from the shared views (migration 00035) so these
	// counts can never drift from what the kickoff workers actually enqueue.
	if tp, err := q.CountTrickplayEligible(ctx); err == nil {
		stats["generate_trickplay"] = TaskStats{
			Complete: int(tp.Complete),
			Pending:  int(tp.Total - tp.Complete),
			Total:    int(tp.Total),
		}
	}

	if th, err := q.CountThumbnailEligible(ctx); err == nil {
		stats["generate_thumbnails"] = TaskStats{
			Complete: int(th.Complete),
			Pending:  int(th.Total - th.Complete),
			Total:    int(th.Total),
		}
	}

	var libTotal int
	var libWithPending int
	row := a.db.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM libraries),
			(SELECT count(DISTINCT l.id) FROM libraries l
			 JOIN library_files lf ON lf.library_id = l.id
			 WHERE lf.status = 'pending' AND lf.deleted_at IS NULL)
	`)
	if row.Scan(&libTotal, &libWithPending) == nil {
		stats["scan_libraries"] = TaskStats{
			Complete: libTotal - libWithPending,
			Pending:  libWithPending,
			Total:    libTotal,
		}
	}

	// refresh_stale_items: derive counts from enrichment_status so the
	// tasks page shows three real buckets (complete / pending / failed)
	// instead of a single "stale" number that hid failures.
	var metaTotal, metaComplete, metaFailed int
	row = a.db.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE mi.enrichment_status = 'complete'),
			count(*) FILTER (WHERE mi.enrichment_status = 'failed')
		FROM media_item_cards mi
		WHERE mi.media_type = 'music'
		   OR EXISTS (SELECT 1 FROM media_item_external_ids ei WHERE ei.media_item_id = mi.id)
	`)
	if row.Scan(&metaTotal, &metaComplete, &metaFailed) == nil {
		stats["refresh_stale_items"] = TaskStats{
			Complete: metaComplete,
			Failed:   metaFailed,
			Pending:  metaTotal - metaComplete - metaFailed,
			Total:    metaTotal,
		}
	}

	var loudTotal, loudDone int
	row = a.db.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE tf.integrated_lufs IS NOT NULL)
		FROM track_files tf
		JOIN library_files lf ON lf.id = tf.library_file_id
		WHERE lf.deleted_at IS NULL
	`)
	if row.Scan(&loudTotal, &loudDone) == nil {
		stats["scan_music_loudness"] = TaskStats{
			Complete: loudDone,
			Pending:  loudTotal - loudDone,
			Total:    loudTotal,
		}
	}

	var fpTotal, fpDone int
	row = a.db.QueryRow(ctx, `
		WITH files AS (
			SELECT fp.library_file_id IS NOT NULL
			       AND fp.source_size = lf.size
			       AND fp.source_mtime IS NOT DISTINCT FROM lf.mtime AS complete
			FROM library_files lf
			JOIN libraries l ON l.id = lf.library_id
			LEFT JOIN library_file_fingerprints fp ON fp.library_file_id = lf.id
			WHERE l.media_type = 'music'
			  AND lf.deleted_at IS NULL
			  AND lower(lf.path) ~ '\\.(flac|mp3|m4a|aac|ogg|opus|wav|wma|ape|wv|alac|aiff|aif)$'
		)
		SELECT count(*), count(*) FILTER (WHERE complete) FROM files
	`)
	if row.Scan(&fpTotal, &fpDone) == nil {
		stats["scan_music_fingerprint"] = TaskStats{
			Complete: fpDone,
			Pending:  fpTotal - fpDone,
			Total:    fpTotal,
		}
	}

	// Universe: tracks with a primary file whose duration is within the
	// analyzer's cap. Mirrors the scheduler's NextTrackForAnalysis filter
	// (both tracks.duration AND every track_files.duration must be ≤ cap;
	// duration=0 means unknown and passes) so the Tasks UI counter agrees
	// with what the scheduler will actually pick up. Without the per-file
	// check, a track with empty upstream metadata (tracks.duration=0) but a
	// real 1h file_files.duration would sneak through.
	var facetsTotal, facetsDone int
	row = a.db.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM tracks t
			 JOIN track_files tf ON tf.track_id = t.id
			 WHERE t.duration <= $1 AND tf.duration <= $1),
			(SELECT count(*) FROM track_facets tfa
			 JOIN tracks t ON t.id = tfa.track_id
			 JOIN track_files tf ON tf.track_id = t.id
			 WHERE t.duration <= $1 AND tf.duration <= $1)
	`, sonicanalysis.MaxAnalysisDurationSeconds)
	if row.Scan(&facetsTotal, &facetsDone) == nil {
		stats["analyze_music_facets"] = TaskStats{
			Complete: facetsDone,
			Pending:  facetsTotal - facetsDone,
			Total:    facetsTotal,
		}
	}

	return stats
}

// UpdateScheduledTask updates the configuration for a scheduled task.
func (a *App) UpdateScheduledTask(ctx context.Context, taskID string, enabled bool, intervalHours, maxRuntimeMinutes int32, dailyStartTime, dailyEndTime string) (sqlc.ScheduledTask, error) {
	if intervalHours < 1 {
		intervalHours = 24
	}
	if maxRuntimeMinutes < 1 {
		maxRuntimeMinutes = 120
	}
	if dailyStartTime == "" {
		dailyStartTime = "02:00"
	}
	if dailyEndTime == "" {
		dailyEndTime = "06:00"
	}

	nextRunAt := pgtype.Timestamptz{}
	if enabled {
		nextRunAt = pgtype.Timestamptz{
			Time:  scheduler.InitialNextRunAfter(time.Now(), intervalHours, dailyStartTime, dailyEndTime),
			Valid: true,
		}
	}

	q := sqlc.New(a.db)
	return q.UpdateScheduledTaskConfig(ctx, sqlc.UpdateScheduledTaskConfigParams{
		ID:                taskID,
		Enabled:           enabled,
		IntervalHours:     intervalHours,
		DailyStartTime:    dailyStartTime,
		DailyEndTime:      dailyEndTime,
		MaxRuntimeMinutes: maxRuntimeMinutes,
		NextRunAt:         nextRunAt,
	})
}

// TriggerTask inserts the kickoff job for one scheduled task ID. The
// kickoff worker is responsible for fanning out the actual per-item
// work. UniqueByArgs on the kickoff jobs short-circuits if one is
// already queued or running, so repeated clicks coalesce.
//
// Every call through here is user-initiated ("Run Now" button, CLI), so
// the run is marked manual: it drains the whole backlog and ignores the
// task's max-runtime window. Cron-started runs go through the scheduler's
// trigger loop directly and keep the window.
func (a *App) TriggerTask(ctx context.Context, taskID string) error {
	if a.scheduler == nil {
		return ErrSchedulerUnavailable
	}
	return a.scheduler.TriggerNow(ctx, taskID, true)
}

// CancelTask cancels queued / running River jobs associated with a scheduled
// task. Kickoff workers stamp scheduled_task_id into every child job they fan
// out; unscoped watcher/manual/view jobs sharing the same worker kinds are left
// alone.
//
// A pump kickoff spends nearly its whole run snoozed, and cancelling a
// snoozed row finalizes it directly — the pump never gets to stamp the
// run's outcome. So the bookkeeping ("stopped", duration, item counts) is
// written here, from a snapshot of the kickoff row taken before the cancel.
func (a *App) CancelTask(ctx context.Context, taskID string) error {
	kinds := taskdefs.TaskKinds(taskID)
	if len(kinds) == 0 {
		return nil
	}
	var run *queueops.ActiveKickoffRun
	if def, ok := taskdefs.ByID(taskID); ok && def.Pump {
		run, _ = queueops.GetActiveKickoff(ctx, a.db, def.KickoffKind, taskID)
	}
	if _, err := a.CancelScheduledTaskJobs(ctx, taskID, kinds); err != nil {
		return err
	}
	if run != nil {
		var counters struct {
			Enqueued int `json:"enqueued"`
			Failed   int `json:"failed"`
		}
		_ = json.Unmarshal(run.Metadata, &counters)
		if _, err := a.db.Exec(ctx, `
			UPDATE scheduled_tasks
			   SET last_run_at = $2,
			       last_run_result = 'stopped',
			       last_run_error = '',
			       last_run_duration_sec = $3,
			       last_run_items_processed = $4,
			       last_run_items_total = $5
			 WHERE id = $1
		`, taskID, run.CreatedAt, int32(time.Since(run.CreatedAt).Seconds()), counters.Enqueued, counters.Enqueued+counters.Failed); err != nil {
			log.Warn().Err(err).Str("task", taskID).Msg("cancel task: stamp stopped run failed")
		}
	}
	return nil
}

// QueryTrickplayItems returns trickplay generation items with pagination.
func (a *App) QueryTrickplayItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	q := sqlc.New(a.db)

	counts, err := q.CountTrickplayEligible(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := q.ListTrickplayEligibleItems(ctx, sqlc.ListTrickplayEligibleItemsParams{
		Status:    status,
		RowLimit:  int32(limit),
		RowOffset: int32(offset),
	})
	if err != nil {
		return nil, err
	}

	items := make([]TaskItem, 0, len(rows))
	for _, r := range rows {
		s := "pending"
		if r.HasTrickplay {
			s = "complete"
		}
		items = append(items, TaskItem{
			ID:     r.ID,
			Name:   filepath.Base(r.Path),
			Path:   r.Path,
			Status: s,
		})
	}

	return &TaskItemsResult{
		Items:    items,
		Total:    int(counts.Total),
		Complete: int(counts.Complete),
		Pending:  int(counts.Total - counts.Complete),
	}, nil
}

// QueryThumbnailItems returns thumbnail generation items with pagination.
func (a *App) QueryThumbnailItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	q := sqlc.New(a.db)

	counts, err := q.CountThumbnailEligible(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := q.ListThumbnailEligibleItems(ctx, sqlc.ListThumbnailEligibleItemsParams{
		Status:    status,
		RowLimit:  int32(limit),
		RowOffset: int32(offset),
	})
	if err != nil {
		return nil, err
	}

	items := make([]TaskItem, 0, len(rows))
	for _, r := range rows {
		s := "pending"
		if r.ThumbnailPath != "" {
			s = "complete"
		}
		name := r.Title
		if name == "" {
			name = filepath.Base(r.FilePath)
		}
		items = append(items, TaskItem{
			ID:     r.ID,
			Name:   name,
			Path:   r.FilePath,
			Status: s,
			Detail: r.ExtraType + " · " + r.MediaTitle,
		})
	}

	return &TaskItemsResult{
		Items:    items,
		Total:    int(counts.Total),
		Complete: int(counts.Complete),
		Pending:  int(counts.Total - counts.Complete),
	}, nil
}

// QueryScanItems returns library scan status items with pagination.
func (a *App) QueryScanItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	rows, err := a.db.Query(ctx, `
		SELECT l.id, l.name, l.media_type,
			count(lf.id) AS total_files,
			count(lf.id) FILTER (WHERE lf.status = 'pending') AS pending_files
		FROM libraries l
		LEFT JOIN library_files lf ON lf.library_id = l.id AND lf.deleted_at IS NULL
		GROUP BY l.id, l.name, l.media_type
		ORDER BY l.name ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TaskItem
	var totalLibs, withPending int
	for rows.Next() {
		var id int64
		var name, mediaType string
		var totalFiles, pendingFiles int
		if err := rows.Scan(&id, &name, &mediaType, &totalFiles, &pendingFiles); err != nil {
			continue
		}
		totalLibs++
		s := "complete"
		if pendingFiles > 0 {
			s = "pending"
			withPending++
		}
		items = append(items, TaskItem{
			ID:     id,
			Name:   name,
			Status: s,
			Detail: mediaType + " · " + strconv.Itoa(totalFiles) + " files",
		})
	}

	if items == nil {
		items = []TaskItem{}
	}

	return &TaskItemsResult{
		Items:    items,
		Total:    totalLibs,
		Complete: totalLibs - withPending,
		Pending:  withPending,
	}, nil
}

// QueryRefreshMetadataItems returns metadata refresh items with pagination.
// Drives the tasks-page items modal for the refresh_stale_items task —
// shows every matched item with its current enrichment_status (complete /
// pending / failed) plus the last error for failed ones.
func (a *App) QueryRefreshMetadataItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	var total, complete, failed int
	err := a.db.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE mi.enrichment_status = 'complete'),
			count(*) FILTER (WHERE mi.enrichment_status = 'failed')
		FROM media_item_cards mi
		WHERE mi.media_type = 'music'
		   OR EXISTS (SELECT 1 FROM media_item_external_ids ei WHERE ei.media_item_id = mi.id)
	`).Scan(&total, &complete, &failed)
	if err != nil {
		return nil, err
	}

	statusFilter := ""
	switch status {
	case "complete":
		statusFilter = `AND mi.enrichment_status = 'complete'`
	case "failed":
		statusFilter = `AND mi.enrichment_status = 'failed'`
	case "pending":
		statusFilter = `AND mi.enrichment_status NOT IN ('complete', 'failed')`
	}

	// Order: failed first (most urgent), then pending (oldest never-refreshed
	// surfaces at the top via NULLS FIRST), then completes by refresh time.
	rows, err := a.db.Query(ctx, `
		SELECT mi.id, mi.title, mi.media_type, mi.enrichment_status,
		       mi.last_enrich_error, mi.metadata_refreshed_at
		FROM media_item_cards mi
		WHERE (mi.media_type = 'music'
		       OR EXISTS (SELECT 1 FROM media_item_external_ids ei WHERE ei.media_item_id = mi.id))
		  `+statusFilter+`
		ORDER BY
		  CASE mi.enrichment_status
		    WHEN 'failed'   THEN 0
		    WHEN 'complete' THEN 2
		    ELSE 1
		  END,
		  mi.metadata_refreshed_at ASC NULLS FIRST,
		  mi.title ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TaskItem
	for rows.Next() {
		var id int64
		var title, mediaType, enrichStatus, lastError string
		var refreshedAt *string
		if err := rows.Scan(&id, &title, &mediaType, &enrichStatus, &lastError, &refreshedAt); err != nil {
			continue
		}
		s := enrichStatus
		if s == "" || s == "partial" {
			s = "pending"
		}
		detail := mediaType
		if refreshedAt != nil {
			detail += " · refreshed: " + *refreshedAt
		} else {
			detail += " · never refreshed"
		}
		items = append(items, TaskItem{
			ID:     id,
			Name:   title,
			Status: s,
			Detail: detail,
			Error:  lastError,
		})
	}

	if items == nil {
		items = []TaskItem{}
	}

	return &TaskItemsResult{
		Items:    items,
		Total:    total,
		Complete: complete,
		Failed:   failed,
		Pending:  total - complete - failed,
	}, nil
}

// QueryLoudnessItems returns track_files paginated by loudness analysis state.
// "complete" rows have integrated_lufs populated; "pending" rows are still
// waiting on the loudness queue.
func (a *App) QueryLoudnessItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	var total, complete int
	err := a.db.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE tf.integrated_lufs IS NOT NULL)
		FROM track_files tf
		JOIN library_files lf ON lf.id = tf.library_file_id
		WHERE lf.deleted_at IS NULL
	`).Scan(&total, &complete)
	if err != nil {
		return nil, err
	}

	statusFilter := ""
	switch status {
	case "complete":
		statusFilter = "AND tf.integrated_lufs IS NOT NULL"
	case "pending":
		statusFilter = "AND tf.integrated_lufs IS NULL"
	}

	rows, err := a.db.Query(ctx, `
		SELECT tf.id, lf.path, tf.integrated_lufs, tf.loudness_analyzed_at
		FROM track_files tf
		JOIN library_files lf ON lf.id = tf.library_file_id
		WHERE lf.deleted_at IS NULL
		  `+statusFilter+`
		ORDER BY tf.integrated_lufs IS NOT NULL, lf.path ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TaskItem
	for rows.Next() {
		var id int64
		var path string
		var lufs *float64
		var analyzedAt *string // pgx will scan timestamptz into *time.Time but a *string also works via the text format; using interface here keeps the column flexible
		_ = analyzedAt
		if err := rows.Scan(&id, &path, &lufs, &analyzedAt); err != nil {
			continue
		}
		s := "pending"
		detail := ""
		if lufs != nil {
			s = "complete"
			detail = strconv.FormatFloat(*lufs, 'f', 1, 64) + " LUFS"
		}
		items = append(items, TaskItem{
			ID:     id,
			Name:   filepath.Base(path),
			Path:   path,
			Status: s,
			Detail: detail,
		})
	}

	if items == nil {
		items = []TaskItem{}
	}

	return &TaskItemsResult{
		Items:    items,
		Total:    total,
		Complete: complete,
		Pending:  total - complete,
	}, nil
}

// QueryFingerprintItems returns physical music files paginated by canonical
// fingerprint state. Source size/mtime are part of completion so a file
// overwritten in place becomes pending again immediately.
func (a *App) QueryFingerprintItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	var total, complete int
	err := a.db.QueryRow(ctx, `
		WITH files AS (
			SELECT fp.library_file_id IS NOT NULL
			       AND fp.source_size = lf.size
			       AND fp.source_mtime IS NOT DISTINCT FROM lf.mtime AS complete
			FROM library_files lf
			JOIN libraries l ON l.id = lf.library_id
			LEFT JOIN library_file_fingerprints fp ON fp.library_file_id = lf.id
			WHERE l.media_type = 'music'
			  AND lf.deleted_at IS NULL
			  AND lower(lf.path) ~ '\\.(flac|mp3|m4a|aac|ogg|opus|wav|wma|ape|wv|alac|aiff|aif)$'
		)
		SELECT count(*), count(*) FILTER (WHERE complete) FROM files
	`).Scan(&total, &complete)
	if err != nil {
		return nil, err
	}

	statusFilter := ""
	switch status {
	case "complete":
		statusFilter = "WHERE complete"
	case "pending":
		statusFilter = "WHERE NOT complete"
	}

	rows, err := a.db.Query(ctx, `
		WITH files AS (
			SELECT lf.id, lf.path,
			       fp.library_file_id IS NOT NULL
			       AND fp.source_size = lf.size
			       AND fp.source_mtime IS NOT DISTINCT FROM lf.mtime AS complete,
			       fp.fingerprint_duration_secs
			FROM library_files lf
			JOIN libraries l ON l.id = lf.library_id
			LEFT JOIN library_file_fingerprints fp ON fp.library_file_id = lf.id
			WHERE l.media_type = 'music'
			  AND lf.deleted_at IS NULL
			  AND lower(lf.path) ~ '\\.(flac|mp3|m4a|aac|ogg|opus|wav|wma|ape|wv|alac|aiff|aif)$'
		)
		SELECT id, path, complete, fingerprint_duration_secs
		FROM files
		`+statusFilter+`
		ORDER BY complete, path ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TaskItem
	for rows.Next() {
		var id int64
		var path string
		var done bool
		var windowSecs *int32
		if err := rows.Scan(&id, &path, &done, &windowSecs); err != nil {
			continue
		}
		s := "pending"
		detail := ""
		if done {
			s = "complete"
			if windowSecs != nil {
				detail = strconv.Itoa(int(*windowSecs)) + "s fingerprinted"
			}
		}
		items = append(items, TaskItem{
			ID:     id,
			Name:   filepath.Base(path),
			Path:   path,
			Status: s,
			Detail: detail,
		})
	}

	if items == nil {
		items = []TaskItem{}
	}

	return &TaskItemsResult{
		Items:    items,
		Total:    total,
		Complete: complete,
		Pending:  total - complete,
	}, nil
}

// QuerySegmentsItems returns movie/episode files paginated by
// skip-segment state. "complete" rows have segments_analyzed_at stamped;
// the detail distinguishes real markers from a checked-but-empty result
// (misses re-check weekly as the community databases grow).
func (a *App) QuerySegmentsItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	var total, complete int
	err := a.db.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE lf.segments_analyzed_at IS NOT NULL)
		FROM library_files lf
		JOIN libraries l ON l.id = lf.library_id
		WHERE l.media_type IN ('movie', 'tv', 'anime')
		  AND lf.deleted_at IS NULL
		  AND lf.media_info IS NOT NULL
		  AND lf.media_item_id IS NOT NULL
	`).Scan(&total, &complete)
	if err != nil {
		return nil, err
	}

	statusFilter := ""
	switch status {
	case "complete":
		statusFilter = "AND lf.segments_analyzed_at IS NOT NULL"
	case "pending":
		statusFilter = "AND lf.segments_analyzed_at IS NULL"
	}

	rows, err := a.db.Query(ctx, `
		SELECT lf.id, lf.path, lf.segments_analyzed_at IS NOT NULL,
		       (SELECT count(*) FROM media_segments ms WHERE ms.library_file_id = lf.id)
		FROM library_files lf
		JOIN libraries l ON l.id = lf.library_id
		WHERE l.media_type IN ('movie', 'tv', 'anime')
		  AND lf.deleted_at IS NULL
		  AND lf.media_info IS NOT NULL
		  AND lf.media_item_id IS NOT NULL
		  `+statusFilter+`
		ORDER BY lf.segments_analyzed_at IS NOT NULL, lf.path ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TaskItem
	for rows.Next() {
		var id int64
		var path string
		var done bool
		var markers int
		if err := rows.Scan(&id, &path, &done, &markers); err != nil {
			continue
		}
		s := "pending"
		detail := ""
		if done {
			s = "complete"
			if markers > 0 {
				detail = strconv.Itoa(markers) + " markers"
			} else {
				detail = "no community markers yet"
			}
		}
		items = append(items, TaskItem{
			ID:     id,
			Name:   filepath.Base(path),
			Path:   path,
			Status: s,
			Detail: detail,
		})
	}

	if items == nil {
		items = []TaskItem{}
	}

	return &TaskItemsResult{
		Items:    items,
		Total:    total,
		Complete: complete,
		Pending:  total - complete,
	}, nil
}

// detectionNeededExpr is the SQL predicate for "local detection still has
// a gap to fill on this file", mirroring the pump's pending queries
// (ListSeasonsPendingDetection / ListMovieFilesPendingDetection): TV files
// need detection while missing an intro OR a credits row (any source),
// movie files while missing a credits row (movies have no intro pass).
// Requires the `library_files lf` + `libraries l` join aliases.
const detectionNeededExpr = `(CASE WHEN l.media_type IN ('tv', 'anime')
	THEN NOT EXISTS (SELECT 1 FROM media_segments ms WHERE ms.library_file_id = lf.id AND ms.segment_type = 'intro')
	  OR NOT EXISTS (SELECT 1 FROM media_segments ms WHERE ms.library_file_id = lf.id AND ms.segment_type = 'credits')
	ELSE NOT EXISTS (SELECT 1 FROM media_segments ms WHERE ms.library_file_id = lf.id AND ms.segment_type = 'credits')
END)`

// detectionCompleteExpr marks a file done for the local-detection task:
// either the detector actually ran (segments_detected_at stamped) or
// nothing is missing in the first place (community/manual rows already
// cover every type the detector would compute — the pump deliberately
// never stamps those, so counting them pending would pend forever).
const detectionCompleteExpr = `(lf.segments_detected_at IS NOT NULL OR NOT ` + detectionNeededExpr + `)`

// QueryDetectionItems returns movie/episode files paginated by local
// skip-segment detection state — the chromaprint/blackdetect gap-filler
// pass that only considers files the community fetch already checked
// (segments_analyzed_at NOT NULL). "complete" means segments_detected_at
// is stamped OR the file has no gap left to fill (TV: intro and credits
// rows both present from any source; movie: credits row present) —
// mirroring the pump's own eligibility, which skips fully covered files
// without ever stamping them. The detail counts markers this pass itself
// produced (source chromaprint/blackframe) so a file whose segments came
// from the community fetch doesn't read as having local results.
func (a *App) QueryDetectionItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	var total, complete int
	err := a.db.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE `+detectionCompleteExpr+`)
		FROM library_files lf
		JOIN libraries l ON l.id = lf.library_id
		WHERE l.media_type IN ('movie', 'tv', 'anime')
		  AND lf.deleted_at IS NULL
		  AND lf.media_info IS NOT NULL
		  AND lf.media_item_id IS NOT NULL
		  AND lf.segments_analyzed_at IS NOT NULL
	`).Scan(&total, &complete)
	if err != nil {
		return nil, err
	}

	statusFilter := ""
	switch status {
	case "complete":
		statusFilter = "AND " + detectionCompleteExpr
	case "pending":
		statusFilter = "AND NOT " + detectionCompleteExpr
	}

	rows, err := a.db.Query(ctx, `
		SELECT lf.id, lf.path, `+detectionCompleteExpr+`,
		       lf.segments_detected_at IS NOT NULL,
		       (SELECT count(*) FROM media_segments ms WHERE ms.library_file_id = lf.id AND ms.source IN ('chromaprint', 'blackframe'))
		FROM library_files lf
		JOIN libraries l ON l.id = lf.library_id
		WHERE l.media_type IN ('movie', 'tv', 'anime')
		  AND lf.deleted_at IS NULL
		  AND lf.media_info IS NOT NULL
		  AND lf.media_item_id IS NOT NULL
		  AND lf.segments_analyzed_at IS NOT NULL
		  `+statusFilter+`
		ORDER BY `+detectionCompleteExpr+`, lf.path ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TaskItem
	for rows.Next() {
		var id int64
		var path string
		var done, detected bool
		var markers int
		if err := rows.Scan(&id, &path, &done, &detected, &markers); err != nil {
			continue
		}
		s := "pending"
		detail := ""
		if done {
			s = "complete"
			switch {
			case markers > 0:
				detail = strconv.Itoa(markers) + " markers"
			case detected:
				detail = "no local markers found"
			default:
				detail = "covered by community/manual markers"
			}
		}
		items = append(items, TaskItem{
			ID:     id,
			Name:   filepath.Base(path),
			Path:   path,
			Status: s,
			Detail: detail,
		})
	}

	if items == nil {
		items = []TaskItem{}
	}

	return &TaskItemsResult{
		Items:    items,
		Total:    total,
		Complete: complete,
		Pending:  total - complete,
	}, nil
}

// QueryFacetsItems returns tracks paginated by sonic-analysis state. A track
// is "complete" when its track_facets row exists. Mirrors the scheduler's
// NextTrackForAnalysis duration filter so the modal only lists tracks we'd
// actually analyze — anything longer than MaxAnalysisDurationSeconds is
// invisible to both the count and the listing.
func (a *App) QueryFacetsItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	maxDuration := sonicanalysis.MaxAnalysisDurationSeconds

	var total, complete int
	err := a.db.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM tracks t
			 JOIN track_files tf ON tf.track_id = t.id
			 WHERE t.duration <= $1 AND tf.duration <= $1),
			(SELECT count(*) FROM track_facets tfa
			 JOIN tracks t ON t.id = tfa.track_id
			 JOIN track_files tf ON tf.track_id = t.id
			 WHERE t.duration <= $1 AND tf.duration <= $1)
	`, maxDuration).Scan(&total, &complete)
	if err != nil {
		return nil, err
	}

	statusFilter := "WHERE t.duration <= $3 AND tf.duration <= $3"
	switch status {
	case "complete":
		statusFilter += " AND tfa.track_id IS NOT NULL"
	case "pending":
		statusFilter += " AND tfa.track_id IS NULL"
	}

	rows, err := a.db.Query(ctx, `
		SELECT t.id, t.title, tfa.track_id IS NOT NULL AS analyzed
		FROM tracks t
		JOIN track_files tf ON tf.track_id = t.id
		LEFT JOIN track_facets tfa ON tfa.track_id = t.id
		`+statusFilter+`
		ORDER BY (tfa.track_id IS NULL) DESC, t.title ASC
		LIMIT $1 OFFSET $2
	`, limit, offset, maxDuration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TaskItem
	for rows.Next() {
		var id int64
		var title string
		var analyzed bool
		if err := rows.Scan(&id, &title, &analyzed); err != nil {
			continue
		}
		s := "pending"
		if analyzed {
			s = "complete"
		}
		items = append(items, TaskItem{
			ID:     id,
			Name:   title,
			Status: s,
		})
	}

	if items == nil {
		items = []TaskItem{}
	}

	return &TaskItemsResult{
		Items:    items,
		Total:    total,
		Complete: complete,
		Pending:  total - complete,
	}, nil
}

// QueryEmbedItems returns the embedding coverage for embed_recommendations:
// every video item, episode-with-overview, and metadata-bearing canonical
// recording, complete when a current-version facet row exists. Hash-level
// staleness isn't computed here — recomposing
// every doc per page view is the sweep's job, not the modal's; a stale row
// still shows complete until the next sweep rewrites it.
func (a *App) QueryEmbedItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	itemsEmbedded, itemsTotal := a.EmbeddedVideoCount(ctx)
	epEmbedded, epTotal := a.EmbeddedEpisodeCount(ctx)
	musicEmbedded, musicTotal := a.EmbeddedMusicCount(ctx)
	total := itemsTotal + epTotal + musicTotal
	complete := itemsEmbedded + epEmbedded + musicEmbedded

	statusFilter := ""
	switch status {
	case "complete":
		statusFilter = "WHERE u.done"
	case "pending":
		statusFilter = "WHERE NOT u.done"
	}

	ver := strconv.Itoa(textembed.Version)
	rows, err := a.db.Query(ctx, `
		SELECT u.id, u.name, u.path, u.done FROM (
			SELECT mi.id, mi.title AS name, mi.media_type::text AS path,
			       (f.media_item_id IS NOT NULL) AS done
			FROM media_item_cards mi
			LEFT JOIN media_item_facets f ON f.media_item_id = mi.id AND f.embedder_version >= `+ver+`
			WHERE mi.media_type IN ('movie','tv','anime')
			UNION ALL
			SELECT e.id,
			       mi.title || ' S' || lpad(se.season_number::text, 2, '0') || 'E' || lpad(e.episode_number::text, 2, '0')
			         || CASE WHEN e.title <> '' THEN ' — ' || e.title ELSE '' END AS name,
			       'episode' AS path,
			       (f.episode_id IS NOT NULL) AS done
			FROM tv_episodes e
			JOIN tv_seasons se ON se.id = e.season_id
			JOIN tv_series ts ON ts.id = se.series_id
			JOIN media_item_cards mi ON mi.id = ts.media_item_id
			LEFT JOIN episode_facets f ON f.episode_id = e.id AND f.embedder_version >= `+ver+`
			WHERE e.overview <> ''
			UNION ALL
			SELECT hashtextextended(r.recording_entity_id::text, 0) AS id,
			       CASE WHEN r.artist_name <> '' THEN r.artist_name || ' — ' ELSE '' END || r.title AS name,
			       'recording' AS path,
			       (f.recording_entity_id IS NOT NULL) AS done
			FROM music_catalog_recordings r
			LEFT JOIN music_recording_facets f ON f.recording_entity_id = r.recording_entity_id
			  AND f.embedder_version >= `+ver+`
			WHERE cardinality(r.genres) + cardinality(r.tags) + cardinality(r.moods) +
			      cardinality(r.instrumentation) + cardinality(r.vocal_characteristics) +
			      cardinality(r.recording_attributes) > 0
		) u
		`+statusFilter+`
		ORDER BY u.done, u.name ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TaskItem
	for rows.Next() {
		var id int64
		var name, kind string
		var done bool
		if err := rows.Scan(&id, &name, &kind, &done); err != nil {
			continue
		}
		s := "pending"
		if done {
			s = "complete"
		}
		items = append(items, TaskItem{ID: id, Name: name, Path: kind, Status: s})
	}
	if items == nil {
		items = []TaskItem{}
	}

	return &TaskItemsResult{
		Items:    items,
		Total:    total,
		Complete: complete,
		Pending:  total - complete,
	}, nil
}
