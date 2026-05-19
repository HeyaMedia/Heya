package transcoder

import "strings"

type PlaybackAction string

const (
	ActionDirectPlay PlaybackAction = "direct_play"
	ActionRemux      PlaybackAction = "remux"
	ActionTranscode  PlaybackAction = "transcode"
)

type ClientCapabilities struct {
	SupportsHEVC bool
	SupportsFLAC bool
	SupportsMP4  bool
	SupportsMKV  bool
}

var DefaultClientCaps = ClientCapabilities{
	SupportsHEVC: false,
	SupportsFLAC: false,
	SupportsMP4:  true,
	SupportsMKV:  false,
}

type PlaybackPlan struct {
	Action  PlaybackAction
	Profile string
	Reason  string
}

type StreamInfo struct {
	CodecName string `json:"codec_name"`
	CodecType string `json:"codec_type"`
}

type MediaInfo struct {
	Container string       `json:"container"`
	Streams   []StreamInfo `json:"streams"`
}

func Decide(info *MediaInfo, caps ClientCapabilities) PlaybackPlan {
	if info == nil || len(info.Streams) == 0 {
		return PlaybackPlan{Action: ActionTranscode, Profile: "1080p", Reason: "no media info"}
	}

	container := strings.ToLower(info.Container)
	videoCodec := ""
	audioCodec := ""

	for _, s := range info.Streams {
		switch s.CodecType {
		case "video":
			if videoCodec == "" {
				videoCodec = strings.ToLower(s.CodecName)
			}
		case "audio":
			if audioCodec == "" {
				audioCodec = strings.ToLower(s.CodecName)
			}
		}
	}

	isMP4Container := containsAny(container, "mp4", "m4v", "mov")
	isMKVContainer := containsAny(container, "matroska", "mkv", "webm")
	isH264 := containsAny(videoCodec, "h264", "avc")
	isHEVC := containsAny(videoCodec, "hevc", "h265")
	isAAC := audioCodec == "aac"
	isFLAC := audioCodec == "flac"

	if isMP4Container && isH264 && isAAC {
		return PlaybackPlan{Action: ActionDirectPlay, Profile: "direct", Reason: "mp4/h264/aac"}
	}

	if isHEVC && caps.SupportsHEVC {
		if isMP4Container && isAAC {
			return PlaybackPlan{Action: ActionDirectPlay, Profile: "direct", Reason: "mp4/hevc/aac with client support"}
		}
	}

	if isMKVContainer && isH264 && isAAC {
		return PlaybackPlan{Action: ActionRemux, Profile: "remux", Reason: "mkv/h264/aac → remux to mp4"}
	}

	if isFLAC && !caps.SupportsFLAC {
		if isH264 || isHEVC {
			return PlaybackPlan{Action: ActionTranscode, Profile: "1080p", Reason: "flac audio needs transcode"}
		}
		return PlaybackPlan{Action: ActionTranscode, Profile: "audio", Reason: "flac → aac"}
	}

	if isHEVC && !caps.SupportsHEVC {
		return PlaybackPlan{Action: ActionTranscode, Profile: "1080p", Reason: "hevc → h264 transcode"}
	}

	if isH264 && isMP4Container {
		return PlaybackPlan{Action: ActionDirectPlay, Profile: "direct", Reason: "compatible format"}
	}

	return PlaybackPlan{Action: ActionTranscode, Profile: "1080p", Reason: "unsupported format"}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
