package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/vfs"
)

// jsonUnmarshalQuiet is a tiny helper for argsJSON decoding — errors are
// tolerable (best-effort enrich title lookup), so the call sites get a
// no-error path when the data is missing or malformed.
func jsonUnmarshalQuiet(s string, v any) error {
	if s == "" {
		return nil
	}
	return json.Unmarshal([]byte(s), v)
}

var (
	// ErrJobNotRetryable is returned when a job cannot be retried because it is
	// not in a failed, cancelled, or retryable state.
	ErrJobNotRetryable = errors.New("job not found or not in a retryable state")

	// ErrJobNotCancellable is returned when a job cannot be cancelled because it
	// is not in an available, retryable, or scheduled state.
	ErrJobNotCancellable = errors.New("job not found or not in a cancellable state")

	// ErrSchedulerUnavailable is returned when a scheduler operation is attempted
	// but the scheduler has not been initialized.
	ErrSchedulerUnavailable = errors.New("scheduler not available")
)

// JobRow represents a single row from the river_job table.
type JobRow struct {
	ID          int64      `json:"id"`
	State       string     `json:"state"`
	Kind        string     `json:"kind"`
	Queue       string     `json:"queue"`
	Args        string     `json:"args"`
	Attempt     int        `json:"attempt"`
	MaxAttempts int        `json:"max_attempts"`
	CreatedAt   time.Time  `json:"created_at"`
	AttemptedAt *time.Time `json:"attempted_at,omitempty"`
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`
	Errors      string     `json:"errors,omitempty"`
}

// JobListResult holds a page of jobs together with the total count.
type JobListResult struct {
	Jobs  []JobRow `json:"jobs"`
	Total int      `json:"total"`
}

// JobSummaryRow holds a per-state job count.
type JobSummaryRow struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

// ListJobs returns a filtered, ordered page of river jobs.
func (a *App) ListJobs(ctx context.Context, state string, kind string, limit, offset int) (JobListResult, error) {
	where := "WHERE 1=1"
	args := []any{}
	argIdx := 1

	if state != "" {
		where += " AND state = $" + strconv.Itoa(argIdx)
		args = append(args, state)
		argIdx++
	}
	if kind != "" {
		where += " AND kind = $" + strconv.Itoa(argIdx)
		args = append(args, kind)
		argIdx++
	}

	var total int
	countQuery := "SELECT count(*) FROM river_job " + where
	if err := a.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return JobListResult{}, err
	}

	query := "SELECT id, state, kind, queue, args::text, attempt, max_attempts, created_at, attempted_at, finalized_at, COALESCE(errors::text, '') FROM river_job " + where +
		" ORDER BY CASE state WHEN 'running' THEN 0 WHEN 'available' THEN 1 WHEN 'retryable' THEN 2 WHEN 'scheduled' THEN 3 WHEN 'cancelled' THEN 4 WHEN 'discarded' THEN 5 WHEN 'completed' THEN 6 END, created_at DESC" +
		" LIMIT $" + strconv.Itoa(argIdx) + " OFFSET $" + strconv.Itoa(argIdx+1)
	args = append(args, limit, offset)

	rows, err := a.db.Query(ctx, query, args...)
	if err != nil {
		return JobListResult{}, err
	}
	defer rows.Close()

	jobs := []JobRow{}
	for rows.Next() {
		var j JobRow
		var attemptedAt, finalizedAt *time.Time
		if err := rows.Scan(&j.ID, &j.State, &j.Kind, &j.Queue, &j.Args, &j.Attempt, &j.MaxAttempts, &j.CreatedAt, &attemptedAt, &finalizedAt, &j.Errors); err != nil {
			continue
		}
		j.AttemptedAt = attemptedAt
		j.FinalizedAt = finalizedAt
		jobs = append(jobs, j)
	}

	return JobListResult{Jobs: jobs, Total: total}, nil
}

// MetadataQueueStatus is a snapshot of the `enrich_media_item` queue:
// pending counts by priority band, the currently-running job (if any) with
// its target item resolved, and a recent throughput window. Naming kept as
// "MetadataQueueStatus" so the existing FE panel keeps consuming the same
// shape — the underlying River queue moved during the per-kind queue split
// but the panel still shows the enrich pipeline's progress.
type MetadataQueueStatus struct {
	Pending           int                   `json:"pending"`
	PendingByPriority map[int]int           `json:"pending_by_priority"`
	Running           *MetadataQueueRunning `json:"running,omitempty"`
	Recent            MetadataQueueRecent   `json:"recent"`
}

// MetadataQueueRunning describes the one job currently executing on the
// enrich_media_item queue (MaxWorkers=1, so there's at most one).
type MetadataQueueRunning struct {
	JobID     int64     `json:"job_id"`
	Kind      string    `json:"kind"`
	Priority  int       `json:"priority"`
	ItemID    int64     `json:"item_id,omitempty"`
	ItemTitle string    `json:"item_title,omitempty"`
	MediaType string    `json:"media_type,omitempty"`
	Source    string    `json:"source,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// MetadataQueueRecent summarises throughput over a short trailing window so
// the UI can show "23 enriched in the last 5 min, avg 3.2s each".
type MetadataQueueRecent struct {
	Completed5Min  int     `json:"completed_5min"`
	AvgDurationSec float64 `json:"avg_duration_sec"`
}

// MetadataQueueStatus returns a snapshot of the enrich queue for the tasks
// page panel. Queries the river_job table directly (no public River API
// exposes this cleanly, and the column layout is stable across patch
// releases).
func (a *App) MetadataQueueStatus(ctx context.Context) (MetadataQueueStatus, error) {
	out := MetadataQueueStatus{
		PendingByPriority: map[int]int{1: 0, 2: 0, 3: 0, 4: 0},
	}

	rows, err := a.db.Query(ctx, `
		SELECT priority, count(*)
		FROM river_job
		WHERE queue = 'enrich_media_item' AND state IN ('available', 'scheduled', 'retryable')
		GROUP BY priority
	`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p, c int
		if err := rows.Scan(&p, &c); err != nil {
			continue
		}
		out.PendingByPriority[p] = c
		out.Pending += c
	}
	rows.Close()

	// Currently-running job. With MaxWorkers=1, at most one.
	var (
		jobID      int64
		kind       string
		argsJSON   string
		priority   int
		startedAt  *time.Time
		running    MetadataQueueRunning
		haveRunner bool
	)
	err = a.db.QueryRow(ctx, `
		SELECT id, kind, args::text, priority, attempted_at
		FROM river_job
		WHERE queue = 'enrich_media_item' AND state = 'running'
		ORDER BY attempted_at ASC
		LIMIT 1
	`).Scan(&jobID, &kind, &argsJSON, &priority, &startedAt)
	if err == nil {
		haveRunner = true
		running.JobID = jobID
		running.Kind = kind
		running.Priority = priority
		if startedAt != nil {
			running.StartedAt = *startedAt
		}
		// Best-effort: resolve item_id + title from args for the enrich job.
		// Other kinds may not carry an item_id — leave those fields empty.
		var args struct {
			ItemID int64  `json:"item_id"`
			Source string `json:"source"`
		}
		if jsonErr := jsonUnmarshalQuiet(argsJSON, &args); jsonErr == nil && args.ItemID != 0 {
			running.ItemID = args.ItemID
			running.Source = args.Source
			var title, mt string
			if titleErr := a.db.QueryRow(ctx,
				`SELECT title, media_type::text FROM media_items WHERE id = $1`,
				args.ItemID,
			).Scan(&title, &mt); titleErr == nil {
				running.ItemTitle = title
				running.MediaType = mt
			}
		}
	}
	// On query error (including the no-rows case) we just leave `running`
	// unset — the panel degrades gracefully rather than propagating.
	if haveRunner {
		out.Running = &running
	}

	// Throughput in the last 5 minutes.
	var done int
	var avgSec float64
	if err := a.db.QueryRow(ctx, `
		SELECT
			count(*),
			COALESCE(avg(extract(epoch from finalized_at - attempted_at)), 0)
		FROM river_job
		WHERE queue = 'enrich_media_item' AND state = 'completed'
		  AND finalized_at > now() - interval '5 minutes'
	`).Scan(&done, &avgSec); err == nil {
		out.Recent.Completed5Min = done
		out.Recent.AvgDurationSec = avgSec
	}

	return out, nil
}

// JobSummary returns per-state job counts.
func (a *App) JobSummary(ctx context.Context) ([]JobSummaryRow, error) {
	rows, err := a.db.Query(ctx, "SELECT state, count(*) FROM river_job GROUP BY state ORDER BY state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summary := []JobSummaryRow{}
	for rows.Next() {
		var s JobSummaryRow
		if err := rows.Scan(&s.State, &s.Count); err != nil {
			continue
		}
		summary = append(summary, s)
	}
	return summary, nil
}

// RetryJob moves a failed, cancelled, or retryable job back to the available state.
func (a *App) RetryJob(ctx context.Context, id int64) error {
	rows, err := queueops.RetryJob(ctx, a.db, id)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrJobNotRetryable
	}
	return nil
}

// CancelJob cancels a pending (available/retryable/scheduled) job.
func (a *App) CancelJob(ctx context.Context, id int64) error {
	rows, err := queueops.CancelJob(ctx, a.db, id)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrJobNotCancellable
	}
	return nil
}

