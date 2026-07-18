package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/eventhub"
	"github.com/stretchr/testify/require"
)

func TestRadioICYMetadataIsScopedToStreamOwner(t *testing.T) {
	metadata := []byte("StreamTitle='Boards of Canada - Dayvan Cowboy';")
	blocks := (len(metadata) + 15) / 16
	padded := append(append([]byte(nil), metadata...), bytes.Repeat([]byte{0}, blocks*16-len(metadata))...)
	stream := append([]byte{'A', byte(blocks)}, padded...)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("icy-metaint", "1")
		_, _ = w.Write(stream)
	}))
	t.Cleanup(upstream.Close)
	client := testPublicMediaClient(t, upstream)

	hub := eventhub.New()
	owner := hub.SubscribePrincipal(eventhub.SubscriberPrincipal{UserID: 7})
	other := hub.SubscribePrincipal(eventhub.SubscriberPrincipal{UserID: 8, IsAdmin: true})
	internal := hub.Subscribe()

	streamURL := "http://radio.example.test/live"
	request := httptest.NewRequest(http.MethodGet, "/api/radio/stream?url="+url.QueryEscape(streamURL), nil)
	response := httptest.NewRecorder()
	handleRadioStream(hub, 7, client).ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "A", response.Body.String(), "ICY metadata bytes must be stripped from the audio")
	require.Equal(t, "audio/mpeg", response.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))

	select {
	case event := <-owner:
		require.Equal(t, eventhub.EventRadioICY, event.Type)
		payload, ok := event.Payload.(eventhub.RadioICYPayload)
		require.True(t, ok)
		require.Equal(t, "Boards of Canada", payload.Artist)
		require.Equal(t, "Dayvan Cowboy", payload.Title)
		require.Equal(t, streamURL, payload.StreamURL)
	default:
		t.Fatal("stream owner did not receive ICY metadata")
	}

	for name, ch := range map[string]<-chan eventhub.Event{"other admin": other, "internal": internal} {
		select {
		case event := <-ch:
			t.Fatalf("%s received private %q event", name, event.Type)
		default:
		}
	}
}

func TestRadioStreamRejectsExplicitHTML(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<script>stealToken()</script>`))
	}))
	t.Cleanup(upstream.Close)
	client := testPublicMediaClient(t, upstream)

	request := httptest.NewRequest(http.MethodGet, "/api/radio/stream?url="+url.QueryEscape("http://radio.example.test/live"), nil)
	response := httptest.NewRecorder()
	handleRadioStream(nil, 1, client).ServeHTTP(response, request)

	require.Equal(t, http.StatusBadGateway, response.Code)
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.NotContains(t, strings.ToLower(response.Body.String()), "<script")
}
