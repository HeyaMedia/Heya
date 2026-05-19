package server

import (
	"net/http"

	"github.com/karbowiak/kura/internal/config"
	"github.com/karbowiak/kura/internal/service"
)

func New(cfg *config.Config, app *service.App) *http.Server {
	mux := http.NewServeMux()
	registerRoutes(mux, app)

	handler := withMiddleware(mux)

	return &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,
	}
}
