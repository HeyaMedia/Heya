package images

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
)

func TestDownloaderTrustedSourcePollsAcceptedImage(t *testing.T) {
	t.Parallel()
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
		_, _ = w.Write([]byte("image-bytes"))
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
	if string(body) != "image-bytes" {
		t.Fatalf("stored body = %q", body)
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

func TestDownloaderFreshReplacesStableCacheFilename(t *testing.T) {
	t.Parallel()
	var body atomic.Value
	body.Store("first")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	defer server.Close()

	downloader := NewDownloader(t.TempDir(), TrustedSource{BaseURL: server.URL})
	_, err := downloader.Download(context.Background(), server.URL+"/api/v2/images/first", "tv", "show", "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	body.Store("second")
	path, err := downloader.DownloadFresh(context.Background(), server.URL+"/api/v2/images/second", "tv", "show", "poster.jpg")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != "second" {
		t.Fatalf("fresh download stored %q, want second", stored)
	}
}
