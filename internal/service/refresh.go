package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/matcher"
	"github.com/karbowiak/heya/internal/worker"
)

// RefreshMediaItem enqueues a forced enrich for a single media item. Called
// from HTTP handlers (user clicked "refresh metadata") and CLI commands.
// Async — the actual fetch happens on the metadata queue and the UI is
// updated via the WebSocket event hub when the enrich worker completes.
func (a *App) RefreshMediaItem(ctx context.Context, mediaItemID int64) error {
	q := sqlc.New(a.db)
	item, err := q.GetMediaItemByID(ctx, mediaItemID)
	if err != nil {
		return fmt.Errorf("media item %d not found: %w", mediaItemID, err)
	}

	return worker.EnqueueEnrichForce(ctx, a.river, mediaItemID, item.MediaType, worker.EnrichSourceForced)
}

// RefreshMusicArtistNow runs the full artist metadata refresh inline —
// including the discography sync — instead of enqueueing it. The queue is
// the norm; this is the CLI/manager lever for "refresh this artist right
// now" that doesn't depend on a worker process being up.
func (a *App) RefreshMusicArtistNow(ctx context.Context, mediaItemID int64) (matcher.RefreshArtistResult, error) {
	q := sqlc.New(a.db)
	artist, err := q.GetArtistByMediaItemID(ctx, mediaItemID)
	if err != nil {
		return matcher.RefreshArtistResult{}, fmt.Errorf("media item %d has no artist row: %w", mediaItemID, err)
	}
	return a.matcher.RefreshMusicArtist(ctx, artist.ID)
}
