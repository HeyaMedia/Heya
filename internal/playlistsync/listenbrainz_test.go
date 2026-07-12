package playlistsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListenBrainzListAndCreate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/user/alice/playlists", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token secret" {
			t.Fatalf("authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"playlist_count": 1,
			"playlists": []any{map[string]any{"playlist": map[string]any{
				"title":      "Road trip",
				"identifier": playlistURI + "abc",
				"annotation": "Loud",
				"extension": map[string]any{jspfPlaylistExtension: map[string]any{
					"public": true, "last_modified_at": "2026-07-12T10:00:00Z",
				}},
			}}},
		})
	})
	mux.HandleFunc("/user/alice/playlists/createdfor", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"playlist_count": 1,
			"playlists": []any{map[string]any{"playlist": map[string]any{
				"title": "Weekly Jams", "identifier": playlistURI + "weekly-id",
			}}},
		})
	})
	mux.HandleFunc("/playlist/create", func(w http.ResponseWriter, r *http.Request) {
		var doc jspfDocument
		if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
			t.Fatal(err)
		}
		if doc.Playlist.Title != "New" || len(doc.Playlist.Tracks) != 1 {
			t.Fatalf("unexpected JSPF: %+v", doc)
		}
		if got := doc.Playlist.Tracks[0].Identifier[0]; got != recordingURI+"recording-id" {
			t.Fatalf("identifier = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "playlist_mbid": "new-id"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &ListenBrainz{Token: "secret", Username: "alice", BaseURL: server.URL, HTTP: server.Client()}
	playlists, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(playlists) != 1 || playlists[0].ExternalID != "abc" || playlists[0].Name != "Road trip" {
		t.Fatalf("playlists = %+v", playlists)
	}
	generated, err := client.ListCollection(context.Background(), "created_for")
	if err != nil {
		t.Fatal(err)
	}
	if len(generated) != 1 || generated[0].ExternalID != "weekly-id" {
		t.Fatalf("generated playlists = %+v", generated)
	}
	id, err := client.Create(context.Background(), Playlist{Name: "New", Tracks: []Track{{ProviderID: "recording-id"}}})
	if err != nil {
		t.Fatal(err)
	}
	if id != "new-id" {
		t.Fatalf("id = %q", id)
	}
}
