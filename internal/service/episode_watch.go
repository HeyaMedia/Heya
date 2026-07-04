package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
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

// MarkSeasonWatched marks the episodes we actually hold in a season as watched
// (the present-episode set — see presentEpisodeIDs), so an unaired episode
// isn't pre-marked watched before its file ever arrives, which would surface it
// as already-watched the moment it downloads.
func (a *App) MarkSeasonWatched(ctx context.Context, userID, seasonID int64) error {
	q := sqlc.New(a.db)
	season, err := q.GetTVSeasonByID(ctx, seasonID)
	if err != nil {
		return err
	}
	series, err := q.GetTVSeriesByID(ctx, season.SeriesID)
	if err != nil {
		return err
	}
	ids, err := a.presentEpisodeIDs(ctx, q, series, seasonID)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return q.MarkEpisodesWatched(ctx, sqlc.MarkEpisodesWatchedParams{UserID: userID, Column2: ids})
}

// presentEpisodeIDs returns the IDs of episodes a bulk watch action should
// touch — the episodes the user can actually see in the listing. When
// seasonID != 0 the result is limited to that season.
//
// This MUST match the read side's definition of "visible" so mark/unmark stay
// in lockstep with what's displayed. Two gates, mirroring the read path:
//
//  1. Season visibility (BuildAvailableSeasonSet): GetMediaDetail hides any
//     season we hold no files for (when the set is non-empty). Those seasons
//     are skipped entirely here — a fully-unaired future season must never be
//     bulk-marked, whether swept by mark-show or named directly by
//     mark-season (e.g. via a Jellyfin client).
//  2. Episode presence within a visible season: keep episodes whose
//     s{season}e{episode} key is in the episode_files map
//     (BuildEpisodeFileMap), falling back to the whole season when none
//     resolve (a season pack parsed without per-episode numbers) — exactly
//     the FE `presentEpisodes` fallback, which shows every episode of such a
//     season. A normal airing season (some files resolve) still marks only
//     the episodes we hold, never the unaired catalog tail.
func (a *App) presentEpisodeIDs(ctx context.Context, q *sqlc.Queries, series sqlc.TvSeries, seasonID int64) ([]int64, error) {
	files, err := q.ListEpisodeFiles(ctx, pgtype.Int8{Int64: series.MediaItemID, Valid: true})
	if err != nil {
		return nil, err
	}
	efMap := BuildEpisodeFileMap(files)
	availableSeasons := BuildAvailableSeasonSet(files)

	seasons, err := q.ListTVSeasonsBySeries(ctx, series.ID)
	if err != nil {
		return nil, err
	}
	seasonNumByID := make(map[int64]int32, len(seasons))
	for _, s := range seasons {
		seasonNumByID[s.ID] = s.SeasonNumber
	}

	eps, err := q.ListTVEpisodesBySeries(ctx, series.ID)
	if err != nil {
		return nil, err
	}

	// Group episodes per season so both gates apply per season, matching the
	// read path.
	bySeason := make(map[int64][]sqlc.TvEpisode)
	for _, ep := range eps {
		if seasonID != 0 && ep.SeasonID != seasonID {
			continue
		}
		bySeason[ep.SeasonID] = append(bySeason[ep.SeasonID], ep)
	}

	var ids []int64
	for sID, seasonEps := range bySeason {
		sn := seasonNumByID[sID]
		// Gate 1: hidden season (no files at all) → untouchable.
		if len(availableSeasons) > 0 && !availableSeasons[int(sn)] {
			continue
		}
		// Gate 2: present episodes, falling back to the whole (visible)
		// season when no per-episode keys resolve.
		var present []int64
		for _, ep := range seasonEps {
			if _, ok := efMap[fmt.Sprintf("s%de%d", sn, ep.EpisodeNumber)]; ok {
				present = append(present, ep.ID)
			}
		}
		if len(present) == 0 {
			for _, ep := range seasonEps {
				ids = append(ids, ep.ID)
			}
			continue
		}
		ids = append(ids, present...)
	}
	return ids, nil
}

