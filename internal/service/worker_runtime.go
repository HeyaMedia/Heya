package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/rs/zerolog/log"
)

const (
	workerRuntimeSettingKey      = "runtime.worker.status"
	workerRuntimeHeartbeatPeriod = 10 * time.Second
	workerRuntimeStaleAfter      = 30 * time.Second
)

// WorkerRuntimeWatcher is one filesystem watcher owned by the dedicated
// worker process. A library with multiple roots has a comma-separated Path,
// matching the historical /api/watchers response.
type WorkerRuntimeWatcher struct {
	LibraryID int64  `json:"library_id"`
	Path      string `json:"path"`
}

// WorkerRuntimeStatus is the worker's durable heartbeat and watcher snapshot.
// It lives in system_settings so a newly restarted API process can report the
// current worker without waiting for an in-memory event.
type WorkerRuntimeStatus struct {
	StartedAt   time.Time              `json:"started_at"`
	HeartbeatAt time.Time              `json:"heartbeat_at"`
	Running     bool                   `json:"running"`
	Watchers    []WorkerRuntimeWatcher `json:"watchers"`
}

// Online reports whether the last heartbeat represents a live worker. The
// explicit Running bit makes graceful shutdown visible immediately; the age
// check covers crashes and lost nodes.
func (s WorkerRuntimeStatus) Online(now time.Time) bool {
	if !s.Running || s.HeartbeatAt.IsZero() || now.Before(s.HeartbeatAt) {
		return false
	}
	return now.Sub(s.HeartbeatAt) <= workerRuntimeStaleAfter
}

// WorkerRuntimeStatus reads the most recently published worker snapshot. A
// missing row is a normal first-start state and returns an empty status.
func (a *App) WorkerRuntimeStatus(ctx context.Context) (WorkerRuntimeStatus, error) {
	raw, err := sqlc.New(a.db).GetSystemSetting(ctx, workerRuntimeSettingKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return WorkerRuntimeStatus{}, nil
		}
		return WorkerRuntimeStatus{}, fmt.Errorf("read worker runtime status: %w", err)
	}

	var status WorkerRuntimeStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return WorkerRuntimeStatus{}, fmt.Errorf("decode worker runtime status: %w", err)
	}
	return status, nil
}

// StartWorkerRuntimeHeartbeat publishes the dedicated worker's liveness and
// watcher state until ctx is cancelled. On graceful shutdown it writes one
// final Running=false snapshot; after a crash, the heartbeat expires naturally.
func (a *App) StartWorkerRuntimeHeartbeat(ctx context.Context) {
	publish := func(writeCtx context.Context, running bool) {
		if a.watcher == nil {
			return
		}
		watcherStatus := a.watcher.Status()
		watchers := make([]WorkerRuntimeWatcher, 0, len(watcherStatus))
		for libraryID, path := range watcherStatus {
			watchers = append(watchers, WorkerRuntimeWatcher{LibraryID: libraryID, Path: path})
		}
		sort.Slice(watchers, func(i, j int) bool { return watchers[i].LibraryID < watchers[j].LibraryID })

		status := WorkerRuntimeStatus{
			StartedAt:   a.startedAt,
			HeartbeatAt: time.Now().UTC(),
			Running:     running,
			Watchers:    watchers,
		}
		raw, err := json.Marshal(status)
		if err != nil {
			log.Error().Err(err).Msg("failed to encode worker runtime heartbeat")
			return
		}
		if err := sqlc.New(a.db).UpsertSystemSetting(writeCtx, sqlc.UpsertSystemSettingParams{
			Key:   workerRuntimeSettingKey,
			Value: raw,
		}); err != nil && writeCtx.Err() == nil {
			log.Warn().Err(err).Msg("failed to publish worker runtime heartbeat")
		}
	}

	publish(ctx, true)
	go func() {
		ticker := time.NewTicker(workerRuntimeHeartbeatPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
				publish(shutdownCtx, false)
				cancel()
				return
			case <-ticker.C:
				publish(ctx, true)
			}
		}
	}()
}
