// Package httpbodylimit bounds response bodies without imposing any policy on
// which hosts an HTTP client may contact.
package httpbodylimit

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrResponseBodyTooLarge reports that a response exceeded its configured
// decoded-body limit.
var ErrResponseBodyTooLarge = errors.New("HTTP response body exceeds size limit")

// NewTransport wraps base and caps every response body at maxBytes. A nil base
// preserves net/http's default transport. Oversized declared Content-Length
// values fail before a body is read; unknown or misleading lengths fail as
// soon as one byte beyond the limit is observed.
//
// The wrapper deliberately does not validate destinations, resolve DNS, or
// alter redirect behavior. It is therefore suitable for clients that are
// intentionally allowed to contact configured private or LAN services.
func NewTransport(base http.RoundTripper, maxBytes int64) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{base: base, maxBytes: maxBytes}
}

type transport struct {
	base     http.RoundTripper
	maxBytes int64
}

func (t *transport) RoundTrip(request *http.Request) (*http.Response, error) {
	if t.maxBytes <= 0 {
		return nil, errors.New("httpbodylimit: max bytes must be positive")
	}
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return response, err
	}
	if response == nil || response.Body == nil {
		return response, nil
	}
	if response.ContentLength > t.maxBytes {
		_ = response.Body.Close()
		return nil, fmt.Errorf("%w: declared %d bytes, limit %d", ErrResponseBodyTooLarge, response.ContentLength, t.maxBytes)
	}
	response.Body = &readCloser{
		body:      response.Body,
		remaining: t.maxBytes,
		limit:     t.maxBytes,
	}
	return response, nil
}

type readCloser struct {
	body      io.ReadCloser
	remaining int64
	limit     int64
}

func (r *readCloser) Read(destination []byte) (int, error) {
	if len(destination) == 0 {
		return r.body.Read(destination)
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.body.Read(probe[:])
		if n > 0 {
			return 0, fmt.Errorf("%w: limit %d", ErrResponseBodyTooLarge, r.limit)
		}
		return 0, err
	}

	readSize := int64(len(destination))
	if readSize > r.remaining {
		// Read one byte beyond the remaining budget so an unknown-length body
		// fails during this call instead of appearing as a valid truncation.
		readSize = r.remaining + 1
	}
	n, err := r.body.Read(destination[:readSize])
	if int64(n) > r.remaining {
		allowed := int(r.remaining)
		r.remaining = 0
		return allowed, fmt.Errorf("%w: limit %d", ErrResponseBodyTooLarge, r.limit)
	}
	r.remaining -= int64(n)
	return n, err
}

func (r *readCloser) Close() error {
	return r.body.Close()
}
