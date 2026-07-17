package saver

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/disintegration/imaging"
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

func TestSaveImageToMediaDirSkipsRecompressedBackdropDuplicate(t *testing.T) {
	dir := t.TempDir()
	mediaDir := filepath.Join(dir, "media")
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		t.Fatal(err)
	}

	base := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for y := 0; y < 180; y++ {
		for x := 0; x < 320; x++ {
			base.Set(x, y, color.RGBA{
				R: uint8((x*7 + y*3) % 256),
				G: uint8((x*2 + y*5) % 256),
				B: uint8(40 + (x+y)%180),
				A: 255,
			})
		}
	}
	writeJPEG := func(path string, img image.Image, quality int) {
		t.Helper()
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := jpeg.Encode(file, img, &jpeg.Options{Quality: quality}); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}

	writeJPEG(filepath.Join(mediaDir, "fanart.jpg"), imaging.Resize(base, 160, 90, imaging.Lanczos), 62)
	cached := filepath.Join(dir, "backdrop-large.jpg")
	writeJPEG(cached, base, 94)

	if err := SaveImageToMediaDir(mediaDir, cached, "backdrop", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mediaDir, "fanart2.jpg")); !os.IsNotExist(err) {
		t.Fatalf("duplicate fanart2.jpg should not be written: err=%v", err)
	}
}
