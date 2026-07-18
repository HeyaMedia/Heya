package artifactdownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFetchPublishesVerifiedArtifact(t *testing.T) {
	t.Parallel()
	payload := []byte("verified artifact")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	digest := sha256.Sum256(payload)
	destination := filepath.Join(t.TempDir(), "models", "artifact.bin")
	var progress int64
	written, err := Fetch(context.Background(), server.Client(), Spec{
		URL:           server.URL,
		Destination:   destination,
		MaxBytes:      int64(len(payload)),
		ExpectedBytes: int64(len(payload)),
		SHA256:        hex.EncodeToString(digest[:]),
		Progress:      func(current int64) { progress = current },
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if written != int64(len(payload)) || progress != written {
		t.Fatalf("written/progress = %d/%d, want %d", written, progress, len(payload))
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("published bytes = %q, want %q", got, payload)
	}
	assertNoDownloadTemps(t, destination)
}

func TestFetchRejectsOversizedContentLengthBeforeStaging(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", "100")
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "artifact.bin")
	_, err := Fetch(context.Background(), server.Client(), Spec{
		URL: server.URL, Destination: destination, MaxBytes: 10,
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Fetch() error = %v, want ErrTooLarge", err)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("destination exists after rejected Content-Length: %v", statErr)
	}
	assertNoDownloadTemps(t, destination)
}

func TestFetchRejectsChunkedBodyPastLimitAndPreservesDestination(t *testing.T) {
	t.Parallel()
	payload := []byte("body larger than the configured cap")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	destination := filepath.Join(t.TempDir(), "artifact.bin")
	if err := os.WriteFile(destination, []byte("existing"), 0o640); err != nil {
		t.Fatal(err)
	}
	_, err := Fetch(context.Background(), server.Client(), Spec{
		URL: server.URL, Destination: destination, MaxBytes: 8,
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Fetch() error = %v, want ErrTooLarge", err)
	}
	got, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "existing" {
		t.Fatalf("destination = %q, want existing bytes", got)
	}
	assertNoDownloadTemps(t, destination)
}

func TestFetchValidationFailurePreservesDestination(t *testing.T) {
	t.Parallel()
	payload := []byte("short")
	validDigest := sha256.Sum256(payload)
	wrongDigest := sha256.Sum256([]byte("different"))

	for _, test := range []struct {
		name     string
		expected int64
		digest   string
		wantErr  error
	}{
		{name: "exact size", expected: int64(len(payload) + 1), digest: hex.EncodeToString(validDigest[:]), wantErr: ErrSizeMismatch},
		{name: "checksum", expected: int64(len(payload)), digest: hex.EncodeToString(wrongDigest[:]), wantErr: ErrHashMismatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				// Flush headers first so ContentLength is unknown and validation
				// necessarily happens against the bounded stream.
				writer.WriteHeader(http.StatusOK)
				if flusher, ok := writer.(http.Flusher); ok {
					flusher.Flush()
				}
				_, _ = writer.Write(payload)
			}))
			defer server.Close()

			destination := filepath.Join(t.TempDir(), "artifact.bin")
			if err := os.WriteFile(destination, []byte("existing"), 0o640); err != nil {
				t.Fatal(err)
			}
			_, err := Fetch(context.Background(), server.Client(), Spec{
				URL: server.URL, Destination: destination, MaxBytes: 32,
				ExpectedBytes: test.expected, SHA256: test.digest,
			})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Fetch() error = %v, want %v", err, test.wantErr)
			}
			got, readErr := os.ReadFile(destination)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != "existing" {
				t.Fatalf("destination = %q, want existing bytes", got)
			}
			assertNoDownloadTemps(t, destination)
		})
	}
}

func TestFetchClientTimeoutCleansTemporaryFile(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		time.Sleep(250 * time.Millisecond)
		_, _ = writer.Write([]byte("late"))
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 25 * time.Millisecond

	destination := filepath.Join(t.TempDir(), "artifact.bin")
	_, err := Fetch(context.Background(), client, Spec{
		URL: server.URL, Destination: destination, MaxBytes: 32,
	})
	<-started
	if err == nil {
		t.Fatal("Fetch() unexpectedly ignored client timeout")
	}
	assertNoDownloadTemps(t, destination)
}

func TestNewClientRejectsPrivateDestination(t *testing.T) {
	t.Parallel()
	client := NewClient(time.Second)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1/artifact", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Do(request)
	if err == nil {
		t.Fatal("public artifact client accepted loopback destination")
	}
}

func assertNoDownloadTemps(t *testing.T, destination string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(destination), filepath.Base(destination)+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary downloads leaked: %v", matches)
	}
}
