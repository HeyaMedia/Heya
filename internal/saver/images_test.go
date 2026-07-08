package saver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveImageToMediaDirWritesConventionalName(t *testing.T) {
	dir := t.TempDir()
	cached := filepath.Join(dir, "cache.webp")
	if err := os.WriteFile(cached, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}

	mediaDir := filepath.Join(dir, "media")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := SaveImageToMediaDir(mediaDir, cached, "backdrop", 0); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(mediaDir, "fanart.webp")); err != nil || string(got) != "image" {
		t.Fatalf("fanart.webp not written correctly: got=%q err=%v", got, err)
	}
}

func TestSaveImageToMediaDirWritesNumberedVariant(t *testing.T) {
	dir := t.TempDir()
	cached := filepath.Join(dir, "cache.jpg")
	if err := os.WriteFile(cached, []byte("image"), 0o644); err != nil {
		t.Fatal(err)
	}
	mediaDir := filepath.Join(dir, "media")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := SaveImageToMediaDir(mediaDir, cached, "backdrop", 2); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(mediaDir, "fanart2.jpg")); err != nil || string(got) != "image" {
		t.Fatalf("fanart2.jpg not written correctly: got=%q err=%v", got, err)
	}
}

func TestSaveImageToMediaDirDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	cached := filepath.Join(dir, "cache.jpg")
	if err := os.WriteFile(cached, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	mediaDir := filepath.Join(dir, "media")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(mediaDir, "poster.jpg")
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SaveImageToMediaDir(mediaDir, cached, "poster", 0); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(existing); err != nil || string(got) != "old" {
		t.Fatalf("poster.jpg should not be overwritten: got=%q err=%v", got, err)
	}
}
