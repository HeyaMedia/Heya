package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/service"
)

// registerJellyfinConfigRoutes mounts /api/jellyfin/config — the Settings UI
// backing for the Jellyfin-compatible API toggle. The compat surface itself
// is NOT huma (see internal/jellyfin); only this on/off knob is, so the
// typed TS client can drive the settings page. Persistence model mirrors
// tailscale: system_settings key "jellyfin.enabled", env-locked writes 409.
// No subsystem kick needed — the jellyfin middleware reads the snapshot per
// request, so flips are live immediately.
func registerJellyfinConfigRoutes(api huma.API, app *service.App) {
	huma.Register(api, secured(op(http.MethodGet, "/api/jellyfin/config", "jellyfin-config", "Jellyfin-compatible API config", "Jellyfin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[jellyfinConfigBody], error) {
			return noStoreJSON(jellyfinConfigBody{
				Enabled: app.JellyfinEnabled(),
			}), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/jellyfin/config", "set-jellyfin-config", "Apply Jellyfin-compatible API config", "Jellyfin")),
		func(ctx context.Context, in *struct {
			Body jellyfinConfigBody
		}) (*JSONOutput[jellyfinConfigBody], error) {
			if err := app.SaveJellyfinSettings(ctx, in.Body.Enabled); err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(jellyfinConfigBody{Enabled: app.JellyfinEnabled()}), nil
		})
}

type jellyfinConfigBody struct {
	Enabled bool `json:"enabled"`
}
