package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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
	p.Genres = emptyNotNil(p.Genres)
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
		Genres:         p.Genres,
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
	// Same present-episode overlay as the native user-state path: both sides
	// of the played fraction are measured against episodes we hold, not the
	// provider catalog (bulk-mark never writes unaired episodes, and stale
	// marks on non-held episodes must not count).
	var watchedIDs []int64
	for _, c := range counts {
		if c.WatchedEpisodes > 0 {
			watchedIDs = append(watchedIDs, c.MediaItemID)
		}
	}
	presentCounts, perr := a.presentShowWatchCounts(ctx, q, userID, watchedIDs)
	if perr != nil {
		presentCounts = map[int64]presentWatchCounts{}
	}
	watchedSeries = make(map[int64]bool)
	showCounts = make(map[int64][2]int32, len(counts))
	for _, c := range counts {
		total, watched := c.TotalEpisodes, c.WatchedEpisodes
		if pc, ok := presentCounts[c.MediaItemID]; ok && c.WatchedEpisodes > 0 {
			total, watched = int32(pc.Total), int32(pc.Watched)
		}
		showCounts[c.MediaItemID] = [2]int32{watched, total}
		if total > 0 && watched >= total {
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

// JFFavoriteIDs returns the favorited entity-id set for one entity type
// ("episode", "track", "album", ... — "media_item" comes via JFUserVideoSets).
func (a *App) JFFavoriteIDs(ctx context.Context, userID int64, entityType string) (map[int64]bool, error) {
	// Music favorites live in the unified rating store (heart band ≥9), the
	// same signal the web reactions and Subsonic stars write — not in the
	// boolean user_favorites rows video uses.
	switch entityType {
	case "track":
		ids, err := a.HeartedTrackIDs(ctx, userID)
		return idSet(ids), err
	case "album":
		ids, err := a.HeartedAlbumIDs(ctx, userID)
		return idSet(ids), err
	}
	ids, err := sqlc.New(a.db).ListFavoritedIDs(ctx, sqlc.ListFavoritedIDsParams{UserID: userID, EntityType: entityType})
	if err != nil {
		return nil, err
	}
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	if entityType == "media_item" {
		// Jellyfin addresses music artists as media_items; overlay their
		// hearts so MusicArtist DTOs show favorite state from the rating store.
		if artistItems, err := a.HeartedArtistMediaItemIDs(ctx, userID); err == nil {
			for _, id := range artistItems {
				out[id] = true
			}
		}
	}
	return out, nil
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

// JFEpisodeFileID resolves the playable library file for one episode via the
// series' parse-result file map (episodes have no file FK — see
// BuildEpisodeFileMap).
func (a *App) JFEpisodeFileID(ctx context.Context, seriesMediaItemID int64, season, episode int32) (int64, bool, error) {
	files, err := sqlc.New(a.db).ListEpisodeFiles(ctx, pgtype.Int8{Int64: seriesMediaItemID, Valid: true})
	if err != nil {
		return 0, false, err
	}
	entry, ok := BuildEpisodeFileMap(files)[fmt.Sprintf("s%de%d", season, episode)]
	return entry.FileID, ok, nil
}

// PassiveMediaUpstream returns the upstream Heya base URL media bytes can be
// proxied from when this process is in passive mode (borrowed prod DB, no
// local media files) — the same upstream the image proxy uses. Empty when
// not passive or unconfigured; callers then serve local bytes only.
func (a *App) PassiveMediaUpstream() string {
	if a.config == nil || !a.config.PassiveMode.Value {
		return ""
	}
	return strings.TrimRight(a.config.ImageProxyURL.Value, "/")
}

// JFEpisodeFileEntries returns the full s{n}e{n} → file-entry map for one
// series — the batch form of JFEpisodeFileID, for decorating a whole episode
// list (fields=MediaSources) with one query instead of one per episode.
func (a *App) JFEpisodeFileEntries(ctx context.Context, seriesMediaItemID int64) (map[string]EpisodeFileEntry, error) {
	files, err := sqlc.New(a.db).ListEpisodeFiles(ctx, pgtype.Int8{Int64: seriesMediaItemID, Valid: true})
	if err != nil {
		return nil, err
	}
	return BuildEpisodeFileMap(files), nil
}

// JFLibraryFilesByIDs batch-hydrates library files, keyed by id.
func (a *App) JFLibraryFilesByIDs(ctx context.Context, ids []int64) (map[int64]sqlc.LibraryFile, error) {
	if len(ids) == 0 {
		return map[int64]sqlc.LibraryFile{}, nil
	}
	rows, err := sqlc.New(a.db).JFLibraryFilesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]sqlc.LibraryFile, len(rows))
	for _, f := range rows {
		out[f.ID] = f
	}
	return out, nil
}

// JFTrackLibraryFiles resolves track_file ids to their library files, keyed
// by track_file id — the batch behind Audio list MediaSources decoration.
func (a *App) JFTrackLibraryFiles(ctx context.Context, trackFileIDs []int64) (map[int64]sqlc.LibraryFile, error) {
	if len(trackFileIDs) == 0 {
		return map[int64]sqlc.LibraryFile{}, nil
	}
	q := sqlc.New(a.db)
	rows, err := q.JFTrackFilesByIDs(ctx, trackFileIDs)
	if err != nil {
		return nil, err
	}
	libIDs := make([]int64, 0, len(rows))
	for _, r := range rows {
		libIDs = append(libIDs, r.LibraryFileID)
	}
	files, err := a.JFLibraryFilesByIDs(ctx, libIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]sqlc.LibraryFile, len(rows))
	for _, r := range rows {
		if f, ok := files[r.LibraryFileID]; ok {
			out[r.ID] = f
		}
	}
	return out, nil
}

