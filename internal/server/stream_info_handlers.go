package server

import (
	"net/http"
	"strings"

	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/karbowiak/heya/internal/worker"
)

type playbackDecision struct {
	Action       string   `json:"action"`
	Profile      string   `json:"profile"`
	Reason       string   `json:"reason"`
	Reasons      []string `json:"reasons"` // human-readable reason tags (container, hdr, codec tag, ...)
	ReasonBits   uint32   `json:"reason_bits"`
	CopyVideo    bool     `json:"copy_video"`
	CopyAudio    bool     `json:"copy_audio"`
	NeedsToneMap bool     `json:"needs_tonemap"`
	NeedsFMP4    bool     `json:"needs_fmp4"`
	// Surgical fixes applied on top of the action. Useful in the UI to
	// explain WHY a "remux" or "transcode" is happening beyond the codec
	// summary in Reason.
	StripDoViEL     bool   `json:"strip_dovi_el,omitempty"`
	RetagHEVC       bool   `json:"retag_hevc,omitempty"`
	RetagDoVi       string `json:"retag_dovi,omitempty"`
	Deinterlace     bool   `json:"deinterlace,omitempty"`
	Rotate          int    `json:"rotate,omitempty"`
	FixAnamorphic   bool   `json:"fix_anamorphic,omitempty"`
	DownmixToStereo bool   `json:"downmix_stereo,omitempty"`
}

type qualityOption struct {
	Label  string `json:"label"`
	Height int    `json:"height"`
}

type streamInfoResponse struct {
	Container string           `json:"container"`
	Duration  float64          `json:"duration"`
	Size      int64            `json:"size"`
	BitRate   int64            `json:"bit_rate"`
	LibraryID int64            `json:"library_id"`
	Playback  playbackDecision `json:"playback"`
	Video     []videoStream    `json:"video"`
	Audio     []audioStream    `json:"audio"`
	Subtitle  []subStream      `json:"subtitle"`
	Qualities []qualityOption  `json:"qualities"`
}

