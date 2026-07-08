package trickplay

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestJellyfinSidecarPaths(t *testing.T) {
	filePath := filepath.Join("media", "Show", "Season 01", "Episode.S01E01.mkv")

	sidecar := SidecarDir(filePath)
	if want := filepath.Join("media", "Show", "Season 01", "Episode.S01E01.mkv.trickplay"); sidecar != want {
		t.Fatalf("SidecarDir() = %q, want %q", sidecar, want)
	}

	grid := GridDir(sidecar)
	if want := filepath.Join(sidecar, "320 - 10x10"); grid != want {
		t.Fatalf("GridDir() = %q, want %q", grid, want)
	}
}

func TestJellyfinSpriteNames(t *testing.T) {
	for idx, want := range []string{"0.jpg", "1.jpg", "2.jpg"} {
		if got := SpriteName(idx); got != want {
			t.Fatalf("SpriteName(%d) = %q, want %q", idx, got, want)
		}
	}
}

func TestBuildVTTSynthesizesFromJellyfinSprites(t *testing.T) {
	vtt, err := BuildVTT(12, func(spriteIdx int) bool {
		return spriteIdx == 0
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"WEBVTT",
		"00:00:00.000 --> 00:00:05.000",
		"0.jpg#xywh=0,0,320,180",
		"00:00:10.000 --> 00:00:12.000",
		"0.jpg#xywh=640,0,320,180",
	} {
		if !strings.Contains(vtt, want) {
			t.Fatalf("VTT missing %q:\n%s", want, vtt)
		}
	}
}

func TestBuildVTTRequiresExpectedSprites(t *testing.T) {
	_, err := BuildVTT(501, func(spriteIdx int) bool {
		return spriteIdx == 0
	})
	if err == nil {
		t.Fatal("expected missing second sprite to fail")
	}
}
