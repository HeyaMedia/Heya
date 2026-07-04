// Package scheduler is a 60-second cron-style trigger loop. It reads
// the scheduled_tasks table (user-editable cadence config) and inserts
// the matching River kickoff job whenever a row is due. All actual work
// happens in the kickoff + per-item River workers in internal/worker/.
//
// The previous in-process task runner (with its own ProgressTracker +
// running map + max_runtime_minutes timeout) was removed during the
// queue-split refactor — cancellation, parallelism, and persistence all
// live in River now. What's left is this thin scheduling decider.
package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/queueops"
	"github.com/karbowiak/heya/internal/taskdefs"
	"github.com/karbowiak/heya/internal/worker"
	"github.com/riverqueue/river"
	"github.com/rs/zerolog/log"
)

// Trigger is the cron decider. Construct once per process, call Start
// with the lifetime ctx, and it drives the kickoff cadence forever.
type Trigger struct {
	db    *pgxpool.Pool
	river *river.Client[pgx.Tx]
}

func NewTrigger(db *pgxpool.Pool, rc *river.Client[pgx.Tx]) *Trigger {
	return &Trigger{db: db, river: rc}
}

func (t *Trigger) Start(ctx context.Context) {
	go t.loop(ctx)
	go t.maxRuntimeLoop(ctx)
}

func (t *Trigger) loop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	t.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.tick(ctx)
		}
	}
}

func (t *Trigger) maxRuntimeLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.enforceMaxRuntimes(ctx)
		}
	}
}

func (t *Trigger) enforceMaxRuntimes(ctx context.Context) {
	q := sqlc.New(t.db)
	rows, err := q.ListScheduledTasks(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("scheduler: list scheduled_tasks for max runtime failed")
		return
	}
	for _, task := range rows {
		if !task.Enabled || task.MaxRuntimeMinutes <= 0 {
			continue
		}
		if err := t.enforceTaskMaxRuntime(ctx, task.ID, task.MaxRuntimeMinutes); err != nil {
			log.Warn().Err(err).Str("task", task.ID).Msg("scheduler: max runtime enforcement failed")
		}
	}
}

func (t *Trigger) enforceTaskMaxRuntime(ctx context.Context, taskID string, maxRuntimeMinutes int32) error {
	def, ok := taskdefs.ByID(taskID)
	if !ok {
		return nil
	}
	kinds := taskdefs.TaskKinds(taskID)
	if len(kinds) == 0 {
		return nil
	}

	if def.Pump {
		// Pump kickoffs own their window: every wake they check max runtime
		// (and the manual "drain everything" exemption) and wind the run
		// down gracefully, stamping the scheduled_tasks row — cancelling
		// their jobs from here would race that wind-down and cancel the
		// centroid follow-ups the pump deliberately preserves. While the
		// pump is alive, leave its jobs alone. If the pump died (crashed,
		// discarded after repeated failures), fall through and reap the
		// orphaned work jobs once they outlive the window.
		_, active, err := queueops.ActiveKickoffSource(ctx, t.db, def.KickoffKind, taskID)
		if err != nil {
			return err
		}
		if active {
			return nil
		}
		kinds = def.WorkKinds
	}

	exceeded, err := queueops.ScheduledTaskExceededRuntime(ctx, t.db, taskID, kinds, maxRuntimeMinutes)
	if err != nil {
		return err
	}
	if !exceeded {
		return nil
	}

	cancelled, err := queueops.CancelPendingByScheduledTask(ctx, t.db, taskID, kinds)
	if err != nil {
		return err
	}
	runningIDs, err := queueops.RunningIDsByScheduledTask(ctx, t.db, taskID, kinds)
	if err != nil {
		return err
	}
	for _, jobID := range runningIDs {
		if _, err := t.river.JobCancel(ctx, jobID); err == nil {
			cancelled++
		}
	}
	log.Warn().Str("task", taskID).Int64("cancelled", cancelled).Msg("scheduler: max runtime exceeded, cancelled task jobs")
	return nil
}

func (t *Trigger) tick(ctx context.Context) {
	q := sqlc.New(t.db)
	rows, err := q.ListScheduledTasks(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("scheduler: list scheduled_tasks failed")
		return
	}
	now := time.Now()
	for _, row := range rows {
		if !row.Enabled {
			continue
		}
		if !row.NextRunAt.Valid {
			// Initialise next_run_at on first sight; don't trigger
			// immediately — that surprises users who just enabled a
			// task and find a kickoff queued before they finished
			// configuring the window.
			next := initialNextRunAfter(now, row.IntervalHours, row.DailyStartTime, row.DailyEndTime)
			if _, err := t.db.Exec(ctx,
				"UPDATE scheduled_tasks SET next_run_at = $1 WHERE id = $2",
				pgtype.Timestamptz{Time: next, Valid: true}, row.ID,
			); err == nil {
				log.Info().Str("task", row.ID).Time("next_run", next).Msg("scheduler: initialised next_run_at")
			}
			continue
		}
		if now.Before(row.NextRunAt.Time) {
			continue
		}
		if !inTimeWindow(now, row.DailyStartTime, row.DailyEndTime) {
			continue
		}
		if err := t.TriggerNow(ctx, row.ID, false); err != nil {
			log.Warn().Err(err).Str("task", row.ID).Msg("scheduler: trigger failed")
			continue
		}
		// next_run_at is bumped by the kickoff worker on completion via
		// finishKickoff. We only need to keep this row from re-firing
		// the same minute, so push next_run_at to "tomorrow at start"
		// here too — the kickoff's stamp will override on success.
		next := nextRunAfter(now, row.IntervalHours, row.DailyStartTime, row.DailyEndTime)
		_, _ = t.db.Exec(ctx,
			"UPDATE scheduled_tasks SET next_run_at = $1 WHERE id = $2",
			pgtype.Timestamptz{Time: next, Valid: true}, row.ID,
		)
	}
}

