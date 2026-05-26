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
			next := nextRunAfter(now, row.DailyStartTime)
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
		if err := t.TriggerNow(ctx, row.ID); err != nil {
			log.Warn().Err(err).Str("task", row.ID).Msg("scheduler: trigger failed")
			continue
		}
		// next_run_at is bumped by the kickoff worker on completion via
		// finishKickoff. We only need to keep this row from re-firing
		// the same minute, so push next_run_at to "tomorrow at start"
		// here too — the kickoff's stamp will override on success.
		next := nextRunAfter(now, row.DailyStartTime)
		_, _ = t.db.Exec(ctx,
			"UPDATE scheduled_tasks SET next_run_at = $1 WHERE id = $2",
			pgtype.Timestamptz{Time: next, Valid: true}, row.ID,
		)
	}
}

// TriggerNow inserts the kickoff job for the named scheduled task.
// UniqueByArgs short-circuits if a kickoff for the same task is already
// queued or running, so concurrent "Run Now" clicks coalesce.
func (t *Trigger) TriggerNow(ctx context.Context, taskID string) error {
	switch taskID {
	case "scan_libraries":
		_, err := t.river.Insert(ctx, worker.KickoffLibraryScanArgs{}, nil)
		return err
	case "refresh_stale_items":
		_, err := t.river.Insert(ctx, worker.KickoffRefreshStaleArgs{}, nil)
		return err
	case "scan_music_loudness":
		_, err := t.river.Insert(ctx, worker.KickoffMusicLoudnessArgs{}, nil)
		return err
	case "generate_trickplay":
		_, err := t.river.Insert(ctx, worker.KickoffTrickplayArgs{}, nil)
		return err
	case "generate_thumbnails":
		_, err := t.river.Insert(ctx, worker.KickoffThumbnailsArgs{}, nil)
		return err
	case "analyze_music_facets":
		_, err := t.river.Insert(ctx, worker.KickoffSonicAnalysisArgs{}, nil)
		return err
	}
	return fmt.Errorf("unknown task: %s", taskID)
}

// EnqueueLibraryScan inserts kickoff_library_scan for one library.
// Called by /api/libraries/{id}/refresh and by the fsnotify watcher
// when a path change wants a rescan. UniqueByArgs deduplicates rapid
// retriggers per (LibraryID, Force) pair.
func (t *Trigger) EnqueueLibraryScan(ctx context.Context, libraryID int64, force bool) error {
	_, err := t.river.Insert(ctx, worker.KickoffLibraryScanArgs{LibraryID: libraryID, Force: force}, nil)
	return err
}

func nextRunAfter(now time.Time, dailyStartTime string) time.Time {
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
