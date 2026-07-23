package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/rs/zerolog/log"
)

type ProcessRole string

const (
	ProcessRoleServer ProcessRole = "server"
	ProcessRoleWorker ProcessRole = "worker"

	workerRestartSettingKey = "runtime.restart.worker"
	restartPollPeriod       = time.Second
	restartResponseGrace    = 500 * time.Millisecond
)

// ProcessRestartRequest is persisted for the worker because the API and
// background runtime are separate processes. Acknowledgement is written
// before cancellation so a replacement worker never loops on a stale request.
type ProcessRestartRequest struct {
	ID             string      `json:"id"`
	Target         ProcessRole `json:"target"`
	RequestedAt    time.Time   `json:"requested_at"`
	AcknowledgedAt *time.Time  `json:"acknowledged_at,omitempty"`
	AcknowledgedBy string      `json:"acknowledged_by,omitempty"`
}

type ProcessRestartResult struct {
	Status    string `json:"status"`
	Target    string `json:"target"`
	RequestID string `json:"request_id,omitempty"`
}

// ConfigureProcessControl binds a long-lived command's cancellation function
// to the App. Calling it follows the ordinary graceful shutdown path; the
// external supervisor (Compose, Kubernetes, or AIO supervisord) owns restart.
func (a *App) ConfigureProcessControl(role ProcessRole, restart context.CancelFunc) {
	if a == nil {
		return
	}
	a.processControlMu.Lock()
	a.processRole = role
	a.processRestart = restart
	a.processControlMu.Unlock()
}

func (a *App) localRestart(role ProcessRole) (context.CancelFunc, bool) {
	if a == nil {
		return nil, false
	}
	a.processControlMu.RLock()
	defer a.processControlMu.RUnlock()
	if a.processRole != role || a.processRestart == nil {
		return nil, false
	}
	return a.processRestart, true
}

// RequestProcessRestart accepts server, worker, or all. Server shutdown is
// delayed briefly so the HTTP response can flush. Worker shutdown is delivered
// through Postgres and observed by the dedicated runtime even when it lives in
// another container or pod.
func (a *App) RequestProcessRestart(ctx context.Context, target string) (ProcessRestartResult, error) {
	switch target {
	case string(ProcessRoleServer):
		if err := a.scheduleLocalRestart(ProcessRoleServer); err != nil {
			return ProcessRestartResult{}, err
		}
		return ProcessRestartResult{Status: "accepted", Target: target}, nil
	case string(ProcessRoleWorker):
		id, err := a.queueWorkerRestart(ctx)
		if err != nil {
			return ProcessRestartResult{}, err
		}
		return ProcessRestartResult{Status: "accepted", Target: target, RequestID: id}, nil
	case "all":
		id, err := a.queueWorkerRestart(ctx)
		if err != nil {
			return ProcessRestartResult{}, err
		}
		if err := a.scheduleLocalRestart(ProcessRoleServer); err != nil {
			return ProcessRestartResult{}, err
		}
		return ProcessRestartResult{Status: "accepted", Target: target, RequestID: id}, nil
	default:
		return ProcessRestartResult{}, fmt.Errorf("invalid restart target %q", target)
	}
}

func (a *App) scheduleLocalRestart(role ProcessRole) error {
	restart, ok := a.localRestart(role)
	if !ok {
		return fmt.Errorf("%s process restart is unavailable in this runtime", role)
	}
	a.restartOnce.Do(func() {
		time.AfterFunc(restartResponseGrace, func() {
			log.Warn().Str("process", string(role)).Msg("admin-requested graceful restart")
			restart()
		})
	})
	return nil
}

func (a *App) queueWorkerRestart(ctx context.Context) (string, error) {
	if a == nil || a.db == nil {
		return "", errors.New("worker restart unavailable: database is not connected")
	}
	req := ProcessRestartRequest{
		ID:          uuid.NewString(),
		Target:      ProcessRoleWorker,
		RequestedAt: time.Now().UTC(),
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	if err := sqlc.New(a.db).UpsertSystemSetting(ctx, sqlc.UpsertSystemSettingParams{
		Key:   workerRestartSettingKey,
		Value: raw,
	}); err != nil {
		return "", fmt.Errorf("queue worker restart: %w", err)
	}
	return req.ID, nil
}

// StartProcessRestartWatcher polls the durable worker command channel. It is
// intentionally separate from the slower diagnostic heartbeat: restart should
// be responsive even when telemetry cadence changes.
func (a *App) StartProcessRestartWatcher(ctx context.Context) {
	restart, ok := a.localRestart(ProcessRoleWorker)
	if !ok || a.db == nil {
		return
	}
	a.startBackground(func() {
		workCtx, cancel := a.backgroundContext(ctx)
		defer cancel()
		ticker := time.NewTicker(restartPollPeriod)
		defer ticker.Stop()

		for {
			handled, err := a.consumeWorkerRestart(workCtx)
			if err != nil && workCtx.Err() == nil {
				log.Warn().Err(err).Msg("failed to poll worker restart request")
			}
			if handled {
				log.Warn().Msg("admin-requested graceful worker restart")
				restart()
				return
			}
			select {
			case <-workCtx.Done():
				return
			case <-ticker.C:
			}
		}
	})
}

func (a *App) consumeWorkerRestart(ctx context.Context) (bool, error) {
	raw, err := sqlc.New(a.db).GetSystemSetting(ctx, workerRestartSettingKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	var req ProcessRestartRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return false, fmt.Errorf("decode worker restart request: %w", err)
	}
	if req.ID == "" || req.Target != ProcessRoleWorker || req.AcknowledgedAt != nil {
		return false, nil
	}

	now := time.Now().UTC()
	req.AcknowledgedAt = &now
	req.AcknowledgedBy = fmt.Sprintf("pid:%d", os.Getpid())
	ack, err := json.Marshal(req)
	if err != nil {
		return false, err
	}
	tag, err := a.db.Exec(ctx, `
		UPDATE system_settings
		SET value = $1, updated_at = now()
		WHERE key = $2
		  AND value->>'id' = $3
		  AND value->>'acknowledged_at' IS NULL
	`, ack, workerRestartSettingKey, req.ID)
	if err != nil {
		return false, fmt.Errorf("acknowledge worker restart: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
