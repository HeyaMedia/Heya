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
	// --- Security posture + process-local protection telemetry ---
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/security", "get-admin-security", "Security posture and recent protection events", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[service.SecurityStatus], error) {
			return noStoreJSON(app.SecurityStatus(ctx)), nil
		})
	huma.Register(api, adminSecured(op(http.MethodPut, "/api/admin/security/trusted-networks", "set-admin-trusted-networks", "Apply the trusted direct-peer CIDR allowlist", "Admin")),
		func(ctx context.Context, in *struct {
			Body struct {
				Networks []string `json:"networks" maxItems:"64" doc:"Direct-peer IP addresses or CIDRs that bypass WAF inspection and authentication attempt buckets"`
			}
		}) (*JSONOutput[service.TrustedNetworksStatus], error) {
			status, err := app.SaveAndApplyTrustedNetworks(ctx, in.Body.Networks)
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return noStoreJSON(status), nil
		})

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
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/sonicanalysis/status", "sonic-analysis-status", "Sonic-analysis runtime status", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[map[string]any], error) {
			return noStoreJSON(collectSonicAnalysisStatus(ctx, app)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/sonicanalysis/settings", "get-sonic-analysis-settings", "Sonic-analysis settings", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[service.SonicAnalysisSettings], error) {
			return noStoreJSON(app.SonicAnalysisSettings(ctx)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/admin/sonicanalysis/settings", "set-sonic-analysis-settings", "Update sonic-analysis settings", "Admin")),
		func(ctx context.Context, in *struct {
			Body service.SonicAnalysisSettings
		}) (*JSONOutput[sonicSaveBody], error) {
			previous := app.SonicAnalysisSettings(ctx)
			if err := app.SetSonicAnalysisSettings(ctx, in.Body); err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			restartRequired := previous.PreprocessAhead != in.Body.PreprocessAhead ||
				previous.GPUWorkers != in.Body.GPUWorkers
			applied := true
			if err := app.ReconfigureSonicAnalysisAnalyzer(ctx); err != nil {
				if errors.Is(err, service.ErrSonicBusy) {
					applied = false
				} else {
					return nil, huma.Error500InternalServerError(err.Error())
				}
			}
			return &JSONOutput[sonicSaveBody]{Body: sonicSaveBody{
				Status:          "saved",
				Applied:         applied,
				RestartRequired: restartRequired,
			}}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/admin/sonicanalysis/fetch", "trigger-sonic-fetch", "Kick off the model fetcher", "Admin")),
		func(ctx context.Context, _ *struct{}) (*StatusOutput, error) {
			f := app.ModelFetcher()
			if f == nil {
				return nil, huma.Error503ServiceUnavailable("model fetcher not available")
			}
			if !app.TriggerSonicAnalysisFetch() {
				return nil, huma.Error503ServiceUnavailable("application is shutting down")
			}
			return statusOK("started"), nil
		})

	// --- Recommendations ML (embedding) engine ---
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/recommendations-ml/status", "recommendations-ml-status", "Embedding recommendation engine status", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[map[string]any], error) {
			return noStoreJSON(collectRecommendationsMLStatus(ctx, app)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodGet, "/api/admin/recommendations-ml/settings", "get-recommendations-ml-settings", "Embedding engine settings", "Admin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[service.RecommendationsMLSettings], error) {
			return noStoreJSON(app.RecommendationsMLSettings(ctx)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/admin/recommendations-ml/settings", "set-recommendations-ml-settings", "Update embedding engine settings", "Admin")),
		func(ctx context.Context, in *struct {
			Body service.RecommendationsMLSettings
		}) (*StatusOutput, error) {
			if err := app.SetRecommendationsMLSettings(ctx, in.Body); err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			return statusOK("saved"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/admin/recommendations-ml/backfill", "recommendations-ml-backfill", "Re-embed the catalog", "Admin")),
		func(ctx context.Context, _ *struct{}) (*StatusOutput, error) {
			if !app.TriggerRecommendationsMLBackfill(false) {
				return nil, huma.Error503ServiceUnavailable("application is shutting down")
			}
			return statusOK("started"), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/admin/recommendations-ml/fetch", "recommendations-ml-fetch", "Download the embedding model", "Admin")),
		func(ctx context.Context, _ *struct{}) (*StatusOutput, error) {
			if !app.TriggerRecommendationsMLFetch() {
				return nil, huma.Error503ServiceUnavailable("application is shutting down")
			}
			return statusOK("started"), nil
		})

	// --- Logs ---
	// Always register the route so it appears in /api/docs and the typed TS
	// client; gate on `buf` at request time instead of registration time. A
	// nil buf returns an empty slice so callers can't tell the difference
	// between "no logs yet" and "ring buffer disabled".
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/logs", "get-logs", "Recent log entries", "Admin")),
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
	Status          string `json:"status"`
	Applied         bool   `json:"applied" doc:"Whether live-applicable settings were applied or queued for the next idle window"`
	RestartRequired bool   `json:"restart_required" doc:"Whether pipeline concurrency changed and needs a worker restart"`
}
