package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/karbowiak/heya/internal/safedial"
	"github.com/stretchr/testify/require"
)

// testPublicMediaClient intentionally maps public-looking test hostnames to a
// loopback httptest listener. Production never uses this trusted dial seam;
// its client retains safedial.Control after DNS resolution.
func testPublicMediaClient(t *testing.T, upstream *httptest.Server) *http.Client {
	t.Helper()
	parsed, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	dialer := &net.Dialer{}
	client := safedial.NewPublicHTTPClientWithDialContext(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, parsed.Host)
	})
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func TestPublicMediaHandlersRejectNonPublicTargetsBeforeDial(t *testing.T) {
	var dialed bool
	client := safedial.NewPublicHTTPClientWithDialContext(func(context.Context, string, string) (net.Conn, error) {
		dialed = true
		return nil, context.Canceled
	})
	t.Cleanup(client.CloseIdleConnections)

	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{name: "radio", handler: handleRadioStream(nil, 1, client)},
		{name: "podcast", handler: handlePodcastStream(client)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, target := range []string{
				"http://127.0.0.1/private",
				"http://10.0.0.1/private",
				"http://100.64.0.1/private",
				"http://169.254.169.254/latest/meta-data",
				"http://[::1]/private",
				"file:///etc/passwd",
			} {
				request := httptest.NewRequest(http.MethodGet, "/stream?url="+url.QueryEscape(target), nil)
				response := httptest.NewRecorder()
				test.handler.ServeHTTP(response, request)
				require.Equal(t, http.StatusBadRequest, response.Code, target)
			}
		})
	}
	require.False(t, dialed, "literal non-public targets must fail before dialing")
}

func TestSafeAudioContentType(t *testing.T) {
	allowed := []struct {
		name     string
		header   string
		url      string
		expected string
	}{
		{name: "missing inferred from path", url: "https://cdn.example.test/episode.m4a?format=.mp3", expected: "audio/mp4"},
		{name: "generic inferred from path", header: "application/octet-stream", url: "https://cdn.example.test/live.ogg", expected: "audio/ogg"},
		{name: "mp3 alias", header: "audio/mp3; charset=binary", expected: "audio/mpeg"},
		{name: "application ogg", header: "application/ogg", expected: "audio/ogg"},
		{name: "hls playlist", header: "application/x-mpegURL", expected: "application/vnd.apple.mpegurl"},
		{name: "radio subtype", header: "audio/aacp", expected: "audio/aacp"},
	}
	for _, test := range allowed {
		t.Run(test.name, func(t *testing.T) {
			contentType, ok := safeAudioContentType(test.header, test.url)
			require.True(t, ok)
			require.Equal(t, test.expected, contentType)
		})
	}

	for _, contentType := range []string{
		"text/html",
		"application/xhtml+xml",
		"application/xml",
		"application/javascript",
		"audio/ecmascript",
		"audio/svg",
		"image/svg+xml",
		"video/mp4",
		"audio/mpeg, text/html",
	} {
		t.Run("reject_"+contentType, func(t *testing.T) {
			_, ok := safeAudioContentType(contentType, "https://attacker.example.test/payload.mp3")
			require.False(t, ok)
		})
	}
}
