package imageserve

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseQueryAcceptsBoundedBlur(t *testing.T) {
	params := ParseQuery(url.Values{"blur": {"24"}, "w": {"960"}, "q": {"58"}, "f": {"webp"}})
	if params.Blur != 24 || params.Width != 960 || params.Quality != 58 || params.Format != "webp" {
		t.Fatalf("ParseQuery() = %#v, want blur=24 width=960 quality=58 format=webp", params)
	}
	if !params.active() {
		t.Fatal("blurred params should activate the transformer")
	}
	if got := ParseQuery(url.Values{"blur": {"65"}}).Blur; got != 0 {
		t.Fatalf("out-of-range blur = %d, want 0", got)
	}
}

func TestServeGeneratesWebPDerivative(t *testing.T) {
	resizer, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "source.png")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 16), G: uint8(y * 16), B: 120, A: 255})
		}
	}
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/image?w=8&blur=2&f=webp", nil)
	resizer.Serve(response, request, source, Params{Width: 8, Quality: 58, Format: "webp", Blur: 2})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "image/webp" {
		t.Fatalf("Content-Type = %q, want image/webp", got)
	}
	if body := response.Body.Bytes(); len(body) < 12 || string(body[:4]) != "RIFF" || string(body[8:12]) != "WEBP" {
		t.Fatalf("body is not a WebP container: %x", body)
	}
}

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

func TestResizeWaiterHonorsRequestCancellation(t *testing.T) {
	resizer, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for range cap(resizer.sem) {
		resizer.sem <- struct{}{}
	}
	source := filepath.Join(t.TempDir(), "source.png")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/image?w=1", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		resizer.Serve(httptest.NewRecorder(), request, source, Params{Width: 1, Quality: 85})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled resize waiter remained blocked")
	}
}

func TestServeFileForcesRasterTypeAndNosniff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spoofed.jpg")
	if err := os.WriteFile(path, []byte("<html><script>alert(1)</script></html>"), 0o640); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	serveFile(response, httptest.NewRequest(http.MethodGet, "/image", nil), path, "")
	if got := response.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("Content-Type = %q, want image/jpeg", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}
