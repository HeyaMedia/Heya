package service

import (
	"context"
	"path/filepath"
	"strconv"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/karbowiak/heya/internal/taskdefs"
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
	for _, def := range defs {
		counts, err := queueops.CountScheduledTask(ctx, a.db, def.ID, taskdefs.TaskKinds(def.ID))
		if err != nil {
			continue
		}
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

	var trickTotal, trickDone int
	row := a.db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE lf.media_info IS NOT NULL AND lf.media_info->'streams' @> '[{"codec_type":"video"}]'),
			count(*) FILTER (WHERE lf.has_trickplay = true AND lf.media_info IS NOT NULL AND lf.media_info->'streams' @> '[{"codec_type":"video"}]')
		FROM library_files lf
		JOIN libraries l ON l.id = lf.library_id
		WHERE lf.deleted_at IS NULL
		  AND lf.status = 'matched'
		  AND l.settings->>'enable_trickplay' = 'true'
	`)
	if row.Scan(&trickTotal, &trickDone) == nil {
		stats["generate_trickplay"] = TaskStats{
			Complete: trickDone,
			Pending:  trickTotal - trickDone,
			Total:    trickTotal,
		}
	}

	var thumbTotal, thumbDone int
	row = a.db.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE me.thumbnail_path != '')
		FROM media_extras me
		JOIN media_items mi ON mi.id = me.media_item_id
		JOIN libraries l ON l.id = mi.library_id
		WHERE me.file_path != ''
		  AND l.settings->>'generate_thumbnails' = 'true'
	`)
	if row.Scan(&thumbTotal, &thumbDone) == nil {
		stats["generate_thumbnails"] = TaskStats{
			Complete: thumbDone,
			Pending:  thumbTotal - thumbDone,
			Total:    thumbTotal,
		}
	}

	var libTotal int
	var libWithPending int
	row = a.db.QueryRow(ctx, `
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
		FROM media_items mi
		JOIN libraries l ON l.id = mi.library_id
		WHERE (mi.media_type = 'music' OR mi.external_ids != '{}')
		  AND COALESCE(NULLIF((l.settings->>'metadata_refresh_days')::int, 0), 0) > 0
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

	q := sqlc.New(a.db)
	return q.UpdateScheduledTaskConfig(ctx, sqlc.UpdateScheduledTaskConfigParams{
		ID:                taskID,
		Enabled:           enabled,
		IntervalHours:     intervalHours,
		DailyStartTime:    dailyStartTime,
		DailyEndTime:      dailyEndTime,
		MaxRuntimeMinutes: maxRuntimeMinutes,
	})
}

// TriggerTask inserts the kickoff job for one scheduled task ID. The
// kickoff worker is responsible for fanning out the actual per-item
// work. UniqueByArgs on the kickoff jobs short-circuits if one is
// already queued or running, so repeated clicks coalesce.
func (a *App) TriggerTask(ctx context.Context, taskID string) error {
	if a.scheduler == nil {
		return ErrSchedulerUnavailable
	}
	return a.scheduler.TriggerNow(ctx, taskID)
}

// CancelTask cancels queued / running River jobs associated with a scheduled
// task. Kickoff workers stamp scheduled_task_id into every child job they fan
// out; unscoped watcher/manual/view jobs sharing the same worker kinds are left
// alone.
func (a *App) CancelTask(ctx context.Context, taskID string) error {
	kinds := taskdefs.TaskKinds(taskID)
	if len(kinds) == 0 {
		return nil
	}
	_, err := a.CancelScheduledTaskJobs(ctx, taskID, kinds)
	return err
}

// QueryTrickplayItems returns trickplay generation items with pagination.
func (a *App) QueryTrickplayItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	var total, complete int
	err := a.db.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE lf.has_trickplay = true)
		FROM library_files lf
		JOIN libraries l ON l.id = lf.library_id
		WHERE lf.deleted_at IS NULL
		  AND lf.status = 'matched'
		  AND lf.media_info IS NOT NULL
		  AND lf.media_info->'streams' @> '[{"codec_type":"video"}]'
		  AND l.settings->>'enable_trickplay' = 'true'
	`).Scan(&total, &complete)
	if err != nil {
		return nil, err
	}

	statusFilter := ""
	switch status {
	case "complete":
		statusFilter = "AND lf.has_trickplay = true"
	case "pending":
		statusFilter = "AND lf.has_trickplay = false"
	}

	rows, err := a.db.Query(ctx, `
		SELECT lf.id, lf.path, lf.has_trickplay
		FROM library_files lf
		JOIN libraries l ON l.id = lf.library_id
		WHERE lf.deleted_at IS NULL
		  AND lf.status = 'matched'
		  AND lf.media_info IS NOT NULL
		  AND lf.media_info->'streams' @> '[{"codec_type":"video"}]'
		  AND l.settings->>'enable_trickplay' = 'true'
		  `+statusFilter+`
		ORDER BY lf.has_trickplay ASC, lf.path ASC
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
		var hasTrickplay bool
		if err := rows.Scan(&id, &path, &hasTrickplay); err != nil {
			continue
		}
		s := "pending"
		if hasTrickplay {
			s = "complete"
		}
		items = append(items, TaskItem{
			ID:     id,
			Name:   filepath.Base(path),
			Path:   path,
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

// QueryThumbnailItems returns thumbnail generation items with pagination.
func (a *App) QueryThumbnailItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	var total, complete int
	err := a.db.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE me.thumbnail_path != '')
		FROM media_extras me
		JOIN media_items mi ON mi.id = me.media_item_id
		JOIN libraries l ON l.id = mi.library_id
		WHERE me.file_path != ''
		  AND l.settings->>'generate_thumbnails' = 'true'
	`).Scan(&total, &complete)
	if err != nil {
		return nil, err
	}

	statusFilter := ""
	switch status {
	case "complete":
		statusFilter = "AND me.thumbnail_path != ''"
	case "pending":
		statusFilter = "AND me.thumbnail_path = ''"
	}

	rows, err := a.db.Query(ctx, `
		SELECT me.id, me.title, me.file_path, me.thumbnail_path, me.extra_type, mi.title
		FROM media_extras me
		JOIN media_items mi ON mi.id = me.media_item_id
		JOIN libraries l ON l.id = mi.library_id
		WHERE me.file_path != ''
		  AND l.settings->>'generate_thumbnails' = 'true'
		  `+statusFilter+`
		ORDER BY (me.thumbnail_path = '') DESC, mi.title ASC, me.title ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TaskItem
	for rows.Next() {
		var id int64
		var title, filePath, thumbPath, extraType, mediaTitle string
		if err := rows.Scan(&id, &title, &filePath, &thumbPath, &extraType, &mediaTitle); err != nil {
			continue
		}
		s := "pending"
		if thumbPath != "" {
			s = "complete"
		}
		name := title
		if name == "" {
			name = filepath.Base(filePath)
		}
		items = append(items, TaskItem{
			ID:     id,
			Name:   name,
			Path:   filePath,
			Status: s,
			Detail: extraType + " · " + mediaTitle,
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
		FROM media_items mi
		JOIN libraries l ON l.id = mi.library_id
		WHERE (mi.media_type = 'music' OR mi.external_ids != '{}')
		  AND COALESCE(NULLIF((l.settings->>'metadata_refresh_days')::int, 0), 0) > 0
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
		FROM media_items mi
		JOIN libraries l ON l.id = mi.library_id
		WHERE (mi.media_type = 'music' OR mi.external_ids != '{}')
		  AND COALESCE(NULLIF((l.settings->>'metadata_refresh_days')::int, 0), 0) > 0
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
