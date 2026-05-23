package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/scheduler"
	"github.com/karbowiak/heya/internal/service"
)

type taskResponse struct {
	ID                    string                  `json:"id"`
	DisplayName           string                  `json:"display_name"`
	Description           string                  `json:"description"`
	Category              string                  `json:"category"`
	Enabled               bool                    `json:"enabled"`
	IntervalHours         int32                   `json:"interval_hours"`
	DailyStartTime        string                  `json:"daily_start_time"`
	DailyEndTime          string                  `json:"daily_end_time"`
	MaxRuntimeMinutes     int32                   `json:"max_runtime_minutes"`
	LastRunAt             *string                 `json:"last_run_at"`
	LastRunResult         string                  `json:"last_run_result"`
	LastRunDurationSec    int32                   `json:"last_run_duration_sec"`
	LastRunItemsProcessed int32                   `json:"last_run_items_processed"`
	LastRunItemsTotal     int32                   `json:"last_run_items_total"`
	NextRunAt             *string                 `json:"next_run_at"`
	State                 string                  `json:"state"`
	Progress              *scheduler.TaskProgress `json:"progress"`
	Stats                 *service.TaskStats      `json:"stats,omitempty"`
}

func taskToResponse(t sqlc.ScheduledTask, progressMap map[scheduler.TaskID]*scheduler.TaskProgress) taskResponse {
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
	if p, ok := progressMap[scheduler.TaskID(t.ID)]; ok && p != nil {
		r.State = p.State
		r.Progress = p
	}
	return r
}

func handleListTasks(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tasks, err := app.ListScheduledTasks(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		progressMap := app.GetAllTaskProgress()
		if progressMap == nil {
			progressMap = make(map[scheduler.TaskID]*scheduler.TaskProgress)
		}

		statsMap := app.QueryTaskStats(ctx)

		result := make([]taskResponse, len(tasks))
		for i, t := range tasks {
			result[i] = taskToResponse(t, progressMap)
			if s, ok := statsMap[t.ID]; ok {
				result[i].Stats = &s
			}
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func handleRunTask(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("id")
		if err := app.TriggerTask(r.Context(), taskID); err != nil {
			if err == service.ErrSchedulerUnavailable {
				writeError(w, http.StatusServiceUnavailable, err.Error())
			} else {
				writeError(w, http.StatusConflict, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
	}
}

func handleCancelTask(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("id")
		if err := app.CancelTask(taskID); err != nil {
			if err == service.ErrSchedulerUnavailable {
				writeError(w, http.StatusServiceUnavailable, err.Error())
			} else {
				writeError(w, http.StatusConflict, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
	}
}

func handleUpdateTask(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("id")
		var body struct {
			Enabled           bool   `json:"enabled"`
			IntervalHours     int32  `json:"interval_hours"`
			DailyStartTime    string `json:"daily_start_time"`
			DailyEndTime      string `json:"daily_end_time"`
			MaxRuntimeMinutes int32  `json:"max_runtime_minutes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		updated, err := app.UpdateScheduledTask(r.Context(), taskID, body.Enabled, body.IntervalHours, body.MaxRuntimeMinutes, body.DailyStartTime, body.DailyEndTime)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, taskToResponse(updated, nil))
	}
}

func handleTaskItems(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("id")
		ctx := r.Context()

		status := r.URL.Query().Get("status")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if limit <= 0 || limit > 200 {
			limit = 50
		}
		if offset < 0 {
			offset = 0
		}

		var resp *service.TaskItemsResult
		var err error

		switch taskID {
		case "generate_trickplay":
			resp, err = app.QueryTrickplayItems(ctx, status, limit, offset)
		case "generate_thumbnails":
			resp, err = app.QueryThumbnailItems(ctx, status, limit, offset)
		case "scan_libraries":
			resp, err = app.QueryScanItems(ctx, status, limit, offset)
		case "refresh_metadata":
			resp, err = app.QueryRefreshMetadataItems(ctx, status, limit, offset)
		default:
			writeError(w, http.StatusNotFound, "unknown task")
			return
		}

		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
