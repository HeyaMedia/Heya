package server

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/publichttp"
	"github.com/stretchr/testify/require"
)

func TestTMDBImageProxyUsesValidatedRasterBytes(t *testing.T) {
	var encoded bytes.Buffer
	pixel := image.NewRGBA(image.Rect(0, 0, 1, 1))
	pixel.Set(0, 0, color.RGBA{R: 12, G: 34, B: 56, A: 255})
	require.NoError(t, png.Encode(&encoded, pixel))
	var upstreamPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		// The proxy must derive the type from decoded bytes, not this header.
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(encoded.Bytes())
	}))
	t.Cleanup(upstream.Close)
	fetcher := tmdbTestFetcher(t, upstream)

	request := httptest.NewRequest(http.MethodGet, "/api/tmdb/image/poster.jpg", nil)
	request.SetPathValue("path", "poster.jpg")
	response := httptest.NewRecorder()
	handleTMDBImageProxy(fetcher).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "/t/p/w342/poster.jpg", upstreamPath)
	require.Equal(t, "image/png", response.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "private, max-age=604800, immutable", response.Header().Get("Cache-Control"))
	require.Equal(t, encoded.Bytes(), response.Body.Bytes())
}

func TestTMDBImageProxyRejectsSpoofedImage(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	}))
	t.Cleanup(upstream.Close)
	fetcher := tmdbTestFetcher(t, upstream)

	request := httptest.NewRequest(http.MethodGet, "/api/tmdb/image/poster.png", nil)
	request.SetPathValue("path", "poster.png")
	response := httptest.NewRecorder()
	handleTMDBImageProxy(fetcher).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadGateway, response.Code)
}

func tmdbTestFetcher(t *testing.T, upstream *httptest.Server) *publichttp.Fetcher {
	t.Helper()
	target, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: tmdbRewriteTransport{target: target, base: transport}}
	return publichttp.NewFetcherWithClient(client, time.Second)
}

// tmdbRewriteTransport is a hermetic-test seam: production publichttp uses
// safedial and never rewrites destinations.
type tmdbRewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t tmdbRewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.URL.Scheme = t.target.Scheme
	cloned.URL.Host = t.target.Host
	return t.base.RoundTrip(cloned)
}
