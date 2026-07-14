package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

// Fingerprint describes the materialized bytes used for exact and conservative
// visual duplicate detection. VisualHash combines a 64-bit difference hash
// with the image's average RGB colour; this avoids treating unrelated flat or
// very dark artwork as identical merely because their edge hashes are sparse.
type Fingerprint struct {
	ContentHash string
	VisualHash  string
	Width       int
	Height      int
	ByteSize    int64
}

// FingerprintFile hashes and decodes an already size-bounded image cache file.
// Unsupported formats still return the exact checksum and size, allowing byte
// duplicates to collapse even when a perceptual fingerprint cannot be made.
func FingerprintFile(path string) (Fingerprint, error) {
	body, err := os.ReadFile(path) //nolint:gosec // caller supplies a managed image-cache path
	if err != nil {
		return Fingerprint{}, err
	}
	digest := sha256.Sum256(body)
	result := Fingerprint{
		ContentHash: hex.EncodeToString(digest[:]),
		ByteSize:    int64(len(body)),
	}

	decoded, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return result, nil
	}
	bounds := decoded.Bounds()
	result.Width, result.Height = bounds.Dx(), bounds.Dy()
	if result.Width <= 0 || result.Height <= 0 {
		return result, nil
	}

	// A 9x8 sample supplies 8 horizontal comparisons per row (64 dHash
	// bits). Lanczos makes the value stable across upstream resize variants.
	sample := imaging.Resize(decoded, 9, 8, imaging.Lanczos)
	var difference uint64
	var red, green, blue uint64
	var samples uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 9; x++ {
			r, g, b, _ := sample.At(x, y).RGBA()
			red += uint64(r >> 8)
			green += uint64(g >> 8)
			blue += uint64(b >> 8)
			samples++
			if x < 8 {
				left := luminance(sample.At(x, y))
				right := luminance(sample.At(x+1, y))
				if left > right {
					difference |= uint64(1) << uint(y*8+x)
				}
			}
		}
	}
	result.VisualHash = fmt.Sprintf("%016x:%02x%02x%02x", difference, red/samples, green/samples, blue/samples)
	return result, nil
}

func luminance(value colorValue) uint32 {
	r, g, b, _ := value.RGBA()
	// Integer Rec. 601 luma. The scale is irrelevant; only ordering matters.
	return (299*(r>>8) + 587*(g>>8) + 114*(b>>8)) / 1000
}

// colorValue is the small interface shared by color.Color values. Keeping the
// helper local avoids exposing image implementation details to callers.
type colorValue interface {
	RGBA() (r, g, b, a uint32)
}
