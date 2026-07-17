package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
)

// registerSystemRoutes wires the always-on bookkeeping endpoints: health
// (live + ready + legacy alias), watcher status. Anything that doesn't fit
// a richer domain bucket lives here.
func registerSystemRoutes(api huma.API, app *service.App) {
	// /api/health/live — process is up. Always 200; useful for very cheap
	// kube/systemd liveness probes that should NOT restart the pod just
	// because the database hiccupped.
	huma.Register(api, op(http.MethodGet, "/api/health/live", "health-live", "Liveness probe", "System"),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[liveBody], error) {
			return noStoreJSON(liveBody{Status: "ok"}), nil
		})

	// /api/health/ready — deep health snapshot. Returns per-component status so
	// the dashboard can show which subsystem is down while the API remains
	// reachable for diagnosis.
	huma.Register(api, op(http.MethodGet, "/api/health/ready", "health-ready", "Readiness probe with per-component status", "System"),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[readyBody], error) {
			body := readyBody{
				Status:     "ok",
				Components: collectHealthComponents(ctx, app),
			}
			for _, c := range body.Components {
				if !c.OK {
					body.Status = "degraded"
					break
				}
			}
			return noStoreJSON(body), nil
		})

	// /api/health — legacy alias for /live, kept so existing probes and the
	// FE health badge keep working. Includes the build version so the FE
	// can display it without a separate /api/version round-trip.
	huma.Register(api, op(http.MethodGet, "/api/health", "health", "Health check (alias for /health/live)", "System"),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[healthBody], error) {
			body := healthBody{Status: "ok", Database: "connected", Version: ui.Version}
			if app.DBPool() == nil {
				body.Database = "disconnected"
			} else if err := app.DBPool().Ping(ctx); err != nil {
				body.Database = "disconnected"
			}
			return noStoreJSON(body), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/watchers", "watcher-status", "Filesystem watcher status", "System")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[watcherStatusBody], error) {
			status, err := app.WorkerRuntimeStatus(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError("failed to read worker status", err)
			}
			body := watcherStatusBody{
				Watchers:     make([]watcherEntry, 0, len(status.Watchers)),
				WorkerOnline: status.Online(time.Now()),
			}
			if !status.HeartbeatAt.IsZero() {
				body.UpdatedAt = &status.HeartbeatAt
			}
			for _, watcher := range status.Watchers {
				body.Watchers = append(body.Watchers, watcherEntry{LibraryID: watcher.LibraryID, Path: watcher.Path})
			}
			body.Count = len(body.Watchers)
			return noStoreJSON(body), nil
		})
}

// collectHealthComponents pings the major subsystems and returns per-component
// status. Each check is best-effort and fast; nothing here should block more
// than a few hundred milliseconds.
func collectHealthComponents(ctx context.Context, app *service.App) []healthComponent {
	components := []healthComponent{
		dbComponent(ctx, app),
		workerComponent(ctx, app),
		transcoderComponent(app),
	}
	// Tailscale only reported when enabled — keeps the response clean for
	// the common no-tsnet case.
	if app.ConfigSnapshot().Tailscale.Enabled.Value {
		components = append(components, tailscaleComponent(app))
	}
	return components
}

func dbComponent(ctx context.Context, app *service.App) healthComponent {
	if err := app.DBPool().Ping(ctx); err != nil {
		return healthComponent{Name: "database", OK: false, Message: err.Error()}
	}
	return healthComponent{Name: "database", OK: true}
}

func workerComponent(ctx context.Context, app *service.App) healthComponent {
	status, err := app.WorkerRuntimeStatus(ctx)
	if err != nil {
		return healthComponent{Name: "worker", OK: false, Message: err.Error()}
	}
	if !status.Online(time.Now()) {
		if status.HeartbeatAt.IsZero() {
			return healthComponent{Name: "worker", OK: false, Message: "no worker heartbeat received"}
		}
		return healthComponent{Name: "worker", OK: false, Message: "worker heartbeat is stale or stopped"}
	}
	return healthComponent{Name: "worker", OK: true}
}

func transcoderComponent(app *service.App) healthComponent {
	// Transcoder is optional — ffmpeg may not be installed. Report present-or-
	// absent rather than a hard failure so a missing ffmpeg doesn't fail the
	// readiness probe on a music-only deployment.
	if app.TranscoderSessions() == nil {
		return healthComponent{Name: "transcoder", OK: true, Message: "disabled (ffmpeg not available)"}
	}
	return healthComponent{Name: "transcoder", OK: true}
}

func tailscaleComponent(app *service.App) healthComponent {
	ts := app.Tailscale()
	if ts == nil {
		return healthComponent{Name: "tailscale", OK: false, Message: "manager not initialised"}
	}
	st := ts.Status()
	if st.LastError != "" {
		return healthComponent{Name: "tailscale", OK: false, Message: st.LastError}
	}
	return healthComponent{Name: "tailscale", OK: true}
}

type liveBody struct {
	Status string `json:"status" example:"ok" doc:"Always 'ok' when the process is alive"`
}

type readyBody struct {
	Status     string            `json:"status" example:"ok" doc:"'ok' when all components healthy, 'degraded' otherwise"`
	Components []healthComponent `json:"components"`
}

type healthComponent struct {
	Name    string `json:"name" example:"database"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty" doc:"Populated only when OK is false (or when reporting an optional-but-disabled component)"`
}

type healthBody struct {
	Status   string `json:"status" example:"ok" doc:"Server status"`
	Database string `json:"database" example:"connected" doc:"Database connection status"`
	Version  string `json:"version" example:"dev" doc:"Build version (overridden at link time)"`
}

type watcherEntry struct {
	LibraryID int64  `json:"library_id"`
	Path      string `json:"path"`
}

type watcherStatusBody struct {
	Watchers     []watcherEntry `json:"watchers"`
	Count        int            `json:"count"`
	WorkerOnline bool           `json:"worker_online" doc:"Whether the dedicated worker heartbeat is current"`
	UpdatedAt    *time.Time     `json:"updated_at,omitempty" doc:"Time of the most recent worker heartbeat"`
}
