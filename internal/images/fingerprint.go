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
	"math"
	"math/bits"
	"os"
	"strconv"
	"strings"

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

// VisuallyEquivalent matches exact bytes or a conservative perceptual
// fingerprint. Resizing and ordinary JPEG recompression are tolerated; aspect
// changes, larger edge-hash differences, and meaningful colour shifts are not.
func VisuallyEquivalent(left, right Fingerprint) bool {
	if left.ContentHash != "" && left.ContentHash == right.ContentHash {
		return true
	}
	if left.VisualHash == "" || right.VisualHash == "" || left.Width <= 0 || left.Height <= 0 || right.Width <= 0 || right.Height <= 0 {
		return false
	}
	leftAspect := float64(left.Width) / float64(left.Height)
	rightAspect := float64(right.Width) / float64(right.Height)
	if math.Abs(leftAspect-rightAspect)/math.Max(leftAspect, rightAspect) > 0.02 {
		return false
	}
	leftHash, leftRGB, ok := parseVisualHash(left.VisualHash)
	if !ok {
		return false
	}
	rightHash, rightRGB, ok := parseVisualHash(right.VisualHash)
	if !ok || bits.OnesCount64(leftHash^rightHash) > 4 {
		return false
	}
	for i := range leftRGB {
		if absInt(int(leftRGB[i])-int(rightRGB[i])) > 12 {
			return false
		}
	}
	return true
}

func parseVisualHash(value string) (uint64, [3]uint8, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || len(parts[1]) != 6 {
		return 0, [3]uint8{}, false
	}
	hash, err := strconv.ParseUint(parts[0], 16, 64)
	if err != nil {
		return 0, [3]uint8{}, false
	}
	var rgb [3]uint8
	for i := range rgb {
		channel, err := strconv.ParseUint(parts[1][i*2:i*2+2], 16, 8)
		if err != nil {
			return 0, [3]uint8{}, false
		}
		rgb[i] = uint8(channel)
	}
	return hash, rgb, true
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
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
