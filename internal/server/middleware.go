package server

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/requestmeta"
	"github.com/rs/zerolog/log"
)

func withMiddleware(h http.Handler) http.Handler {
	// Outermost first: recovery → request metadata → logging → security
	// headers → CSRF gate → request limits → CORS → gzip → etag → handler.
	// Recovery wraps everything so panics from gzip/etag/logging still return
	// 500. Gzip sits inside CORS so OPTIONS replies (no body) skip it. ETag
	// sits inside gzip so the hash is computed over uncompressed bytes (a
	// gzip compressor upgrade must not silently invalidate browser caches).
	return withRecovery(withRequestMetadata(withLogging(withSecurityHeaders(withCSRFGate(withRequestBodyLimit(withCORS(withGzip(withETag(h)))))))))
}

const maxApplicationBodyBytes int64 = 1 << 20

func withRequestMetadata(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, requestmeta.WithClientIP(r))
	})
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		// Start CSP as reporting-only: the SPA and compatibility clients need a
		// measured policy before enforcement, while every other header here is
		// safe to enforce immediately.
		w.Header().Set("Content-Security-Policy-Report-Only", "default-src 'self'; base-uri 'self'; frame-ancestors 'none'; object-src 'none'")
		if ingress := requestmeta.Ingress(r.Context()); ingress == "remote" || ingress == "funnel" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

func withRequestBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestMayHaveBody(r.Method) && !isImageUploadPath(r.URL.Path) {
			if r.ContentLength > maxApplicationBodyBytes {
				http.Error(w, "request body is too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxApplicationBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

func withCSRFGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestMayHaveBody(r.Method) && cookieSessionRequest(r) && r.Header.Get("Authorization") == "" {
			origin, err := url.Parse(r.Header.Get("Origin"))
			if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host != r.Host {
				http.Error(w, "cross-origin cookie request rejected", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func cookieSessionRequest(r *http.Request) bool {
	cookie, err := r.Cookie("session_token")
	return err == nil && cookie.Value != ""
}

func requestMayHaveBody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isImageUploadPath(path string) bool {
	return (strings.HasPrefix(path, "/api/media/") && strings.HasSuffix(path, "/assets/upload")) ||
		(strings.HasPrefix(path, "/api/me/playlists/") && strings.HasSuffix(path, "/cover"))
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", sw.status).
			Dur("duration", time.Since(start)).
			Msg("request")
	})
}

func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &startedWriter{ResponseWriter: w}
		defer func() {
			err := recover()
			if err == nil {
				return
			}
			// A client that goes away mid-response is normal traffic, not a
			// server error: ReverseProxy (the passive-mode image proxy) and
			// ServeContent panic with ErrAbortHandler when the peer hangs up.
			// Re-panic so net/http aborts the connection silently — logging it
			// as ERR and stuffing a 500 into a half-written response was pure
			// noise ("superfluous WriteHeader" spam under Feishin's cover-art
			// request storms).
			if err == http.ErrAbortHandler { //nolint:errorlint // sentinel comparison on a recover() value, by contract
				panic(err)
			}
			log.Error().
				Str("panic", fmt.Sprintf("%v", err)).
				Str("path", r.URL.Path).
				Str("stack", string(debug.Stack())).
				Msg("panic recovered")
			if !sw.started {
				http.Error(sw, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(sw, r)
	})
}

// startedWriter tracks whether any part of the response reached the wire, so
// the recovery path knows a 500 is still writable.
type startedWriter struct {
	http.ResponseWriter
	started bool
}

func (w *startedWriter) WriteHeader(code int) {
	w.started = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *startedWriter) Write(b []byte) (int, error) {
	w.started = true
	return w.ResponseWriter.Write(b)
}

func (w *startedWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *startedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("hijack not supported")
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		// The X-Emby-* / X-MediaBrowser-Token names are the Jellyfin client
		// auth headers (internal/jellyfin) — required for browser-based
		// Jellyfin clients (jellyfin-web) hosted on another origin.
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Emby-Authorization, X-Emby-Token, X-MediaBrowser-Token")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("hijack not supported")
}
