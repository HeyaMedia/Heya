package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
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
