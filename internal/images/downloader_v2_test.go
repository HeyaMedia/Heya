package images

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/safedial"
)

func TestDownloaderTrustedSourcePollsAcceptedImage(t *testing.T) {
	t.Parallel()
	imageBody := testJPEG(t, color.RGBA{R: 40, G: 90, B: 180, A: 255})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q", got)
		}
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":1,"state":"working"}`))
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(imageBody)
	}))
	defer server.Close()

	downloader := NewDownloader(t.TempDir(), TrustedSource{BaseURL: server.URL, BearerToken: "secret"})
	path, err := downloader.Download(context.Background(), server.URL+"/api/v2/images/00000000-0000-0000-0000-000000000001", "movie", "test", "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, imageBody) {
		t.Fatal("stored body does not match downloaded JPEG")
	}
}

func TestDownloaderUsesBoundedTrustedImageVariant(t *testing.T) {
	t.Parallel()
	imageBody := testJPEG(t, color.RGBA{R: 40, G: 90, B: 180, A: 255})
	requestedPath := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath <- r.URL.Path
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(imageBody)
	}))
	defer server.Close()

	downloader := NewDownloader(t.TempDir(), TrustedSource{BaseURL: server.URL, ImageVariantWidth: 1920})
	_, err := downloader.Download(t.Context(), server.URL+"/api/v2/images/00000000-0000-0000-0000-000000000001", "music", "album-1", "cover.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if got := <-requestedPath; got != "/api/v2/images/00000000-0000-0000-0000-000000000001/variants/webp/1920" {
		t.Fatalf("requested path = %q", got)
	}
}

func TestDownloaderBoundsOnlyCanonicalTrustedImageURLs(t *testing.T) {
	t.Parallel()
	downloader := NewDownloader(t.TempDir(), TrustedSource{
		BaseURL: "https://metadata.test/root", ImageVariantWidth: 1920,
	})
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "canonical image",
			url:  "https://metadata.test/root/api/v2/images/image-id",
			want: "https://metadata.test/root/api/v2/images/image-id/variants/webp/1920",
		},
		{
			name: "existing variant",
			url:  "https://metadata.test/root/api/v2/images/image-id/variants/webp/640",
			want: "https://metadata.test/root/api/v2/images/image-id/variants/webp/640",
		},
		{
			name: "unrelated trusted route",
			url:  "https://metadata.test/root/api/v2/entities/entity-id/images",
			want: "https://metadata.test/root/api/v2/entities/entity-id/images",
		},
		{
			name: "different base path",
			url:  "https://metadata.test/api/v2/images/image-id",
			want: "https://metadata.test/api/v2/images/image-id",
		},
		{
			name: "untrusted origin",
			url:  "https://other.test/root/api/v2/images/image-id",
			want: "https://other.test/root/api/v2/images/image-id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := downloader.boundedImageURL(tt.url); got != tt.want {
				t.Fatalf("bounded URL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDownloaderDoesNotForwardTrustedAccessAcrossOrigins(t *testing.T) {
	t.Parallel()
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Errorf("trusted Authorization leaked across origin")
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("image"))
	}))
	defer destination.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL+"/image.jpg", http.StatusFound)
	}))
	defer source.Close()

	downloader := NewDownloader(t.TempDir(), TrustedSource{BaseURL: source.URL, BearerToken: "secret"})
	_, err := downloader.Download(context.Background(), source.URL+"/api/v2/images/id", "movie", "test", "poster.jpg")
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Code != http.StatusFound {
		t.Fatalf("cross-origin redirect error = %v", err)
	}
	if redirected.Load() != 0 {
		t.Fatalf("cross-origin redirect was followed %d time(s)", redirected.Load())
	}
}

func TestDownloaderTrustedSourceBoundsRedirectChain(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer server.Close()

	downloader := NewDownloader(t.TempDir(), TrustedSource{BaseURL: server.URL, BearerToken: "secret"})
	_, err := downloader.Download(t.Context(), server.URL+"/loop", "movie", "one", "poster.jpg")
	if err == nil {
		t.Fatal("redirect loop unexpectedly succeeded")
	}
	if got := requests.Load(); got != 10 {
		t.Fatalf("requests = %d, want redirect chain capped at 10 requests", got)
	}
}

func TestDownloaderFreshReplacesStableCacheFilename(t *testing.T) {
	t.Parallel()
	var body atomic.Value
	first := testJPEG(t, color.RGBA{R: 200, A: 255})
	second := testJPEG(t, color.RGBA{B: 200, A: 255})
	body.Store(first)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(body.Load().([]byte))
	}))
	defer server.Close()

	downloader := NewDownloader(t.TempDir(), TrustedSource{BaseURL: server.URL})
	_, err := downloader.Download(context.Background(), server.URL+"/api/v2/images/first", "tv", "show", "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	body.Store(second)
	path, err := downloader.DownloadFresh(context.Background(), server.URL+"/api/v2/images/second", "tv", "show", "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, second) {
		t.Fatal("fresh download did not replace the stable cache filename")
	}
}

