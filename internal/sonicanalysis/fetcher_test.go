package sonicanalysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/karbowiak/heya/internal/artifactdownload"
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
		fetcher.client = server.Client()
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
	fetcher.client = server.Client()

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

func TestModelFetcherRejectsExplicitlyOversizedArtifact(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", "64")
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	target := t.TempDir()
	manifest := []ModelFile{{
		Name: "model.onnx", URL: server.URL, Size: 32, MaxBytes: 16,
	}}
	fetcher := NewModelFetcherWithManifest(target, "", manifest)
	fetcher.client = server.Client()

	err := fetcher.Run(context.Background())
	if !errors.Is(err, artifactdownload.ErrTooLarge) {
		t.Fatalf("Run() error = %v, want ErrTooLarge", err)
	}
	destination := filepath.Join(target, manifest[0].Name)
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("oversized artifact was published: %v", statErr)
	}
	leftovers, globErr := filepath.Glob(destination + ".tmp-*")
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary downloads leaked: %v", leftovers)
	}
}

func TestModelDownloadLimitLeavesRoomForApproximateSizes(t *testing.T) {
	t.Parallel()

	if got, want := modelDownloadLimit(ModelFile{Size: 1 << 20}), int64(9<<20); got != want {
		t.Fatalf("small model limit = %d, want %d", got, want)
	}
	if got, want := modelDownloadLimit(ModelFile{Size: 100 << 20}), int64(150<<20); got != want {
		t.Fatalf("mid-size model limit = %d, want %d", got, want)
	}
	if got, want := modelDownloadLimit(ModelFile{Size: 1 << 30}), int64((1<<30)+(256<<20)); got != want {
		t.Fatalf("large model limit = %d, want %d", got, want)
	}
	if got := modelDownloadLimit(ModelFile{Size: 100, MaxBytes: 1234}); got != 1234 {
		t.Fatalf("explicit model limit = %d, want 1234", got)
	}
}
