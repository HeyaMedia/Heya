package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

const (
	metadataContinuationBatch      = 100
	metadataContinuationAdoptBatch = 10_000
	metadataContinuationLease      = 5 * time.Minute
	metadataSearchRetryMinimum     = time.Minute
	metadataSearchRetryStep        = 10 * time.Second
	metadataSearchRetryMaximum     = 5 * time.Minute
	metadataSearchReconcileMinimum = 30 * time.Minute
	metadataSearchReconcileSpread  = 10 * time.Minute
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
	DB      *pgxpool.Pool
	Backoff *metadataContinuationBackoff
}

// metadataContinuationBackoff is shared by the search workers and the
// continuation sweeper in one River runtime. The sweeper refreshes the compact
// waiting population once per tick; search workers use that cached count to
// avoid a COUNT query for every deferred item.
type metadataContinuationBackoff struct {
	mu            sync.RWMutex
	searchWaiting map[sqlc.MediaType]int64
	lastRefresh   atomic.Int64
}

func newMetadataContinuationBackoff() *metadataContinuationBackoff {
	return &metadataContinuationBackoff{searchWaiting: make(map[sqlc.MediaType]int64)}
}

func (b *metadataContinuationBackoff) refresh(ctx context.Context, db queueops.DB) error {
	if b == nil {
		return nil
	}
	now := time.Now().UnixNano()
	last := b.lastRefresh.Load()
	if last > 0 && time.Duration(now-last) < time.Minute {
		return nil
	}
	if !b.lastRefresh.CompareAndSwap(last, now) {
		return nil
	}
	rows, err := db.Query(ctx, `
		SELECT library.media_type, count(*)
		FROM scanner_metadata_continuations continuation
		JOIN libraries library ON library.id = continuation.library_id
		WHERE continuation.kind = 'search_metadata' AND continuation.workflow_id IS NULL
		GROUP BY library.media_type
	`)
	if err != nil {
		b.lastRefresh.Store(last)
		return err
	}
	defer rows.Close()
	waiting := make(map[sqlc.MediaType]int64)
	for rows.Next() {
		var mediaType sqlc.MediaType
		var count int64
		if err := rows.Scan(&mediaType, &count); err != nil {
			b.lastRefresh.Store(last)
			return err
		}
		waiting[mediaType] = count
	}
	if err := rows.Err(); err != nil {
		b.lastRefresh.Store(last)
		return err
	}
	b.mu.Lock()
	b.searchWaiting = waiting
	b.mu.Unlock()
	return nil
}

func (b *metadataContinuationBackoff) searchRetryAfter(mediaType sqlc.MediaType, providerDelay time.Duration) (time.Duration, int64) {
	waiting := int64(0)
	if b != nil {
		b.mu.RLock()
		waiting = max(b.searchWaiting[mediaType], 0)
		b.mu.RUnlock()
	}
	adaptive := metadataSearchRetryMinimum + time.Duration(waiting/100)*metadataSearchRetryStep
	if adaptive > metadataSearchRetryMaximum {
		adaptive = metadataSearchRetryMaximum
	}
	if providerDelay > adaptive {
		adaptive = providerDelay
	}
	return adaptive, waiting
}

// metadataSearchReconcileAfter is the slow correctness backstop for a
// discovery that should normally be woken by the workflow event feed. Stable
// jitter keeps a bulk scan from making every fallback due in the same sweep.
func metadataSearchReconcileAfter(workflowID string, providerDelay time.Duration) time.Duration {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(workflowID))
	spreadSeconds := uint32(metadataSearchReconcileSpread / time.Second)
	delay := metadataSearchReconcileMinimum
	if spreadSeconds > 0 {
		delay += time.Duration(hash.Sum32()%spreadSeconds) * time.Second
	}
	if providerDelay > delay {
		return providerDelay
	}
	return delay
}

type metadataContinuationWorkflow struct {
	Kind string
	ID   string
}

type metadataContinuationRow struct {
	ID       int64
	Kind     string
	ArgsJSON []byte
	Priority int
	Source   string
}

