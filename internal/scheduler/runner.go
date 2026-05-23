package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/rs/zerolog/log"
)

type runState struct {
	tracker  *ProgressTracker
	cancelFn context.CancelFunc
}

type Runner struct {
	db      *pgxpool.Pool
	hub     *eventhub.Hub
	dataDir string
	tasks   map[TaskID]Task
	ctx     context.Context

	mu      sync.RWMutex
	running map[TaskID]*runState
}

func NewRunner(db *pgxpool.Pool, hub *eventhub.Hub, dataDir string) *Runner {
	return &Runner{
		db:      db,
		hub:     hub,
		dataDir: dataDir,
		tasks:   make(map[TaskID]Task),
		running: make(map[TaskID]*runState),
	}
}

func (r *Runner) Register(task Task) {
	r.tasks[task.ID()] = task
}

func (r *Runner) Start(ctx context.Context) {
	r.ctx = ctx
	go r.tickLoop(ctx)
	go r.broadcastLoop(ctx)
}

func (r *Runner) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	r.checkSchedules(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkSchedules(ctx)
		}
	}
}

func (r *Runner) checkSchedules(ctx context.Context) {
	q := sqlc.New(r.db)
	tasks, err := q.ListScheduledTasks(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("scheduler: failed to list tasks")
		return
	}

	now := time.Now()

	for _, t := range tasks {
		if !t.Enabled {
			continue
		}

		if !t.NextRunAt.Valid {
			nextRun := r.computeNextRun(t.DailyStartTime)
			r.db.Exec(ctx, "UPDATE scheduled_tasks SET next_run_at = $1 WHERE id = $2", nextRun, t.ID)
			log.Info().Str("task", t.ID).Time("next_run", nextRun).Msg("scheduler: initialized next_run_at")
			continue
		}

		if now.Before(t.NextRunAt.Time) {
			continue
		}

		if !r.inTimeWindow(now, t.DailyStartTime, t.DailyEndTime) {
			continue
		}

		taskID := TaskID(t.ID)
		r.mu.RLock()
		_, alreadyRunning := r.running[taskID]
		r.mu.RUnlock()
		if alreadyRunning {
			continue
		}

		go r.runTask(ctx, taskID)
	}
}

func (r *Runner) inTimeWindow(now time.Time, startStr, endStr string) bool {
	start, err := time.Parse("15:04", startStr)
	if err != nil {
		return false
	}
	end, err := time.Parse("15:04", endStr)
	if err != nil {
		return false
	}

	nowMinutes := now.Hour()*60 + now.Minute()
	startMinutes := start.Hour()*60 + start.Minute()
	endMinutes := end.Hour()*60 + end.Minute()

	if endMinutes > startMinutes {
		return nowMinutes >= startMinutes && nowMinutes < endMinutes
	}
	return nowMinutes >= startMinutes || nowMinutes < endMinutes
}

func (r *Runner) runTask(parentCtx context.Context, taskID TaskID) {
	task, ok := r.tasks[taskID]
	if !ok {
		log.Warn().Str("task", string(taskID)).Msg("scheduler: unknown task")
		return
	}

	q := sqlc.New(r.db)
	dbTask, err := q.GetScheduledTask(parentCtx, string(taskID))
	if err != nil {
		log.Error().Err(err).Str("task", string(taskID)).Msg("scheduler: failed to load task config")
		return
	}

	maxDuration := time.Duration(dbTask.MaxRuntimeMinutes) * time.Minute
	ctx, cancel := context.WithTimeout(parentCtx, maxDuration)
	defer cancel()

	tracker := NewProgressTracker(taskID, 0)

	r.mu.Lock()
	r.running[taskID] = &runState{tracker: tracker, cancelFn: cancel}
	r.mu.Unlock()

	startedAt := time.Now()
	log.Info().Str("task", string(taskID)).Msg("scheduler: task started")
	r.hub.Emit(eventhub.EventTaskProgress, tracker.Snapshot())

	defer func() {
		r.mu.Lock()
		delete(r.running, taskID)
		r.mu.Unlock()

		snap := tracker.Snapshot()
		duration := int32(time.Since(startedAt).Seconds())

		result := "completed"
		if snap.Failed > 0 && snap.Failed < snap.Total {
			result = "partial"
		}
		if snap.Failed > 0 && snap.Failed >= snap.Total {
			result = "error"
		}
		if ctx.Err() != nil {
			result = "stopped"
		}
		if err != nil {
			result = "error"
		}

		nextRun := r.computeNextRun(dbTask.DailyStartTime)

		bgCtx := context.Background()
		q.UpdateScheduledTaskRun(bgCtx, sqlc.UpdateScheduledTaskRunParams{
			ID:                    string(taskID),
			LastRunAt:             pgTimestamp(startedAt),
			LastRunResult:         result,
			LastRunDurationSec:    duration,
			LastRunItemsProcessed: int32(snap.Completed),
			LastRunItemsTotal:     int32(snap.Total),
			NextRunAt:             pgTimestamp(nextRun),
		})

		r.hub.Emit(eventhub.EventTaskProgress, TaskProgress{
			TaskID: string(taskID),
			State:  string(TaskIdle),
		})

		log.Info().
			Str("task", string(taskID)).
			Str("result", result).
			Int("processed", snap.Completed).
			Int("total", snap.Total).
			Dur("duration", time.Since(startedAt)).
			Msg("scheduler: task finished")
	}()

	total, countErr := task.CountPending(ctx)
	if countErr != nil {
		err = countErr
		log.Error().Err(err).Str("task", string(taskID)).Msg("scheduler: count pending failed")
		return
	}

	tracker.SetTotal(total)
	if total == 0 {
		log.Info().Str("task", string(taskID)).Msg("scheduler: nothing to do")
		return
	}

	err = task.Run(ctx, tracker)
	if err != nil {
		log.Error().Err(err).Str("task", string(taskID)).Msg("scheduler: task error")
	}
}

func (r *Runner) computeNextRun(dailyStartTime string) time.Time {
	now := time.Now()
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

func (r *Runner) TriggerNow(taskID TaskID) error {
	if _, ok := r.tasks[taskID]; !ok {
		return fmt.Errorf("unknown task: %s", taskID)
	}

	r.mu.RLock()
	_, alreadyRunning := r.running[taskID]
	r.mu.RUnlock()
	if alreadyRunning {
		return fmt.Errorf("task %s is already running", taskID)
	}

	go r.runTask(r.ctx, taskID)
	return nil
}

func (r *Runner) Cancel(taskID TaskID) error {
	r.mu.RLock()
	rs, ok := r.running[taskID]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("task %s is not running", taskID)
	}
	rs.cancelFn()
	return nil
}

func (r *Runner) GetAllProgress() map[TaskID]*TaskProgress {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.running) == 0 {
		return nil
	}

	result := make(map[TaskID]*TaskProgress, len(r.running))
	for id, rs := range r.running {
		snap := rs.tracker.Snapshot()
		result[id] = &snap
	}
	return result
}

func (r *Runner) broadcastLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.mu.RLock()
			snapshots := make([]TaskProgress, 0, len(r.running))
			for _, rs := range r.running {
				snapshots = append(snapshots, rs.tracker.Snapshot())
			}
			r.mu.RUnlock()
			for _, snap := range snapshots {
				r.hub.Emit(eventhub.EventTaskProgress, snap)
			}
		}
	}
}

func pgTimestamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
