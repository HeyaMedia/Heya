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

	appliedScopeArtifacts, err := q.CleanupCompletedScanRunArtifactsForAppliedScopes(ctx)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, 0, 0, err)
		return err
	}
	entityArtifacts, err := q.CleanupAppliedScannerEntityArtifactsOlderThan(ctx, cutoff)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(appliedScopeArtifacts), 0, err)
		return err
	}
	staleInFlight, err := q.CleanupStaleInFlightScannerEntitiesOlderThan(ctx, cutoff)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(appliedScopeArtifacts+entityArtifacts), 0, err)
		return err
	}
	orphaned, err := listOrphanedInFlightScannerEntities(ctx, w.DB, time.Now().Add(-orphanedScannerEntityRetention))
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(appliedScopeArtifacts+entityArtifacts+staleInFlight.EntitiesDeleted+staleInFlight.EntityArtifactsDeleted+staleInFlight.ScanRunArtifactsDeleted), 0, err)
		return err
	}
	orphanedInFlight, err := cleanupOrphanedInFlightScannerEntities(ctx, w.DB, orphaned)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(appliedScopeArtifacts+entityArtifacts+staleInFlight.EntitiesDeleted+staleInFlight.EntityArtifactsDeleted+staleInFlight.ScanRunArtifactsDeleted), 0, err)
		return err
	}
	requeued := reenqueueOrphanedScannerScopes(ctx, river.ClientFromContext[pgx.Tx](ctx), orphaned)
	scanRunArtifacts, err := q.CleanupOldScanRunArtifacts(ctx, cutoff)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(appliedScopeArtifacts+entityArtifacts+staleInFlight.ScanRunArtifactsDeleted+orphanedInFlight.ScanRunArtifactsDeleted), 0, err)
		return err
	}

	total := int(appliedScopeArtifacts + entityArtifacts + staleInFlight.EntitiesDeleted + staleInFlight.ScanRunArtifactsDeleted + staleInFlight.EntityArtifactsDeleted + orphanedInFlight.EntitiesDeleted + orphanedInFlight.EntityArtifactsDeleted + orphanedInFlight.ScanRunArtifactsDeleted + scanRunArtifacts)
	finishKickoff(ctx, q, taskID, startedAt, total, 0, nil)
	log.Info().
		Int("retention_days", retentionDays).
		Int64("applied_scope_scan_run_artifacts", appliedScopeArtifacts).
		Int64("scanner_entity_artifacts", entityArtifacts).
		Int64("stale_in_flight_entities", staleInFlight.EntitiesDeleted).
		Int64("stale_in_flight_entity_artifacts", staleInFlight.EntityArtifactsDeleted).
		Int64("stale_in_flight_scan_run_artifacts", staleInFlight.ScanRunArtifactsDeleted).
		Int64("orphaned_in_flight_entities", orphanedInFlight.EntitiesDeleted).
		Int64("orphaned_in_flight_entity_artifacts", orphanedInFlight.EntityArtifactsDeleted).
		Int64("orphaned_in_flight_scan_run_artifacts", orphanedInFlight.ScanRunArtifactsDeleted).
		Int64("scan_run_artifacts", scanRunArtifacts).
		Int("orphaned_scopes_requeued", requeued).
		Msg("cleanup_scanner_artifacts: complete")
	return nil
}

// orphanedScannerEntity is an in-flight scanner entity whose queue job died —
// crash, cancelled deploy, exhausted retries — leaving no live
// fetch_metadata/apply_metadata job to ever advance it.
type orphanedScannerEntity struct {
	ID         int64
	LibraryID  int64
	ScopePaths []string
}

