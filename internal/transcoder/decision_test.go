package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecideDirectPlayMP4H264AAC(t *testing.T) {
	info := &MediaInfo{
		Container: "mp4",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "h264"},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	plan := Decide(info, DefaultClientCaps)
	assert.Equal(t, ActionDirectPlay, plan.Action)
	assert.Equal(t, "direct", plan.Profile)
}

func TestDecideRemuxMKVH264AAC(t *testing.T) {
	info := &MediaInfo{
		Container: "matroska,webm",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "h264"},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	plan := Decide(info, DefaultClientCaps)
	assert.Equal(t, ActionRemux, plan.Action)
	assert.Equal(t, "remux", plan.Profile)
}

func TestDecideTranscodeHEVCNoSupport(t *testing.T) {
	info := &MediaInfo{
		Container: "matroska,webm",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "hevc"},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	plan := Decide(info, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
}

func TestDecideDirectPlayHEVCWithSupport(t *testing.T) {
	info := &MediaInfo{
		Container: "mp4",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "hevc"},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	caps := ClientCapabilities{SupportsHEVC: true, SupportsMP4: true}
	plan := Decide(info, caps)
	assert.Equal(t, ActionDirectPlay, plan.Action)
}

func TestDecideTranscodeFLAC(t *testing.T) {
	info := &MediaInfo{
		Container: "matroska,webm",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "h264"},
			{CodecType: "audio", CodecName: "flac"},
		},
	}
	plan := Decide(info, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
}

func TestDecideNilInfo(t *testing.T) {
	plan := Decide(nil, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
	assert.Equal(t, "1080p", plan.Profile)
}

func TestDecideEmptyStreams(t *testing.T) {
	info := &MediaInfo{Container: "mp4"}
	plan := Decide(info, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
}

func TestDecideM4VContainer(t *testing.T) {
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
