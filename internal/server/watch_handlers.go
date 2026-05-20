package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/service"
)

type watchProgressRequest struct {
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id"`
	Progress   int32  `json:"progress_seconds"`
	Total      int32  `json:"total_seconds"`
}

func handleWatchProgress(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var req watchProgressRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.EntityType == "" {
			req.EntityType = "movie"
		}
		if req.EntityID == 0 {
			mediaItemID, err := strconv.ParseInt(r.PathValue("media_item_id"), 10, 64)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid media item id")
				return
			}
			req.EntityID = mediaItemID
		}

		completed := req.Total > 0 && req.Progress >= req.Total-30

		if app.Hub != nil {
			app.Hub.Emit(eventhub.EventMediaWatched, eventhub.WatchPayload{
				UserID:      user.ID,
				MediaItemID: req.EntityID,
				Progress:    req.Progress,
				Total:       req.Total,
				Completed:   completed,
			})
		}

		q := sqlc.New(app.DB)
		entry, err := q.UpsertWatchProgress(r.Context(), sqlc.UpsertWatchProgressParams{
			UserID:          user.ID,
			EntityType:      req.EntityType,
			EntityID:        req.EntityID,
			ProgressSeconds: req.Progress,
			TotalSeconds:    req.Total,
			Completed:       completed,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, entry)
	}
}

func handleContinueWatching(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		q := sqlc.New(app.DB)
		items, err := q.ListContinueWatching(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleRecentlyWatched(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		q := sqlc.New(app.DB)
		items, err := q.ListRecentlyWatched(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func handleWatchHistory(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		q := sqlc.New(app.DB)
		items, err := q.ListRecentlyWatched(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}
