package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildHwAccelConfigNone(t *testing.T) {
	cfg := BuildHwAccelConfig(HwAccelNone)
	assert.Equal(t, HwAccelNone, cfg.Type)
	assert.Equal(t, "libx264", cfg.EncoderH264)
	assert.Equal(t, "libx265", cfg.EncoderHEVC)
	assert.Empty(t, cfg.InputFlags)
	assert.Equal(t, "scale", cfg.ScaleFilter)
}

func TestBuildHwAccelConfigNVENC(t *testing.T) {
	cfg := BuildHwAccelConfig(HwAccelNVENC)
	assert.Equal(t, HwAccelNVENC, cfg.Type)
	assert.Equal(t, "h264_nvenc", cfg.EncoderH264)
	assert.Equal(t, "hevc_nvenc", cfg.EncoderHEVC)
	assert.Contains(t, cfg.InputFlags, "-hwaccel")
	assert.Contains(t, cfg.InputFlags, "cuda")
	assert.Equal(t, "scale_cuda", cfg.ScaleFilter)
}

func TestBuildHwAccelConfigVAAPI(t *testing.T) {
	cfg := BuildHwAccelConfig(HwAccelVAAPI)
	assert.Equal(t, HwAccelVAAPI, cfg.Type)
	assert.Equal(t, "h264_vaapi", cfg.EncoderH264)
	assert.Equal(t, "hevc_vaapi", cfg.EncoderHEVC)
	assert.Contains(t, cfg.InputFlags, "vaapi")
	assert.Equal(t, "/dev/dri/renderD128", cfg.Device)
	assert.Equal(t, "scale_vaapi", cfg.ScaleFilter)
}

func TestBuildHwAccelConfigQSV(t *testing.T) {
	cfg := BuildHwAccelConfig(HwAccelQSV)
	assert.Equal(t, HwAccelQSV, cfg.Type)
	assert.Equal(t, "h264_qsv", cfg.EncoderH264)
	assert.Equal(t, "hevc_qsv", cfg.EncoderHEVC)
	assert.Equal(t, "scale_qsv", cfg.ScaleFilter)
}

func TestBuildHwAccelConfigVideoToolbox(t *testing.T) {
	cfg := BuildHwAccelConfig(HwAccelVideoToolbox)
	assert.Equal(t, HwAccelVideoToolbox, cfg.Type)
	assert.Equal(t, "h264_videotoolbox", cfg.EncoderH264)
	assert.Equal(t, "hevc_videotoolbox", cfg.EncoderHEVC)
	assert.Empty(t, cfg.InputFlags)
	assert.Equal(t, "scale", cfg.ScaleFilter)
}

func TestBuildHwAccelConfigUnknownFallsToNone(t *testing.T) {
	cfg := BuildHwAccelConfig("bogus")
	assert.Equal(t, HwAccelNone, cfg.Type)
	assert.Equal(t, "libx264", cfg.EncoderH264)
}
