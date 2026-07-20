package requestmeta

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithClientIPUsesDirectPeerOnly(t *testing.T) {
	r := httptest.NewRequest("GET", "https://heya.example/api/auth/login", nil)
	r.RemoteAddr = "203.0.113.7:43210"
	r.Header.Set("X-Forwarded-For", "198.51.100.9")

	r = WithClientIP(r)

	assert.Equal(t, "203.0.113.7", ClientIP(r.Context()))
}

func TestIngressRoundTrip(t *testing.T) {
	r := httptest.NewRequest("GET", "https://heya.example/", nil)
	ctx := WithIngress(r.Context(), "remote")
	assert.Equal(t, "remote", Ingress(ctx))
}

func TestSecureTransportIncludesTLSAndPreterminatedIngress(t *testing.T) {
	r := httptest.NewRequest("GET", "https://heya.example/", nil)
	r = WithClientIP(r)
	assert.True(t, SecureTransport(r.Context()))

	ctx := WithSecureTransport(r.Context(), true)
	assert.True(t, SecureTransport(ctx))
}
