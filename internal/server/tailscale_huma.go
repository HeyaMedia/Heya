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
			if err := tailscaleReadOnly(app.ConfigSnapshot()); err != nil {
				return nil, err
			}
			cur, err := app.SaveAndApplyTailscaleSettings(ctx, service.TailscaleUpdate{
				Enabled:  in.Body.Enabled,
				HTTPS:    in.Body.HTTPS,
				Funnel:   in.Body.Funnel,
				Hostname: in.Body.Hostname,
			})
			if err != nil {
				return nil, humaServiceError(err)
			}
			log.Info().
				Bool("enabled", cur.Enabled.Value).
				Bool("https", cur.HTTPS.Value).
				Bool("funnel", cur.Funnel.Value).
				Str("hostname", cur.Hostname.Value).
				Msg("tailscale config saved")

			if !cur.Enabled.Value {
				return &JSONOutput[statusBody]{Body: statusBody{Status: "disabling"}}, nil
			}
			return &JSONOutput[statusBody]{Body: statusBody{Status: "enabling"}}, nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/tailscale/funnel", "toggle-tailscale-funnel", "Toggle Funnel", "Tailscale")),
		func(ctx context.Context, in *struct {
			Body struct {
				Enabled bool `json:"enabled"`
			}
		}) (*JSONOutput[funnelBody], error) {
			if err := tailscaleReadOnly(app.ConfigSnapshot()); err != nil {
				return nil, err
			}
			cur := app.ConfigSnapshot().Tailscale
			if _, err := app.SaveAndApplyTailscaleSettings(ctx, service.TailscaleUpdate{
				Enabled:  cur.Enabled.Value,
				HTTPS:    cur.HTTPS.Value,
				Funnel:   in.Body.Enabled,
				Hostname: cur.Hostname.Value,
			}); err != nil {
				return nil, humaServiceError(err)
			}
			log.Info().Bool("funnel", in.Body.Enabled).Msg("tailscale funnel preference saved")
			return &JSONOutput[funnelBody]{Body: funnelBody{Funnel: in.Body.Enabled}}, nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/tailscale/logout", "tailscale-logout", "Log this node out of the tailnet", "Tailscale")),
		func(ctx context.Context, _ *struct{}) (*StatusOutput, error) {
			if err := tailscaleReadOnly(app.ConfigSnapshot()); err != nil {
				return nil, err
			}
			cur := app.ConfigSnapshot().Tailscale
			if err := app.SaveAndLogoutTailscale(ctx, service.TailscaleUpdate{
				Enabled:  false,
				HTTPS:    cur.HTTPS.Value,
				Funnel:   cur.Funnel.Value,
				Hostname: cur.Hostname.Value,
			}); err != nil {
				return nil, humaServiceError(err)
			}
			return statusOK("logging out"), nil
		})
}

// tailscaleReadOnly gates the mutating tailscale endpoints when the server is
// in passive mode. Passive mode is a guest on a borrowed (usually production)
// DB: persisting tailscale settings would mutate that DB (logout would even
// flip prod's enabled flag off), and bringing the node up would join the
// tailnet under the source server's identity — a node-name collision with the
// real server. The read-only status endpoints stay available; only the
// mutating ones are gated. Mirrors the boot-time guard in service.New that
// skips LoadTailscaleFromDB.
func tailscaleReadOnly(cfg *config.Config) error {
	if cfg != nil && cfg.PassiveMode.Value {
		return huma.Error403Forbidden("Tailscale is read-only in passive mode (HEYA_PASSIVE_MODE)")
	}
	return nil
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
