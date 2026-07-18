package safedial

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateHTTPURL(t *testing.T) {
	allowed := []string{
		"http://media.example.test/audio.mp3",
		"https://8.8.8.8/audio.mp3",
		"https://[2606:4700:4700::1111]/audio.mp3",
	}
	blocked := []string{
		"file:///etc/passwd",
		"ftp://example.com/audio.mp3",
		"http:///missing-host",
		"http://localhost/audio.mp3",
		"http://radio.localhost./audio.mp3",
		"http://127.0.0.1/audio.mp3",
		"http://10.0.0.1/audio.mp3",
		"http://100.64.0.1/audio.mp3",
		"http://169.254.169.254/latest/meta-data",
		"http://[::1]/audio.mp3",
		"http://[fc00::1]/audio.mp3",
	}
	for _, raw := range allowed {
		t.Run("allow_"+raw, func(t *testing.T) {
			target, err := url.Parse(raw)
			require.NoError(t, err)
			require.NoError(t, ValidateHTTPURL(target))
		})
	}
	for _, raw := range blocked {
		t.Run("block_"+raw, func(t *testing.T) {
			target, err := url.Parse(raw)
			require.NoError(t, err)
			require.Error(t, ValidateHTTPURL(target))
		})
	}
}

func TestPublicHTTPClientDisablesEnvironmentProxy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "audio")
	}))
	t.Cleanup(upstream.Close)
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)

	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")

	var dialed string
	dialer := &net.Dialer{}
	client := NewPublicHTTPClientWithDialContext(func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = address
		return dialer.DialContext(ctx, network, upstreamURL.Host)
	})
	t.Cleanup(client.CloseIdleConnections)

	response, err := client.Get("http://media.example.test/audio.mp3")
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "media.example.test:80", dialed)
}

func TestPublicHTTPClientRejectsLoopbackDestination(t *testing.T) {
	var reached atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached.Store(true)
	}))
	t.Cleanup(upstream.Close)

	client := NewPublicHTTPClient()
	t.Cleanup(client.CloseIdleConnections)
	_, err := client.Get(upstream.URL)
	require.Error(t, err)
	require.False(t, reached.Load(), "blocked loopback target was reached")
}

func TestPublicHTTPClientOptionsOnlyTuneConnectionPool(t *testing.T) {
	client := NewPublicHTTPClientWithOptions(PublicHTTPClientOptions{
		MaxIdleConns:        77,
		MaxIdleConnsPerHost: 11,
	})
	t.Cleanup(client.CloseIdleConnections)
	transport, ok := client.Transport.(*publicHTTPTransport)
	require.True(t, ok)
	require.Equal(t, 77, transport.base.MaxIdleConns)
	require.Equal(t, 11, transport.base.MaxIdleConnsPerHost)
	require.Nil(t, transport.base.Proxy)
	require.NotNil(t, transport.base.DialContext)
	require.Equal(t, publicResponseHeaderTimeout, transport.base.ResponseHeaderTimeout)
}

func TestPublicHTTPClientHeaderTimeoutDoesNotLimitStreamingBody(t *testing.T) {
	headerRelease := make(chan struct{})
	defer close(headerRelease)
	bodyRelease := make(chan struct{})
	bodyReleased := false
	defer func() {
		if !bodyReleased {
			close(bodyRelease)
		}
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/headers":
			<-headerRelease
		case "/body":
			w.Header().Set("Content-Type", "audio/mpeg")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-bodyRelease
			_, _ = io.WriteString(w, "audio")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	dialer := &net.Dialer{}
	client := NewPublicHTTPClientWithDialContext(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, serverURL.Host)
	})
	t.Cleanup(client.CloseIdleConnections)
	const testHeaderTimeout = 25 * time.Millisecond
	transport := client.Transport.(*publicHTTPTransport)
	transport.base.ResponseHeaderTimeout = testHeaderTimeout

	_, err = client.Get("http://media.example.test/headers")
	require.Error(t, err, "a connected endpoint that never sends headers must time out")

	response, err := client.Get("http://media.example.test/body")
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	bodyResult := make(chan struct {
		body []byte
		err  error
	}, 1)
	go func() {
		body, readErr := io.ReadAll(response.Body)
		bodyResult <- struct {
			body []byte
			err  error
		}{body: body, err: readErr}
	}()

	select {
	case result := <-bodyResult:
		t.Fatalf("streaming body ended during response-header timeout window: body=%q err=%v", result.body, result.err)
	case <-time.After(3 * testHeaderTimeout):
	}
	close(bodyRelease)
	bodyReleased = true
	result := <-bodyResult
	require.NoError(t, result.err)
	require.Equal(t, "audio", string(result.body))
}
