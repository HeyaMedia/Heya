package server

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/karbowiak/kura/internal/config"
)

func New(cfg *config.Config, db *pgxpool.Pool) *http.Server {
	mux := http.NewServeMux()
	registerRoutes(mux, db)

	handler := withMiddleware(mux)

	return &http.Server{
		Addr:    cfg.Addr(),
		Handler: handler,
	}
}
