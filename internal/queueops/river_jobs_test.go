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

func TestMetadataChangeEnrichCoalescingKeepsOneTrailingRefresh(t *testing.T) {
	pool := queueopsTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	insert := func(itemID int64, source, state string, ageSeconds int) int64 {
		t.Helper()
		var id int64
		require.NoError(t, tx.QueryRow(ctx, `
			INSERT INTO river_job (state, max_attempts, kind, queue, args, metadata, created_at)
			VALUES ($3, 3, 'enrich_media_item', 'enrich_media_item',
			        jsonb_build_object('item_id', $1::bigint, 'source', $2::text, 'force', true),
			        '{}'::jsonb, now() + ($4::int * interval '1 second'))
			RETURNING id
		`, itemID, source, state, ageSeconds).Scan(&id))
		return id
	}

	// Item 41 has a running refresh plus three queued refreshes. Coalescing
	// must retain the running job and only the newest queued trailing job.
	insert(41, "metadata_change", "available", -30)
	insert(41, "metadata_change", "scheduled", -20)
	insert(41, "metadata_change", "retryable", -10)
	insert(41, "metadata_change", "running", 0)
	// A foreground refresh for the same item is a separate priority path and
	// must never be cancelled by metadata-change maintenance.
	insert(41, "view", "available", 1)

	// Item 42 has no running refresh, so one of its two queued rows survives.
	insert(42, "metadata_change", "available", -20)
	insert(42, "metadata_change", "available", -10)

	cancelled, err := CoalesceMetadataChangeEnrichJobs(ctx, tx)
	require.NoError(t, err)
	assert.EqualValues(t, 3, cancelled)

	var pending41, running41, cancelled41, foreground41 int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE args->>'source' = 'metadata_change' AND state IN ('available','pending','retryable','scheduled')),
			count(*) FILTER (WHERE args->>'source' = 'metadata_change' AND state = 'running'),
			count(*) FILTER (WHERE args->>'source' = 'metadata_change' AND state = 'cancelled'),
			count(*) FILTER (WHERE args->>'source' = 'view' AND state = 'available')
		FROM river_job
		WHERE kind = 'enrich_media_item' AND NULLIF(args->>'item_id', '')::bigint = 41
	`).Scan(&pending41, &running41, &cancelled41, &foreground41))
	assert.Equal(t, 1, pending41)
	assert.Equal(t, 1, running41)
	assert.Equal(t, 2, cancelled41)
	assert.Equal(t, 1, foreground41)

	// A newer invalidation atomically replaces the retained trailing row. The
	// currently-running metadata refresh and foreground work remain untouched.
	cancelled, err = CancelPendingMetadataChangeEnrichJobs(ctx, tx, []int64{41})
	require.NoError(t, err)
	assert.EqualValues(t, 1, cancelled)

	require.NoError(t, tx.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE args->>'source' = 'metadata_change' AND state IN ('available','pending','retryable','scheduled')),
			count(*) FILTER (WHERE args->>'source' = 'metadata_change' AND state = 'running'),
			count(*) FILTER (WHERE args->>'source' = 'view' AND state = 'available')
		FROM river_job
		WHERE kind = 'enrich_media_item' AND NULLIF(args->>'item_id', '')::bigint = 41
	`).Scan(&pending41, &running41, &foreground41))
	assert.Zero(t, pending41)
	assert.Equal(t, 1, running41)
	assert.Equal(t, 1, foreground41)

	var pending42 int
	require.NoError(t, tx.QueryRow(ctx, `
		SELECT count(*) FROM river_job
		WHERE kind = 'enrich_media_item'
		  AND args->>'source' = 'metadata_change'
		  AND NULLIF(args->>'item_id', '')::bigint = 42
		  AND state IN ('available','pending','retryable','scheduled')
	`).Scan(&pending42))
	assert.Equal(t, 1, pending42, "replacing item 41 must not disturb another item's trailing refresh")
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
	const childKind = "process_scan"
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

// TestCountLiveByKindAndTaskMatchesPerDefinitionHelpers pins the grouped
// single-pass count to the per-definition helpers it replaced in the hot
// paths: folding by (kinds, taskID) must yield exactly what
// CountScheduledTask/CountByKinds report, including the splits the old
// helpers encoded — 'pending' state is not counted, and jobs missing a
// scheduled_task_id only count for synthetic (taskID == "") folds.
func TestCountLiveByKindAndTaskMatchesPerDefinitionHelpers(t *testing.T) {
	pool := queueopsTestPool(t)
	defer pool.Close()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	const taskID = "grpcount_task"
	const kindA = "grpcount_child_a"
	const kindB = "grpcount_child_b"
	taskArgs := `{"scheduled_task_id": "` + taskID + `"}`

	insert := func(kind, state, args string) {
		_, err := tx.Exec(ctx, `
			INSERT INTO river_job (state, max_attempts, kind, queue, args, metadata)
			VALUES ($1, 1, $2, $2, $3::jsonb, '{}'::jsonb)
		`, state, kind, args)
		require.NoError(t, err)
	}
	insert(kindA, "available", taskArgs)
	insert(kindA, "available", taskArgs)
	insert(kindA, "running", taskArgs)
	insert(kindA, "available", `{}`) // no owner: synthetic folds only
	insert(kindB, "scheduled", taskArgs)
	insert(kindA, "pending", taskArgs) // never counted

	live, err := CountLiveByKindAndTask(ctx, tx)
	require.NoError(t, err)

	scheduled := RuntimeCountsFor(live, []string{kindA, kindB}, taskID)
	assert.Equal(t, RuntimeCounts{Pending: 3, Running: 1}, scheduled)
	fromHelper, err := CountScheduledTask(ctx, tx, taskID, []string{kindA, kindB})
	require.NoError(t, err)
	assert.Equal(t, fromHelper, scheduled)

	synthetic := RuntimeCountsFor(live, []string{kindA}, "")
	assert.Equal(t, RuntimeCounts{Pending: 3, Running: 1}, synthetic)
	fromHelper, err = CountByKinds(ctx, tx, []string{kindA})
	require.NoError(t, err)
	assert.Equal(t, fromHelper, synthetic)

	assert.Equal(t, RuntimeCounts{Pending: 1}, RuntimeCountsFor(live, []string{kindB}, ""))
	assert.Equal(t, RuntimeCounts{}, RuntimeCountsFor(live, []string{"grpcount_absent"}, taskID))
}
