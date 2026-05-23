package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/vfs"
)

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
	tag, err := a.db.Exec(ctx,
		"UPDATE river_job SET state = 'available', attempt = GREATEST(attempt - 1, 0), scheduled_at = now(), finalized_at = NULL WHERE id = $1 AND state IN ('discarded', 'cancelled', 'retryable')", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrJobNotRetryable
	}
	return nil
}

// CancelJob cancels a pending (available/retryable/scheduled) job.
func (a *App) CancelJob(ctx context.Context, id int64) error {
	tag, err := a.db.Exec(ctx,
		"UPDATE river_job SET state = 'cancelled', finalized_at = now() WHERE id = $1 AND state IN ('available', 'retryable', 'scheduled')", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrJobNotCancellable
	}
	return nil
}

// RescueStuckJobs rescues jobs that have been running for more than 30 seconds.
// It returns the total number of rescued jobs and the number whose retry counts
// were reset because they had exhausted their max attempts.
func (a *App) RescueStuckJobs(ctx context.Context) (rescued, retriesReset int64, err error) {
	tag1, err := a.db.Exec(ctx,
		`UPDATE river_job
		 SET state = 'available', attempted_at = NULL, attempted_by = NULL
		 WHERE state = 'running'
		   AND attempted_at < now() - interval '30 seconds'
		   AND attempt < max_attempts`)
	if err != nil {
		return 0, 0, err
	}

	tag2, err := a.db.Exec(ctx,
		`UPDATE river_job
		 SET state = 'available', attempted_at = NULL, attempted_by = NULL,
		     attempt = GREATEST(attempt - 1, 0)
		 WHERE state = 'running'
		   AND attempted_at < now() - interval '30 seconds'
		   AND attempt >= max_attempts`)
	if err != nil {
		return 0, 0, err
	}

	return tag1.RowsAffected() + tag2.RowsAffected(), tag2.RowsAffected(), nil
}

// ClearCompletedJobs deletes all completed, discarded, and cancelled jobs.
// It returns the number of deleted rows.
func (a *App) ClearCompletedJobs(ctx context.Context) (int64, error) {
	tag, err := a.db.Exec(ctx, "DELETE FROM river_job WHERE state IN ('completed', 'discarded', 'cancelled')")
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ClearAllJobs deletes every row from the river_job table.
// It returns the number of deleted rows.
func (a *App) ClearAllJobs(ctx context.Context) (int64, error) {
	tag, err := a.db.Exec(ctx, "DELETE FROM river_job")
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CancelLibraryJobs cancels all pending jobs whose args contain the given library ID.
// It returns the number of cancelled jobs.
func (a *App) CancelLibraryJobs(ctx context.Context, libraryID int64) (int64, error) {
	tag, err := a.db.Exec(ctx,
		`UPDATE river_job SET state = 'cancelled', finalized_at = now()
		 WHERE state IN ('available', 'retryable', 'scheduled')
		   AND (args->>'library_id')::bigint = $1`, libraryID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// CancelAllPendingJobs cancels every available, retryable, or scheduled job.
// It returns the number of cancelled jobs.
func (a *App) CancelAllPendingJobs(ctx context.Context) (int64, error) {
	tag, err := a.db.Exec(ctx,
		`UPDATE river_job SET state = 'cancelled', finalized_at = now()
		 WHERE state IN ('available', 'retryable', 'scheduled')`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
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
