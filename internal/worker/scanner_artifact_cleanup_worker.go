package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

const defaultScannerArtifactRetentionDays = 2
const orphanedScannerEntityRetention = 15 * time.Minute

type CleanupScannerArtifactsWorker struct {
	river.WorkerDefaults[CleanupScannerArtifactsArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func (w *CleanupScannerArtifactsWorker) Work(ctx context.Context, job *river.Job[CleanupScannerArtifactsArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	retentionDays := int(job.Args.RetentionDays)
	if retentionDays <= 0 {
		retentionDays = defaultScannerArtifactRetentionDays
	}
	cutoff := pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, -retentionDays), Valid: true}
	q := sqlc.New(w.DB)

	w.Progress.Set("cleanup_scanner_artifacts", CleanupScannerArtifactsArgs{}.Kind(), "scanner artifacts")

	entityArtifacts, err := q.CleanupAppliedScannerEntityArtifactsOlderThan(ctx, cutoff)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}
	staleInFlight, err := q.CleanupStaleInFlightScannerEntitiesOlderThan(ctx, cutoff)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(entityArtifacts), 0, err)
		return err
	}
	orphaned, err := listOrphanedInFlightScannerEntities(ctx, w.DB, time.Now().Add(-orphanedScannerEntityRetention), 0)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(entityArtifacts+staleInFlight.EntitiesDeleted+staleInFlight.EntityArtifactsDeleted), 0, err)
		return err
	}
	orphanedInFlight, err := cleanupOrphanedInFlightScannerEntities(ctx, w.DB, orphaned)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(entityArtifacts+staleInFlight.EntitiesDeleted+staleInFlight.EntityArtifactsDeleted), 0, err)
		return err
	}
	requeued := reenqueueOrphanedScannerScopes(ctx, river.ClientFromContext[pgx.Tx](ctx), w.DB, orphaned)

	total := int(entityArtifacts + staleInFlight.EntitiesDeleted + staleInFlight.EntityArtifactsDeleted + orphanedInFlight.EntitiesDeleted + orphanedInFlight.EntityArtifactsDeleted)
	finishKickoff(ctx, q, taskID, startedAt, total, 0, nil)
	log.Info().
		Int("retention_days", retentionDays).
		Int64("scanner_entity_artifacts", entityArtifacts).
		Int64("stale_in_flight_entities", staleInFlight.EntitiesDeleted).
		Int64("stale_in_flight_entity_artifacts", staleInFlight.EntityArtifactsDeleted).
		Int64("orphaned_in_flight_entities", orphanedInFlight.EntitiesDeleted).
		Int64("orphaned_in_flight_entity_artifacts", orphanedInFlight.EntityArtifactsDeleted).
		Int("orphaned_scopes_requeued", requeued).
		Msg("cleanup_scanner_artifacts: complete")
	return nil
}

// orphanedScannerEntity is an in-flight scanner entity whose queue job died —
// crash, cancelled deploy, exhausted retries — leaving no live
// search_metadata/fetch_metadata/apply_metadata job to ever advance it.
type orphanedScannerEntity struct {
	ID         int64
	LibraryID  int64
	ScopePaths []string
	// Cancelled marks entities whose most recent pipeline job was cancelled
	// by the user: they are cleaned up but NOT requeued — cancel means stop,
	// and the next scan re-discovers the work through change detection.
	Cancelled bool
}

