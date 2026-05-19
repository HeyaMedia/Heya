package server

import (
	"net/http"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/service"
)

func New(cfg *config.Config, app *service.App) *http.Server {
	mux := http.NewServeMux()

	registerRoutes(mux, app)

	docsMux := http.NewServeMux()
	NewHumaAPI(docsMux, app)

	mux.Handle("GET /api/openapi.json", docsMux)
	mux.Handle("GET /api/openapi.yaml", docsMux)
	mux.HandleFunc("GET /api/docs", scalarHandler("/api/openapi.json"))

	handler := withMiddleware(mux)

	return &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,
	}
}
