package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/discovery"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/ui"
	"github.com/rs/zerolog/log"
)

// registerDiscoveryRoutes mounts the LAN-discovery control plane
// (/api/discovery/*, admin) plus the public GET /api/server/info that a
// client calls to confirm what it found over mDNS.
//
// Persistence mirrors remote/tailscale: UI-editable fields live in
// system_settings under "discovery.*", env locks win (409 on write). The
// manager is non-nil in dev as well as production — mDNS is a LAN broadcast,
// not an exposure — but mutations are still refused in passive mode.
func registerDiscoveryRoutes(api huma.API, app *service.App) {
	huma.Register(api, adminSecured(op(http.MethodGet, "/api/discovery/status", "discovery-status", "LAN discovery (mDNS) status", "Discovery")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[discoveryStatusBody], error) {
			return noStoreJSON(buildDiscoveryStatusBody(app)), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/discovery/config", "set-discovery-config", "Apply LAN discovery config", "Discovery")),
		func(ctx context.Context, in *struct {
			Body discoveryConfigPayload
		}) (*JSONOutput[discoveryStatusBody], error) {
			if err := discoveryReadOnly(app.ConfigSnapshot()); err != nil {
				return nil, err
			}
			cur, err := app.SaveAndApplyDiscoverySettings(ctx, service.DiscoveryUpdate{
				Enabled: in.Body.Enabled,
				Name:    in.Body.Name,
			})
			if err != nil {
				return nil, humaServiceErrorStatus(err, http.StatusBadRequest)
			}
			log.Info().
				Bool("enabled", cur.Enabled.Value).
				Str("name", app.ServerDisplayName()).
				Msg("LAN discovery config saved")
			return noStoreJSON(buildDiscoveryStatusBody(app)), nil
		})

	// Public by design, and the counterpart of the mDNS record: a client that
	// browsed _heya._tcp has an address and a TXT blob, and needs to confirm
	// there is a real Heya server behind it before offering it to the user —
	// with no credentials, because it doesn't have any yet.
	//
	// Everything here is already discoverable by anyone who can reach the
	// port: the name and version are in the TXT record, the id is meaningless
	// without a login, and which auth flow to start is not a secret. No
	// library, user, or configuration detail belongs in this response.
	huma.Register(api, op(http.MethodGet, "/api/server/info", "server-info", "Public server identity for client discovery", "Discovery"),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[serverInfoBody], error) {
			return noStoreJSON(serverInfoBody{
				Product:     "heya",
				ID:          app.ServerID(ctx),
				Name:        app.ServerDisplayName(),
				Version:     ui.Version,
				APIBasePath: "/api",
				ServiceType: discovery.ServiceType,
			}), nil
		})
}

// discoveryReadOnly gates mutations in passive mode — a borrowed (usually
// production) DB must not have its discovery.* settings rewritten by a dev
// box that is only visiting.
func discoveryReadOnly(cfg *config.Config) error {
	if cfg != nil && cfg.PassiveMode.Value {
		return huma.Error403Forbidden("LAN discovery is read-only in passive mode (HEYA_PASSIVE_MODE)")
	}
	return nil
}

type discoveryConfigPayload struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name,omitempty" maxLength:"63" doc:"Name clients display; empty uses the system hostname"`
}

type discoveryConfigView struct {
	Enabled bool `json:"enabled"`
	// Name is the stored value (may be empty); DisplayName is what actually
	// goes on the wire once the hostname fallback is applied.
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Port        int    `json:"port" doc:"Advertised port; 0 means the port this server binds"`
	Host        string `json:"host,omitempty"`
	Addresses   string `json:"addresses,omitempty"`
	Interfaces  string `json:"interfaces,omitempty"`
}

type discoveryStatusBody struct {
	Available bool                           `json:"available"`
	Config    discoveryConfigView            `json:"config"`
	Status    *discovery.AdvertisementStatus `json:"status,omitempty"`
	Message   string                         `json:"message,omitempty"`
}

type serverInfoBody struct {
	Product     string `json:"product" example:"heya" doc:"Always 'heya' — lets a client reject a foreign service on the same port"`
	ID          string `json:"id" doc:"Stable per-install id; key saved-server entries on this"`
	Name        string `json:"name" doc:"Display name for the server"`
	Version     string `json:"version" example:"dev" doc:"Build version"`
	APIBasePath string `json:"api_base_path" example:"/api"`
	ServiceType string `json:"service_type" example:"_heya._tcp" doc:"DNS-SD service type this server advertises"`
}

func buildDiscoveryStatusBody(app *service.App) discoveryStatusBody {
	cur := app.ConfigSnapshot().Discovery
	body := discoveryStatusBody{
		Available: app.Discovery() != nil,
		Config: discoveryConfigView{
			Enabled:     cur.Enabled.Value,
			Name:        cur.Name.Value,
			DisplayName: app.ServerDisplayName(),
			Port:        cur.Port.Value,
			Host:        cur.Host.Value,
			Addresses:   cur.Addresses.Value,
			Interfaces:  cur.Interfaces.Value,
		},
	}
	if mgr := app.Discovery(); mgr != nil {
		st := mgr.Status()
		body.Status = &st
	} else {
		body.Message = "LAN discovery is owned by the server process (heya serve)."
	}
	return body
}
