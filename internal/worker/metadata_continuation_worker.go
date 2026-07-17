package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

const (
	metadataContinuationBatch      = 256
	metadataContinuationAdoptBatch = 1000
	metadataContinuationLease      = 5 * time.Minute
)

// MetadataContinuationSweepArgs promotes a bounded number of due remote
// metadata continuations into River. The durable waiting population lives in
// scanner_metadata_continuations; River only carries work that can run now.
type MetadataContinuationSweepArgs struct{}

func (MetadataContinuationSweepArgs) Kind() string { return "metadata_continuation_sweep" }
func (MetadataContinuationSweepArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "metadata_continuation_sweep", MaxAttempts: 1}
}

type MetadataContinuationSweepWorker struct {
	river.WorkerDefaults[MetadataContinuationSweepArgs]
	DB *pgxpool.Pool
}

type metadataContinuationRow struct {
	ID       int64
	Kind     string
	ArgsJSON []byte
	Priority int
	Source   string
}

func parkMetadataContinuation(ctx context.Context, db queueops.DB, kind string, libraryID, scannerEntityID, artifactID int64, args any, priority int, source string, retryAfter time.Duration) error {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("marshal metadata continuation: %w", err)
	}
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	var scheduledTaskID string
	switch typed := args.(type) {
	case SearchLibraryMetadataArgs:
		scheduledTaskID = typed.ScheduledTaskID
	case FetchLibraryMetadataArgs:
		scheduledTaskID = typed.ScheduledTaskID
	}
	_, err = db.Exec(ctx, `
		INSERT INTO scanner_metadata_continuations
			(kind, library_id, scanner_entity_id, artifact_id, args, priority, source, scheduled_task_id, next_attempt_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9)
		ON CONFLICT (kind, scanner_entity_id, artifact_id) DO UPDATE
		SET library_id = EXCLUDED.library_id,
		    args = EXCLUDED.args,
		    priority = EXCLUDED.priority,
		    source = EXCLUDED.source,
		    scheduled_task_id = EXCLUDED.scheduled_task_id,
		    next_attempt_at = EXCLUDED.next_attempt_at,
		    updated_at = now()
	`, kind, libraryID, scannerEntityID, artifactID, argsJSON, priority, source, scheduledTaskID, time.Now().Add(retryAfter))
	if err != nil {
		return fmt.Errorf("park metadata continuation: %w", err)
	}
	return nil
}

func deleteMetadataContinuation(ctx context.Context, db queueops.DB, kind string, scannerEntityID, artifactID int64) {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := db.Exec(persistCtx, `
		DELETE FROM scanner_metadata_continuations
		WHERE kind = $1 AND scanner_entity_id = $2 AND artifact_id = $3
	`, kind, scannerEntityID, artifactID); err != nil {
		log.Warn().Err(err).Str("kind", kind).Int64("scanner_entity_id", scannerEntityID).Int64("artifact_id", artifactID).Msg("delete metadata continuation failed")
	}
}

// DeleteMetadataContinuationForRiverJob prevents an explicitly-cancelled poll
// job from being re-promoted by the sweeper after its lease expires.
func DeleteMetadataContinuationForRiverJob(ctx context.Context, db queueops.DB, jobID int64) error {
	_, err := db.Exec(ctx, `
		DELETE FROM scanner_metadata_continuations continuation
		USING river_job job
		WHERE job.id = $1
		  AND continuation.kind = job.kind
		  AND continuation.scanner_entity_id = NULLIF(job.args->>'scanner_entity_id', '')::bigint
		  AND continuation.artifact_id = CASE job.kind
		      WHEN 'search_metadata' THEN NULLIF(job.args->>'analysis_artifact_id', '')::bigint
		      WHEN 'fetch_metadata' THEN NULLIF(job.args->>'search_artifact_id', '')::bigint
		      ELSE NULL
		  END
	`, jobID)
	return err
}

