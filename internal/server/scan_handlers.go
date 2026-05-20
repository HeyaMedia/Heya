package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/service"
)

func handleCancelLibraryScan(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library id")
			return
		}

		tag, err := app.DB.Exec(r.Context(),
			`UPDATE river_job SET state = 'cancelled', finalized_at = now()
			 WHERE state IN ('available', 'retryable', 'scheduled')
			   AND (args->>'library_id')::bigint = $1`, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "cancelled",
			"cancelled": tag.RowsAffected(),
			"message":   fmt.Sprintf("cancelled %d jobs for library %d", tag.RowsAffected(), id),
		})
	}
}

func handleCancelAllScans(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tag, err := app.DB.Exec(r.Context(),
			`UPDATE river_job SET state = 'cancelled', finalized_at = now()
			 WHERE state IN ('available', 'retryable', 'scheduled')`)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "cancelled",
			"cancelled": tag.RowsAffected(),
			"message":   fmt.Sprintf("cancelled %d jobs", tag.RowsAffected()),
		})
	}
}

func handleScanLibrary(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library id")
			return
		}

		force := r.URL.Query().Get("force") == "true"

		if err := app.EnqueueScanLibrary(r.Context(), id, force); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusAccepted, map[string]string{
			"status":  "queued",
			"message": "library scan enqueued",
		})
	}
}

func handleListLibraryFiles(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library id")
			return
		}

		limit := int32(50)
		offset := int32(0)
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.ParseInt(l, 10, 32); err == nil {
				limit = int32(n)
			}
		}
		if o := r.URL.Query().Get("offset"); o != "" {
			if n, err := strconv.ParseInt(o, 10, 32); err == nil {
				offset = int32(n)
			}
		}

		files, err := app.ListLibraryFiles(r.Context(), id, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, files)
	}
}

func handleLibraryFileStats(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library id")
			return
		}

		stats, err := app.LibraryFileStats(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusOK, stats)
	}
}
