package studios

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadFilePublishesOnlyDecodedRaster(t *testing.T) {
	dir := t.TempDir()
	var raster bytes.Buffer
	if err := png.Encode(&raster, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(raster.Bytes())
	}))
	defer server.Close()

	destination := filepath.Join(dir, "studio.png")
	if !downloadFile(context.Background(), server.URL, destination) {
		t.Fatal("valid PNG was not downloaded")
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	}

	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<svg><script>alert(1)</script></svg>"))
	}))
	defer invalid.Close()
	if downloadFile(context.Background(), invalid.URL, destination) {
		t.Fatal("active non-raster response was accepted")
	}
	stored, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, raster.Bytes()) {
		t.Fatal("invalid replacement changed existing studio logo")
	}
}

func TestResolverIgnoresLegacySVG(t *testing.T) {
	dataDir := t.TempDir()
	resolver := NewResolver(dataDir)
	if err := os.WriteFile(filepath.Join(dataDir, "studios", "example.svg"), []byte("<svg/>"), 0o640); err != nil {
		t.Fatal(err)
	}
	if path := resolver.LogoPath("Example"); path != "" {
		t.Fatalf("LogoPath returned legacy SVG %q", path)
	}
}
