// Package jellyfin implements a Jellyfin-compatible API surface on top of
// Heya's service layer, so stock Jellyfin clients (Infuse, Finamp,
// Streamyfin, Findroid, jellyfin-web, ...) can use Heya as their server.
//
// Design constraints, in order:
//
//   - Everything lives in this package. The only hooks elsewhere are the
//     root protocol dispatch in internal/server.New and the config/settings
//     plumbing (internal/service/jellyfin_settings.go).
//   - No huma. The generated OpenAPI client (web/shared/api.openapi.json)
//     must not see this surface; the contract is Jellyfin's own vendored
//     spec (spec/, enforced by the coverage manifest in manifest.go).
//   - Handlers go through *service.App only — never internal/database/sqlc
//     directly, and never internal/server (whose helpers are a parallel,
//     huma-bound world).
//   - The surface targets Jellyfin 10.11.11 semantics (see system.go).
//
// Requests are matched case-insensitively with an optional /emby prefix.
// Root dispatch uses ClaimsRootRequest to keep Heya SPA routes separate.
package jellyfin

import (
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/service"
	"github.com/rs/zerolog/log"
)

type Server struct {
	app  *service.App
	hub  *eventhub.Hub
	rt   *router
	next http.Handler
	// native is the full server mux (/api/* included), set via SetNative
	// after mount. Image requests dispatch through it in-process so the
	// native pipeline (media_assets walk, resizer, passive-mode proxy)
	// serves bytes directly — some clients (Feishin) don't follow image
	// redirects, and real Jellyfin serves bytes, not 302s.
	native http.Handler

	socketsMu sync.RWMutex
	sockets   map[*socketConn]struct{}
}

// SetNative hands the middleware the fully-built server mux for in-process
// dispatch to native endpoints. Called once from internal/server.New after
// the mux is assembled (it can't be passed at construction — the middleware
// is itself part of the mux).
func (s *Server) SetNative(h http.Handler) { s.native = h }