// RescueOrphanedJobsAtStartup releases every river_job stuck in
// state='running' from a previous process. Called before
// app.StartWorkers — no worker in *this* process has started yet, so
// every state='running' row is definitionally an orphan from a prior
// boot (process killed mid-job, air reload, OS OOM, etc.) and safe to
// flip back to available.
//
// River's own periodic rescuer would eventually catch these via
// RescueStuckJobsAfter (queueops.RescueStuckAfter), but that's far too
// long to make MaxWorkers=N look violated in the UI after every dev
// reload. Doing it eagerly at boot keeps the running-job snapshot honest.
//
// Returns the count rescued so the caller can log it (zero on a clean
// boot, non-zero after an unclean shutdown).
func (a *App) RescueOrphanedJobsAtStartup(ctx context.Context) (int64, error) {
	return queueops.RescueOrphanedRunning(ctx, a.db)
}

// RescueStuckJobs rescues jobs that have been running past
// queueops.RescueStuckAfter (i.e. beyond their context deadline, so
// genuinely stuck rather than merely slow). It returns the total number of
// rescued jobs and the number whose retry counts were reset because they had
// exhausted their max attempts.
func (a *App) RescueStuckJobs(ctx context.Context) (rescued, retriesReset int64, err error) {
	return queueops.RescueStuckRunning(ctx, a.db)
}

