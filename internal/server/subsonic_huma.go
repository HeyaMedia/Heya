package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/service"
)

// registerSubsonicRoutes mounts the native-API side of the Subsonic compat
// feature: the Settings toggle (mirrors /api/jellyfin/config) and the
// per-user app-password lifecycle. The compat surface itself is NOT huma
// (see internal/subsonic); Subsonic clients never see these routes.
//
// The credential secret IS returned by GET — that's the feature: Subsonic
// token auth needs a shared secret both sides know, so the user must be
// able to read it back to type into a client. It is scoped to this API
// (rotating it never touches the real account password).
func registerSubsonicRoutes(api huma.API, app *service.App) {
	huma.Register(api, secured(op(http.MethodGet, "/api/subsonic/config", "subsonic-config", "Subsonic-compatible API config", "Subsonic")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[subsonicConfigBody], error) {
			return noStoreJSON(subsonicConfigBody{Enabled: app.SubsonicEnabled()}), nil
		})

	huma.Register(api, adminSecured(op(http.MethodPut, "/api/subsonic/config", "set-subsonic-config", "Apply Subsonic-compatible API config", "Subsonic")),
		func(ctx context.Context, in *struct {
			Body subsonicConfigBody
		}) (*JSONOutput[subsonicConfigBody], error) {
			if err := app.SaveSubsonicSettings(ctx, in.Body.Enabled); err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(subsonicConfigBody{Enabled: app.SubsonicEnabled()}), nil
		})

	huma.Register(api, secured(op(http.MethodGet, "/api/me/subsonic-credential", "get-subsonic-credential", "Current user's Subsonic app password", "Subsonic")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[subsonicCredentialBody], error) {
			cred, err := app.GetSubsonicCredential(ctx, userFrom(ctx).ID)
			if err != nil {
				if errors.Is(err, service.ErrSubsonicNoCredential) {
					return nil, huma.Error404NotFound("no subsonic credential — create one first")
				}
				return nil, humaServiceError(err)
			}
			return noStoreJSON(credentialBody(cred)), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/subsonic-credential", "rotate-subsonic-credential", "Create or rotate the Subsonic app password", "Subsonic")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[subsonicCredentialBody], error) {
			cred, err := app.RotateSubsonicCredential(ctx, userFrom(ctx).ID)
			if err != nil {
				return nil, humaServiceError(err)
			}
			return noStoreJSON(credentialBody(cred)), nil
		})

	huma.Register(api, secured(op(http.MethodDelete, "/api/me/subsonic-credential", "revoke-subsonic-credential", "Revoke the Subsonic app password", "Subsonic")),
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			if err := app.RevokeSubsonicCredential(ctx, userFrom(ctx).ID); err != nil {
				return nil, humaServiceError(err)
			}
			return &struct{}{}, nil
		})
}

type subsonicConfigBody struct {
	Enabled bool `json:"enabled"`
}

type subsonicCredentialBody struct {
	Secret     string     `json:"secret"`
	CreatedAt  time.Time  `json:"created_at"`
	RotatedAt  time.Time  `json:"rotated_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func credentialBody(c service.SubsonicCredential) subsonicCredentialBody {
	return subsonicCredentialBody{
		Secret:     c.Secret,
		CreatedAt:  c.CreatedAt,
		RotatedAt:  c.RotatedAt,
		LastUsedAt: c.LastUsedAt,
	}
}
