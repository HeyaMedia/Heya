package subsonic

import (
	"crypto/md5" //nolint:gosec // testing the protocol's own token scheme
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIDRoundTrip(t *testing.T) {
	cases := []struct {
		kind Kind
		id   int64
		want string
	}{
		{KindArtist, 5, "ar-5"},
		{KindAlbum, 10, "al-10"},
		{KindTrack, 123456, "tr-123456"},
		{KindFolder, 1, "mf-1"},
		{KindPlaylist, 42, "pl-42"},
	}
	for _, c := range cases {
		enc := EncodeID(c.kind, c.id)
		if enc != c.want {
			t.Fatalf("EncodeID(%d,%d) = %q, want %q", c.kind, c.id, enc, c.want)
		}
		kind, id, err := DecodeID(enc)
		if err != nil || kind != c.kind || id != c.id {
			t.Fatalf("DecodeID(%q) = (%d,%d,%v), want (%d,%d,nil)", enc, kind, id, err, c.kind, c.id)
		}
	}

	// Foreign / malformed ids must be rejected, not misparsed.
	for _, bad := range []string{"", "5", "xx-5", "tr-", "tr-abc", "tr--5", "al-9999999999999999999999"} {
		if _, _, err := DecodeID(bad); err == nil {
			t.Fatalf("DecodeID(%q) succeeded, want error", bad)
		}
	}

	// Kind constraint.
	if _, err := DecodeIDKind("al-3", KindTrack); err == nil {
		t.Fatal("DecodeIDKind accepted a mismatched kind")
	}
	if id, err := DecodeIDKind("tr-3", KindTrack); err != nil || id != 3 {
		t.Fatalf("DecodeIDKind(tr-3) = (%d,%v)", id, err)
	}
}

func TestEndpointName(t *testing.T) {
	cases := []struct {
		path string
		want string
		ok   bool
	}{
		{"/rest/ping", "ping", true},
		{"/rest/ping.view", "ping", true},
		{"/rest/getArtists.view", "getartists", true},
		{"/REST/GetArtists.VIEW", "getartists", true},
		{"/rest/", "", false},
		{"/rest", "", false},
		{"/other/ping", "", false},
		{"/restaurant", "", false},
		{"/rest/a/b", "", false},
	}
	for _, c := range cases {
		got, ok := endpointName(c.path)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("endpointName(%q) = (%q,%v), want (%q,%v)", c.path, got, ok, c.want, c.ok)
		}
	}
}

func TestClaimsPath(t *testing.T) {
	for _, path := range []string{"/rest/ping", "/rest/ping.view", "/REST/GetArtists.VIEW", "/rest/futureEndpoint"} {
		if !ClaimsPath(path) {
			t.Errorf("ClaimsPath(%q) = false, want true", path)
		}
	}
	for _, path := range []string{"/", "/rest", "/restaurant", "/subsonic/rest/ping.view", "/music"} {
		if ClaimsPath(path) {
			t.Errorf("ClaimsPath(%q) = true, want false", path)
		}
	}
}

// --- envelope serialization ---

