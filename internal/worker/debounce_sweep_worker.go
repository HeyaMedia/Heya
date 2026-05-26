package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// DebounceSweepArgs is the periodic sweep that fires
// debounced_enriches whose fire_at has elapsed. Scheduled by River's
// PeriodicJobs every 10 seconds — the cadence is tight enough that
// "user sees fresh metadata within a minute of dropping new files in"
// holds, and the sweep is cheap (index-only scan plus N enqueues).
//
// Empty args + UniqueByArgs means concurrent ticks coalesce to one
// running sweep, which is exactly what we want.
type DebounceSweepArgs struct{}

func (DebounceSweepArgs) Kind() string { return "debounce_sweep" }
func (DebounceSweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       "debounce_sweep",
		MaxAttempts: 1,
		// No UniqueByArgs: the periodic enqueuer in River v0.38 treats a
		// completed job from a prior tick as a unique conflict for the
		// next tick, suppressing every subsequent enqueue. The sweep is
		// itself idempotent (FOR UPDATE SKIP LOCKED + DELETE) so letting
		// every periodic tick insert a fresh job is safe.
	}
}

// DebounceSweepWorker pulls due rows out of debounced_enriches and
// fires a forced enrich for each. The actual SELECT-enqueue-DELETE
// happens inside a single transaction (see service.SweepDueDebouncedEnriches)
// so a failed enqueue keeps the row alive for the next tick.
type DebounceSweepWorker struct {
	river.WorkerDefaults[DebounceSweepArgs]
	DB *pgxpool.Pool
}

// Work runs the sweep. The river.Client lives in ctx (River injects it
// for any worker that needs to schedule follow-up work) and we hand it
// to the per-row enqueue closure as a tx-bound rc — the SweepDueDebouncedEnriches
// transaction owns the lifecycle. Note: River's Insert isn't strictly
// joined to our pgx.Tx here, but the worst case (enqueue succeeds, row
// delete fails) just re-fires the enrich next tick — which the enrich
// worker's idempotency gate handles gracefully when nothing actually
// changed in between.
func (w *DebounceSweepWorker) Work(ctx context.Context, _ *river.Job[DebounceSweepArgs]) error {
	rc := river.ClientFromContext[pgx.Tx](ctx)
	if rc == nil {
		return fmt.Errorf("debounce_sweep: no river client in context")
	}

	tx, err := w.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := sqlc.New(tx)
	rows, err := q.LockDueDebouncedEnriches(ctx, 64)
	if err != nil {
		return fmt.Errorf("lock due rows: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}

	// Warn when the oldest row is much older than the debounce window —
	// that's the "sweeper was down" canary. The row stays correct (we
	// still process it now); the log line just helps us spot extended
	// outages.
	oldest := rows[0].FireAt.Time
	if age := time.Since(oldest); age > time.Hour {
		log.Warn().Dur("age", age).Int("backlog", len(rows)).Msg("debounce_sweep: oldest row > 1h overdue; sweeper may have been paused")
	}

	fired := 0
	for _, r := range rows {
		item, err := q.GetMediaItemByID(ctx, r.MediaItemID)
		if err != nil {
			// Item vanished between upsert and sweep — drop the row.
			_ = q.DeleteDebouncedEnrich(ctx, r.MediaItemID)
			log.Debug().Err(err).Int64("media_item_id", r.MediaItemID).Msg("debounce_sweep: media_item gone, dropping debounce")
			continue
		}
		if err := EnqueueEnrichForce(ctx, rc, item.ID, item.MediaType, EnrichSourceForced); err != nil {
			// Don't delete the row on enqueue failure — let the next
			// tick retry. Rollback below restores all unprocessed rows
			// anyway via the FOR UPDATE lock release.
			return fmt.Errorf("enqueue enrich for %d: %w", item.ID, err)
		}
		if err := q.DeleteDebouncedEnrich(ctx, r.MediaItemID); err != nil {
			return fmt.Errorf("delete debounce row %d: %w", r.MediaItemID, err)
		}
		fired++
		log.Debug().Int64("media_item_id", item.ID).Str("title", item.Title).Str("requested_by", r.RequestedBy).Msg("debounce_sweep: enrich fired")
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	log.Info().Int("fired", fired).Msg("debounce_sweep")
	return nil
}
