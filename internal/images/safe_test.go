package images

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStageRasterUsesDecodedFormatAndPublishes(t *testing.T) {
	dir := t.TempDir()
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	staged, err := StageRaster(dir, &body)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = staged.Rollback() }()
	if staged.Info.Format != "png" || staged.Info.Extension != ".png" || staged.Info.Width != 3 || staged.Info.Height != 2 {
		t.Fatalf("Info = %+v", staged.Info)
	}
	destination := filepath.Join(dir, "cover.png")
	if err := staged.Publish(destination); err != nil {
		t.Fatal(err)
	}
	if err := staged.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	}
}

func TestStageRasterRejectsFilenameShapedGarbage(t *testing.T) {
	_, err := StageRaster(t.TempDir(), strings.NewReader("not really a jpeg"))
	if !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("error = %v, want ErrInvalidImage", err)
	}
}

func TestValidateRasterBytesUsesSamePolicy(t *testing.T) {
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 4, 5))); err != nil {
		t.Fatal(err)
	}
	info, err := ValidateRasterBytes(body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if info.Format != "png" || info.Width != 4 || info.Height != 5 {
		t.Fatalf("Info = %+v", info)
	}
}

func TestValidateRasterBytesRejectsTruncatedImageAfterValidConfig(t *testing.T) {
	var complete bytes.Buffer
	if err := jpeg.Encode(&complete, image.NewRGBA(image.Rect(0, 0, 64, 64)), nil); err != nil {
		t.Fatal(err)
	}
	var truncated []byte
	for cut := len(complete.Bytes()) - 1; cut > 0; cut-- {
		candidate := complete.Bytes()[:cut]
		if _, _, err := image.DecodeConfig(bytes.NewReader(candidate)); err != nil {
			continue
		}
		if _, _, err := image.Decode(bytes.NewReader(candidate)); err != nil {
			truncated = candidate
			break
		}
	}
	if truncated == nil {
		t.Fatal("could not construct JPEG with valid config and truncated pixels")
	}
	if _, err := ValidateRasterBytes(truncated); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("error = %v, want ErrInvalidImage", err)
	}
}

func TestValidateRasterBytesRejectsPixelBudgetFromHeader(t *testing.T) {
	body := pngHeader(5_000, 5_000)
	if _, _, err := image.DecodeConfig(bytes.NewReader(body)); err != nil {
		t.Fatalf("crafted PNG config is invalid: %v", err)
	}
	if _, err := ValidateRasterBytes(body); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("error = %v, want ErrImageTooLarge", err)
	}
}

func TestValidateRasterBytesRejectsSVGSpoof(t *testing.T) {
	body := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>`)
	if _, err := ValidateRasterBytes(body); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("error = %v, want ErrInvalidImage", err)
	}
}

func TestValidateRasterBytesRejectsGIF(t *testing.T) {
	var body bytes.Buffer
	if err := gif.Encode(&body, image.NewPaletted(image.Rect(0, 0, 2, 2), []color.Color{color.Black}), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateRasterBytes(body.Bytes()); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("error = %v, want ErrInvalidImage", err)
	}
}

func pngHeader(width, height uint32) []byte {
	body := append([]byte(nil), []byte("\x89PNG\r\n\x1a\n")...)
	data := make([]byte, 13)
	binary.BigEndian.PutUint32(data[0:4], width)
	binary.BigEndian.PutUint32(data[4:8], height)
	data[8] = 8 // bit depth
	data[9] = 6 // RGBA
	chunk := append([]byte("IHDR"), data...)
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(data)))
	body = append(body, length...)
	body = append(body, chunk...)
	checksum := make([]byte, 4)
	binary.BigEndian.PutUint32(checksum, crc32.ChecksumIEEE(chunk))
	return append(body, checksum...)
}

func TestStageRasterRejectsOversizedBodyAndCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	_, err := StageRaster(dir, ioLimitlessByteReader{remaining: MaxImageBytes + 1})
	if !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("error = %v, want ErrImageTooLarge", err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary files leaked: %v", entries)
	}
}

func TestStageRasterContextCancellationWhileDecodeQueuedCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}

	// Occupy the only aggregate-memory decode slot so StageRasterContext must
	// wait after staging its bytes.
	imageDecodeSlots <- struct{}{}
	defer func() { <-imageDecodeSlots }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := StageRasterContext(ctx, dir, bytes.NewReader(body.Bytes()))
		result <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("staged image did not reach the decode queue")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled decode-slot wait did not return")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary files leaked after cancellation: %v", entries)
	}
}

type ioLimitlessByteReader struct{ remaining int64 }

func (r ioLimitlessByteReader) Read(body []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, nil
	}
	if int64(len(body)) > r.remaining {
		body = body[:r.remaining]
	}
	for i := range body {
		body[i] = 'x'
	}
	return len(body), nil
}
