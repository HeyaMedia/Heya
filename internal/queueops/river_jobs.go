package queueops

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Queue-lifetime tunables, shared so the River client config and the manual
// rescue sweeps below can't drift apart.
const (
	// JobTimeout is the per-job context deadline River applies to every
	// Work(ctx) (wired into the river.Config in internal/worker). River's
	// own default is 1 minute, which silently killed long jobs — SMB library
	// scans, the 30-minute sonic model fetch, transcode/loudness/disk-walk —
	// with "context deadline exceeded". 6h is a generous ceiling no legitimate
	// single job should reach.
	JobTimeout = 6 * time.Hour

	// RescueStuckAfter is how long a job must sit in state='running' before
	// it's treated as genuinely stuck (worker crashed or wedged) rather than
	// merely slow. It MUST exceed JobTimeout: past its timeout a healthy job
	// has had its context cancelled, so anything still 'running' beyond this
	// window is dead. Anything shorter would rescue — and thus duplicate — a
	// job that's slow but still actively working. Used by both River's
	// automatic rescuer (RescueStuckJobsAfter) and the manual RescueStuckRunning
	// sweep so "stuck" means the same thing on-demand as on the periodic tick.
	RescueStuckAfter = JobTimeout + time.Hour
)

type DB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type RuntimeCounts struct {
	Pending int
	Running int
}

// KickoffSourceManual marks a kickoff run started by a user's "Run Now"
// (UI button or CLI) rather than the cron trigger loop. Stored under the
// "source" key of the kickoff job's metadata — metadata is not part of
// River's unique key, so a manual and a scheduled kickoff still coalesce.
// Manual runs are exempt from max-runtime enforcement: they drain the
// whole backlog no matter how long it takes.
const KickoffSourceManual = "manual"

// kickoffActiveStates matches worker.uniqueWhileActive — the states in
// which at most one kickoff per (kind, args) can exist.
const kickoffActiveStates = `('available', 'pending', 'running', 'retryable', 'scheduled')`

// ActiveKickoffSource returns the metadata source ("manual" or "") of the
// task's active kickoff job, plus whether one exists at all. Uniqueness
// guarantees at most one active kickoff per task, but ORDER BY id DESC
// keeps the answer deterministic even if that invariant is ever violated.
func ActiveKickoffSource(ctx context.Context, db DB, kickoffKind, taskID string) (source string, active bool, err error) {
	if kickoffKind == "" || taskID == "" {
		return "", false, nil
	}
	err = db.QueryRow(ctx, `
		SELECT COALESCE(metadata->>'source', '') FROM river_job
		WHERE kind = $1
		  AND args->>'scheduled_task_id' = $2
		  AND state IN `+kickoffActiveStates+`
		ORDER BY id DESC
		LIMIT 1
	`, kickoffKind, taskID).Scan(&source)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return source, true, nil
}

// MarkActiveKickoffManual upgrades the task's active kickoff run to
// manual. Used when a "Run Now" click coalesces with an already-active
// (cron-started) kickoff: the user asked for a full drain, so the run
// sheds its max-runtime window instead of silently no-oping.
//
// Rows that have claimed "finishing" (ClaimKickoffFinish) are excluded:
// their pump has already committed to completing and will never re-read
// the source, so an upgrade would be silently lost. Returning 0 instead
// tells TriggerNow to start a fresh manual run once the row completes.
func MarkActiveKickoffManual(ctx context.Context, db DB, kickoffKind, taskID string) (int64, error) {
	if kickoffKind == "" || taskID == "" {
		return 0, nil
	}
	tag, err := db.Exec(ctx, `
		UPDATE river_job
		   SET metadata = metadata || '{"source": "manual"}'::jsonb
		 WHERE kind = $1
		   AND args->>'scheduled_task_id' = $2
		   AND state IN `+kickoffActiveStates+`
		   AND NOT COALESCE((metadata->>'finishing')::boolean, false)
	`, kickoffKind, taskID)
	if err != nil {
		return 0, err
	}
	n := tag.RowsAffected()
	if n > 0 {
		if _, err := MarkScheduledTaskJobsManual(ctx, db, taskID); err != nil {
			return n, err
		}
	}
	return n, nil
}

