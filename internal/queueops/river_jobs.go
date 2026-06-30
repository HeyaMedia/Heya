package queueops

import (
	"context"
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
		)
	`, kinds, taskID, minutes).Scan(&exceeded)
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

func RunningIDsByKinds(ctx context.Context, db DB, kinds []string) ([]int64, error) {
	return runningIDsByKinds(ctx, db, kinds, "")
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

func CancelPendingByKinds(ctx context.Context, db DB, kinds []string) (int64, error) {
	if len(kinds) == 0 {
		return 0, nil
	}
	tag, err := db.Exec(ctx, `
		UPDATE river_job
		   SET state = 'cancelled', finalized_at = now()
		 WHERE state IN ('available', 'retryable', 'scheduled')
		   AND kind = ANY($1::text[])
	`, kinds)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
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

func CancelPendingByLibrary(ctx context.Context, db DB, libraryID int64) (int64, error) {
	tag, err := db.Exec(ctx, `
		UPDATE river_job
		   SET state = 'cancelled', finalized_at = now()
		 WHERE state IN ('available', 'retryable', 'scheduled')
		   AND (args->>'library_id')::bigint = $1
	`, libraryID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func CancelAllPending(ctx context.Context, db DB) (int64, error) {
	tag, err := db.Exec(ctx, `
		UPDATE river_job
		   SET state = 'cancelled', finalized_at = now()
		 WHERE state IN ('available', 'retryable', 'scheduled')
	`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func runningIDsByKinds(ctx context.Context, db DB, kinds []string, extraPredicate string) ([]int64, error) {
	if len(kinds) == 0 {
		return nil, nil
	}
	query := `
		SELECT id FROM river_job
		WHERE state = 'running'
		  AND kind = ANY($1::text[])
	` + extraPredicate
	rows, err := db.Query(ctx, query, kinds)
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
