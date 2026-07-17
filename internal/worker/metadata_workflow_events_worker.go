package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

const (
	metadataWorkflowEventConsumer = "heya-scanner"
	metadataWorkflowEventPageSize = int64(500)
	metadataWorkflowEventMaxPages = 10
)

type metadataWorkflowEventSource interface {
	WorkflowEvents(context.Context, int64, int64, string) (heyametadata.WorkflowEventPage, error)
}

// SyncMetadataWorkflowEventsWorker consumes the global completion feed and
// wakes only discovery IDs known to this Heya instance. No River work is
// inserted here: the existing bounded continuation sweep meters ready checks
// onto the per-media-type queues.
type SyncMetadataWorkflowEventsWorker struct {
	river.WorkerDefaults[SyncMetadataWorkflowEventsArgs]
	DB     *pgxpool.Pool
	Source metadataWorkflowEventSource
}

func (w *SyncMetadataWorkflowEventsWorker) Work(ctx context.Context, _ *river.Job[SyncMetadataWorkflowEventsArgs]) error {
	if w.Source == nil {
		return fmt.Errorf("sync metadata workflow events: metadata client is required")
	}
	cursor, streamID, err := readMetadataWorkflowEventCursor(ctx, w.DB)
	if err != nil {
		return err
	}
	if streamID == "" && cursor != 0 {
		log.Warn().Int64("old_cursor", cursor).Msg("heyametadata workflow cursor has no stream identity; replaying from zero")
		cursor = 0
	}

	pages, eventCount, recognized, woken := 0, 0, 0, int64(0)
	for pages < metadataWorkflowEventMaxPages {
		page, pageErr := w.Source.WorkflowEvents(ctx, cursor, metadataWorkflowEventPageSize, streamID)
		if pageErr != nil {
			var conflict *heyametadata.WorkflowStreamConflict
			if !errors.As(pageErr, &conflict) {
				return pageErr
			}
			if err := resetMetadataWorkflowEventStream(ctx, w.DB, conflict.StreamID); err != nil {
				return err
			}
			log.Warn().
				Str("reason", conflict.Code).
				Str("stream_id", conflict.StreamID).
				Int64("old_cursor", cursor).
				Int64("head_cursor", conflict.HeadCursor).
				Msg("heyametadata workflow event stream reset; replaying from zero")
			cursor, streamID = 0, conflict.StreamID
			continue
		}
		if page.StreamID == "" {
			return fmt.Errorf("metadata workflow events response has no stream ID")
		}
		if _, err := uuid.Parse(page.StreamID); err != nil {
			return fmt.Errorf("metadata workflow events response has invalid stream ID %q: %w", page.StreamID, err)
		}
		if page.NextCursor > page.HeadCursor {
			return fmt.Errorf("metadata workflow events cursor %d exceeds reported head %d", page.NextCursor, page.HeadCursor)
		}
		if page.NextCursor < cursor {
			return fmt.Errorf("metadata workflow events cursor regressed from %d to %d", cursor, page.NextCursor)
		}

		pageRecognized, pageWoken, err := applyMetadataWorkflowEventPage(ctx, w.DB, metadataWorkflowEventConsumer, page)
		if err != nil {
			return err
		}
		pages++
		eventCount += len(page.Events)
		recognized += pageRecognized
		woken += pageWoken
		streamID = page.StreamID
		if page.NextCursor == cursor || page.NextCursor >= page.HeadCursor {
			break
		}
		cursor = page.NextCursor
	}

	if _, err := w.DB.Exec(ctx, `
		DELETE FROM metadata_workflow_event_inbox inbox
		WHERE NOT EXISTS (
		        SELECT 1 FROM metadata_resolution_workflows workflow
		        WHERE workflow.discovery_id = inbox.workflow_id
		      )
		  AND NOT EXISTS (
		        SELECT 1 FROM scanner_metadata_continuations continuation
		        WHERE continuation.workflow_kind = inbox.workflow_kind
		          AND continuation.workflow_id = inbox.workflow_id
		      )
	`); err != nil {
		return fmt.Errorf("clean metadata workflow event inbox: %w", err)
	}
	if eventCount > 0 || woken > 0 {
		log.Info().Int("pages", pages).Int("events", eventCount).Int("recognized", recognized).Int64("woken", woken).Msg("heyametadata workflow event feed synchronized")
	}
	return nil
}

func readMetadataWorkflowEventCursor(ctx context.Context, db *pgxpool.Pool) (int64, string, error) {
	if _, err := db.Exec(ctx, `
		INSERT INTO metadata_workflow_event_consumers (consumer)
		VALUES ($1) ON CONFLICT (consumer) DO NOTHING
	`, metadataWorkflowEventConsumer); err != nil {
		return 0, "", fmt.Errorf("ensure metadata workflow event cursor: %w", err)
	}
	var cursor int64
	var stream pgtype.UUID
	if err := db.QueryRow(ctx, `
		SELECT next_cursor, stream_id
		FROM metadata_workflow_event_consumers
		WHERE consumer = $1
	`, metadataWorkflowEventConsumer).Scan(&cursor, &stream); err != nil {
		return 0, "", fmt.Errorf("read metadata workflow event cursor: %w", err)
	}
	return cursor, metadataChangeStreamString(stream), nil
}

