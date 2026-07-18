package transcoder

import "strings"

type PlaybackAction string

const (
	ActionDirectPlay PlaybackAction = "direct_play"
	ActionRemux      PlaybackAction = "remux"
	ActionTranscode  PlaybackAction = "transcode"
)

type ClientCapabilities struct {
	SupportsHEVC     bool
	SupportsHEVCHev1 bool // some Chromecast/Android clients accept hev1; Safari does not
	SupportsFLAC     bool
	SupportsAV1      bool
	SupportsOpus     bool
	SupportsAC3      bool
	SupportsEAC3     bool
	SupportsMP4      bool
	SupportsMKV      bool
	SupportsWebM     bool
	SupportsHDR      bool // generic HDR catch-all (true => supports HDR10 + HLG + DoVi)
	SupportsHDR10    bool
	SupportsHLG      bool
	SupportsDoVi     bool
}

var DefaultClientCaps = ClientCapabilities{
	SupportsMP4: true,
}

// TranscodeReason is a bitmask of the specific reasons a media file cannot be
// played as-is and needs remuxing or transcoding. Multiple reasons can apply
// simultaneously (e.g. HDR HEVC in MKV: container + video range + maybe codec).
//
// Tests assert against this bitmask exactly. The prose `Reason` string remains
// for UI display.
type TranscodeReason uint32

const (
	// ReasonContainerNotSupported — the file's container can't be played
	// directly by the client (e.g. MKV in any browser).
	ReasonContainerNotSupported TranscodeReason = 1 << iota
	// ReasonVideoCodecNotSupported — the video codec isn't supported by the
	// client (e.g. AV1 without browser AV1 support).
	ReasonVideoCodecNotSupported
	// ReasonAudioCodecNotSupported — the audio codec isn't supported by the
	// client in the chosen container (e.g. EAC3 in Firefox).
	ReasonAudioCodecNotSupported
	// ReasonVideoBitDepthNotSupported — bit depth incompatible with the
	// target container (e.g. 10-bit H.264 in MPEG-TS).
	ReasonVideoBitDepthNotSupported
	// ReasonHDRNotSupported — source is HDR but client is SDR; tone map needed.
	ReasonHDRNotSupported
	// ReasonAudioChannelsNotSupported — surround layout the client can't
	// decode; downmix needed.
	ReasonAudioChannelsNotSupported
	// ReasonQualityOverride — user explicitly picked a quality below source,
	// forcing transcode regardless of native compatibility.
	ReasonQualityOverride
	// ReasonVideoCodecTagNotSupported — codec is supported but the codec tag
	// (e.g. HEVC `hev1` vs Safari-required `hvc1`) is not. Only remux is needed.
	ReasonVideoCodecTagNotSupported
	// ReasonVideoRotationNotSupported — non-zero Display Matrix rotation that
	// browsers won't apply in MSE. Forces video transcode with transpose filter.
	ReasonVideoRotationNotSupported
	// ReasonInterlacedNotSupported — interlaced field order. Browsers cannot
	// deinterlace in MSE, so we yadif/vpp the source.
	ReasonInterlacedNotSupported
	// ReasonAnamorphicNotSupported — sample aspect ratio is not 1:1. Browsers
	// ignore PAR in MSE; we need to scale to correct DAR.
	ReasonAnamorphicNotSupported
	// ReasonAudioLosslessNotSupported — lossless format (TrueHD, DTS, DTS-HD,
	// MLP, PCM) that no browser MSE can decode. Always transcode to AAC.
	ReasonAudioLosslessNotSupported
	// ReasonDolbyVisionNotSupported — DV profile not playable by client. Profile
	// 8 with HDR10 BL compat can remux+strip; other profiles must transcode.
	ReasonDolbyVisionNotSupported
)

