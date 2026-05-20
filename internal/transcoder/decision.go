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
	SupportsAV1  bool
	SupportsOpus bool
	SupportsMP4  bool
	SupportsMKV  bool
	SupportsWebM bool
}

var DefaultClientCaps = ClientCapabilities{
	SupportsHEVC: false,
	SupportsFLAC: false,
	SupportsAV1:  false,
	SupportsOpus: false,
	SupportsMP4:  true,
	SupportsMKV:  false,
	SupportsWebM: false,
}

type PlaybackPlan struct {
	Action     PlaybackAction
	Profile    string
	Reason     string
	CopyVideo  bool
	CopyAudio  bool
}

type StreamInfo struct {
	CodecName string `json:"codec_name"`
	CodecType string `json:"codec_type"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
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
	videoHeight := 0

	for _, s := range info.Streams {
		switch s.CodecType {
		case "video":
			if videoCodec == "" {
				videoCodec = strings.ToLower(s.CodecName)
				videoHeight = s.Height
			}
		case "audio":
			if audioCodec == "" {
				audioCodec = strings.ToLower(s.CodecName)
			}
		}
	}

	isMP4Container := containsAny(container, "mp4", "m4v", "mov")
	isMKVContainer := containsAny(container, "matroska", "mkv", "webm")
	isWebMContainer := containsAny(container, "webm")

	isH264 := containsAny(videoCodec, "h264", "avc")
	isHEVC := containsAny(videoCodec, "hevc", "h265")
	isAV1 := containsAny(videoCodec, "av1", "av01")
	isVP9 := containsAny(videoCodec, "vp9", "vp09")

	isAAC := audioCodec == "aac"
	isFLAC := audioCodec == "flac"
	isOpus := audioCodec == "opus"
	isAC3 := containsAny(audioCodec, "ac3", "eac3")

	videoCompatible := (isH264) ||
		(isHEVC && caps.SupportsHEVC) ||
		(isAV1 && caps.SupportsAV1) ||
		(isVP9 && caps.SupportsWebM)

	audioCompatible := isAAC ||
		(isFLAC && caps.SupportsFLAC) ||
		(isOpus && caps.SupportsOpus) ||
		isAC3

	containerCompatible := (isMP4Container && caps.SupportsMP4) ||
		(isMKVContainer && caps.SupportsMKV) ||
		(isWebMContainer && caps.SupportsWebM)

	if videoCompatible && audioCompatible && containerCompatible {
		return PlaybackPlan{Action: ActionDirectPlay, Profile: "direct", Reason: "fully compatible", CopyVideo: true, CopyAudio: true}
	}

	if videoCompatible && audioCompatible && !containerCompatible {
		if (isMKVContainer || isWebMContainer) && caps.SupportsMP4 {
			return PlaybackPlan{Action: ActionRemux, Profile: "remux", Reason: "remux to mp4 container", CopyVideo: true, CopyAudio: true}
		}
	}

	if videoCompatible && !audioCompatible {
		return PlaybackPlan{
			Action:    ActionTranscode,
			Profile:   profileForHeight(videoHeight),
			Reason:    "copy video, transcode audio (" + audioCodec + " → aac)",
			CopyVideo: true,
			CopyAudio: false,
		}
	}

	if !videoCompatible && audioCompatible {
		return PlaybackPlan{
			Action:    ActionTranscode,
			Profile:   profileForHeight(videoHeight),
			Reason:    "transcode video (" + videoCodec + " → h264), copy audio",
			CopyVideo: false,
			CopyAudio: true,
		}
	}

	return PlaybackPlan{
		Action:    ActionTranscode,
		Profile:   profileForHeight(videoHeight),
		Reason:    "transcode video (" + videoCodec + " → h264) + audio (" + audioCodec + " → aac)",
		CopyVideo: false,
		CopyAudio: false,
	}
}

func profileForHeight(height int) string {
	switch {
	case height >= 2160:
		return "2160p"
	case height >= 1440:
		return "1440p"
	case height >= 1080:
		return "1080p"
	case height >= 720:
		return "720p"
	case height >= 480:
		return "480p"
	default:
		return "1080p"
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
