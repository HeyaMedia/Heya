package worker

import (
	"context"
	"time"

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
	orphanedInFlight, err := cleanupOrphanedInFlightScannerEntities(ctx, w.DB, time.Now().Add(-orphanedScannerEntityRetention))
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(appliedScopeArtifacts+entityArtifacts+staleInFlight.EntitiesDeleted+staleInFlight.EntityArtifactsDeleted+staleInFlight.ScanRunArtifactsDeleted), 0, err)
		return err
	}
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
		Msg("cleanup_scanner_artifacts: complete")
	return nil
}

type scannerInFlightCleanupCounts struct {
	EntitiesDeleted         int64
	EntityArtifactsDeleted  int64
	ScanRunArtifactsDeleted int64
}

func cleanupOrphanedInFlightScannerEntities(ctx context.Context, db *pgxpool.Pool, cutoff time.Time) (scannerInFlightCleanupCounts, error) {
	const query = `
WITH target AS (
    SELECT entity.id, entity.library_id, entity.media_type, entity.scope_key, entity.search_scan_run_id, entity.fetch_scan_run_id
    FROM scanner_entities entity
    WHERE entity.status IN ('matched', 'fetching')
      AND entity.updated_at < $1
      AND NOT EXISTS (
        SELECT 1
        FROM river_job job
        WHERE job.kind IN ('fetch_metadata', 'apply_metadata')
          AND job.state IN ('available', 'pending', 'running', 'retryable', 'scheduled')
          AND job.args ? 'scanner_entity_id'
          AND (job.args->>'scanner_entity_id')::bigint = entity.id
      )
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
	err := db.QueryRow(ctx, query, cutoff).Scan(&counts.EntitiesDeleted, &counts.EntityArtifactsDeleted, &counts.ScanRunArtifactsDeleted)
	return counts, err
}
