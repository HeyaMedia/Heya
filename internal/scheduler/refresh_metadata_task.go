package scheduler

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// RefreshStaleItemsTask walks every media_item past its library's
// MetadataRefreshDays staleness window and enqueues a unified enrich job.
// Replaces the old refresh_metadata (non-music) and refresh_music_artists
// (music) tasks — the unified EnrichMediaItemWorker dispatches internally
// by media_type, so one scheduler task and one queue covers all four
// media types now.
type RefreshStaleItemsTask struct {
	DB    *pgxpool.Pool
	River *river.Client[pgx.Tx]
}

func (t *RefreshStaleItemsTask) ID() TaskID { return TaskRefreshStaleItems }

type staleItem struct {
	MediaItemID int64
	MediaType   string
	Title       string
}

func (t *RefreshStaleItemsTask) findStaleItems(ctx context.Context) ([]staleItem, error) {
	// Two staleness signals coexist here:
	//   1. metadata_refreshed_at — flipped by MarkEnrichComplete on every
	//      successful enrich. Covers items that have at least been enriched
	//      once and are now past the library's refresh window.
	//   2. enrichment_status — pending / partial / complete / failed. A
	//      stub-matched item has metadata_refreshed_at = NULL (never
	//      enriched) so the ASC NULLS FIRST sort surfaces it first.
	//
	// For music, the worker uses the artist-search path which doesn't
	// require external_ids on media_items — so we don't gate on it here.
	// For movies/TV/books the worker fails fast via markFailed if no
	// provider id is resolvable from external_ids.
	rows, err := t.DB.Query(ctx, `
		SELECT mi.id, mi.media_type, mi.title, l.settings, mi.metadata_refreshed_at
		FROM media_items mi
		JOIN libraries l ON l.id = mi.library_id
		WHERE mi.media_type = 'music' OR mi.external_ids != '{}'
		ORDER BY mi.metadata_refreshed_at ASC NULLS FIRST
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	var items []staleItem
	for rows.Next() {
		var item staleItem
		var settingsJSON []byte
		var refreshedAt *time.Time
		if err := rows.Scan(&item.MediaItemID, &item.MediaType, &item.Title, &settingsJSON, &refreshedAt); err != nil {
			continue
		}

		settings := metadata.ParseSettings(settingsJSON)
		if settings.MetadataRefreshDays <= 0 {
			// Library opted out of periodic refresh.
			continue
		}

		// Never enriched OR past the refresh window.
		if refreshedAt == nil {
			items = append(items, item)
			continue
		}
		cutoff := now.AddDate(0, 0, -settings.MetadataRefreshDays)
		if refreshedAt.Before(cutoff) {
			items = append(items, item)
		}
	}
	return items, rows.Err()
}

func (t *RefreshStaleItemsTask) CountPending(ctx context.Context) (int, error) {
	items, err := t.findStaleItems(ctx)
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

func (t *RefreshStaleItemsTask) Run(ctx context.Context, progress *ProgressTracker) error {
	items, err := t.findStaleItems(ctx)
	if err != nil {
		return err
	}

	progress.SetTotal(len(items))

	for _, item := range items {
		if ctx.Err() != nil {
			return nil
		}

		if err := worker.EnqueueEnrich(ctx, t.River, item.MediaItemID, sqlc.MediaType(item.MediaType), worker.EnrichSourceScheduled); err != nil {
			log.Warn().Err(err).Int64("item_id", item.MediaItemID).Msg("refresh_stale_items: enqueue enrich failed")
			progress.Fail(item.Title)
			continue
		}

		progress.Advance(item.Title)
	}

	if progress.Snapshot().Completed > 0 {
		log.Info().Int("enqueued", progress.Snapshot().Completed).Msg("refresh_stale_items: enqueued")
	}

	return nil
}