// UnmarkSeasonWatched removes watched marks from all episodes in a season.
func (a *App) UnmarkSeasonWatched(ctx context.Context, userID, seasonID int64) error {
	q := sqlc.New(a.db)
	return q.UnmarkSeasonWatched(ctx, sqlc.UnmarkSeasonWatchedParams{
		UserID:   userID,
		SeasonID: seasonID,
	})
}

// MarkShowWatched marks the episodes we actually hold across a show as watched.
// As with MarkSeasonWatched, unaired catalog episodes are left untouched.
func (a *App) MarkShowWatched(ctx context.Context, userID, mediaItemID int64) error {
	q := sqlc.New(a.db)
	series, err := q.GetTVSeriesByMediaItemID(ctx, mediaItemID)
	if err != nil {
		return err
	}
	ids, err := a.presentEpisodeIDs(ctx, q, series, 0)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return q.MarkEpisodesWatched(ctx, sqlc.MarkEpisodesWatchedParams{UserID: userID, Column2: ids})
}

// UnmarkShowWatched removes watched marks from all episodes in a show.
func (a *App) UnmarkShowWatched(ctx context.Context, userID, mediaItemID int64) error {
	q := sqlc.New(a.db)
	return q.UnmarkShowWatched(ctx, sqlc.UnmarkShowWatchedParams{
		UserID:      userID,
		MediaItemID: mediaItemID,
	})
}

// MarkMediaWatched routes to the per-type marker by inspecting the media
// item's type. Unifies the FE call-path: a single POST /api/me/watched/media/{id}
// works for both TV shows (which need all episodes flagged) and movies
// (single-row marker), replacing the older split endpoints.
func (a *App) MarkMediaWatched(ctx context.Context, userID, mediaItemID int64, watched bool) error {
	q := sqlc.New(a.db)
	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return err
	}
	switch item.MediaType {
	case sqlc.MediaTypeTv:
		if watched {
			return a.MarkShowWatched(ctx, userID, mediaItemID)
		}
		return a.UnmarkShowWatched(ctx, userID, mediaItemID)
	default:
		if watched {
			return a.MarkMovieWatched(ctx, userID, mediaItemID)
		}
		return a.UnmarkMovieWatched(ctx, userID, mediaItemID)
	}
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

	// Overlay the localized episode title using the series library's
	// PreferredLanguage. Without this an anime up-next stays in Japanese
	// even when the library is set to English.
	title := ep.Title
	if item, err := q.GetMediaItemByID(ctx, mediaItemID); err == nil {
		if lib, err := q.GetLibraryByID(ctx, item.LibraryID); err == nil {
			lang := metadata.ParseSettings(lib.Settings).PreferredLanguage
			if lang != "" {
				if t, err := q.GetEpisodeTitleByLanguage(ctx, sqlc.GetEpisodeTitleByLanguageParams{EpisodeID: ep.EpisodeID, Language: lang}); err == nil && t.Title != "" {
					title = t.Title
				} else if lang != "en" {
					if t, err := q.GetEpisodeTitleByLanguage(ctx, sqlc.GetEpisodeTitleByLanguageParams{EpisodeID: ep.EpisodeID, Language: "en"}); err == nil && t.Title != "" {
						title = t.Title
					}
				}
			}
		}
	}

	return UpNextResult{
		HasNext:       true,
		EpisodeID:     ep.EpisodeID,
		EpisodeNumber: ep.EpisodeNumber,
		EpisodeTitle:  title,
		SeasonNumber:  ep.SeasonNumber,
		SeasonID:      ep.SeasonID,
		MediaItemID:   ep.MediaItemID,
		Runtime:       ep.RuntimeMinutes,
		FileID:        fileID,
	}, nil
}
