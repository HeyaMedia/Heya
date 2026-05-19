package server

import (
	"net/http"

	"github.com/karbowiak/kura/internal/service"
)

func handleWatcherStatus(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := app.Watcher.Status()

		type entry struct {
			LibraryID int64  `json:"library_id"`
			Path      string `json:"path"`
		}

		var entries []entry
		for id, path := range status {
			entries = append(entries, entry{LibraryID: id, Path: path})
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"watchers": entries,
			"count":    len(entries),
		})
	}
}
