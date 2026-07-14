package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/remote"
	"github.com/karbowiak/heya/internal/service"
	"github.com/rs/zerolog/log"
)

// registerRemoteRoutes mounts /api/remote/* (admin) plus the public
// GET /api/connectivity/probe counterpart of the heya.media check service.
//
// Persistence model mirrors tailscale: UI-editable fields live in
// system_settings under "remote.*" keys, env locks win (409 on write).
// The manager is nil under --dev-backend (remote access is production-only)
// and in passive mode mutations are refused.
func registerRemoteRoutes(api huma.API, app *service.App, _ *config.Config) {
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/remote/status", "remote-status", "Remote access status", "Remote")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[remoteStatusBody], error) {
			return noStoreJSON(buildRemoteStatusBody(app)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/remote/config", "set-remote-config", "Apply remote access config", "Remote")),
		func(ctx context.Context, in *struct {
			Body remoteConfigPayload
		}) (*JSONOutput[statusBody], error) {
			if err := remoteReadOnly(app.ConfigSnapshot()); err != nil {
				return nil, err
			}
			if err := app.SaveRemoteSettings(ctx, service.RemoteUpdate{
				Enabled:     in.Body.Enabled,
				Port:        in.Body.Port,
				ACMEEmail:   in.Body.ACMEEmail,
				DNSProvider: in.Body.DNSProvider,
				DNSToken:    in.Body.DNSToken,
				Domain:      in.Body.Domain,
				Subdomain:   in.Body.Subdomain,
			}); err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			cur := app.ConfigSnapshot().Remote
			log.Info().
				Bool("enabled", cur.Enabled.Value).
				Str("provider", cur.DNSProvider.Value).
				Str("domain", cur.Domain.Value).
				Msg("remote access config saved")

			mgr := app.Remote()
			if mgr == nil {
				return nil, huma.Error400BadRequest("remote access is production-only (unavailable under --dev-backend)")
			}
			if !cur.Enabled.Value {
				// Backgrounded: Disable unmaps the router port and must not
				// deadlock a request that arrived over the remote listener.
				go func() { _ = mgr.Disable() }()
				return &JSONOutput[statusBody]{Body: statusBody{Status: "disabling"}}, nil
			}
			// Enable re-resolves the runtime config (may mint + persist the
			// random port on first run) and rebuilds the stack. Progress
			// streams over the remote.status WS event.
			go func() {
				ctx := app.LifetimeContext()
				rc, err := app.RemoteRuntimeConfig(ctx)
				if err != nil {
					log.Warn().Err(err).Msg("remote runtime config failed")
					return
				}
				if err := mgr.Enable(ctx, rc); err != nil {
					log.Warn().Err(err).Msg("remote access enable failed")
				}
			}()
			return &JSONOutput[statusBody]{Body: statusBody{Status: "enabling"}}, nil
		})

	huma.Register(api, adminSecured(op(http.MethodPost, "/api/remote/check", "remote-check", "Re-run the reachability check", "Remote")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[remoteStatusBody], error) {
			if err := remoteReadOnly(app.ConfigSnapshot()); err != nil {
				return nil, err
			}
			mgr := app.Remote()
			if mgr == nil {
				return nil, huma.Error400BadRequest("remote access is production-only (unavailable under --dev-backend)")
			}
			if _, err := mgr.Recheck(ctx); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(buildRemoteStatusBody(app)), nil
		})

	// Public by design: the heya.media prober dials back in from the internet
	// with no credentials and reads the short-lived challenge nonce minted
	// for the in-flight check. 404 whenever no check is running, so the
	// standing exposure is nil (see the connectivity-check spec).
	huma.Register(api, op(http.MethodGet, "/api/connectivity/probe", "connectivity-probe", "Reachability check challenge echo", "Remote"),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[probeBody], error) {
			mgr := app.Remote()
			if mgr == nil {
				return nil, huma.Error404NotFound("no check in flight")
			}
			challenge, ok := mgr.ProbeChallenge()
			if !ok {
				return nil, huma.Error404NotFound("no check in flight")
			}
			return noStoreJSON(probeBody{Challenge: challenge}), nil
		})
}

// remoteReadOnly gates mutating endpoints in passive mode — a borrowed
// (usually production) DB must not have its remote.* settings rewritten,
// and this process must not open ports for a server it isn't.
func remoteReadOnly(cfg *config.Config) error {
	if cfg != nil && cfg.PassiveMode.Value {
		return huma.Error403Forbidden("remote access is read-only in passive mode (HEYA_PASSIVE_MODE)")
	}
	return nil
}

type remoteConfigPayload struct {
	Enabled bool `json:"enabled"`
	// Port 0 keeps the current (or auto-generates a sticky random) port.
	Port        int    `json:"port,omitempty" doc:"External+listener port; 0 = keep current / auto-generate"`
	ACMEEmail   string `json:"acme_email,omitempty"`
	DNSProvider string `json:"dns_provider,omitempty" enum:",desec,duckdns,cloudflare" doc:"DNS provider for hostnames + certificates"`
	// DNSToken is write-only: never echoed back; empty keeps the stored one.
	DNSToken  string `json:"dns_token,omitempty" doc:"Provider API token (write-only; empty keeps existing)"`
	Domain    string `json:"domain,omitempty" doc:"Zone managed at the provider (myname.dedyn.io, example.com)"`
	Subdomain string `json:"subdomain,omitempty" doc:"Optional label under the domain (heya → wan.heya.example.com)"`
}

type remoteConfigView struct {
	Enabled     bool   `json:"enabled"`
	Port        int    `json:"port"`
	ACMEEmail   string `json:"acme_email,omitempty"`
	DNSProvider string `json:"dns_provider,omitempty"`
	TokenSet    bool   `json:"token_set"`
	Domain      string `json:"domain,omitempty"`
	Subdomain   string `json:"subdomain,omitempty"`
}

type remoteStatusBody struct {
	Available bool                 `json:"available"`
	Config    remoteConfigView     `json:"config"`
	Status    *remote.RemoteStatus `json:"status,omitempty"`
	Message   string               `json:"message,omitempty"`
}

type probeBody struct {
	Challenge string `json:"challenge"`
}

func buildRemoteStatusBody(app *service.App) remoteStatusBody {
	cur := app.ConfigSnapshot().Remote
	body := remoteStatusBody{
		Available: app.Remote() != nil,
		Config: remoteConfigView{
			Enabled:     cur.Enabled.Value,
			Port:        cur.Port.Value,
			ACMEEmail:   cur.ACMEEmail.Value,
			DNSProvider: cur.DNSProvider.Value,
			TokenSet:    cur.DNSToken.Value != "",
			Domain:      cur.Domain.Value,
			Subdomain:   cur.Subdomain.Value,
		},
	}
	if mgr := app.Remote(); mgr != nil {
		st := mgr.Status()
		body.Status = &st
	} else {
		body.Message = "Remote access runs in production mode only (heya serve without --dev-backend)."
	}
	return body
}
