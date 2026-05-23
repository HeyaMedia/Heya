package transcoder

import (
	"context"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type HwAccelType string

const (
	HwAccelNone         HwAccelType = "none"
	HwAccelVAAPI        HwAccelType = "vaapi"
	HwAccelQSV          HwAccelType = "qsv"
	HwAccelNVENC        HwAccelType = "nvenc"
	HwAccelVideoToolbox HwAccelType = "videotoolbox"
)

type HwAccelConfig struct {
	Type        HwAccelType
	Device      string
	EncoderH264 string
	EncoderHEVC string
	InputFlags  []string
	ScaleFilter string
}

var (
	detectedAccel HwAccelType
	detectOnce    sync.Once
)

func DetectHardwareAccel() HwAccelType {
	detectOnce.Do(func() {
		detectedAccel = probeHardwareAccel()
	})
	return detectedAccel
}

func probeHardwareAccel() HwAccelType {
	if !IsFFmpegAvailable() {
		return HwAccelNone
	}

	if runtime.GOOS == "darwin" {
		if probeEncoder("h264_videotoolbox") {
			log.Info().Msg("detected VideoToolbox hardware acceleration")
			return HwAccelVideoToolbox
		}
	}

	if probeEncoder("h264_nvenc") {
		log.Info().Msg("detected NVIDIA NVENC hardware acceleration")
		return HwAccelNVENC
	}

	if probeEncoderWithFlags("h264_qsv", "-init_hw_device", "qsv=hw", "-filter_hw_device", "hw") {
		log.Info().Msg("detected Intel QSV hardware acceleration")
		return HwAccelQSV
	}

	if probeEncoderWithFlags("h264_vaapi", "-vaapi_device", "/dev/dri/renderD128") {
		log.Info().Msg("detected VAAPI hardware acceleration")
		return HwAccelVAAPI
	}

	return HwAccelNone
}

func probeEncoder(encoder string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-f", "lavfi", "-i", "nullsrc=s=64x64:d=1",
		"-c:v", encoder,
		"-f", "null", "-",
	)
	return cmd.Run() == nil
}

func probeEncoderWithFlags(encoder string, extraFlags ...string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := []string{"-f", "lavfi", "-i", "nullsrc=s=64x64:d=1"}
	args = append(args, extraFlags...)
	args = append(args, "-c:v", encoder, "-f", "null", "-")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	return cmd.Run() == nil
}

func BuildHwAccelConfig(accelType HwAccelType) HwAccelConfig {
	switch accelType {
	case HwAccelNVENC:
		return HwAccelConfig{
			Type:        HwAccelNVENC,
			EncoderH264: "h264_nvenc",
			EncoderHEVC: "hevc_nvenc",
			InputFlags:  []string{"-hwaccel", "cuda", "-hwaccel_output_format", "cuda"},
			ScaleFilter: "scale_cuda",
		}
	case HwAccelVAAPI:
		return HwAccelConfig{
			Type:        HwAccelVAAPI,
			Device:      "/dev/dri/renderD128",
			EncoderH264: "h264_vaapi",
			EncoderHEVC: "hevc_vaapi",
			InputFlags:  []string{"-hwaccel", "vaapi", "-vaapi_device", "/dev/dri/renderD128", "-hwaccel_output_format", "vaapi"},
			ScaleFilter: "scale_vaapi",
		}
	case HwAccelQSV:
		return HwAccelConfig{
			Type:        HwAccelQSV,
			EncoderH264: "h264_qsv",
			EncoderHEVC: "hevc_qsv",
			InputFlags: []string{
				"-init_hw_device", "qsv=hw",
				"-filter_hw_device", "hw",
				"-hwaccel", "qsv",
				"-hwaccel_output_format", "qsv",
			},
			ScaleFilter: "scale_qsv",
		}
	case HwAccelVideoToolbox:
		return HwAccelConfig{
			Type:        HwAccelVideoToolbox,
			EncoderH264: "h264_videotoolbox",
			EncoderHEVC: "hevc_videotoolbox",
			InputFlags:  []string{},
			ScaleFilter: "scale",
		}
	default:
		return HwAccelConfig{
			Type:        HwAccelNone,
			EncoderH264: "libx264",
			EncoderHEVC: "libx265",
			InputFlags:  []string{},
			ScaleFilter: "scale",
		}
	}
}

func (c HwAccelConfig) ScaleVideoFilter(height int) string {
	switch c.Type {
	case HwAccelNVENC:
		return "scale_cuda=-2:min'(%d,ih)'"
	case HwAccelVAAPI:
		return "scale_vaapi=w=-2:h=min(%d\\,ih)"
	case HwAccelQSV:
		return "scale_qsv=w=-2:h=min(%d\\,ih)"
	case HwAccelVideoToolbox:
		return "scale=-2:min(%d\\,ih),format=nv12"
	default:
		return "scale=-2:'min(%d,ih)'"
	}
}