var reasonNames = []struct {
	r    TranscodeReason
	name string
}{
	{ReasonContainerNotSupported, "container"},
	{ReasonVideoCodecNotSupported, "video codec"},
	{ReasonAudioCodecNotSupported, "audio codec"},
	{ReasonVideoBitDepthNotSupported, "bit depth"},
	{ReasonHDRNotSupported, "HDR tone map"},
	{ReasonAudioChannelsNotSupported, "audio channels"},
	{ReasonQualityOverride, "quality override"},
	{ReasonVideoCodecTagNotSupported, "codec tag"},
	{ReasonVideoRotationNotSupported, "rotation"},
	{ReasonInterlacedNotSupported, "interlaced"},
	{ReasonAnamorphicNotSupported, "anamorphic"},
	{ReasonAudioLosslessNotSupported, "lossless audio"},
	{ReasonDolbyVisionNotSupported, "dolby vision"},
}

// String returns a comma-separated list of reason names for display.
func (r TranscodeReason) String() string {
	if r == 0 {
		return ""
	}
	var parts []string
	for _, entry := range reasonNames {
		if r&entry.r != 0 {
			parts = append(parts, entry.name)
		}
	}
	return strings.Join(parts, ", ")
}

// Has reports whether the bitmask includes the given reason.
func (r TranscodeReason) Has(reason TranscodeReason) bool {
	return r&reason != 0
}

type PlaybackPlan struct {
	Action       PlaybackAction
	Profile      string
	Reason       string          // prose, for UI display
	Reasons      TranscodeReason // bitmask, for assertions/logic
	CopyVideo    bool
	CopyAudio    bool
	NeedsToneMap bool
	NeedsFMP4    bool

	// Surgical flags used by the ffmpeg arg builder. None of these change the
	// Action by themselves — they describe the side effects applied on top of
	// remux/transcode that the standard codec/container decision picks.
	StripDoViEL     bool   // remove DV enhancement layer + RPU to play as HDR10 base layer
	RetagHEVC       bool   // -tag:v hvc1 for Safari (when copying HEVC)
	RetagDoVi       string // profile-correct Dolby Vision tag (dvh1 for P5, hvc1 for P7/P8)
	Deinterlace     bool   // apply yadif (or hw equivalent)
	Rotate          int    // 0/90/180/270 — transpose filter degrees CW
	FixAnamorphic   bool   // setsar=1:1 + correct scale
	DownmixToStereo bool   // multi-channel AAC transcode → stereo
}

type StreamInfo struct {
	CodecName      string `json:"codec_name"`
	CodecType      string `json:"codec_type"`
	Profile        string `json:"profile,omitempty"`
	PixFmt         string `json:"pix_fmt,omitempty"`
	Width          int    `json:"width,omitempty"`
	Height         int    `json:"height,omitempty"`
	ColorTransfer  string `json:"color_transfer,omitempty"`
	ColorPrimaries string `json:"color_primaries,omitempty"`
	ColorSpace     string `json:"color_space,omitempty"`

	// Fields derived from ffprobe output during the worker → transcoder
	// translation step. Empty/zero values mean "unknown / not detected" and
	// preserve pre-expansion behaviour.
	CodecTag          string `json:"codec_tag,omitempty"`      // hvc1 / hev1
	BitDepth          int    `json:"bit_depth,omitempty"`      // 8 / 10 / 12
	SampleAspectRatio string `json:"sar,omitempty"`            // "1:1" or "8:9"
	FieldOrder        string `json:"field_order,omitempty"`    // progressive / tt / bb / tb / bt
	Rotation          int    `json:"rotation,omitempty"`       // 0 / 90 / 180 / 270 (positive CW)
	DvProfile         int    `json:"dv_profile,omitempty"`     // 0 = none, 5/7/8/10 = DV
	DvBlCompatID      int    `json:"dv_bl_compat,omitempty"`   // 0 = DV-only, 1 = HDR10 BL, 4 = HLG BL
	Channels          int    `json:"channels,omitempty"`       // audio channel count
	ChannelLayout     string `json:"channel_layout,omitempty"` // "5.1", "7.1", "stereo"
}

