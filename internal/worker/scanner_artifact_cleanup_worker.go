package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/generatedwrite"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

const defaultScannerArtifactRetentionDays = 2
const orphanedScannerEntityRetention = 15 * time.Minute
const generatedSidecarReconcileLimit = 250
const generatedSidecarReconcileTimeout = 30 * time.Second

type CleanupScannerArtifactsWorker struct {
	river.WorkerDefaults[CleanupScannerArtifactsArgs]
	DB       *pgxpool.Pool
	Progress *TaskProgressBroadcaster
}

func cleanupAppliedScannerEntityArtifactsOlderThan(ctx context.Context, db *pgxpool.Pool, cutoff time.Time) (int64, error) {
	const query = `
WITH target AS MATERIALIZED (
    SELECT entity.id
    FROM scanner_entities entity
    WHERE entity.status = 'applied'
      AND entity.applied_at IS NOT NULL
      AND entity.applied_at < $1
      AND NOT EXISTS (
          SELECT 1 FROM scanner_metadata_continuations continuation
          WHERE continuation.scanner_entity_id = entity.id
      )
      AND NOT EXISTS (
          SELECT 1 FROM river_job job
          WHERE job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
            AND job.args ? 'scanner_entity_id'
            AND COALESCE(job.args->>'scanner_entity_id', '') ~ '^[0-9]+$'
            AND (job.args->>'scanner_entity_id')::bigint = entity.id
      )
    FOR UPDATE
),
safe_target AS MATERIALIZED (
    SELECT target.id
    FROM target
    WHERE NOT EXISTS (
        SELECT 1 FROM scanner_metadata_continuations continuation
        WHERE continuation.scanner_entity_id = target.id
    )
      AND NOT EXISTS (
          SELECT 1 FROM river_job job
          WHERE job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
            AND job.args ? 'scanner_entity_id'
            AND COALESCE(job.args->>'scanner_entity_id', '') ~ '^[0-9]+$'
            AND (job.args->>'scanner_entity_id')::bigint = target.id
      )
),
updated AS (
    UPDATE scanner_entities entity
    SET analysis_artifact_id = NULL,
        search_artifact_id = NULL,
        metadata_artifact_id = NULL,
        apply_artifact_id = NULL,
        updated_at = now()
    FROM safe_target
    WHERE entity.id = safe_target.id
    RETURNING entity.id
),
deleted AS (
    DELETE FROM scanner_entity_artifacts artifact
    USING updated
    WHERE artifact.entity_id = updated.id
    RETURNING artifact.id
)
SELECT count(*)::bigint FROM deleted`
	var count int64
	err := db.QueryRow(ctx, query, cutoff).Scan(&count)
	return count, err
}

func cleanupStaleInFlightScannerEntitiesOlderThan(ctx context.Context, db *pgxpool.Pool, cutoff time.Time) (scannerInFlightCleanupCounts, error) {
	const query = `
WITH target AS MATERIALIZED (
    SELECT entity.id
    FROM scanner_entities entity
    WHERE entity.status IN ('matched', 'fetching')
      AND entity.updated_at < $1
      AND NOT EXISTS (
          SELECT 1 FROM scanner_metadata_continuations continuation
          WHERE continuation.scanner_entity_id = entity.id
      )
      AND NOT EXISTS (
          SELECT 1 FROM river_job job
          WHERE job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
            AND job.args ? 'scanner_entity_id'
            AND COALESCE(job.args->>'scanner_entity_id', '') ~ '^[0-9]+$'
            AND (job.args->>'scanner_entity_id')::bigint = entity.id
      )
    FOR UPDATE
),
safe_target AS MATERIALIZED (
    SELECT target.id
    FROM target
    WHERE NOT EXISTS (
        SELECT 1 FROM scanner_metadata_continuations continuation
        WHERE continuation.scanner_entity_id = target.id
    )
      AND NOT EXISTS (
          SELECT 1 FROM river_job job
          WHERE job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
            AND job.args ? 'scanner_entity_id'
            AND COALESCE(job.args->>'scanner_entity_id', '') ~ '^[0-9]+$'
            AND (job.args->>'scanner_entity_id')::bigint = target.id
      )
),
artifact_count AS MATERIALIZED (
    SELECT count(*)::bigint AS count
    FROM scanner_entity_artifacts artifact
    JOIN safe_target ON safe_target.id = artifact.entity_id
),
entities_deleted AS (
    DELETE FROM scanner_entities entity
    USING safe_target
    WHERE entity.id = safe_target.id
    RETURNING entity.id
)
SELECT
    (SELECT count(*) FROM entities_deleted)::bigint,
    (SELECT count FROM artifact_count)::bigint`
	var counts scannerInFlightCleanupCounts
	err := db.QueryRow(ctx, query, cutoff).Scan(&counts.EntitiesDeleted, &counts.EntityArtifactsDeleted)
	return counts, err
}

