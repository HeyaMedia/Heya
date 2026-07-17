package cast

import (
	"bytes"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestScopedMediaURL(t *testing.T) {
	m := New(t.TempDir())
	m.SetMediaOrigin("http://192.168.20.10:8080", "8080")
	dev := Device{Name: "Living room", Addr: "192.168.20.50"}
	track := TrackInfo{
		PullPath:  "/api/cast/media/music/42",
		PullQuery: "supports_flac=1",
		Duration:  180,
	}
	raw, err := m.mediaURLFor(dev, 7, track)
	if err != nil {
		t.Fatalf("media URL: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Scheme + "://" + u.Host; got != "http://192.168.20.10:8080" {
		t.Fatalf("origin = %q", got)
	}
	if u.Path != track.PullPath || u.Query().Get("supports_flac") != "1" {
		t.Fatalf("URL = %s", raw)
	}
	token := u.Query().Get("cast_token")
	if token == "" {
		t.Fatal("media URL has no scoped token")
	}
	userID, err := m.ValidateMediaToken(token, track.PullPath)
	if err != nil || userID != 7 {
		t.Fatalf("validate = user %d, err %v", userID, err)
	}
	if _, err := m.ValidateMediaToken(token, "/api/cast/media/music/43"); err == nil {
		t.Fatal("token was accepted for another media path")
	}
	payloadPart, sigPart, ok := strings.Cut(token, ".")
	if !ok || sigPart == "" {
		t.Fatalf("invalid test token %q", token)
	}
	replacement := byte('A')
	if sigPart[0] == replacement {
		replacement = 'B'
	}
	tampered := payloadPart + "." + string(replacement) + sigPart[1:]
	if _, err := m.ValidateMediaToken(tampered, track.PullPath); err == nil {
		t.Fatal("tampered token was accepted")
	}

	// A 32-byte SHA-256 signature ends with four data bits and two unused
	// Base64 bits. Flip only an unused bit to create a different string that
	// permissive decoding maps to the exact same signature bytes. Tokens must
	// still reject that noncanonical alias.
	const base64URLAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	lastIndex := strings.IndexByte(base64URLAlphabet, sigPart[len(sigPart)-1])
	if lastIndex < 0 || lastIndex%4 != 0 || lastIndex+1 >= len(base64URLAlphabet) {
		t.Fatalf("unexpected canonical signature ending %q", sigPart[len(sigPart)-1])
	}
	noncanonicalSig := sigPart[:len(sigPart)-1] + string(base64URLAlphabet[lastIndex+1])
	canonicalBytes, err := base64.RawURLEncoding.DecodeString(sigPart)
	if err != nil {
		t.Fatal(err)
	}
	noncanonicalBytes, err := base64.RawURLEncoding.DecodeString(noncanonicalSig)
	if err != nil || !bytes.Equal(canonicalBytes, noncanonicalBytes) {
		t.Fatalf("test setup did not preserve signature bytes: %v", err)
	}
	if _, err := m.ValidateMediaToken(payloadPart+"."+noncanonicalSig, track.PullPath); err == nil {
		t.Fatal("noncanonical token encoding was accepted")
	}
}

func TestAutomaticMediaOriginUsesHTTPS(t *testing.T) {
	m := New(t.TempDir())
	m.SetMediaOrigin("", "8080")
	origin, err := m.mediaOriginFor(Device{Name: "local", Addr: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if origin != "https://127.0.0.1:8080" {
		t.Fatalf("automatic origin = %q", origin)
	}
}

func TestScopedMediaURLAllowsOnlyOneHLSSubtree(t *testing.T) {
	m := New(t.TempDir())
	m.SetMediaOrigin("http://192.168.20.10:8080", "8080")
	dev := Device{Name: "Living room", Addr: "192.168.20.50"}
	track := TrackInfo{
		PullPath:      "/api/cast/media/video/file-a/hls/master.m3u8",
		PullScopePath: "/api/cast/media/video/file-a",
		Duration:      7200,
	}
	raw, err := m.mediaURLFor(dev, 7, track)
	if err != nil {
		t.Fatalf("media URL: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	token := u.Query().Get("cast_token")
	for _, path := range []string{
		track.PullPath,
		"/api/cast/media/video/file-a/hls/index.m3u8",
		"/api/cast/media/video/file-a/hls/seg_0001.ts",
		"/api/cast/media/video/file-a/subtitles/7",
	} {
		if userID, err := m.ValidateMediaToken(token, path); err != nil || userID != 7 {
			t.Fatalf("validate %q = user %d, err %v", path, userID, err)
		}
	}
	for _, path := range []string{
		"/api/cast/media/video/file-ab/hls/seg_0001.ts",
		"/api/cast/media/video/file-b/hls/seg_0001.ts",
		"/api/cast/media/music/42",
	} {
		if _, err := m.ValidateMediaToken(token, path); err == nil {
			t.Fatalf("subtree token was accepted for %q", path)
		}
	}
}

func TestMediaDependencyURLReusesOnlyScopedToken(t *testing.T) {
	primary := "http://192.168.20.10:8080/api/cast/media/video/file-a/hls/master.m3u8?audio=2&cast_token=signed-token&quality=1080p&sid=cast-1"
	got, err := mediaDependencyURL(primary, "/api/cast/media/video/file-a/subtitles/7")
	if err != nil {
		t.Fatalf("dependency URL: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/api/cast/media/video/file-a/subtitles/7" || u.Query().Get("cast_token") != "signed-token" {
		t.Fatalf("dependency URL = %s", got)
	}
	if u.Query().Has("audio") || u.Query().Has("quality") || u.Query().Has("sid") {
		t.Fatalf("primary playback query leaked into dependency URL: %s", got)
	}
}

func TestRoutedLocalIPLoopback(t *testing.T) {
	ip, err := routedLocalIP("127.0.0.1")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if !ip.IsLoopback() {
		t.Fatalf("loopback receiver selected %s", ip)
	}
}
