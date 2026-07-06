package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/metadata"
)

// emitWatched broadcasts a media.watched event for a watch-state change made
// through one of the mark/unmark entry points below. Mirrors the hub access +
// nil-guard in UpdateWatchProgress (watch.go) — Progress/Total are left zero
// since these entry points are toggles, not the playback-progress path.
func (a *App) emitWatched(userID, mediaItemID int64, completed bool) {
	if a.hub == nil {
		return
	}
	a.hub.Emit(eventhub.EventMediaWatched, eventhub.WatchPayload{
		UserID:      userID,
		MediaItemID: mediaItemID,
		Completed:   completed,
	})
}

// MarkEpisodeWatched marks a single episode as watched for a user.
func (a *App) MarkEpisodeWatched(ctx context.Context, userID, episodeID int64) error {
	q := sqlc.New(a.db)
	if err := q.MarkEpisodeWatched(ctx, sqlc.MarkEpisodeWatchedParams{
		UserID:   userID,
		EntityID: episodeID,
	}); err != nil {
		return err
	}
	a.emitWatched(userID, episodeID, true)
	return nil
}

// UnmarkEpisodeWatched removes the watched mark from a single episode.
func (a *App) UnmarkEpisodeWatched(ctx context.Context, userID, episodeID int64) error {
	q := sqlc.New(a.db)
	if err := q.UnmarkEpisodeWatched(ctx, sqlc.UnmarkEpisodeWatchedParams{
		UserID:   userID,
		EntityID: episodeID,
	}); err != nil {
		return err
	}
	a.emitWatched(userID, episodeID, false)
	return nil
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
	if err := q.MarkEpisodesWatched(ctx, sqlc.MarkEpisodesWatchedParams{UserID: userID, Column2: ids}); err != nil {
		return err
	}
	a.emitWatched(userID, series.MediaItemID, true)
	return nil
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
	sets, err := a.seasonPresentEpisodeSets(ctx, q, series)
	if err != nil {
		return nil, err
	}
	if seasonID != 0 {
		return sets[seasonID], nil
	}
	var ids []int64
	for _, set := range sets {
		ids = append(ids, set...)
	}
	return ids, nil
}

// seasonPresentEpisodeSets returns, per season id, the episode IDs that are
// visible/markable in that season — the per-season decomposition behind
// presentEpisodeIDs, also used to measure per-season watched state so
// numerator and denominator share one definition. Hidden seasons (gate 1)
// have no entry.
func (a *App) seasonPresentEpisodeSets(ctx context.Context, q *sqlc.Queries, series sqlc.TvSeries) (map[int64][]int64, error) {
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
		bySeason[ep.SeasonID] = append(bySeason[ep.SeasonID], ep)
	}

	sets := make(map[int64][]int64, len(bySeason))
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
				present = append(present, ep.ID)
			}
		}
		sets[sID] = present
	}
	return sets, nil
}

// UnmarkSeasonWatched removes watched marks from all episodes in a season.
func (a *App) UnmarkSeasonWatched(ctx context.Context, userID, seasonID int64) error {
	q := sqlc.New(a.db)
	if err := q.UnmarkSeasonWatched(ctx, sqlc.UnmarkSeasonWatchedParams{
		UserID:   userID,
		SeasonID: seasonID,
	}); err != nil {
		return err
	}
	if season, err := q.GetTVSeasonByID(ctx, seasonID); err == nil {
		if series, err := q.GetTVSeriesByID(ctx, season.SeriesID); err == nil {
			a.emitWatched(userID, series.MediaItemID, false)
		}
	}
	return nil
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
	if err := q.MarkEpisodesWatched(ctx, sqlc.MarkEpisodesWatchedParams{UserID: userID, Column2: ids}); err != nil {
		return err
	}
	a.emitWatched(userID, mediaItemID, true)
	return nil
}

// UnmarkShowWatched removes watched marks from all episodes in a show.
func (a *App) UnmarkShowWatched(ctx context.Context, userID, mediaItemID int64) error {
	q := sqlc.New(a.db)
	if err := q.UnmarkShowWatched(ctx, sqlc.UnmarkShowWatchedParams{
		UserID:      userID,
		MediaItemID: mediaItemID,
	}); err != nil {
		return err
	}
	a.emitWatched(userID, mediaItemID, false)
	return nil
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
	if err := q.MarkMovieWatched(ctx, sqlc.MarkMovieWatchedParams{
		UserID:   userID,
		EntityID: mediaItemID,
	}); err != nil {
		return err
	}
	a.emitWatched(userID, mediaItemID, true)
	return nil
}