// JFBestVideoFiles returns the primary playable file per movie media item,
// batched — the same matched-first pick JFMovieFileID makes for one item.
func (a *App) JFBestVideoFiles(ctx context.Context, mediaItemIDs []int64) (map[int64]sqlc.LibraryFile, error) {
	if len(mediaItemIDs) == 0 {
		return map[int64]sqlc.LibraryFile{}, nil
	}
	rows, err := sqlc.New(a.db).JFBestVideoFilesForItems(ctx, mediaItemIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]sqlc.LibraryFile, len(rows))
	for _, f := range rows {
		if f.MediaItemID.Valid {
			out[f.MediaItemID.Int64] = f
		}
	}
	return out, nil
}

// JFMovieFileID returns the primary playable file for a movie media item
// (first non-deleted match, mirroring what the FE plays).
func (a *App) JFMovieFileID(ctx context.Context, mediaItemID int64) (sqlc.LibraryFile, bool, error) {
	files, err := sqlc.New(a.db).ListLibraryFilesByMediaItem(ctx, pgtype.Int8{Int64: mediaItemID, Valid: true})
	if err != nil {
		return sqlc.LibraryFile{}, false, err
	}
	for _, f := range files {
		if f.Status == sqlc.FileStatusMatched {
			return f, true, nil
		}
	}
	if len(files) > 0 {
		return files[0], true, nil
	}
	return sqlc.LibraryFile{}, false, nil
}

// JFFileHasSegments reports whether a library file has any stored skip
// markers — backs MediaSourceInfo.HasSegments, which jellyfin-web gates its
// entire /MediaSegments fetch on at playback start. A query error is
// swallowed to false: a missed skip-intro button beats a broken PlaybackInfo
// response.
func (a *App) JFFileHasSegments(ctx context.Context, fileID int64) bool {
	has, err := sqlc.New(a.db).JFFileHasSegments(ctx, fileID)
	if err != nil {
		return false
	}
	return has
}

// JFSimilarLocalItemIDs returns local media_item ids recommended for the
// given item, strongest provider recommendation first (media_recommendations
// rows that matched a library item by external ids).
func (a *App) JFSimilarLocalItemIDs(ctx context.Context, mediaItemID int64, limit int32) ([]int64, error) {
	rows, err := sqlc.New(a.db).ListMediaRecommendationsWithLibrary(ctx, mediaItemID)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, limit)
	for _, r := range rows {
		if r.LocalMediaItemID == 0 {
			continue
		}
		out = append(out, r.LocalMediaItemID)
		if int32(len(out)) >= limit {
			break
		}
	}
	return out, nil
}

// emptyNotNil keeps pgx happy: a nil []int64 binds as NULL, and
// cardinality(NULL) is NULL, which would disable the "0 = filter off"
// convention. Always bind at least an empty array.
// emptyNotNil normalizes a nil slice to an empty (non-nil) one so pgx encodes
// it as an empty SQL array `{}` rather than NULL — the JF list/count queries
// gate on cardinality(arg)=0, and cardinality(NULL) is NULL (not 0), which
// would flip an "empty = no filter" clause into "matches nothing".
func emptyNotNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func idSet(ids []int64) map[int64]bool {
	out := make(map[int64]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}