// ClearCompletedJobs deletes all completed, discarded, and cancelled jobs.
// It returns the number of deleted rows.
func (a *App) ClearCompletedJobs(ctx context.Context) (int64, error) {
	return queueops.ClearCompleted(ctx, a.db)
}

// ClearAllJobs deletes every row from the river_job table.
// It returns the number of deleted rows.
func (a *App) ClearAllJobs(ctx context.Context) (int64, error) {
	return queueops.ClearAll(ctx, a.db)
}

// CancelJobsByKind cancels every River job whose kind matches one of the
// provided kinds. It runs in two passes:
//
//  1. SQL UPDATE: every available/scheduled/retryable job for those kinds is
//     immediately marked cancelled — they'll never be picked up.
//  2. River JobCancel: for any currently-running job, ask River to mark it
//     for cancellation. River sends LISTEN/NOTIFY to the producer and the
//     worker's ctx fires Done. Workers that respect ctx.Done() exit
//     promptly; ones that don't will run to natural completion (River
//     can't preempt a goroutine, only signal it).
//
// Used by the tasks-page Cancel button: cancelling a "task" now means
// cancelling every queued + running job across the kinds that task
// fans out into.
func (a *App) CancelJobsByKind(ctx context.Context, kinds []string) (int64, error) {
	if len(kinds) == 0 {
		return 0, nil
	}

	// Pass 1: bulk-cancel queued/scheduled/retryable rows.
	cancelled, err := queueops.CancelPendingByKinds(ctx, a.db, kinds)
	if err != nil {
		return 0, err
	}

	// Pass 2: signal running jobs via River so the worker's ctx is
	// cancelled (LISTEN/NOTIFY → producer → job ctx.Done). Best-effort —
	// a worker that ignores ctx will run to completion regardless.
	if a.river != nil {
		jobIDs, err := queueops.RunningIDsByKinds(ctx, a.db, kinds)
		if err == nil {
			for _, jobID := range jobIDs {
				if _, err := a.river.JobCancel(ctx, jobID); err == nil {
					cancelled++
				}
			}
		}
	}

	return cancelled, nil
}

