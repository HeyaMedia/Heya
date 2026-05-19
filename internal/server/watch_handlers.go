package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/service"
)

type watchProgressRequest struct {
	ProgressSeconds int32 `json:"progress_seconds"`
	TotalSeconds    int32 `json:"total_seconds"`
}

func handleWatchProgress(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		mediaItemID, err := strconv.ParseInt(r.PathValue("media_item_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media item id")
			return
		}

		var req watchProgressRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		completed := req.TotalSeconds > 0 && req.ProgressSeconds >= req.TotalSeconds-30

		q := sqlc.New(app.DB)

		existing, err := q.GetLatestWatchHistory(r.Context(), sqlc.GetLatestWatchHistoryParams{
			UserID:      user.ID,
			MediaItemID: mediaItemID,
		})
		if err == nil {
			entry, err := q.UpdateWatchProgress(r.Context(), sqlc.UpdateWatchProgressParams{
				ID:              existing.ID,
				ProgressSeconds: req.ProgressSeconds,
				TotalSeconds:    req.TotalSeconds,
				Completed:       completed,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, entry)
			return
		}

		entry, err := q.CreateWatchHistory(r.Context(), sqlc.CreateWatchHistoryParams{
			UserID:          user.ID,
			MediaItemID:     mediaItemID,
			ProgressSeconds: req.ProgressSeconds,
			TotalSeconds:    req.TotalSeconds,
			Completed:       completed,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, entry)
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

func handleWatchHistory(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
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

		q := sqlc.New(app.DB)
		items, err := q.ListWatchHistoryByUser(r.Context(), sqlc.ListWatchHistoryByUserParams{
			UserID: user.ID,
			Limit:  limit,
			Offset: offset,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}
