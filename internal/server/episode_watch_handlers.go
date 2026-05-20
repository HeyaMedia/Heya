package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
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

		q := sqlc.New(app.DB)
		q.MarkEpisodeWatched(r.Context(), sqlc.MarkEpisodeWatchedParams{
			UserID:   user.ID,
			EntityID: episodeID,
		})
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

		q := sqlc.New(app.DB)
		q.UnmarkEpisodeWatched(r.Context(), sqlc.UnmarkEpisodeWatchedParams{
			UserID:   user.ID,
			EntityID: episodeID,
		})
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

		q := sqlc.New(app.DB)
		if req.Watched {
			q.MarkSeasonWatched(r.Context(), sqlc.MarkSeasonWatchedParams{
				UserID:   user.ID,
				SeasonID: seasonID,
			})
		} else {
			q.UnmarkSeasonWatched(r.Context(), sqlc.UnmarkSeasonWatchedParams{
				UserID:   user.ID,
				SeasonID: seasonID,
			})
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

		q := sqlc.New(app.DB)
		if req.Watched {
			q.MarkShowWatched(r.Context(), sqlc.MarkShowWatchedParams{
				UserID:      user.ID,
				MediaItemID: mediaItemID,
			})
		} else {
			q.UnmarkShowWatched(r.Context(), sqlc.UnmarkShowWatchedParams{
				UserID:      user.ID,
				MediaItemID: mediaItemID,
			})
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

		q := sqlc.New(app.DB)
		if req.Watched {
			q.MarkMovieWatched(r.Context(), sqlc.MarkMovieWatchedParams{
				UserID:   user.ID,
				EntityID: mediaItemID,
			})
		} else {
			q.UnmarkMovieWatched(r.Context(), sqlc.UnmarkMovieWatchedParams{
				UserID:   user.ID,
				EntityID: mediaItemID,
			})
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

		q := sqlc.New(app.DB)
		ctx := r.Context()

		watchedIDs, _ := q.ListFullyWatchedShows(ctx, user.ID)
		favIDs, _ := q.ListFavoritedMediaItemIDs(ctx, user.ID)

		writeJSON(w, http.StatusOK, map[string]any{
			"watched":   watchedIDs,
			"favorited": favIDs,
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

		q := sqlc.New(app.DB)
		series, err := q.GetTVSeriesByMediaItemID(r.Context(), mediaID)
		if err != nil {
			writeError(w, http.StatusNotFound, "series not found")
			return
		}

		seasons, _ := q.ListTVSeasonsBySeries(r.Context(), series.ID)

		type seasonWatch struct {
			SeasonID   int64   `json:"season_id"`
			Watched    int32   `json:"watched"`
			Total      int     `json:"total"`
			EpisodeIDs []int64 `json:"episode_ids"`
		}

		var result []seasonWatch
		for _, s := range seasons {
			eps, _ := q.ListTVEpisodesBySeason(r.Context(), s.ID)
			epIDs := make([]int64, len(eps))
			for i, e := range eps {
				epIDs[i] = e.ID
			}

			watched, _ := q.CountWatchedInSeason(r.Context(), sqlc.CountWatchedInSeasonParams{
				UserID:   user.ID,
				SeasonID: s.ID,
			})

			watchedIDs, _ := q.ListWatchedEpisodeIDs(r.Context(), sqlc.ListWatchedEpisodeIDsParams{
				UserID:  user.ID,
				Column2: epIDs,
			})

			result = append(result, seasonWatch{
				SeasonID:   s.ID,
				Watched:    watched,
				Total:      len(eps),
				EpisodeIDs: watchedIDs,
			})
		}

		writeJSON(w, http.StatusOK, result)
	}
}
