package publichttp

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/safedial"
	"github.com/stretchr/testify/require"
)

func testFetcher(t *testing.T, handler http.Handler) (*Fetcher, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	dialer := &net.Dialer{}
	client := safedial.NewPublicHTTPClientWithDialContext(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, serverURL.Host)
	})
	t.Cleanup(client.CloseIdleConnections)
	return NewFetcherWithClient(client, time.Second), "http://public.example.test"
}

func TestGetRejectsNonPublicURLBeforeClient(t *testing.T) {
	var reached atomic.Bool
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		reached.Store(true)
		return nil, errors.New("unexpected request")
	})}
	fetcher := NewFetcherWithClient(client, time.Second)

	_, err := fetcher.Get(t.Context(), "http://169.254.169.254/latest/meta-data", 1024, nil)
	require.Error(t, err)
	require.False(t, reached.Load())
}

func TestGetRejectsRedirectToNonPublicURL(t *testing.T) {
	var requests atomic.Int32
	fetcher, baseURL := testFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
	}))

	_, err := fetcher.Get(t.Context(), baseURL+"/start", 1024, nil)
	require.Error(t, err)
	require.Equal(t, int32(1), requests.Load())
}

func TestGetRejectsBodiesOverLimit(t *testing.T) {
	fetcher, baseURL := testFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "5")
		_, _ = io.WriteString(w, "12345")
	}))

	_, err := fetcher.Get(t.Context(), baseURL+"/large", 4, nil)
	require.ErrorIs(t, err, ErrBodyTooLarge)
}

func TestGetRejectsChunkedBodiesOverLimit(t *testing.T) {
	fetcher, baseURL := testFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "12345")
	}))

	_, err := fetcher.Get(t.Context(), baseURL+"/large", 4, nil)
	require.ErrorIs(t, err, ErrBodyTooLarge)
}

func TestFetchImageValidatesTypeAndReturnsBoundedBytes(t *testing.T) {
	var encoded bytes.Buffer
	pixel := image.NewRGBA(image.Rect(0, 0, 1, 1))
	pixel.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	require.NoError(t, png.Encode(&encoded, pixel))
	fetcher, baseURL := testFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "image/*", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(encoded.Bytes())
	}))

	image, err := fetcher.FetchImage(t.Context(), baseURL+"/cover", 1024)
	require.NoError(t, err)
	require.Equal(t, "image/png", image.ContentType)
	require.Equal(t, encoded.Bytes(), image.Body)
}

func TestFetchImageRejectsSpoofedRasterSVGAndStatus(t *testing.T) {
	fetcher, baseURL := testFetcher(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/missing") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		if strings.HasSuffix(r.URL.Path, "/svg") {
			_, _ = io.WriteString(w, `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
			return
		}
		_, _ = io.WriteString(w, "not actually a png")
	}))

	_, err := fetcher.FetchImage(t.Context(), baseURL+"/spoofed", 1024)
	require.ErrorIs(t, err, ErrNotImage)
	_, err = fetcher.FetchImage(t.Context(), baseURL+"/svg", 1024)
	require.ErrorIs(t, err, ErrNotImage)

	_, err = fetcher.FetchImage(t.Context(), baseURL+"/missing", 1024)
	var statusErr *StatusError
	require.ErrorAs(t, err, &statusErr)
	require.Equal(t, http.StatusNotFound, statusErr.Code)
}

func TestFetchImageHonorsCancellationWaitingForSlot(t *testing.T) {
	for range maxImageFetches {
		imageFetchSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for range maxImageFetches {
			<-imageFetchSlots
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	fetcher := NewFetcher(time.Second)
	_, err := fetcher.FetchImage(ctx, "https://media.example.test/cover.png", 1024)
	require.ErrorIs(t, err, context.Canceled)
}

func TestServeImageSetsSafeHeadersAndHonorsHEAD(t *testing.T) {
	request := httptest.NewRequest(http.MethodHead, "/cover", nil)
	response := httptest.NewRecorder()
	ServeImage(response, request, &Image{ContentType: "image/png", Body: []byte("image")}, "public, max-age=60")

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "image/png", response.Header().Get("Content-Type"))
	require.Equal(t, "5", response.Header().Get("Content-Length"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "public, max-age=60", response.Header().Get("Cache-Control"))
	require.Empty(t, response.Body.Bytes())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
