package server

import (
	"net/http"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/service"
)

func handleGetUserSettings(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		settings, err := app.GetUserSettings(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load settings")
			return
		}

		writeJSON(w, http.StatusOK, settings)
	}
}

func handleUpdateUserSettings(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var settings service.UserSettings
		if err := readJSON(r, &settings); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := app.UpdateUserSettings(r.Context(), user.ID, settings); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save settings")
			return
		}

		writeJSON(w, http.StatusOK, settings)
	}
}