// MarkScheduledTaskJobsManual marks every active job owned by the scheduled
// task as user-initiated. This is deliberately broader than the kickoff row:
// for non-pump tasks the kickoff can finish quickly after fanning out child
// work, and a later "Run Now" should still upgrade the already-active children
// so the max-runtime sweep does not cancel them under the user's feet.
func MarkScheduledTaskJobsManual(ctx context.Context, db DB, taskID string) (int64, error) {
	if taskID == "" {
		return 0, nil
	}
	tag, err := db.Exec(ctx, `
		UPDATE river_job
		   SET metadata = metadata || '{"source": "manual"}'::jsonb
		 WHERE args->>'scheduled_task_id' = $1
		   AND state IN `+kickoffActiveStates+`
	`, taskID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ClaimKickoffFinish atomically stamps the kickoff row as finishing and
// returns its live source. This serializes a pump's wind-down against a
// concurrent Run-Now upgrade: an upgrade that committed first is visible
// in the returned source (the pump aborts the wind-down and continues as
// a manual full drain); one that arrives after the claim is rejected by
// MarkActiveKickoffManual's finishing guard, so its TriggerNow starts a
// fresh manual run instead. A pump that aborts (or resumes after a crash
// mid-finish) clears the claim via its next state patch.
func ClaimKickoffFinish(ctx context.Context, db DB, jobID int64) (string, error) {
	var source string
	err := db.QueryRow(ctx, `
		UPDATE river_job
		   SET metadata = metadata || '{"finishing": true}'::jsonb
		 WHERE id = $1
		 RETURNING COALESCE(metadata->>'source', '')
	`, jobID).Scan(&source)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return source, err
}

// MergeJobMetadata merges a JSON object into one job's metadata. The ||
// merge preserves keys the patch doesn't mention (notably "source", which
// MarkActiveKickoffManual may flip concurrently), so pump state writes
// can't clobber a mid-run manual upgrade.
func MergeJobMetadata(ctx context.Context, db DB, jobID int64, patch []byte) error {
	_, err := db.Exec(ctx, `
		UPDATE river_job SET metadata = metadata || $2::jsonb WHERE id = $1
	`, jobID, patch)
	return err
}

// ActiveKickoffRun is a snapshot of a task's active kickoff row — enough
// to stamp run bookkeeping when the run is cancelled from outside. (A
// snoozed pump row is finalized directly by cancellation, so the pump
// never gets to write its own last_run stats.)
type ActiveKickoffRun struct {
	JobID     int64
	CreatedAt time.Time
	Metadata  []byte
}

func GetActiveKickoff(ctx context.Context, db DB, kickoffKind, taskID string) (*ActiveKickoffRun, error) {
	if kickoffKind == "" || taskID == "" {
		return nil, nil
	}
	var run ActiveKickoffRun
	err := db.QueryRow(ctx, `
		SELECT id, created_at, metadata FROM river_job
		WHERE kind = $1
		  AND args->>'scheduled_task_id' = $2
		  AND state IN `+kickoffActiveStates+`
		ORDER BY id DESC
		LIMIT 1
	`, kickoffKind, taskID).Scan(&run.JobID, &run.CreatedAt, &run.Metadata)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func CountByKinds(ctx context.Context, db DB, kinds []string) (RuntimeCounts, error) {
	if len(kinds) == 0 {
		return RuntimeCounts{}, nil
	}
	var counts RuntimeCounts
	err := db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE state IN ('available', 'scheduled', 'retryable')),
			count(*) FILTER (WHERE state = 'running')
		FROM river_job
		WHERE kind = ANY($1::text[])
	`, kinds).Scan(&counts.Pending, &counts.Running)
	return counts, err
}

func CountScheduledTask(ctx context.Context, db DB, taskID string, kinds []string) (RuntimeCounts, error) {
	if taskID == "" || len(kinds) == 0 {
		return RuntimeCounts{}, nil
	}
	var counts RuntimeCounts
	err := db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE state IN ('available', 'scheduled', 'retryable')),
			count(*) FILTER (WHERE state = 'running')
		FROM river_job
		WHERE kind = ANY($1::text[])
		  AND args->>'scheduled_task_id' = $2
	`, kinds, taskID).Scan(&counts.Pending, &counts.Running)
	return counts, err
}

func ScheduledTaskExceededRuntime(ctx context.Context, db DB, taskID string, kinds []string, minutes int32) (bool, error) {
	if taskID == "" || len(kinds) == 0 || minutes <= 0 {
		return false, nil
	}
	var exceeded bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM river_job
			WHERE state IN ('available', 'scheduled', 'retryable', 'running')
			  AND kind = ANY($1::text[])
			  AND args->>'scheduled_task_id' = $2
			  AND created_at < now() - ($3::int * interval '1 minute')
			  AND COALESCE(metadata->>'source', '') <> $4
		)
	`, kinds, taskID, minutes, KickoffSourceManual).Scan(&exceeded)
	return exceeded, err
}

func CountActiveByKinds(ctx context.Context, db DB, kinds []string) (int64, error) {
	if len(kinds) == 0 {
		return 0, nil
	}
	var count int64
	err := db.QueryRow(ctx, `
		SELECT count(*) FROM river_job
		WHERE state IN ('available', 'running', 'retryable', 'scheduled')
		  AND kind = ANY($1::text[])
	`, kinds).Scan(&count)
	return count, err
}

func CountActive(ctx context.Context, db DB) (RuntimeCounts, error) {
	var counts RuntimeCounts
	err := db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE state = 'available' OR state = 'retryable'),
			count(*) FILTER (WHERE state = 'running')
		FROM river_job
	`).Scan(&counts.Pending, &counts.Running)
	return counts, err
}