// Superseded artifacts include every old non-current hand-off, even when it
// belongs to the current generation. Retried search/fetch stages can produce
// more than one artifact within a generation, so generation comparison alone
// leaks the abandoned attempt forever.
func cleanupSupersededScannerEntityArtifactsOlderThan(ctx context.Context, db *pgxpool.Pool, cutoff time.Time) (int64, error) {
	const query = `
WITH target AS MATERIALIZED (
    SELECT artifact.id
    FROM scanner_entity_artifacts artifact
    JOIN scanner_entities entity ON entity.id = artifact.entity_id
    WHERE artifact.created_at < $1
      AND artifact.id IS DISTINCT FROM entity.analysis_artifact_id
      AND artifact.id IS DISTINCT FROM entity.search_artifact_id
      AND artifact.id IS DISTINCT FROM entity.metadata_artifact_id
      AND artifact.id IS DISTINCT FROM entity.apply_artifact_id
      AND NOT EXISTS (
          SELECT 1 FROM scanner_metadata_continuations continuation
          WHERE continuation.scanner_entity_id = entity.id
             OR continuation.artifact_id = artifact.id
      )
      AND NOT EXISTS (
          SELECT 1 FROM river_job job
          WHERE job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
            AND (
                (job.args ? 'scanner_entity_id'
                 AND COALESCE(job.args->>'scanner_entity_id', '') ~ '^[0-9]+$'
                 AND (job.args->>'scanner_entity_id')::bigint = entity.id)
                OR EXISTS (
                    SELECT 1
                    FROM jsonb_each_text(job.args) argument
                    WHERE argument.key LIKE '%artifact_id'
                      AND argument.value ~ '^[0-9]+$'
                      AND argument.value::bigint = artifact.id
                )
            )
      )
),
safe_target AS MATERIALIZED (
    SELECT target.id
    FROM target
    JOIN scanner_entity_artifacts artifact ON artifact.id = target.id
    JOIN scanner_entities entity ON entity.id = artifact.entity_id
    WHERE artifact.id IS DISTINCT FROM entity.analysis_artifact_id
      AND artifact.id IS DISTINCT FROM entity.search_artifact_id
      AND artifact.id IS DISTINCT FROM entity.metadata_artifact_id
      AND artifact.id IS DISTINCT FROM entity.apply_artifact_id
      AND NOT EXISTS (
          SELECT 1 FROM scanner_metadata_continuations continuation
          WHERE continuation.scanner_entity_id = entity.id
             OR continuation.artifact_id = artifact.id
      )
      AND NOT EXISTS (
          SELECT 1 FROM river_job job
          WHERE job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
            AND (
                (job.args ? 'scanner_entity_id'
                 AND COALESCE(job.args->>'scanner_entity_id', '') ~ '^[0-9]+$'
                 AND (job.args->>'scanner_entity_id')::bigint = entity.id)
                OR EXISTS (
                    SELECT 1
                    FROM jsonb_each_text(job.args) argument
                    WHERE argument.key LIKE '%artifact_id'
                      AND argument.value ~ '^[0-9]+$'
                      AND argument.value::bigint = artifact.id
                )
            )
      )
),
deleted AS (
    DELETE FROM scanner_entity_artifacts artifact
    USING safe_target
    WHERE artifact.id = safe_target.id
    RETURNING artifact.id
)
SELECT count(*)::bigint FROM deleted`
	var count int64
	err := db.QueryRow(ctx, query, cutoff).Scan(&count)
	return count, err
}

