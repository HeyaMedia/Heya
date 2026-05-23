package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// MarkEpisodeWatched marks a single episode as watched for a user.
func (a *App) MarkEpisodeWatched(ctx context.Context, userID, episodeID int64) error {
	q := sqlc.New(a.db)
	return q.MarkEpisodeWatched(ctx, sqlc.MarkEpisodeWatchedParams{
		UserID:   userID,
		EntityID: episodeID,
	})
}

// UnmarkEpisodeWatched removes the watched mark from a single episode.
func (a *App) UnmarkEpisodeWatched(ctx context.Context, userID, episodeID int64) error {
	q := sqlc.New(a.db)
	return q.UnmarkEpisodeWatched(ctx, sqlc.UnmarkEpisodeWatchedParams{
		UserID:   userID,
		EntityID: episodeID,
	})
}

// MarkSeasonWatched marks all episodes in a season as watched.
func (a *App) MarkSeasonWatched(ctx context.Context, userID, seasonID int64) error {
	q := sqlc.New(a.db)
	return q.MarkSeasonWatched(ctx, sqlc.MarkSeasonWatchedParams{
		UserID:   userID,
		SeasonID: seasonID,
	})
}

// UnmarkSeasonWatched removes watched marks from all episodes in a season.
func (a *App) UnmarkSeasonWatched(ctx context.Context, userID, seasonID int64) error {
	q := sqlc.New(a.db)
	return q.UnmarkSeasonWatched(ctx, sqlc.UnmarkSeasonWatchedParams{
		UserID:   userID,
		SeasonID: seasonID,
	})
}

// MarkShowWatched marks all episodes in a show as watched.
func (a *App) MarkShowWatched(ctx context.Context, userID, mediaItemID int64) error {
	q := sqlc.New(a.db)
	return q.MarkShowWatched(ctx, sqlc.MarkShowWatchedParams{
		UserID:      userID,
		MediaItemID: mediaItemID,
	})
}

// UnmarkShowWatched removes watched marks from all episodes in a show.
func (a *App) UnmarkShowWatched(ctx context.Context, userID, mediaItemID int64) error {
	q := sqlc.New(a.db)
	return q.UnmarkShowWatched(ctx, sqlc.UnmarkShowWatchedParams{
		UserID:      userID,
		MediaItemID: mediaItemID,
	})
}

// MarkMovieWatched marks a movie as watched.
func (a *App) MarkMovieWatched(ctx context.Context, userID, mediaItemID int64) error {
	q := sqlc.New(a.db)
	return q.MarkMovieWatched(ctx, sqlc.MarkMovieWatchedParams{
		UserID:   userID,
		EntityID: mediaItemID,
	})
}

// UnmarkMovieWatched removes the watched mark from a movie.
func (a *App) UnmarkMovieWatched(ctx context.Context, userID, mediaItemID int64) error {
	q := sqlc.New(a.db)
	return q.UnmarkMovieWatched(ctx, sqlc.UnmarkMovieWatchedParams{
		UserID:   userID,
		EntityID: mediaItemID,
	})
}

// UserMediaState holds the fully-watched show IDs and favorited media item IDs.
type UserMediaState struct {
	WatchedIDs   []int64 `json:"watched"`
	FavoritedIDs []int64 `json:"favorited"`
}

// GetUserMediaState returns all fully-watched show IDs and favorited media item IDs for a user.
func (a *App) GetUserMediaState(ctx context.Context, userID int64) (UserMediaState, error) {
	q := sqlc.New(a.db)
	watchedIDs, _ := q.ListFullyWatchedShows(ctx, userID)
	favIDs, _ := q.ListFavoritedMediaItemIDs(ctx, userID)
	return UserMediaState{WatchedIDs: watchedIDs, FavoritedIDs: favIDs}, nil
}

// SeasonWatchInfo contains per-season watched episode counts and IDs.
type SeasonWatchInfo struct {
	SeasonID   int64   `json:"season_id"`
	Watched    int32   `json:"watched"`
	Total      int     `json:"total"`
	EpisodeIDs []int64 `json:"episode_ids"`
}

// GetWatchedEpisodes returns per-season watched episode info for a series.
func (a *App) GetWatchedEpisodes(ctx context.Context, userID, mediaItemID int64) ([]SeasonWatchInfo, error) {
	q := sqlc.New(a.db)

	series, err := q.GetTVSeriesByMediaItemID(ctx, mediaItemID)
	if err != nil {
		return nil, fmt.Errorf("series not found: %w", err)
	}

	seasons, _ := q.ListTVSeasonsBySeries(ctx, series.ID)

	var result []SeasonWatchInfo
	for _, s := range seasons {
		eps, _ := q.ListTVEpisodesBySeason(ctx, s.ID)
		epIDs := make([]int64, len(eps))
		for i, e := range eps {
			epIDs[i] = e.ID
		}

		watched, _ := q.CountWatchedInSeason(ctx, sqlc.CountWatchedInSeasonParams{
			UserID:   userID,
			SeasonID: s.ID,
		})

		watchedIDs, _ := q.ListWatchedEpisodeIDs(ctx, sqlc.ListWatchedEpisodeIDsParams{
			UserID:  userID,
			Column2: epIDs,
		})

		result = append(result, SeasonWatchInfo{
			SeasonID:   s.ID,
			Watched:    watched,
			Total:      len(eps),
			EpisodeIDs: watchedIDs,
		})
	}

	return result, nil
}

// UpNextResult describes the next unwatched episode for a show.
type UpNextResult struct {
	HasNext       bool   `json:"has_next"`
	EpisodeID     int64  `json:"episode_id,omitempty"`
	EpisodeNumber int32  `json:"episode_number,omitempty"`
	EpisodeTitle  string `json:"episode_title,omitempty"`
	SeasonNumber  int32  `json:"season_number,omitempty"`
	SeasonID      int64  `json:"season_id,omitempty"`
	MediaItemID   int64  `json:"media_item_id,omitempty"`
	Runtime       int32  `json:"runtime,omitempty"`
	FileID        int64  `json:"file_id,omitempty"`
}

// GetUpNext returns the next unwatched episode for a series, including a file ID if available.
func (a *App) GetUpNext(ctx context.Context, userID, mediaItemID int64) (UpNextResult, error) {
	q := sqlc.New(a.db)
	ep, err := q.GetNextUnwatchedEpisode(ctx, sqlc.GetNextUnwatchedEpisodeParams{
		UserID:      userID,
		MediaItemID: mediaItemID,
	})
	if err != nil {
		return UpNextResult{HasNext: false}, nil
	}

	var fileID int64
	epKey := fmt.Sprintf("s%de%d", ep.SeasonNumber, ep.EpisodeNumber)
	if files, err := q.ListEpisodeFiles(ctx, pgtype.Int8{Int64: mediaItemID, Valid: true}); err == nil {
		efMap := BuildEpisodeFileMap(files)
		if entry, ok := efMap[epKey]; ok {
			fileID = entry.FileID
		}
	}

	return UpNextResult{
		HasNext:       true,
		EpisodeID:     ep.EpisodeID,
		EpisodeNumber: ep.EpisodeNumber,
		EpisodeTitle:  ep.Title,
		SeasonNumber:  ep.SeasonNumber,
		SeasonID:      ep.SeasonID,
		MediaItemID:   ep.MediaItemID,
		Runtime:       ep.RuntimeMinutes,
		FileID:        fileID,
	}, nil
}
