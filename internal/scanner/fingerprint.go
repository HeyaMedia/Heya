package scanner

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

type mediaInfoSummary struct {
	Duration  float64         `json:"duration"`
	Size      int64           `json:"size"`
	Container string          `json:"container"`
	Streams   []streamSummary `json:"streams"`
}

type streamSummary struct {
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

func ComputeContentHash(size int64, mediaInfoJSON []byte) string {
	if len(mediaInfoJSON) == 0 || string(mediaInfoJSON) == "{}" {
		return ""
	}

	var info mediaInfoSummary
	if err := json.Unmarshal(mediaInfoJSON, &info); err != nil {
		return ""
	}

	if info.Duration <= 0 {
		return ""
	}

	var videoCodec string
	var width, height int
	streamCount := len(info.Streams)
	for _, s := range info.Streams {
		if s.CodecType == "video" && videoCodec == "" {
			videoCodec = s.CodecName
			width = s.Width
			height = s.Height
		}
	}

	raw := fmt.Sprintf("%d|%.3f|%s|%d|%s|%dx%d", size, info.Duration, info.Container, streamCount, videoCodec, width, height)
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h[:8])
}
