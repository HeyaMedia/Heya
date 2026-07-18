package transcoder

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	output, err := gen.CombinedOutput()
	require.NoError(t, err, "generate test video: %s", output)

	ends, err := RealSegmentBoundaries(context.Background(), input, 6.0)
	require.NoError(t, err)
	require.NotEmpty(t, ends)

	for i := 1; i < len(ends); i++ {
		assert.Greater(t, ends[i], ends[i-1], "boundary %d not increasing", i)
	}
	assert.InDelta(t, 30.0, ends[len(ends)-1], 0.5, "last boundary should be ~file duration")
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
	keyframes := &Keyframes{IFrames: []float64{0, 10.2, 20.5, 30.8, 41.0}, Duration: 45.0}
	assert.Equal(t, []float64{10.2, 20.5, 30.8, 41.0, 45.0}, PlannedSegmentTimes(keyframes, 45.0, 6.0))
}

func TestPlannedSegmentTimes_NoKeyframes(t *testing.T) {
	assert.Equal(t, []float64{6, 12, 18, 24, 30}, PlannedSegmentTimes(nil, 30.0, 6.0))
}

func TestPlannedSegmentTimes_EmptyKeyframes(t *testing.T) {
	keyframes := &Keyframes{IFrames: []float64{}, Duration: 30.0}
	assert.Equal(t, []float64{6, 12, 18, 24, 30}, PlannedSegmentTimes(keyframes, 30.0, 6.0))
}

func TestPlannedSegmentTimes_ShortFile(t *testing.T) {
	assert.Equal(t, []float64{3.5}, PlannedSegmentTimes(nil, 3.5, 6.0))
}

func TestKeyframesToSegmentTimesBasic(t *testing.T) {
	keyframes := &Keyframes{IFrames: []float64{0, 2, 4, 6, 8, 10, 12, 14, 16}, Duration: 16}
	assert.Equal(t, []float64{4.0, 8.0, 12.0, 16.0}, KeyframesToSegmentTimes(keyframes, 4.0))
}

func TestKeyframesToSegmentTimesRespectsMinDuration(t *testing.T) {
	keyframes := &Keyframes{IFrames: []float64{0, 1, 2, 3, 4, 5, 6}, Duration: 6}
	assert.Equal(t, []float64{3.0, 6.0}, KeyframesToSegmentTimes(keyframes, 3.0))
}

func TestKeyframesToSegmentTimesEmpty(t *testing.T) {
	assert.Nil(t, KeyframesToSegmentTimes(nil, 4.0))
	assert.Nil(t, KeyframesToSegmentTimes(&Keyframes{}, 4.0))
}

func TestKeyframesToSegmentTimesSingle(t *testing.T) {
	keyframes := &Keyframes{IFrames: []float64{0, 5}, Duration: 5}
	assert.Equal(t, []float64{5.0}, KeyframesToSegmentTimes(keyframes, 4.0))
}

func TestKeyframesToSegmentTimesShortFile(t *testing.T) {
	keyframes := &Keyframes{IFrames: []float64{0, 1, 2}, Duration: 2}
	assert.Empty(t, KeyframesToSegmentTimes(keyframes, 4.0))
}

func shellCommand(output string, exitCode int) *exec.Cmd {
	script := "printf '%s' " + shellQuote(output)
	if exitCode != 0 {
		script += "; printf '%s' 'synthetic ffprobe failure' >&2; exit " + strconv.Itoa(exitCode)
	}
	return exec.Command("sh", "-c", script)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestExtractKeyframesRejectsPartialOutputFromFailedProbe(t *testing.T) {
	factory := func(context.Context, string, ...string) *exec.Cmd {
		return shellCommand("0.000,K_\n6.000,K_\n", 7)
	}

	keyframes, err := extractKeyframes(context.Background(), filepath.Join(t.TempDir(), "ignored.mkv"), factory)
	require.Error(t, err)
	assert.Nil(t, keyframes)
	assert.Contains(t, err.Error(), "synthetic ffprobe failure")
}

func TestExtractKeyframesUsesDocumentedDurationFallback(t *testing.T) {
	call := 0
	factory := func(context.Context, string, ...string) *exec.Cmd {
		call++
		if call == 1 {
			return shellCommand("0.000,K_\n6.500,K_\n", 0)
		}
		return shellCommand("", 1)
	}

	keyframes, err := extractKeyframes(context.Background(), filepath.Join(t.TempDir(), "ignored.mkv"), factory)
	require.NoError(t, err)
	require.NotNil(t, keyframes)
	assert.Equal(t, []float64{0, 6.5}, keyframes.IFrames)
	assert.Equal(t, 6.5, keyframes.Duration)
}
