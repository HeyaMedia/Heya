package server

import (
	"net/http"

	"github.com/karbowiak/heya/internal/service"
)

func handleActivityFeed(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items := app.GetActivityFeed(r.Context())
		writeJSON(w, http.StatusOK, items)
	}
}
