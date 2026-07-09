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
	scanRunArtifacts, err := q.CleanupOldScanRunArtifacts(ctx, cutoff)
	if err != nil {
		finishKickoff(ctx, q, taskID, startedAt, int(appliedScopeArtifacts+entityArtifacts+staleInFlight.ScanRunArtifactsDeleted), 0, err)
		return err
	}

	total := int(appliedScopeArtifacts + entityArtifacts + staleInFlight.EntitiesDeleted + staleInFlight.ScanRunArtifactsDeleted + staleInFlight.EntityArtifactsDeleted + scanRunArtifacts)
	finishKickoff(ctx, q, taskID, startedAt, total, 0, nil)
	log.Info().
		Int("retention_days", retentionDays).
		Int64("applied_scope_scan_run_artifacts", appliedScopeArtifacts).
		Int64("scanner_entity_artifacts", entityArtifacts).
		Int64("stale_in_flight_entities", staleInFlight.EntitiesDeleted).
		Int64("stale_in_flight_entity_artifacts", staleInFlight.EntityArtifactsDeleted).
		Int64("stale_in_flight_scan_run_artifacts", staleInFlight.ScanRunArtifactsDeleted).
		Int64("scan_run_artifacts", scanRunArtifacts).
		Msg("cleanup_scanner_artifacts: complete")
	return nil
}
