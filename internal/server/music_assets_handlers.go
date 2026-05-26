package server

import (
	"net/http"

	"github.com/karbowiak/heya/internal/imageserve"
	"github.com/karbowiak/heya/internal/service"
)

// handleAlbumCover serves the local cover.jpg for an album addressed by
// (artist_slug, album_slug). Falls back to a 302 when the album's cover_path
// still points at an upstream URL (heya.media / Deezer CDN) — that happens
// when no local sidecar / embedded art was found at refresh time. Browsers
// handle the 302 transparently for `<img src>` so the consumer doesn't need
// to branch.
//
// Path is /api/music/artists/{artist_slug}/albums/{album_slug}/cover so the
// album-cover URL stays human-readable in the network panel and aligns with
// the rest of the music surface (everything else addressable by slug uses it).
func handleAlbumCover(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		artistSlug := r.PathValue("artist_slug")
		albumSlug := r.PathValue("album_slug")
		if artistSlug == "" || albumSlug == "" {
			http.NotFound(w, r)
			return
		}
		id, err := app.ResolveAlbumIDBySlugs(r.Context(), artistSlug, albumSlug)
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
		app.ImageResizer().Serve(w, r, path, imageserve.ParseQuery(r.URL.Query()))
	}
}