func (a *App) CancelScheduledTaskJobs(ctx context.Context, taskID string, kinds []string) (int64, error) {
	if taskID == "" || len(kinds) == 0 {
		return 0, nil
	}
	cancelled, err := queueops.CancelPendingByScheduledTask(ctx, a.db, taskID, kinds)
	if err != nil {
		return 0, err
	}
	if a.river != nil {
		jobIDs, err := queueops.RunningIDsByScheduledTask(ctx, a.db, taskID, kinds)
		if err == nil {
			for _, jobID := range jobIDs {
				if _, err := a.river.JobCancel(ctx, jobID); err == nil {
					cancelled++
				}
			}
		}
	}
	return cancelled, nil
}

// CancelLibraryJobs cancels all pending jobs whose args contain the given library ID.
// It returns the number of cancelled jobs.
func (a *App) CancelLibraryJobs(ctx context.Context, libraryID int64) (int64, error) {
	return queueops.CancelPendingByLibrary(ctx, a.db, libraryID)
}

// CancelAllPendingJobs cancels every available, retryable, or scheduled job.
// It returns the number of cancelled jobs.
func (a *App) CancelAllPendingJobs(ctx context.Context) (int64, error) {
	return queueops.CancelAllPending(ctx, a.db)
}

// ScheduleEntry describes a periodic schedule derived from library settings.
type ScheduleEntry struct {
	LibraryID   int64  `json:"library_id"`
	LibraryName string `json:"library_name"`
	MediaType   string `json:"media_type"`
	Type        string `json:"type"`
	Interval    string `json:"interval"`
	IntervalSec int    `json:"interval_sec"`
}

// ListSchedules computes the active periodic schedules from all library settings.
func (a *App) ListSchedules(ctx context.Context) ([]ScheduleEntry, error) {
	libs, err := a.ListLibraries(ctx)
	if err != nil {
		return nil, err
	}

	entries := []ScheduleEntry{}

	for _, lib := range libs {
		settings := metadata.ParseSettings(lib.Settings)

		if settings.Watch {
			hasSMB := false
			for _, p := range lib.Paths {
				if vfs.IsSMBPath(p) {
					hasSMB = true
					break
				}
			}
			if hasSMB {
				interval := time.Hour
				if lib.ScanInterval.Valid {
					interval = time.Duration(lib.ScanInterval.Microseconds) * time.Microsecond
				}
				entries = append(entries, ScheduleEntry{
					LibraryID:   lib.ID,
					LibraryName: lib.Name,
					MediaType:   string(lib.MediaType),
					Type:        "scan",
					Interval:    FormatDuration(interval),
					IntervalSec: int(interval.Seconds()),
				})
			}
		}

		if settings.MetadataRefreshDays > 0 {
			interval := time.Duration(settings.MetadataRefreshDays) * 24 * time.Hour
			entries = append(entries, ScheduleEntry{
				LibraryID:   lib.ID,
				LibraryName: lib.Name,
				MediaType:   string(lib.MediaType),
				Type:        "metadata_refresh",
				Interval:    FormatDuration(interval),
				IntervalSec: int(interval.Seconds()),
			})
		}
	}

	return entries, nil
}

// FormatDuration formats a duration as a human-readable string.
func FormatDuration(d time.Duration) string {
	if d >= 24*time.Hour {
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day"
		}
		return formatInt(days) + " days"
	}
	if d >= time.Hour {
		h := int(d.Hours())
		if h == 1 {
			return "1 hour"
		}
		return formatInt(h) + " hours"
	}
	m := int(d.Minutes())
	if m == 1 {
		return "1 minute"
	}
	return formatInt(m) + " minutes"
}

func formatInt(n int) string {
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if s == "" {
		return "0"
	}
	return s
}
