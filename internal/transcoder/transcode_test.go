package transcoder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIfNoFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
}

func generateTestVideo(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "input.mkv")
	cmd := exec.Command("ffmpeg", "-y",
		"-f", "lavfi", "-i", "testsrc2=duration=5:size=640x360:rate=24",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=5:sample_rate=48000",
		"-c:v", "libx264", "-preset", "ultrafast", "-crf", "28",
		"-c:a", "flac",
		"-shortest",
		src,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "generate test video: %s", string(out))
	return src
}

func TestTranscodeToMP4_CopyVideoTranscodeAudio(t *testing.T) {
	skipIfNoFFmpeg(t)
	dir := t.TempDir()
	src := generateTestVideo(t, dir)
	outPath := filepath.Join(dir, "output.mp4")

	err := TranscodeToMP4(context.Background(), TranscodeOpts{
		Input: src,
		Profile: Profile{
			Name:       "360p",
			VideoCodec: "copy",
			AudioCodec: "aac",
		},
		HWAccel: BuildHwAccelConfig(HwAccelNone),
	}, outPath)
	require.NoError(t, err)

	stat, err := os.Stat(outPath)
	require.NoError(t, err)
	assert.Greater(t, stat.Size(), int64(1000), "output MP4 should be non-trivial size")

	probeCodecs(t, outPath, "h264", "aac")
}

func TestTranscodeToMP4_FullTranscode(t *testing.T) {
	skipIfNoFFmpeg(t)
	dir := t.TempDir()
	src := generateTestVideo(t, dir)
	outPath := filepath.Join(dir, "output.mp4")

	err := TranscodeToMP4(context.Background(), TranscodeOpts{
		Input: src,
		Profile: Profile{
			Name:       "240p",
			VideoCodec: "libx264",
			AudioCodec: "aac",
			CRF:        26,
			Preset:     "ultrafast",
			MaxHeight:  240,
		},
		HWAccel: BuildHwAccelConfig(HwAccelNone),
	}, outPath)
	require.NoError(t, err)

	stat, err := os.Stat(outPath)
	require.NoError(t, err)
	assert.Greater(t, stat.Size(), int64(500))

	probeCodecs(t, outPath, "h264", "aac")
	probeHeight(t, outPath, 240)
}

func TestTranscodeToMP4_Remux(t *testing.T) {
	skipIfNoFFmpeg(t)
	dir := t.TempDir()
	src := generateTestVideo(t, dir)
	outPath := filepath.Join(dir, "output.mp4")

	err := TranscodeToMP4(context.Background(), TranscodeOpts{
		Input: src,
		Profile: Profile{
			Name:       "remux",
			VideoCodec: "copy",
			AudioCodec: "copy",
		},
		HWAccel: BuildHwAccelConfig(HwAccelNone),
	}, outPath)
	require.NoError(t, err)

	stat, err := os.Stat(outPath)
	require.NoError(t, err)
	assert.Greater(t, stat.Size(), int64(1000))

	probeCodecs(t, outPath, "h264", "flac")
}

func TestTranscodeToMP4_FragmentedSeekable(t *testing.T) {
	skipIfNoFFmpeg(t)
	dir := t.TempDir()
	src := generateTestVideo(t, dir)
	outPath := filepath.Join(dir, "output.mp4")

	err := TranscodeToMP4(context.Background(), TranscodeOpts{
		Input: src,
		Profile: Profile{
			Name:       "360p",
			VideoCodec: "copy",
			AudioCodec: "aac",
		},
		HWAccel: BuildHwAccelConfig(HwAccelNone),
	}, outPath)
	require.NoError(t, err)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)

	hasFtyp := len(data) >= 8 && string(data[4:8]) == "ftyp"
	assert.True(t, hasFtyp, "fragmented MP4 should start with ftyp box")

	hasMoov := false
	for i := 0; i < len(data)-4; i++ {
		if string(data[i:i+4]) == "moov" {
			hasMoov = true
			break
		}
	}
	assert.True(t, hasMoov, "fragmented MP4 should contain moov atom")
}

func probeCodecs(t *testing.T, path, wantVideo, wantAudio string) {
	t.Helper()
	out, err := exec.Command("ffprobe",
		"-v", "quiet", "-show_entries", "stream=codec_name,codec_type",
		"-of", "csv=p=0", path,
	).Output()
	require.NoError(t, err, "ffprobe failed")

	output := string(out)
	assert.Contains(t, output, wantVideo)
	assert.Contains(t, output, wantAudio)
}

func probeHeight(t *testing.T, path string, wantHeight int) {
	t.Helper()
	out, err := exec.Command("ffprobe",
		"-v", "quiet", "-select_streams", "v:0",
		"-show_entries", "stream=height",
		"-of", "csv=p=0", path,
	).Output()
	require.NoError(t, err)
	assert.Contains(t, string(out), fmt.Sprintf("%d", wantHeight))
}
