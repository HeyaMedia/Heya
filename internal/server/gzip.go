package server

import (
	"net/http"

	"github.com/klauspost/compress/gzhttp"
	"github.com/rs/zerolog/log"
)

// withGzip wraps the handler with gzip negotiation. We exclude content types
// that are either already compressed (images/video/audio binaries) or that
// must stream uninterrupted (HLS playlists, SSE, anything carrying an
// Upgrade header). Below the 1KB threshold gzip overhead would outweigh the
// savings, so we skip those too.
//
// Order matters in withMiddleware: gzip wraps INSIDE recovery + logging but
// OUTSIDE the actual handler. That way panics aren't half-compressed, and
// the access log sees the post-compression status code.
func withGzip(next http.Handler) http.Handler {
	wrap, err := gzhttp.NewWrapper(
		gzhttp.MinSize(1024),
		gzhttp.ContentTypes([]string{
			"application/json",
			"application/xml",
			"application/yaml",
			"application/javascript",
			"text/html",
			"text/plain",
			"text/css",
			"text/csv",
			"text/javascript",
			"text/xml",
			"text/vtt",
			"text/x-ssa",
			"image/svg+xml",
		}),
		gzhttp.ExceptContentTypes([]string{
			// Streaming responses — gzipping breaks Range/segment boundaries.
			"application/vnd.apple.mpegurl",
			"text/event-stream",
		}),
	)
	if err != nil {
		// gzhttp.NewWrapper only errors on invalid options, which we control —
		// log and pass through uncompressed rather than panic.
		log.Error().Err(err).Msg("gzip wrapper init failed; serving uncompressed")
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebSocket / HTTP/2 upgrades must not be wrapped — the response is
		// hijacked before any Content-Type is set, and the wrapper would
		// swallow the Hijacker interface.
		if r.Header.Get("Upgrade") != "" {
			next.ServeHTTP(w, r)
			return
		}
		wrap(next).ServeHTTP(w, r)
	})
}
