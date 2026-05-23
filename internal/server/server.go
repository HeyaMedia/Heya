package server

import (
	"context"
	"net"
	"net/http"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/service"
)

func New(cfg *config.Config, app *service.App, opts ...Option) *http.Server {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	mux := http.NewServeMux()

	registerRoutes(mux, app)
	registerTailscaleRoutes(mux, app, cfg)

	if o.logBuf != nil {
		registerLogRoutes(mux, app, o.logBuf)
	}

	if o.hub != nil {
		mux.HandleFunc("GET /api/ws", handleWebSocket(o.hub, app.SessionLookup()))
		// pprof + runtime stats. Admin-only because these dump goroutine
		// stacks, heap contents, and 30-second CPU profiles. The mux is
		// wrapped by the admin gate when we register it.
		debugMux := http.NewServeMux()
		registerDebugRoutes(debugMux, o.hub)
		mux.Handle("/api/debug/", auth.Middleware(app.SessionLookup())(adminOnly(debugMux)))
	}

	docsMux := http.NewServeMux()
	NewHumaAPI(docsMux, app)

	mux.Handle("GET /api/openapi.json", docsMux)
	mux.Handle("GET /api/openapi.yaml", docsMux)
	mux.HandleFunc("GET /api/docs", scalarHandler("/api/openapi.json"))

	mux.Handle("/", spaHandler())

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

type options struct {
	logBuf  *logbuf.RingBuffer
	hub     *eventhub.Hub
	baseCtx context.Context
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
