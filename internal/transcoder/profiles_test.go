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

func TestGetProfileBackwardCompat(t *testing.T) {
	p, ok := GetProfile("1080p")
	assert.True(t, ok)
	assert.Equal(t, 1080, p.MaxHeight)
	assert.Equal(t, "libx264", p.VideoCodec)

	_, ok = GetProfile("nonexistent")
	assert.False(t, ok)
}
