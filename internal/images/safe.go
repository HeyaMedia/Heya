package images

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/karbowiak/heya/internal/atomicfile"
	_ "golang.org/x/image/webp"
)

const (
	// MaxImageBytes bounds both remote artwork and authenticated uploads.
	MaxImageBytes int64 = 25 << 20
	// Keep decoded artwork comfortably below pathological allocation sizes while
	// still accepting high-resolution posters and 8K-wide banners/panoramas.
	MaxImageDimension int64 = 16_384
	MaxImagePixels    int64 = 20_000_000
)

var (
	ErrImageTooLarge    = errors.New("image exceeds upload limits")
	ErrInvalidImage     = errors.New("invalid or unsupported image")
	ErrImageStageClosed = errors.New("staged image already completed")
)

// A 16-bit PNG can require roughly eight bytes per pixel while decoding.
// Serializing the bounded full-decode step caps aggregate validation memory at
// roughly 160 MiB instead of multiplying that cost by concurrent uploads.
var imageDecodeSlots = make(chan struct{}, 1)

// Info describes image bytes after decoding their actual contents. It never
// derives format or dimensions from the supplied filename or HTTP headers.
type Info struct {
	Format      string
	Extension   string
	ContentType string
	Width       int
	Height      int
	Size        int64
}

// Staged is a validated image waiting to be atomically published. Callers must
// defer Rollback immediately; Rollback is harmless after Commit.
type Staged struct {
	pending *atomicfile.Pending
	Info    Info
}

// StageRaster writes a bounded reader beside its future destination and runs
// the standard decoder for JPEG, PNG, or WebP before returning. Animated GIF
// is intentionally excluded because first-frame validation cannot bound its
// cumulative frame count or decoded pixel work.
func StageRaster(dir string, reader io.Reader) (*Staged, error) {
	return StageRasterContext(context.Background(), dir, reader)
}

// StageRasterContext is StageRaster with cancellation while copying and while
// waiting for the bounded full-decode slot.
func StageRasterContext(ctx context.Context, dir string, reader io.Reader) (*Staged, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if reader == nil {
		return nil, fmt.Errorf("%w: empty reader", ErrInvalidImage)
	}
	pending, err := atomicfile.Create(filepath.Join(dir, "image-upload"), 0o640)
	if err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = pending.Rollback()
		}
	}()

	size, err := io.Copy(pending, io.LimitReader(contextReader{ctx: ctx, reader: reader}, MaxImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("stage image: %w", err)
	}
	if size > MaxImageBytes {
		return nil, fmt.Errorf("%w: maximum is %d bytes", ErrImageTooLarge, MaxImageBytes)
	}
	if err := pending.Close(); err != nil {
		return nil, err
	}

	info, err := validateRasterFile(ctx, pending.TempPath(), size)
	if err != nil {
		return nil, err
	}

	cleanup = false
	return &Staged{pending: pending, Info: info}, nil
}

// Publish atomically makes the staged bytes visible at destination while
// retaining enough state for Rollback to restore an existing file.
func (s *Staged) Publish(destination string) error {
	if s == nil || s.pending == nil {
		return ErrImageStageClosed
	}
	if err := s.pending.Retarget(destination); err != nil {
		return err
	}
	return s.pending.Publish()
}

// Commit accepts a published image and discards its rollback backup.
func (s *Staged) Commit() error {
	if s == nil || s.pending == nil {
		return nil
	}
	err := s.pending.Commit()
	if err == nil {
		s.pending = nil
	}
	return err
}

// Rollback removes unpublished bytes or restores the file replaced by Publish.
func (s *Staged) Rollback() error {
	if s == nil || s.pending == nil {
		return nil
	}
	err := s.pending.Rollback()
	if err == nil {
		s.pending = nil
	}
	return err
}

// ValidateRasterFile verifies an existing cache entry against the same limits
// used for new downloads.
func ValidateRasterFile(path string) (Info, error) {
	return ValidateRasterFileContext(context.Background(), path)
}

// ValidateRasterFileContext validates a managed cache file with a cancelable
// wait for the shared full-decode slot.
func ValidateRasterFileContext(ctx context.Context, path string) (Info, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Info{}, err
	}
	if info.Size() > MaxImageBytes {
		return Info{}, fmt.Errorf("%w: maximum is %d bytes", ErrImageTooLarge, MaxImageBytes)
	}
	return validateRasterFile(ctx, path, info.Size())
}

// ValidateRasterBytes fully decodes in-memory bytes using the same format,
// size, dimension, pixel, and aggregate-concurrency limits as persisted images.
func ValidateRasterBytes(body []byte) (Info, error) {
	return ValidateRasterBytesContext(context.Background(), body)
}

// ValidateRasterBytesContext validates in-memory bytes with a cancelable wait
// for the shared full-decode slot.
func ValidateRasterBytesContext(ctx context.Context, body []byte) (Info, error) {
	if int64(len(body)) > MaxImageBytes {
		return Info{}, fmt.Errorf("%w: maximum is %d bytes", ErrImageTooLarge, MaxImageBytes)
	}
	return validateRaster(ctx, bytes.NewReader(body), int64(len(body)))
}

func validateRasterFile(ctx context.Context, path string, size int64) (Info, error) {
	file, err := os.Open(path) //nolint:gosec // path is a just-created private staging file or managed cache path
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = file.Close() }()

	return validateRaster(ctx, file, size)
}

func validateRaster(ctx context.Context, reader io.ReadSeeker, size int64) (Info, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	config, format, err := image.DecodeConfig(reader)
	if err != nil {
		return Info{}, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}
	extension, contentType, ok := rasterFormat(format)
	if !ok {
		return Info{}, fmt.Errorf("%w: format %q", ErrInvalidImage, format)
	}
	if err := validateDimensions(config.Width, config.Height); err != nil {
		return Info{}, err
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return Info{}, err
	}
	select {
	case imageDecodeSlots <- struct{}{}:
	case <-ctx.Done():
		return Info{}, ctx.Err()
	}
	defer func() { <-imageDecodeSlots }()
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	decoded, decodedFormat, err := image.Decode(reader)
	if err != nil {
		return Info{}, fmt.Errorf("%w: %v", ErrInvalidImage, err)
	}
	bounds := decoded.Bounds()
	if decodedFormat != format || bounds.Dx() != config.Width || bounds.Dy() != config.Height {
		return Info{}, fmt.Errorf("%w: inconsistent decoded image", ErrInvalidImage)
	}
	return Info{
		Format: format, Extension: extension, ContentType: contentType,
		Width: config.Width, Height: config.Height, Size: size,
	}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(body []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(body)
}

func validateDimensions(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("%w: dimensions must be positive", ErrInvalidImage)
	}
	w, h := int64(width), int64(height)
	if w > MaxImageDimension || h > MaxImageDimension || w*h > MaxImagePixels {
		return fmt.Errorf("%w: dimensions %dx%d exceed %d pixels or %d per side", ErrImageTooLarge, width, height, MaxImagePixels, MaxImageDimension)
	}
	return nil
}

func rasterFormat(format string) (extension, contentType string, ok bool) {
	switch format {
	case "jpeg":
		return ".jpg", "image/jpeg", true
	case "png":
		return ".png", "image/png", true
	case "webp":
		return ".webp", "image/webp", true
	default:
		return "", "", false
	}
}
