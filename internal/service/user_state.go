package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// ShowWatchState describes the watched state of a single show.
type ShowWatchState struct {
	MediaItemID     int64 `json:"media_item_id"`
	TotalEpisodes   int32 `json:"total_episodes"`
	WatchedEpisodes int32 `json:"watched_episodes"`
}

// SeasonWatchState describes the watched state of a single season.
type SeasonWatchState struct {
	SeasonID        int64 `json:"season_id"`
	TotalEpisodes   int32 `json:"total_episodes"`
	WatchedEpisodes int32 `json:"watched_episodes"`
}

// EpisodeProgress describes watch progress for a single episode.
type EpisodeProgress struct {
	EpisodeID       int64 `json:"episode_id"`
	ProgressSeconds int32 `json:"progress_seconds"`
	TotalSeconds    int32 `json:"total_seconds"`
	Completed       bool  `json:"completed"`
}

// GetUserState builds user state for the given scope.
// Scope must be "movies", "series", "seasons", or "episodes".
// For "seasons" and "episodes", seriesMediaItemID must be non-zero.
func (a *App) GetUserState(ctx context.Context, userID int64, scope string, seriesMediaItemID int64) (map[string]any, error) {
	q := sqlc.New(a.db)
	result := map[string]any{}

	switch scope {
	case "movies":
		favIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: userID, EntityType: "media_item"})
		watchedIDs, _ := q.ListWatchedMovieIDs(ctx, userID)
		result["favorited"] = favIDs
		result["watched"] = watchedIDs

	case "series":
		showCounts, _ := q.ListShowWatchCounts(ctx, userID)
		favIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: userID, EntityType: "media_item"})

		// Totals are the provider-catalog count (tv_series.number_of_episodes)
		// which counts unaired episodes too — but bulk-mark only writes the
		// episodes we hold, so progress/fully-watched must be measured against
		// present episodes. Overlaying only shows with watch rows bounds the
		// cost; a zero-watched show renders no badge either way.
		var watchedIDs []int64
		for _, s := range showCounts {
			if s.WatchedEpisodes > 0 {
				watchedIDs = append(watchedIDs, s.MediaItemID)
			}
		}
		totals, terr := a.presentEpisodeTotals(ctx, q, watchedIDs)
		if terr != nil {
			totals = map[int64]int{}
		}

		shows := make([]ShowWatchState, len(showCounts))
		for i, s := range showCounts {
			total := s.TotalEpisodes
			if t, ok := totals[s.MediaItemID]; ok && s.WatchedEpisodes > 0 {
				total = int32(t)
			}
			shows[i] = ShowWatchState{
				MediaItemID:     s.MediaItemID,
				TotalEpisodes:   total,
				WatchedEpisodes: min(s.WatchedEpisodes, total),
			}
		}
		result["shows"] = shows
		result["favorited"] = favIDs

	case "seasons":
		if seriesMediaItemID == 0 {
			return nil, fmt.Errorf("series_id required for scope=seasons")
		}
		series, err := q.GetTVSeriesByMediaItemID(ctx, seriesMediaItemID)
		if err != nil {
			return nil, fmt.Errorf("series not found: %w", err)
		}

		seasonCounts, _ := q.ListSeasonWatchCounts(ctx, sqlc.ListSeasonWatchCountsParams{
			UserID:   userID,
			SeriesID: series.ID,
		})

		favMediaIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: userID, EntityType: "media_item"})
		favSeasonIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: userID, EntityType: "season"})

		seasons := make([]SeasonWatchState, len(seasonCounts))
		for i, s := range seasonCounts {
			seasons[i] = SeasonWatchState{
				SeasonID:        s.SeasonID,
				TotalEpisodes:   s.TotalEpisodes,
				WatchedEpisodes: s.WatchedEpisodes,
			}
		}
		result["seasons"] = seasons
		result["favorited_media"] = favMediaIDs
		result["favorited_seasons"] = favSeasonIDs

	case "episodes":
		if seriesMediaItemID == 0 {
			return nil, fmt.Errorf("series_id required for scope=episodes")
		}
		series, err := q.GetTVSeriesByMediaItemID(ctx, seriesMediaItemID)
		if err != nil {
			return nil, fmt.Errorf("series not found: %w", err)
		}

		seasonCounts, _ := q.ListSeasonWatchCounts(ctx, sqlc.ListSeasonWatchCountsParams{
			UserID:   userID,
			SeriesID: series.ID,
		})
		watchedEpIDs, _ := q.ListWatchedEpisodeIDsForSeries(ctx, sqlc.ListWatchedEpisodeIDsForSeriesParams{
			UserID:   userID,
			SeriesID: series.ID,
		})

		favSeasonIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: userID, EntityType: "season"})
		favMediaIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: userID, EntityType: "media_item"})

		seasons := make([]SeasonWatchState, len(seasonCounts))
		for i, s := range seasonCounts {
			seasons[i] = SeasonWatchState{
				SeasonID:        s.SeasonID,
				TotalEpisodes:   s.TotalEpisodes,
				WatchedEpisodes: s.WatchedEpisodes,
			}
		}

		epProgress, _ := q.ListEpisodeProgressForSeries(ctx, sqlc.ListEpisodeProgressForSeriesParams{
			UserID:   userID,
			SeriesID: series.ID,
		})

		progress := make([]EpisodeProgress, len(epProgress))
		for i, p := range epProgress {
			progress[i] = EpisodeProgress{
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
		return nil, fmt.Errorf("scope must be one of: movies, series, seasons, episodes")
	}

	return result, nil
}
