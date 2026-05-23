package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/service"
)

func handleMarkEpisodeWatched(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		episodeID, err := strconv.ParseInt(r.PathValue("episode_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid episode id")
			return
		}

		app.MarkEpisodeWatched(r.Context(), user.ID, episodeID)
		writeJSON(w, http.StatusOK, map[string]any{"watched": true})
	}
}

func handleUnmarkEpisodeWatched(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		episodeID, err := strconv.ParseInt(r.PathValue("episode_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid episode id")
			return
		}

		app.UnmarkEpisodeWatched(r.Context(), user.ID, episodeID)
		writeJSON(w, http.StatusOK, map[string]any{"watched": false})
	}
}

func handleMarkSeasonWatched(app *service.App) http.HandlerFunc {
	type markReq struct {
		Watched bool `json:"watched"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		seasonID, err := strconv.ParseInt(r.PathValue("season_id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid season id")
			return
		}

		var req markReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.Watched {
			app.MarkSeasonWatched(r.Context(), user.ID, seasonID)
		} else {
			app.UnmarkSeasonWatched(r.Context(), user.ID, seasonID)
		}
		writeJSON(w, http.StatusOK, map[string]any{"watched": req.Watched})
	}
}

func handleMarkShowWatched(app *service.App) http.HandlerFunc {
	type markReq struct {
		Watched bool `json:"watched"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		mediaItemID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		var req markReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.Watched {
			app.MarkShowWatched(r.Context(), user.ID, mediaItemID)
		} else {
			app.UnmarkShowWatched(r.Context(), user.ID, mediaItemID)
		}
		writeJSON(w, http.StatusOK, map[string]any{"watched": req.Watched})
	}
}

func handleMarkMovieWatched(app *service.App) http.HandlerFunc {
	type markReq struct {
		Watched bool `json:"watched"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		mediaItemID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		var req markReq
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.Watched {
			app.MarkMovieWatched(r.Context(), user.ID, mediaItemID)
		} else {
			app.UnmarkMovieWatched(r.Context(), user.ID, mediaItemID)
		}
		writeJSON(w, http.StatusOK, map[string]any{"watched": req.Watched})
	}
}

func handleGetUserMediaState(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		state, _ := app.GetUserMediaState(r.Context(), user.ID)
		writeJSON(w, http.StatusOK, map[string]any{
			"watched":   state.WatchedIDs,
			"favorited": state.FavoritedIDs,
		})
	}
}

func handleGetWatchedEpisodes(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		mediaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid media id")
			return
		}

		result, err := app.GetWatchedEpisodes(r.Context(), user.ID, mediaID)
		if err != nil {
			writeError(w, http.StatusNotFound, "series not found")
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}
