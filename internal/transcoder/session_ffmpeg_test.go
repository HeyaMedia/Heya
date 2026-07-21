package transcoder

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateTestVideo renders a 90s synthetic AV file for end-to-end session
// tests. Shared by the fMP4 and MPEG-TS seek regressions below.
func generateTestVideo(t *testing.T) string {
	t.Helper()
	input := filepath.Join(t.TempDir(), "input.mp4")
	gen := exec.Command("ffmpeg", "-y",
		"-f", "lavfi", "-i", "testsrc2=duration=90:size=320x180:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=90",
		"-c:v", "libx264", "-preset", "ultrafast",
		"-c:a", "aac", "-shortest", input)
	out, err := gen.CombinedOutput()
	require.NoError(t, err, "generate test video: %s", out)
	return input
}

// probeSegment returns (start_time, duration) of a produced segment.
func probeSegment(t *testing.T, path string) (float64, float64) {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=start_time,duration",
		"-of", "csv=p=0", path).Output()
	require.NoError(t, err, "ffprobe %s", path)
	fields := strings.Split(strings.TrimSpace(string(out)), ",")
	require.Len(t, fields, 2, "ffprobe output: %q", out)
	start, err := strconv.ParseFloat(fields[0], 64)
	require.NoError(t, err)
	dur, err := strconv.ParseFloat(fields[1], 64)
	require.NoError(t, err)
	return start, dur
}

// End-to-end seek regression with a real ffmpeg: forward seek past the
// threshold, then a backward seek behind the running head's start segment.
// Pre-fix, the backward request waited on a head that would never produce
// the segment (10s 503), and the reconcile range-fill then marked the gap
// segments ready, dead-ending playback in permanent 404s.
func TestSession_SeekBackwardWithRealFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	if testing.Short() {
		t.Skip("short mode")
	}

	input := generateTestVideo(t)

	cache := NewCacheManager(t.TempDir(), 0)
	sm := NewSessionManager(cache, NewHwAccelProvider(t.TempDir(), "none"), NewFFmpegBuilder())
	defer sm.Close()

	opts := TranscodeOpts{
		Input: input,
		Profile: Profile{
			Name: "240p", VideoCodec: "libx264", AudioCodec: "aac",
			CRF: 30, MaxBitrate: "700k", Preset: "ultrafast", MaxHeight: 240,
		},
		HWAccel: BuildHwAccelConfig(HwAccelNone),
		UseFMP4: true,
	}
	sess := sm.GetOrCreate(context.Background(), 1, input, opts, "test", 90, nil)
	require.NotNil(t, sess)
	require.Equal(t, 15, sess.TotalSegs, "90s at 6s segments")

	request := func(idx int) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return sess.RequestSegment(ctx, idx)
	}

	// Playback start: head_8 via a mid-file request (forward-seek shape).
	require.True(t, request(8), "seg 8 should transcode")
	assert.True(t, sess.segmentFileExists(8))

	// Backward seek behind head_8's start while it may still be running.
	require.True(t, request(2), "backward seek must spawn a new head, not wait forever")
	assert.True(t, sess.segmentFileExists(2))

	// Sanity: everything reported ready is actually on disk (no range-fill
	// poisoning from the reconcile tick).
	for i := 0; i < sess.TotalSegs; i++ {
		if sess.IsSegmentReady(i) {
			assert.True(t, sess.segmentFileExists(i), "seg %d marked ready but missing on disk", i)
		}
	}
}

// MPEG-TS seek regression with a real ffmpeg. Pre-fix, the TS path ran the
// `segment` muxer whose boundary grid is offset by -segment_start_number on
// top of -copyts timestamps: a seek head's first cut landed
// start_number*SegmentDuration seconds too late, so the first segment after
// a seek swallowed many minutes of content (356MB in the wild) while the
// player timed out and re-requested it in a loop. The hls muxer cuts
// relative to the stream start, so the first post-seek segment must start at
// the seek target and span ~one SegmentDuration.
func TestSession_TSSeekSegmentsAlignWithPlaylist(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	if testing.Short() {
		t.Skip("short mode")
	}

	input := generateTestVideo(t)

	cache := NewCacheManager(t.TempDir(), 0)
	sm := NewSessionManager(cache, NewHwAccelProvider(t.TempDir(), "none"), NewFFmpegBuilder())
	defer sm.Close()

	opts := TranscodeOpts{
		Input: input,
		Profile: Profile{
			Name: "240p", VideoCodec: "libx264", AudioCodec: "aac",
			CRF: 30, MaxBitrate: "700k", Preset: "ultrafast", MaxHeight: 240,
		},
		HWAccel: BuildHwAccelConfig(HwAccelNone),
		UseFMP4: false,
	}
	sess := sm.GetOrCreate(context.Background(), 2, input, opts, "test-ts", 90, nil)
	require.NotNil(t, sess)
	require.Equal(t, ".ts", sess.SegExt)
	require.Equal(t, 15, sess.TotalSegs, "90s at 6s segments")

	request := func(idx int) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return sess.RequestSegment(ctx, idx)
	}

	// Seek shape: first request lands mid-file → head_8 with -ss 48.
	require.True(t, request(8), "seg 8 should transcode")
	require.True(t, sess.segmentFileExists(8))

	start, dur := probeSegment(t, sess.SegmentPath(8))
	assert.InDelta(t, 48.0, start, 1.5, "first post-seek segment must start at the seek target (copyts)")
	assert.Less(t, dur, 8.0, "post-seek segment must be ~SegmentDuration, not the rest of the file")

	// The same head keeps producing aligned segments past the seek point.
	require.True(t, request(9), "seg 9 should follow from the same head")
	start9, dur9 := probeSegment(t, sess.SegmentPath(9))
	assert.InDelta(t, start+dur, start9, 0.5, "segments must be contiguous")
	assert.Less(t, dur9, 8.0)
}