type MediaInfo struct {
	Container string       `json:"container"`
	Streams   []StreamInfo `json:"streams"`
}

func IsHDRStream(s *StreamInfo) bool {
	ct := strings.ToLower(s.ColorTransfer)
	return ct == "smpte2084" || ct == "arib-std-b67"
}

// IsDolbyVision reports whether a stream carries a Dolby Vision configuration
// record (any profile). Profile 0 means "no DV detected".
func IsDolbyVision(s *StreamInfo) bool {
	if s == nil {
		return false
	}
	return s.DvProfile > 0
}

// IsDoViHDR10Compatible reports whether a DV stream has an HDR10 base layer
// that can be played by HDR10-capable clients after stripping the EL/RPU.
// Per Dolby's spec: profile 8 + BL compat ID 1 = HDR10 BL. Profile 7 (TrueHD-EL),
// profile 5 (DV-only), and other variants don't have a usable base layer.
func IsDoViHDR10Compatible(s *StreamInfo) bool {
	if s == nil {
		return false
	}
	return s.DvProfile == 8 && s.DvBlCompatID == 1
}

// IsLosslessAudio reports whether a codec is in the "lossless / object-based"
// family that no browser MSE pipeline can decode. These must always go
// through an AAC transcode.
func IsLosslessAudio(codec string) bool {
	switch strings.ToLower(codec) {
	case "truehd", "mlp",
		"dts", "dca", "dts-hd", "dtshd", "dts_hd",
		"pcm_s16le", "pcm_s16be", "pcm_s24le", "pcm_s24be", "pcm_s32le", "pcm_s32be",
		"pcm_f32le", "pcm_f32be",
		"pcm_dvd", "pcm_bluray":
		return true
	}
	return false
}

// IsInterlaced reports whether the stream's field_order indicates an interlaced
// source. Empty/"progressive"/"unknown" all read as not-interlaced.
func IsInterlaced(s *StreamInfo) bool {
	if s == nil {
		return false
	}
	fo := strings.ToLower(s.FieldOrder)
	return fo != "" && fo != "progressive" && fo != "unknown"
}

// IsAnamorphic reports whether the sample aspect ratio implies non-square
// pixels. 0:1 / 0:0 / 1:1 / empty are all square.
func IsAnamorphic(sar string) bool {
	sar = strings.TrimSpace(sar)
	switch sar {
	case "", "0:0", "0:1", "1:1":
		return false
	}
	return true
}

// IsRotated reports whether the stream has a non-zero rotation.
func IsRotated(s *StreamInfo) bool {
	return s != nil && s.Rotation != 0
}

// SubtitleDelivery captures how a subtitle stream can be served to the
// client. Text-based formats (srt/ass/webvtt/mov_text) can be extracted and
// served as a separate file. Bitmap formats (PGS / dvb / dvd_subtitle) have
// no text representation — they must be burned into the video stream during
// transcoding. Anything else is unsupported.
type SubtitleDelivery int

const (
	SubDeliveryUnsupported SubtitleDelivery = iota
	SubDeliveryExternal
	SubDeliveryBurnIn
)

// SubtitleDeliveryFor classifies a subtitle codec by how it can be delivered.
func SubtitleDeliveryFor(codec string) SubtitleDelivery {
	switch strings.ToLower(codec) {
	case "subrip", "srt", "webvtt", "vtt", "ass", "ssa", "mov_text", "text":
		return SubDeliveryExternal
	case "pgs", "hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle", "dvbsub":
		return SubDeliveryBurnIn
	}
	return SubDeliveryUnsupported
}

