package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/requestmeta"
	"github.com/stretchr/testify/assert"
)

func TestSecurityHeadersAreEnforcedAtTheApplicationBoundary(t *testing.T) {
	h := withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request = request.WithContext(requestmeta.WithIngress(request.Context(), "remote"))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)

	assert.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", response.Header().Get("X-Frame-Options"))
	assert.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))
	assert.Equal(t, "max-age=31536000", response.Header().Get("Strict-Transport-Security"))
	assert.Contains(t, response.Header().Get("Content-Security-Policy-Report-Only"), "frame-ancestors 'none'")
}

func TestHSTSIsLimitedToPublicTLSIngress(t *testing.T) {
	h := withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, ingress := range []string{"", "host", "tailnet"} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request = request.WithContext(requestmeta.WithIngress(request.Context(), ingress))
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		assert.Empty(t, response.Header().Get("Strict-Transport-Security"), ingress)
	}
}

func TestApplicationRequestBodyLimit(t *testing.T) {
	reached := false
	h := withRequestBodyLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(strings.Repeat("x", int(maxApplicationBodyBytes)+1)))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)

	assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	assert.False(t, reached)
}

func TestApplicationRequestBodyLimitLeavesBoundedImageUploadsToHuma(t *testing.T) {
	reached := false
	h := withRequestBodyLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/media/42/assets/upload", strings.NewReader(strings.Repeat("x", int(maxApplicationBodyBytes)+1)))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)

	assert.Equal(t, http.StatusNoContent, response.Code)
	assert.True(t, reached)
}

func TestCSRFGateRequiresSameOriginForCookieMutations(t *testing.T) {
	h := withCSRFGate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, test := range []struct {
		name   string
		origin string
		want   int
	}{
		{name: "same origin", origin: "https://heya.example:8443", want: http.StatusNoContent},
		{name: "cross origin", origin: "https://evil.example", want: http.StatusForbidden},
		{name: "missing origin", want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "https://heya.example:8443/api/me/settings", nil)
			request.AddCookie(&http.Cookie{Name: "session_token", Value: "secret"})
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response := httptest.NewRecorder()
			h.ServeHTTP(response, request)
			assert.Equal(t, test.want, response.Code)
		})
	}
}