type videoStream struct {
	Index          int    `json:"index"`
	Codec          string `json:"codec"`
	CodecLong      string `json:"codec_long"`
	Profile        string `json:"profile,omitempty"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	PixFmt         string `json:"pix_fmt,omitempty"`
	HDR            bool   `json:"hdr"`
	ColorTransfer  string `json:"color_transfer,omitempty"`
	ColorPrimaries string `json:"color_primaries,omitempty"`
	ColorSpace     string `json:"color_space,omitempty"`
	BitRate        string `json:"bit_rate,omitempty"`
	IsDefault      bool   `json:"is_default"`
}

type audioStream struct {
	Index         int    `json:"index"`
	Codec         string `json:"codec"`
	CodecLong     string `json:"codec_long"`
	Channels      int    `json:"channels"`
	ChannelLayout string `json:"channel_layout,omitempty"`
	SampleRate    string `json:"sample_rate,omitempty"`
	BitRate       string `json:"bit_rate,omitempty"`
	Language      string `json:"language"`
	Title         string `json:"title,omitempty"`
	IsDefault     bool   `json:"is_default"`
}

type subStream struct {
	Index             int    `json:"index"`
	Codec             string `json:"codec"`
	Language          string `json:"language"`
	Title             string `json:"title,omitempty"`
	IsDefault         bool   `json:"is_default"`
	IsForced          bool   `json:"is_forced"`
	IsHearingImpaired bool   `json:"is_hearing_impaired"`
	Delivery          string `json:"delivery"`
}

func parseClientCaps(r *http.Request) transcoder.ClientCapabilities {
	caps := transcoder.DefaultClientCaps
	q := r.URL.Query()
	if queryFlag(q.Get("supports_hevc")) {
		caps.SupportsHEVC = true
	}
	if queryFlag(q.Get("supports_av1")) {
		caps.SupportsAV1 = true
	}
	if queryFlag(q.Get("supports_flac")) {
		caps.SupportsFLAC = true
	}
	if queryFlag(q.Get("supports_opus")) {
		caps.SupportsOpus = true
	}
	if queryFlag(q.Get("supports_ac3")) {
		caps.SupportsAC3 = true
	}
	if queryFlag(q.Get("supports_eac3")) {
		caps.SupportsEAC3 = true
	}
	if queryFlag(q.Get("supports_mkv")) {
		caps.SupportsMKV = true
	}
	if queryFlag(q.Get("supports_webm")) {
		caps.SupportsWebM = true
	}
	if queryFlag(q.Get("supports_hdr")) {
		caps.SupportsHDR = true
	}
	if queryFlag(q.Get("supports_hdr10")) {
		caps.SupportsHDR10 = true
	}
	if queryFlag(q.Get("supports_hlg")) {
		caps.SupportsHLG = true
	}
	if queryFlag(q.Get("supports_dovi")) {
		caps.SupportsDoVi = true
	}
	if queryFlag(q.Get("supports_hevc_hev1")) {
		caps.SupportsHEVCHev1 = true
	}
	return caps
}

func queryFlag(v string) bool {
	return v == "1" || strings.EqualFold(v, "true")
}

func buildStreamInfoResponse(info worker.MediaInfo, caps transcoder.ClientCapabilities, filePath string, libraryID int64) streamInfoResponse {
	tInfo := workerToTranscoderInfo(&info)
	plan := transcoder.Decide(&tInfo, caps)

	if plan.Action == transcoder.ActionDirectPlay && vfs.IsSMBPath(filePath) {
		plan = transcoder.PlaybackPlan{Action: transcoder.ActionRemux, Profile: "remux", Reason: "remote file requires HLS delivery"}
	}

	sourceHeight := 0
	for _, s := range info.Streams {
		if s.CodecType == "video" && s.Height > 0 {
			sourceHeight = s.Height
			break
		}
	}

	var qualities []qualityOption
	if plan.Action != transcoder.ActionDirectPlay {
		ladder := transcoder.BuildBitrateLadder(sourceHeight)
		for _, q := range ladder {
			qualities = append(qualities, qualityOption{
				Label:  q.String(),
				Height: q.Height(),
			})
		}
	}

	resp := streamInfoResponse{
		Container: info.Container,
		Duration:  info.Duration,
		Size:      info.Size,
		BitRate:   info.BitRate,
		LibraryID: libraryID,
		Playback: playbackDecision{
			Action:          string(plan.Action),
			Profile:         plan.Profile,
			Reason:          plan.Reason,
			Reasons:         reasonStrings(plan.Reasons),
			ReasonBits:      uint32(plan.Reasons),
			CopyVideo:       plan.CopyVideo,
			CopyAudio:       plan.CopyAudio,
			NeedsToneMap:    plan.NeedsToneMap,
			NeedsFMP4:       plan.NeedsFMP4,
			StripDoViEL:     plan.StripDoViEL,
			RetagHEVC:       plan.RetagHEVC,
			RetagDoVi:       plan.RetagDoVi,
			Deinterlace:     plan.Deinterlace,
			Rotate:          plan.Rotate,
			FixAnamorphic:   plan.FixAnamorphic,
			DownmixToStereo: plan.DownmixToStereo,
		},
		Qualities: qualities,
	}

	for _, s := range info.Streams {
		isDefault := s.Disposition != nil && s.Disposition.Default == 1

		switch s.CodecType {
		case "video":
			resp.Video = append(resp.Video, videoStream{
				Index:          s.Index,
				Codec:          s.CodecName,
				CodecLong:      s.CodecLongName,
				Profile:        s.Profile,
				Width:          s.Width,
				Height:         s.Height,
				PixFmt:         s.PixFmt,
				HDR:            isHDR(s),
				ColorTransfer:  s.ColorTransfer,
				ColorPrimaries: s.ColorPrimaries,
				ColorSpace:     s.ColorSpace,
				BitRate:        s.BitRate,
				IsDefault:      isDefault,
			})

		case "audio":
			resp.Audio = append(resp.Audio, audioStream{
				Index:         s.Index,
				Codec:         s.CodecName,
				CodecLong:     s.CodecLongName,
				Channels:      s.Channels,
				ChannelLayout: s.ChannelLayout,
				SampleRate:    s.SampleRate,
				BitRate:       s.BitRate,
				Language:      s.Tags["language"],
				Title:         s.Tags["title"],
				IsDefault:     isDefault,
			})

		case "subtitle":
			isForced := s.Disposition != nil && s.Disposition.Forced == 1
			isHI := s.Disposition != nil && s.Disposition.HearingImpaired == 1
			resp.Subtitle = append(resp.Subtitle, subStream{
				Index:             s.Index,
				Codec:             s.CodecName,
				Language:          s.Tags["language"],
				Title:             s.Tags["title"],
				IsDefault:         isDefault,
				IsForced:          isForced,
				IsHearingImpaired: isHI,
				Delivery:          subtitleDeliveryString(transcoder.SubtitleDeliveryFor(s.CodecName)),
			})
		}
	}

	if resp.Video == nil {
		resp.Video = []videoStream{}
	}
	if resp.Audio == nil {
		resp.Audio = []audioStream{}
	}
	if resp.Subtitle == nil {
		resp.Subtitle = []subStream{}
	}

	return resp
}

func isHDR(s worker.StreamInfo) bool {
	switch s.ColorTransfer {
	case "smpte2084", "arib-std-b67":
		return true
	}
	return false
}

// reasonStrings expands a TranscodeReason bitmask into the individual reason
// tag names used by the UI. Order follows the bit definition order, which the
// UI relies on for stable rendering.
func reasonStrings(r transcoder.TranscodeReason) []string {
	if r == 0 {
		return []string{}
	}
	out := make([]string, 0, 4)
	type entry struct {
		bit  transcoder.TranscodeReason
		name string
	}
	for _, e := range []entry{
		{transcoder.ReasonContainerNotSupported, "container"},
		{transcoder.ReasonVideoCodecNotSupported, "video_codec"},
		{transcoder.ReasonAudioCodecNotSupported, "audio_codec"},
		{transcoder.ReasonVideoBitDepthNotSupported, "bit_depth"},
		{transcoder.ReasonHDRNotSupported, "hdr"},
		{transcoder.ReasonAudioChannelsNotSupported, "audio_channels"},
		{transcoder.ReasonQualityOverride, "quality_override"},
		{transcoder.ReasonVideoCodecTagNotSupported, "codec_tag"},
		{transcoder.ReasonVideoRotationNotSupported, "rotation"},
		{transcoder.ReasonInterlacedNotSupported, "interlaced"},
		{transcoder.ReasonAnamorphicNotSupported, "anamorphic"},
		{transcoder.ReasonAudioLosslessNotSupported, "lossless_audio"},
		{transcoder.ReasonDolbyVisionNotSupported, "dolby_vision"},
	} {
		if r.Has(e.bit) {
			out = append(out, e.name)
		}
	}
	return out
}
