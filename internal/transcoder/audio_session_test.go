package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsAllowedAudioBitrate(t *testing.T) {
	for _, kbps := range []int{320, 256, 192, 128} {
		assert.True(t, IsAllowedAudioBitrate(kbps), "kbps=%d", kbps)
	}
	for _, kbps := range []int{0, 64, 96, 160, 224, 288, 384, -1} {
		assert.False(t, IsAllowedAudioBitrate(kbps), "kbps=%d", kbps)
	}
}

func TestAudioProfile(t *testing.T) {
	assert.Equal(t, "aac-256", audioProfile(256))
	assert.Equal(t, "aac-128", audioProfile(128))
}

// TestShouldTranscodeForTier locks in the direct-vs-transcode rule for an
// explicit "quality" tier request:
//   - lossless sources (flac/alac/wav) always transcode — there's headroom
//     worth shaping into the requested bitrate.
//   - lossy sources only transcode when their on-disk bitrate meaningfully
//     exceeds the tier (more than +16kbps of slack); otherwise re-encoding
//     "up" gains nothing and the caller should serve the file direct.
func TestShouldTranscodeForTier(t *testing.T) {
	tests := []struct {
		name        string
		format      string
		bitrateKbps int
		tierKbps    int
		want        bool
	}{
		{"flac always transcodes even far under tier", "flac", 50, 320, true},
		{"alac always transcodes", "alac", 1000, 128, true},
		{"wav always transcodes", "wav", 1411, 320, true},
		{"format case-insensitive lossless", "FLAC", 50, 320, true},

		{"mp3 well under tier serves direct (upsampling gains nothing)", "mp3", 128, 320, false},
		{"mp3 exactly at tier serves direct", "mp3", 192, 192, false},
		{"mp3 within +16 margin serves direct", "mp3", 192, 176, false},
		{"mp3 just over +16 margin transcodes", "mp3", 192, 175, true},
		{"mp3 far over tier transcodes", "mp3", 320, 128, true},
		{"aac at tier serves direct", "aac", 256, 256, false},
		{"m4a at tier serves direct", "m4a", 256, 256, false},
		{"unknown format treated as lossy", "wma", 128, 320, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ShouldTranscodeForTier(tt.format, tt.bitrateKbps, tt.tierKbps))
		})
	}
}

func TestStderrTailBoundsMemory(t *testing.T) {
	tw := newStderrTail(16)
	_, _ = tw.Write([]byte("0123456789"))
	_, _ = tw.Write([]byte("abcdefghij"))
	// 20 bytes written, only the last 16 should survive.
	assert.Equal(t, "456789abcdefghij", tw.String())
	assert.LessOrEqual(t, len(tw.buf), 16)
}
