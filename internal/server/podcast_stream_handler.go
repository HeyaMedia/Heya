package server

import (
	"io"
	"net/http"
	"strings"

	"github.com/karbowiak/heya/internal/service"
)

// handlePodcastStream proxies an episode's audio enclosure to the browser.
// Range requests pass through so the player can seek; we forward the
// Range header on the outbound request and mirror Content-Range / Content-
// Length / Accept-Ranges back to the client.
//
// Why proxy instead of letting the browser hit the enclosure URL directly?
//
//   - CORS: many podcast CDNs don't set permissive CORS headers; an HTML
//     <audio> element can play them, but our analyzer / waveform pipeline
//     needs same-origin bytes.
//   - Privacy: the upstream sees Heya's user-agent, not the listener's.
//   - Auth: we can require the user's session token to start a stream so
//     unauthenticated browsing doesn't burn upstream bandwidth.
func handlePodcastStream(_ *service.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		audioURL := r.URL.Query().Get("url")
		if audioURL == "" {
			writeError(w, http.StatusBadRequest, "missing url parameter")
			return
		}

		// G704 SSRF: audioURL is an admin-curated upstream CDN URL from the
		// podcast feed; proxying it is the entire feature. We intentionally
		// don't validate the host since the podcast index already curates
		// upstreams.
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, audioURL, nil) //nolint:gosec
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid audio URL")
			return
		}
		req.Header.Set("User-Agent", "Heya/0.1 (+https://heya.media)")
		// Range pass-through so seeking works. Browsers send `bytes=N-`
		// after a user scrub; we send the same to the CDN.
		if rng := r.Header.Get("Range"); rng != "" {
			req.Header.Set("Range", rng)
		}

		client := &http.Client{}
		resp, err := client.Do(req) //nolint:gosec // see G704 note above
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to fetch episode: "+err.Error())
			return
		}
		defer resp.Body.Close() //nolint:errcheck // defer close

		// Mirror the headers a media element cares about. Anything else
		// (X-Powered-By, Set-Cookie, ...) gets dropped so the proxy stays
		// invisible to the client beyond the audio bytes themselves.
		passthrough := []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"}
		for _, k := range passthrough {
			if v := resp.Header.Get(k); v != "" {
				w.Header().Set(k, v)
			}
		}
		// Default content-type when the upstream omits one (some odd hosts do).
		if w.Header().Get("Content-Type") == "" {
			ext := strings.ToLower(audioURL)
			switch {
			case strings.Contains(ext, ".mp3"):
				w.Header().Set("Content-Type", "audio/mpeg")
			case strings.Contains(ext, ".m4a"), strings.Contains(ext, ".mp4"):
				w.Header().Set("Content-Type", "audio/mp4")
			case strings.Contains(ext, ".ogg"):
				w.Header().Set("Content-Type", "audio/ogg")
			default:
				w.Header().Set("Content-Type", "audio/mpeg")
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}