// listOrphanedInFlightScannerEntities finds entities stuck in any in-flight
// state past the cutoff with no live pipeline job referencing them. It covers
// 'fetched' and 'applying' as well as 'matched'/'fetching': an apply job that
// died after fetch persisted leaves those states orphaned exactly the same
// way.
func listOrphanedInFlightScannerEntities(ctx context.Context, db *pgxpool.Pool, cutoff time.Time) ([]orphanedScannerEntity, error) {
	rows, err := db.Query(ctx, `
		SELECT entity.id, entity.library_id, entity.scope_paths
		FROM scanner_entities entity
		WHERE entity.status IN ('matched', 'fetching', 'fetched', 'applying')
		  AND entity.updated_at < $1
		  AND NOT EXISTS (
		    SELECT 1
		    FROM river_job job
		    WHERE job.kind IN ('fetch_metadata', 'apply_metadata')
		      AND job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
		      AND job.args ? 'scanner_entity_id'
		      AND (job.args->>'scanner_entity_id')::bigint = entity.id
		  )`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []orphanedScannerEntity
	for rows.Next() {
		var e orphanedScannerEntity
		if err := rows.Scan(&e.ID, &e.LibraryID, &e.ScopePaths); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
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
func reenqueueOrphanedScannerScopes(ctx context.Context, rc *river.Client[pgx.Tx], orphaned []orphanedScannerEntity) int {
	if rc == nil || len(orphaned) == 0 {
		return 0
	}
	seen := map[string]bool{}
	requeued := 0
	for _, entity := range orphaned {
		key := fmt.Sprintf("%d\x00%s", entity.LibraryID, strings.Join(entity.ScopePaths, "\x00"))
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := enqueueProcessLibraryScan(ctx, rc, ProcessLibraryScanArgs{
			LibraryID:  entity.LibraryID,
			ScopePaths: entity.ScopePaths,
			Force:      true,
		}, PriorityScan, "cleanup_scanner_artifacts"); err != nil {
			log.Warn().Err(err).Int64("library_id", entity.LibraryID).Strs("scopes", entity.ScopePaths).Msg("cleanup_scanner_artifacts: requeue orphaned scope failed")
			continue
		}
		requeued++
	}
	return requeued
}

type scannerInFlightCleanupCounts struct {
	EntitiesDeleted         int64
	EntityArtifactsDeleted  int64
	ScanRunArtifactsDeleted int64
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
    SELECT entity.id, entity.library_id, entity.media_type, entity.scope_key, entity.search_scan_run_id, entity.fetch_scan_run_id
    FROM scanner_entities entity
    WHERE entity.id = ANY($1)
),
target_runs AS (
    SELECT library_id, media_type, scope_key, search_scan_run_id AS scan_run_id
    FROM target
    WHERE search_scan_run_id IS NOT NULL
    UNION
    SELECT library_id, media_type, scope_key, fetch_scan_run_id AS scan_run_id
    FROM target
    WHERE fetch_scan_run_id IS NOT NULL
    UNION
    SELECT target.library_id, target.media_type, target.scope_key, artifact.scan_run_id
    FROM scanner_entity_artifacts artifact
    JOIN target ON target.id = artifact.entity_id
    WHERE artifact.scan_run_id IS NOT NULL
),
scan_deleted AS (
    DELETE FROM scan_run_artifacts artifact
    USING target_runs, scan_runs
    WHERE artifact.scan_run_id = target_runs.scan_run_id
      AND artifact.scope_key = target_runs.scope_key
      AND scan_runs.id = artifact.scan_run_id
      AND scan_runs.finished_at IS NOT NULL
      AND NOT EXISTS (
        SELECT 1
        FROM scanner_entities peer
        WHERE peer.library_id = target_runs.library_id
          AND peer.media_type = target_runs.media_type
          AND peer.scope_key = target_runs.scope_key
          AND NOT EXISTS (
            SELECT 1
            FROM target
            WHERE target.id = peer.id
          )
          AND (
            peer.search_scan_run_id = target_runs.scan_run_id
            OR peer.fetch_scan_run_id = target_runs.scan_run_id
            OR EXISTS (
                SELECT 1
                FROM scanner_entity_artifacts peer_artifact
                WHERE peer_artifact.entity_id = peer.id
                  AND peer_artifact.scan_run_id = target_runs.scan_run_id
            )
          )
      )
    RETURNING artifact.id
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
    (SELECT count(*) FROM entity_artifacts_deleted)::bigint AS entity_artifacts_deleted,
    (SELECT count(*) FROM scan_deleted)::bigint AS scan_run_artifacts_deleted;
`
	var counts scannerInFlightCleanupCounts
	err := db.QueryRow(ctx, query, ids).Scan(&counts.EntitiesDeleted, &counts.EntityArtifactsDeleted, &counts.ScanRunArtifactsDeleted)
	return counts, err
}