func TestDownloaderInvalidReplacementPreservesExistingImage(t *testing.T) {
	t.Parallel()
	valid := testJPEG(t, color.RGBA{G: 180, A: 255})
	var body atomic.Value
	body.Store(valid)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(body.Load().([]byte))
	}))
	defer server.Close()

	downloader := NewDownloader(t.TempDir(), TrustedSource{BaseURL: server.URL})
	path, err := downloader.Download(context.Background(), server.URL+"/valid", "movie", "one", "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	body.Store([]byte("truncated replacement"))
	if _, err := downloader.DownloadFresh(context.Background(), server.URL+"/invalid", "movie", "one", "poster.jpg"); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("DownloadFresh error = %v, want ErrInvalidImage", err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, valid) {
		t.Fatal("invalid replacement changed existing cached image")
	}
}

func TestDownloaderUsesDecodedExtensionForOpaqueURL(t *testing.T) {
	t.Parallel()
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body.Bytes())
	}))
	defer server.Close()

	downloader := NewDownloader(t.TempDir(), TrustedSource{BaseURL: server.URL})
	path, err := downloader.Download(context.Background(), server.URL+"/opaque", "movie", "one", "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != ".png" {
		t.Fatalf("cached path = %q, want decoded .png extension", path)
	}
	again, err := downloader.Download(context.Background(), server.URL+"/opaque", "movie", "one", "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if again != path || requests.Load() != 1 {
		t.Fatalf("cache reuse = %q (%d requests), want %q (1 request)", again, requests.Load(), path)
	}
}

func TestDownloaderBoundsAcceptedPolling(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	downloader := NewDownloader(t.TempDir(), TrustedSource{BaseURL: server.URL})
	_, err := downloader.Download(context.Background(), server.URL+"/pending", "movie", "one", "poster.jpg")
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Code != http.StatusAccepted {
		t.Fatalf("error = %v, want bounded HTTP 202 status error", err)
	}
	if got := requests.Load(); got != maxAcceptedImagePolls {
		t.Fatalf("requests = %d, want %d", got, maxAcceptedImagePolls)
	}
}

func TestDownloaderUntrustedClientFollowsPublicCrossOriginRedirect(t *testing.T) {
	t.Parallel()
	imageBody := testJPEG(t, color.RGBA{R: 80, G: 120, B: 160, A: 255})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "http://cdn.example.test/final", http.StatusFound)
		case "/final":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(imageBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	downloader := untrustedTestDownloader(t, server)
	path, err := downloader.Download(t.Context(), "http://images.example.test/start", "movie", "one", "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want public redirect and final request", requests.Load())
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, imageBody) {
		t.Fatal("stored body does not match redirected image")
	}
}

func TestDownloaderUntrustedClientRejectsPrivateRedirect(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "http://127.0.0.1/private", http.StatusFound)
	}))
	defer server.Close()

	downloader := untrustedTestDownloader(t, server)
	_, err := downloader.Download(t.Context(), "http://images.example.test/start", "movie", "one", "poster.jpg")
	if err == nil {
		t.Fatal("private redirect unexpectedly succeeded")
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, private redirect should fail before another dial", requests.Load())
	}
}

func TestDownloaderUntrustedClientRejectsPrivateDirectURLBeforeDial(t *testing.T) {
	t.Parallel()
	var dialed atomic.Bool
	client := safedial.NewPublicHTTPClientWithDialContext(func(context.Context, string, string) (net.Conn, error) {
		dialed.Store(true)
		return nil, errors.New("unexpected dial")
	})
	t.Cleanup(client.CloseIdleConnections)
	downloader := NewDownloader(t.TempDir())
	downloader.client = client

	_, err := downloader.Download(t.Context(), "http://169.254.169.254/latest/meta-data", "movie", "one", "poster.jpg")
	if err == nil {
		t.Fatal("private direct URL unexpectedly succeeded")
	}
	if dialed.Load() {
		t.Fatal("private direct URL reached the dialer")
	}
}

func untrustedTestDownloader(t *testing.T, server *httptest.Server) *Downloader {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	dialer := &net.Dialer{}
	client := safedial.NewPublicHTTPClientWithDialContext(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, target.Host)
	})
	client.Timeout = time.Second
	t.Cleanup(client.CloseIdleConnections)
	downloader := NewDownloader(t.TempDir())
	downloader.client = client
	return downloader
}

func testJPEG(t *testing.T, fill color.Color) []byte {
	t.Helper()
	var body bytes.Buffer
	imageBody := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			imageBody.Set(x, y, fill)
		}
	}
	if err := jpeg.Encode(&body, imageBody, nil); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}
