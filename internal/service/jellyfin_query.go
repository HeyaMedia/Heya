package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Service passthroughs for the Jellyfin-compatible API (internal/jellyfin).
// The jellyfin handlers observe the same rule as internal/server: no direct
// sqlc access — every query goes through App. Queries live in
// queries/jellyfin.sql.

func (a *App) JFListLibraryItems(ctx context.Context, p sqlc.JFListLibraryItemsParams) ([]sqlc.JFListLibraryItemsRow, int64, error) {
	p.OnlyIds = emptyNotNil(p.OnlyIds)
	p.PlayedIds = emptyNotNil(p.PlayedIds)
	p.FavoriteIds = emptyNotNil(p.FavoriteIds)
	q := sqlc.New(a.db)
	rows, err := q.JFListLibraryItems(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	total, err := q.JFCountLibraryItems(ctx, sqlc.JFCountLibraryItemsParams{
		MediaType:      p.MediaType,
		LibraryID:      p.LibraryID,
		OnlyIds:        p.OnlyIds,
		Search:         p.Search,
		FilterPlayed:   p.FilterPlayed,
		PlayedIds:      p.PlayedIds,
		FilterUnplayed: p.FilterUnplayed,
		FilterFavorite: p.FilterFavorite,
		FavoriteIds:    p.FavoriteIds,
	})
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (a *App) JFListSeasons(ctx context.Context, seriesMediaItemID int64, onlyIDs []int64) ([]sqlc.JFListSeasonsRow, error) {
	return sqlc.New(a.db).JFListSeasons(ctx, sqlc.JFListSeasonsParams{
		SeriesMediaItemID: seriesMediaItemID,
		OnlyIds:           emptyNotNil(onlyIDs),
	})
}

func (a *App) JFListEpisodes(ctx context.Context, p sqlc.JFListEpisodesParams) ([]sqlc.JFListEpisodesRow, int64, error) {
	p.OnlyIds = emptyNotNil(p.OnlyIds)
	q := sqlc.New(a.db)
	rows, err := q.JFListEpisodes(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	total, err := q.JFCountEpisodes(ctx, sqlc.JFCountEpisodesParams{
		SeasonID:          p.SeasonID,
		SeriesMediaItemID: p.SeriesMediaItemID,
		LibraryID:         p.LibraryID,
		OnlyIds:           p.OnlyIds,
		Search:            p.Search,
	})
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (a *App) JFListAlbums(ctx context.Context, p sqlc.JFListAlbumsParams) ([]sqlc.JFListAlbumsRow, int64, error) {
	p.OnlyIds = emptyNotNil(p.OnlyIds)
	q := sqlc.New(a.db)
	rows, err := q.JFListAlbums(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	total, err := q.JFCountAlbums(ctx, sqlc.JFCountAlbumsParams{
		ArtistMediaItemID: p.ArtistMediaItemID,
		LibraryID:         p.LibraryID,
		OnlyIds:           p.OnlyIds,
		Search:            p.Search,
	})
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (a *App) JFListTracks(ctx context.Context, p sqlc.JFListTracksParams) ([]sqlc.JFListTracksRow, int64, error) {
	p.OnlyIds = emptyNotNil(p.OnlyIds)
	q := sqlc.New(a.db)
	rows, err := q.JFListTracks(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	total, err := q.JFCountTracks(ctx, sqlc.JFCountTracksParams{
		AlbumID:           p.AlbumID,
		ArtistMediaItemID: p.ArtistMediaItemID,
		LibraryID:         p.LibraryID,
		OnlyIds:           p.OnlyIds,
		Search:            p.Search,
	})
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// JFUserVideoSets returns the id-sets used both to decorate dtos with
// UserData and to answer IsPlayed/IsFavorite filters: fully-watched movie
// media_item ids, fully-watched series media_item ids, favorited media_item
// ids, and per-series (watched, total) episode counts.
func (a *App) JFUserVideoSets(ctx context.Context, userID int64) (watchedMovies, watchedSeries, favorites map[int64]bool, showCounts map[int64][2]int32, err error) {
	q := sqlc.New(a.db)

	movieIDs, err := q.ListWatchedMovieIDs(ctx, userID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	watchedMovies = make(map[int64]bool, len(movieIDs))
	for _, id := range movieIDs {
		watchedMovies[id] = true
	}

	counts, err := q.ListShowWatchCounts(ctx, userID)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	watchedSeries = make(map[int64]bool)
	showCounts = make(map[int64][2]int32, len(counts))
	for _, c := range counts {
		showCounts[c.MediaItemID] = [2]int32{c.WatchedEpisodes, c.TotalEpisodes}
		if c.TotalEpisodes > 0 && c.WatchedEpisodes >= c.TotalEpisodes {
			watchedSeries[c.MediaItemID] = true
		}
	}

	favIDs, err := q.ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: userID, EntityType: "media_item"})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	favorites = make(map[int64]bool, len(favIDs))
	for _, id := range favIDs {
		favorites[id] = true
	}
	return watchedMovies, watchedSeries, favorites, showCounts, nil
}

// JFWatchProgressByIDs returns progress rows for a page of entities.
// entityType ∈ {"movie", "episode"}.
func (a *App) JFWatchProgressByIDs(ctx context.Context, userID int64, entityType string, ids []int64) (map[int64]sqlc.JFListWatchProgressByIDsRow, error) {
	if len(ids) == 0 {
		return map[int64]sqlc.JFListWatchProgressByIDsRow{}, nil
	}
	rows, err := sqlc.New(a.db).JFListWatchProgressByIDs(ctx, sqlc.JFListWatchProgressByIDsParams{
		UserID:     userID,
		EntityType: entityType,
		EntityIds:  ids,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[int64]sqlc.JFListWatchProgressByIDsRow, len(rows))
	for _, r := range rows {
		out[r.EntityID] = r
	}
	return out, nil
}

// JFNextUnwatchedEpisode wraps GetNextUnwatchedEpisode for the /Shows/NextUp
// translation. Returns ok=false when the series is fully watched.
func (a *App) JFNextUnwatchedEpisode(ctx context.Context, userID, seriesMediaItemID int64) (sqlc.GetNextUnwatchedEpisodeRow, bool, error) {
	row, err := sqlc.New(a.db).GetNextUnwatchedEpisode(ctx, sqlc.GetNextUnwatchedEpisodeParams{
		UserID:      userID,
		MediaItemID: seriesMediaItemID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.GetNextUnwatchedEpisodeRow{}, false, nil
		}
		return sqlc.GetNextUnwatchedEpisodeRow{}, false, err
	}
	return row, true, nil
}

// emptyNotNil keeps pgx happy: a nil []int64 binds as NULL, and
// cardinality(NULL) is NULL, which would disable the "0 = filter off"
// convention. Always bind at least an empty array.
func emptyNotNil(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}