func (w *CleanupScannerArtifactsWorker) Work(ctx context.Context, job *river.Job[CleanupScannerArtifactsArgs]) error {
	startedAt := time.Now()
	taskID := job.Args.ScheduledTaskID
	retentionDays := int(job.Args.RetentionDays)
	if retentionDays <= 0 {
		retentionDays = defaultScannerArtifactRetentionDays
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	q := sqlc.New(w.DB)

	w.Progress.Set("cleanup_scanner_artifacts", CleanupScannerArtifactsArgs{}.Kind(), "scanner artifacts")

	entityArtifacts, err := cleanupAppliedScannerEntityArtifactsOlderThan(ctx, w.DB, cutoff)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}
	// Orphan reconciliation below covers every stale matched/fetching entity,
	// requeues its scope, and also handles discovered/fetched/applying. The old
	// retention-only delete ran first and silently discarded the very rows the
	// orphan pass needed in order to requeue, so it is intentionally retired.
	staleInFlight := scannerInFlightCleanupCounts{}
	supersededArtifacts, err := cleanupSupersededScannerEntityArtifactsOlderThan(ctx, w.DB, cutoff)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(entityArtifacts+staleInFlight.EntitiesDeleted+staleInFlight.EntityArtifactsDeleted), 0, err)
		return err
	}
	orphaned, err := listOrphanedInFlightScannerEntities(ctx, w.DB, time.Now().Add(-orphanedScannerEntityRetention), 0)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(entityArtifacts+staleInFlight.EntitiesDeleted+staleInFlight.EntityArtifactsDeleted), 0, err)
		return err
	}
	rc := river.ClientFromContext[pgx.Tx](ctx)
	requeued, orphanedInFlight, err := requeueThenCleanupOrphanedScannerEntities(ctx, w.DB, orphaned, func(ctx context.Context, args ProcessLibraryScanArgs) error {
		if rc == nil {
			return errors.New("cleanup_scanner_artifacts: River client unavailable for durable orphan requeue")
		}
		return EnqueueProcessLibraryScan(ctx, rc, w.DB, args, PriorityScan, "cleanup_scanner_artifacts")
	})
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(entityArtifacts+staleInFlight.EntitiesDeleted+staleInFlight.EntityArtifactsDeleted), 0, err)
		return err
	}
	reconcileCtx, reconcileCancel := context.WithTimeout(ctx, generatedSidecarReconcileTimeout)
	generatedSidecars, err := generatedwrite.Reconcile(reconcileCtx, w.DB, generatedSidecarReconcileLimit)
	reconcileCancel()
	if err != nil {
		log.Warn().
			Err(vfs.RedactError(err)).
			Int("generated_sidecars_examined", generatedSidecars.Examined).
			Int("generated_sidecars_recovered", generatedSidecars.Recovered).
			Int("generated_sidecars_retired", generatedSidecars.Retired).
			Int("generated_sidecars_busy", generatedSidecars.Skipped).
			Int("generated_sidecars_failed", generatedSidecars.Failed).
			Msg("cleanup_scanner_artifacts: generated-sidecar page completed with failures")
		finishKickoff(ctx, q, taskID, startedAt, int(entityArtifacts+supersededArtifacts+orphanedInFlight.EntitiesDeleted+orphanedInFlight.EntityArtifactsDeleted), 0, err)
		return err
	}

	total := int(entityArtifacts + supersededArtifacts + staleInFlight.EntitiesDeleted + staleInFlight.EntityArtifactsDeleted + orphanedInFlight.EntitiesDeleted + orphanedInFlight.EntityArtifactsDeleted + int64(generatedSidecars.Recovered+generatedSidecars.Retired))
	finishKickoff(ctx, q, taskID, startedAt, total, 0, nil)
	log.Info().
		Int("retention_days", retentionDays).
		Int64("scanner_entity_artifacts", entityArtifacts).
		Int64("superseded_scanner_entity_artifacts", supersededArtifacts).
		Int64("stale_in_flight_entities", staleInFlight.EntitiesDeleted).
		Int64("stale_in_flight_entity_artifacts", staleInFlight.EntityArtifactsDeleted).
		Int64("orphaned_in_flight_entities", orphanedInFlight.EntitiesDeleted).
		Int64("orphaned_in_flight_entity_artifacts", orphanedInFlight.EntityArtifactsDeleted).
		Int("orphaned_scopes_requeued", requeued).
		Int("generated_sidecars_examined", generatedSidecars.Examined).
		Int("generated_sidecars_recovered", generatedSidecars.Recovered).
		Int("generated_sidecars_retired", generatedSidecars.Retired).
		Int("generated_sidecars_busy", generatedSidecars.Skipped).
		Int("generated_sidecars_failed", generatedSidecars.Failed).
		Msg("cleanup_scanner_artifacts: complete")
	return nil
}

