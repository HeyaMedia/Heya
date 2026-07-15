package server

import (
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCastHLSPathsPreserveScopedAuthAndSessionRouting(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/cast/media/video/input-id/hls/master.m3u8?cast_token=signed&sid=cast-123&audio=1&quality=720p", nil)
	if got := hlsBasePath(r, "public-id"); got != "/api/cast/media/video/public-id/hls" {
		t.Fatalf("base path = %q", got)
	}
	masterQuery, err := url.ParseQuery(queryPassthrough(r)[1:])
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"cast_token": "signed", "sid": "cast-123", "audio": "1", "quality": "720p"} {
		if got := masterQuery.Get(key); got != want {
			t.Fatalf("master %s = %q, want %q", key, got, want)
		}
	}
	childQuery, err := url.ParseQuery(hlsChildQuery(r))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"cast_token": "signed", "sid": "cast-123", "audio": "1"} {
		if got := childQuery.Get(key); got != want {
			t.Fatalf("child %s = %q, want %q", key, got, want)
		}
	}
	if childQuery.Has("quality") {
		t.Fatal("quality should be consumed by the playlist request, not repeated on segments")
	}
}

func TestNativeHLSPathsUseHeaderAuthAndPreserveOnlyTranscodeRouting(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/playback/native/media/input-id/hls/master.m3u8?sid=native-123&audio=2&quality=1080p", nil)
	if got := hlsBasePath(r, "public-id"); got != "/api/playback/native/media/public-id/hls" {
		t.Fatalf("base path = %q", got)
	}
	masterQuery, err := url.ParseQuery(queryPassthrough(r)[1:])
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"sid": "native-123", "audio": "2", "quality": "1080p"} {
		if got := masterQuery.Get(key); got != want {
			t.Fatalf("master %s = %q, want %q", key, got, want)
		}
	}
	childQuery, err := url.ParseQuery(hlsChildQuery(r))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"sid": "native-123", "audio": "2"} {
		if got := childQuery.Get(key); got != want {
			t.Fatalf("child %s = %q, want %q", key, got, want)
		}
	}
	if childQuery.Has("quality") || childQuery.Has("token") || childQuery.Has("cast_token") {
		t.Fatal("native child URLs must rely on the fixed grant header and consumed transcode settings")
	}
}
