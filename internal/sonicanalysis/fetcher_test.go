package sonicanalysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestModelFetchersCanShareTargetDirectory(t *testing.T) {
	t.Parallel()

	payload := []byte("shared model artifact")
	arrived := make(chan struct{})
	var requests atomic.Int32
	var release sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 2 {
			release.Do(func() { close(arrived) })
		}
		<-arrived
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	sum := sha256.Sum256(payload)
	manifest := []ModelFile{{
		Name:   "bge-m3/model.onnx",
		URL:    server.URL,
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(payload)),
	}}
	target := t.TempDir()
	fetchers := []*ModelFetcher{
		NewModelFetcherWithManifest(target, "", manifest),
		NewModelFetcherWithManifest(target, "", manifest),
	}

	start := make(chan struct{})
	errs := make(chan error, len(fetchers))
	for _, fetcher := range fetchers {
		go func() {
			<-start
			errs <- fetcher.Run(context.Background())
		}()
	}
	close(start)
	for range fetchers {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent fetch failed: %v", err)
		}
	}

	path := filepath.Join(target, manifest[0].Name)
	got, err := os.ReadFile(path) //nolint:gosec // test-owned temporary path
	if err != nil {
		t.Fatalf("read downloaded model: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("downloaded model = %q, want %q", got, payload)
	}
	leftovers, err := filepath.Glob(path + ".tmp-*")
	if err != nil {
		t.Fatalf("glob temporary downloads: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary downloads were not cleaned up: %v", leftovers)
	}
	for _, fetcher := range fetchers {
		if fetcher.State() != FetcherReady {
			t.Fatalf("fetcher state = %s, want ready", fetcher.State())
		}
	}
}

func TestModelFetcherCanRetryAfterFailure(t *testing.T) {
	t.Parallel()

	payload := []byte("model artifact")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	sum := sha256.Sum256(payload)
	manifest := []ModelFile{{
		Name:   "model.onnx",
		URL:    server.URL,
		SHA256: hex.EncodeToString(sum[:]),
		Size:   int64(len(payload)),
	}}
	fetcher := NewModelFetcherWithManifest(t.TempDir(), "", manifest)

	if err := fetcher.Run(context.Background()); err == nil {
		t.Fatal("first fetch unexpectedly succeeded")
	}
	if fetcher.State() != FetcherFailed {
		t.Fatalf("fetcher state after failure = %s, want failed", fetcher.State())
	}
	if err := fetcher.Run(context.Background()); err != nil {
		t.Fatalf("retry fetch failed: %v", err)
	}
	if fetcher.State() != FetcherReady {
		t.Fatalf("fetcher state after retry = %s, want ready", fetcher.State())
	}
	if fetcher.LastError() != nil {
		t.Fatalf("last error was not cleared after retry: %v", fetcher.LastError())
	}
}
