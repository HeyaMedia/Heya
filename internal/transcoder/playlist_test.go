package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestGenerateDynamicPlaylistWithCastSessionQuery(t *testing.T) {
	sess := &TranscodeSession{
		Duration:    12,
		TotalSegs:   2,
		SegExt:      ".ts",
		SegmentEnds: []float64{6, 12},
		segments:    make([]*segReady, 2),
	}
	for i := range sess.segments {
		sess.segments[i] = newSegReady()
	}
	pl := GenerateDynamicPlaylistWithQuery(sess, "audio=1&cast_token=signed&sid=cast-123")
	assert.Contains(t, pl, "seg_0000.ts?audio=1&cast_token=signed&sid=cast-123")
	assert.NotContains(t, pl, "?token=")
}
