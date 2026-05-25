package server

import (
	"net/http"
	"strconv"

	"github.com/karbowiak/heya/internal/service"
)

// handleAlbumCover serves the local cover.jpg for an album, falling back to
// a redirect when the album's cover_path still points at an upstream URL
// (heya.media / Deezer CDN) — that happens when no local sidecar / embedded
// art was found at refresh time. Browsers handle the 302 transparently for
// `<img src>` so the consumer doesn't need to branch.
func handleAlbumCover(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		path, remote, ok := app.GetAlbumCover(r.Context(), id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if remote {
			// Upstream HTTPS URL — bounce the client. Cache the redirect
			// briefly so a swept rail of albums doesn't re-resolve every
			// scroll-into-view, but not so long that a follow-up refresh
			// (which would replace the URL with a local file) gets stuck
			// behind a stale 302.
			w.Header().Set("Cache-Control", "public, max-age=300")
			http.Redirect(w, r, path, http.StatusFound)
			return
		}
		serveFile(w, r, path)
	}
}
