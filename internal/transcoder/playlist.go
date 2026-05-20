package transcoder

import (
	"fmt"
	"math"
	"strings"
)

const SegmentDuration = 6.0

func GeneratePlaylist(totalDuration float64, segmentPattern string, token string) string {
	if totalDuration <= 0 {
		totalDuration = 1
	}

	segCount := int(math.Ceil(totalDuration / SegmentDuration))
	targetDuration := int(math.Ceil(SegmentDuration)) + 1

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:3\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", targetDuration))
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")

	tokenSuffix := ""
	if token != "" {
		tokenSuffix = "?token=" + token
	}

	remaining := totalDuration
	for i := 0; i < segCount; i++ {
		dur := SegmentDuration
		if remaining < dur {
			dur = remaining
		}
		if dur < 0.001 {
			dur = 0.001
		}
		b.WriteString(fmt.Sprintf("#EXTINF:%.6f,\n", dur))
		b.WriteString(fmt.Sprintf(segmentPattern, i))
		b.WriteString(tokenSuffix)
		b.WriteString("\n")
		remaining -= dur
	}

	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}