func TestEnvelopeXML(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/rest/getLicense", nil)
	respond(w, r, "license", &License{Valid: true})

	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/xml") {
		t.Fatalf("content type = %q, want text/xml", ct)
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, xml.Header) {
		t.Fatalf("missing xml header: %q", body[:40])
	}

	var env struct {
		XMLName       xml.Name `xml:"subsonic-response"`
		Status        string   `xml:"status,attr"`
		Version       string   `xml:"version,attr"`
		Type          string   `xml:"type,attr"`
		ServerVersion string   `xml:"serverVersion,attr"`
		OpenSubsonic  bool     `xml:"openSubsonic,attr"`
		License       *struct {
			Valid bool `xml:"valid,attr"`
		} `xml:"license"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, body)
	}
	if env.Status != "ok" || env.Version != "1.16.1" || env.Type != "heya" || !env.OpenSubsonic {
		t.Fatalf("envelope attrs wrong: %+v", env)
	}
	if env.License == nil || !env.License.Valid {
		t.Fatalf("license payload missing: %s", body)
	}
	if !strings.Contains(body, `xmlns="http://subsonic.org/restapi"`) {
		t.Fatalf("missing xmlns: %s", body)
	}
}

func TestEnvelopeJSON(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/rest/getLicense?f=json", nil)
	respond(w, r, "license", &License{Valid: true})

	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content type = %q, want application/json", ct)
	}
	var doc map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, w.Body.String())
	}
	env, ok := doc["subsonic-response"]
	if !ok {
		t.Fatalf("missing subsonic-response wrapper: %s", w.Body.String())
	}
	if env["status"] != "ok" || env["version"] != "1.16.1" || env["openSubsonic"] != true || env["type"] != "heya" {
		t.Fatalf("envelope fields wrong: %v", env)
	}
	lic, ok := env["license"].(map[string]any)
	if !ok || lic["valid"] != true {
		t.Fatalf("license payload wrong: %v", env["license"])
	}
}

func TestEnvelopeError(t *testing.T) {
	// XML
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/rest/ping", nil)
	respondError(w, r, errWrongCredentials, "wrong username or password")
	if w.Code != http.StatusOK {
		t.Fatalf("subsonic errors must ride HTTP 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `status="failed"`) || !strings.Contains(body, `<error code="40"`) {
		t.Fatalf("xml error shape wrong: %s", body)
	}

	// JSON
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/rest/ping?f=json", nil)
	respondError(w, r, errNotFound, "nope")
	var doc map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	env := doc["subsonic-response"]
	if env["status"] != "failed" {
		t.Fatalf("status = %v, want failed", env["status"])
	}
	errObj, ok := env["error"].(map[string]any)
	if !ok || errObj["code"] != float64(70) || errObj["message"] != "nope" {
		t.Fatalf("json error shape wrong: %v", env["error"])
	}
}

func TestEnvelopeJSONP(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/rest/ping?f=jsonp&callback=cb1", nil)
	respond(w, r, "", nil)
	body := w.Body.String()
	if !strings.HasPrefix(body, "cb1(") || !strings.HasSuffix(body, ");") {
		t.Fatalf("jsonp wrap wrong: %s", body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Fatalf("jsonp content type = %q", ct)
	}

	// A script-injection callback must not be reflected.
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/rest/ping?f=jsonp&callback=alert(1)//", nil)
	respond(w, r, "", nil)
	if strings.Contains(w.Body.String(), "alert(1)") {
		t.Fatalf("unsafe callback reflected: %s", w.Body.String())
	}
}

// Multi-payload structs must marshal their construction-time XMLName.
func TestDynamicXMLName(t *testing.T) {
	buf, err := xml.Marshal(&SongList{XMLName: xml.Name{Local: "songsByGenre"}, Songs: []Child{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf), "<songsByGenre") {
		t.Fatalf("dynamic XMLName not honored: %s", buf)
	}

	// Regression: encoding/xml prefers an XMLName TAG over the field value,
	// so shared payload structs must keep XMLName untagged — getIndexes
	// once answered <artists> because of this.
	buf, err = xml.Marshal(&ArtistsID3{XMLName: xml.Name{Local: "indexes"}, Index: []IndexID3{}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf), "<indexes") {
		t.Fatalf("ArtistsID3 XMLName not swappable: %s", buf)
	}
}

// --- auth ---

func authedRequest(query string) *http.Request {
	return httptest.NewRequest(http.MethodGet, "/rest/ping?v=1.16.1&c=test&"+query, nil)
}

func TestAuthPlaintext(t *testing.T) {
	s := newTestServer(t)
	u, aerr := s.authenticate(authedRequest("u=admin&p=sekret-app-pw"))
	if aerr != nil {
		t.Fatalf("plaintext auth failed: %+v", aerr)
	}
	if u.Username != "admin" {
		t.Fatalf("wrong user: %+v", u)
	}
}

func TestAuthEncHex(t *testing.T) {
	s := newTestServer(t)
	enc := "enc:" + hex.EncodeToString([]byte("sekret-app-pw"))
	if _, aerr := s.authenticate(authedRequest("u=admin&p=" + enc)); aerr != nil {
		t.Fatalf("enc: auth failed: %+v", aerr)
	}
}

func TestAuthToken(t *testing.T) {
	s := newTestServer(t)
	salt := "abc123"
	sum := md5.Sum([]byte("sekret-app-pw" + salt)) //nolint:gosec
	token := hex.EncodeToString(sum[:])
	if _, aerr := s.authenticate(authedRequest("u=admin&t=" + token + "&s=" + salt)); aerr != nil {
		t.Fatalf("token auth failed: %+v", aerr)
	}

	// Wrong token → 40.
	_, aerr := s.authenticate(authedRequest("u=admin&t=deadbeef&s=" + salt))
	if aerr == nil || aerr.code != errWrongCredentials {
		t.Fatalf("wrong token = %+v, want code 40", aerr)
	}
}

func TestAuthFailures(t *testing.T) {
	s := newTestServer(t)

	// Wrong password → 40.
	if _, aerr := s.authenticate(authedRequest("u=admin&p=nope")); aerr == nil || aerr.code != errWrongCredentials {
		t.Fatalf("wrong password should be 40, got %+v", aerr)
	}
	// Unknown user → 40 (indistinguishable from wrong password).
	if _, aerr := s.authenticate(authedRequest("u=ghost&p=x")); aerr == nil || aerr.code != errWrongCredentials {
		t.Fatalf("unknown user should be 40, got %+v", aerr)
	}
	// Missing username → 10.
	if _, aerr := s.authenticate(authedRequest("p=x")); aerr == nil || aerr.code != errMissingParameter {
		t.Fatalf("missing u should be 10, got %+v", aerr)
	}
	// Token without salt → 10.
	if _, aerr := s.authenticate(authedRequest("u=admin&t=deadbeef")); aerr == nil || aerr.code != errMissingParameter {
		t.Fatalf("t without s should be 10, got %+v", aerr)
	}
}

func TestAuthAPIKey(t *testing.T) {
	s := newTestServer(t)

	if u, aerr := s.authenticate(authedRequest("apiKey=sekret-app-pw")); aerr != nil || u.Username != "admin" {
		t.Fatalf("apiKey auth failed: %+v %+v", u, aerr)
	}
	// apiKey + u → 43 (conflicting mechanisms).
	if _, aerr := s.authenticate(authedRequest("apiKey=sekret-app-pw&u=admin")); aerr == nil || aerr.code != errAuthConflict {
		t.Fatalf("apiKey+u should be 43, got %+v", aerr)
	}
	// Unknown apiKey → 44.
	if _, aerr := s.authenticate(authedRequest("apiKey=wrong")); aerr == nil || aerr.code != errInvalidAPIKey {
		t.Fatalf("bad apiKey should be 44, got %+v", aerr)
	}
}

// Auth failures must still be HTTP 200 with an enveloped error, and
// getOpenSubsonicExtensions must work with no credentials at all.
func TestServeHTTPAuthGate(t *testing.T) {
	s := newTestServer(t)

	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rest/ping?f=json", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"code":10`) {
		t.Fatalf("unauthenticated ping: code=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rest/getOpenSubsonicExtensions?f=json", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "apiKeyAuthentication") {
		t.Fatalf("getOpenSubsonicExtensions must not require auth: %s", w.Body.String())
	}

	// Unknown endpoint → in-protocol error 0, never HTML.
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rest/jukeboxControl?f=json", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"code":0`) {
		t.Fatalf("unknown endpoint answer wrong: code=%d body=%s", w.Code, w.Body.String())
	}

	// Disabled server falls through to next (404 here).
	s.app.(*fakeBackend).enabled = false
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rest/ping", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("disabled surface must fall through, got %d", w.Code)
	}
	s.app.(*fakeBackend).enabled = true
}
