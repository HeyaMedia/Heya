package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/service"
)

// /api/me/music-services — per-user ListenBrainz / Last.fm links: credentials,
// outbound scrobbling toggles, and listen-history imports. All secured (each
// user manages their own links); the only server-level piece is the Last.fm
// app key pair from env.
func registerMusicServicesRoutes(api huma.API, app *service.App) {
	huma.Register(api, secured(op(http.MethodGet, "/api/me/music-services", "list-music-services", "Linked external music services", "Me")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[musicServicesBody], error) {
			views, err := app.ListUserMusicServices(ctx, userFrom(ctx).ID)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
			return noStoreJSON(musicServicesBody{Services: views}), nil
		})

	huma.Register(api, secured(op(http.MethodPut, "/api/me/music-services/{service}", "set-music-service", "Update an external music service link", "Me")),
		func(ctx context.Context, in *struct {
			Service string `path:"service" enum:"listenbrainz,lastfm"`
			Body    service.MusicServiceUpdate
		}) (*JSONOutput[service.MusicServiceView], error) {
			view, err := app.SetUserMusicService(ctx, userFrom(ctx).ID, in.Service, in.Body)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(view), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/music-services/{service}/import", "start-listen-import", "Import listen history into play events", "Me")),
		func(ctx context.Context, in *struct {
			Service string `path:"service" enum:"listenbrainz,lastfm"`
		}) (*StatusOutput, error) {
			if err := app.StartListenImport(ctx, userFrom(ctx).ID, in.Service); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return statusOK("importing"), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/music-services/lastfm/auth-start", "lastfm-auth-start", "Begin the Last.fm connect flow", "Me")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[lastfmAuthStartBody], error) {
			authURL, token, err := app.LastfmAuthStart(ctx)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(lastfmAuthStartBody{AuthURL: authURL, Token: token}), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/music-services/lastfm/auth-complete", "lastfm-auth-complete", "Finish the Last.fm connect flow", "Me")),
		func(ctx context.Context, in *struct {
			Body struct {
				Token string `json:"token" minLength:"1" maxLength:"128"`
			}
		}) (*JSONOutput[service.MusicServiceView], error) {
			view, err := app.LastfmAuthComplete(ctx, userFrom(ctx).ID, in.Body.Token)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(view), nil
		})
}

type musicServicesBody struct {
	Services []service.MusicServiceView `json:"services"`
}

type lastfmAuthStartBody struct {
	AuthURL string `json:"auth_url"`
	Token   string `json:"token"`
}
