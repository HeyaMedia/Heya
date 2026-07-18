package heyametadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/httpbodylimit"
)

func TestClientResponseLimitStillAllowsPrivateBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v2/health/ready" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"dependencies":{},"status":"ok"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Ready(context.Background()); err != nil {
		t.Fatalf("private HeyaMetadata readiness failed: %v", err)
	}
}

func TestClientRejectsOversizedDeclaredResponse(t *testing.T) {
	const limit int64 = 64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Content-Length", strconv.FormatInt(limit+1, 10))
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := newClientWithResponseLimit(server.URL, "", limit)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Ready(context.Background())
	if !errors.Is(err, httpbodylimit.ErrResponseBodyTooLarge) {
		t.Fatalf("error = %v, want ErrResponseBodyTooLarge", err)
	}
}

func TestClientRejectsOversizedChunkedResponse(t *testing.T) {
	const limit int64 = 64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = writer.Write([]byte(strings.Repeat("x", int(limit+1))))
	}))
	defer server.Close()

	client, err := newClientWithResponseLimit(server.URL, "", limit)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Ready(context.Background())
	if !errors.Is(err, httpbodylimit.ErrResponseBodyTooLarge) {
		t.Fatalf("error = %v, want ErrResponseBodyTooLarge", err)
	}
}