// IsHEVCHev1Tag reports whether a stream is HEVC tagged as `hev1` (instead of
// the Safari-required `hvc1`). Empty tags read as "unknown" → false.
func IsHEVCHev1Tag(s *StreamInfo) bool {
	if s == nil {
		return false
	}
	codec := strings.ToLower(s.CodecName)
	if !containsAny(codec, "hevc", "h265") {
		return false
	}
	return strings.ToLower(s.CodecTag) == "hev1"
}

// clientSupportsDoVi reports whether the client can directly play Dolby Vision.
// DoVi requires a dedicated decoder/license, so the generic SupportsHDR flag
// does NOT imply support — only the explicit DoVi capability counts.
func clientSupportsDoVi(caps ClientCapabilities) bool {
	return caps.SupportsDoVi
}

func Decide(info *MediaInfo, caps ClientCapabilities) PlaybackPlan {
	if info == nil || len(info.Streams) == 0 {
		return PlaybackPlan{Action: ActionTranscode, Profile: "1080p", Reason: "no media info"}
	}

	container := strings.ToLower(info.Container)
	var video *StreamInfo
	var audio *StreamInfo

	for i := range info.Streams {
		switch info.Streams[i].CodecType {
		case "video":
			if video == nil {
				video = &info.Streams[i]
			}
		case "audio":
			if audio == nil {
				audio = &info.Streams[i]
			}
		}
	}

	videoCodec := ""
	audioCodec := ""
	videoHeight := 0
	if video != nil {
		videoCodec = strings.ToLower(video.CodecName)
		videoHeight = video.Height
	}
	if audio != nil {
		audioCodec = strings.ToLower(audio.CodecName)
	}

	isMP4Container := containsAny(container, "mp4", "m4v", "mov")
	isMKVContainer := containsAny(container, "matroska", "mkv")
	isWebMContainer := containsAny(container, "webm") && !containsAny(container, "matroska")

	isH264 := containsAny(videoCodec, "h264", "avc")
	isHEVC := containsAny(videoCodec, "hevc", "h265")
	isAV1 := containsAny(videoCodec, "av1", "av01")
	isVP9 := containsAny(videoCodec, "vp9", "vp09")

	isAAC := audioCodec == "aac"
	isFLAC := audioCodec == "flac"
	isOpus := audioCodec == "opus"
	isAC3 := audioCodec == "ac3" || audioCodec == "ac-3"
	isEAC3 := audioCodec == "eac3" || audioCodec == "ec-3"

	needsToneMap := needsToneMapFor(video, caps)
	isInterlaced := IsInterlaced(video)
	isRotated := IsRotated(video)
	isAnamorphic := video != nil && IsAnamorphic(video.SampleAspectRatio)
	isLossless := IsLosslessAudio(audioCodec)
	isDoVi := IsDolbyVision(video)
	dvHDR10Compat := IsDoViHDR10Compatible(video)
	dvNeedsHandling := isDoVi && !clientSupportsDoVi(caps)
	// DV stripping is always possible when the source carries an HDR10 base
	// layer (profile 8 + compat ID 1). We deliver HDR10 as-is; if the client's
	// display can't render HDR10 it falls back to clipped colors at the OS
	// level, which is better than a black screen from DV-decode failure.
	canStripDVtoHDR10 := dvNeedsHandling && dvHDR10Compat
	hevcRetagNeeded := caps.SupportsHEVC && !caps.SupportsHEVCHev1 && IsHEVCHev1Tag(video)
	doviRetag := requiredDoViTag(video, caps)

	baseVideoCompat := (isH264) ||
		(isHEVC && caps.SupportsHEVC) ||
		(isAV1 && caps.SupportsAV1) ||
		(isVP9 && caps.SupportsWebM)

	videoCompatible := baseVideoCompat
	if needsToneMap {
		videoCompatible = false
	}
	if isInterlaced || isRotated || isAnamorphic {
		videoCompatible = false
	}
	if dvNeedsHandling && !canStripDVtoHDR10 {
		videoCompatible = false
	}

	audioCompatible := !isLossless && (isAAC ||
		(isFLAC && caps.SupportsFLAC) ||
		(isOpus && caps.SupportsOpus) ||
		(isAC3 && caps.SupportsAC3) ||
		(isEAC3 && caps.SupportsEAC3))

	containerCompatible := (isMP4Container && caps.SupportsMP4) ||
		(isMKVContainer && caps.SupportsMKV) ||
		(isWebMContainer && caps.SupportsWebM)

	// Aggregate the specific incompatibility reasons.
	var reasons TranscodeReason
	if !containerCompatible {
		reasons |= ReasonContainerNotSupported
	}
	if !videoCompatible {
		switch {
		case dvNeedsHandling && !canStripDVtoHDR10:
			reasons |= ReasonDolbyVisionNotSupported
			if needsToneMap {
				reasons |= ReasonHDRNotSupported
			}
		case needsToneMap:
			reasons |= ReasonHDRNotSupported
		case isInterlaced:
			reasons |= ReasonInterlacedNotSupported
		case isRotated:
			reasons |= ReasonVideoRotationNotSupported
		case isAnamorphic:
			reasons |= ReasonAnamorphicNotSupported
		default:
			reasons |= ReasonVideoCodecNotSupported
		}
	}
	if !audioCompatible {
		if isLossless {
			reasons |= ReasonAudioLosslessNotSupported
		} else {
			reasons |= ReasonAudioCodecNotSupported
		}
	}
	// Surgical-fix reasons: video stays copy-able but we mark the underlying
	// reason so UIs can explain "remuxed because of DV strip / Safari tag".
	if videoCompatible {
		if hevcRetagNeeded {
			reasons |= ReasonVideoCodecTagNotSupported
		}
		if doviRetag != "" {
			reasons |= ReasonVideoCodecTagNotSupported
		}
		if canStripDVtoHDR10 {
			reasons |= ReasonDolbyVisionNotSupported
		}
	}

	needsSurgicalRemux := videoCompatible && (hevcRetagNeeded || doviRetag != "" || canStripDVtoHDR10)
	// HEVC browser playback is carried in fragmented MP4. MPEG-TS support is
	// not implied by MediaSource accepting HEVC in MP4, and TS cannot preserve
	// the Dolby Vision configuration record needed by Apple clients.
	fmp4 := isHEVC || isAV1 || isVP9

	// Build the surgical-flag bundle that flows into ffmpeg arg construction.
	plan := PlaybackPlan{
		Profile:       profileForHeight(videoHeight),
		Reasons:       reasons,
		Deinterlace:   isInterlaced,
		FixAnamorphic: isAnamorphic,
	}
	if isRotated && video != nil {
		plan.Rotate = video.Rotation
	}
	if needsSurgicalRemux {
		plan.RetagHEVC = hevcRetagNeeded
		plan.RetagDoVi = doviRetag
		plan.StripDoViEL = canStripDVtoHDR10
	}

	if videoCompatible && audioCompatible && containerCompatible && !needsSurgicalRemux {
		plan.Action = ActionDirectPlay
		plan.Profile = "direct"
		plan.Reason = "fully compatible"
		plan.CopyVideo = true
		plan.CopyAudio = true
		return plan
	}

	if videoCompatible && audioCompatible {
		if needsSurgicalRemux || ((isMKVContainer || isWebMContainer) && caps.SupportsMP4) {
			plan.Action = ActionRemux
			plan.Profile = "remux"
			plan.Reason = "remux to mp4 container"
			plan.CopyVideo = true
			plan.CopyAudio = true
			plan.NeedsFMP4 = fmp4
			return plan
		}
	}

	if videoCompatible && !audioCompatible {
		// Video copies; only audio re-encodes. We label this as Remux because
		// the heavy work (video transcode) isn't happening — same convention
		// DecideForHLS uses. Reasons still convey the underlying audio issue.
		plan.Action = ActionRemux
		plan.Profile = "remux"
		plan.Reason = "remux + transcode audio (" + audioCodec + " → aac)"
		plan.CopyVideo = true
		plan.CopyAudio = false
		plan.NeedsFMP4 = fmp4
		plan.DownmixToStereo = true
		return plan
	}

	// When the video path forces transcoding (whether due to HDR-not-renderable,
	// DV-not-strippable, interlace, rotation, anamorphic, or codec mismatch) and
	// the source is HDR, the tone-map filter is required regardless of whether
	// the *client* could have rendered HDR — our transcode profiles target SDR
	// h264, so the colors have to come down.
	sourceIsHDR := video != nil && (IsHDRStream(video) || IsDolbyVision(video))
	reason := "transcode video (" + videoCodec + " → h264)"
	if sourceIsHDR {
		reason += " + HDR→SDR tone map"
		plan.NeedsToneMap = true
	}

	if !videoCompatible && audioCompatible {
		plan.Action = ActionTranscode
		plan.Reason = reason + ", copy audio"
		plan.CopyVideo = false
		plan.CopyAudio = true
		return plan
	}

	plan.Action = ActionTranscode
	plan.Reason = reason + " + audio (" + audioCodec + " → aac)"
	plan.CopyVideo = false
	plan.CopyAudio = false
	plan.DownmixToStereo = true
	return plan
}

