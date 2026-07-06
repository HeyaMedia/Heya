package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
)

// ContinueWatchingEnrichedRow extends the base SQL row with a resolved
// `file_id` so the FE can navigate straight into the player without a
// second round-trip to look up the playable file. file_id can be 0 when
// the resolution failed (file was deleted, parse_result missing, etc.) —
// the FE should hide the tile rather than crash in that case.
type ContinueWatchingEnrichedRow struct {
	sqlc.ListContinueWatchingRow
	FileID int64 `json:"file_id"`
}

// PlaybackEvent is the unified emission shape for the player engines. Whether
// the entity is a movie / episode / track determines which storage layer the
// event lands in — see RecordPlayback for the dispatch.
type PlaybackEvent struct {
	EntityType      string `json:"entity_type" enum:"movie,episode,track" doc:"What's being played"`
	EntityID        int64  `json:"entity_id"                                 doc:"Movie media_item id, episode id, or track id"`
	PositionSeconds int32  `json:"position_seconds" minimum:"0"              doc:"How far into the item the player is"`
	TotalSeconds    int32  `json:"total_seconds"    minimum:"0"              doc:"Total length (0 if unknown)"`
	Completed       bool   `json:"completed"                                 doc:"Whether playback reached the end / scrobble threshold"`
	Source          string `json:"source,omitempty" maxLength:"40"           doc:"Origin label: queue | radio | album | playlist | search | browse | similar"`
}

// RecordPlayback dispatches one playback emission to the right backing store:
//
//   - movie / episode → upsert into user_watch_progress (resume state)
//   - track            → append to play_events           (history log)
//
// The dispatch lives here so the HTTP handler stays a thin pass-through and
// the FE has exactly one endpoint + composable to call regardless of media.
func (a *App) RecordPlayback(ctx context.Context, userID int64, ev PlaybackEvent) error {
	switch ev.EntityType {
	case "movie", "episode", "":
		_, err := a.UpdateWatchProgress(ctx, userID, ev.EntityType, ev.EntityID, ev.PositionSeconds, ev.TotalSeconds)
		return err
	case "track":
		_, err := a.RecordPlayEvent(ctx, userID, RecordPlayEventInput{
			TrackID:         ev.EntityID,
			ListenedSeconds: ev.PositionSeconds,
			Completed:       ev.Completed,
			Source:          ev.Source,
		})
		return err
	default:
		return fmt.Errorf("unsupported entity_type %q (want movie | episode | track)", ev.EntityType)
	}
}

func (a *App) UpdateWatchProgress(ctx context.Context, userID int64, entityType string, entityID int64, progress, total int32) (sqlc.UserWatchProgress, error) {
	if entityType == "" {
		entityType = "movie"
	}

	// Treat "watched" as 90% through — matches the Plex/Jellyfin convention.
	// A fixed 30s-from-end tail left anything with a minute of credits still
	// sitting in Continue Watching. Integer math (progress ≥ 90% of total)
	// avoids float rounding; total is seconds so total*9 can't overflow int32.
	completed := total > 0 && progress >= total*9/10

	if a.hub != nil {
		a.hub.Emit(eventhub.EventMediaWatched, eventhub.WatchPayload{
			UserID:      userID,
			MediaItemID: entityID,
			Progress:    progress,
			Total:       total,
			Completed:   completed,
		})
	}

	q := sqlc.New(a.db)
	return q.UpsertWatchProgress(ctx, sqlc.UpsertWatchProgressParams{
		UserID:          userID,
		EntityType:      entityType,
		EntityID:        entityID,
		ProgressSeconds: progress,
		TotalSeconds:    total,
		Completed:       completed,
	})
}