// NewMiddleware builds the Jellyfin surface in front of next. The caller owns
// root dispatch so the case-insensitive Jellyfin route table cannot steal a
// colliding Heya SPA path. It also seeds the enabled-flag's DB overlay — done
// here, not in service.App.New, to keep the feature's boot footprint inside
// this package.
func NewMiddleware(app *service.App, hub *eventhub.Hub, next http.Handler) *Server {
	s := &Server{app: app, hub: hub, next: next}
	s.rt = s.buildRouter()
	if app != nil && app.DBPool() != nil {
		app.LoadJellyfinFromDB(app.LifetimeContext())
		if app.JellyfinEnabled() {
			log.Info().Str("component", "jellyfin").Str("advertised_version", jellyfinVersion).Msg("jellyfin-compatible api enabled")
		}
	}
	if app != nil && hub != nil {
		go s.bridgeEvents()
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
	// A registered or Jellyfin-shaped request we don't route
	// must NOT fall through to the SPA: an HTML body with a 200 makes strict
	// clients (Infuse) throw "unexpected server response" when they try to
	// parse it as JSON. Real Jellyfin answers unknown endpoints with a 404,
	// which clients tolerate. Return that, and log at WARN so the specific
	// endpoint the client needs is visible (implement it if load-bearing).
	// Root dispatch resolves the one known SPA collision before entering this
	// handler, so returning a protocol 404 here cannot shadow the web app.
	if ClaimsPath(path) || hasJellyfinRequestIdentity(r) || strings.HasPrefix(strings.ToLower(r.URL.Path), "/emby/") {
		log.Warn().Str("component", "jellyfin").Str("method", r.Method).Str("path", r.URL.Path).
			Msg("unrouted Jellyfin request — returning 404 (client may need this endpoint)")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	s.next.ServeHTTP(w, r)
}

// jellyfinShaped reports whether a path looks like a canonical Jellyfin API
// call (first segment starts with an uppercase letter) rather than an SPA
// route (Heya pages are lowercase).
func jellyfinShaped(path string) bool {
	p := strings.TrimPrefix(path, "/")
	if p == "" {
		return false
	}
	seg := p
	if i := strings.IndexByte(p, '/'); i >= 0 {
		seg = p[:i]
	}
	c := seg[0]
	return c >= 'A' && c <= 'Z'
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
	rt.handle(http.MethodGet, "/Items/Root", s.requireAuth(s.handleItemsRoot))
	rt.handle(http.MethodGet, "/Items", s.requireAuth(s.handleItems))
	rt.handle(http.MethodGet, "/Items/{itemId}", s.requireAuth(s.handleItemByID))
	rt.handle(http.MethodGet, "/Items/{itemId}/Intros", s.requireAuth(s.handleItemIntros))
	rt.handle(http.MethodGet, "/Items/{itemId}/LocalTrailers", s.requireAuth(s.handleItemExtrasArray))
	rt.handle(http.MethodGet, "/Items/{itemId}/SpecialFeatures", s.requireAuth(s.handleItemExtrasArray))
	// Item deletion: 404 unknown, 403 known — never a mutating 204.
	rt.handle(http.MethodDelete, "/Items/{itemId}", s.requireAuth(s.handleDeleteItems))
	rt.handle(http.MethodDelete, "/Items", s.requireAuth(s.handleDeleteItems))
	rt.handle(http.MethodGet, "/UserItems/Resume", s.requireAuth(s.handleResume))
	rt.handle(http.MethodGet, "/Shows/NextUp", s.requireAuth(s.handleNextUp))
	rt.handle(http.MethodGet, "/Shows/{seriesId}/Seasons", s.requireAuth(s.handleShowSeasons))
	rt.handle(http.MethodGet, "/Shows/{seriesId}/Episodes", s.requireAuth(s.handleShowEpisodes))
	rt.handle(http.MethodGet, "/Artists", s.requireAuth(s.handleArtists))
	rt.handle(http.MethodGet, "/Artists/AlbumArtists", s.requireAuth(s.handleArtists))

	// Images. Stock clients attach api_key to these URLs; keeping the endpoint
	// authenticated avoids exposing library artwork on public instances.
	rt.handle(http.MethodGet, "/Items/{itemId}/Images/{imageType}", s.requireAuth(s.handleItemImage))
	rt.handle(http.MethodGet, "/Items/{itemId}/Images/{imageType}/{imageIndex}", s.requireAuth(s.handleItemImage))

	// Legacy pre-10.9 user-scoped aliases. Removed from the 10.11 spec but
	// still emitted by clients that keep compatibility with older servers;
	// upstream still answers them, so we do too (manifest extras). The
	// {userId} segment is ignored — the token decides the user.
	rt.handle(http.MethodGet, "/Users/{userId}/Views", s.requireAuth(s.handleUserViews))
	rt.handle(http.MethodGet, "/Users/{userId}/Items/Resume", s.requireAuth(s.handleResume))
	rt.handle(http.MethodGet, "/Users/{userId}/Items/Latest", s.requireAuth(s.handleItemsLatest))
	rt.handle(http.MethodGet, "/Users/{userId}/Items/Root", s.requireAuth(s.handleItemsRoot))
	rt.handle(http.MethodGet, "/Users/{userId}/Items", s.requireAuth(s.handleItems))
	rt.handle(http.MethodGet, "/Users/{userId}/Items/{itemId}", s.requireAuth(s.handleItemByID))
	rt.handle(http.MethodGet, "/Users/{userId}/Items/{itemId}/Intros", s.requireAuth(s.handleItemIntros))
	rt.handle(http.MethodGet, "/Users/{userId}/Items/{itemId}/LocalTrailers", s.requireAuth(s.handleItemExtrasArray))
	rt.handle(http.MethodGet, "/Users/{userId}/Items/{itemId}/SpecialFeatures", s.requireAuth(s.handleItemExtrasArray))
	rt.handle(http.MethodGet, "/Users/{userId}/Items/{itemId}/Lyrics", s.requireAuth(s.handleUserItemLyrics))

	// Playback negotiation + delivery. The HEAD registrations exist because
	// the upstream spec declares them; the router also HEAD→GET falls back.
	rt.handle(http.MethodGet, "/Items/{itemId}/PlaybackInfo", s.requireAuth(s.handlePlaybackInfo))
	rt.handle(http.MethodPost, "/Items/{itemId}/PlaybackInfo", s.requireAuth(s.handlePlaybackInfo))
	rt.handle(http.MethodGet, "/Videos/{itemId}/stream", s.requireAuth(s.handleVideoStream))
	rt.handle(http.MethodGet, "/Videos/{itemId}/stream.{container}", s.requireAuth(s.handleVideoStream))
	rt.handle(http.MethodHead, "/Videos/{itemId}/stream", s.requireAuth(s.handleVideoStream))
	rt.handle(http.MethodHead, "/Videos/{itemId}/stream.{container}", s.requireAuth(s.handleVideoStream))
	rt.handle(http.MethodGet, "/Audio/{itemId}/stream", s.requireAuth(s.handleAudioStream))
	rt.handle(http.MethodGet, "/Audio/{itemId}/stream.{container}", s.requireAuth(s.handleAudioStream))
	rt.handle(http.MethodHead, "/Audio/{itemId}/stream", s.requireAuth(s.handleAudioStream))
	rt.handle(http.MethodHead, "/Audio/{itemId}/stream.{container}", s.requireAuth(s.handleAudioStream))
	rt.handle(http.MethodGet, "/Audio/{itemId}/universal", s.requireAuth(s.handleAudioUniversal))
	rt.handle(http.MethodHead, "/Audio/{itemId}/universal", s.requireAuth(s.handleAudioUniversal))

	// Playstate reporting.
	rt.handle(http.MethodPost, "/Sessions/Playing", s.requireAuth(s.handlePlaying(playStart)))
	rt.handle(http.MethodPost, "/Sessions/Playing/Progress", s.requireAuth(s.handlePlaying(playProgress)))
	rt.handle(http.MethodPost, "/Sessions/Playing/Stopped", s.requireAuth(s.handlePlaying(playStopped)))
	rt.handle(http.MethodPost, "/Sessions/Playing/Ping", s.requireAuth(s.handlePlayingPing))

	// Userdata: favorites + played flags (with legacy user-scoped aliases).
	rt.handle(http.MethodPost, "/UserFavoriteItems/{itemId}", s.requireAuth(s.handleSetFavorite(true)))
	rt.handle(http.MethodDelete, "/UserFavoriteItems/{itemId}", s.requireAuth(s.handleSetFavorite(false)))
	rt.handle(http.MethodPost, "/UserPlayedItems/{itemId}", s.requireAuth(s.handleSetPlayed(true)))
	rt.handle(http.MethodDelete, "/UserPlayedItems/{itemId}", s.requireAuth(s.handleSetPlayed(false)))
	rt.handle(http.MethodPost, "/Users/{userId}/FavoriteItems/{itemId}", s.requireAuth(s.handleSetFavorite(true)))
	rt.handle(http.MethodDelete, "/Users/{userId}/FavoriteItems/{itemId}", s.requireAuth(s.handleSetFavorite(false)))
	rt.handle(http.MethodPost, "/Users/{userId}/PlayedItems/{itemId}", s.requireAuth(s.handleSetPlayed(true)))
	rt.handle(http.MethodDelete, "/Users/{userId}/PlayedItems/{itemId}", s.requireAuth(s.handleSetPlayed(false)))

	// Display preferences, filters, similar, lyrics, session listing.
	rt.handle(http.MethodGet, "/DisplayPreferences/{displayPreferencesId}", s.requireAuth(s.handleGetDisplayPreferences))
	rt.handle(http.MethodPost, "/DisplayPreferences/{displayPreferencesId}", s.requireAuth(s.handleSetDisplayPreferences))
	rt.handle(http.MethodGet, "/Items/Filters", s.requireAuth(s.handleItemFilters))
	rt.handle(http.MethodGet, "/Items/Filters2", s.requireAuth(s.handleItemFilters2))
	rt.handle(http.MethodGet, "/Items/{itemId}/Similar", s.requireAuth(s.requireItem(s.handleSimilar)))
	rt.handle(http.MethodGet, "/Movies/{itemId}/Similar", s.requireAuth(s.requireItem(s.handleSimilar)))
	rt.handle(http.MethodGet, "/Shows/{itemId}/Similar", s.requireAuth(s.requireItem(s.handleSimilar)))
	rt.handle(http.MethodGet, "/Albums/{itemId}/Similar", s.requireAuth(s.requireItem(s.handleSimilar)))
	rt.handle(http.MethodGet, "/Artists/{itemId}/Similar", s.requireAuth(s.requireItem(s.handleSimilar)))
	rt.handle(http.MethodGet, "/Trailers/{itemId}/Similar", s.requireAuth(s.requireItem(s.handleSimilar)))
	rt.handle(http.MethodGet, "/Audio/{itemId}/Lyrics", s.requireAuth(s.handleLyrics))

	// Localization reference data + user avatar
	rt.handle(http.MethodGet, "/Localization/Cultures", s.requireAuth(s.handleCultures))
	rt.handle(http.MethodGet, "/Localization/ParentalRatings", s.requireAuth(s.handleParentalRatings))
	rt.handle(http.MethodGet, "/UserImage", s.handleUserImage)

	// InstantMix — all upstream aliases route to the one handler
	rt.handle(http.MethodGet, "/Items/{itemId}/InstantMix", s.requireAuth(s.handleInstantMix))
	rt.handle(http.MethodGet, "/Songs/{itemId}/InstantMix", s.requireAuth(s.handleInstantMix))
	rt.handle(http.MethodGet, "/Albums/{itemId}/InstantMix", s.requireAuth(s.handleInstantMix))
	rt.handle(http.MethodGet, "/Artists/{itemId}/InstantMix", s.requireAuth(s.handleInstantMix))
	rt.handle(http.MethodGet, "/Playlists/{itemId}/InstantMix", s.requireAuth(s.handleInstantMix))

	// Playlists (Heya-native, owner-private)
	rt.handle(http.MethodPost, "/Playlists", s.requireAuth(s.handleCreatePlaylist))
	rt.handle(http.MethodGet, "/Playlists/{playlistId}/Items", s.requireAuth(s.handleGetPlaylistItems))
	rt.handle(http.MethodPost, "/Playlists/{playlistId}/Items", s.requireAuth(s.handleAddPlaylistItems))
	rt.handle(http.MethodDelete, "/Playlists/{playlistId}/Items", s.requireAuth(s.handleRemovePlaylistItems))
	rt.handle(http.MethodGet, "/Playlists/{playlistId}/Users/{userId}", s.requireAuth(s.handlePlaylistUser))
	rt.handle(http.MethodGet, "/Sessions", s.requireAuth(s.handleSessionsList))
	rt.handle(http.MethodPost, "/Sessions/Capabilities", s.requireAuth(s.handleSessionsCapabilities))
	rt.handle(http.MethodPost, "/Sessions/Capabilities/Full", s.requireAuth(s.handleSessionsCapabilities))
	rt.handle(http.MethodPost, "/Sessions/Viewing", s.requireAuth(s.handleSessionsViewing))

	// Library structure — Infuse fetches this during add-server to enumerate
	// libraries; a 404 aborts its add flow. Mutations are validated like
	// upstream, then answered 403: Heya manages libraries itself.
	rt.handle(http.MethodGet, "/Library/VirtualFolders", s.requireAuth(s.handleVirtualFolders))
	rt.handle(http.MethodPost, "/Library/VirtualFolders", s.requireAdmin(s.handleAddVirtualFolder))
	rt.handle(http.MethodDelete, "/Library/VirtualFolders", s.requireAdmin(s.handleDeleteVirtualFolder))
	rt.handle(http.MethodPost, "/Library/VirtualFolders/Name", s.requireAdmin(s.handleRenameVirtualFolder))
	rt.handle(http.MethodPost, "/Library/VirtualFolders/LibraryOptions", s.requireAdmin(s.handleUpdateLibraryOptions))
	rt.handle(http.MethodPost, "/Library/VirtualFolders/Paths", s.requireAdmin(s.handleAddMediaPath))
	rt.handle(http.MethodPost, "/Library/VirtualFolders/Paths/Update", s.requireAdmin(s.handleUpdateMediaPath))
	rt.handle(http.MethodDelete, "/Library/VirtualFolders/Paths", s.requireAdmin(s.handleRemoveMediaPath))

	// Small real conveniences.
	rt.handle(http.MethodGet, "/GetUtcTime", s.handleGetUtcTime)
	rt.handle(http.MethodGet, "/Playback/BitrateTest", s.requireAuth(s.handleBitrateTest))
	rt.handle(http.MethodGet, "/Items/Counts", s.requireAuth(s.handleItemCounts))
	rt.handle(http.MethodGet, "/Search/Hints", s.requireAuth(s.handleSearchHints))
	rt.handle(http.MethodGet, "/Items/{itemId}/Download", s.requireAuth(s.handleItemDownload))
	rt.handle(http.MethodGet, "/Items/{itemId}/File", s.requireAuth(s.handleItemDownload))
	rt.handle(http.MethodGet, "/Genres", s.requireAuth(s.handleGenres))
	rt.handle(http.MethodGet, "/Genres/{genreName}", s.requireAuth(s.handleGenreDetail))
	rt.handle(http.MethodGet, "/Videos/{routeItemId}/{routeMediaSourceId}/Subtitles/{routeIndex}/Stream.{routeFormat}", s.handleSubtitleStream)
	rt.handle(http.MethodGet, "/Videos/{routeItemId}/{routeMediaSourceId}/Subtitles/{routeIndex}/{routeStartPositionTicks}/Stream.{routeFormat}", s.handleSubtitleStream)

	// Graceful "feature off" stubs — see stubs.go. A probing client must
	// conclude "disabled", never "broken".
	rt.handle(http.MethodGet, "/System/Endpoint", s.requireAuth(s.handleSystemEndpoint))
	rt.handle(http.MethodGet, "/LiveTv/Info", s.requireAuth(s.handleLiveTvInfo))
	rt.handle(http.MethodGet, "/Auth/Providers", s.requireAdmin(s.handleAuthProviders))
	rt.handle(http.MethodGet, "/Auth/PasswordResetProviders", s.requireAdmin(s.handlePasswordResetProviders))
	rt.handle(http.MethodGet, "/Items/{itemId}/ThemeMedia", s.requireAuth(s.requireItem(s.handleThemeMedia)))
	rt.handle(http.MethodGet, "/Items/{itemId}/ThemeSongs", s.requireAuth(s.requireItem(s.handleThemeSongsOrVideos)))
	rt.handle(http.MethodGet, "/Items/{itemId}/ThemeVideos", s.requireAuth(s.requireItem(s.handleThemeSongsOrVideos)))
	rt.handle(http.MethodGet, "/Items/{itemId}/Ancestors", s.requireAuth(s.requireItem(s.stubEmptyArray)))
	rt.handle(http.MethodGet, "/Items/{itemId}/CriticReviews", s.requireAuth(s.requireItem(s.stubEmptyQueryResult)))
	rt.handle(http.MethodGet, "/Items/Suggestions", s.requireAuth(s.handleSuggestions))
	rt.handle(http.MethodGet, "/MediaSegments/{itemId}", s.requireAuth(s.requireItem(s.handleMediaSegments)))
	rt.handle(http.MethodGet, "/System/ActivityLog/Entries", s.requireAdmin(s.handleActivityLogEntries))
	rt.handle(http.MethodGet, "/web/ConfigurationPages", s.requireAuth(s.handleConfigurationPages))
	rt.handle(http.MethodGet, "/web/ConfigurationPage", s.stubNotFound)
	rt.handle(http.MethodPost, "/LiveTv/TunerHosts", s.requireAuth(s.stubNotFound))

	// Startup wizard: a Heya server is configured through Heya itself, so the
	// wizard is always complete — upstream locks these behind 401 then.
	rt.handle(http.MethodGet, "/Startup/User", s.handleStartupLocked)
	rt.handle(http.MethodPost, "/Startup/User", s.handleStartupLocked)
	rt.handle(http.MethodGet, "/Startup/Configuration", s.handleStartupLocked)
	rt.handle(http.MethodPost, "/Startup/Configuration", s.handleStartupLocked)
	rt.handle(http.MethodPost, "/Startup/Complete", s.handleStartupLocked)
	rt.handle(http.MethodGet, "/Startup/FirstUser", s.handleStartupLocked)

	// Served like a real server: the vendored 10.11 spec, and robots (a
	// personal media server wants no crawler; see spec.go).
	rt.handle(http.MethodGet, "/api-docs/openapi.json", s.handleOpenAPISpec)
	rt.handle(http.MethodGet, "/robots.txt", s.handleRobotsRedirect)
	rt.handle(http.MethodGet, "/web/robots.txt", s.handleRobotsTxt)
	rt.handle(http.MethodGet, "/Videos/{itemId}/AdditionalParts", s.requireAuth(s.handleAdditionalParts))
	rt.handle(http.MethodGet, "/Videos/{itemId}/Trickplay/{width}/{index}.jpg", s.requireAuth(s.handleTrickplayTile))
	rt.handle(http.MethodGet, "/Devices", s.requireAdmin(s.handleDevices))
	rt.handle(http.MethodGet, "/ScheduledTasks", s.requireAdmin(s.stubEmptyArray))
	rt.handle(http.MethodGet, "/Plugins", s.requireAdmin(s.stubEmptyArray))
	rt.handle(http.MethodGet, "/Packages", s.requireAdmin(s.stubEmptyArray))
	rt.handle(http.MethodGet, "/Channels", s.requireAuth(s.stubEmptyQueryResult))
	rt.handle(http.MethodGet, "/Persons", s.requireAuth(s.stubEmptyQueryResult))
	rt.handle(http.MethodGet, "/Studios", s.requireAuth(s.stubEmptyQueryResult))
	rt.handle(http.MethodGet, "/Years", s.requireAuth(s.stubEmptyQueryResult))
	rt.handle(http.MethodGet, "/MusicGenres", s.requireAuth(s.stubEmptyQueryResult))
	rt.handle(http.MethodGet, "/Shows/Upcoming", s.requireAuth(s.stubEmptyQueryResult))
	rt.handle(http.MethodGet, "/Movies/Recommendations", s.requireAuth(s.handleMovieRecommendations))
	rt.handle(http.MethodGet, "/UserViews/GroupingOptions", s.requireAuth(s.handleGroupingOptions))

	// Remote-control command acks: we have no command channel to other
	// players yet, and Jellyfin itself 204s commands to gone sessions.
	rt.handle(http.MethodPost, "/Sessions/{sessionId}/Command", s.requireAuth(s.stubNoContent))
	rt.handle(http.MethodPost, "/Sessions/{sessionId}/Command/{command}", s.requireAuth(s.stubNoContent))
	rt.handle(http.MethodPost, "/Sessions/{sessionId}/Message", s.requireAuth(s.stubNoContent))
	rt.handle(http.MethodPost, "/Sessions/{sessionId}/Playing", s.requireAuth(s.stubNoContent))
	rt.handle(http.MethodPost, "/Sessions/{sessionId}/Playing/{command}", s.requireAuth(s.stubNoContent))
	rt.handle(http.MethodPost, "/Sessions/{sessionId}/System/{command}", s.requireAuth(s.stubNoContent))
	rt.handle(http.MethodPost, "/Sessions/{sessionId}/Viewing", s.requireAuth(s.stubNoContent))

	// WebSocket. "/socket" isn't part of the REST spec; the manifest lists
	// it as an extra.
	rt.handle(http.MethodGet, "/socket", s.handleSocket)
	rt.handle(http.MethodGet, "/embywebsocket", s.handleSocket)

	rt.finalize()
	return rt
}

// ClaimsPath reports whether the Jellyfin surface owns path. It is
// method-agnostic and independent of the enabled flag so invalid methods on a
// real protocol path still receive a protocol 404 instead of SPA HTML.
func ClaimsPath(path string) bool {
	p := stripEmbyPrefix(path)
	// Claim registered routes AND any Jellyfin-shaped path (PascalCase first
	// segment). The latter lets unimplemented Jellyfin endpoints return a
	// protocol-shaped 404 instead of an HTML fallback.
	return claimsRouter.claims(p) || jellyfinShaped(p)
}

// ClaimsRootRequest reports whether a request at Heya's origin belongs to the
// Jellyfin protocol. The actual route table is the ownership manifest: every
// registered path is accepted case-insensitively, and canonical PascalCase or
// /emby-shaped misses enter the protocol too so clients receive a JSON 404.
//
// Jellyfin's GET /Movies/Recommendations is the sole route that collides with
// a Heya page (/movies/recommendations). Canonical Jellyfin casing or
// Jellyfin request identity selects the protocol; an ordinary lowercase
// browser navigation stays with the SPA.
func ClaimsRootRequest(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	path := r.URL.Path
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return false
	}
	if c := trimmed[0]; c >= 'A' && c <= 'Z' {
		return true
	}

	lower := strings.ToLower(path)
	if strings.HasPrefix(lower, "/emby/") {
		return true
	}
	if hasJellyfinRequestIdentity(r) {
		return true
	}
	if !ClaimsPath(path) {
		return false
	}
	if strings.Trim(lower, "/") == "movies/recommendations" {
		return false
	}
	return true
}

// hasJellyfinRequestIdentity recognizes both authenticated requests and the
// client-identification header sent before login. Once a request identifies
// itself as Jellyfin, even an unregistered lowercase path belongs to the
// protocol and must receive JSON rather than SPA HTML.
func hasJellyfinRequestIdentity(r *http.Request) bool {
	if r.Header.Get("X-Emby-Authorization") != "" ||
		r.Header.Get("X-Emby-Token") != "" ||
		r.Header.Get("X-MediaBrowser-Token") != "" {
		return true
	}
	authorization := strings.ToLower(strings.TrimSpace(r.Header.Get("Authorization")))
	if strings.HasPrefix(authorization, "mediabrowser ") || strings.HasPrefix(authorization, "emby ") {
		return true
	}
	query := r.URL.Query()
	return query.Get("api_key") != "" || query.Get("ApiKey") != "" || query.Get("token") != ""
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
