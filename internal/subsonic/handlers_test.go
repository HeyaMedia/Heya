package subsonic

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/sessions"
)

const testAuth = "u=admin&p=sekret-app-pw&v=1.16.1&c=go-test"

func doJSON(t *testing.T, s *Server, endpoint, extra string) map[string]any {
	t.Helper()
	url := "/rest/" + endpoint + "?" + testAuth + "&f=json"
	if extra != "" {
		url += "&" + extra
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("%s: http %d", endpoint, w.Code)
	}
	var doc map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("%s: unmarshal: %v\n%s", endpoint, err, w.Body.String())
	}
	env, ok := doc["subsonic-response"]
	if !ok {
		t.Fatalf("%s: no envelope: %s", endpoint, w.Body.String())
	}
	if env["status"] != "ok" {
		t.Fatalf("%s: status=%v error=%v", endpoint, env["status"], env["error"])
	}
	return env
}

func TestGetArtistsJSON(t *testing.T) {
	s := newTestServer(t)
	env := doJSON(t, s, "getArtists", "")
	artists, ok := env["artists"].(map[string]any)
	if !ok {
		t.Fatalf("no artists payload: %v", env)
	}
	if artists["ignoredArticles"] != ignoredArticles {
		t.Fatalf("ignoredArticles = %v", artists["ignoredArticles"])
	}
	index, ok := artists["index"].([]any)
	if !ok || len(index) != 2 { // "A" (Aphex Twin) and "P" (Prodigy, The)
		t.Fatalf("index buckets = %v", artists["index"])
	}
	first := index[0].(map[string]any)
	if first["name"] != "A" {
		t.Fatalf("first bucket = %v, want A", first["name"])
	}
	entries := first["artist"].([]any)
	entry := entries[0].(map[string]any)
	if entry["id"] != "ar-6" || entry["name"] != "Aphex Twin" {
		t.Fatalf("artist entry wrong: %v", entry)
	}
}

