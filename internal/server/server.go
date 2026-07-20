package server

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/jellyfin"
	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/service"
	"github.com/karbowiak/heya/internal/subsonic"
)

func New(cfg *config.Config, app *service.App, opts ...Option) *http.Server {
	srv := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           NewHandler(cfg, app, opts...),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	if baseCtx := collectOptions(opts...).baseCtx; baseCtx != nil {
		srv.BaseContext = func(_ net.Listener) context.Context { return baseCtx }
	}
	return srv
}

// NewHandler builds Heya's complete application handler without binding a
// socket or owning an http.Server. Production passes this directly to the
// embedded Caddy ingress module; tests and the OpenAPI CLI can still construct
// the route tree without starting any network runtime.
func NewHandler(cfg *config.Config, app *service.App, opts ...Option) http.Handler {
	mux := http.NewServeMux()
	BuildAPI(mux, app, cfg, opts...)

	o := collectOptions(opts...)

	// Compatibility clients use Heya's origin as their server address.
	// Jellyfin owns its registered root routes (plus canonical-shaped misses),
	// while OpenSubsonic owns /rest/*. The dispatcher preserves Heya's one
	// exact Jellyfin collision, /movies/recommendations, for ordinary browser
	// navigation; Jellyfin casing or request identity selects the protocol.
	jf := jellyfin.NewMiddleware(app, o.hub, http.NotFoundHandler())
	sub := subsonic.NewMiddleware(app, http.NotFoundHandler())
	mux.Handle("/", protocolRouter(jf, sub, spaHandler()))
	jf.SetNative(mux)
	sub.SetNative(mux)

	return withMiddleware(mux)
}

// protocolRouter dispatches the two compatibility protocols at Heya's origin
// while leaving every other request to the SPA.
func protocolRouter(jellyfinHandler, subsonicHandler, spa http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subsonic.ClaimsPath(r.URL.Path) {
			subsonicHandler.ServeHTTP(w, r)
			return
		}
		if jellyfin.ClaimsRootRequest(r) {
			jellyfinHandler.ServeHTTP(w, r)
			return
		}
		spa.ServeHTTP(w, r)
	})
}

// BuildAPI registers every Heya operation against mux and returns the API. Use
// this directly when you need the typed huma.API surface without booting an
// http.Server — most notably the `heya openapi-spec` CLI, which dumps the
// generated OpenAPI document without ever serving traffic, and humatest
// fixtures that exercise input validation / auth gates without a database.
//
// app may be a zero-valued &service.App{}: handler closures capture it but
// are never invoked during pure registration. The hub/logbuf-gated routes
// self-skip when those options are absent. For spec / test invocations we
// also short-circuit auth so a missing db doesn't panic — see WithSessions
// for the opt-in.
func BuildAPI(mux *http.ServeMux, app *service.App, cfg *config.Config, opts ...Option) huma.API {
	o := collectOptions(opts...)
	sessions := o.sessions
	if sessions == nil && app != nil && app.DBPool() != nil {
		sessions = app.SessionLookup()
	}

	// Huma owns the entire /api/* surface. Every endpoint — JSON, binary,
	// SSE, WebSocket, pprof — is registered as a typed operation so it
	// shows up in /api/docs. The actual byte handling for streaming
	// endpoints is delegated through humago.Unwrap to existing stdlib
	// handlers (see wrapStream in binary_huma.go).
	trustedIP := func(string) bool { return false }
	if app != nil {
		trustedIP = app.TrustedClientIP
	}
	api := newHumaAPI(mux, sessions, trustedIP)
	registerSystemRoutes(api, app)
	registerAuthRoutes(api, app, cfg)
	registerAdminRoutes(api, app, o.logBuf)
	registerAdminSystemRoutes(api, app, o.hub)
	registerAdminDiagnosticsRoutes(api, app, o.hub, o.logBuf)
	registerAdminWorkerRoutes(api, app)
	registerAdminNetworkRoutes(api, app, o.hub)
	registerAdminDoctorRoutes(api, app, o.logBuf)
	registerTailscaleRoutes(api, app, cfg)
	registerRemoteRoutes(api, app, cfg)
	registerAIRoutes(api, app)
	registerJellyfinConfigRoutes(api, app)
	registerSubsonicRoutes(api, app)
	registerLibraryRoutes(api, app)
	registerJobRoutes(api, app)
	registerTaskRoutes(api, app)
	registerMediaRoutes(api, app)
	registerMetadataEditorRoutes(api, app)
	registerOpenSubtitlesRoutes(api, app)
	registerMusicRoutes(api, app)
	registerMusicHomeRoutes(api, app)
	registerMusicServicesRoutes(api, app)
	registerMeRoutes(api, app)
	registerSessionRoutes(api, app)
	registerRadioRoutes(api, app)
	registerPodcastRoutes(api, app)
	registerCastRoutes(api, app)
	registerQueueRoutes(api, app)
	if o.hub != nil {
		registerClientDeviceRoutes(api, o.hub)
	}
	registerStreamRoutes(api, app)
	registerNativePlaybackRoutes(api, app)
	registerBinaryRoutes(api, app)
	registerDocsRoutes(api)
	if o.hub != nil {
		registerWebSocketRoutes(api, app, o.hub)
		registerHumaDebugRoutes(api, o.hub)
	}
	registerLogStreamRoute(api, o.logBuf)
	return api
}

func collectOptions(opts ...Option) *options {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

type options struct {
	logBuf   *logbuf.RingBuffer
	hub      *eventhub.Hub
	baseCtx  context.Context
	sessions auth.SessionLookup
}

type Option func(*options)

func WithLogBuffer(buf *logbuf.RingBuffer) Option {
	return func(o *options) {
		o.logBuf = buf
	}
}

func WithEventHub(hub *eventhub.Hub) Option {
	return func(o *options) {
		o.hub = hub
	}
}

func WithBaseContext(ctx context.Context) Option {
	return func(o *options) {
		o.baseCtx = ctx
	}
}

// WithSessions injects a SessionLookup for the auth middleware. Production
// callers don't need this — BuildAPI derives it from the App's DB pool —
// but tests can pass a mock (or nil to force every secured op to 401).
func WithSessions(s auth.SessionLookup) Option {
	return func(o *options) {
		o.sessions = s
	}
}
