package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/secrettext"
	"github.com/rs/zerolog"
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
	StartedAt        time.Time              `json:"started_at"`
	HeartbeatAt      time.Time              `json:"heartbeat_at"`
	Running          bool                   `json:"running"`
	Hostname         string                 `json:"hostname,omitempty"`
	PID              int                    `json:"pid,omitempty"`
	CPUPercent       float64                `json:"cpu_percent"`
	HostCPUPercent   float64                `json:"host_cpu_percent"`
	HostCPUAvailable bool                   `json:"host_cpu_available"`
	HostCPUMetric    string                 `json:"host_cpu_metric"`
	Goroutines       int                    `json:"goroutines"`
	HeapInUseBytes   uint64                 `json:"heap_inuse_bytes"`
	HeapAllocBytes   uint64                 `json:"heap_alloc_bytes"`
	SysBytes         uint64                 `json:"sys_bytes"`
	NumCPU           int                    `json:"num_cpu"`
	GOMAXPROCS       int                    `json:"gomaxprocs"`
	LogLevel         string                 `json:"log_level"`
	Watchers         []WorkerRuntimeWatcher `json:"watchers"`
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
	// Defense in depth for heartbeats written by older binaries before paths
	// were redacted at publication time.
	for i := range status.Watchers {
		status.Watchers[i].Path = secrettext.Redact(status.Watchers[i].Path)
	}
	return status, nil
}

// StartWorkerRuntimeHeartbeat publishes the dedicated worker's liveness and
// watcher state until ctx is cancelled. On graceful shutdown it writes one
// final Running=false snapshot; after a crash, the heartbeat expires naturally.
func (a *App) StartWorkerRuntimeHeartbeat(ctx context.Context) {
	publish := func(writeCtx context.Context, running bool) {
		watchers := []WorkerRuntimeWatcher{}
		if a.watcher != nil {
			watcherStatus := a.watcher.Status()
			watchers = make([]WorkerRuntimeWatcher, 0, len(watcherStatus))
			for libraryID, path := range watcherStatus {
				watchers = append(watchers, WorkerRuntimeWatcher{LibraryID: libraryID, Path: secrettext.Redact(path)})
			}
		}
		sort.Slice(watchers, func(i, j int) bool { return watchers[i].LibraryID < watchers[j].LibraryID })

		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		hostname, _ := os.Hostname()
		cpu := a.Diagnostics().CPUUsage()
		status := WorkerRuntimeStatus{
			StartedAt:        a.startedAt,
			HeartbeatAt:      time.Now().UTC(),
			Running:          running,
			Hostname:         hostname,
			PID:              os.Getpid(),
			CPUPercent:       cpu.ProcessPercent,
			HostCPUPercent:   cpu.HostPercent,
			HostCPUAvailable: cpu.HostAvailable,
			HostCPUMetric:    cpu.HostMetric,
			Goroutines:       runtime.NumGoroutine(),
			HeapInUseBytes:   memory.HeapInuse,
			HeapAllocBytes:   memory.HeapAlloc,
			SysBytes:         memory.Sys,
			NumCPU:           runtime.NumCPU(),
			GOMAXPROCS:       runtime.GOMAXPROCS(0),
			LogLevel:         zerolog.GlobalLevel().String(),
			Watchers:         watchers,
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

	a.startBackground(func() {
		workCtx, cancel := a.backgroundContext(ctx)
		defer cancel()
		publish(workCtx, true)
		ticker := time.NewTicker(workerRuntimeHeartbeatPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-workCtx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(workCtx), 2*time.Second)
				publish(shutdownCtx, false)
				cancel()
				return
			case <-ticker.C:
				publish(workCtx, true)
			}
		}
	})
}
