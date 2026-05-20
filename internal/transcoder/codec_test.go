package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVideoCodecString(t *testing.T) {
	tests := []struct {
		codec    string
		expected string
	}{
		{"h264", "avc1.640028"},
		{"avc", "avc1.640028"},
		{"hevc", "hev1.1.6.L120.B0"},
		{"h265", "hev1.1.6.L120.B0"},
		{"av1", "av01.0.08M.10"},
		{"vp9", "vp09.02.10.10"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.codec, func(t *testing.T) {
			assert.Equal(t, tt.expected, VideoCodecString(tt.codec))
		})
	}
}

func TestAudioCodecString(t *testing.T) {
	tests := []struct {
		codec    string
		expected string
	}{
		{"aac", "mp4a.40.2"},
		{"opus", "Opus"},
		{"flac", "fLaC"},
		{"ac3", "ac-3"},
		{"eac3", "ec-3"},
		{"mp3", "mp4a.40.34"},
		{"vorbis", "vorbis"},
	}
	for _, tt := range tests {
		t.Run(tt.codec, func(t *testing.T) {
			assert.Equal(t, tt.expected, AudioCodecString(tt.codec))
		})
	}
}

func TestFormatCodecString(t *testing.T) {
	assert.Equal(t, "avc1.640028,mp4a.40.2", FormatCodecString("h264", "aac"))
	assert.Equal(t, "hev1.1.6.L120.B0,mp4a.40.2", FormatCodecString("hevc", "aac"))
	assert.Equal(t, "mp4a.40.2", FormatCodecString("", "aac"))
	assert.Equal(t, "avc1.640028", FormatCodecString("h264", ""))
	assert.Equal(t, "", FormatCodecString("", ""))
}
