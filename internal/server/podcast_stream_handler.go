package server

import (
	"io"
	"net/http"
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
func handlePodcastStream(client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		audioURL := r.URL.Query().Get("url")
		if audioURL == "" {
			writeError(w, http.StatusBadRequest, "missing url parameter")
			return
		}

		req, err := newPublicMediaRequest(r.Context(), audioURL)
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

		// newPublicMediaRequest validates the URL and production uses the
		// public-only transport; a custom client is accepted for isolated tests.
		resp, err := mediaHTTPClient(client).Do(req) //nolint:gosec
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to fetch episode")
			return
		}
		defer resp.Body.Close() //nolint:errcheck // defer close

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			status := http.StatusBadGateway
			if resp.StatusCode >= http.StatusBadRequest && resp.StatusCode <= 599 {
				status = resp.StatusCode
			}
			writeError(w, status, "episode upstream returned "+resp.Status)
			return
		}
		contentURL := audioURL
		if resp.Request != nil && resp.Request.URL != nil {
			contentURL = resp.Request.URL.String()
		}
		contentType, ok := safeAudioContentType(resp.Header.Get("Content-Type"), contentURL)
		if !ok {
			writeError(w, http.StatusBadGateway, "episode upstream returned non-audio content")
			return
		}

		// Mirror the headers a media element cares about. Anything else
		// (X-Powered-By, Set-Cookie, ...) gets dropped so the proxy stays
		// invisible to the client beyond the audio bytes themselves.
		passthrough := []string{"Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "ETag"}
		for _, k := range passthrough {
			if v := resp.Header.Get(k); v != "" {
				w.Header().Set(k, v)
			}
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}
