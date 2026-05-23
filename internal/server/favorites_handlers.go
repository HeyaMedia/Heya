package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/service"
)

func handleToggleFavorite(app *service.App) http.HandlerFunc {
	type toggleReq struct {
		EntityType string `json:"entity_type"`
		EntityID   int64  `json:"entity_id"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req toggleReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if err := app.ToggleFavorite(r.Context(), user.ID, req.EntityID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Check the new state after toggle.
		favorited, _ := app.IsFavorited(r.Context(), user.ID, req.EntityID)
		writeJSON(w, http.StatusOK, map[string]any{"favorited": favorited})
	}
}

func handleCheckFavorite(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		entityID, _ := strconv.ParseInt(r.URL.Query().Get("entity_id"), 10, 64)

		favorited, _ := app.IsFavorited(r.Context(), user.ID, entityID)

		writeJSON(w, http.StatusOK, map[string]any{"favorited": favorited})
	}
}