// requiredDoViTag returns the MP4 sample-entry tag required for direct Dolby
// Vision playback, or "" when the source is already correctly tagged (or the
// client cannot play Dolby Vision). Profile 5 uses dvh1; profiles 7/8 use hvc1.
func requiredDoViTag(s *StreamInfo, caps ClientCapabilities) string {
	if s == nil || !IsDolbyVision(s) || !clientSupportsDoVi(caps) {
		return ""
	}
	want := "hvc1"
	if s.DvProfile == 5 {
		want = "dvh1"
	}
	if strings.EqualFold(s.CodecTag, want) {
		return ""
	}
	return want
}

// needsToneMapFor reports whether an HDR source needs to be tone-mapped to
// SDR for the client. Honours the generic SupportsHDR catch-all plus the
// transfer-function-specific HDR10/HLG/DoVi flags.
func needsToneMapFor(s *StreamInfo, caps ClientCapabilities) bool {
	if s == nil || !IsHDRStream(s) {
		return false
	}
	if caps.SupportsHDR {
		return false
	}
	ct := strings.ToLower(s.ColorTransfer)
	if ct == "smpte2084" && (caps.SupportsHDR10 || (caps.SupportsDoVi && IsDolbyVision(s))) {
		return false
	}
	if ct == "arib-std-b67" && caps.SupportsHLG {
		return false
	}
	return true
}

