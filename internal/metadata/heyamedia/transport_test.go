package heyamedia

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLoggingTransport_PassesThrough verifies the wrapper is transparent:
// requests still reach the underlying transport and responses still come
// back intact. This is the important property — the logging side effects
// (DEBUG lines) aren't asserted here since they'd require capturing
// zerolog's global writer, and the real risk this test guards against is
// the wrapper accidentally swallowing the request/response or the body.
func TestLoggingTransport_PassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ping" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.RawQuery != "q=1" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	}))
	defer srv.Close()

	client := &http.Client{Transport: newLoggingTransport(nil)}
	resp, err := client.Get(srv.URL + "/api/v1/ping?q=1")
	if err != nil {
		t.Fatalf("request through wrapped transport failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(body) != "pong" {
		t.Fatalf("body = %q, want %q", body, "pong")
	}
}

// TestLoggingTransport_ErrorPassesThrough verifies transport errors (e.g.
// connection refused) still propagate unchanged instead of being absorbed
// by the logging wrapper.
func TestLoggingTransport_ErrorPassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // now nothing is listening

	client := &http.Client{Transport: newLoggingTransport(nil)}
	_, err := client.Get(url)
	if err == nil {
		t.Fatal("expected an error from a closed server, got nil")
	}
}

// TestNewLoggingTransport_DefaultsToDefaultTransport confirms passing nil
// falls back to http.DefaultTransport rather than leaving inner nil (which
// would panic on first RoundTrip).
func TestNewLoggingTransport_DefaultsToDefaultTransport(t *testing.T) {
	lt := newLoggingTransport(nil)
	if lt.inner != http.DefaultTransport {
		t.Fatalf("inner = %v, want http.DefaultTransport", lt.inner)
	}
}