// DeleteMetadataContinuations removes parked scan work during scan/queue
// cancellation. libraryID=0 means every library.
func DeleteMetadataContinuations(ctx context.Context, db queueops.DB, libraryID int64) (int64, error) {
	tag, err := db.Exec(ctx, `
		DELETE FROM scanner_metadata_continuations
		WHERE $1::bigint = 0 OR library_id = $1
	`, libraryID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteMetadataContinuationsByKind mirrors the scoped River Jobs-page flush.
// Parked continuations are scheduled work, so completed-only flushes leave
// them intact.
func DeleteMetadataContinuationsByKind(ctx context.Context, db queueops.DB, kind, state string) (int64, error) {
	if kind != "search_metadata" && kind != "fetch_metadata" {
		return 0, nil
	}
	if state != "" && state != "available" && state != "pending" && state != "retryable" && state != "scheduled" {
		return 0, nil
	}
	tag, err := db.Exec(ctx, `DELETE FROM scanner_metadata_continuations WHERE kind = $1`, kind)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func metadataPollQueues() []string {
	queues := []string{"search_metadata_poll", "fetch_metadata_poll"}
	for _, mediaType := range scannerQueueMediaTypes {
		queues = append(queues,
			scannerQueueName("search_metadata_poll", mediaType),
			scannerQueueName("fetch_metadata_poll", mediaType),
		)
	}
	return queues
}

// adoptLegacyPollJobs drains old scheduled poll rows in small transactions.
// The state+queue predicate follows River's dequeue index, avoiding a repeated
// JSON scan over the whole hot table during the rolling cutover.
func (w *MetadataContinuationSweepWorker) adoptLegacyPollJobs(ctx context.Context) (int, error) {
	var adopted int
	err := w.DB.QueryRow(ctx, `
		WITH legacy AS MATERIALIZED (
			SELECT id, kind, args, priority, metadata, scheduled_at
			FROM river_job
			WHERE state IN ('available', 'pending', 'retryable', 'scheduled')
			  AND queue = ANY($1::text[])
			  AND kind IN ('search_metadata', 'fetch_metadata')
			  AND COALESCE(args->>'poll', 'false') = 'true'
			  AND COALESCE(args->>'library_id', '') ~ '^[0-9]+$'
			  AND COALESCE(args->>'scanner_entity_id', '') ~ '^[0-9]+$'
			  AND CASE kind
			      WHEN 'search_metadata' THEN COALESCE(args->>'analysis_artifact_id', '') ~ '^[0-9]+$'
			      WHEN 'fetch_metadata' THEN COALESCE(args->>'search_artifact_id', '') ~ '^[0-9]+$'
			      ELSE false
			  END
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		), parked AS (
			INSERT INTO scanner_metadata_continuations
				(kind, library_id, scanner_entity_id, artifact_id, args, priority, source, scheduled_task_id, next_attempt_at)
			SELECT kind,
			       (args->>'library_id')::bigint,
			       (args->>'scanner_entity_id')::bigint,
			       CASE kind
			           WHEN 'search_metadata' THEN (args->>'analysis_artifact_id')::bigint
			           ELSE (args->>'search_artifact_id')::bigint
			       END,
			       args, priority, COALESCE(metadata->>'source', ''), COALESCE(args->>'scheduled_task_id', ''), GREATEST(scheduled_at, now())
			FROM legacy
			ON CONFLICT (kind, scanner_entity_id, artifact_id) DO UPDATE
			SET args = EXCLUDED.args,
			    priority = EXCLUDED.priority,
			    source = EXCLUDED.source,
			    scheduled_task_id = EXCLUDED.scheduled_task_id,
			    next_attempt_at = LEAST(scanner_metadata_continuations.next_attempt_at, EXCLUDED.next_attempt_at),
			    updated_at = now()
			RETURNING 1
		), deleted AS (
			DELETE FROM river_job job
			USING legacy
			WHERE job.id = legacy.id
			RETURNING 1
		)
		SELECT count(*)::int FROM deleted
	`, metadataPollQueues(), metadataContinuationAdoptBatch).Scan(&adopted)
	return adopted, err
}

func (w *MetadataContinuationSweepWorker) claimDue(ctx context.Context) ([]metadataContinuationRow, error) {
	rows, err := w.DB.Query(ctx, `
		WITH due AS (
			SELECT id
			FROM scanner_metadata_continuations
			WHERE next_attempt_at <= now()
			ORDER BY next_attempt_at, id
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		UPDATE scanner_metadata_continuations continuation
		SET next_attempt_at = $2,
		    updated_at = now()
		FROM due
		WHERE continuation.id = due.id
		RETURNING continuation.id, continuation.kind, continuation.args, continuation.priority, continuation.source
	`, metadataContinuationBatch, time.Now().Add(metadataContinuationLease))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	claimed := make([]metadataContinuationRow, 0, metadataContinuationBatch)
	for rows.Next() {
		var row metadataContinuationRow
		if err := rows.Scan(&row.ID, &row.Kind, &row.ArgsJSON, &row.Priority, &row.Source); err != nil {
			return nil, err
		}
		claimed = append(claimed, row)
	}
	return claimed, rows.Err()
}

func (w *MetadataContinuationSweepWorker) retrySoon(ctx context.Context, id int64) {
	_, _ = w.DB.Exec(ctx, `
		UPDATE scanner_metadata_continuations
		SET next_attempt_at = now() + interval '10 seconds', updated_at = now()
		WHERE id = $1
	`, id)
}

func (w *MetadataContinuationSweepWorker) dropInvalid(ctx context.Context, row metadataContinuationRow, err error) {
	_, _ = w.DB.Exec(ctx, `DELETE FROM scanner_metadata_continuations WHERE id = $1`, row.ID)
	log.Error().Err(err).Int64("continuation_id", row.ID).Str("kind", row.Kind).Msg("metadata continuation had invalid arguments; dropped")
}

func (w *MetadataContinuationSweepWorker) Work(ctx context.Context, _ *river.Job[MetadataContinuationSweepArgs]) error {
	rc := river.ClientFromContext[pgx.Tx](ctx)
	if rc == nil {
		return fmt.Errorf("metadata_continuation_sweep: no river client in context")
	}

	adopted, err := w.adoptLegacyPollJobs(ctx)
	if err != nil {
		return fmt.Errorf("adopt legacy metadata polls: %w", err)
	}
	claimed, err := w.claimDue(ctx)
	if err != nil {
		return fmt.Errorf("claim metadata continuations: %w", err)
	}

	enqueued := 0
	for _, row := range claimed {
		var args river.JobArgs
		var opts river.InsertOpts
		switch row.Kind {
		case "search_metadata":
			var searchArgs SearchLibraryMetadataArgs
			if err := json.Unmarshal(row.ArgsJSON, &searchArgs); err != nil {
				w.dropInvalid(ctx, row, err)
				continue
			}
			searchArgs.Poll = true
			args = searchArgs
			opts = searchArgs.InsertOpts()
		case "fetch_metadata":
			var fetchArgs FetchLibraryMetadataArgs
			if err := json.Unmarshal(row.ArgsJSON, &fetchArgs); err != nil {
				w.dropInvalid(ctx, row, err)
				continue
			}
			fetchArgs.Poll = true
			args = fetchArgs
			opts = fetchArgs.InsertOpts()
		default:
			w.dropInvalid(ctx, row, fmt.Errorf("unknown kind %q", row.Kind))
			continue
		}

		opts.Priority = row.Priority
		if _, err := rc.Insert(ctx, args, applyScheduledJobSource(opts, row.Source)); err != nil {
			w.retrySoon(ctx, row.ID)
			log.Warn().Err(err).Int64("continuation_id", row.ID).Str("kind", row.Kind).Msg("enqueue metadata continuation failed")
			continue
		}
		enqueued++
	}

	if adopted > 0 || enqueued > 0 {
		log.Info().Int("adopted", adopted).Int("enqueued", enqueued).Int("claimed", len(claimed)).Msg("metadata_continuation_sweep")
	}
	return nil
}
