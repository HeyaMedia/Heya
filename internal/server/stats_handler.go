package server

import (
	"net/http"

	"github.com/karbowiak/heya/internal/service"
)

func handleDashboardStats(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stats := app.GetDashboardStats(r.Context())
		writeJSON(w, http.StatusOK, stats)
	}
}

func handleListMissing(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := app.ListMissingMedia(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query failed")
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleCleanupMissing(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count, err := app.CleanupMissingMedia(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to find missing items")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": count})
	}
}
