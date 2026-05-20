package transcoder

import (
	"fmt"
	"strings"
)

func VideoCodecString(codec string) string {
	switch strings.ToLower(codec) {
	case "h264", "avc", "avc1":
		return "avc1.640028"
	case "hevc", "h265", "hev1":
		return "hev1.1.6.L120.B0"
	case "av1", "av01":
		return "av01.0.08M.10"
	case "vp9", "vp09":
		return "vp09.02.10.10"
	default:
		return codec
	}
}

func AudioCodecString(codec string) string {
	switch strings.ToLower(codec) {
	case "aac":
		return "mp4a.40.2"
	case "opus":
		return "Opus"
	case "flac":
		return "fLaC"
	case "ac3", "ac-3":
		return "ac-3"
	case "eac3", "ec-3":
		return "ec-3"
	case "mp3":
		return "mp4a.40.34"
	case "vorbis":
		return "vorbis"
	default:
		return codec
	}
}

func FormatCodecString(videoCodec, audioCodec string) string {
	v := VideoCodecString(videoCodec)
	a := AudioCodecString(audioCodec)
	if v == "" && a == "" {
		return ""
	}
	if v == "" {
		return a
	}
	if a == "" {
		return v
	}
	return fmt.Sprintf("%s,%s", v, a)
}

func TranscodeCodecString(quality VideoQuality, hwAccel HwAccelConfig, audioCodec string) string {
	videoStr := "avc1.640028"
	if strings.Contains(hwAccel.EncoderH264, "hevc") || strings.Contains(hwAccel.EncoderHEVC, "hevc") {
		videoStr = "hev1.1.6.L120.B0"
	}
	audioStr := AudioCodecString(audioCodec)
	if audioStr == "" {
		audioStr = "mp4a.40.2"
	}
	return fmt.Sprintf("%s,%s", videoStr, audioStr)
}
