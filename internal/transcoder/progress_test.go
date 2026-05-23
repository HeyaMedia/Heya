package transcoder

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Sample of the exact format ffmpeg emits via `-progress pipe:N`. Pulled
// from a real `ffmpeg -progress pipe:1 ...` run.
const sampleProgressBlock = `frame=120
fps=23.98
stream_0_0_q=23.0
bitrate=2500.0kbits/s
total_size=12345678
out_time_us=5000000
out_time=00:00:05.000000
dup_frames=0
drop_frames=2
speed=1.05x
progress=continue
frame=240
fps=24.10
bitrate=2510.5kbits/s
total_size=25000000
out_time_us=10000000
dup_frames=0
drop_frames=2
speed=1.06x
progress=end
`

func TestProgressReader_ParsesBlocks(t *testing.T) {
	var snapshots []ProgressStats
	started := time.Now()

	progressReader(strings.NewReader(sampleProgressBlock), started, func(apply func(*ProgressStats)) {
		var p ProgressStats
		apply(&p)
		snapshots = append(snapshots, p)
	})

	if !assert.Len(t, snapshots, 2, "two progress blocks should produce two snapshots") {
		return
	}

	first := snapshots[0]
	assert.Equal(t, int64(120), first.Frame)
	assert.InDelta(t, 23.98, first.FPS, 0.001)
	assert.InDelta(t, 2500.0, first.Bitrate, 0.001)
	assert.Equal(t, int64(12345678), first.TotalSize)
	assert.InDelta(t, 5.0, first.OutTimeSeconds, 0.001)
	assert.InDelta(t, 1.05, first.Speed, 0.001)
	assert.Equal(t, int64(2), first.DropFrames)
	assert.True(t, first.Running, "running=true mid-stream")
	assert.Equal(t, started, first.StartedAt)
	assert.False(t, first.UpdatedAt.IsZero(), "UpdatedAt set on each block")

	last := snapshots[1]
	assert.Equal(t, int64(240), last.Frame)
	assert.InDelta(t, 10.0, last.OutTimeSeconds, 0.001)
	assert.False(t, last.Running, "running=false after progress=end")
}

// "N/A" values appear early when ffmpeg hasn't built up stats yet. The parser
// should silently skip them rather than crash or zero out the field.
func TestProgressReader_HandlesNotAvailable(t *testing.T) {
	input := `frame=N/A
fps=N/A
bitrate=N/A
total_size=N/A
speed=N/A
progress=continue
`
	var got ProgressStats
	progressReader(strings.NewReader(input), time.Now(), func(apply func(*ProgressStats)) {
		apply(&got)
	})
	assert.Zero(t, got.Frame)
	assert.Zero(t, got.Speed)
	assert.Zero(t, got.Bitrate)
	assert.True(t, got.Running)
}

// `out_time_ms` is misnamed in older ffmpeg (it's microseconds). Both
// `out_time_us` and `out_time_ms` should produce the same result.
func TestProgressReader_OutTimeMsAlias(t *testing.T) {
	input := `out_time_ms=5000000
progress=continue
`
	var got ProgressStats
	progressReader(strings.NewReader(input), time.Now(), func(apply func(*ProgressStats)) {
		apply(&got)
	})
	assert.InDelta(t, 5.0, got.OutTimeSeconds, 0.001)
}
