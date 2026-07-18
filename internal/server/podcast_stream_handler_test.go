package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPodcastStreamFollowsPublicRedirectAndPreservesRange(t *testing.T) {
	var startRange, finalRange string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			startRange = r.Header.Get("Range")
			http.Redirect(w, r, "http://cdn.example.test/final", http.StatusFound)
		case "/final":
			finalRange = r.Header.Get("Range")
			w.Header().Set("Content-Type", "audio/mpeg")
			w.Header().Set("Content-Length", "4")
			w.Header().Set("Content-Range", "bytes 10-13/100")
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("ETag", `"episode-v1"`)
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("DATA"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	client := testPublicMediaClient(t, upstream)

	request := httptest.NewRequest(http.MethodGet, "/api/podcasts/episode/stream?url="+url.QueryEscape("http://feed.example.test/start"), nil)
	request.Header.Set("Range", "bytes=10-13")
	response := httptest.NewRecorder()
	handlePodcastStream(client).ServeHTTP(response, request)

	require.Equal(t, http.StatusPartialContent, response.Code)
	require.Equal(t, "DATA", response.Body.String())
	require.Equal(t, "bytes=10-13", startRange)
	require.Equal(t, "bytes=10-13", finalRange)
	require.Equal(t, "bytes 10-13/100", response.Header().Get("Content-Range"))
	require.Equal(t, "bytes", response.Header().Get("Accept-Ranges"))
	require.Equal(t, `"episode-v1"`, response.Header().Get("ETag"))
	require.Equal(t, "audio/mpeg", response.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
}

func TestPodcastStreamRejectsExplicitHTML(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<script>stealToken()</script>`))
	}))
	t.Cleanup(upstream.Close)
	client := testPublicMediaClient(t, upstream)

	request := httptest.NewRequest(http.MethodGet, "/api/podcasts/episode/stream?url="+url.QueryEscape("http://feed.example.test/episode.mp3"), nil)
	response := httptest.NewRecorder()
	handlePodcastStream(client).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadGateway, response.Code)
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.NotContains(t, strings.ToLower(response.Body.String()), "<script")
}

func TestPodcastStreamRejectsUnsafeRedirects(t *testing.T) {
	var privateTargetReached atomic.Bool
	privateTarget := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		privateTargetReached.Store(true)
	}))
	t.Cleanup(privateTarget.Close)

	tests := []struct {
		name     string
		location string
	}{
		{name: "private address", location: privateTarget.URL + "/audio.mp3"},
		{name: "non HTTP scheme", location: "file:///etc/passwd"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, test.location, http.StatusFound)
			}))
			t.Cleanup(redirector.Close)
			client := testPublicMediaClient(t, redirector)

			request := httptest.NewRequest(http.MethodGet, "/api/podcasts/episode/stream?url="+url.QueryEscape("http://feed.example.test/start"), nil)
			response := httptest.NewRecorder()
			handlePodcastStream(client).ServeHTTP(response, request)
			require.Equal(t, http.StatusBadGateway, response.Code)
		})
	}
	require.False(t, privateTargetReached.Load(), "redirect reached a private listener")
}
