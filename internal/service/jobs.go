package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/secrettext"
	"github.com/karbowiak/heya/internal/taskdefs"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/rs/zerolog/log"
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
	Jobs         []JobRow `json:"jobs"`
	Total        int      `json:"total"`
	HasMore      bool     `json:"has_more"`
	NextBeforeID int64    `json:"next_before_id,omitempty"`
}

// JobSummaryRow holds a per-state job count.
type JobSummaryRow struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

// JobKindSummaryRow holds a per-kind job count — powers the Jobs page kind
// filter (only kinds that actually have rows, with their counts).
type JobKindSummaryRow struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// Periodic maintenance rows would drown out real work in the Jobs UI. They
// stay queryable by explicit kind but are excluded from the default list,
// summaries, and WebSocket queue status.
const hiddenJobKind = "debounce_sweep"
const metadataContinuationSweepKind = "metadata_continuation_sweep"

func isHiddenJobKind(kind string) bool {
	return kind == hiddenJobKind || kind == metadataContinuationSweepKind
}

const jobCountsTTL = 15 * time.Second

type jobCountRow struct {
	State string
	Kind  string
	Count int
}

// cachedJobCounts turns the state summary, kind summary, and filtered list
// total into views over one grouped scan. Holding the mutex while refreshing
// also coalesces the three parallel requests made by the Jobs page.
func (a *App) cachedJobCounts(ctx context.Context) ([]jobCountRow, error) {
	a.jobCountsMu.Lock()
	defer a.jobCountsMu.Unlock()

	if !a.jobCountsAt.IsZero() && time.Since(a.jobCountsAt) < jobCountsTTL {
		return a.jobCounts, nil
	}

	rows, err := a.db.Query(ctx, `SELECT state, kind, count(*) FROM river_job GROUP BY state, kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make([]jobCountRow, 0, 64)
	for rows.Next() {
		var row jobCountRow
		if err := rows.Scan(&row.State, &row.Kind, &row.Count); err != nil {
			return nil, err
		}
		counts = append(counts, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	a.jobCounts = counts
	a.jobCountsAt = time.Now()
	return a.jobCounts, nil
}

func (a *App) invalidateJobCounts() {
	a.jobCountsMu.Lock()
	a.jobCountsAt = time.Time{}
	a.jobCountsMu.Unlock()
}

func filteredJobTotal(counts []jobCountRow, state, kind string) int {
	total := 0
	for _, row := range counts {
		if state != "" && row.State != state {
			continue
		}
		if kind != "" {
			if row.Kind != kind {
				continue
			}
		} else if isHiddenJobKind(row.Kind) {
			continue
		}
		total += row.Count
	}
	return total
}

// ListJobs returns a filtered page newest-first. ID keyset paging lets
// Postgres walk river_job's primary key instead of sorting and skipping an
// ever-growing offset through the entire queue.
func (a *App) ListJobs(ctx context.Context, state string, kind string, limit int, beforeID int64) (JobListResult, error) {
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
	} else {
		where += " AND kind NOT IN ('" + hiddenJobKind + "', '" + metadataContinuationSweepKind + "')"
	}
	if beforeID > 0 {
		where += " AND id < $" + strconv.Itoa(argIdx)
		args = append(args, beforeID)
		argIdx++
	}

	counts, err := a.cachedJobCounts(ctx)
	if err != nil {
		return JobListResult{}, err
	}
	total := filteredJobTotal(counts, state, kind)

	query := "SELECT id, state, kind, queue, args::text, attempt, max_attempts, created_at, attempted_at, finalized_at, COALESCE(errors::text, '') FROM river_job " + where +
		" ORDER BY id DESC LIMIT $" + strconv.Itoa(argIdx)
	args = append(args, limit+1)

	rows, err := a.db.Query(ctx, query, args...)
	if err != nil {
		return JobListResult{}, err
	}
	defer rows.Close()

	jobs := make([]JobRow, 0, limit+1)
	for rows.Next() {
		var j JobRow
		var attemptedAt, finalizedAt *time.Time
		if err := rows.Scan(&j.ID, &j.State, &j.Kind, &j.Queue, &j.Args, &j.Attempt, &j.MaxAttempts, &j.CreatedAt, &attemptedAt, &finalizedAt, &j.Errors); err != nil {
			continue
		}
		j.AttemptedAt = attemptedAt
		j.FinalizedAt = finalizedAt
		// Redact only the response copy. River retains the exact stored args and
		// errors required for execution, retries, and operator forensics.
		j = redactJobRow(j)
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return JobListResult{}, err
	}

	hasMore := len(jobs) > limit
	if hasMore {
		jobs = jobs[:limit]
	}
	var nextBeforeID int64
	if hasMore && len(jobs) > 0 {
		nextBeforeID = jobs[len(jobs)-1].ID
	}

	return JobListResult{Jobs: jobs, Total: total, HasMore: hasMore, NextBeforeID: nextBeforeID}, nil
}

func redactJobRow(job JobRow) JobRow {
	job.Args = secrettext.RedactJSONOrText(job.Args)
	job.Errors = secrettext.RedactJSONOrText(job.Errors)
	return job
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
				`SELECT title, media_type::text FROM media_item_cards WHERE id = $1`,
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
	counts, err := a.cachedJobCounts(ctx)
	if err != nil {
		return nil, err
	}
	byState := make(map[string]int)
	for _, row := range counts {
		if isHiddenJobKind(row.Kind) {
			continue
		}
		byState[row.State] += row.Count
	}
	states := make([]string, 0, len(byState))
	for state := range byState {
		states = append(states, state)
	}
	sort.Strings(states)
	summary := make([]JobSummaryRow, 0, len(states))
	for _, state := range states {
		summary = append(summary, JobSummaryRow{State: state, Count: byState[state]})
	}
	return summary, nil
}

// JobKindSummary returns per-kind job counts, ordered by kind.
func (a *App) JobKindSummary(ctx context.Context) ([]JobKindSummaryRow, error) {
	counts, err := a.cachedJobCounts(ctx)
	if err != nil {
		return nil, err
	}
	byKind := make(map[string]int)
	for _, row := range counts {
		if isHiddenJobKind(row.Kind) {
			continue
		}
		byKind[row.Kind] += row.Count
	}
	kinds := make([]string, 0, len(byKind))
	for kind := range byKind {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	summary := make([]JobKindSummaryRow, 0, len(kinds))
	for _, kind := range kinds {
		summary = append(summary, JobKindSummaryRow{Kind: kind, Count: byKind[kind]})
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
	a.invalidateJobCounts()
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
	if err := worker.DeleteMetadataContinuationForRiverJob(ctx, a.db, id); err != nil {
		return err
	}
	a.invalidateJobCounts()
	return nil
}

// RescueOrphanedJobsAtStartup releases every river_job stuck in
// state='running' from a previous process. Called before
// app.StartWorkers. The long-lived worker command holds the singleton
// coordinator advisory lease before calling this method, so no other healthy
// coordinator can still own a running row; every state='running' row is an
// orphan from a prior boot (process killed mid-job, air reload, OS OOM, etc.)
// and safe to flip back to available.
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
	rescued, retriesReset, err = queueops.RescueStuckRunning(ctx, a.db)
	if err == nil && (rescued > 0 || retriesReset > 0) {
		a.invalidateJobCounts()
	}
	return rescued, retriesReset, err
}

// ClearCompletedJobs deletes all completed, discarded, and cancelled jobs.
// It returns the number of deleted rows.
func (a *App) ClearCompletedJobs(ctx context.Context) (int64, error) {
	n, err := queueops.ClearCompleted(ctx, a.db)
	if err == nil && n > 0 {
		a.invalidateJobCounts()
	}
	return n, err
}

// ClearAllJobs deletes every row from the river_job table.
// It returns the number of deleted rows.
func (a *App) ClearAllJobs(ctx context.Context) (int64, error) {
	continuations, err := worker.DeleteMetadataContinuations(ctx, a.db, 0)
	if err != nil {
		return 0, err
	}
	n, err := queueops.ClearAll(ctx, a.db)
	if err == nil && n > 0 {
		a.invalidateJobCounts()
	}
	return n + continuations, err
}

// ClearJobsByKind deletes every job of the given kind, optionally scoped to a
// single state. Returns the number of deleted rows. An empty kind deletes
// nothing (queueops.ClearByKind guards it).
func (a *App) ClearJobsByKind(ctx context.Context, kind, state string) (int64, error) {
	continuations, err := worker.DeleteMetadataContinuationsByKind(ctx, a.db, kind, state)
	if err != nil {
		return 0, err
	}
	n, err := queueops.ClearByKind(ctx, a.db, kind, state)
	if err == nil && n > 0 {
		a.invalidateJobCounts()
	}
	return n + continuations, err
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
		// A pump kickoff caught mid-wake may have inserted more work between
		// the pending sweep above and its own cancellation — sweep once more
		// so a freshly-topped wave doesn't survive the cancel.
		if extra, err := queueops.CancelPendingByScheduledTask(ctx, a.db, taskID, kinds); err == nil {
			cancelled += extra
		}
	}
	return cancelled, nil
}

// cancelScanJobs actually stops a scan instead of pretending to:
//  1. cancels every not-yet-running job of the scan task's kinds — the old
//     pending-only, all-kinds cancel left running stage jobs alive, and
//     each of those kept spawning its next stage (a running kickoff would
//     re-fan thousands of units seconds after the "cancel");
//  2. cancels RUNNING scan jobs through river's JobCancel, which aborts the
//     worker's context — the walk, search, fetch, and apply loops are all
//     context-aware, so a mid-walk kickoff stops instead of fanning out;
//  3. sweeps the in-flight scanner entities WITHOUT requeueing them, so the
//     orphan pruner (which requeues by design) doesn't resurrect work the
//     user explicitly stopped. The next scan re-discovers cancelled units
//     through normal change detection.
//
// The cancel runs as a LOOP until quiescent, because a single pass races
// the pipeline: a running stage job (or a mid-fan-out kickoff) can insert
// fresh 'available' successors after the pending sweep, and JobCancel
// signals land asynchronously — a signalled job may still finalize and
// spawn. Each round re-sweeps pending and re-signals running; the loop
// exits when a round finds nothing (nothing left to spawn from), or at a
// deadline, after which the pruner's cancelled-flag mops up any straggler
// without resurrecting it. Entities are swept only after quiescence so
// nothing in flight can recreate them.
func (a *App) cancelScanJobs(ctx context.Context, kinds []string, libraryID int64) (int64, error) {
	var cancelled int64
	signalled := map[int64]bool{}
	deadline := time.Now().Add(10 * time.Second)
	for {
		parked, err := worker.DeleteMetadataContinuations(ctx, a.db, libraryID)
		if err != nil {
			return cancelled, err
		}
		cancelled += parked

		n, err := queueops.CancelPendingByKinds(ctx, a.db, kinds, libraryID)
		if err != nil {
			return cancelled, err
		}
		cancelled += n

		var running []int64
		if a.river != nil {
			running, err = queueops.ListRunningJobIDsByKinds(ctx, a.db, kinds, libraryID)
			if err != nil {
				log.Warn().Err(err).Msg("cancel scans: listing running jobs failed; pending jobs were cancelled")
				break
			}
			for _, id := range running {
				if signalled[id] {
					continue
				}
				signalled[id] = true
				if _, err := a.river.JobCancel(ctx, id); err == nil {
					cancelled++
				}
			}
		}

		// Quiescence must be observed in ONE snapshot: this round's cancel
		// count and running list are separate statements, and a job can
		// finalize and spawn a successor in the gap between them. A single
		// query showing zero pending AND zero running simultaneously proves
		// no spawner existed at that instant.
		pending, active, err := queueops.CountActiveScanJobs(ctx, a.db, kinds, libraryID)
		if err != nil {
			log.Warn().Err(err).Msg("cancel scans: quiescence check failed; pruner will mop up without requeueing")
			break
		}
		if pending == 0 && active == 0 {
			break
		}
		if time.Now().After(deadline) {
			log.Warn().Int("still_running", len(running)).Msg("cancel scans: not quiescent at deadline; pruner will mop up without requeueing")
			break
		}
		select {
		case <-ctx.Done():
			return cancelled, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}

	if swept, err := worker.SweepCancelledScannerEntities(ctx, a.db, libraryID); err != nil {
		log.Warn().Err(err).Int64("swept", swept).Msg("cancel scans: entity sweep failed; pruner may requeue leftovers")
	}
	return cancelled, nil
}

// CancelLibraryJobs stops one library's scan: the pipeline kinds carrying
// library_id in their args (kickoff/process/search/fetch/apply). Derived per-file
// work (ffprobe, fingerprints, …) has no library_id and is only reachable
// via CancelAllPendingJobs.
func (a *App) CancelLibraryJobs(ctx context.Context, libraryID int64) (int64, error) {
	kinds := []string{"kickoff_library_scan", "process_scan", "search_metadata", "fetch_metadata", "apply_metadata"}
	return a.cancelScanJobs(ctx, kinds, libraryID)
}

// CancelAllPendingJobs stops every library scan and the scan task's derived
// work (probes, keyframes, fingerprints, loudness, facets, enrichment).
func (a *App) CancelAllPendingJobs(ctx context.Context) (int64, error) {
	return a.cancelScanJobs(ctx, taskdefs.TaskKinds("scan_libraries"), 0)
}
