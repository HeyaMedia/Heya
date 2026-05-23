package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Narrative edge-case tests for the decision logic.
// The bulk of (profile × media) → decision combinations are exercised by
// the data-driven matrix in decision_matrix_test.go. This file keeps
// targeted tests for edge cases that don't fit the matrix shape, plus a
// couple of historical regressions worth pinning explicitly.

// --- Degenerate input ---------------------------------------------------

func TestDecideNilInfo(t *testing.T) {
	plan := Decide(nil, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
	assert.Equal(t, "1080p", plan.Profile)
}

func TestDecideEmptyStreams(t *testing.T) {
	plan := Decide(&MediaInfo{Container: "mp4"}, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
}

func TestDecideForHLSNilInfo(t *testing.T) {
	plan := DecideForHLS(nil, 0, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
	assert.Equal(t, "1080p", plan.Profile)
}

// --- Profile-name resolution -------------------------------------------

func TestDecide_PicksTranscodeProfileByHeight(t *testing.T) {
	cases := []struct {
		height      int
		wantProfile string
	}{
		{2160, "2160p"},
		{1440, "1440p"},
		{1080, "1080p"},
		{720, "720p"},
		{480, "480p"},
		{360, "1080p"}, // sub-480p falls back to 1080p (intentional baseline)
	}
	for _, tc := range cases {
		t.Run(tc.wantProfile, func(t *testing.T) {
			info := &MediaInfo{
				Container: "matroska,webm",
				Streams: []StreamInfo{
					{CodecType: "video", CodecName: "hevc", Height: tc.height},
					{CodecType: "audio", CodecName: "aac"},
				},
			}
			plan := Decide(info, DefaultClientCaps)
			assert.Equal(t, ActionTranscode, plan.Action)
			assert.Equal(t, tc.wantProfile, plan.Profile)
		})
	}
}

// --- Container detection regressions -----------------------------------

// MKV files report container "matroska,webm" via ffprobe. The decision logic
// must treat them as MKV, not WebM, even when the client supports WebM.
// (Regression: see commit history around plain MKV remux.)
func TestDecide_MKVContainerNotTreatedAsWebM(t *testing.T) {
	info := &MediaInfo{
		Container: "matroska,webm",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "h264"},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	caps := ClientCapabilities{SupportsWebM: true, SupportsMP4: true}
	plan := Decide(info, caps)
	assert.Equal(t, ActionRemux, plan.Action, "MKV should remux, not direct-play via WebM match")
	assert.True(t, plan.Reasons.Has(ReasonContainerNotSupported))
}

// .m4v should be treated as MP4 for direct play.
func TestDecide_M4VAcceptedAsMP4(t *testing.T) {
	info := &MediaInfo{
		Container: "m4v",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "h264"},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	plan := Decide(info, DefaultClientCaps)
	assert.Equal(t, ActionDirectPlay, plan.Action)
}

// --- Audio codec helpers ----------------------------------------------

func TestAudioCanCopyToTS(t *testing.T) {
	allCaps := ClientCapabilities{SupportsAC3: true, SupportsEAC3: true}
	noCaps := DefaultClientCaps

	// Universal codecs (no caps required)
	assert.True(t, AudioCanCopyToTS("aac", noCaps))
	assert.True(t, AudioCanCopyToTS("mp3", noCaps))
	assert.True(t, AudioCanCopyToTS("mp2", noCaps))

	// AC3/EAC3 gated on caps
	assert.True(t, AudioCanCopyToTS("ac3", allCaps))
	assert.False(t, AudioCanCopyToTS("ac3", noCaps))
	assert.True(t, AudioCanCopyToTS("eac3", allCaps))
	assert.False(t, AudioCanCopyToTS("eac3", noCaps))

	// Opus/FLAC/Vorbis: never go into MPEG-TS
	assert.False(t, AudioCanCopyToTS("opus", allCaps))
	assert.False(t, AudioCanCopyToTS("flac", allCaps))
	assert.False(t, AudioCanCopyToTS("vorbis", allCaps))
}

func TestAudioCanCopyToFMP4(t *testing.T) {
	full := ClientCapabilities{
		SupportsAC3: true, SupportsEAC3: true,
		SupportsFLAC: true, SupportsOpus: true,
	}
	bare := DefaultClientCaps

	assert.True(t, AudioCanCopyToFMP4("aac", bare))
	assert.True(t, AudioCanCopyToFMP4("mp3", bare))

	assert.True(t, AudioCanCopyToFMP4("ac3", full))
	assert.True(t, AudioCanCopyToFMP4("eac3", full))
	assert.True(t, AudioCanCopyToFMP4("opus", full))
	assert.True(t, AudioCanCopyToFMP4("flac", full))

	assert.False(t, AudioCanCopyToFMP4("ac3", bare))
	assert.False(t, AudioCanCopyToFMP4("eac3", bare))
	assert.False(t, AudioCanCopyToFMP4("opus", bare))
	assert.False(t, AudioCanCopyToFMP4("flac", bare))
	assert.False(t, AudioCanCopyToFMP4("dts", full)) // never copyable
}

// --- VideoCanCopyToTS / VideoNeedsFMP4 --------------------------------

func TestVideoCanCopyToTS(t *testing.T) {
	// 8-bit H.264 baseline — OK
	assert.True(t, VideoCanCopyToTS(&StreamInfo{CodecName: "h264", PixFmt: "yuv420p"}))

	// 10-bit H.264 (Hi10P) — NOT OK in TS
	assert.False(t, VideoCanCopyToTS(&StreamInfo{CodecName: "h264", Profile: "High 10", PixFmt: "yuv420p10le"}))

	// 4:2:2 H.264 — NOT OK
	assert.False(t, VideoCanCopyToTS(&StreamInfo{CodecName: "h264", Profile: "High 4:2:2"}))

	// HEVC — wrong codec for our TS copy path
	assert.False(t, VideoCanCopyToTS(&StreamInfo{CodecName: "hevc", PixFmt: "yuv420p"}))

	// HDR H.264 — NOT OK
	assert.False(t, VideoCanCopyToTS(&StreamInfo{CodecName: "h264", PixFmt: "yuv420p", ColorTransfer: "smpte2084"}))
}

func TestVideoNeedsFMP4(t *testing.T) {
	assert.True(t, VideoNeedsFMP4("av1"))
	assert.True(t, VideoNeedsFMP4("av01"))
	assert.True(t, VideoNeedsFMP4("vp9"))
	assert.True(t, VideoNeedsFMP4("vp09"))
	assert.False(t, VideoNeedsFMP4("h264"))
	assert.False(t, VideoNeedsFMP4("hevc"))
}
