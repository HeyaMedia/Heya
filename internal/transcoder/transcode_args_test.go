package transcoder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The MPEG-TS delivery path must use the hls muxer, not the segment muxer:
// the segment muxer anchors cut boundaries to an absolute grid offset by
// -segment_start_number, which double-counts against -copyts timestamps on a
// seek head and packed everything from the seek point until
// start_number*SegmentDuration into one giant first segment (a real-world
// seek to 16:48 produced a single 356MB, 17-minute seg that took 3.5 minutes
// to encode while the player timed out and retried in a loop).
func TestBuildHLSArgs_TSPathUsesHLSMuxer(t *testing.T) {
	opts := TranscodeOpts{
		Input:        "/in.mkv",
		Profile:      Profile{Name: "1080p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 20},
		HWAccel:      BuildHwAccelConfig(HwAccelNone),
		StartTime:    1008,
		StartSegment: 168,
	}
	joined := strings.Join(BuildHLSArgs(opts, "/out"), " ")

	assert.Contains(t, joined, "-f hls ")
	assert.NotContains(t, joined, "-f segment")
	assert.Contains(t, joined, "-hls_segment_type mpegts")
	assert.Contains(t, joined, "-start_number 168")
	assert.Contains(t, joined, "temp_file")
	assert.Contains(t, joined, "/out/seg_%04d.ts")
	// Without forced keyframes the encoder's default GOP (~10s) decides the
	// real cut points while the playlist declares SegmentDuration everywhere.
	assert.Contains(t, joined, "-force_key_frames expr:gte(t,n_forced*6.0)")
}

// QSV and NVENC silently ignore the pict_type hint -force_key_frames plants
// unless the promote-to-IDR flag is passed, leaving segments on the encoder's
// default GOP despite the forced-keyframe expression.
func TestBuildHLSArgs_ForcedIDRPerEncoder(t *testing.T) {
	base := TranscodeOpts{
		Input:   "/in.mkv",
		Profile: Profile{Name: "1080p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 20},
	}

	qsv := base
	qsv.HWAccel = BuildHwAccelConfig(HwAccelQSV)
	assert.Contains(t, strings.Join(BuildHLSArgs(qsv, "/out"), " "), "-forced_idr 1")

	nvenc := base
	nvenc.HWAccel = BuildHwAccelConfig(HwAccelNVENC)
	assert.Contains(t, strings.Join(BuildHLSArgs(nvenc, "/out"), " "), "-forced-idr 1")

	none := base
	none.HWAccel = BuildHwAccelConfig(HwAccelNone)
	joined := strings.Join(BuildHLSArgs(none, "/out"), " ")
	assert.NotContains(t, joined, "forced_idr")
	assert.NotContains(t, joined, "forced-idr")
}

// Copy-video sessions have no encoder to force keyframes on; cuts land on
// source keyframes exactly as the RealSegmentBoundaries probe predicts.
func TestBuildHLSArgs_CopyVideoSkipsForcedKeyframes(t *testing.T) {
	opts := TranscodeOpts{
		Input:   "/in.mkv",
		Profile: Profile{Name: "remux", VideoCodec: "copy", AudioCodec: "copy"},
		HWAccel: BuildHwAccelConfig(HwAccelNone),
	}
	joined := strings.Join(BuildHLSArgs(opts, "/out"), " ")
	assert.NotContains(t, joined, "-force_key_frames")
	assert.Contains(t, joined, "-f hls ")
}
