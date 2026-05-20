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
			{CodecType: "video", CodecName: "hevc", Height: 1080},
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
			{CodecType: "video", CodecName: "h264", Height: 1080},
			{CodecType: "audio", CodecName: "flac"},
		},
	}
	plan := Decide(info, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
	assert.True(t, plan.CopyVideo, "video should be copied since h264 is compatible")
	assert.False(t, plan.CopyAudio, "flac audio should be transcoded")
	assert.Contains(t, plan.Reason, "copy video")
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

func TestDecideAV1DirectPlay(t *testing.T) {
	info := &MediaInfo{
		Container: "mp4",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "av1"},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	caps := ClientCapabilities{SupportsAV1: true, SupportsMP4: true}
	plan := Decide(info, caps)
	assert.Equal(t, ActionDirectPlay, plan.Action)
}

func TestDecideAV1NoSupport(t *testing.T) {
	info := &MediaInfo{
		Container: "mp4",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "av1", Height: 2160},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	plan := Decide(info, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
	assert.Equal(t, "2160p", plan.Profile)
}

func TestDecideVP9WebMDirectPlay(t *testing.T) {
	info := &MediaInfo{
		Container: "webm",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "vp9"},
			{CodecType: "audio", CodecName: "opus"},
		},
	}
	caps := ClientCapabilities{SupportsWebM: true, SupportsOpus: true}
	plan := Decide(info, caps)
	assert.Equal(t, ActionDirectPlay, plan.Action)
}

func TestDecideAudioOnlyTranscode(t *testing.T) {
	info := &MediaInfo{
		Container: "mp4",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "h264", Height: 1080},
			{CodecType: "audio", CodecName: "flac"},
		},
	}
	plan := Decide(info, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
	assert.True(t, plan.CopyVideo)
	assert.False(t, plan.CopyAudio)
}

func TestDecideVideoOnlyTranscode(t *testing.T) {
	info := &MediaInfo{
		Container: "matroska,webm",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "hevc", Height: 1080},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	plan := Decide(info, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
	assert.False(t, plan.CopyVideo, "hevc should be transcoded without support")
	assert.True(t, plan.CopyAudio, "aac should be copied")
	assert.Contains(t, plan.Reason, "copy audio")
}

func TestDecide4KProfile(t *testing.T) {
	info := &MediaInfo{
		Container: "matroska,webm",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "hevc", Height: 2160},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	plan := Decide(info, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
	assert.Equal(t, "2160p", plan.Profile)
}

func TestDecide720pProfile(t *testing.T) {
	info := &MediaInfo{
		Container: "matroska,webm",
		Streams: []StreamInfo{
			{CodecType: "video", CodecName: "hevc", Height: 720},
			{CodecType: "audio", CodecName: "aac"},
		},
	}
	plan := Decide(info, DefaultClientCaps)
	assert.Equal(t, ActionTranscode, plan.Action)
	assert.Equal(t, "720p", plan.Profile)
}
