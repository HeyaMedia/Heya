package server

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// etagBufferLimit caps how much body we'll buffer to compute an ETag. Above
// this we drop to streaming so a huge response doesn't blow up memory.
const etagBufferLimit = 1 << 20 // 1 MiB

// withETag buffers JSON GET/HEAD responses, computes a SHA-256-prefix ETag,
// sets the header, and responds 304 when the client's If-None-Match matches.
// Anything that can't be cleanly buffered (Hijack for WS, Flush for SSE/HLS,
// non-JSON content, oversize body, non-200 status) falls through to streaming
// mode and is sent through untouched.
//
// Sits INSIDE withGzip so the hash is computed over the uncompressed body —
// otherwise the ETag would depend on the compressor version, which would
// silently invalidate caches whenever klauspost ships a new gzip release.
func withETag(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		// The Jellyfin-compatible surface must never be ETagged: a real
		// Jellyfin doesn't 304 its API JSON, and strict clients (Infuse)
		// error on the empty conditional body they've never seen from a
		// real server.
		if isJellyfinPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		ew := &etagWriter{ResponseWriter: w, req: r}
		next.ServeHTTP(ew, r)
		ew.finalize()
	})
}

func isJellyfinPath(path string) bool {
	if strings.EqualFold(path, "/jellyfin") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(path), "/jellyfin/")
}

type etagWriter struct {
	http.ResponseWriter
	req       *http.Request
	buf       bytes.Buffer
	code      int
	streaming bool // committed to pass-through; finalize is a no-op once set
	decided   bool // first write seen — content-type evaluated
}

func (w *etagWriter) WriteHeader(code int) {
	if w.streaming {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	w.code = code
}

func (w *etagWriter) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	if w.streaming {
		return w.ResponseWriter.Write(p)
	}
	if !w.decided {
		w.decided = true
		ct := w.Header().Get("Content-Type")
		if w.code != http.StatusOK || !strings.Contains(ct, "json") {
			w.startStreaming()
			return w.ResponseWriter.Write(p)
		}
	}
	if w.buf.Len()+len(p) > etagBufferLimit {
		w.startStreaming()
		return w.ResponseWriter.Write(p)
	}
	return w.buf.Write(p)
}

// startStreaming commits whatever we've buffered so far to the underlying
// writer and switches to pass-through. Hijack callers must NOT trigger this
// path — they need the writer untouched for the protocol upgrade.
func (w *etagWriter) startStreaming() {
	if w.streaming {
		return
	}
	w.streaming = true
	if w.code != 0 {
		w.ResponseWriter.WriteHeader(w.code)
	}
	if w.buf.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.buf.Bytes()) //nolint:gosec // G705 false positive: buf holds an already-rendered JSON body from a Huma operation; we relay bytes verbatim with no HTML context
		w.buf.Reset()
	}
}

// finalize runs after the wrapped handler returns. If we still hold the body
// in the buffer we hash it, set ETag, honor If-None-Match, and write the
// response.
func (w *etagWriter) finalize() {
	if w.streaming {
		return
	}
	if w.code == 0 {
		w.code = http.StatusOK
	}
	if w.buf.Len() == 0 {
		w.ResponseWriter.WriteHeader(w.code)
		return
	}
	// no-store explicitly forbids the client from caching, so there's nothing
	// to revalidate against — skip the hash work.
	if strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		w.ResponseWriter.WriteHeader(w.code)
		_, _ = w.ResponseWriter.Write(w.buf.Bytes()) //nolint:gosec // G705 false positive: buf holds an already-rendered JSON body from a Huma operation; we relay bytes verbatim with no HTML context
		return
	}
	sum := sha256.Sum256(w.buf.Bytes())
	etag := fmt.Sprintf(`"%s"`, hex.EncodeToString(sum[:16]))
	w.Header().Set("ETag", etag)
	if matchETag(w.req.Header.Get("If-None-Match"), etag) {
		// 304: stdlib strips Content-Length/Type for us when we don't write
		// a body. Cache-Control + ETag are preserved.
		w.ResponseWriter.WriteHeader(http.StatusNotModified)
		return
	}
	w.ResponseWriter.WriteHeader(w.code)
	_, _ = w.ResponseWriter.Write(w.buf.Bytes())
}

// matchETag does loose RFC 7232 If-None-Match handling: comma-separated list,
// wildcard match, weak-tag tolerance. We never emit weak tags but a client
// echoing `W/"foo"` for our strong `"foo"` should still match.
func matchETag(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	if strings.TrimSpace(ifNoneMatch) == "*" {
		return true
	}
	for _, part := range strings.Split(ifNoneMatch, ",") {
		p := strings.TrimPrefix(strings.TrimSpace(part), "W/")
		if p == etag {
			return true
		}
	}
	return false
}

func (w *etagWriter) Flush() {
	w.startStreaming()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack must NOT flush our buffer — the connection is about to be torn out
// from under net/http for protocol upgrade. We just mark streaming so
// finalize is a no-op.
func (w *etagWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.streaming = true
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}
