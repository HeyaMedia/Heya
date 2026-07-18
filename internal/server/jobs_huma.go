package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/secrettext"
	"github.com/karbowiak/heya/internal/service"
)

// registerJobRoutes covers /api/jobs/* runtime queue inspection and
// rescue/retry/cancel verbs. Recurring work is exposed separately through the
// durable global tasks under /api/tasks/*.
func registerJobRoutes(api huma.API, app *service.App) {
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/jobs", "list-jobs", "List background jobs", "Jobs")),
		func(ctx context.Context, in *struct {
			State    string `query:"state" enum:"available,running,scheduled,retryable,completed,cancelled,discarded" doc:"Filter by River state"`
			Kind     string `query:"kind" maxLength:"64" doc:"Filter by job kind (River task name)"`
			Limit    int    `query:"limit" minimum:"1" maximum:"200" default:"50"`
			BeforeID int64  `query:"before_id" minimum:"0" default:"0" doc:"Return jobs with IDs lower than this cursor"`
		}) (*JSONOutput[service.JobListResult], error) {
			result, err := app.ListJobs(ctx, in.State, in.Kind, in.Limit, in.BeforeID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(result), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/jobs/summary", "job-summary", "Job state summary", "Jobs")),
		simpleGet(app.JobSummary, 0))

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/jobs/kinds", "job-kind-summary", "Per-kind job counts", "Jobs")),
		simpleGet(app.JobKindSummary, 0))

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/jobs/rescue", "rescue-jobs", "Rescue stuck jobs", "Jobs")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[rescueBody], error) {
			rescued, retriesReset, err := app.RescueStuckJobs(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[rescueBody]{Body: rescueBody{Rescued: rescued, RetriesReset: retriesReset}}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/jobs/{id}/retry", "retry-job", "Retry a job", "Jobs")),
		func(ctx context.Context, in *IDPath) (*StatusOutput, error) {
			if err := app.RetryJob(ctx, in.ID); err != nil {
				return nil, humaServiceError(err)
			}
			return statusOK("retried"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/jobs/{id}/cancel", "cancel-job", "Cancel a job", "Jobs")),
		func(ctx context.Context, in *IDPath) (*StatusOutput, error) {
			if err := app.CancelJob(ctx, in.ID); err != nil {
				return nil, humaServiceError(err)
			}
			return statusOK("cancelled"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/jobs/completed", "clear-completed-jobs", "Clear completed jobs", "Jobs")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[clearedBody], error) {
			n, err := app.ClearCompletedJobs(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[clearedBody]{Body: clearedBody{Cleared: n}}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/jobs", "clear-all-jobs", "Clear all jobs", "Jobs")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[clearedBody], error) {
			n, err := app.ClearAllJobs(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[clearedBody]{Body: clearedBody{Cleared: n}}, nil
		})

	// Scoped flush — deletes jobs of a single kind, optionally narrowed to one
	// state. Kept distinct from "clear-all-jobs" above so a missing kind can't
	// be coerced into a queue-wide wipe; an empty kind deletes nothing.
	huma.Register(api, adminSecured(op(http.MethodDelete, "/api/jobs/by-kind", "clear-jobs-by-kind", "Clear jobs of a single kind", "Jobs")),
		func(ctx context.Context, in *struct {
			Kind  string `query:"kind" required:"true" maxLength:"64" doc:"Job kind to flush (River task name)"`
			State string `query:"state" enum:"available,running,scheduled,retryable,completed,cancelled,discarded" doc:"Optional state to narrow the flush"`
		}) (*JSONOutput[clearedBody], error) {
			n, err := app.ClearJobsByKind(ctx, in.Kind, in.State)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[clearedBody]{Body: clearedBody{Cleared: n}}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/jobs/queue/metadata", "metadata-queue-status", "Snapshot of the unified metadata enrich queue (pending counts by priority, current item, throughput)", "Jobs")),
		simpleGet(app.MetadataQueueStatus, 0))

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/jobs/worker-settings", "job-worker-settings", "Queue worker concurrency settings", "Jobs")),
		simpleGet(app.JobWorkerSettings, 0))

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/jobs/worker-settings", "set-job-worker-settings", "Save queue worker concurrency settings", "Jobs")),
		func(ctx context.Context, in *struct {
			Body service.JobWorkerUpdate
		}) (*JSONOutput[statusBody], error) {
			if err := app.SaveJobWorkerSettings(ctx, in.Body); err != nil {
				return nil, humaServiceError(err)
			}
			return &JSONOutput[statusBody]{Body: statusBody{Status: "saved"}}, nil
		})
}

// registerTaskRoutes covers /api/tasks/* — the scheduled-task UI for trickplay,
// thumbnails, scan, and metadata refresh.
func registerTaskRoutes(api huma.API, app *service.App) {
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/tasks", "list-tasks", "List scheduled tasks", "Tasks")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[[]taskResponse], error) {
			tasks, err := app.ListScheduledTasks(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			runtime := app.GetAllTaskRuntimeState(ctx)
			statsMap := app.QueryTaskStats(ctx)
			result := make([]taskResponse, len(tasks))
			for i, t := range tasks {
				result[i] = taskToResponse(t, runtime)
				if s, ok := statsMap[t.ID]; ok {
					result[i].Stats = &s
				}
			}
			return noStoreJSON(result), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/tasks/{id}/run", "run-task", "Trigger a scheduled task immediately", "Tasks")),
		func(ctx context.Context, in *struct {
			ID string `path:"id" enum:"generate_trickplay,generate_thumbnails,scan_libraries,refresh_stale_items,scan_music_loudness,scan_music_fingerprint,scan_media_segments,detect_media_segments,analyze_music_facets,cleanup_scanner_artifacts,embed_recommendations,sync_music_services" doc:"Task identifier"`
		}) (*StatusOutput, error) {
			if err := app.TriggerTask(ctx, in.ID); err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusConflict)
			}
			return statusOK("started"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/tasks/{id}/cancel", "cancel-task", "Cancel an in-flight scheduled task", "Tasks")),
		func(ctx context.Context, in *struct {
			ID string `path:"id" enum:"generate_trickplay,generate_thumbnails,scan_libraries,refresh_stale_items,scan_music_loudness,scan_music_fingerprint,scan_media_segments,detect_media_segments,analyze_music_facets,cleanup_scanner_artifacts,embed_recommendations,sync_music_services" doc:"Task identifier"`
		}) (*StatusOutput, error) {
			if err := app.CancelTask(ctx, in.ID); err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusConflict)
			}
			return statusOK("cancelled"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/tasks/{id}", "update-task", "Update a scheduled task's window/cadence", "Tasks")),
		func(ctx context.Context, in *struct {
			ID   string `path:"id" enum:"generate_trickplay,generate_thumbnails,scan_libraries,refresh_stale_items,scan_music_loudness,scan_music_fingerprint,scan_media_segments,detect_media_segments,analyze_music_facets,cleanup_scanner_artifacts,embed_recommendations,sync_music_services" doc:"Task identifier"`
			Body struct {
				Enabled           bool   `json:"enabled"`
				IntervalHours     int32  `json:"interval_hours" minimum:"0" maximum:"720"`
				DailyStartTime    string `json:"daily_start_time" maxLength:"5" doc:"HH:MM 24h or empty"`
				DailyEndTime      string `json:"daily_end_time" maxLength:"5"`
				MaxRuntimeMinutes int32  `json:"max_runtime_minutes" minimum:"0" maximum:"1440"`
			}
		}) (*JSONOutput[taskResponse], error) {
			updated, err := app.UpdateScheduledTask(ctx, in.ID, in.Body.Enabled, in.Body.IntervalHours, in.Body.MaxRuntimeMinutes, in.Body.DailyStartTime, in.Body.DailyEndTime)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return &JSONOutput[taskResponse]{Body: taskToResponse(updated, nil)}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/tasks/{id}/items", "task-items", "Items associated with a task run", "Tasks")),
		func(ctx context.Context, in *struct {
			ID     string `path:"id" enum:"generate_trickplay,generate_thumbnails,scan_libraries,refresh_stale_items,scan_music_loudness,scan_music_fingerprint,scan_media_segments,detect_media_segments,analyze_music_facets,cleanup_scanner_artifacts,embed_recommendations,sync_music_services" doc:"Task identifier"`
			Status string `query:"status" maxLength:"32" doc:"Filter by item status (pending/running/done/error)"`
			Limit  int    `query:"limit" minimum:"1" maximum:"200" default:"50"`
			Offset int    `query:"offset" minimum:"0" default:"0"`
		}) (*JSONOutput[*service.TaskItemsResult], error) {
			var (
				resp *service.TaskItemsResult
				err  error
			)
			switch in.ID {
			case "generate_trickplay":
				resp, err = app.QueryTrickplayItems(ctx, in.Status, in.Limit, in.Offset)
			case "generate_thumbnails":
				resp, err = app.QueryThumbnailItems(ctx, in.Status, in.Limit, in.Offset)
			case "scan_libraries":
				resp, err = app.QueryScanItems(ctx, in.Status, in.Limit, in.Offset)
			case "refresh_stale_items":
				resp, err = app.QueryRefreshMetadataItems(ctx, in.Status, in.Limit, in.Offset)
			case "scan_music_loudness":
				resp, err = app.QueryLoudnessItems(ctx, in.Status, in.Limit, in.Offset)
			case "scan_music_fingerprint":
				resp, err = app.QueryFingerprintItems(ctx, in.Status, in.Limit, in.Offset)
			case "scan_media_segments":
				resp, err = app.QuerySegmentsItems(ctx, in.Status, in.Limit, in.Offset)
			case "detect_media_segments":
				resp, err = app.QueryDetectionItems(ctx, in.Status, in.Limit, in.Offset)
			case "analyze_music_facets":
				resp, err = app.QueryFacetsItems(ctx, in.Status, in.Limit, in.Offset)
			case "cleanup_scanner_artifacts":
				resp = &service.TaskItemsResult{}
			case "embed_recommendations":
				resp, err = app.QueryEmbedItems(ctx, in.Status, in.Limit, in.Offset)
			case "sync_music_services":
				resp = &service.TaskItemsResult{}
			default:
				return nil, huma.Error404NotFound("unknown task")
			}
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(redactTaskItemsForResponse(resp)), nil
		})
}

func redactTaskItemsForResponse(result *service.TaskItemsResult) *service.TaskItemsResult {
	if result == nil {
		return nil
	}
	redacted := *result
	redacted.Items = append([]service.TaskItem(nil), result.Items...)
	for i := range redacted.Items {
		redacted.Items[i].Path = secrettext.Redact(redacted.Items[i].Path)
		redacted.Items[i].Error = secrettext.Redact(redacted.Items[i].Error)
	}
	return &redacted
}

type rescueBody struct {
	Rescued      int64 `json:"rescued"`
	RetriesReset int64 `json:"retries_reset"`
}

type clearedBody struct {
	Cleared int64 `json:"cleared"`
}

// taskRuntime mirrors service.TaskRuntimeState into the JSON response
// (pending + running counts derived from river_job for the task's
// kinds). Replaces the old in-memory ProgressTracker payload.
type taskRuntime struct {
	State   string `json:"state"`
	Pending int    `json:"pending"`
	Running int    `json:"running"`
}

type taskResponse struct {
	ID                    string             `json:"id"`
	DisplayName           string             `json:"display_name"`
	Description           string             `json:"description"`
	Category              string             `json:"category"`
	Enabled               bool               `json:"enabled"`
	IntervalHours         int32              `json:"interval_hours"`
	DailyStartTime        string             `json:"daily_start_time"`
	DailyEndTime          string             `json:"daily_end_time"`
	MaxRuntimeMinutes     int32              `json:"max_runtime_minutes"`
	LastRunAt             *string            `json:"last_run_at"`
	LastRunResult         string             `json:"last_run_result"`
	LastRunDurationSec    int32              `json:"last_run_duration_sec"`
	LastRunItemsProcessed int32              `json:"last_run_items_processed"`
	LastRunItemsTotal     int32              `json:"last_run_items_total"`
	NextRunAt             *string            `json:"next_run_at"`
	State                 string             `json:"state"`
	Runtime               *taskRuntime       `json:"runtime,omitempty"`
	Stats                 *service.TaskStats `json:"stats,omitempty"`
}

func taskToResponse(t sqlc.ScheduledTask, runtime map[string]service.TaskRuntimeState) taskResponse {
	r := taskResponse{
		ID:                    t.ID,
		DisplayName:           t.DisplayName,
		Description:           t.Description,
		Category:              t.Category,
		Enabled:               t.Enabled,
		IntervalHours:         t.IntervalHours,
		DailyStartTime:        t.DailyStartTime,
		DailyEndTime:          t.DailyEndTime,
		MaxRuntimeMinutes:     t.MaxRuntimeMinutes,
		LastRunResult:         t.LastRunResult,
		LastRunDurationSec:    t.LastRunDurationSec,
		LastRunItemsProcessed: t.LastRunItemsProcessed,
		LastRunItemsTotal:     t.LastRunItemsTotal,
		State:                 "idle",
	}
	if t.LastRunAt.Valid {
		s := t.LastRunAt.Time.UTC().Format("2006-01-02T15:04:05Z")
		r.LastRunAt = &s
	}
	if t.NextRunAt.Valid {
		s := t.NextRunAt.Time.UTC().Format("2006-01-02T15:04:05Z")
		r.NextRunAt = &s
	}
	if rs, ok := runtime[t.ID]; ok {
		r.State = rs.State
		r.Runtime = &taskRuntime{State: rs.State, Pending: rs.Pending, Running: rs.Running}
	}
	return r
}