// UnmarkMovieWatched removes the watched mark from a movie.
func (a *App) UnmarkMovieWatched(ctx context.Context, userID, mediaItemID int64) error {
	q := sqlc.New(a.db)
	if err := q.UnmarkMovieWatched(ctx, sqlc.UnmarkMovieWatchedParams{
		UserID:   userID,
		EntityID: mediaItemID,
	}); err != nil {
		return err
	}
	a.emitWatched(userID, mediaItemID, false)
	return nil
}

// presentWatchCounts is a show's watched state measured against the episodes
// we actually hold: Total is the count analogue of presentEpisodeIDs (same
// two gates), and Watched counts only the user's completed episodes that fall
// inside that same present set — a stale watched row on an episode we don't
// hold (old catalog-wide bulk marks, deleted files) must neither complete a
// show nor inflate its progress.
type presentWatchCounts struct {
	Total   int
	Watched int
}

// presentShowWatchCounts returns per-item present-aware (total, watched)
// pairs, batched across items with three slim queries so the browse-state
// rollups can afford it. Bulk-mark only writes present episodes, so both
// sides of the fraction must be measured against the same present set —
// numerator and denominator gate identically. Items with no episodes simply
// have no entry; callers keep their raw-catalog fallback.
func (a *App) presentShowWatchCounts(ctx context.Context, q *sqlc.Queries, userID int64, itemIDs []int64) (map[int64]presentWatchCounts, error) {
	if len(itemIDs) == 0 {
		return map[int64]presentWatchCounts{}, nil
	}

	parses, err := q.ListEpisodeFileParses(ctx, itemIDs)
	if err != nil {
		return nil, err
	}
	type seKey struct{ s, e int }
	presentKeys := map[int64]map[seKey]bool{}
	availSeasons := map[int64]map[int]bool{}
	for _, p := range parses {
		id := p.MediaItemID.Int64
		seasons := intsFromJSON(p.Seasons)
		episodes := intsFromJSON(p.Episodes)
		for _, s := range seasons {
			if availSeasons[id] == nil {
				availSeasons[id] = map[int]bool{}
			}
			availSeasons[id][s] = true
			for _, e := range episodes {
				if presentKeys[id] == nil {
					presentKeys[id] = map[seKey]bool{}
				}
				presentKeys[id][seKey{s, e}] = true
			}
		}
	}

	eps, err := q.ListEpisodeNumbersForMediaItems(ctx, itemIDs)
	if err != nil {
		return nil, err
	}
	type itemSeason struct {
		item int64
		sn   int
	}
	catalog := map[itemSeason]int{}
	present := map[itemSeason]int{}
	for _, ep := range eps {
		k := itemSeason{ep.MediaItemID, int(ep.SeasonNumber)}
		catalog[k]++
		if presentKeys[ep.MediaItemID][seKey{int(ep.SeasonNumber), int(ep.EpisodeNumber)}] {
			present[k]++
		}
	}

	// Per-season mode, shared by both sides of the fraction:
	//   hidden   — season has no files at all → contributes nothing
	//   keyed    — some episodes resolve to file keys → count only those
	//   fallback — files exist but none carry per-episode numbers (season
	//              packs) → the whole season counts, matching the listing
	const (
		modeHidden = iota
		modeKeyed
		modeFallback
	)
	seasonMode := func(k itemSeason) int {
		avail := availSeasons[k.item]
		if len(avail) > 0 && !avail[k.sn] {
			return modeHidden
		}
		if present[k] > 0 {
			return modeKeyed
		}
		return modeFallback
	}

	counts := map[int64]presentWatchCounts{}
	for k, total := range catalog {
		c := counts[k.item]
		switch seasonMode(k) {
		case modeKeyed:
			c.Total += present[k]
		case modeFallback:
			c.Total += total
		}
		counts[k.item] = c
	}

	watchedRows, err := q.ListWatchedEpisodeNumbersForMediaItems(ctx, sqlc.ListWatchedEpisodeNumbersForMediaItemsParams{
		UserID:       userID,
		MediaItemIds: itemIDs,
	})
	if err != nil {
		return nil, err
	}
	for _, w := range watchedRows {
		k := itemSeason{w.MediaItemID, int(w.SeasonNumber)}
		switch seasonMode(k) {
		case modeKeyed:
			if !presentKeys[w.MediaItemID][seKey{int(w.SeasonNumber), int(w.EpisodeNumber)}] {
				continue
			}
		case modeHidden:
			continue
		}
		c := counts[k.item]
		c.Watched++
		counts[k.item] = c
	}
	return counts, nil
}

