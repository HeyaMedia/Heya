package server

import (
	"net/http"

	"github.com/karbowiak/heya/internal/service"
)

func handleListSchedules(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := app.ListSchedules(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, entries)
	}
}
