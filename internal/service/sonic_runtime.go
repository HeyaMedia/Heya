package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/sonicanalysis"
	"github.com/rs/zerolog/log"
)

const (
	sonicRuntimeSettingKey      = "runtime.sonic.status"
	sonicRuntimeHeartbeatPeriod = 5 * time.Second
	sonicRuntimeStaleAfter      = 20 * time.Second

	// The scheduled-task ID that owns per-track sonic progress events
	// (see internal/taskdefs/registry.go).
	sonicTaskID = "analyze_music_facets"
)

// SonicRuntimeStatus is the worker process's published snapshot of the
// sonic-analysis model runtime. The Holder that actually loads the model
// lives only in the dedicated worker process, while the API process serves
// /api/admin/sonicanalysis/status; without this row the API process can
// only report its own never-borrowed Holder — permanently "cold" even while
// the worker's GPU is busy. Same system_settings pattern as
// runtime.worker.status.
type SonicRuntimeStatus struct {
	HeartbeatAt  time.Time            `json:"heartbeat_at"`
	Running      bool                 `json:"running"`
	Holder       sonicanalysis.Status `json:"holder"`
	CurrentItem  string               `json:"current_item,omitempty"`
	CurrentStage string               `json:"current_stage,omitempty"`
}

// Fresh reports whether this snapshot should be trusted over the local
// process's own Holder. Running=false (graceful worker shutdown) and stale
// heartbeats (worker crash) both fall back to local state.
func (s SonicRuntimeStatus) Fresh(now time.Time) bool {
	if !s.Running || s.HeartbeatAt.IsZero() || now.Before(s.HeartbeatAt) {
		return false
	}
	return now.Sub(s.HeartbeatAt) <= sonicRuntimeStaleAfter
}

// SonicRuntimeStatus reads the worker's most recent sonic-runtime snapshot.
// A missing row is a normal first-start state and returns an empty status.
func (a *App) SonicRuntimeStatus(ctx context.Context) (SonicRuntimeStatus, error) {
	raw, err := sqlc.New(a.db).GetSystemSetting(ctx, sonicRuntimeSettingKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SonicRuntimeStatus{}, nil
		}
		return SonicRuntimeStatus{}, fmt.Errorf("read sonic runtime status: %w", err)
	}

	var status SonicRuntimeStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return SonicRuntimeStatus{}, fmt.Errorf("decode sonic runtime status: %w", err)
	}
	return status, nil
}

// StartSonicRuntimeHeartbeat publishes the worker's live Holder state until
// ctx is cancelled. On graceful shutdown it writes one final Running=false
// snapshot; after a crash the heartbeat expires naturally.
func (a *App) StartSonicRuntimeHeartbeat(ctx context.Context) {
	if a.sonicHolder == nil {
		return
	}
	publish := func(writeCtx context.Context, running bool) {
		status := SonicRuntimeStatus{
			HeartbeatAt: time.Now().UTC(),
			Running:     running,
			Holder:      a.sonicHolder.Status(),
		}
		// A held lease means a track (or centroid batch) is being worked
		// on right now — attach the broadcaster's last progress payload so
		// HTTP pollers see it without waiting for the next WS event.
		if status.Holder.Refs > 0 {
			if p, ok := a.taskProgress.Current(sonicTaskID); ok {
				status.CurrentItem = p.CurrentItem
				status.CurrentStage = p.CurrentStage
			}
		}
		raw, err := json.Marshal(status)
		if err != nil {
			log.Error().Err(err).Msg("failed to encode sonic runtime heartbeat")
			return
		}
		if err := sqlc.New(a.db).UpsertSystemSetting(writeCtx, sqlc.UpsertSystemSettingParams{
			Key:   sonicRuntimeSettingKey,
			Value: raw,
		}); err != nil && writeCtx.Err() == nil {
			log.Warn().Err(err).Msg("failed to publish sonic runtime heartbeat")
		}
	}

	publish(ctx, true)
	go func() {
		ticker := time.NewTicker(sonicRuntimeHeartbeatPeriod)
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
