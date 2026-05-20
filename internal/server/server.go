package server

import (
	"net/http"

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

	if o.logBuf != nil {
		registerLogRoutes(mux, app, o.logBuf)
	}

	if o.hub != nil {
		mux.HandleFunc("GET /api/ws", handleWebSocket(o.hub, app.Queries()))
	}

	docsMux := http.NewServeMux()
	NewHumaAPI(docsMux, app)

	mux.Handle("GET /api/openapi.json", docsMux)
	mux.Handle("GET /api/openapi.yaml", docsMux)
	mux.HandleFunc("GET /api/docs", scalarHandler("/api/openapi.json"))

	mux.Handle("/", spaHandler())

	handler := withMiddleware(mux)

	return &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,
	}
}

type options struct {
	logBuf *logbuf.RingBuffer
	hub    *eventhub.Hub
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
