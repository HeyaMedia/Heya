package transcoder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGeneratePlaylistBasic(t *testing.T) {
	pl := GeneratePlaylist(30.0, "seg_%04d.ts", "abc123")

	assert.Contains(t, pl, "#EXTM3U")
	assert.Contains(t, pl, "#EXT-X-PLAYLIST-TYPE:VOD")
	assert.Contains(t, pl, "#EXT-X-ENDLIST")
	assert.Contains(t, pl, "seg_0000.ts?token=abc123")
	assert.Contains(t, pl, "seg_0004.ts?token=abc123")

	segments := strings.Count(pl, "#EXTINF:")
	assert.Equal(t, 5, segments)
}

func TestGeneratePlaylistDuration(t *testing.T) {
	pl := GeneratePlaylist(4921.067, "seg_%04d.ts", "tok")

	segments := strings.Count(pl, "#EXTINF:")
	assert.Equal(t, 821, segments)
	assert.Contains(t, pl, "#EXT-X-TARGETDURATION:7")
	assert.Contains(t, pl, "seg_0000.ts?token=tok")
	assert.Contains(t, pl, "seg_0820.ts?token=tok")
}

func TestGeneratePlaylistNoToken(t *testing.T) {
	pl := GeneratePlaylist(12.0, "seg_%04d.ts", "")

	assert.Contains(t, pl, "seg_0000.ts\n")
	assert.NotContains(t, pl, "?token=")
}

func TestGenerateDynamicPlaylist_FMP4(t *testing.T) {
	sess := &TranscodeSession{
		Duration:    30.0,
		TotalSegs:   5,
		SegExt:      ".m4s",
		SegmentEnds: []float64{6, 12, 18, 24, 30},
		segments:    make([]*segReady, 5),
	}
	for i := range sess.segments {
		sess.segments[i] = newSegReady()
	}

	pl := GenerateDynamicPlaylist(sess, "tok123")
	assert.Contains(t, pl, "#EXT-X-MAP:URI=\"init.mp4?token=tok123\"")
	assert.Contains(t, pl, "seg_0.m4s?token=tok123")
	assert.Contains(t, pl, "seg_4.m4s?token=tok123")
	assert.Contains(t, pl, "#EXT-X-ENDLIST")
	assert.NotContains(t, pl, ".ts")
}

func TestGenerateDynamicPlaylist_MPEGTS(t *testing.T) {
	sess := &TranscodeSession{
		Duration:    30.0,
		TotalSegs:   5,
		SegExt:      ".ts",
		SegmentEnds: []float64{6, 12, 18, 24, 30},
		segments:    make([]*segReady, 5),
	}
	for i := range sess.segments {
		sess.segments[i] = newSegReady()
	}

	pl := GenerateDynamicPlaylist(sess, "tok123")
	assert.NotContains(t, pl, "#EXT-X-MAP")
	assert.Contains(t, pl, "seg_0000.ts?token=tok123")
}

func TestGenerateDynamicPlaylist_VariableDurations(t *testing.T) {
	sess := &TranscodeSession{
		Duration:    30.0,
		TotalSegs:   5,
		SegExt:      ".m4s",
		SegmentEnds: []float64{5.5, 11.7, 17.4, 23.0, 30.0},
		segments:    make([]*segReady, 5),
	}
	for i := range sess.segments {
		sess.segments[i] = newSegReady()
	}

	pl := GenerateDynamicPlaylist(sess, "")
	assert.Contains(t, pl, "#EXTINF:5.500000,")
	assert.Contains(t, pl, "#EXTINF:6.200000,")
	assert.Contains(t, pl, "#EXTINF:7.000000,")
	assert.Contains(t, pl, "#EXT-X-ENDLIST")
}
