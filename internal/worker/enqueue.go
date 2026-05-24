package worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/riverqueue/river"
)

// EnrichSource describes where an enrich request originated. It maps to a
// River priority band so jobs queued for visible foreground actions (a
// watcher-discovered file, a user opening a detail page) preempt bulk
// background work.
type EnrichSource string

const (
	// EnrichSourceWatcher — fsnotify just dropped a new file. The user is
	// likely about to look for it, so the enrich runs at priority 1
	// regardless of media type.
	EnrichSourceWatcher EnrichSource = "watcher"

	// EnrichSourceView — a user opened a detail page that's still on a
	// stub match. Promote that one item to priority 1 ahead of the bulk
	// queue. Handled by cancel + re-enqueue since River can't bump an
	// existing job's priority in place.
	EnrichSourceView EnrichSource = "view"

	// EnrichSourceScan — match was just made during a library scan; the
	// enrich runs at the default priority for the media type.
	EnrichSourceScan EnrichSource = "scan"

	// EnrichSourceScheduled — periodic refresh_stale_items task picked
	// this up because its metadata_refreshed_at crossed the library's
	// staleness window.
	EnrichSourceScheduled EnrichSource = "scheduled"

	// EnrichSourceForced — user clicked "refresh metadata" in the UI for
	// this item or its whole library.
	EnrichSourceForced EnrichSource = "forced"
)

// PriorityFor maps a (source, media_type) combination to a River priority
// band. River caps priorities at 1..4 (1 = highest), so we collapse media
// types into two groups: movies+tv at priority 2, music+books at 3. There
// aren't enough books in practice to justify the deferred-ScheduledAt hack
// needed to push them past music inside a single River queue.
//
// Watcher and view sources always preempt to priority 1 — those are the
// two cases where a real user is staring at the screen waiting.
func PriorityFor(source EnrichSource, mediaType sqlc.MediaType) int {
	switch source {
	case EnrichSourceWatcher, EnrichSourceView:
		return 1
	}
	switch mediaType {
	case sqlc.MediaTypeMovie, sqlc.MediaTypeTv:
		return 2
	default:
		return 3
	}
}

// EnqueueEnrich is the single entry point for queuing enrich work. All
// callers (match worker, scheduled refresh tasks, force-refresh workers,
// view-promotion) go through here so priority + idempotency stay consistent.
//
// The job runs on the `metadata` River queue (MaxWorkers=1) and dispatches
// internally by media_type. The worker looks up everything it needs from
// media_items + library settings — callers only have to know the item ID,
// media type (for priority), and the source.
func EnqueueEnrich(ctx context.Context, rc *river.Client[pgx.Tx], itemID int64, mediaType sqlc.MediaType, source EnrichSource) error {
	return enqueueEnrich(ctx, rc, itemID, mediaType, source, false, 0, 0, 0)
}

// EnqueueEnrichForce is the force-refresh variant. Sets Force=true on the
// job so the worker bypasses its "already complete" idempotency gate.
func EnqueueEnrichForce(ctx context.Context, rc *river.Client[pgx.Tx], itemID int64, mediaType sqlc.MediaType, source EnrichSource) error {
	return enqueueEnrich(ctx, rc, itemID, mediaType, source, true, 0, 0, 0)
}

// EnqueueEnrichBatch is the music post-scan fan-out variant. The extra
// batch context lets the worker emit "Refreshing 17/200 (Calvin Harris)"
// progress events without consulting River's job table.
func EnqueueEnrichBatch(ctx context.Context, rc *river.Client[pgx.Tx], itemID int64, mediaType sqlc.MediaType, source EnrichSource, batchLibraryID int64, batchTotal, batchPosition int) error {
	return enqueueEnrich(ctx, rc, itemID, mediaType, source, false, batchLibraryID, batchTotal, batchPosition)
}

// EnqueueEnrichTx variant for callers inside a river worker that already
// have a pgx.Tx context — pulls the River client out of ctx rather than
// requiring it as an arg.
func EnqueueEnrichTx(ctx context.Context, itemID int64, mediaType sqlc.MediaType, source EnrichSource) error {
	rc := river.ClientFromContext[pgx.Tx](ctx)
	if rc == nil {
		return fmt.Errorf("EnqueueEnrichTx: no river client in context")
	}
	return enqueueEnrich(ctx, rc, itemID, mediaType, source, false, 0, 0, 0)
}

func enqueueEnrich(ctx context.Context, rc *river.Client[pgx.Tx], itemID int64, mediaType sqlc.MediaType, source EnrichSource, force bool, batchLibraryID int64, batchTotal, batchPosition int) error {
	priority := PriorityFor(source, mediaType)
	_, err := rc.Insert(ctx, EnrichMediaItemArgs{
		ItemID:         itemID,
		Source:         string(source),
		Force:          force,
		BatchLibraryID: batchLibraryID,
		BatchTotal:     batchTotal,
		BatchPosition:  batchPosition,
	}, &river.InsertOpts{Priority: priority})
	return err
}