// DecideForHLS determines copy/transcode flags and segment format for HLS delivery.
// It uses actual client capabilities to decide whether the video codec can be
// copied (remuxed) or must be transcoded, and whether fMP4 or MPEG-TS segments
// are needed.
func DecideForHLS(info *MediaInfo, audioStreamIdx int, caps ClientCapabilities) PlaybackPlan {
	if info == nil || len(info.Streams) == 0 {
		return PlaybackPlan{Action: ActionTranscode, Profile: "1080p", Reason: "no media info"}
	}

	var video *StreamInfo
	audioN := 0
	var selectedAudio *StreamInfo

	for i := range info.Streams {
		switch info.Streams[i].CodecType {
		case "video":
			if video == nil {
				video = &info.Streams[i]
			}
		case "audio":
			if audioN == audioStreamIdx {
				selectedAudio = &info.Streams[i]
			}
			audioN++
		}
	}

	videoHeight := 0
	copyVideo := false
	needsFMP4 := false
	needsToneMap := false

	isInterlaced := IsInterlaced(video)
	isRotated := IsRotated(video)
	isAnamorphic := video != nil && IsAnamorphic(video.SampleAspectRatio)
	isDoVi := IsDolbyVision(video)
	dvHDR10Compat := IsDoViHDR10Compatible(video)
	dvNeedsHandling := isDoVi && !clientSupportsDoVi(caps)
	canStripDVtoHDR10 := dvNeedsHandling && dvHDR10Compat
	hevcRetagNeeded := caps.SupportsHEVC && !caps.SupportsHEVCHev1 && IsHEVCHev1Tag(video)
	doviRetag := requiredDoViTag(video, caps)

	if video != nil {
		videoHeight = video.Height
		videoCodec := strings.ToLower(video.CodecName)
		needsToneMap = needsToneMapFor(video, caps)

		filterFix := isInterlaced || isRotated || isAnamorphic
		dvForcesTranscode := dvNeedsHandling && !canStripDVtoHDR10

		if !needsToneMap && !filterFix && !dvForcesTranscode {
			isH264 := containsAny(videoCodec, "h264", "avc")
			isHEVC := containsAny(videoCodec, "hevc", "h265")
			isAV1 := containsAny(videoCodec, "av1", "av01")
			isVP9 := containsAny(videoCodec, "vp9", "vp09")

			if isH264 && VideoCanCopyToTS(video) {
				copyVideo = true
			} else if isHEVC && caps.SupportsHEVC {
				copyVideo = true
				needsFMP4 = true
			} else if isAV1 && caps.SupportsAV1 {
				copyVideo = true
				needsFMP4 = true
			} else if isVP9 && caps.SupportsWebM {
				copyVideo = true
				needsFMP4 = true
			}
		}
	}

	copyAudio := false
	isLossless := false
	if selectedAudio != nil {
		audioCodec := strings.ToLower(selectedAudio.CodecName)
		isLossless = IsLosslessAudio(audioCodec)
		if !isLossless {
			if needsFMP4 {
				copyAudio = AudioCanCopyToFMP4(audioCodec, caps)
			} else {
				copyAudio = AudioCanCopyToTS(audioCodec, caps)
			}
		}
	}

	var reasonStrs []string
	videoCodecName := ""
	audioCodecName := ""
	if video != nil {
		videoCodecName = video.CodecName
	}
	if selectedAudio != nil {
		audioCodecName = selectedAudio.CodecName
	}

	if copyVideo {
		reasonStrs = append(reasonStrs, "copy video")
	} else {
		r := "transcode video (" + videoCodecName + " → h264)"
		if needsToneMap {
			r += " + HDR→SDR tone map"
		}
		reasonStrs = append(reasonStrs, r)
	}
	if copyAudio {
		reasonStrs = append(reasonStrs, "copy audio")
	} else {
		reasonStrs = append(reasonStrs, "transcode audio ("+audioCodecName+" → aac)")
	}

	action := ActionRemux
	if !copyVideo {
		action = ActionTranscode
	}

	// Always emit ReasonContainerNotSupported for HLS delivery — by definition
	// we're not playing the source container directly.
	reasonBits := ReasonContainerNotSupported
	if !copyVideo {
		switch {
		case dvNeedsHandling && !canStripDVtoHDR10:
			reasonBits |= ReasonDolbyVisionNotSupported
			if needsToneMap {
				reasonBits |= ReasonHDRNotSupported
			}
		case needsToneMap:
			reasonBits |= ReasonHDRNotSupported
		case isInterlaced:
			reasonBits |= ReasonInterlacedNotSupported
		case isRotated:
			reasonBits |= ReasonVideoRotationNotSupported
		case isAnamorphic:
			reasonBits |= ReasonAnamorphicNotSupported
		default:
			reasonBits |= ReasonVideoCodecNotSupported
		}
	}
	if copyVideo {
		if hevcRetagNeeded {
			reasonBits |= ReasonVideoCodecTagNotSupported
		}
		if doviRetag != "" {
			reasonBits |= ReasonVideoCodecTagNotSupported
		}
		if canStripDVtoHDR10 {
			reasonBits |= ReasonDolbyVisionNotSupported
		}
	}
	if !copyAudio {
		if isLossless {
			reasonBits |= ReasonAudioLosslessNotSupported
		} else {
			reasonBits |= ReasonAudioCodecNotSupported
		}
	}

	sourceIsHDR := video != nil && (IsHDRStream(video) || IsDolbyVision(video))
	plan := PlaybackPlan{
		Action:    action,
		Profile:   profileForHeight(videoHeight),
		Reason:    strings.Join(reasonStrs, ", "),
		Reasons:   reasonBits,
		CopyVideo: copyVideo,
		CopyAudio: copyAudio,
		// Tone-map is applied by the ffmpeg filter chain. We need it whenever
		// the source is HDR and we're transcoding the video (target is SDR h264).
		NeedsToneMap:  sourceIsHDR && !copyVideo,
		NeedsFMP4:     needsFMP4,
		Deinterlace:   isInterlaced && !copyVideo,
		FixAnamorphic: isAnamorphic && !copyVideo,
	}
	if isRotated && !copyVideo && video != nil {
		plan.Rotate = video.Rotation
	}
	if copyVideo {
		plan.RetagHEVC = hevcRetagNeeded
		plan.RetagDoVi = doviRetag
		plan.StripDoViEL = canStripDVtoHDR10
	}
	if !copyAudio {
		plan.DownmixToStereo = true
	}
	return plan
}

