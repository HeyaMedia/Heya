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
		favIDs, _ := q.ListFavoritedMediaIDsByType(ctx, sqlc.ListFavoritedMediaIDsByTypeParams{UserID: userID, MediaType: sqlc.MediaTypeMovie})
		watchedIDs, _ := q.ListWatchedMovieIDs(ctx, userID)
		result["favorited"] = favIDs
		result["watched"] = watchedIDs

	case "series":
		showCounts, _ := q.ListShowWatchCounts(ctx, userID)
		favIDs, _ := q.ListFavoritedMediaIDsByType(ctx, sqlc.ListFavoritedMediaIDsByTypeParams{UserID: userID, MediaType: sqlc.MediaTypeTv})

		// Raw counts compare the user's watch rows to the provider-catalog
		// total — but bulk-mark only writes the episodes we hold, and stale
		// marks can sit on episodes we don't. Both sides of the fraction are
		// re-measured against the present set (presentShowWatchCounts).
		// Overlaying only shows with watch rows bounds the cost; a
		// zero-watched show renders no badge either way.
		var watchedIDs []int64
		for _, s := range showCounts {
			if s.WatchedEpisodes > 0 {
				watchedIDs = append(watchedIDs, s.MediaItemID)
			}
		}
		presentCounts, perr := a.presentShowWatchCounts(ctx, q, userID, watchedIDs)
		if perr != nil {
			presentCounts = map[int64]presentWatchCounts{}
		}

		shows := make([]ShowWatchState, len(showCounts))
		for i, s := range showCounts {
			total, watched := s.TotalEpisodes, s.WatchedEpisodes
			if pc, ok := presentCounts[s.MediaItemID]; ok && s.WatchedEpisodes > 0 {
				total, watched = int32(pc.Total), int32(pc.Watched)
			}
			shows[i] = ShowWatchState{
				MediaItemID:     s.MediaItemID,
				TotalEpisodes:   total,
				WatchedEpisodes: watched,
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

		favMediaIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: userID, EntityType: "media_item"})
		favSeasonIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: userID, EntityType: "season"})

		result["seasons"] = a.presentSeasonStates(ctx, q, userID, series)
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

		watchedEpIDs, _ := q.ListWatchedEpisodeIDsForSeries(ctx, sqlc.ListWatchedEpisodeIDsForSeriesParams{
			UserID:   userID,
			SeriesID: series.ID,
		})

		favSeasonIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: userID, EntityType: "season"})
		favMediaIDs, _ := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: userID, EntityType: "media_item"})

		seasons := a.presentSeasonStates(ctx, q, userID, series)

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

// presentSeasonStates builds per-season watched state measured against the
// episodes we actually hold: totals come from seasonPresentEpisodeSets (the
// same sets bulk-mark writes) and watched counts only the user's completed
// episodes inside those sets — a stale mark on a non-held episode neither
// completes a season nor inflates its progress. Hidden (fileless) seasons
// have no entry, matching the detail view that never lists them.
func (a *App) presentSeasonStates(ctx context.Context, q *sqlc.Queries, userID int64, series sqlc.TvSeries) []SeasonWatchState {
	sets, err := a.seasonPresentEpisodeSets(ctx, q, series)
	if err != nil {
		return []SeasonWatchState{}
	}
	watchedEpIDs, _ := q.ListWatchedEpisodeIDsForSeries(ctx, sqlc.ListWatchedEpisodeIDsForSeriesParams{
		UserID:   userID,
		SeriesID: series.ID,
	})
	watchedSet := make(map[int64]bool, len(watchedEpIDs))
	for _, id := range watchedEpIDs {
		watchedSet[id] = true
	}

	seasons := make([]SeasonWatchState, 0, len(sets))
	for seasonID, set := range sets {
		watched := 0
		for _, epID := range set {
			if watchedSet[epID] {
				watched++
			}
		}
		seasons = append(seasons, SeasonWatchState{
			SeasonID:        seasonID,
			TotalEpisodes:   int32(len(set)),
			WatchedEpisodes: int32(watched),
		})
	}
	return seasons
}