func TestGetArtistsXML(t *testing.T) {
	s := newTestServer(t)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rest/getArtists.view?"+testAuth, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("http %d", w.Code)
	}
	var env struct {
		Status  string `xml:"status,attr"`
		Artists struct {
			IgnoredArticles string `xml:"ignoredArticles,attr"`
			Index           []struct {
				Name    string `xml:"name,attr"`
				Artists []struct {
					ID   string `xml:"id,attr"`
					Name string `xml:"name,attr"`
				} `xml:"artist"`
			} `xml:"index"`
		} `xml:"artists"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, w.Body.String())
	}
	if env.Status != "ok" || len(env.Artists.Index) != 2 {
		t.Fatalf("xml artists wrong: %+v\n%s", env, w.Body.String())
	}
	if env.Artists.Index[1].Name != "P" || env.Artists.Index[1].Artists[0].ID != "ar-5" {
		t.Fatalf("P bucket wrong: %+v", env.Artists.Index[1])
	}
}

func TestGetAlbum(t *testing.T) {
	s := newTestServer(t)
	env := doJSON(t, s, "getAlbum", "id=al-10")
	album := env["album"].(map[string]any)
	if album["id"] != "al-10" || album["name"] != "The Fat of the Land" || album["artistId"] != "ar-5" {
		t.Fatalf("album header wrong: %v", album)
	}
	if album["year"] != float64(1997) || album["coverArt"] != "al-10" {
		t.Fatalf("album meta wrong: %v", album)
	}
	songs := album["song"].([]any)
	if len(songs) != 2 {
		t.Fatalf("song count = %d", len(songs))
	}
	song := songs[0].(map[string]any)
	if song["id"] != "tr-100" || song["suffix"] != "flac" || song["contentType"] != "audio/flac" {
		t.Fatalf("song file facts wrong: %v", song)
	}
	if song["albumId"] != "al-10" || song["artistId"] != "ar-5" || song["isDir"] != false {
		t.Fatalf("song linkage wrong: %v", song)
	}
	if song["duration"] != float64(342) || song["track"] != float64(1) {
		t.Fatalf("song numbers wrong: %v", song)
	}
}

func TestGetArtistAndDirectory(t *testing.T) {
	s := newTestServer(t)
	env := doJSON(t, s, "getArtist", "id=ar-5")
	artist := env["artist"].(map[string]any)
	if artist["name"] != "The Prodigy" || artist["albumCount"] != float64(1) {
		t.Fatalf("artist wrong: %v", artist)
	}
	albums := artist["album"].([]any)
	if len(albums) != 1 || albums[0].(map[string]any)["id"] != "al-10" {
		t.Fatalf("artist albums wrong: %v", albums)
	}

	// Directory browse of the same artist serves album rows as children.
	env = doJSON(t, s, "getMusicDirectory", "id=ar-5")
	dir := env["directory"].(map[string]any)
	children := dir["child"].([]any)
	if len(children) != 1 || children[0].(map[string]any)["isDir"] != true {
		t.Fatalf("directory children wrong: %v", dir)
	}

	// Unknown artist → error 70.
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rest/getArtist?"+testAuth+"&f=json&id=ar-999", nil))
	if !strings.Contains(w.Body.String(), `"code":70`) {
		t.Fatalf("missing artist should be 70: %s", w.Body.String())
	}
}

func TestSearch3(t *testing.T) {
	s := newTestServer(t)
	env := doJSON(t, s, "search3", "query=breathe")
	result := env["searchResult3"].(map[string]any)
	songs, _ := result["song"].([]any)
	if len(songs) != 1 || songs[0].(map[string]any)["title"] != "Breathe" {
		t.Fatalf("search3 songs wrong: %v", result)
	}

	// The offline-sync "everything" spellings must not filter.
	env = doJSON(t, s, "search3", `query=%22%22&songCount=10`)
	result = env["searchResult3"].(map[string]any)
	if songs, _ := result["song"].([]any); len(songs) != 2 {
		t.Fatalf(`query="" should return everything: %v`, result)
	}
}

func TestStarAndRating(t *testing.T) {
	s := newTestServer(t)
	fake := s.app.(*fakeBackend)

	doJSON(t, s, "star", "id=tr-100")
	if !fake.lovedTracks[100] {
		t.Fatal("star did not set loved state")
	}
	doJSON(t, s, "unstar", "id=tr-100")
	if fake.lovedTracks[100] {
		t.Fatal("unstar did not clear loved state")
	}

	doJSON(t, s, "setRating", "id=tr-100&rating=4")
	if fake.ratedTracks[100] != 8 { // 4 stars → Heya 8/10
		t.Fatalf("rating mapped to %d, want 8", fake.ratedTracks[100])
	}
	doJSON(t, s, "setRating", "id=al-10&rating=0")
	if r, ok := fake.ratedAlbums[10]; !ok || r != 0 {
		t.Fatalf("album rating clear = (%d,%v)", r, ok)
	}
}

func TestScrobbleAndPlayQueue(t *testing.T) {
	s := newTestServer(t)
	fake := s.app.(*fakeBackend)

	doJSON(t, s, "scrobble", "id=tr-100")
	if len(fake.scrobbles) != 1 || !fake.scrobbles[0].Completed || fake.scrobbles[0].EntityID != 100 {
		t.Fatalf("scrobble wrong: %+v", fake.scrobbles)
	}
	doJSON(t, s, "scrobble", "id=tr-101&submission=false")
	if len(fake.scrobbles) != 1 {
		t.Fatalf("now-playing report must not append a play event: %+v", fake.scrobbles)
	}

	doJSON(t, s, "savePlayQueue", "id=tr-100&id=tr-101&current=tr-101&position=42000")
	env := doJSON(t, s, "getPlayQueue", "")
	q := env["playQueue"].(map[string]any)
	if q["current"] != "tr-101" || q["position"] != float64(42000) {
		t.Fatalf("play queue head wrong: %v", q)
	}
	if entries := q["entry"].([]any); len(entries) != 2 {
		t.Fatalf("play queue entries wrong: %v", q)
	}
}

func TestManifestEndpointsAnswerInProtocol(t *testing.T) {
	// Every implemented/stubbed endpoint must produce a Subsonic envelope
	// (or bytes) — never an HTML fallthrough or a panic. Binary endpoints
	// are exercised for "no 500/panic" only.
	s := newTestServer(t)
	for name, status := range manifest {
		if status == opUnsupported {
			continue
		}
		w := httptest.NewRecorder()
		url := "/rest/" + name + "?" + testAuth + "&f=json"
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code == http.StatusInternalServerError {
			t.Errorf("%s answered 500", name)
		}
		ct := w.Header().Get("Content-Type")
		if w.Code == http.StatusOK && strings.HasPrefix(ct, "text/html") {
			t.Errorf("%s answered HTML — fell through to the SPA?", name)
		}
	}
}

func TestGetNowPlayingOwnOrAdmin(t *testing.T) {
	f := newFakeBackend()
	store := sessions.New(context.Background(), nil)
	store.Upsert(sessions.Session{SessionID: "s-own", UserID: f.user.ID, Username: "admin",
		MediaType: "music", EntityType: "track", EntityID: 100})
	store.Upsert(sessions.Session{SessionID: "s-other", UserID: f.user.ID + 1, Username: "bob",
		MediaType: "music", EntityType: "track", EntityID: 101})
	f.sessions = store
	s := NewMiddleware(f, http.NotFoundHandler())

	entriesOf := func(env map[string]any) []any {
		np, ok := env["nowPlaying"].(map[string]any)
		if !ok {
			t.Fatalf("no nowPlaying payload: %v", env)
		}
		entries, _ := np["entry"].([]any)
		return entries
	}

	// Admin sees every user's session.
	if got := entriesOf(doJSON(t, s, "getNowPlaying", "")); len(got) != 2 {
		t.Fatalf("admin should see 2 sessions, got %d: %v", len(got), got)
	}

	// A non-admin caller only sees their own — other users' activity must
	// not leak through the Subsonic surface.
	f.user.IsAdmin = false
	got := entriesOf(doJSON(t, s, "getNowPlaying", ""))
	if len(got) != 1 {
		t.Fatalf("non-admin should see only own session, got %d: %v", len(got), got)
	}
	if got[0].(map[string]any)["username"] != "admin" {
		t.Fatalf("non-admin saw someone else's session: %v", got[0])
	}
}

// coverNative fakes the native image pipeline: album covers 404 (no art),
// artist posters serve bytes. Records the methods it was called with.
type coverNative struct {
	methods []string
}

func (n *coverNative) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n.methods = append(n.methods, r.Method+" "+r.URL.Path)
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/media/") && strings.HasSuffix(r.URL.Path, "/image/poster"):
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpegbytes"))
	default:
		http.NotFound(w, r)
	}
}

func TestGetCoverArtFallbackAndPOST(t *testing.T) {
	native := &coverNative{}
	s := NewMiddleware(newFakeBackend(), http.NotFoundHandler())
	s.SetNative(native)

	// Album al-10 has no album art in the native pipeline → must fall back
	// to the artist poster (artist media item 50) and serve bytes.
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rest/getCoverArt?"+testAuth+"&id=al-10", nil))
	if w.Code != http.StatusOK || w.Body.String() != "jpegbytes" {
		t.Fatalf("album fallback: code=%d body=%q", w.Code, w.Body.String())
	}

	// POST form-encoded (py-sonic / formPost extension): the native dispatch
	// must be normalized to GET or the GET-registered image routes 404.
	native.methods = nil
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/rest/getCoverArt",
		strings.NewReader(testAuth+"&id=ar-5"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Body.String() != "jpegbytes" {
		t.Fatalf("POST cover art: code=%d body=%q", w.Code, w.Body.String())
	}
	for _, m := range native.methods {
		if !strings.HasPrefix(m, "GET ") {
			t.Fatalf("native pipeline saw non-GET dispatch: %v", native.methods)
		}
	}

	// Nothing anywhere → Subsonic error envelope (code 70), not a bare 404.
	s2 := NewMiddleware(newFakeBackend(), http.NotFoundHandler())
	s2.SetNative(failingNative{})
	w = httptest.NewRecorder()
	s2.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rest/getCoverArt?"+testAuth+"&id=al-10&f=json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("missing art should answer 200 + envelope, got %d", w.Code)
	}
	var doc map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("envelope parse: %v — %s", err, w.Body.String())
	}
	env := doc["subsonic-response"]
	errObj, _ := env["error"].(map[string]any)
	if env["status"] != "failed" || errObj == nil || errObj["code"] != float64(70) {
		t.Fatalf("want error code 70 envelope, got %s", w.Body.String())
	}
}

type failingNative struct{}

func (failingNative) ServeHTTP(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }

func TestSetRatingRoundTripOnGetSong(t *testing.T) {
	s := newTestServer(t)
	_ = doJSON(t, s, "setRating", "id=tr-100&rating=4")
	env := doJSON(t, s, "getSong", "id=tr-100")
	song := env["song"].(map[string]any)
	if song["userRating"] != float64(4) {
		t.Fatalf("userRating = %v, want 4", song["userRating"])
	}
	_ = doJSON(t, s, "setRating", "id=tr-100&rating=0")
	env = doJSON(t, s, "getSong", "id=tr-100")
	if _, present := env["song"].(map[string]any)["userRating"]; present {
		t.Fatalf("rating 0 should clear userRating: %v", env["song"])
	}
}