// VideoCanCopyToTS checks if a video stream can be muxed into MPEG-TS without re-encoding.
// Only 8-bit H.264 (up to High profile) is safe. Hi10P, HEVC, AV1, VP9 all need transcode.
func VideoCanCopyToTS(s *StreamInfo) bool {
	codec := strings.ToLower(s.CodecName)
	if !containsAny(codec, "h264", "avc") {
		return false
	}
	if IsHDRStream(s) {
		return false
	}
	profile := strings.ToLower(s.Profile)
	if strings.Contains(profile, "high 10") || strings.Contains(profile, "hi10") ||
		strings.Contains(profile, "high 4:2:2") || strings.Contains(profile, "high 4:4:4") {
		return false
	}
	pix := strings.ToLower(s.PixFmt)
	if pix != "" && pix != "yuv420p" && pix != "yuvj420p" && pix != "nv12" {
		return false
	}
	return true
}

// AudioCanCopyToTS checks if an audio codec can be muxed into MPEG-TS HLS
// segments and decoded by the client. AAC/MP3 are universally supported;
// AC3/EAC3 require the browser to support them in MSE.
func AudioCanCopyToTS(codec string, caps ClientCapabilities) bool {
	switch strings.ToLower(codec) {
	case "aac", "mp3", "mp2":
		return true
	case "ac3", "ac-3":
		return caps.SupportsAC3
	case "eac3", "ec-3":
		return caps.SupportsEAC3
	default:
		return false
	}
}

// AudioCanCopyToFMP4 checks if an audio codec can be muxed into fMP4 HLS
// segments and decoded by the client. AAC/MP3 are universally supported;
// other codecs require explicit MSE support reported by the browser.
func AudioCanCopyToFMP4(codec string, caps ClientCapabilities) bool {
	switch strings.ToLower(codec) {
	case "aac", "mp3", "mp2":
		return true
	case "ac3", "ac-3":
		return caps.SupportsAC3
	case "eac3", "ec-3":
		return caps.SupportsEAC3
	case "opus":
		return caps.SupportsOpus
	case "flac":
		return caps.SupportsFLAC
	default:
		return false
	}
}

func VideoNeedsFMP4(codec string) bool {
	c := strings.ToLower(codec)
	return containsAny(c, "hevc", "h265", "av1", "av01", "vp9", "vp09")
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
