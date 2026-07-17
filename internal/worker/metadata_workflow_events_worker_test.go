package worker

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/database/sqlc"
	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestMetadataWorkflowEventsWakeParkedDiscoveryIdempotently(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	q := sqlc.New(pool)
	lib, err := q.CreateLibrary(ctx, sqlc.CreateLibraryParams{
		Name:         "workflow-event-wake-" + uuid.NewString(),
		MediaType:    sqlc.MediaTypeMovie,
		Paths:        []string{"/media/workflow-event-wake"},
		ScanInterval: pgtype.Interval{Microseconds: 3600000000, Valid: true},
		CreatedBy:    testutil.TestUserID(t, pool),
		Settings:     []byte("{}"),
	})
	require.NoError(t, err)
	t.Cleanup(func() { testutil.CleanupLibrary(t, pool, lib.ID) })

	var scannerEntityID, artifactID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO scanner_entities (library_id, media_type, identity_key, title)
		VALUES ($1, $2, $3, 'event wake') RETURNING id
	`, lib.ID, lib.MediaType, uuid.NewString()).Scan(&scannerEntityID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO scanner_entity_artifacts (entity_id, stage)
		VALUES ($1, 'analysis') RETURNING id
	`, scannerEntityID).Scan(&artifactID))

	workflowID := uuid.New()
	requestKey := "workflow-event-test-" + uuid.NewString()
	_, err = pool.Exec(ctx, `
		INSERT INTO metadata_resolution_workflows (request_key, kind, state, discovery_id)
		VALUES ($1, 'movie', 'discovering', $2)
	`, requestKey, workflowID)
	require.NoError(t, err)
	consumer := "workflow-event-test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM metadata_workflow_event_inbox WHERE workflow_id = $1`, workflowID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM metadata_workflow_event_consumers WHERE consumer = $1`, consumer)
		_, _ = pool.Exec(context.Background(), `DELETE FROM metadata_resolution_workflows WHERE request_key = $1`, requestKey)
	})

	streamID := uuid.NewString()
	completedAt := time.Now().UTC().Truncate(time.Microsecond)
	page := heyametadata.WorkflowEventPage{
		StreamID: streamID, HeadCursor: 1, NextCursor: 1,
		Events: []heyametadata.WorkflowEvent{{
			Sequence: 1, Kind: "discovery", ID: workflowID.String(),
			State: "completed", CompletedAt: completedAt,
		}},
	}

	// The feed may win the race and commit before the scanner parks its row.
	recognized, woken, err := applyMetadataWorkflowEventPage(ctx, pool, consumer, page)
	require.NoError(t, err)
	require.Equal(t, 1, recognized)
	require.Zero(t, woken)

	args := SearchLibraryMetadataArgs{
		LibraryID: lib.ID, MediaType: lib.MediaType,
		ScannerEntityID: scannerEntityID, AnalysisArtifactID: artifactID, Poll: true,
	}
	require.NoError(t, parkMetadataContinuation(
		ctx, pool, args.Kind(), lib.ID, scannerEntityID, artifactID, args,
		PriorityScan, "", 30*time.Minute,
		metadataContinuationWorkflow{Kind: "discovery", ID: workflowID.String()},
	))
	var nextAttempt time.Time
	var sequence int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT next_attempt_at, workflow_event_sequence
		FROM scanner_metadata_continuations
		WHERE kind = 'search_metadata' AND scanner_entity_id = $1 AND artifact_id = $2
	`, scannerEntityID, artifactID).Scan(&nextAttempt, &sequence))
	require.EqualValues(t, 1, sequence)
	require.WithinDuration(t, time.Now(), nextAttempt, 5*time.Second)

	// Re-parking the same generation consumes no old event again, and replaying
	// the same sequence is a complete no-op.
	require.NoError(t, parkMetadataContinuation(
		ctx, pool, args.Kind(), lib.ID, scannerEntityID, artifactID, args,
		PriorityScan, "", 30*time.Minute,
		metadataContinuationWorkflow{Kind: "discovery", ID: workflowID.String()},
	))
	recognized, woken, err = applyMetadataWorkflowEventPage(ctx, pool, consumer, page)
	require.NoError(t, err)
	require.Zero(t, recognized)
	require.Zero(t, woken)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT next_attempt_at FROM scanner_metadata_continuations
		WHERE kind = 'search_metadata' AND scanner_entity_id = $1 AND artifact_id = $2
	`, scannerEntityID, artifactID).Scan(&nextAttempt))
	require.True(t, nextAttempt.After(time.Now().Add(25*time.Minute)))

	// A later completion for the same re-run ID carries a new sequence and
	// wakes the row exactly once.
	page.HeadCursor = 2
	page.NextCursor = 2
	page.Events[0].Sequence = 2
	recognized, woken, err = applyMetadataWorkflowEventPage(ctx, pool, consumer, page)
	require.NoError(t, err)
	require.Equal(t, 1, recognized)
	require.EqualValues(t, 1, woken)
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT next_attempt_at, workflow_event_sequence
		FROM scanner_metadata_continuations
		WHERE kind = 'search_metadata' AND scanner_entity_id = $1 AND artifact_id = $2
	`, scannerEntityID, artifactID).Scan(&nextAttempt, &sequence))
	require.EqualValues(t, 2, sequence)
	require.WithinDuration(t, time.Now(), nextAttempt, 5*time.Second)

	// Global events for IDs this server never parked advance the cursor but do
	// not enter the local inbox or wake anything.
	page.HeadCursor = 3
	page.NextCursor = 3
	page.Events = []heyametadata.WorkflowEvent{{
		Sequence: 3, Kind: "discovery", ID: uuid.NewString(),
		State: "failed", CompletedAt: completedAt,
	}}
	recognized, woken, err = applyMetadataWorkflowEventPage(ctx, pool, consumer, page)
	require.NoError(t, err)
	require.Zero(t, recognized)
	require.Zero(t, woken)

	var cursor int64
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT next_cursor FROM metadata_workflow_event_consumers WHERE consumer = $1
	`, consumer).Scan(&cursor))
	require.EqualValues(t, 3, cursor)

	// A 409 reset clears stream-scoped inbox/sequence state and adopts the new
	// identity atomically. Run inside a rolled-back transaction so the shared
	// integration database keeps any unrelated local state.
	resetTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = resetTx.Rollback(context.Background()) })
	newStream, err := metadataChangeStreamUUID(uuid.NewString())
	require.NoError(t, err)
	require.NoError(t, resetMetadataWorkflowEventStreamTx(ctx, resetTx, consumer, newStream))
	var resetCursor int64
	var resetStream pgtype.UUID
	require.NoError(t, resetTx.QueryRow(ctx, `
		SELECT next_cursor, stream_id
		FROM metadata_workflow_event_consumers WHERE consumer = $1
	`, consumer).Scan(&resetCursor, &resetStream))
	require.Zero(t, resetCursor)
	require.Equal(t, metadataChangeStreamString(newStream), metadataChangeStreamString(resetStream))
	var inboxRows int
	require.NoError(t, resetTx.QueryRow(ctx, `SELECT count(*) FROM metadata_workflow_event_inbox`).Scan(&inboxRows))
	require.Zero(t, inboxRows)
	require.NoError(t, resetTx.QueryRow(ctx, `
		SELECT workflow_event_sequence
		FROM scanner_metadata_continuations
		WHERE kind = 'search_metadata' AND scanner_entity_id = $1 AND artifact_id = $2
	`, scannerEntityID, artifactID).Scan(&sequence))
	require.Zero(t, sequence)
}
