package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/kura/internal/scanner"
	"github.com/karbowiak/kura/internal/service"
)

func handleScanLibrary(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid library id")
			return
		}

		async := r.URL.Query().Get("async") == "true"

		if async {
			if err := app.EnqueueScanLibrary(r.Context(), id, false); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]string{
				"status":  "queued",
				"message": "library scan enqueued",
			})
			return
		}

		scanResult, err := app.ScanLibrary(r.Context(), id, scanner.ScanOptions{})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		matchResult, err := app.MatchLibrary(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"scan":  scanResult,
				"match": nil,
				"error": err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"scan":  scanResult,
			"match": matchResult,
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
