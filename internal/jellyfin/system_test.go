package jellyfin

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestBaseURLIncludesJellyfinMount(t *testing.T) {
	t.Run("direct TLS", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "https://heya.example/System/Info/Public", nil)
		r.TLS = &tls.ConnectionState{}
		if got, want := requestBaseURL(r), "https://heya.example/jellyfin"; got != want {
			t.Fatalf("requestBaseURL() = %q, want %q", got, want)
		}
	})

	t.Run("forwarded origin", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/System/Info/Public", nil)
		r.Header.Set("X-Forwarded-Proto", "https, http")
		r.Header.Set("X-Forwarded-Host", "media.example, internal")
		if got, want := requestBaseURL(r), "https://media.example/jellyfin"; got != want {
			t.Fatalf("requestBaseURL() = %q, want %q", got, want)
		}
	})
}
