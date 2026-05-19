package server

import (
	"net/http"

	"github.com/karbowiak/kura/internal/auth"
	"github.com/karbowiak/kura/internal/service"
)

func registerRoutes(mux *http.ServeMux, app *service.App) {
	mux.HandleFunc("GET /api/health", healthHandler(app.DB))

	mux.HandleFunc("POST /api/auth/register", handleRegister(app))
	mux.HandleFunc("POST /api/auth/login", handleLogin(app))
	mux.HandleFunc("POST /api/auth/logout", handleLogout(app))

	authed := auth.Middleware(app.Queries())

	mux.Handle("GET /api/auth/me", authed(http.HandlerFunc(handleMe(app))))

	mux.Handle("POST /api/libraries", authed(http.HandlerFunc(handleCreateLibrary(app))))
	mux.Handle("GET /api/libraries", authed(http.HandlerFunc(handleListLibraries(app))))
	mux.Handle("GET /api/libraries/{id}", authed(http.HandlerFunc(handleGetLibrary(app))))
	mux.Handle("PUT /api/libraries/{id}", authed(http.HandlerFunc(handleUpdateLibrary(app))))
	mux.Handle("DELETE /api/libraries/{id}", authed(http.HandlerFunc(handleDeleteLibrary(app))))

	mux.Handle("POST /api/libraries/{id}/scan", authed(http.HandlerFunc(handleScanLibrary(app))))
	mux.Handle("GET /api/libraries/{id}/files", authed(http.HandlerFunc(handleListLibraryFiles(app))))
	mux.Handle("GET /api/libraries/{id}/files/stats", authed(http.HandlerFunc(handleLibraryFileStats(app))))
	mux.Handle("GET /api/libraries/{id}/unmatched", authed(http.HandlerFunc(handleListUnmatched(app))))

	mux.Handle("POST /api/library-files/{id}/resolve", authed(http.HandlerFunc(handleResolveMatch(app))))

	mux.Handle("GET /api/media", authed(http.HandlerFunc(handleListMedia(app))))
	mux.Handle("GET /api/media/{id}", authed(http.HandlerFunc(handleGetMedia(app))))
	mux.Handle("GET /api/search", authed(http.HandlerFunc(handleSearchMedia(app))))
}
