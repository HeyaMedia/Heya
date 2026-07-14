package imageserve

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceExtUsesImageBytesForOpaqueCanonicalFilename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opaque-canonical-id.jpg")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	transparent := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	transparent.Set(0, 0, color.NRGBA{R: 255, A: 80})
	if err := png.Encode(file, transparent); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if got := sourceExt(path); got != "png" {
		t.Fatalf("sourceExt() = %q, want png", got)
	}
}
