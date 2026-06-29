package server

import (
	"io"
	"net/http"
	"strings"
	"time"
)

// handleTMDBImageProxy serves images from image.tmdb.org through Heya so
// browsers don't talk to TMDB directly. Used for recommendation posters where
// we only have the upstream TMDB poster_path (no local asset yet).
//
// Path:  /api/tmdb/image/{path...}              (path is the TMDB poster path)
// Query: ?size=w92|w154|w185|w342|w500|w780|original   (default w342)
func handleTMDBImageProxy() http.HandlerFunc {
	allowedSizes := map[string]bool{
		"w92": true, "w154": true, "w185": true, "w342": true,
		"w500": true, "w780": true, "original": true,
	}
	const upstream = "https://image.tmdb.org/t/p/"

	return func(w http.ResponseWriter, r *http.Request) {
		path := r.PathValue("path")
		if path == "" {
			http.NotFound(w, r)
			return
		}
		// TMDB paths look like abcDEF.jpg — reject anything that escapes.
		if strings.Contains(path, "..") {
			http.NotFound(w, r)
			return
		}
		path = "/" + strings.TrimPrefix(path, "/")

		size := r.URL.Query().Get("size")
		if size == "" {
			size = "w342"
		}
		if !allowedSizes[size] {
			http.Error(w, "invalid size", http.StatusBadRequest)
			return
		}

		target := upstream + size + path

		// Both calls are flagged by gosec G704 (taint analysis) because `target`
		// is derived from r.URL — but we've already constrained path (rejects
		// "..") and size (allow-list) above, and the host is hard-coded.
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil) //nolint:gosec // G704: target is upstream + allow-listed size + sanitized path; no SSRF surface
		if err != nil {
			http.Error(w, "upstream error", http.StatusBadGateway)
			return
		}
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req) //nolint:gosec // G704: see above
		if err != nil {
			http.Error(w, "upstream unreachable", http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			http.Error(w, "upstream "+resp.Status, resp.StatusCode)
			return
		}
		if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(strings.ToLower(ct), "image/") {
			http.Error(w, "upstream returned non-image content", http.StatusBadGateway)
			return
		}

		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			w.Header().Set("Content-Length", cl)
		}
		w.Header().Set("Cache-Control", "public, max-age=604800, immutable")

		_, _ = io.Copy(w, resp.Body)
	}
}
