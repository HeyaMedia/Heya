// Package jellyfin implements a Jellyfin-compatible API surface on top of
// Heya's service layer, so stock Jellyfin clients (Infuse, Finamp,
// Streamyfin, Findroid, jellyfin-web, ...) can use Heya as their server.
//
// Design constraints, in order:
//
//   - Everything lives in this package. The only hooks elsewhere are the
//     one-line mount in internal/server.New, the dev-proxy path claim, and
//     the config/settings plumbing (internal/service/jellyfin_settings.go).
//   - No huma. The generated OpenAPI client (web/shared/api.openapi.json)
//     must not see this surface; the contract is Jellyfin's own vendored
//     spec (spec/, enforced by the coverage manifest in manifest.go).
//   - Handlers go through *service.App only — never internal/database/sqlc
//     directly, and never internal/server (whose helpers are a parallel,
//     huma-bound world).
//   - The surface targets Jellyfin 10.11.11 semantics (see system.go).
//
// Requests are matched case-insensitively with an optional /emby prefix;
// anything unmatched falls through to the wrapped handler (the SPA), so an
// off toggle — or a miss — behaves exactly as if this package didn't exist.
package jellyfin

import (
	"net/http"
	"os"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/service"
	"github.com/rs/zerolog/log"
)

type Server struct {
	app  *service.App
	hub  *eventhub.Hub
	rt   *router
	next http.Handler
}

