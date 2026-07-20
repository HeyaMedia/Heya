package server

import (
	"context"
	"errors"
	"net/http"
	"time"

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

	// Per-user Jellyfin PIN lifecycle. The PIN IS returned by GET — that's
	// the feature: the user reads it off this page to type into a TV client.
	// It is scoped to the Jellyfin login only (rotating/revoking it never
	// touches the real account password).
	huma.Register(api, secured(op(http.MethodGet, "/api/me/jellyfin-credential", "get-jellyfin-credential", "Current user's Jellyfin PIN", "Jellyfin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[jellyfinCredentialBody], error) {
			cred, err := app.GetJellyfinCredential(ctx, userFrom(ctx).ID)
			if err != nil {
				if errors.Is(err, service.ErrJellyfinNoCredential) {
					return nil, huma.Error404NotFound("no jellyfin pin — create one first")
				}
				return nil, humaServiceError(err)
			}
			return noStoreJSON(jellyfinCredentialView(cred)), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/jellyfin-credential", "rotate-jellyfin-credential", "Create or rotate the Jellyfin PIN", "Jellyfin")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[jellyfinCredentialBody], error) {
			cred, err := app.RotateJellyfinCredential(ctx, userFrom(ctx).ID)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(jellyfinCredentialView(cred)), nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/jellyfin-credential", "revoke-jellyfin-credential", "Revoke the Jellyfin PIN", "Jellyfin")),
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			if err := app.RevokeJellyfinCredential(ctx, userFrom(ctx).ID); err != nil {
				return nil, humaServiceError(err)
			}
			return &struct{}{}, nil
		})
}

type jellyfinConfigBody struct {
	Enabled bool `json:"enabled"`
}

type jellyfinCredentialBody struct {
	Pin        string     `json:"pin"`
	CreatedAt  time.Time  `json:"created_at"`
	RotatedAt  time.Time  `json:"rotated_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func jellyfinCredentialView(c service.JellyfinCredential) jellyfinCredentialBody {
	return jellyfinCredentialBody{
		Pin:        c.PIN,
		CreatedAt:  c.CreatedAt,
		RotatedAt:  c.RotatedAt,
		LastUsedAt: c.LastUsedAt,
	}
}