// orphanedScannerEntity is an in-flight scanner entity whose queue job died —
// crash, cancelled deploy, exhausted retries — leaving no live
// search_metadata/fetch_metadata/apply_metadata job to ever advance it.
type orphanedScannerEntity struct {
	ID                 int64
	LibraryID          int64
	ScopePaths         []string
	PipelineGeneration int64
	Status             string
	UpdatedAt          time.Time
	// Cancelled marks entities whose most recent pipeline job was cancelled
	// by the user: they are cleaned up but NOT requeued — cancel means stop,
	// and the next scan re-discovers the work through change detection.
	Cancelled bool
}

// listOrphanedInFlightScannerEntities finds entities stuck in any in-flight
// state past the cutoff with no live pipeline job referencing them. It covers
// stale handoffs and exhausted error jobs as well as the ordinary stages: once
// River has no live retry, none of those rows has another path forward.
func listOrphanedInFlightScannerEntities(ctx context.Context, db *pgxpool.Pool, cutoff time.Time, libraryID int64) ([]orphanedScannerEntity, error) {
	rows, err := db.Query(ctx, `
		SELECT entity.id, entity.library_id, entity.scope_paths,
		       entity.pipeline_generation, entity.status, entity.updated_at,
		  EXISTS (
		    SELECT 1
		    FROM river_job job
		    WHERE job.state = 'cancelled'
		      AND job.args ? 'scanner_entity_id'
		      AND COALESCE(job.args->>'scanner_entity_id', '') ~ '^[0-9]+$'
		      AND (job.args->>'scanner_entity_id')::bigint = entity.id
		      AND EXISTS (
		        SELECT 1
		        FROM LATERAL jsonb_each_text(job.args) argument
		        JOIN scanner_entity_artifacts artifact
		          ON argument.value ~ '^[0-9]+$'
		         AND artifact.id = argument.value::bigint
		         AND artifact.entity_id = entity.id
		         AND artifact.pipeline_generation = entity.pipeline_generation
		        WHERE (argument.key = 'analysis_artifact_id' AND artifact.id = entity.analysis_artifact_id)
		           OR (argument.key = 'search_artifact_id' AND artifact.id = entity.search_artifact_id)
		           OR (argument.key = 'metadata_artifact_id' AND artifact.id = entity.metadata_artifact_id)
		      )
		  ) AS cancelled
		FROM scanner_entities entity
		WHERE entity.status IN (
		  'discovered', 'matched', 'fetching', 'fetched', 'applying',
		  'stale', 'error', 'metadata_error', 'apply_error', 'failed'
		)
		  AND entity.updated_at < $1
		  AND ($2::bigint = 0 OR entity.library_id = $2)
		  AND NOT EXISTS (
		    SELECT 1
		    FROM river_job job
		    WHERE job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
		      AND (
		        (job.args ? 'scanner_entity_id'
		         AND COALESCE(job.args->>'scanner_entity_id', '') ~ '^[0-9]+$'
		         AND (job.args->>'scanner_entity_id')::bigint = entity.id)
		        OR EXISTS (
		          SELECT 1
		          FROM scanner_entity_artifacts artifact,
		               LATERAL jsonb_each_text(job.args) argument
		          WHERE artifact.entity_id = entity.id
		            AND argument.key LIKE '%artifact_id'
		            AND argument.value ~ '^[0-9]+$'
		            AND argument.value::bigint = artifact.id
		        )
		      )
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
		if err := rows.Scan(
			&e.ID, &e.LibraryID, &e.ScopePaths,
			&e.PipelineGeneration, &e.Status, &e.UpdatedAt, &e.Cancelled,
		); err != nil {
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
	cancelled := orphaned[:0]
	for _, entity := range orphaned {
		if entity.Cancelled {
			cancelled = append(cancelled, entity)
		}
	}
	counts, err := cleanupOrphanedInFlightScannerEntities(ctx, db, cancelled)
	if err != nil {
		return 0, err
	}
	return counts.EntitiesDeleted, nil
}

// requeueThenCleanupOrphanedScannerEntities puts every non-cancelled orphan
// scope durably back into the pipeline before deleting any entity. Deleting
// alone used to rely
// on the next scan's change detection to rediscover the work — which held
// only while the mtime bug made everything look changed. Now that unchanged
// files (and parked unmatched ones) stay quiet, an NFO-triggered or
// previously-applied scope would otherwise never be retried: its NFO
// seen-marker was consumed at kickoff and its files read as unchanged.
// Force bypasses change detection; the jobs dedupe by (library, scopes)
// while active, so shared scopes re-enqueue once. If even one enqueue fails,
// no candidate is deleted: retaining duplicate/stale state is recoverable,
// losing the only pointer to work is not.
func requeueThenCleanupOrphanedScannerEntities(
	ctx context.Context,
	db *pgxpool.Pool,
	orphaned []orphanedScannerEntity,
	enqueue func(context.Context, ProcessLibraryScanArgs) error,
) (int, scannerInFlightCleanupCounts, error) {
	argsList := orphanedScannerRequeueArgs(orphaned)
	if len(argsList) > 0 && enqueue == nil {
		return 0, scannerInFlightCleanupCounts{}, errors.New("cleanup_scanner_artifacts: orphan scope enqueuer unavailable")
	}
	for index, args := range argsList {
		if err := enqueue(ctx, args); err != nil {
			return index, scannerInFlightCleanupCounts{}, fmt.Errorf("cleanup_scanner_artifacts: durably requeue orphan scope: %w", err)
		}
	}
	counts, err := cleanupOrphanedInFlightScannerEntities(ctx, db, orphaned)
	if err != nil {
		return len(argsList), counts, err
	}
	return len(argsList), counts, nil
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
	EntityIDs              []int64
}

func deletedOrphanedScannerEntities(orphaned []orphanedScannerEntity, deletedIDs []int64) []orphanedScannerEntity {
	if len(orphaned) == 0 || len(deletedIDs) == 0 {
		return nil
	}
	deleted := make(map[int64]struct{}, len(deletedIDs))
	for _, id := range deletedIDs {
		deleted[id] = struct{}{}
	}
	out := make([]orphanedScannerEntity, 0, len(deletedIDs))
	for _, entity := range orphaned {
		if _, ok := deleted[entity.ID]; ok {
			out = append(out, entity)
		}
	}
	return out
}

func cleanupOrphanedInFlightScannerEntities(ctx context.Context, db *pgxpool.Pool, orphaned []orphanedScannerEntity) (scannerInFlightCleanupCounts, error) {
	if len(orphaned) == 0 {
		return scannerInFlightCleanupCounts{}, nil
	}
	ids := make([]int64, 0, len(orphaned))
	generations := make([]int64, 0, len(orphaned))
	statuses := make([]string, 0, len(orphaned))
	updatedAts := make([]time.Time, 0, len(orphaned))
	for _, entity := range orphaned {
		ids = append(ids, entity.ID)
		generations = append(generations, entity.PipelineGeneration)
		statuses = append(statuses, entity.Status)
		updatedAts = append(updatedAts, entity.UpdatedAt)
	}
	// Candidates were read before replacement work was enqueued. Match the
	// exact generation and lifecycle snapshot so a fast replacement analysis
	// cannot reuse the row and then be deleted by this older cleanup pass.
	const query = `
WITH candidate AS MATERIALIZED (
    SELECT *
    FROM unnest(
        $1::bigint[],
        $2::bigint[],
        $3::text[],
        $4::timestamptz[]
    ) AS row(id, pipeline_generation, status, updated_at)
),
target AS MATERIALIZED (
    SELECT entity.id,
           (SELECT count(*)::bigint
            FROM scanner_entity_artifacts artifact
            WHERE artifact.entity_id = entity.id) AS artifact_count
    FROM scanner_entities entity
    JOIN candidate
      ON candidate.id = entity.id
     AND candidate.pipeline_generation = entity.pipeline_generation
     AND candidate.status = entity.status
     AND candidate.updated_at = entity.updated_at
    WHERE true
      AND NOT EXISTS (
          SELECT 1 FROM scanner_metadata_continuations continuation
          WHERE continuation.scanner_entity_id = entity.id
      )
      AND NOT EXISTS (
          SELECT 1 FROM river_job job
          WHERE job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
            AND (
                (job.args ? 'scanner_entity_id'
                 AND COALESCE(job.args->>'scanner_entity_id', '') ~ '^[0-9]+$'
                 AND (job.args->>'scanner_entity_id')::bigint = entity.id)
                OR EXISTS (
                    SELECT 1
                    FROM scanner_entity_artifacts artifact,
                         LATERAL jsonb_each_text(job.args) argument
                    WHERE artifact.entity_id = entity.id
                      AND argument.key LIKE '%artifact_id'
                      AND argument.value ~ '^[0-9]+$'
                      AND argument.value::bigint = artifact.id
                )
            )
      )
    FOR UPDATE
),
entities_deleted AS (
    DELETE FROM scanner_entities entity
    USING target
    WHERE entity.id = target.id
      AND NOT EXISTS (
          SELECT 1 FROM scanner_metadata_continuations continuation
          WHERE continuation.scanner_entity_id = entity.id
      )
      AND NOT EXISTS (
          SELECT 1 FROM river_job job
          WHERE job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
            AND job.args ? 'scanner_entity_id'
            AND COALESCE(job.args->>'scanner_entity_id', '') ~ '^[0-9]+$'
            AND (job.args->>'scanner_entity_id')::bigint = entity.id
      )
    RETURNING entity.id, target.artifact_count
)
SELECT
	count(*)::bigint AS entities_deleted,
	COALESCE(sum(artifact_count), 0)::bigint AS entity_artifacts_deleted,
	COALESCE(array_agg(id ORDER BY id), '{}'::bigint[]) AS entity_ids
FROM entities_deleted;
`
	var counts scannerInFlightCleanupCounts
	err := db.QueryRow(ctx, query, ids, generations, statuses, updatedAts).Scan(&counts.EntitiesDeleted, &counts.EntityArtifactsDeleted, &counts.EntityIDs)
	return counts, err
}