func CountActiveExcludingKind(ctx context.Context, db DB, excludedKind string) (RuntimeCounts, error) {
	var counts RuntimeCounts
	err := db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE state = 'available' OR state = 'retryable'),
			count(*) FILTER (WHERE state = 'running')
		FROM river_job
		WHERE kind <> $1
	`, excludedKind).Scan(&counts.Pending, &counts.Running)
	return counts, err
}

func RunningIDsByScheduledTask(ctx context.Context, db DB, taskID string, kinds []string) ([]int64, error) {
	if taskID == "" || len(kinds) == 0 {
		return nil, nil
	}
	rows, err := db.Query(ctx, `
		SELECT id FROM river_job
		WHERE state = 'running'
		  AND kind = ANY($1::text[])
		  AND args->>'scheduled_task_id' = $2
	`, kinds, taskID)
	if err != nil {
		return nil, err
	}
	return scanJobIDs(rows)
}

func CancelPendingByScheduledTask(ctx context.Context, db DB, taskID string, kinds []string) (int64, error) {
	if taskID == "" || len(kinds) == 0 {
		return 0, nil
	}
	tag, err := db.Exec(ctx, `
		UPDATE river_job
		   SET state = 'cancelled', finalized_at = now()
		 WHERE state IN ('available', 'retryable', 'scheduled')
		   AND kind = ANY($1::text[])
		   AND args->>'scheduled_task_id' = $2
	`, kinds, taskID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func CancelJob(ctx context.Context, db DB, id int64) (int64, error) {
	tag, err := db.Exec(ctx, `
		UPDATE river_job
		   SET state = 'cancelled', finalized_at = now()
		 WHERE id = $1
		   AND state IN ('available', 'retryable', 'scheduled')
	`, id)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func RetryJob(ctx context.Context, db DB, id int64) (int64, error) {
	tag, err := db.Exec(ctx, `
		UPDATE river_job
		   SET state = 'available',
		       attempt = GREATEST(attempt - 1, 0),
		       scheduled_at = now(),
		       finalized_at = NULL
		 WHERE id = $1
		   AND state IN ('discarded', 'cancelled', 'retryable')
	`, id)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func RescueOrphanedRunning(ctx context.Context, db DB) (int64, error) {
	tag, err := db.Exec(ctx, `
		UPDATE river_job
		   SET state = 'available', attempted_at = NULL, attempted_by = NULL
		 WHERE state = 'running'
	`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func RescueStuckRunning(ctx context.Context, db DB) (rescued int64, retriesReset int64, err error) {
	// Only sweep jobs past RescueStuckAfter — i.e. beyond their context
	// deadline and therefore genuinely stuck. A shorter window would flip a
	// live, slow-but-working job (e.g. a large SMB scan) back to 'available'
	// and run it a second time.
	stuckSecs := RescueStuckAfter.Seconds()
	tag1, err := db.Exec(ctx, `
		UPDATE river_job
		   SET state = 'available', attempted_at = NULL, attempted_by = NULL
		 WHERE state = 'running'
		   AND attempted_at < now() - make_interval(secs => $1)
		   AND attempt < max_attempts
	`, stuckSecs)
	if err != nil {
		return 0, 0, err
	}
	tag2, err := db.Exec(ctx, `
		UPDATE river_job
		   SET state = 'available', attempted_at = NULL, attempted_by = NULL,
		       attempt = GREATEST(attempt - 1, 0)
		 WHERE state = 'running'
		   AND attempted_at < now() - make_interval(secs => $1)
		   AND attempt >= max_attempts
	`, stuckSecs)
	if err != nil {
		return 0, 0, err
	}
	return tag1.RowsAffected() + tag2.RowsAffected(), tag2.RowsAffected(), nil
}

func ClearCompleted(ctx context.Context, db DB) (int64, error) {
	tag, err := db.Exec(ctx, "DELETE FROM river_job WHERE state IN ('completed', 'discarded', 'cancelled')")
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func ClearAll(ctx context.Context, db DB) (int64, error) {
	tag, err := db.Exec(ctx, "DELETE FROM river_job")
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ClearByKind deletes every river_job of the given kind, optionally scoped to a
// single state. An empty kind is a no-op (returns 0) — the guard is deliberate
// so a missing kind can never collapse into a queue-wide DELETE the way
// ClearAll would. With state empty this hard-deletes running rows too, matching
// the "Wipe queue" (ClearAll) precedent.
func ClearByKind(ctx context.Context, db DB, kind, state string) (int64, error) {
	if kind == "" {
		return 0, nil
	}
	query := "DELETE FROM river_job WHERE kind = $1"
	args := []any{kind}
	if state != "" {
		query += " AND state = $2"
		args = append(args, state)
	}
	tag, err := db.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CancelPendingByKinds cancels every not-yet-running job of the given
// kinds, optionally scoped to one library (libraryID > 0 matches the
// args->>'library_id' the scan pipeline kinds carry; derived kinds like
// ffprobe carry library_file_id instead and are only reachable with
// libraryID == 0). Running jobs are NOT touched here — flipping a running
// row's state under River corrupts its bookkeeping; cancel those through
// the client's JobCancel so the worker's context aborts cleanly.
func CancelPendingByKinds(ctx context.Context, db DB, kinds []string, libraryID int64) (int64, error) {
	tag, err := db.Exec(ctx, `
		UPDATE river_job
		   SET state = 'cancelled', finalized_at = now()
		 WHERE state IN ('available', 'pending', 'retryable', 'scheduled')
		   AND kind = ANY($1)
		   AND ($2::bigint = 0 OR NULLIF(args->>'library_id', '')::bigint = $2)
	`, kinds, libraryID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CountActiveScanJobs returns pending (not yet running) and running counts
// of the given kinds in ONE statement — a single MVCC snapshot. If both are
// zero simultaneously, nothing exists that could spawn a successor: any job
// appearing afterwards belongs to a new, externally triggered burst. Two
// separate queries cannot make that claim (a job can finalize and spawn
// between them).
func CountActiveScanJobs(ctx context.Context, db DB, kinds []string, libraryID int64) (pending, running int64, err error) {
	err = db.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE state IN ('available', 'pending', 'retryable', 'scheduled')),
			count(*) FILTER (WHERE state = 'running')
		FROM river_job
		WHERE state IN ('available', 'pending', 'retryable', 'scheduled', 'running')
		  AND kind = ANY($1)
		  AND ($2::bigint = 0 OR NULLIF(args->>'library_id', '')::bigint = $2)
	`, kinds, libraryID).Scan(&pending, &running)
	return pending, running, err
}

// ListRunningJobIDsByKinds returns the ids of currently running jobs of the
// given kinds (optionally per-library), for cancellation via river's
// JobCancel.
func ListRunningJobIDsByKinds(ctx context.Context, db DB, kinds []string, libraryID int64) ([]int64, error) {
	rows, err := db.Query(ctx, `
		SELECT id FROM river_job
		 WHERE state = 'running'
		   AND kind = ANY($1)
		   AND ($2::bigint = 0 OR NULLIF(args->>'library_id', '')::bigint = $2)
	`, kinds, libraryID)
	if err != nil {
		return nil, err
	}
	return scanJobIDs(rows)
}

func scanJobIDs(rows pgx.Rows) ([]int64, error) {
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}
