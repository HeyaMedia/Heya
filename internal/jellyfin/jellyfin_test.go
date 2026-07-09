package jellyfin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIDRoundTrip(t *testing.T) {
	cases := []struct {
		kind Kind
		id   int64
	}{
		{KindItem, 1}, {KindItem, 9223372036854775807}, {KindEpisode, 42},
		{KindTrack, 123456}, {KindUser, 1}, {KindGenre, hashName("Sci-Fi")},
	}
	for _, c := range cases {
		enc := EncodeID(c.kind, c.id)
		if len(enc) != 32 {
			t.Fatalf("EncodeID(%d,%d) = %q, want 32 hex chars", c.kind, c.id, enc)
		}
		kind, id, err := DecodeID(enc)
		if err != nil || kind != c.kind || id != c.id {
			t.Fatalf("DecodeID(%q) = (%d,%d,%v), want (%d,%d,nil)", enc, kind, id, err, c.kind, c.id)
		}
	}

	// Dashed GUID form must decode identically.
	enc := EncodeID(KindItem, 77)
	dashed := enc[0:8] + "-" + enc[8:12] + "-" + enc[12:16] + "-" + enc[16:20] + "-" + enc[20:]
	if _, id, err := DecodeID(dashed); err != nil || id != 77 {
		t.Fatalf("dashed DecodeID = (%d, %v), want (77, nil)", id, err)
	}

	// Foreign GUIDs (real Jellyfin ids) must be rejected, not misparsed.
	if _, _, err := DecodeID("f27caa37e5142225cceded48f6553502"); err == nil {
		t.Fatal("foreign GUID decoded without error")
	}
}

func TestRouterCaseInsensitiveAndEmby(t *testing.T) {
	rt := newRouter()
	var hits []string
	rt.handle(http.MethodGet, "/System/Info/Public", func(w http.ResponseWriter, r *http.Request, p Params) {
		hits = append(hits, "info")
	})
	rt.handle(http.MethodGet, "/Users/{userId}/Items/{itemId}", func(w http.ResponseWriter, r *http.Request, p Params) {
		hits = append(hits, p["userId"]+"/"+p["itemId"])
	})
	rt.handle(http.MethodGet, "/Audio/{itemId}/stream.{container}", func(w http.ResponseWriter, r *http.Request, p Params) {
		hits = append(hits, p["itemId"]+"."+p["container"])
	})

	for _, path := range []string{"/System/Info/Public", "/system/info/public", "/SYSTEM/INFO/PUBLIC"} {
		h, _, ok := rt.match(http.MethodGet, stripEmbyPrefix(path))
		if !ok {
			t.Fatalf("no match for %s", path)
		}
		h(nil, nil, nil)
	}
	for _, path := range []string{"/emby/System/Info/Public", "/EMBY/system/info/public"} {
		if _, _, ok := rt.match(http.MethodGet, stripEmbyPrefix(path)); !ok {
			t.Fatalf("no match for %s", path)
		}
	}

	// Params keep original casing.
	h, p, ok := rt.match(http.MethodGet, "/users/AbC123/items/DeF456")
	if !ok {
		t.Fatal("param route did not match")
	}
	h(nil, nil, p)
	if hits[len(hits)-1] != "AbC123/DeF456" {
		t.Fatalf("params lost casing: %q", hits[len(hits)-1])
	}

	// Dotted segment split.
	h, p, ok = rt.match(http.MethodGet, "/audio/xyz/STREAM.flac")
	if !ok {
		t.Fatal("dotted route did not match")
	}
	h(nil, nil, p)
	if hits[len(hits)-1] != "xyz.flac" {
		t.Fatalf("dotted params wrong: %q", hits[len(hits)-1])
	}

	// HEAD falls back to GET.
	if _, _, ok := rt.match(http.MethodHead, "/system/info/public"); !ok {
		t.Fatal("HEAD did not fall back to GET route")
	}

	// Non-matches must not match.
	if _, _, ok := rt.match(http.MethodGet, "/search"); ok {
		t.Fatal("/search must not match any route")
	}
	if _, _, ok := rt.match(http.MethodPost, "/System/Info/Public"); ok {
		t.Fatal("POST must not match GET-only route")
	}
}

