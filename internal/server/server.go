package server

import (
	"net/http"

	"github.com/karbowiak/kura/internal/config"
	"github.com/karbowiak/kura/internal/service"
)

func New(cfg *config.Config, app *service.App) *http.Server {
	mux := http.NewServeMux()

	registerRoutes(mux, app)

	docsMux := http.NewServeMux()
	NewHumaAPI(docsMux, app)

	mux.Handle("GET /api/openapi.json", docsMux)
	mux.Handle("GET /api/openapi.yaml", docsMux)
	mux.Handle("GET /api/docs", docsMux)

	handler := withMiddleware(mux)

	return &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,
	}
}
