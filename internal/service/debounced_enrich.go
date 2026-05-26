package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// DebouncedEnrichWindow is the trailing-edge debounce delay applied to
// child-content additions (new albums/tracks under a music artist, new
// seasons/episodes under a TV series). Every additional change inside
// the window slides the wall-clock target forward; the sweeper only
// fires once the library has been quiet for the full window.
//
// 30s for music: covers a downloader dropping a 200-track box-set into
// the watch folder without firing 200 heya.media artist refetches. TV
// uses the same window today — release nights typically drop a season
// over a few minutes, so the same scale fits.
const DebouncedEnrichWindow = 30 * time.Second

// DebouncedEnrichRequester is the implementation that lets callers (the
// matcher in practice) push the debounce forward without needing a
// service.App handle. Kept tiny so test fakes are one-liners.
type DebouncedEnrichRequester interface {
	RequestDebouncedEnrich(ctx context.Context, mediaItemID int64, delay time.Duration, requestedBy string) error
}

// RequestDebouncedEnrich upserts the debounce row for `mediaItemID`.
// `delay` is rounded up to the next second to keep fire_at human-
// readable in admin queries. `requestedBy` is a free-form tag the
// sweeper logs alongside the kickoff — use it to attribute slow churns
// to a source ("matcher.music", "matcher.tv", "watcher").
//
// The transaction is left to the caller via the q parameter — the
// matcher batches several upserts in its own tx, and reusing it keeps
// the per-file overhead to a single round-trip.
func RequestDebouncedEnrich(ctx context.Context, q *sqlc.Queries, mediaItemID int64, delay time.Duration, requestedBy string) error {
	if delay <= 0 {
		delay = DebouncedEnrichWindow
	}
	if requestedBy == "" {
		requestedBy = "matcher"
	}
	fireAt := time.Now().Add(delay)
	return q.UpsertDebouncedEnrich(ctx, sqlc.UpsertDebouncedEnrichParams{
		MediaItemID: mediaItemID,
		FireAt:      pgtype.Timestamptz{Time: fireAt, Valid: true},
		RequestedBy: requestedBy,
	})
}

// RequestDebouncedEnrich is the App method form — for callers that
// already hold a service handle (HTTP handlers, CLI commands) and want
// to push a debounce themselves (e.g. "user uploaded a new ZIP, give
// the scanner 60s to settle, then refresh").
func (a *App) RequestDebouncedEnrich(ctx context.Context, mediaItemID int64, delay time.Duration, requestedBy string) error {
	q := sqlc.New(a.db)
	return RequestDebouncedEnrich(ctx, q, mediaItemID, delay, requestedBy)
}

// SweepDueDebouncedEnriches is the transactional sweep used by the
// periodic worker. It SELECTs due rows FOR UPDATE SKIP LOCKED, hands
// each off via `enqueue`, and deletes the row in the same transaction.
// If anything in `enqueue` errors the whole batch rolls back so the row
// stays alive for the next tick — no lost enriches.
//
// Returns (firedCount, oldestFireAt, error). The oldest fire_at lets
// the worker log a warning when a sweep is finding rows aged > 1h, which
// signals "sweeper was down recently".
func (a *App) SweepDueDebouncedEnriches(
	ctx context.Context,
	batchSize int32,
	enqueue func(ctx context.Context, mediaItemID int64, requestedBy string) error,
) (int, time.Time, error) {
	if batchSize <= 0 {
		batchSize = 64
	}
	tx, err := a.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)
	rows, err := q.LockDueDebouncedEnriches(ctx, batchSize)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("lock due rows: %w", err)
	}

	var oldest time.Time
	for _, r := range rows {
		if !oldest.IsZero() && !r.FireAt.Time.Before(oldest) {
			// rows ORDER BY fire_at ASC so the first one is the oldest
		} else {
			oldest = r.FireAt.Time
		}

		if err := enqueue(ctx, r.MediaItemID, r.RequestedBy); err != nil {
			return 0, oldest, fmt.Errorf("enqueue media_item %d: %w", r.MediaItemID, err)
		}
		if err := q.DeleteDebouncedEnrich(ctx, r.MediaItemID); err != nil {
			return 0, oldest, fmt.Errorf("delete debounce row %d: %w", r.MediaItemID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, oldest, fmt.Errorf("commit: %w", err)
	}
	return len(rows), oldest, nil
}