// listOrphanedInFlightScannerEntities finds entities stuck in any in-flight
// state past the cutoff with no live pipeline job referencing them. It covers
// 'fetched' and 'applying' as well as 'matched'/'fetching': an apply job that
// died after fetch persisted leaves those states orphaned exactly the same
// way.
func listOrphanedInFlightScannerEntities(ctx context.Context, db *pgxpool.Pool, cutoff time.Time, libraryID int64) ([]orphanedScannerEntity, error) {
	rows, err := db.Query(ctx, `
		SELECT entity.id, entity.library_id, entity.scope_paths,
		  EXISTS (
		    SELECT 1
		    FROM river_job job
		    WHERE job.kind IN ('search_metadata', 'fetch_metadata', 'apply_metadata')
		      AND job.state = 'cancelled'
		      AND job.args ? 'scanner_entity_id'
		      AND (job.args->>'scanner_entity_id')::bigint = entity.id
		  ) AS cancelled
		FROM scanner_entities entity
		WHERE entity.status IN ('discovered', 'matched', 'fetching', 'fetched', 'applying')
		  AND entity.updated_at < $1
		  AND ($2::bigint = 0 OR entity.library_id = $2)
		  AND NOT EXISTS (
		    SELECT 1
		    FROM river_job job
		    WHERE job.kind IN ('search_metadata', 'fetch_metadata', 'apply_metadata')
		      AND job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
		      AND job.args ? 'scanner_entity_id'
		      AND (job.args->>'scanner_entity_id')::bigint = entity.id
		  )
		  AND NOT EXISTS (
		    SELECT 1
		    FROM scanner_metadata_continuations continuation
		    WHERE continuation.scanner_entity_id = entity.id
		  )`, cutoff, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []orphanedScannerEntity
	for rows.Next() {
		var e orphanedScannerEntity
		if err := rows.Scan(&e.ID, &e.LibraryID, &e.ScopePaths, &e.Cancelled); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SweepCancelledScannerEntities removes in-flight scanner entities left
// behind by a user cancellation — WITHOUT requeueing them. Run right after
// cancelling scan jobs so the orphan pruner (which requeues by design)
// doesn't resurrect work the user explicitly stopped. libraryID 0 sweeps
// every library. The next scan re-discovers the cancelled units through
// normal change detection.
func SweepCancelledScannerEntities(ctx context.Context, db *pgxpool.Pool, libraryID int64) (int64, error) {
	orphaned, err := listOrphanedInFlightScannerEntities(ctx, db, time.Now(), libraryID)
	if err != nil {
		return 0, err
	}
	counts, err := cleanupOrphanedInFlightScannerEntities(ctx, db, orphaned)
	if err != nil {
		return 0, err
	}
	return counts.EntitiesDeleted, nil
}

// reenqueueOrphanedScannerScopes puts the deleted entities' scopes back into
// the pipeline with a forced scoped process_scan. Deleting alone used to rely
// on the next scan's change detection to rediscover the work — which held
// only while the mtime bug made everything look changed. Now that unchanged
// files (and parked unmatched ones) stay quiet, an NFO-triggered or
// previously-applied scope would otherwise never be retried: its NFO
// seen-marker was consumed at kickoff and its files read as unchanged.
// Force bypasses change detection; the jobs dedupe by (library, scopes)
// while active, so shared scopes re-enqueue once.
func reenqueueOrphanedScannerScopes(ctx context.Context, rc *river.Client[pgx.Tx], db *pgxpool.Pool, orphaned []orphanedScannerEntity) int {
	if rc == nil {
		return 0
	}
	requeued := 0
	for _, args := range orphanedScannerRequeueArgs(orphaned) {
		if err := EnqueueProcessLibraryScan(ctx, rc, db, args, PriorityScan, "cleanup_scanner_artifacts"); err != nil {
			log.Warn().Err(err).Int64("library_id", args.LibraryID).Strs("scopes", args.ScopePaths).Msg("cleanup_scanner_artifacts: requeue orphaned scope failed")
			continue
		}
		requeued++
	}
	return requeued
}

// orphanedScannerRequeueArgs splits each orphaned entity's scopes into one
// forced per-scope process_scan (per owner unit) — a legacy multi-owner
// entity requeues per owner instead of resurrecting the batch. An entity
// with no scope paths (a legacy whole-library pass) requeues as a nil-scope
// job, which the process_scan worker re-fans into owner units itself.
func orphanedScannerRequeueArgs(orphaned []orphanedScannerEntity) []ProcessLibraryScanArgs {
	seen := map[string]bool{}
	var out []ProcessLibraryScanArgs
	add := func(libraryID int64, scopes []string) {
		key := fmt.Sprintf("%d\x00%s", libraryID, strings.Join(scopes, "\x00"))
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, ProcessLibraryScanArgs{
			LibraryID:  libraryID,
			ScopePaths: scopes,
			Force:      true,
		})
	}
	for _, entity := range orphaned {
		if entity.Cancelled {
			continue // user said stop; don't resurrect their cancellation
		}
		if len(entity.ScopePaths) == 0 {
			add(entity.LibraryID, nil)
			continue
		}
		for _, scope := range entity.ScopePaths {
			add(entity.LibraryID, []string{scope})
		}
	}
	return out
}

type scannerInFlightCleanupCounts struct {
	EntitiesDeleted        int64
	EntityArtifactsDeleted int64
}

func cleanupOrphanedInFlightScannerEntities(ctx context.Context, db *pgxpool.Pool, orphaned []orphanedScannerEntity) (scannerInFlightCleanupCounts, error) {
	if len(orphaned) == 0 {
		return scannerInFlightCleanupCounts{}, nil
	}
	ids := make([]int64, 0, len(orphaned))
	for _, entity := range orphaned {
		ids = append(ids, entity.ID)
	}
	const query = `
WITH target AS (
    SELECT entity.id
    FROM scanner_entities entity
    WHERE entity.id = ANY($1)
),
entity_artifacts_deleted AS (
    DELETE FROM scanner_entity_artifacts artifact
    USING target
    WHERE artifact.entity_id = target.id
    RETURNING artifact.id
),
entities_deleted AS (
    DELETE FROM scanner_entities entity
    USING target
    WHERE entity.id = target.id
    RETURNING entity.id
)
SELECT
    (SELECT count(*) FROM entities_deleted)::bigint AS entities_deleted,
    (SELECT count(*) FROM entity_artifacts_deleted)::bigint AS entity_artifacts_deleted;
`
	var counts scannerInFlightCleanupCounts
	err := db.QueryRow(ctx, query, ids).Scan(&counts.EntitiesDeleted, &counts.EntityArtifactsDeleted)
	return counts, err
}
