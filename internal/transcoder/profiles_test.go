package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVideoQualityHeight(t *testing.T) {
	assert.Equal(t, 1080, Quality1080p.Height())
	assert.Equal(t, 720, Quality720p.Height())
	assert.Equal(t, 2160, Quality2160p.Height())
	assert.Equal(t, 0, QualityOriginal.Height())
}

func TestVideoQualityMaxBitrate(t *testing.T) {
	assert.Equal(t, int64(8_000_000), Quality1080p.MaxBitrate("h264"))
	assert.Equal(t, int64(6_000_000), Quality1080p.MaxBitrate("hevc"))
	assert.Equal(t, int64(4_000_000), Quality1080p.MaxBitrate("av1"))

	assert.Equal(t, int64(8_000_000), Quality1080p.MaxBitrate("unknown_codec"))
}

func TestVideoQualityString(t *testing.T) {
	assert.Equal(t, "1080p", Quality1080p.String())
	assert.Equal(t, "720p", Quality720p.String())
	assert.Equal(t, "original", QualityOriginal.String())
}

func TestBuildBitrateLadder1080p(t *testing.T) {
	ladder := BuildBitrateLadder(1080)
	assert.Equal(t, []VideoQuality{Quality1080p, Quality720p, Quality480p, Quality360p, Quality240p}, ladder)
}

func TestBuildBitrateLadder4K(t *testing.T) {
	ladder := BuildBitrateLadder(2160)
	assert.Equal(t, Quality2160p, ladder[0])
	assert.Contains(t, ladder, Quality1080p)
	assert.Contains(t, ladder, Quality720p)
}

func TestBuildBitrateLadder720p(t *testing.T) {
	ladder := BuildBitrateLadder(720)
	assert.Equal(t, Quality720p, ladder[0])
	assert.NotContains(t, ladder, Quality1080p)
}

func TestBuildBitrateLadderVerySmall(t *testing.T) {
	ladder := BuildBitrateLadder(100)
	assert.Len(t, ladder, 1)
	assert.Equal(t, Quality240p, ladder[0])
}

func TestAudioQualityBitrate(t *testing.T) {
	assert.Equal(t, 128_000, Audio128k.Bitrate())
	assert.Equal(t, 256_000, Audio256k.Bitrate())
	assert.Equal(t, 0, AudioOriginal.Bitrate())
}

func TestGetProfileBackwardCompat(t *testing.T) {
	p, ok := GetProfile("1080p")
	assert.True(t, ok)
	assert.Equal(t, 1080, p.MaxHeight)
	assert.Equal(t, "libx264", p.VideoCodec)

	_, ok = GetProfile("nonexistent")
	assert.False(t, ok)
}

func TestQualityToProfile(t *testing.T) {
	hwNone := BuildHwAccelConfig(HwAccelNone)
	p := QualityToProfile(Quality1080p, hwNone)
	assert.Equal(t, "1080p", p.Name)
	assert.Equal(t, "libx264", p.VideoCodec)
	assert.Equal(t, 1080, p.MaxHeight)

	hwNvenc := BuildHwAccelConfig(HwAccelNVENC)
	p = QualityToProfile(Quality720p, hwNvenc)
	assert.Equal(t, "h264_nvenc", p.VideoCodec)
	assert.Equal(t, 720, p.MaxHeight)
}
