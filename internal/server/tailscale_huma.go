package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/service"
	tsnetwrap "github.com/karbowiak/heya/internal/tailscale"
	"github.com/rs/zerolog/log"
)

// registerTailscaleRoutes mounts /api/tailscale/*. Tailscale is optional and
// may be off entirely; the manager pointer is nil in that case and individual
// operations return 400.
//
// Persistence model: the four UI-editable fields (enabled, https, funnel,
// hostname) live in system_settings under "tailscale.*" keys. Env-set values
// take precedence and lock the field — PUTs to env-locked fields return 409.
// AuthKey and StateDir are env-only and never persist anywhere.
func registerTailscaleRoutes(api huma.API, app *service.App, _ *config.Config) {
	huma.Register(api, secured(op(http.MethodGet, "/api/tailscale/status", "tailscale-status", "Tailscale node status", "Tailscale")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[tailscaleStatusBody], error) {
			cur := app.ConfigSnapshot().Tailscale
			body := tailscaleStatusBody{
				Enabled: cur.Enabled.Value,
				Config: &tailscaleConfigPayload{
					Enabled:  cur.Enabled.Value,
					Hostname: cur.Hostname.Value,
					HTTPS:    cur.HTTPS.Value,
					Funnel:   cur.Funnel.Value,
				},
			}
			if ts := app.Tailscale(); ts != nil {
				st := ts.Status()
				body.Status = &st
			} else {
				body.Message = "Tailscale manager not initialized — restart the server."
			}
			return noStoreJSON(body), nil
		})

	// Raw ipnstate is large and changes on every peer-tick; no-store so the
	// admin debug panel always sees ground truth.
	huma.Register(api, secured(op(http.MethodGet, "/api/tailscale/raw", "tailscale-raw-status", "Raw ipnstate dump", "Tailscale")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[any], error) {
			ts := app.Tailscale()
			if ts == nil {
				return nil, huma.Error400BadRequest("Tailscale is not running")
			}
			st, err := ts.RawStatus(ctx)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON[any](st), nil
		})

	huma.Register(api, secured(op(http.MethodPut, "/api/tailscale/config", "set-tailscale-config", "Apply Tailscale config", "Tailscale")),
		func(ctx context.Context, in *struct {
			Body tailscaleConfigPayload
		}) (*JSONOutput[statusBody], error) {
			if err := app.SaveTailscaleSettings(ctx, service.TailscaleUpdate{
				Enabled:  in.Body.Enabled,
				HTTPS:    in.Body.HTTPS,
				Funnel:   in.Body.Funnel,
				Hostname: in.Body.Hostname,
			}); err != nil {
				if lerr, ok := err.(*service.ErrFieldLockedByEnv); ok {
					return nil, huma.Error409Conflict(lerr.Error())
				}
				return nil, huma.Error500InternalServerError(err.Error())
			}
			cur := app.ConfigSnapshot().Tailscale
			log.Info().
				Bool("enabled", cur.Enabled.Value).
				Bool("https", cur.HTTPS.Value).
				Bool("funnel", cur.Funnel.Value).
				Str("hostname", cur.Hostname.Value).
				Msg("tailscale config saved")

			ts := app.Tailscale()
			if ts == nil {
				return nil, huma.Error500InternalServerError("tailscale manager not initialized")
			}

			if !cur.Enabled.Value {
				// Backgrounded to avoid deadlocking on http.Server.Shutdown
				// when the request itself came in over a tsnet listener.
				go func() { _ = ts.Disable() }()
				return &JSONOutput[statusBody]{Body: statusBody{Status: "disabling"}}, nil
			}

			// Enable is potentially long-running (90s timeout on first auth).
			// Fire and forget — the UI picks up the login URL via the
			// tailscale.status WS event. Bound to app lifetime so shutdown
			// cancels an in-flight tsnet bring-up.
			go func() {
				_ = ts.Enable(app.LifetimeContext(), tsnetwrap.Config{
					Enabled:  true,
					Hostname: cur.Hostname.Value,
					AuthKey:  cur.AuthKey.Value,
					StateDir: cur.StateDir.Value,
					HTTPS:    cur.HTTPS.Value,
					Funnel:   cur.Funnel.Value,
				})
			}()
			return &JSONOutput[statusBody]{Body: statusBody{Status: "enabling"}}, nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/tailscale/funnel", "toggle-tailscale-funnel", "Toggle Funnel", "Tailscale")),
		func(ctx context.Context, in *struct {
			Body struct {
				Enabled bool `json:"enabled"`
			}
		}) (*JSONOutput[funnelBody], error) {
			ts := app.Tailscale()
			if ts == nil {
				return nil, huma.Error400BadRequest("Tailscale is not running")
			}
			cur := app.ConfigSnapshot().Tailscale
			if err := app.SaveTailscaleSettings(ctx, service.TailscaleUpdate{
				Enabled:  cur.Enabled.Value,
				HTTPS:    cur.HTTPS.Value,
				Funnel:   in.Body.Enabled,
				Hostname: cur.Hostname.Value,
			}); err != nil {
				if lerr, ok := err.(*service.ErrFieldLockedByEnv); ok {
					return nil, huma.Error409Conflict(lerr.Error())
				}
				log.Warn().Err(err).Msg("failed to persist tailscale funnel preference")
			} else {
				log.Info().Bool("funnel", in.Body.Enabled).Msg("tailscale funnel preference saved")
			}
			// Backgrounded for the same deadlock reason as Disable.
			go func() { _ = ts.SetFunnel(app.LifetimeContext(), in.Body.Enabled) }()
			return &JSONOutput[funnelBody]{Body: funnelBody{Funnel: in.Body.Enabled}}, nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/tailscale/logout", "tailscale-logout", "Log this node out of the tailnet", "Tailscale")),
		func(ctx context.Context, _ *struct{}) (*StatusOutput, error) {
			ts := app.Tailscale()
			if ts == nil {
				return nil, huma.Error400BadRequest("Tailscale is not running")
			}
			cur := app.ConfigSnapshot().Tailscale
			_ = app.SaveTailscaleSettings(ctx, service.TailscaleUpdate{
				Enabled:  false,
				HTTPS:    cur.HTTPS.Value,
				Funnel:   cur.Funnel.Value,
				Hostname: cur.Hostname.Value,
			})
			go func() { _ = ts.Logout(app.LifetimeContext()) }()
			return statusOK("logging out"), nil
		})
}

type tailscaleConfigPayload struct {
	Enabled  bool   `json:"enabled"`
	Hostname string `json:"hostname"`
	HTTPS    bool   `json:"https"`
	Funnel   bool   `json:"funnel"`
}

type tailscaleStatusBody struct {
	Enabled bool                    `json:"enabled"`
	Config  *tailscaleConfigPayload `json:"config,omitempty"`
	Status  *tsnetwrap.Status       `json:"status,omitempty"`
	Message string                  `json:"message,omitempty"`
}

type statusBody struct {
	Status string `json:"status"`
}

type funnelBody struct {
	Funnel bool `json:"funnel"`
}