func (a *App) ListContinueWatching(ctx context.Context, userID int64) ([]ContinueWatchingEnrichedRow, error) {
	q := sqlc.New(a.db)
	rows, err := q.ListContinueWatching(ctx, userID)
	if err != nil {
		return nil, err
	}
	resolveTitle := a.preferredTitleResolver(ctx, q)

	// Per-series episode-file maps are cached during the request — TV
	// users typically have several CW rows for the same series and we
	// don't want to rebuild the map per row.
	episodeFileMaps := make(map[int64]map[string]EpisodeFileEntry)

	out := make([]ContinueWatchingEnrichedRow, 0, len(rows))
	for _, r := range rows {
		r.Title = resolveTitle(r.MediaItemID, r.LibraryID, r.Title)

		fileID := int64(0)
		switch r.EntityType {
		case "movie":
			// Primary file = first non-deleted library_file for the media item.
			if files, err := q.ListLibraryFilesByMediaItem(ctx, pgtype.Int8{Int64: r.MediaItemID, Valid: true}); err == nil && len(files) > 0 {
				fileID = files[0].ID
			}
		case "episode":
			efMap, ok := episodeFileMaps[r.MediaItemID]
			if !ok {
				if files, err := q.ListEpisodeFiles(ctx, pgtype.Int8{Int64: r.MediaItemID, Valid: true}); err == nil {
					efMap = BuildEpisodeFileMap(files)
				}
				episodeFileMaps[r.MediaItemID] = efMap
			}
			if r.SeasonNumber.Valid && r.EpisodeNumber.Valid {
				key := fmt.Sprintf("s%de%d", r.SeasonNumber.Int32, r.EpisodeNumber.Int32)
				if entry, ok := efMap[key]; ok {
					fileID = entry.FileID
				}
			}
		}
		out = append(out, ContinueWatchingEnrichedRow{ListContinueWatchingRow: r, FileID: fileID})
	}
	return out, nil
}

func (a *App) ListRecentlyWatched(ctx context.Context, userID int64) ([]sqlc.ListRecentlyWatchedRow, error) {
	q := sqlc.New(a.db)
	rows, err := q.ListRecentlyWatched(ctx, userID)
	if err != nil {
		return rows, err
	}
	resolveTitle := a.preferredTitleResolver(ctx, q)
	for i := range rows {
		rows[i].Title = resolveTitle(rows[i].MediaItemID, rows[i].LibraryID, rows[i].Title)
	}
	return rows, nil
}

// ListRecentlyWatchedEpisodes is the episode-level counterpart of
// ListRecentlyWatched for the TV "Recently Watched" rail: one row per watched
// episode (not deduped to the show), each carrying its series' media item so the
// FE paints the show poster with an "S02E03 · Title" subtitle. Series titles are
// localized like the deduped variant.
func (a *App) ListRecentlyWatchedEpisodes(ctx context.Context, userID int64) ([]sqlc.ListRecentlyWatchedEpisodesRow, error) {
	q := sqlc.New(a.db)
	rows, err := q.ListRecentlyWatchedEpisodes(ctx, userID)
	if err != nil {
		return rows, err
	}
	resolveTitle := a.preferredTitleResolver(ctx, q)
	for i := range rows {
		rows[i].SeriesTitle = resolveTitle(rows[i].MediaItemID, rows[i].LibraryID, rows[i].SeriesTitle)
	}
	return rows, nil
}

// ToggleFavorite flips the favorite flag for (entityType, entityID) and returns
// the resulting state. entityType is honored (episode/season/track/artist/album
// share this table keyed by their own id space — the old hardcoded "media_item"
// collided those ids with media_items). A re-click now removes the favorite
// instead of 500-ing; a racing double-click is benign.
func (a *App) ToggleFavorite(ctx context.Context, userID int64, entityType string, entityID int64) (bool, error) {
	if entityType == "" {
		entityType = "media_item"
	}
	q := sqlc.New(a.db)
	favorited, err := q.IsFavorited(ctx, sqlc.IsFavoritedParams{UserID: userID, EntityType: entityType, EntityID: entityID})
	if err != nil {
		return false, err
	}
	if favorited {
		if err := q.RemoveFavorite(ctx, sqlc.RemoveFavoriteParams{UserID: userID, EntityType: entityType, EntityID: entityID}); err != nil {
			return false, err
		}
		if a.hub != nil {
			a.hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{MediaItemID: entityID})
		}
		return false, nil
	}
	// ErrNoRows = INSERT hit ON CONFLICT DO NOTHING (already favorited via a
	// race) — benign; the end state is still favorited.
	if _, err := q.ToggleFavorite(ctx, sqlc.ToggleFavoriteParams{UserID: userID, EntityType: entityType, EntityID: entityID}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	if a.hub != nil {
		a.hub.Emit(eventhub.EventMediaUpdated, eventhub.MediaPayload{MediaItemID: entityID})
	}
	return true, nil
}

func (a *App) IsFavorited(ctx context.Context, userID int64, entityType string, entityID int64) (bool, error) {
	if entityType == "" {
		entityType = "media_item"
	}
	q := sqlc.New(a.db)
	return q.IsFavorited(ctx, sqlc.IsFavoritedParams{UserID: userID, EntityType: entityType, EntityID: entityID})
}