// TestLiteralRoutePrecedence: literal paths must beat param siblings no
// matter the registration order — regression for /Items/Filters2 being
// swallowed by /Items/{itemId}.
func TestLiteralRoutePrecedence(t *testing.T) {
	rt := newRouter()
	var hit string
	rt.handle(http.MethodGet, "/Items/{itemId}", func(http.ResponseWriter, *http.Request, Params) { hit = "param" })
	rt.handle(http.MethodGet, "/Items/Filters2", func(http.ResponseWriter, *http.Request, Params) { hit = "literal" })
	rt.finalize()

	h, _, ok := rt.match(http.MethodGet, "/Items/Filters2")
	if !ok {
		t.Fatal("no match for /Items/Filters2")
	}
	h(nil, nil, nil)
	if hit != "literal" {
		t.Fatalf("literal route shadowed by param route (hit=%q)", hit)
	}

	h, p, ok := rt.match(http.MethodGet, "/Items/abc123")
	if !ok {
		t.Fatal("no match for /Items/abc123")
	}
	h(nil, nil, p)
	if hit != "param" || p["itemId"] != "abc123" {
		t.Fatalf("param route broken after sort (hit=%q, p=%v)", hit, p)
	}
}

func TestClaimsPathPrecision(t *testing.T) {
	// Jellyfin-shaped paths are claimed...
	for _, p := range []string{"/System/Info/Public", "/system/ping", "/Users/AuthenticateByName", "/emby/System/Info/Public", "/socket"} {
		if !ClaimsPath(p) {
			t.Errorf("ClaimsPath(%q) = false, want true", p)
		}
	}
	// ...near-misses are not.
	for _, p := range []string{"/", "/search", "/movies", "/music/artists", "/settings/network", "/login", "/genre/action", "/person/12"} {
		if ClaimsPath(p) {
			t.Errorf("ClaimsPath(%q) = true, want false", p)
		}
	}
}

func TestParseAuthHeader(t *testing.T) {
	d, ok := parseAuthScheme(`MediaBrowser Client="Jellyfin Web", Device="Firefox", DeviceId="abc-123", Version="10.11.0", Token="deadbeef"`)
	if !ok || d.Client != "Jellyfin Web" || d.Device != "Firefox" || d.DeviceID != "abc-123" || d.Version != "10.11.0" || d.Token != "deadbeef" {
		t.Fatalf("parseAuthScheme quoted = %+v ok=%v", d, ok)
	}
	d, ok = parseAuthScheme(`Emby Client=Infuse, Device=AppleTV, DeviceId=xyz, Version=8.0`)
	if !ok || d.Client != "Infuse" || d.DeviceID != "xyz" {
		t.Fatalf("parseAuthScheme unquoted emby = %+v ok=%v", d, ok)
	}
	if _, ok := parseAuthScheme("Bearer sometoken"); ok {
		t.Fatal("Bearer scheme must not parse as MediaBrowser")
	}

	r := httptest.NewRequest(http.MethodGet, "/Items?api_key=qtoken", nil)
	if d := extractAuth(r); d.Token != "qtoken" {
		t.Fatalf("api_key query extraction failed: %+v", d)
	}
	r = httptest.NewRequest(http.MethodGet, "/Items?ApiKey=qtoken2", nil)
	if d := extractAuth(r); d.Token != "qtoken2" {
		t.Fatalf("ApiKey query extraction failed: %+v", d)
	}
	r = httptest.NewRequest(http.MethodGet, "/Items", nil)
	r.Header.Set("X-Emby-Token", "htoken")
	if d := extractAuth(r); d.Token != "htoken" {
		t.Fatalf("X-Emby-Token extraction failed: %+v", d)
	}
	r = httptest.NewRequest(http.MethodGet, "/Items", nil)
	r.Header.Set("X-Emby-Authorization", `MediaBrowser Client="Finamp", DeviceId="d1", Token="xtoken"`)
	if d := extractAuth(r); d.Token != "xtoken" || d.Client != "Finamp" {
		t.Fatalf("X-Emby-Authorization extraction failed: %+v", d)
	}
}
