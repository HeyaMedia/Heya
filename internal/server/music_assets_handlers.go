package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/karbowiak/heya/internal/imageserve"
	"github.com/karbowiak/heya/internal/publichttp"
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
			// Canvas consumers (tone sampling) opt into a byte proxy: the
			// 302 lands on third-party CDNs that send no CORS headers, so a
			// crossorigin image load fails and the canvas would taint.
			if r.URL.Query().Get("proxy") == "1" {
				proxyRemoteImage(w, r, path)
				return
			}
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

// proxyRemoteImage streams an upstream image through the server so the
// browser sees a same-origin response it can canvas-read. URL comes from
// the DB (provider metadata), never from the request. A non-image or error
// upstream just 404s — the consumer falls back gracefully.
func proxyRemoteImage(w http.ResponseWriter, r *http.Request, rawURL string) {
	image, err := publichttp.FetchImage(r.Context(), rawURL)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	publichttp.ServeImage(w, r, image, "public, max-age=3600")
}

// handlePlaylistCover serves a user playlist's custom cover file. Registered
// unauthenticated (see binary_huma.go's registerBinaryRoutes and
// GetUserPlaylistCoverPath's doc comment) so a plain <img src> works the same
// way it does for every other image endpoint in the app. Unlike the
// resized/cached media images, this is a small, rarely-resized custom upload
// — served as-is (no imageserve.Resizer) with a short private Cache-Control
// so a re-upload is picked up quickly instead of riding the resizer's
// week-long immutable cache.
func handlePlaylistCover(app *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		path, err := app.GetUserPlaylistCoverPath(r.Context(), 0, id)
		if err != nil || path == "" {
			http.NotFound(w, r)
			return
		}
		f, openErr := os.Open(path) //nolint:gosec // path is DataDir-joined, not user-controlled
		if openErr != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = f.Close() }()
		stat, statErr := f.Stat()
		if statErr != nil || !stat.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", imageserve.ContentTypeForPath(path))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, max-age=300")
		http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), f)
	}
}
