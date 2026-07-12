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

	huma.Register(api, secured(op(http.MethodGet, "/api/me/music-services/{service}/playlists", "list-external-playlists", "List playlists available for synchronization", "Me")),
		func(ctx context.Context, in *struct {
			Service string `path:"service" enum:"listenbrainz,lastfm"`
		}) (*JSONOutput[service.PlaylistServiceCatalog], error) {
			catalog, err := app.ListExternalPlaylists(ctx, userFrom(ctx).ID, in.Service)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(catalog), nil
		})

	// Select an existing provider playlist from Settings. Enabling imports a
	// local copy and establishes the link; disabling keeps both copies.
	huma.Register(api, secured(op(http.MethodPut, "/api/me/music-services/{service}/playlists/{external_id}/sync", "set-external-playlist-sync", "Link or unlink an external playlist", "Me")),
		func(ctx context.Context, in *struct {
			Service    string `path:"service" enum:"listenbrainz,lastfm"`
			ExternalID string `path:"external_id" minLength:"1" maxLength:"256"`
			Body       playlistSyncToggle
		}) (*JSONOutput[externalPlaylistSyncBody], error) {
			playlistID, err := app.EnableExternalPlaylistSync(ctx, userFrom(ctx).ID, in.Service, in.ExternalID, in.Body.Enabled)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return noStoreJSON(externalPlaylistSyncBody{PlaylistID: playlistID, Enabled: in.Body.Enabled}), nil
		})

	// Opt a Heya playlist into a provider from the playlist itself. This path
	// creates the provider-side copy on first enable.
	huma.Register(api, secured(op(http.MethodPut, "/api/me/playlists/{id}/sync/{service}", "set-local-playlist-sync", "Enable or disable playlist synchronization", "Me")),
		func(ctx context.Context, in *struct {
			IDPath
			Service string `path:"service" enum:"listenbrainz,lastfm"`
			Body    playlistSyncToggle
		}) (*StatusOutput, error) {
			if err := app.EnableLocalPlaylistSync(ctx, userFrom(ctx).ID, in.ID, in.Service, in.Body.Enabled); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return statusOK("saved"), nil
		})

	huma.Register(api, secured(op(http.MethodPost, "/api/me/playlists/{id}/sync/{service}", "sync-playlist-now", "Run a two-way playlist synchronization now", "Me")),
		func(ctx context.Context, in *struct {
			IDPath
			Service string `path:"service" enum:"listenbrainz,lastfm"`
		}) (*StatusOutput, error) {
			if err := app.SyncPlaylist(ctx, userFrom(ctx).ID, in.ID, in.Service); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return statusOK("synced"), nil
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

type playlistSyncToggle struct {
	Enabled bool `json:"enabled"`
}

type externalPlaylistSyncBody struct {
	PlaylistID int64 `json:"playlist_id,omitempty"`
	Enabled    bool  `json:"enabled"`
}
