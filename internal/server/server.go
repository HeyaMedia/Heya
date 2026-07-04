package server

import (
	"context"
	"net"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/jellyfin"
	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/service"
)

func New(cfg *config.Config, app *service.App, opts ...Option) *http.Server {
	mux := http.NewServeMux()
	BuildAPI(mux, app, cfg, opts...)

	o := collectOptions(opts...)

	// The catch-all is the Jellyfin-compatible surface wrapped around the
	// SPA: when the jellyfin.enabled toggle is on it claims its route tree
	// (/System/*, /Users/*, /Items/*, /socket, /emby/*...), everything else
	// — and everything when off — falls through to the SPA exactly as
	// before. See internal/jellyfin.
	mux.Handle("/", jellyfin.NewMiddleware(app, o.hub, spaHandler()))

	handler := withMiddleware(mux)
	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,
	}
	if o.baseCtx != nil {
		srv.BaseContext = func(_ net.Listener) context.Context { return o.baseCtx }
	}
	return srv
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
	api := newHumaAPI(mux, sessions)
	registerSystemRoutes(api, app)
	registerAuthRoutes(api, app)
	registerAdminRoutes(api, app, o.logBuf)
	registerAdminSystemRoutes(api, app, o.hub)
	registerTailscaleRoutes(api, app, cfg)
	registerJellyfinConfigRoutes(api, app)
	registerLibraryRoutes(api, app)
	registerJobRoutes(api, app)
	registerTaskRoutes(api, app)
	registerMediaRoutes(api, app)
	registerMetadataEditorRoutes(api, app)
	registerOpenSubtitlesRoutes(api, app)
	registerMusicRoutes(api, app)
	registerMusicHomeRoutes(api, app)
	registerMeRoutes(api, app)
	registerSessionRoutes(api, app)
	registerRadioRoutes(api, app)
	registerPodcastRoutes(api, app)
	registerStreamRoutes(api, app)
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
