package server

import (
	"net/http"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
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

		q := sqlc.New(app.DB)
		ctx := r.Context()
		result := map[string]any{}

		switch req.Scope {
		case "movies":
			favIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: user.ID, EntityType: "media_item"})
			watchedIDs, _ := q.ListWatchedMovieIDs(ctx, user.ID)
			result["favorited"] = favIDs
			result["watched"] = watchedIDs

		case "series":
			showCounts, _ := q.ListShowWatchCounts(ctx, user.ID)
			favIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: user.ID, EntityType: "media_item"})

			type showState struct {
				MediaItemID     int64 `json:"media_item_id"`
				TotalEpisodes   int32 `json:"total_episodes"`
				WatchedEpisodes int32 `json:"watched_episodes"`
			}
			shows := make([]showState, len(showCounts))
			for i, s := range showCounts {
				shows[i] = showState{
					MediaItemID:     s.MediaItemID,
					TotalEpisodes:   s.TotalEpisodes,
					WatchedEpisodes: s.WatchedEpisodes,
				}
			}
			result["shows"] = shows
			result["favorited"] = favIDs

		case "seasons":
			if req.SeriesID == 0 {
				writeError(w, http.StatusBadRequest, "series_id required for scope=seasons")
				return
			}
			series, err := q.GetTVSeriesByMediaItemID(ctx, req.SeriesID)
			if err != nil {
				writeError(w, http.StatusNotFound, "series not found")
				return
			}

			seasonCounts, _ := q.ListSeasonWatchCounts(ctx, sqlc.ListSeasonWatchCountsParams{
				UserID:   user.ID,
				SeriesID: series.ID,
			})

			favMediaIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: user.ID, EntityType: "media_item"})
			favSeasonIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: user.ID, EntityType: "season"})

			type seasonState struct {
				SeasonID        int64 `json:"season_id"`
				TotalEpisodes   int32 `json:"total_episodes"`
				WatchedEpisodes int32 `json:"watched_episodes"`
			}
			seasons := make([]seasonState, len(seasonCounts))
			for i, s := range seasonCounts {
				seasons[i] = seasonState{
					SeasonID:        s.SeasonID,
					TotalEpisodes:   s.TotalEpisodes,
					WatchedEpisodes: s.WatchedEpisodes,
				}
			}
			result["seasons"] = seasons
			result["favorited_media"] = favMediaIDs
			result["favorited_seasons"] = favSeasonIDs

		case "episodes":
			if req.SeriesID == 0 {
				writeError(w, http.StatusBadRequest, "series_id required for scope=episodes")
				return
			}
			series, err := q.GetTVSeriesByMediaItemID(ctx, req.SeriesID)
			if err != nil {
				writeError(w, http.StatusNotFound, "series not found")
				return
			}

			seasonCounts, _ := q.ListSeasonWatchCounts(ctx, sqlc.ListSeasonWatchCountsParams{
				UserID:   user.ID,
				SeriesID: series.ID,
			})
			watchedEpIDs, _ := q.ListWatchedEpisodeIDsForSeries(ctx, sqlc.ListWatchedEpisodeIDsForSeriesParams{
				UserID:   user.ID,
				SeriesID: series.ID,
			})

			favSeasonIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: user.ID, EntityType: "season"})
			favMediaIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: user.ID, EntityType: "media_item"})

			type seasonState struct {
				SeasonID        int64 `json:"season_id"`
				TotalEpisodes   int32 `json:"total_episodes"`
				WatchedEpisodes int32 `json:"watched_episodes"`
			}
			seasons := make([]seasonState, len(seasonCounts))
			for i, s := range seasonCounts {
				seasons[i] = seasonState{
					SeasonID:        s.SeasonID,
					TotalEpisodes:   s.TotalEpisodes,
					WatchedEpisodes: s.WatchedEpisodes,
				}
			}
			epProgress, _ := q.ListEpisodeProgressForSeries(ctx, sqlc.ListEpisodeProgressForSeriesParams{
				UserID:   user.ID,
				SeriesID: series.ID,
			})

			type epProg struct {
				EpisodeID       int64 `json:"episode_id"`
				ProgressSeconds int32 `json:"progress_seconds"`
				TotalSeconds    int32 `json:"total_seconds"`
				Completed       bool  `json:"completed"`
			}
			progress := make([]epProg, len(epProgress))
			for i, p := range epProgress {
				progress[i] = epProg{
					EpisodeID:       p.EpisodeID,
					ProgressSeconds: p.ProgressSeconds,
					TotalSeconds:    p.TotalSeconds,
					Completed:       p.Completed,
				}
			}

			result["seasons"] = seasons
			result["watched_episode_ids"] = watchedEpIDs
			result["episode_progress"] = progress
			result["favorited_media"] = favMediaIDs
			result["favorited_seasons"] = favSeasonIDs

		default:
			writeError(w, http.StatusBadRequest, "scope must be one of: movies, series, seasons, episodes")
			return
		}

		writeJSON(w, http.StatusOK, result)
	}
}