func parkMetadataContinuation(ctx context.Context, db queueops.DB, kind string, libraryID, scannerEntityID, artifactID int64, args any, priority int, source string, retryAfter time.Duration, workflow metadataContinuationWorkflow) error {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("marshal metadata continuation: %w", err)
	}
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	workflow.Kind = strings.TrimSpace(workflow.Kind)
	workflow.ID = strings.TrimSpace(workflow.ID)
	var workflowUUID pgtype.UUID
	if workflow.ID != "" {
		parsed, parseErr := uuid.Parse(workflow.ID)
		if parseErr != nil {
			return fmt.Errorf("park metadata continuation: invalid workflow ID %q: %w", workflow.ID, parseErr)
		}
		if workflow.Kind == "" {
			return fmt.Errorf("park metadata continuation: workflow kind is required with ID %s", workflow.ID)
		}
		workflowUUID = pgtype.UUID{Bytes: [16]byte(parsed), Valid: true}
	} else {
		workflow.Kind = ""
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
			(kind, library_id, scanner_entity_id, artifact_id, args, priority, source, scheduled_task_id,
			 next_attempt_at, workflow_kind, workflow_id, workflow_event_sequence)
		VALUES (
			$1, $2, $3, $4, $5::jsonb, $6, $7, $8,
			CASE WHEN COALESCE((
				SELECT sequence FROM metadata_workflow_event_inbox
				WHERE workflow_kind = $10 AND workflow_id = $11
			), 0) > 0 THEN now() ELSE $9 END,
			$10, $11,
			COALESCE((
				SELECT sequence FROM metadata_workflow_event_inbox
				WHERE workflow_kind = $10 AND workflow_id = $11
			), 0)
		)
		ON CONFLICT (kind, scanner_entity_id, artifact_id) DO UPDATE
		SET library_id = EXCLUDED.library_id,
		    args = EXCLUDED.args,
		    priority = EXCLUDED.priority,
		    source = EXCLUDED.source,
		    scheduled_task_id = EXCLUDED.scheduled_task_id,
		    next_attempt_at = CASE
		        WHEN scanner_metadata_continuations.workflow_kind = EXCLUDED.workflow_kind
		         AND scanner_metadata_continuations.workflow_id IS NOT DISTINCT FROM EXCLUDED.workflow_id
		         AND EXCLUDED.workflow_event_sequence > scanner_metadata_continuations.workflow_event_sequence
		        THEN LEAST($9, now())
		        WHEN scanner_metadata_continuations.workflow_kind <> EXCLUDED.workflow_kind
		          OR scanner_metadata_continuations.workflow_id IS DISTINCT FROM EXCLUDED.workflow_id
		        THEN EXCLUDED.next_attempt_at
		        ELSE $9
		    END,
		    workflow_event_sequence = CASE
		        WHEN scanner_metadata_continuations.workflow_kind = EXCLUDED.workflow_kind
		         AND scanner_metadata_continuations.workflow_id IS NOT DISTINCT FROM EXCLUDED.workflow_id
		        THEN GREATEST(scanner_metadata_continuations.workflow_event_sequence, EXCLUDED.workflow_event_sequence)
		        ELSE EXCLUDED.workflow_event_sequence
		    END,
		    workflow_kind = EXCLUDED.workflow_kind,
		    workflow_id = EXCLUDED.workflow_id,
		    updated_at = now()
	`, kind, libraryID, scannerEntityID, artifactID, argsJSON, priority, source, scheduledTaskID, time.Now().Add(retryAfter), workflow.Kind, workflowUUID)
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
		WITH ranked AS MATERIALIZED (
			SELECT continuation.id,
			       continuation.next_attempt_at,
			       row_number() OVER (
			           PARTITION BY library.media_type
			           ORDER BY continuation.next_attempt_at, continuation.id
			       ) AS media_rank
			FROM scanner_metadata_continuations continuation
			JOIN libraries library ON library.id = continuation.library_id
			WHERE continuation.next_attempt_at <= now()
		), due AS (
			SELECT continuation.id
			FROM scanner_metadata_continuations continuation
			JOIN ranked ON ranked.id = continuation.id
			WHERE ranked.media_rank <= $1
			ORDER BY ranked.next_attempt_at, continuation.id
			FOR UPDATE OF continuation SKIP LOCKED
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
	if err := w.Backoff.refresh(ctx, w.DB); err != nil {
		log.Debug().Err(err).Msg("metadata continuation backoff count unavailable")
	}
	claimed, err := w.claimDue(ctx)
	if err != nil {
		return fmt.Errorf("claim metadata continuations: %w", err)
	}

	inserts := make([]river.InsertManyParams, 0, len(claimed))
	insertRows := make([]metadataContinuationRow, 0, len(claimed))
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
		inserts = append(inserts, river.InsertManyParams{Args: args, InsertOpts: applyScheduledJobSource(opts, row.Source)})
		insertRows = append(insertRows, row)
	}

	enqueued := 0
	if len(inserts) > 0 {
		results, insertErr := rc.InsertMany(ctx, inserts)
		if insertErr != nil {
			for _, row := range insertRows {
				w.retrySoon(ctx, row.ID)
			}
			return fmt.Errorf("enqueue metadata continuation batch: %w", insertErr)
		}
		for _, result := range results {
			if !result.UniqueSkippedAsDuplicate {
				enqueued++
			}
		}
	}

	if adopted > 0 || enqueued > 0 {
		log.Debug().Int("adopted", adopted).Int("enqueued", enqueued).Int("claimed", len(claimed)).Msg("metadata_continuation_sweep")
	}
	return nil
}
