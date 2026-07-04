package transcoder

import (
	"fmt"
	"math"
	"strings"
)

const SegmentDuration = 6.0

func GenerateDynamicPlaylist(sess *TranscodeSession, token string) string {
	// Compute per-segment durations and target duration from SegmentEnds.
	n := len(sess.SegmentEnds)
	if n == 0 {
		n = sess.TotalSegs
	}
	durs := make([]float64, n)
	prev := 0.0
	var maxDur float64
	for i := 0; i < n; i++ {
		var d float64
		if i < len(sess.SegmentEnds) {
			d = sess.SegmentEnds[i] - prev
			prev = sess.SegmentEnds[i]
		} else {
			d = SegmentDuration
		}
		if d < 0.001 {
			d = 0.001
		}
		durs[i] = d
		if d > maxDur {
			maxDur = d
		}
	}
	if maxDur < SegmentDuration {
		maxDur = SegmentDuration
	}
	targetDuration := int(math.Ceil(maxDur))

	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	b.WriteString("#EXT-X-VERSION:6\n")
	b.WriteString(fmt.Sprintf("#EXT-X-TARGETDURATION:%d\n", targetDuration))
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n")
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	b.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")

	tokenSuffix := ""
	if token != "" {
		tokenSuffix = "?token=" + token
	}

	if sess.IsFMP4() {
		b.WriteString(fmt.Sprintf("#EXT-X-MAP:URI=\"init.mp4%s\"\n", tokenSuffix))
	}

	segFmt := "seg_%04d" + sess.SegExt
	if sess.IsFMP4() {
		segFmt = "seg_%d" + sess.SegExt
	}
	for i := 0; i < n; i++ {
		b.WriteString(fmt.Sprintf("#EXTINF:%.6f,\n", durs[i]))
		b.WriteString(fmt.Sprintf(segFmt+"%s\n", i, tokenSuffix))
	}
	b.WriteString("#EXT-X-ENDLIST\n")

	return b.String()
}
