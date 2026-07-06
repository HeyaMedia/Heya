package heyamedia

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
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

// Throughput + backoff knobs. heya.media does its own upstream rate limiting,
// so Heya's job is to (a) not open an unbounded number of sockets when many
// parallel workers hammer it at once, and (b) back off gracefully when it
// signals overload (429 / Retry-After) instead of treating that as a hard
// failure. The starting numbers are conservative; tune the ceiling upward
// only while heya.media's observed 429 rate stays near zero. A cold entity is
// floored by heya.media's own per-provider limiter regardless, so pushing the
// ceiling past ~8 buys little until it grows a batch endpoint.
const (
	heyaMaxConcurrent = 8                // in-flight requests to heya.media, process-wide
	heyaMaxRetries    = 3                // client-side retries per logical call
	heyaBaseBackoff   = 1 * time.Second  // first backoff step
	heyaMaxBackoff    = 30 * time.Second // backoff ceiling
	heyaMaxRetryAfter = 30 * time.Second // cap on an honored Retry-After
)

// newBaseTransport returns an *http.Transport tuned for parallel heya.media
// traffic. Cloning http.DefaultTransport keeps its sane dial/TLS/proxy
// defaults; we only raise the per-host pool (stock is 2) so a burst reuses
// warm connections instead of paying a fresh TLS handshake each time, and add
// a hard per-host cap as defense-in-depth if HTTP/2 multiplexing isn't
// negotiated for some reason.
func newBaseTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100
	t.MaxIdleConnsPerHost = 16
	t.MaxConnsPerHost = 16
	t.ForceAttemptHTTP2 = true
	return t
}

// retryTransport bounds concurrency to heya.media with a semaphore and retries
// transient failures (429/5xx, connection blips, the client's own timeout)
// with capped exponential backoff, honoring a server-sent Retry-After. It sits
// OUTSIDE loggingTransport so every attempt is traced. Critically, the
// semaphore slot is released BEFORE any backoff sleep — a throttled request
// must never head-of-line-block the other slots while it waits out a
// Retry-After. All heya.media calls are GETs with no request body, so replay
// is safe.
type retryTransport struct {
	inner http.RoundTripper
	sem   chan struct{}
}

func newRetryTransport(inner http.RoundTripper) *retryTransport {
	return &retryTransport{inner: inner, sem: make(chan struct{}, heyaMaxConcurrent)}
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	backoff := heyaBaseBackoff

	for attempt := 0; ; attempt++ {
		// Acquire a concurrency slot (or bail if the caller's context ends).
		select {
		case t.sem <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		resp, err := t.inner.RoundTrip(req)
		<-t.sem // release before any retry decision / backoff sleep

		retry := false
		var wait time.Duration
		switch {
		case err != nil:
			retry = isTransientTransportErr(err)
		case isRetryableStatus(resp.StatusCode):
			retry = true
			wait = retryAfter(resp)
		}

		if !retry || attempt >= heyaMaxRetries {
			return resp, err
		}

		// Drain+close the retryable response so the connection can be reused,
		// then wait (server-directed Retry-After if present, else backoff).
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
			_ = resp.Body.Close()
		}
		if wait <= 0 {
			wait = jitter(backoff)
		}
		log.Debug().
			Int("attempt", attempt+1).
			Int("status", statusOf(resp)).
			Dur("wait", wait).
			Str("path", req.URL.Path).
			Err(err).
			Msg("heya.media retry")
		if !sleepCtx(ctx, wait) {
			return nil, ctx.Err()
		}
		if backoff *= 2; backoff > heyaMaxBackoff {
			backoff = heyaMaxBackoff
		}
	}
}

// isRetryableStatus reports whether an HTTP status is a transient upstream
// state worth retrying. 501 is deliberately excluded: heya.media returns it
// for a genuine book miss, not a transient fault.
func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, // 408
		http.StatusTooManyRequests,     // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout:      // 504
		return true
	}
	return false
}

// isTransientTransportErr classifies a RoundTrip-level error (no HTTP response)
// as retryable. Caller cancellation is not retryable; a timeout or any net.Error
// (dial refused/reset, DNS, TLS) is.
func isTransientTransportErr(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF)
}

// retryAfter parses a Retry-After header (delta-seconds or HTTP-date), capped
// so one bad value can't park a worker indefinitely. Returns 0 when absent or
// unparseable (caller falls back to exponential backoff).
func retryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	v := resp.Header.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return capDuration(time.Duration(secs) * time.Second)
	}
	if when, err := http.ParseTime(v); err == nil {
		return capDuration(time.Until(when))
	}
	return 0
}

func capDuration(d time.Duration) time.Duration {
	switch {
	case d < 0:
		return 0
	case d > heyaMaxRetryAfter:
		return heyaMaxRetryAfter
	default:
		return d
	}
}

// jitter applies ±20% so retrying workers don't resynchronize into a thundering
// herd against heya.media.
func jitter(d time.Duration) time.Duration {
	delta := int64(d) / 5
	if delta <= 0 {
		return d
	}
	return d + time.Duration(rand.Int64N(2*delta)-delta) //nolint:gosec // non-crypto jitter for backoff, not security-sensitive
}

// sleepCtx waits for d or ctx cancellation; returns false if ctx ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func statusOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
