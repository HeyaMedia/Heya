package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/karbowiak/heya/internal/service"
)

type jobRow struct {
	ID          int64     `json:"id"`
	State       string    `json:"state"`
	Kind        string    `json:"kind"`
	Queue       string    `json:"queue"`
	Args        string    `json:"args"`
	Attempt     int       `json:"attempt"`
	MaxAttempts int       `json:"max_attempts"`
	CreatedAt   time.Time `json:"created_at"`
	AttemptedAt *time.Time `json:"attempted_at,omitempty"`
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`
	Errors      string    `json:"errors,omitempty"`
}

type jobListResponse struct {
	Jobs  []jobRow `json:"jobs"`
	Total int      `json:"total"`
}

func handleListJobs(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		state := r.URL.Query().Get("state")
		kind := r.URL.Query().Get("kind")
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if limit <= 0 || limit > 200 {
			limit = 50
		}
		if offset < 0 {
			offset = 0
		}

		where := "WHERE 1=1"
		args := []any{}
		argIdx := 1

		if state != "" {
			where += " AND state = $" + strconv.Itoa(argIdx)
			args = append(args, state)
			argIdx++
		}
		if kind != "" {
			where += " AND kind = $" + strconv.Itoa(argIdx)
			args = append(args, kind)
			argIdx++
		}

		var total int
		countQuery := "SELECT count(*) FROM river_job " + where
		app.DB.QueryRow(ctx, countQuery, args...).Scan(&total)

		query := "SELECT id, state, kind, queue, args::text, attempt, max_attempts, created_at, attempted_at, finalized_at, COALESCE(errors::text, '') FROM river_job " + where +
			" ORDER BY CASE state WHEN 'running' THEN 0 WHEN 'available' THEN 1 WHEN 'retryable' THEN 2 WHEN 'scheduled' THEN 3 WHEN 'cancelled' THEN 4 WHEN 'discarded' THEN 5 WHEN 'completed' THEN 6 END, created_at DESC" +
			" LIMIT $" + strconv.Itoa(argIdx) + " OFFSET $" + strconv.Itoa(argIdx+1)
		args = append(args, limit, offset)

		rows, err := app.DB.Query(ctx, query, args...)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()

		jobs := []jobRow{}
		for rows.Next() {
			var j jobRow
			var attemptedAt, finalizedAt *time.Time
			if err := rows.Scan(&j.ID, &j.State, &j.Kind, &j.Queue, &j.Args, &j.Attempt, &j.MaxAttempts, &j.CreatedAt, &attemptedAt, &finalizedAt, &j.Errors); err != nil {
				continue
			}
			j.AttemptedAt = attemptedAt
			j.FinalizedAt = finalizedAt
			jobs = append(jobs, j)
		}

		writeJSON(w, http.StatusOK, jobListResponse{Jobs: jobs, Total: total})
	}
}

type jobSummaryRow struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

func handleJobSummary(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		rows, err := app.DB.Query(ctx, "SELECT state, count(*) FROM river_job GROUP BY state ORDER BY state")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()

		summary := []jobSummaryRow{}
		for rows.Next() {
			var s jobSummaryRow
			rows.Scan(&s.State, &s.Count)
			summary = append(summary, s)
		}
		writeJSON(w, http.StatusOK, summary)
	}
}

func handleRetryJob(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid job id")
			return
		}

		tag, err := app.DB.Exec(ctx,
			"UPDATE river_job SET state = 'available', attempt = GREATEST(attempt - 1, 0), scheduled_at = now(), finalized_at = NULL WHERE id = $1 AND state IN ('discarded', 'cancelled', 'retryable')", id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "job not found or not in a retryable state")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "retried"})
	}
}

func handleCancelJob(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid job id")
			return
		}

		tag, err := app.DB.Exec(ctx,
			"UPDATE river_job SET state = 'cancelled', finalized_at = now() WHERE id = $1 AND state IN ('available', 'retryable', 'scheduled')", id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if tag.RowsAffected() == 0 {
			writeError(w, http.StatusNotFound, "job not found or not in a cancellable state")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
	}
}

func handleClearJobs(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tag, err := app.DB.Exec(ctx, "DELETE FROM river_job WHERE state IN ('completed', 'discarded', 'cancelled')")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]int64{"cleared": tag.RowsAffected()})
	}
}

func handleClearAllJobs(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tag, err := app.DB.Exec(ctx, "DELETE FROM river_job")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]int64{"cleared": tag.RowsAffected()})
	}
}
