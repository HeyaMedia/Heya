package server

import (
	"net/http"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/service"
)

func handleGetUserState(app *service.App) http.HandlerFunc {
	type stateReq struct {
		Scope    string `json:"scope"`
		SeriesID int64  `json:"series_id,omitempty"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req stateReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		result, err := app.GetUserState(r.Context(), user.ID, req.Scope, req.SeriesID)
		if err != nil {
			msg := err.Error()
			if msg == "scope must be one of: movies, series, seasons, episodes" {
				writeError(w, http.StatusBadRequest, msg)
			} else if msg == "series_id required for scope=seasons" || msg == "series_id required for scope=episodes" {
				writeError(w, http.StatusBadRequest, msg)
			} else {
				writeError(w, http.StatusNotFound, msg)
			}
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}