// TriggerNow inserts the kickoff job for the named scheduled task.
// UniqueByArgs short-circuits if a kickoff for the same task is already
// queued or running, so concurrent "Run Now" clicks coalesce — and a cron
// firing during an active run skips its window the same way.
//
// manual marks user-initiated runs ("Run Now" / CLI). Manual runs carry
// {"source":"manual"} in the job metadata (not the args, so uniqueness
// still coalesces them with scheduled runs) and are exempt from
// max-runtime enforcement: they keep going until there's nothing left.
// When a manual trigger coalesces with an already-active scheduled run,
// the active run is upgraded to manual — the user asked for a full drain,
// so silently no-oping would be wrong.
func (t *Trigger) TriggerNow(ctx context.Context, taskID string, manual bool) error {
	var args river.JobArgs
	switch taskID {
	case "scan_libraries":
		args = worker.KickoffLibraryScanArgs{ScheduledTaskID: taskID}
	case "refresh_stale_items":
		args = worker.KickoffRefreshStaleArgs{ScheduledTaskID: taskID}
	case "scan_music_loudness":
		args = worker.KickoffMusicLoudnessArgs{ScheduledTaskID: taskID}
	case "scan_music_fingerprint":
		args = worker.KickoffMusicFingerprintArgs{ScheduledTaskID: taskID}
	case "generate_trickplay":
		args = worker.KickoffTrickplayArgs{ScheduledTaskID: taskID}
	case "generate_thumbnails":
		args = worker.KickoffThumbnailsArgs{ScheduledTaskID: taskID}
	case "analyze_music_facets":
		args = worker.KickoffSonicAnalysisArgs{ScheduledTaskID: taskID}
	default:
		return fmt.Errorf("unknown task: %s", taskID)
	}

	var opts *river.InsertOpts
	if manual {
		opts = &river.InsertOpts{Metadata: []byte(`{"source": "manual"}`)}
	}
	res, err := t.river.Insert(ctx, args, opts)
	if err != nil {
		return err
	}
	if !manual || !res.UniqueSkippedAsDuplicate {
		return nil
	}
	def, ok := taskdefs.ByID(taskID)
	if !ok {
		return nil
	}
	// The insert coalesced with an active run — upgrade that run to manual
	// so it drains fully instead of silently no-oping. The retry loop
	// covers the finishing handshake: a pump that has claimed "finishing"
	// rejects the upgrade (MarkActiveKickoffManual matches 0 rows) and
	// completes within milliseconds, after which a fresh manual insert
	// wins. Either way the click is never swallowed.
	for attempt := 0; attempt < 5; attempt++ {
		n, err := queueops.MarkActiveKickoffManual(ctx, t.db, def.KickoffKind, taskID)
		if err != nil {
			return err
		}
		if n > 0 {
			log.Info().Str("task", taskID).Msg("scheduler: upgraded active kickoff run to manual (full drain)")
			return nil
		}
		res, err := t.river.Insert(ctx, args, opts)
		if err != nil {
			return err
		}
		if !res.UniqueSkippedAsDuplicate {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("task %s: the active run is finishing — retry in a moment", taskID)
}

// EnqueueLibraryScan inserts kickoff_library_scan for one library.
// Called by /api/libraries/{id}/refresh and by the fsnotify watcher
// when a path change wants a rescan. UniqueByArgs deduplicates rapid
// retriggers per (LibraryID, Force) pair.
func (t *Trigger) EnqueueLibraryScan(ctx context.Context, libraryID int64, force bool) error {
	_, err := t.river.Insert(ctx, worker.KickoffLibraryScanArgs{LibraryID: libraryID, Force: force}, nil)
	return err
}

func nextRunAfter(now time.Time, intervalHours int32, dailyStartTime, dailyEndTime string) time.Time {
	if intervalHours < 1 {
		intervalHours = 24
	}
	candidate := now.Add(time.Duration(intervalHours) * time.Hour)
	if inTimeWindow(candidate, dailyStartTime, dailyEndTime) {
		return candidate
	}
	return nextWindowStartAfter(candidate, dailyStartTime)
}

func initialNextRunAfter(now time.Time, intervalHours int32, dailyStartTime, dailyEndTime string) time.Time {
	if inTimeWindow(now, dailyStartTime, dailyEndTime) {
		return nextRunAfter(now, intervalHours, dailyStartTime, dailyEndTime)
	}
	if _, err := time.Parse("15:04", dailyStartTime); err != nil {
		return nextRunAfter(now, intervalHours, dailyStartTime, dailyEndTime)
	}
	if _, err := time.Parse("15:04", dailyEndTime); err != nil {
		return nextRunAfter(now, intervalHours, dailyStartTime, dailyEndTime)
	}
	return nextWindowStartAfter(now, dailyStartTime)
}

func nextWindowStartAfter(now time.Time, dailyStartTime string) time.Time {
	start, err := time.Parse("15:04", dailyStartTime)
	if err != nil {
		return now.Add(24 * time.Hour)
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), start.Hour(), start.Minute(), 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func inTimeWindow(now time.Time, startStr, endStr string) bool {
	start, err := time.Parse("15:04", startStr)
	if err != nil {
		return false
	}
	end, err := time.Parse("15:04", endStr)
	if err != nil {
		return false
	}
	nowM := now.Hour()*60 + now.Minute()
	startM := start.Hour()*60 + start.Minute()
	endM := end.Hour()*60 + end.Minute()
	if endM > startM {
		return nowM >= startM && nowM < endM
	}
	// Window wraps midnight (e.g. 23:00–02:00).
	return nowM >= startM || nowM < endM
}
