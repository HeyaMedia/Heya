package heyamedia

import (
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// loggingTransport is an http.RoundTripper that emits one DEBUG line per
// upstream heya.media request. It exists because today enrichment failures
// are invisible at the transport level — workers log what they were trying
// to do, but never the actual request/response that hit heya.media, so
// diagnosing a misbehaving upstream means guessing. The global log level is
// runtime-switchable (PUT /api/admin/log-level), so these lines cost nothing
// until someone flips to debug.
//
// It deliberately never touches headers, in either direction. There's no
// Authorization header on heya.media calls today, but there may be one in
// the future, and the safest way to guarantee it never leaks into logs is to
// never read request.Header or response.Header at all rather than trust a
// redaction list to stay complete.
//
// It also never reads the response body — only Content-Length, which is a
// header value the net/http stack already parsed for us. Draining or
// wrapping resp.Body here would risk perturbing streaming reads or doubling
// memory for large payloads, for a debug log line that isn't worth that
// risk.
type loggingTransport struct {
	inner http.RoundTripper
}

// newLoggingTransport wraps inner (falling back to http.DefaultTransport if
// nil) with request/response debug logging.
func newLoggingTransport(inner http.RoundTripper) *loggingTransport {
	if inner == nil {
		inner = http.DefaultTransport
	}
	return &loggingTransport{inner: inner}
}

// RoundTrip logs the request/response pair at DEBUG and delegates to the
// wrapped transport. On error it logs at DEBUG only — the caller (Client /
// HeyaProvider) already turns transport and non-2xx errors into
// upstreamErr, which gets logged wherever it's ultimately handled; logging
// the same failure again here at WARN/ERROR would just double it up.
func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	method := req.Method
	path := req.URL.Path
	query := req.URL.RawQuery

	resp, err := t.inner.RoundTrip(req)
	elapsed := time.Since(start)

	if err != nil {
		log.Debug().
			Err(err).
			Str("method", method).
			Str("path", path).
			Dur("duration_ms", elapsed).
			Msg("heya.media request")
		return resp, err
	}

	log.Debug().
		Str("method", method).
		Str("path", path).
		Str("query", query).
		Int("status", resp.StatusCode).
		Dur("duration_ms", elapsed).
		Int64("content_length", resp.ContentLength).
		Msg("heya.media request")

	return resp, err
}
