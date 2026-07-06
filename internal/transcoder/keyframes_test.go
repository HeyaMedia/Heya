package transcoder

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RealSegmentBoundaries shells out to a real ffmpeg hls-muxer dry run with
// all segment bytes routed to the null device (only the playlist touches
// disk). This exercises the full pipeline — including the -strftime 1 +
// os.DevNull segment-sink trick — against whatever ffmpeg build is on PATH,
// and sanity-checks the parsed boundary sequence.
func TestRealSegmentBoundaries_RealFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH")
	}
	if testing.Short() {
		t.Skip("short mode")
	}

	// 30s of video with a keyframe every 2s (48 frames @ 24fps) and scene-cut
	// keyframes disabled, so ffmpeg's 6s-target split lands deterministically
	// on every third keyframe.
	input := filepath.Join(t.TempDir(), "input.mp4")
	gen := exec.Command("ffmpeg", "-y",
		"-f", "lavfi", "-i", "testsrc2=duration=30:size=320x180:rate=24",
		"-c:v", "libx264", "-preset", "ultrafast",
		"-g", "48", "-x264opts", "scenecut=0",
		input)
	out, err := gen.CombinedOutput()
	require.NoError(t, err, "generate test video: %s", out)

	ends, err := RealSegmentBoundaries(context.Background(), input, 6.0)
	require.NoError(t, err)
	require.NotEmpty(t, ends)

	// Strictly increasing, last boundary ≈ total duration.
	for i := 1; i < len(ends); i++ {
		assert.Greater(t, ends[i], ends[i-1], "boundary %d not increasing", i)
	}
	assert.InDelta(t, 30.0, ends[len(ends)-1], 0.5, "last boundary should be ~file duration")

	// With keyframes exactly every 2s and a 6s target, every full segment is
	// exactly 6s (the muxer cuts at the first keyframe at/after the target).
	assert.Equal(t, 5, len(ends), "30s / 6s keyframe-aligned segments")
	for i, end := range ends[:len(ends)-1] {
		assert.InDelta(t, float64(i+1)*6.0, end, 0.1, "boundary %d", i)
	}
}

func TestRealSegmentBoundaries_EmptyPath(t *testing.T) {
	_, err := RealSegmentBoundaries(context.Background(), "", 6.0)
	assert.Error(t, err)
}

func TestPlannedSegmentTimes_WithKeyframes(t *testing.T) {
	kf := &Keyframes{
		IFrames:  []float64{0, 10.2, 20.5, 30.8, 41.0},
		Duration: 45.0,
	}
	ends := PlannedSegmentTimes(kf, 45.0, 6.0)
	// AV1-style 10s keyframes — each gap is >= 4.5s (75% of 6s)
	assert.Equal(t, []float64{10.2, 20.5, 30.8, 41.0, 45.0}, ends)
}

func TestPlannedSegmentTimes_NoKeyframes(t *testing.T) {
	ends := PlannedSegmentTimes(nil, 30.0, 6.0)
	assert.Equal(t, []float64{6, 12, 18, 24, 30}, ends)
}

func TestPlannedSegmentTimes_EmptyKeyframes(t *testing.T) {
	kf := &Keyframes{IFrames: []float64{}, Duration: 30.0}
	ends := PlannedSegmentTimes(kf, 30.0, 6.0)
	assert.Equal(t, []float64{6, 12, 18, 24, 30}, ends)
}

func TestPlannedSegmentTimes_ShortFile(t *testing.T) {
	ends := PlannedSegmentTimes(nil, 3.5, 6.0)
	assert.Equal(t, []float64{3.5}, ends)
}

func TestKeyframesToSegmentTimesBasic(t *testing.T) {
	kf := &Keyframes{
		IFrames:  []float64{0, 2.0, 4.0, 6.0, 8.0, 10.0, 12.0, 14.0, 16.0},
		Duration: 16.0,
	}
	times := KeyframesToSegmentTimes(kf, 4.0)
	assert.Equal(t, []float64{4.0, 8.0, 12.0, 16.0}, times)
}

func TestKeyframesToSegmentTimesRespectsMinDuration(t *testing.T) {
	kf := &Keyframes{
		IFrames:  []float64{0, 1.0, 2.0, 3.0, 4.0, 5.0, 6.0},
		Duration: 6.0,
	}
	times := KeyframesToSegmentTimes(kf, 3.0)
	assert.Equal(t, []float64{3.0, 6.0}, times)
}

func TestKeyframesToSegmentTimesEmpty(t *testing.T) {
	assert.Nil(t, KeyframesToSegmentTimes(nil, 4.0))
	assert.Nil(t, KeyframesToSegmentTimes(&Keyframes{}, 4.0))
}

func TestKeyframesToSegmentTimesSingle(t *testing.T) {
	kf := &Keyframes{IFrames: []float64{0, 5.0}, Duration: 5.0}
	times := KeyframesToSegmentTimes(kf, 4.0)
	assert.Equal(t, []float64{5.0}, times)
}

func TestKeyframesToSegmentTimesShortFile(t *testing.T) {
	kf := &Keyframes{IFrames: []float64{0, 1.0, 2.0}, Duration: 2.0}
	times := KeyframesToSegmentTimes(kf, 4.0)
	assert.Empty(t, times)
}