func resetMetadataWorkflowEventStream(ctx context.Context, db *pgxpool.Pool, streamID string) error {
	parsed, err := metadataChangeStreamUUID(streamID)
	if err != nil {
		return fmt.Errorf("reset metadata workflow event cursor: %w", err)
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin metadata workflow event reset: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := resetMetadataWorkflowEventStreamTx(ctx, tx, metadataWorkflowEventConsumer, parsed); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit metadata workflow event reset: %w", err)
	}
	return nil
}

func resetMetadataWorkflowEventStreamTx(ctx context.Context, tx pgx.Tx, consumer string, stream pgtype.UUID) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO metadata_workflow_event_consumers (consumer, next_cursor, stream_id)
		VALUES ($1, 0, $2)
		ON CONFLICT (consumer) DO UPDATE
		SET next_cursor = 0, stream_id = EXCLUDED.stream_id, updated_at = now()
	`, consumer, stream); err != nil {
		return fmt.Errorf("reset metadata workflow event cursor: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM metadata_workflow_event_inbox`); err != nil {
		return fmt.Errorf("clear metadata workflow event inbox: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE scanner_metadata_continuations
		SET workflow_event_sequence = 0, updated_at = now()
		WHERE workflow_id IS NOT NULL
	`); err != nil {
		return fmt.Errorf("reset metadata continuation workflow sequences: %w", err)
	}
	return nil
}

type metadataWorkflowEventRecord struct {
	Sequence    int64     `json:"sequence"`
	Kind        string    `json:"kind"`
	ID          string    `json:"id"`
	State       string    `json:"state"`
	CompletedAt time.Time `json:"completed_at"`
}

func applyMetadataWorkflowEventPage(ctx context.Context, db *pgxpool.Pool, consumer string, page heyametadata.WorkflowEventPage) (int, int64, error) {
	stream, err := metadataChangeStreamUUID(page.StreamID)
	if err != nil {
		return 0, 0, fmt.Errorf("commit metadata workflow event cursor: %w", err)
	}
	records := make([]metadataWorkflowEventRecord, 0, len(page.Events))
	for _, event := range page.Events {
		if event.Kind != "discovery" {
			continue
		}
		if _, err := uuid.Parse(event.ID); err != nil {
			return 0, 0, fmt.Errorf("workflow event %d has invalid discovery ID %q: %w", event.Sequence, event.ID, err)
		}
		records = append(records, metadataWorkflowEventRecord{
			Sequence: event.Sequence, Kind: event.Kind, ID: event.ID,
			State: event.State, CompletedAt: event.CompletedAt,
		})
	}
	payload, err := json.Marshal(records)
	if err != nil {
		return 0, 0, fmt.Errorf("encode metadata workflow event page: %w", err)
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("begin metadata workflow event page: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var recognized int
	var woken int64
	if err := tx.QueryRow(ctx, `
		WITH incoming AS (
			SELECT sequence, kind, id, state, completed_at
			FROM jsonb_to_recordset($1::jsonb) AS event(
				sequence bigint, kind text, id uuid, state text, completed_at timestamptz
			)
		), relevant AS (
			SELECT event.*
			FROM incoming event
			WHERE EXISTS (
				SELECT 1 FROM metadata_resolution_workflows workflow
				WHERE workflow.discovery_id = event.id
			)
		), stored AS (
			INSERT INTO metadata_workflow_event_inbox (
				workflow_kind, workflow_id, sequence, state, completed_at
			)
			SELECT kind, id, sequence, state, completed_at FROM relevant
			ON CONFLICT (workflow_kind, workflow_id) DO UPDATE
			SET sequence = EXCLUDED.sequence,
			    state = EXCLUDED.state,
			    completed_at = EXCLUDED.completed_at,
			    updated_at = now()
			WHERE metadata_workflow_event_inbox.sequence < EXCLUDED.sequence
			RETURNING workflow_kind, workflow_id, sequence
		), wakes AS (
			UPDATE scanner_metadata_continuations continuation
			SET next_attempt_at = LEAST(continuation.next_attempt_at, now()),
			    workflow_event_sequence = stored.sequence,
			    updated_at = now()
			FROM stored
			WHERE continuation.workflow_kind = stored.workflow_kind
			  AND continuation.workflow_id = stored.workflow_id
			  AND continuation.workflow_event_sequence < stored.sequence
			RETURNING continuation.id
		), committed AS (
			INSERT INTO metadata_workflow_event_consumers (consumer, next_cursor, stream_id)
			VALUES ($2, $3, $4)
			ON CONFLICT (consumer) DO UPDATE
			SET next_cursor = CASE
			        WHEN metadata_workflow_event_consumers.stream_id IS NOT DISTINCT FROM EXCLUDED.stream_id
			        THEN GREATEST(metadata_workflow_event_consumers.next_cursor, EXCLUDED.next_cursor)
			        ELSE EXCLUDED.next_cursor
			    END,
			    stream_id = EXCLUDED.stream_id,
			    updated_at = now()
			RETURNING 1
		)
		SELECT (SELECT count(*)::int FROM stored),
		       (SELECT count(*)::bigint FROM wakes)
		FROM committed
	`, payload, consumer, page.NextCursor, stream).Scan(&recognized, &woken); err != nil {
		return 0, 0, fmt.Errorf("apply metadata workflow event page: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("commit metadata workflow event page: %w", err)
	}
	return recognized, woken, nil
}
