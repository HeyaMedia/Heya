package queueops

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func queueopsTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping DB integration test in short mode")
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://heya:heya@localhost:5440/heya?sslmode=disable"
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("database not available: %v", err)
	}
	return pool
}

// TestKickoffFinishingHandshake pins the serialization between a pump's
// completion and a concurrent Run-Now upgrade: an upgrade that lands first
// is visible to ClaimKickoffFinish (the pump continues as manual); one
// that arrives after the claim is rejected, so TriggerNow starts a fresh
// manual run instead of the click being swallowed by the completing pump.
func TestKickoffFinishingHandshake(t *testing.T) {
	pool := queueopsTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	const kind = "kickoff_music_loudness"
	const taskID = "scan_music_loudness"
	var jobID int64
	require.NoError(t, tx.QueryRow(ctx, `
		INSERT INTO river_job (state, max_attempts, kind, queue, args, metadata)
		VALUES ('scheduled', 1, $1, $1, $2::jsonb, '{}'::jsonb)
		RETURNING id
	`, kind, `{"scheduled_task_id": "`+taskID+`"}`).Scan(&jobID))

	// Upgrade before any claim: matches and flips the source.
	n, err := MarkActiveKickoffManual(ctx, tx, kind, taskID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	source, active, err := ActiveKickoffSource(ctx, tx, kind, taskID)
	require.NoError(t, err)
	assert.True(t, active)
	assert.Equal(t, KickoffSourceManual, source)

	// The claim returns the live source — an already-landed upgrade is
	// visible, so the pump aborts its wind-down.
	source, err = ClaimKickoffFinish(ctx, tx, jobID)
	require.NoError(t, err)
	assert.Equal(t, KickoffSourceManual, source)

	// After the claim, upgrades are rejected: the pump is committed to
	// completing and would never re-read the source.
	n, err = MarkActiveKickoffManual(ctx, tx, kind, taskID)
	require.NoError(t, err)
	assert.Zero(t, n)

	// A continued run's state patch clears the claim (patch always writes
	// finishing=false), making the row upgradable again.
	require.NoError(t, MergeJobMetadata(ctx, tx, jobID, []byte(`{"finishing": false}`)))
	n, err = MarkActiveKickoffManual(ctx, tx, kind, taskID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	// GetActiveKickoff snapshots the row for cancel bookkeeping.
	run, err := GetActiveKickoff(ctx, tx, kind, taskID)
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, jobID, run.JobID)
	assert.Contains(t, string(run.Metadata), `"manual"`)

	// A finalized row is invisible to all of it.
	_, err = tx.Exec(ctx, `UPDATE river_job SET state = 'completed', finalized_at = now() WHERE id = $1`, jobID)
	require.NoError(t, err)
	_, active, err = ActiveKickoffSource(ctx, tx, kind, taskID)
	require.NoError(t, err)
	assert.False(t, active)
	n, err = MarkActiveKickoffManual(ctx, tx, kind, taskID)
	require.NoError(t, err)
	assert.Zero(t, n)
	run, err = GetActiveKickoff(ctx, tx, kind, taskID)
	require.NoError(t, err)
	assert.Nil(t, run)
}

func TestScheduledTaskExceededRuntimeIgnoresManualJobs(t *testing.T) {
	pool := queueopsTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	const taskID = "scan_libraries"
	const kickoffKind = "kickoff_library_scan"
	const childKind = "process_library_scan"
	old := `now() - interval '2 hours'`

	var kickoffID, childID int64
	require.NoError(t, tx.QueryRow(ctx, `
		INSERT INTO river_job (state, max_attempts, kind, queue, args, metadata, created_at)
		VALUES ('scheduled', 1, $1, $1, $2::jsonb, '{}'::jsonb, `+old+`)
		RETURNING id
	`, kickoffKind, `{"scheduled_task_id": "`+taskID+`"}`).Scan(&kickoffID))
	require.NoError(t, tx.QueryRow(ctx, `
		INSERT INTO river_job (state, max_attempts, kind, queue, args, metadata, created_at)
		VALUES ('available', 1, $1, $1, $2::jsonb, '{}'::jsonb, `+old+`)
		RETURNING id
	`, childKind, `{"scheduled_task_id": "`+taskID+`"}`).Scan(&childID))

	exceeded, err := ScheduledTaskExceededRuntime(ctx, tx, taskID, []string{kickoffKind, childKind}, 30)
	require.NoError(t, err)
	assert.True(t, exceeded)

	n, err := MarkActiveKickoffManual(ctx, tx, kickoffKind, taskID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	var childSource string
	require.NoError(t, tx.QueryRow(ctx, `SELECT COALESCE(metadata->>'source', '') FROM river_job WHERE id = $1`, childID).Scan(&childSource))
	assert.Equal(t, KickoffSourceManual, childSource)

	exceeded, err = ScheduledTaskExceededRuntime(ctx, tx, taskID, []string{kickoffKind, childKind}, 30)
	require.NoError(t, err)
	assert.False(t, exceeded)

	_ = kickoffID
}
