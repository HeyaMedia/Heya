package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/service"
)

// registerAdminRoutes mounts the admin-only system-management surface:
// system settings KV, sonic-analysis settings + status, log inspection,
// and the global config-provenance endpoint.
//
// The shared Huma admin middleware enforces user.IsAdmin on operations
// declared with adminSecured(); regular bearer auth covers the rest.
func registerAdminRoutes(api huma.API, app *service.App, buf *logbuf.RingBuffer) {
	// --- Config provenance (drives the disabled-when-env UI behaviour) ---
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/config/sources", "get-config-sources", "Per-field config provenance", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[service.ConfigSources], error) {
			return noStoreJSON(app.ConfigSources(ctx)), nil
		})

	// --- System settings (admin-only KV used for OpenSubtitles, etc.) ---
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/system-settings/{key}", "get-system-setting", "Get a system setting", "Admin")),
		func(ctx context.Context, in *struct {
			Key string `path:"key" pattern:"^[a-z][a-z0-9_.-]*$" maxLength:"64" doc:"Setting key (lowercase, dots/dashes/underscores allowed)"`
		}) (*JSONOutput[systemSettingBody], error) {
			val, err := app.GetSystemSetting(ctx, in.Key)
			body := systemSettingBody{Key: in.Key}
			if err == nil {
				body.Value = val
			}
			return noStoreJSON(body), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/system-settings/{key}", "set-system-setting", "Update a system setting", "Admin")),
		func(ctx context.Context, in *struct {
			Key  string `path:"key" pattern:"^[a-z][a-z0-9_.-]*$" maxLength:"64" doc:"Setting key"`
			Body struct {
				Value json.RawMessage `json:"value" doc:"JSON-encoded value"`
			}
		}) (*StatusOutput, error) {
			if envVar, locked := app.SystemSettingEnvLock(in.Key); locked {
				return nil, huma.Error409Conflict("setting " + in.Key + " is locked by environment variable " + envVar)
			}
			if err := app.SetSystemSetting(ctx, in.Key, in.Body.Value); err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return statusOK("saved"), nil
		})

	// --- Sonic analysis ---
	huma.Register(api, secured(op(http.MethodGet, "/api/admin/sonicanalysis/status", "sonic-analysis-status", "Sonic-analysis runtime status", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[map[string]any], error) {
			return noStoreJSON(collectSonicAnalysisStatus(app)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/sonicanalysis/settings", "get-sonic-analysis-settings", "Sonic-analysis settings", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[service.SonicAnalysisSettings], error) {
			return noStoreJSON(app.SonicAnalysisSettings(ctx)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/admin/sonicanalysis/settings", "set-sonic-analysis-settings", "Update sonic-analysis settings", "Admin")),
		func(ctx context.Context, in *struct {
			Body service.SonicAnalysisSettings
		}) (*JSONOutput[sonicSaveBody], error) {
			if err := app.SetSonicAnalysisSettings(ctx, in.Body); err != nil {
				if lerr, ok := err.(*service.ErrFieldLockedByEnv); ok {
					return nil, huma.Error409Conflict(lerr.Error())
				}
				return nil, huma.Error400BadRequest(err.Error())
			}
			applied := true
			if err := app.ReconfigureSonicAnalysisAnalyzer(ctx); err != nil {
				if errors.Is(err, service.ErrSonicBusy) {
					applied = false
				} else {
					return nil, huma.Error500InternalServerError(err.Error())
				}
			}
			return &JSONOutput[sonicSaveBody]{Body: sonicSaveBody{Status: "saved", Applied: applied}}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/admin/sonicanalysis/fetch", "trigger-sonic-fetch", "Kick off the model fetcher", "Admin")),
		func(ctx context.Context, _ *struct{}) (*StatusOutput, error) {
			f := app.ModelFetcher()
			if f == nil {
				return nil, huma.Error503ServiceUnavailable("model fetcher not available")
			}
			// Detach from the request context — fetches take minutes; let the
			// request return immediately. Bound to app lifetime so a graceful
			// shutdown cancels in-flight downloads.
			go func() { _ = f.Run(app.LifetimeContext()) }()
			return statusOK("started"), nil
		})

	// --- Logs ---
	// Always register the route so it appears in /api/docs and the typed TS
	// client; gate on `buf` at request time instead of registration time. A
	// nil buf returns an empty slice so callers can't tell the difference
	// between "no logs yet" and "ring buffer disabled".
	huma.Register(api, secured(op(http.MethodGet, "/api/logs", "get-logs", "Recent log entries", "Admin")),
		func(ctx context.Context, in *struct {
			N     int    `query:"n" minimum:"1" maximum:"1000" default:"200" doc:"Number of entries"`
			Level string `query:"level" maxLength:"16" doc:"Filter by log level (trace|debug|info|warn|error)"`
		}) (*JSONOutput[[]logbuf.Entry], error) {
			if buf == nil {
				return noStoreJSON([]logbuf.Entry{}), nil
			}
			entries := buf.Recent(in.N)
			if in.Level != "" {
				filtered := make([]logbuf.Entry, 0, len(entries))
				for _, e := range entries {
					if e.Level == in.Level {
						filtered = append(filtered, e)
					}
				}
				entries = filtered
			}
			return noStoreJSON(entries), nil
		})
}

type systemSettingBody struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value,omitempty"`
}

type sonicSaveBody struct {
	Status  string `json:"status"`
	Applied bool   `json:"applied" doc:"Whether the new settings were live-applied or queued for next idle window"`
}