// NewMiddleware mounts the Jellyfin surface in front of next (the SPA
// catch-all). It also seeds the enabled-flag's DB overlay — done here, not in
// service.App.New, to keep the feature's boot footprint inside this package.
func NewMiddleware(app *service.App, hub *eventhub.Hub, next http.Handler) *Server {
	s := &Server{app: app, hub: hub, next: next}
	s.rt = s.buildRouter()
	if app != nil && app.DBPool() != nil {
		app.LoadJellyfinFromDB(app.LifetimeContext())
		if app.JellyfinEnabled() {
			log.Info().Str("component", "jellyfin").Str("advertised_version", jellyfinVersion).Msg("jellyfin-compatible api enabled")
		}
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.app == nil || !s.app.JellyfinEnabled() {
		s.next.ServeHTTP(w, r)
		return
	}
	path := stripEmbyPrefix(r.URL.Path)
	if h, p, ok := s.rt.match(r.Method, path); ok {
		h(w, r, p)
		return
	}
	// OPTIONS preflights for claimed paths are answered by the outer CORS
	// middleware before reaching here. Everything else that we don't claim
	// belongs to the SPA.
	s.next.ServeHTTP(w, r)
}

// buildRouter registers every implemented route. Patterns are byte-identical
// to the vendored OpenAPI spec paths — manifest_test cross-checks that.
func (s *Server) buildRouter() *router {
	rt := newRouter()

	// System (anonymous)
	rt.handle(http.MethodGet, "/System/Info/Public", s.handleSystemInfoPublic)
	rt.handle(http.MethodGet, "/System/Ping", s.handlePing)
	rt.handle(http.MethodPost, "/System/Ping", s.handlePing)
	rt.handle(http.MethodGet, "/Branding/Configuration", s.handleBrandingConfiguration)
	rt.handle(http.MethodGet, "/Branding/Css", s.handleBrandingCss)
	rt.handle(http.MethodGet, "/Branding/Css.css", s.handleBrandingCss)
	rt.handle(http.MethodGet, "/QuickConnect/Enabled", s.handleQuickConnectEnabled)

	// System (authenticated)
	rt.handle(http.MethodGet, "/System/Info", s.requireAdmin(s.handleSystemInfo))

	// Users / auth
	rt.handle(http.MethodPost, "/Users/AuthenticateByName", s.handleAuthenticateByName)
	rt.handle(http.MethodGet, "/Users/Me", s.requireAuth(s.handleUsersMe))
	rt.handle(http.MethodGet, "/Users/Public", s.handleUsersPublic)
	rt.handle(http.MethodGet, "/Users", s.requireAdmin(s.handleUsers))
	rt.handle(http.MethodGet, "/Users/{userId}", s.requireAuth(s.handleUserByID))
	rt.handle(http.MethodPost, "/Sessions/Logout", s.requireAuth(s.handleSessionsLogout))

	// Browse. Registration order matters: this router is linear first-match,
	// so literal paths (/Items/Latest) must precede their param siblings
	// (/Items/{itemId}).
	rt.handle(http.MethodGet, "/UserViews", s.requireAuth(s.handleUserViews))
	rt.handle(http.MethodGet, "/Items/Latest", s.requireAuth(s.handleItemsLatest))
	rt.handle(http.MethodGet, "/Items", s.requireAuth(s.handleItems))
	rt.handle(http.MethodGet, "/Items/{itemId}", s.requireAuth(s.handleItemByID))
	rt.handle(http.MethodGet, "/UserItems/Resume", s.requireAuth(s.handleResume))
	rt.handle(http.MethodGet, "/Shows/NextUp", s.requireAuth(s.handleNextUp))
	rt.handle(http.MethodGet, "/Shows/{seriesId}/Seasons", s.requireAuth(s.handleShowSeasons))
	rt.handle(http.MethodGet, "/Shows/{seriesId}/Episodes", s.requireAuth(s.handleShowEpisodes))
	rt.handle(http.MethodGet, "/Artists", s.requireAuth(s.handleArtists))
	rt.handle(http.MethodGet, "/Artists/AlbumArtists", s.requireAuth(s.handleArtists))

	// Images (anonymous, like upstream — <img> tags carry no headers).
	rt.handle(http.MethodGet, "/Items/{itemId}/Images/{imageType}", s.handleItemImage)
	rt.handle(http.MethodGet, "/Items/{itemId}/Images/{imageType}/{imageIndex}", s.handleItemImage)

	// Legacy pre-10.9 user-scoped aliases. Removed from the 10.11 spec but
	// still emitted by clients that keep compatibility with older servers;
	// upstream still answers them, so we do too (manifest extras). The
	// {userId} segment is ignored — the token decides the user.
	rt.handle(http.MethodGet, "/Users/{userId}/Views", s.requireAuth(s.handleUserViews))
	rt.handle(http.MethodGet, "/Users/{userId}/Items/Resume", s.requireAuth(s.handleResume))
	rt.handle(http.MethodGet, "/Users/{userId}/Items/Latest", s.requireAuth(s.handleItemsLatest))
	rt.handle(http.MethodGet, "/Users/{userId}/Items", s.requireAuth(s.handleItems))
	rt.handle(http.MethodGet, "/Users/{userId}/Items/{itemId}", s.requireAuth(s.handleItemByID))

	// WebSocket. "/socket" isn't part of the REST spec; the manifest lists
	// it as an extra.
	rt.handle(http.MethodGet, "/socket", s.handleSocket)
	rt.handle(http.MethodGet, "/embywebsocket", s.handleSocket)

	return rt
}

// ClaimsPath reports whether the Jellyfin surface would handle path. The dev
// proxy uses it to route Jellyfin-shaped requests to the Go backend instead
// of the Nuxt dev server. Method-agnostic and independent of the enabled
// flag: when disabled, the backend middleware falls through to the (embedded)
// SPA, which for API-shaped paths is indistinguishable from a 404 to clients.
func ClaimsPath(path string) bool {
	return claimsRouter.claims(stripEmbyPrefix(path))
}

// claimsRouter is a handler-less route table for ClaimsPath. Handlers bound
// to the zero Server are never invoked through it.
var claimsRouter = func() *router {
	var s Server
	return s.buildRouter()
}()

// serverID returns the stable advertised server GUID.
func (s *Server) serverID(r *http.Request) string {
	return s.app.JellyfinServerID(r.Context())
}

func hostname() (string, error) { return os.Hostname() }