// intsFromJSON coerces a jsonb array scanned into interface{} (pgx decodes to
// []any of float64) into ints. Nil / non-array / non-numeric input yields nil.
func intsFromJSON(v any) []int {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]int, 0, len(arr))
	for _, x := range arr {
		if f, ok := x.(float64); ok {
			out = append(out, int(f))
		}
	}
	return out
}

// UserMediaState holds the fully-watched show IDs and favorited media item IDs.
type UserMediaState struct {
	WatchedIDs   []int64 `json:"watched"`
	FavoritedIDs []int64 `json:"favorited"`
}

// GetUserMediaState returns all fully-watched show IDs and favorited media
// item IDs for a user. "Fully watched" is measured against the episodes we
// actually hold (presentEpisodeTotals), not the provider catalog — bulk-mark
// only writes present episodes, so an airing show with every held episode
// watched counts as done.
func (a *App) GetUserMediaState(ctx context.Context, userID int64) (UserMediaState, error) {
	q := sqlc.New(a.db)
	watchedIDs := a.fullyWatchedShowIDs(ctx, q, userID)
	favIDs, _ := q.ListFavoritedMediaItemIDs(ctx, userID)
	return UserMediaState{WatchedIDs: watchedIDs, FavoritedIDs: favIDs}, nil
}

// fullyWatchedShowIDs lists shows where every present episode is watched —
// both sides present-gated via presentShowWatchCounts, so stale marks on
// non-held episodes can't complete a show. Falls back to raw catalog counts
// when the helper has no entry for an item (no episodes at all).
func (a *App) fullyWatchedShowIDs(ctx context.Context, q *sqlc.Queries, userID int64) []int64 {
	counts, err := q.ListShowWatchCounts(ctx, userID)
	if err != nil {
		return []int64{}
	}
	var candidateIDs []int64
	for _, c := range counts {
		if c.WatchedEpisodes > 0 {
			candidateIDs = append(candidateIDs, c.MediaItemID)
		}
	}
	presentCounts, err := a.presentShowWatchCounts(ctx, q, userID, candidateIDs)
	if err != nil {
		presentCounts = map[int64]presentWatchCounts{}
	}
	watched := []int64{}
	for _, c := range counts {
		if c.WatchedEpisodes == 0 {
			continue
		}
		total, done := int(c.TotalEpisodes), int(c.WatchedEpisodes)
		if pc, ok := presentCounts[c.MediaItemID]; ok {
			total, done = pc.Total, pc.Watched
		}
		if total > 0 && done >= total {
			watched = append(watched, c.MediaItemID)
		}
	}
	return watched
}

// SeasonWatchInfo contains per-season watched episode counts and IDs.
type SeasonWatchInfo struct {
	SeasonID   int64   `json:"season_id"`
	Watched    int32   `json:"watched"`
	Total      int     `json:"total"`
	EpisodeIDs []int64 `json:"episode_ids"`
}

// GetWatchedEpisodes returns per-season watched episode info for a series,
// measured against the episodes we hold (seasonPresentEpisodeSets) so it
// agrees with the listing and the bulk-mark scope.
func (a *App) GetWatchedEpisodes(ctx context.Context, userID, mediaItemID int64) ([]SeasonWatchInfo, error) {
	q := sqlc.New(a.db)

	series, err := q.GetTVSeriesByMediaItemID(ctx, mediaItemID)
	if err != nil {
		return nil, fmt.Errorf("series not found: %w", err)
	}

	sets, err := a.seasonPresentEpisodeSets(ctx, q, series)
	if err != nil {
		return nil, err
	}
	watchedEpIDs, _ := q.ListWatchedEpisodeIDsForSeries(ctx, sqlc.ListWatchedEpisodeIDsForSeriesParams{
		UserID:   userID,
		SeriesID: series.ID,
	})
	watchedSet := make(map[int64]bool, len(watchedEpIDs))
	for _, id := range watchedEpIDs {
		watchedSet[id] = true
	}

	var result []SeasonWatchInfo
	for seasonID, set := range sets {
		watchedIDs := []int64{}
		for _, epID := range set {
			if watchedSet[epID] {
				watchedIDs = append(watchedIDs, epID)
			}
		}
		result = append(result, SeasonWatchInfo{
			SeasonID:   seasonID,
			Watched:    int32(len(watchedIDs)),
			Total:      len(set),
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
