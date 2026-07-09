// Package subsonic implements a Subsonic 1.16.1 + OpenSubsonic compatible
// API on top of Heya's service layer, so stock Subsonic music clients
// (Symfonium, DSub, play:Sub, Tempo, Supersonic, Sonixd...) can browse,
// search, and stream Heya's music libraries.
//
// Design constraints, in order (mirroring internal/jellyfin):
//
//   - Everything lives in this package. The only hooks elsewhere are the
//     prefixed mount in internal/server.New, the settings plumbing
//     (internal/service/subsonic_settings.go), and the credential service
//     (internal/service/subsonic_credentials.go).
//   - No huma. The generated OpenAPI client must not see this surface; the
//     contract is the Subsonic API spec, enforced by the coverage manifest
//     in manifest.go.
//   - Handlers go through the Backend interface (satisfied by *service.App)
//     only — never internal/database/sqlc queries directly.
//   - Music only. Heya's movie/TV/book libraries do not exist on this
//     surface; video endpoints answer like a music-only Subsonic server.
//
// Every endpoint answers under /rest/<name> and /rest/<name>.view, via GET
// or POST (the OpenSubsonic formPost extension), XML by default and JSON
// with f=json. HTTP status is always 200 — errors ride in the envelope.
package subsonic

import (
	"context"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

type Server struct {
	app  Backend
	next http.Handler
	// native is the full server mux (/api/* included), set via SetNative
	// after mount. getCoverArt dispatches through it in-process so the
	// native image pipeline (media_assets walk, resizer, passive-mode
	// proxy) serves the bytes — same trick as the Jellyfin layer.
	native http.Handler

	routes map[string]http.HandlerFunc
}

// SetNative hands the middleware the fully-built server mux for in-process
// dispatch to native endpoints. Called once from internal/server.New after
// the mux is assembled.
func (s *Server) SetNative(h http.Handler) { s.native = h }

// NewMiddleware mounts the Subsonic surface in front of next. The caller
// owns namespacing; production mounts it under /subsonic. Also seeds the
// enabled-flag's DB overlay — done here, not in service.App.New, to keep
// the feature's boot footprint inside this package.
func NewMiddleware(app Backend, next http.Handler) *Server {
	s := &Server{app: app, next: next}
	s.routes = s.buildRoutes()
	if app != nil {
		app.LoadSubsonicFromDB(context.Background())
		if app.SubsonicEnabled() {
			log.Info().Str("component", "subsonic").Str("api_version", apiVersion).Msg("subsonic-compatible api enabled")
		}
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.app == nil || !s.app.SubsonicEnabled() {
		s.next.ServeHTTP(w, r)
		return
	}
	name, ok := endpointName(r.URL.Path)
	if !ok {
		s.next.ServeHTTP(w, r)
		return
	}
	if h, ok := s.routes[name]; ok {
		h(w, r)
		return
	}
	// A /rest/-shaped request we don't implement must answer in-protocol,
	// never fall through to a 404 HTML page. Real Subsonic answers unknown
	// views with error 0; the WARN log is the worklist if a client needs it.
	log.Warn().Str("component", "subsonic").Str("endpoint", name).
		Msg("unimplemented Subsonic endpoint requested")
	respondError(w, r, errGeneric, "endpoint not implemented: "+name)
}

// endpointName extracts the view name from /rest/<name>[.view] paths
// (case-insensitive, tolerant of a double slash). ok=false when the path
// isn't a Subsonic REST call at all.
func endpointName(path string) (string, bool) {
	p := strings.ToLower(strings.Trim(path, "/"))
	if !strings.HasPrefix(p, "rest") {
		return "", false
	}
	p = strings.TrimPrefix(p, "rest")
	p = strings.Trim(p, "/")
	if p == "" || strings.Contains(p, "/") {
		return "", false
	}
	return strings.TrimSuffix(p, ".view"), true
}

// buildRoutes maps lowercased endpoint names to handlers. Keys must match
// the manifest's implemented/stubbed entries — manifest_test enforces it.
func (s *Server) buildRoutes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		// System.
		"ping":                      s.requireAuth(s.handlePing),
		"getlicense":                s.requireAuth(s.handleGetLicense),
		"getopensubsonicextensions": s.handleGetOpenSubsonicExtensions, // spec: no auth required
		"tokeninfo":                 s.requireAuth(s.handleTokenInfo),

		// Browsing.
		"getmusicfolders":   s.requireAuth(s.handleGetMusicFolders),
		"getindexes":        s.requireAuth(s.handleGetIndexes),
		"getmusicdirectory": s.requireAuth(s.handleGetMusicDirectory),
		"getgenres":         s.requireAuth(s.handleGetGenres),
		"getartists":        s.requireAuth(s.handleGetArtists),
		"getartist":         s.requireAuth(s.handleGetArtist),
		"getalbum":          s.requireAuth(s.handleGetAlbum),
		"getsong":           s.requireAuth(s.handleGetSong),
		"getvideos":         s.requireAuth(s.handleGetVideos),
		"getartistinfo":     s.requireAuth(s.handleGetArtistInfo),
		"getartistinfo2":    s.requireAuth(s.handleGetArtistInfo2),
		"getalbuminfo":      s.requireAuth(s.handleGetAlbumInfo),
		"getalbuminfo2":     s.requireAuth(s.handleGetAlbumInfo),
		"getsimilarsongs":   s.requireAuth(s.handleGetSimilarSongs),
		"getsimilarsongs2":  s.requireAuth(s.handleGetSimilarSongs2),
		"gettopsongs":       s.requireAuth(s.handleGetTopSongs),

		// Album/song lists.
		"getalbumlist":    s.requireAuth(s.handleGetAlbumList),
		"getalbumlist2":   s.requireAuth(s.handleGetAlbumList2),
		"getrandomsongs":  s.requireAuth(s.handleGetRandomSongs),
		"getsongsbygenre": s.requireAuth(s.handleGetSongsByGenre),
		"getnowplaying":   s.requireAuth(s.handleGetNowPlaying),
		"getstarred":      s.requireAuth(s.handleGetStarred),
		"getstarred2":     s.requireAuth(s.handleGetStarred2),

		// Searching.
		"search":  s.requireAuth(s.handleSearch2),
		"search2": s.requireAuth(s.handleSearch2),
		"search3": s.requireAuth(s.handleSearch3),

		// Playlists.
		"getplaylists":   s.requireAuth(s.handleGetPlaylists),
		"getplaylist":    s.requireAuth(s.handleGetPlaylist),
		"createplaylist": s.requireAuth(s.handleCreatePlaylist),
		"updateplaylist": s.requireAuth(s.handleUpdatePlaylist),
		"deleteplaylist": s.requireAuth(s.handleDeletePlaylist),

		// Media retrieval.
		"stream":            s.requireAuth(s.handleStream),
		"download":          s.requireAuth(s.handleDownload),
		"getcoverart":       s.requireAuth(s.handleGetCoverArt),
		"getlyrics":         s.requireAuth(s.handleGetLyrics),
		"getlyricsbysongid": s.requireAuth(s.handleGetLyricsBySongID),
		"getavatar":         s.requireAuth(s.handleGetAvatar),

		// Media annotation.
		"star":      s.requireAuth(s.handleStar(true)),
		"unstar":    s.requireAuth(s.handleStar(false)),
		"setrating": s.requireAuth(s.handleSetRating),
		"scrobble":  s.requireAuth(s.handleScrobble),

		// Bookmarks + play queue.
		"getbookmarks":  s.requireAuth(s.handleGetBookmarks),
		"getplayqueue":  s.requireAuth(s.handleGetPlayQueue),
		"saveplayqueue": s.requireAuth(s.handleSavePlayQueue),

		// User management.
		"getuser":        s.requireAuth(s.handleGetUser),
		"getusers":       s.requireAdmin(s.handleGetUsers),
		"createuser":     s.requireAdmin(s.refuseUserMutation),
		"updateuser":     s.requireAdmin(s.refuseUserMutation),
		"deleteuser":     s.requireAdmin(s.refuseUserMutation),
		"changepassword": s.requireAuth(s.refuseUserMutation),

		// Library scanning.
		"getscanstatus": s.requireAuth(s.handleGetScanStatus),
		"startscan":     s.requireAdmin(s.handleStartScan),

		// Graceful "feature absent" stubs — see stubs.go.
		"getshares":                s.requireAuth(s.stubEmpty("shares", &Shares{})),
		"getpodcasts":              s.requireAuth(s.stubEmpty("podcasts", &Podcasts{})),
		"getnewestpodcasts":        s.requireAuth(s.stubEmpty("newestPodcasts", &NewestPodcasts{})),
		"getinternetradiostations": s.requireAuth(s.stubEmpty("internetRadioStations", &InternetRadioStations{})),
		"getchatmessages":          s.requireAuth(s.stubEmpty("chatMessages", &ChatMessages{})),
	}
}
