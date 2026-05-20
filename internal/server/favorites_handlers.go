package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
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

		q := sqlc.New(app.DB)
		ctx := r.Context()

		favorited, _ := q.IsFavorited(ctx, sqlc.IsFavoritedParams{
			UserID:     user.ID,
			EntityType: req.EntityType,
			EntityID:   req.EntityID,
		})

		if favorited {
			if err := q.RemoveFavorite(ctx, sqlc.RemoveFavoriteParams{
				UserID:     user.ID,
				EntityType: req.EntityType,
				EntityID:   req.EntityID,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"favorited": false})
		} else {
			if _, err := q.ToggleFavorite(ctx, sqlc.ToggleFavoriteParams{
				UserID:     user.ID,
				EntityType: req.EntityType,
				EntityID:   req.EntityID,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"favorited": true})
		}
	}
}

func handleCheckFavorite(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		entityType := r.URL.Query().Get("entity_type")
		entityID, _ := strconv.ParseInt(r.URL.Query().Get("entity_id"), 10, 64)

		q := sqlc.New(app.DB)
		favorited, _ := q.IsFavorited(r.Context(), sqlc.IsFavoritedParams{
			UserID:     user.ID,
			EntityType: entityType,
			EntityID:   entityID,
		})

		writeJSON(w, http.StatusOK, map[string]any{"favorited": favorited})
	}
}
