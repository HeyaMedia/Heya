package server

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

func registerRoutes(mux *http.ServeMux, db *pgxpool.Pool) {
	mux.HandleFunc("GET /api/health", healthHandler(db))
}
