package scrobble

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func okResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{}`)),
	}
}

func TestLastFMUpdateNowPlayingIsTransient(t *testing.T) {
	var got *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		got = req.Clone(req.Context())
		return okResponse(), nil
	})}
	lastfm := &LastFM{APIKey: "key", Secret: "secret", SessionKey: "session", HTTP: client}
	listen := Listen{
		ArtistName: "Artist", TrackName: "Track", ReleaseName: "Album",
		RecordingMBID: "recording-mbid", DurationSec: 123,
		ListenedAt: time.Unix(1_700_000_000, 0),
	}

	if err := lastfm.UpdateNowPlaying(context.Background(), listen); err != nil {
		t.Fatalf("UpdateNowPlaying: %v", err)
	}
	if got == nil {
		t.Fatal("no request captured")
	}
	q := got.URL.Query()
	if q.Get("method") != "track.updateNowPlaying" || q.Get("artist") != "Artist" || q.Get("track") != "Track" {
		t.Fatalf("unexpected request query: %v", q)
	}
	if q.Get("album") != "Album" || q.Get("duration") != "123" || q.Get("mbid") != "recording-mbid" {
		t.Fatalf("metadata missing from request query: %v", q)
	}
	if q.Get("api_sig") == "" || q.Get("sk") != "session" {
		t.Fatalf("authenticated request was not signed: %v", q)
	}
	if q.Has("timestamp") {
		t.Fatalf("now-playing must not carry a completion timestamp: %v", q)
	}
}

func TestListenBrainzPlayingNowOmitsListenedAt(t *testing.T) {
	var body map[string]any
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		return okResponse(), nil
	})}
	lb := &ListenBrainz{Token: "token", HTTP: client}
	listen := Listen{ArtistName: "Artist", TrackName: "Track", ListenedAt: time.Unix(1_700_000_000, 0)}

	if err := lb.Submit(context.Background(), "playing_now", []Listen{listen}); err != nil {
		t.Fatalf("Submit playing_now: %v", err)
	}
	if body["listen_type"] != "playing_now" {
		t.Fatalf("listen_type = %v", body["listen_type"])
	}
	payload, ok := body["payload"].([]any)
	if !ok || len(payload) != 1 {
		t.Fatalf("payload = %#v", body["payload"])
	}
	item, ok := payload[0].(map[string]any)
	if !ok {
		t.Fatalf("payload item = %#v", payload[0])
	}
	if _, exists := item["listened_at"]; exists {
		t.Fatalf("playing_now must omit listened_at: %#v", item)
	}
}
