package transcoder

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	input := filepath.Join(t.TempDir(), "input.mp4")
	gen := exec.Command("ffmpeg", "-y",
		"-f", "lavfi", "-i", "testsrc2=duration=90:size=320x180:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=90",
		"-c:v", "libx264", "-preset", "ultrafast",
		"-c:a", "aac", "-shortest", input)
	out, err := gen.CombinedOutput()
	require.NoError(t, err, "generate test video: %s", out)

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
