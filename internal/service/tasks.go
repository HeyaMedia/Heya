package service

import (
	"context"
	"path/filepath"
	"strconv"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/scheduler"
)

// TaskStats holds completion counts for a scheduled task.
type TaskStats struct {
	Complete int `json:"complete"`
	Pending  int `json:"pending"`
	Total    int `json:"total"`
}

// TaskItem represents a single work item for a scheduled task.
type TaskItem struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// TaskItemsResult holds a page of task items together with counts.
type TaskItemsResult struct {
	Items    []TaskItem `json:"items"`
	Total    int        `json:"total"`
	Complete int        `json:"complete"`
	Pending  int        `json:"pending"`
}

// ListScheduledTasks returns all scheduled tasks from the database.
func (a *App) ListScheduledTasks(ctx context.Context) ([]sqlc.ScheduledTask, error) {
	q := sqlc.New(a.db)
	return q.ListScheduledTasks(ctx)
}

// GetAllTaskProgress returns the current progress for all running tasks.
func (a *App) GetAllTaskProgress() map[scheduler.TaskID]*scheduler.TaskProgress {
	if a.scheduler == nil {
		return nil
	}
	return a.scheduler.GetAllProgress()
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

	var metaTracked, metaStale int
	row = a.db.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE mi.metadata_refreshed_at < now() - make_interval(days => COALESCE(
				NULLIF((l.settings->>'metadata_refresh_days')::int, 0), 30)))
		FROM media_items mi
		JOIN libraries l ON l.id = mi.library_id
		WHERE mi.external_ids != '{}'
		  AND mi.metadata_refreshed_at IS NOT NULL
		  AND COALESCE(NULLIF((l.settings->>'metadata_refresh_days')::int, 0), 0) > 0
	`)
	if row.Scan(&metaTracked, &metaStale) == nil {
		stats["refresh_metadata"] = TaskStats{
			Complete: 0,
			Total:    metaStale,
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

// TriggerTask triggers a scheduled task to run immediately. For scan_libraries
// it also enqueues all libraries.
func (a *App) TriggerTask(ctx context.Context, taskID string) error {
	if a.scheduler == nil {
		return ErrSchedulerUnavailable
	}

	if taskID == string(scheduler.TaskScanLibraries) && a.scanTask != nil {
		q := sqlc.New(a.db)
		libs, err := q.ListLibraries(ctx)
		if err == nil {
			for _, lib := range libs {
				a.scanTask.Enqueue(lib.ID, false)
			}
		}
	}

	return a.scheduler.TriggerNow(scheduler.TaskID(taskID))
}

// CancelTask cancels a currently running scheduled task.
func (a *App) CancelTask(taskID string) error {
	if a.scheduler == nil {
		return ErrSchedulerUnavailable
	}
	return a.scheduler.Cancel(scheduler.TaskID(taskID))
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
func (a *App) QueryRefreshMetadataItems(ctx context.Context, status string, limit, offset int) (*TaskItemsResult, error) {
	var total, stale int
	err := a.db.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE mi.metadata_refreshed_at < now() - make_interval(days => COALESCE(
				NULLIF((l.settings->>'metadata_refresh_days')::int, 0), 30)))
		FROM media_items mi
		JOIN libraries l ON l.id = mi.library_id
		WHERE mi.external_ids != '{}'
		  AND mi.metadata_refreshed_at IS NOT NULL
		  AND COALESCE(NULLIF((l.settings->>'metadata_refresh_days')::int, 0), 0) > 0
	`).Scan(&total, &stale)
	if err != nil {
		return nil, err
	}

	statusFilter := ""
	switch status {
	case "complete":
		statusFilter = `AND mi.metadata_refreshed_at >= now() - make_interval(days => COALESCE(
			NULLIF((l.settings->>'metadata_refresh_days')::int, 0), 30))`
	case "pending":
		statusFilter = `AND mi.metadata_refreshed_at < now() - make_interval(days => COALESCE(
			NULLIF((l.settings->>'metadata_refresh_days')::int, 0), 30))`
	}

	rows, err := a.db.Query(ctx, `
		SELECT mi.id, mi.title, mi.media_type, mi.metadata_refreshed_at
		FROM media_items mi
		JOIN libraries l ON l.id = mi.library_id
		WHERE mi.external_ids != '{}'
		  AND mi.metadata_refreshed_at IS NOT NULL
		  AND COALESCE(NULLIF((l.settings->>'metadata_refresh_days')::int, 0), 0) > 0
		  `+statusFilter+`
		ORDER BY mi.metadata_refreshed_at ASC, mi.title ASC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TaskItem
	for rows.Next() {
		var id int64
		var title, mediaType string
		var refreshedAt *string
		if err := rows.Scan(&id, &title, &mediaType, &refreshedAt); err != nil {
			continue
		}
		s := "pending"
		detail := mediaType + " · never refreshed"
		if refreshedAt != nil {
			detail = mediaType + " · refreshed: " + *refreshedAt
			s = "complete"
		}
		items = append(items, TaskItem{
			ID:     id,
			Name:   title,
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
		Complete: total - stale,
		Pending:  stale,
	}, nil
}
