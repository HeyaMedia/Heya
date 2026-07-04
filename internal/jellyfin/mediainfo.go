package jellyfin

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/mediaprobe"
	"github.com/karbowiak/heya/internal/transcoder"
)

// ffprobe → transcoder/Jellyfin translation. The toTranscoderInfo family is
// adapted from internal/server/streaming_handlers.go (package-private there;
// the jellyfin surface deliberately doesn't import internal/server). If the
// transcode decision inputs grow a field, both sites need it — the decision
// matrix tests in internal/transcoder cover the semantics.

func toTranscoderInfo(info *mediaprobe.MediaInfo) transcoder.MediaInfo {
	var streams []transcoder.StreamInfo
	for _, s := range info.Streams {
		dvProfile, dvCompat, rotation := sideDataFields(s.SideDataList)
		streams = append(streams, transcoder.StreamInfo{
			CodecName:         s.CodecName,
			CodecType:         s.CodecType,
			Profile:           s.Profile,
			PixFmt:            s.PixFmt,
			Width:             s.Width,
			Height:            s.Height,
			ColorTransfer:     s.ColorTransfer,
			ColorPrimaries:    s.ColorPrimaries,
			ColorSpace:        s.ColorSpace,
			CodecTag:          s.CodecTagString,
			BitDepth:          bitDepthOf(s.BitsPerRawSample, s.PixFmt),
			SampleAspectRatio: s.SampleAspectRatio,
			FieldOrder:        s.FieldOrder,
			Rotation:          rotation,
			DvProfile:         dvProfile,
			DvBlCompatID:      dvCompat,
			Channels:          s.Channels,
			ChannelLayout:     s.ChannelLayout,
		})
	}
	return transcoder.MediaInfo{Container: info.Container, Streams: streams}
}

func sideDataFields(side []mediaprobe.SideData) (dvProfile, dvCompat, rotation int) {
	for _, sd := range side {
		switch sd.Type {
		case "DOVI configuration record", "Dolby Vision configuration record", "Dolby Vision Configuration":
			if sd.DvProfile > 0 {
				dvProfile = sd.DvProfile
				dvCompat = sd.DvBlSignalCompatibilityID
			}
		case "Display Matrix":
			r := -sd.Rotation % 360
			if r < 0 {
				r += 360
			}
			switch r {
			case 90, 180, 270:
				rotation = r
			}
		}
	}
	return
}

func bitDepthOf(bitsStr, pixFmt string) int {
	if bitsStr != "" {
		if n, err := strconv.Atoi(bitsStr); err == nil && n > 0 {
			return n
		}
	}
	pix := strings.ToLower(pixFmt)
	switch {
	case strings.Contains(pix, "12le"), strings.Contains(pix, "12be"):
		return 12
	case strings.Contains(pix, "10le"), strings.Contains(pix, "10be"):
		return 10
	case pix == "":
		return 0
	default:
		return 8
	}
}

// buildMediaStreams renders ffprobe streams as Jellyfin MediaStreams.
// Subtitle DeliveryUrl points at Heya's native extraction endpoint — the
// client's api_key is a Heya session token, so native ?token= auth just
// works (same trick the TranscodingUrl uses).
func buildMediaStreams(fileID int64, token string, info *mediaprobe.MediaInfo) (streams []mediaStream, defaultAudio, defaultSub *int) {
	for _, s := range info.Streams {
		ms := mediaStream{
			Codec:              s.CodecName,
			Index:              s.Index,
			Profile:            s.Profile,
			Language:           s.Tags["language"],
			Title:              s.Tags["title"],
			TimeBase:           "1/1000",
			ColorTransfer:      s.ColorTransfer,
			ColorPrimaries:     s.ColorPrimaries,
			ColorSpace:         s.ColorSpace,
			PixelFormat:        s.PixFmt,
			AudioSpatialFormat: "None",
			LocalizedDefault:   "Default",
			LocalizedExternal:  "External",
			LocalizedForced:    "Forced",
		}
		if d := s.Disposition; d != nil {
			ms.IsDefault = d.Default == 1
			ms.IsForced = d.Forced == 1
			ms.IsHearingImpaired = d.HearingImpaired == 1
		}
		if br, err := strconv.ParseInt(s.BitRate, 10, 64); err == nil {
			ms.BitRate = br
		}

		switch s.CodecType {
		case "video":
			ms.Type = "Video"
			ms.Width = s.Width
			ms.Height = s.Height
			ms.BitDepth = bitDepthOf(s.BitsPerRawSample, s.PixFmt)
			ms.IsAVC = s.CodecName == "h264"
			ms.VideoRange, ms.VideoRangeType = videoRangeOf(s)
			ms.DisplayTitle = videoDisplayTitle(s, ms.VideoRangeType)
			ms.Level = float64(s.Level)
			ms.RefFrames = 1
			if fr := parseFrameRate(s.RFrameRate); fr > 0 {
				ms.RealFrameRate = fr
				ms.ReferenceFrameRate = fr
			}
			if fr := parseFrameRate(s.AvgFrameRate); fr > 0 {
				ms.AverageFrameRate = fr
			}
			if s.DisplayAspectRatio != "" {
				ms.AspectRatio = s.DisplayAspectRatio
			}
		case "audio":
			ms.Type = "Audio"
			ms.Channels = s.Channels
			ms.ChannelLayout = s.ChannelLayout
			if sr, err := strconv.Atoi(s.SampleRate); err == nil {
				ms.SampleRate = sr
			}
			ms.DisplayTitle = audioDisplayTitle(s)
			if defaultAudio == nil || ms.IsDefault {
				idx := s.Index
				if defaultAudio == nil || ms.IsDefault {
					defaultAudio = &idx
				}
			}
		case "subtitle":
			ms.Type = "Subtitle"
			ms.DisplayTitle = subtitleDisplayTitle(s)
			switch transcoder.SubtitleDeliveryFor(s.CodecName) {
			case transcoder.SubDeliveryExternal:
				ms.IsTextSubtitleStream = true
				ms.SupportsExternalStream = true
				ms.DeliveryMethod = "External"
				ms.DeliveryURL = fmt.Sprintf("/api/stream/%d/subtitles/%d?token=%s", fileID, s.Index, url.QueryEscape(token))
			default:
				// Bitmap subs need burn-in; advertising Encode makes clients
				// re-request playback with the subtitle index set.
				ms.DeliveryMethod = "Encode"
			}
			if ms.IsDefault && defaultSub == nil {
				idx := s.Index
				defaultSub = &idx
			}
		default:
			continue
		}
		// Upstream always carries a BitRate on av streams; fall back to the
		// container bitrate when ffprobe has no per-stream figure (mkv).
		if ms.BitRate == 0 && (ms.Type == "Video" || ms.Type == "Audio") && info.BitRate > 0 {
			ms.BitRate = info.BitRate
		}
		streams = append(streams, ms)
	}
	return streams, defaultAudio, defaultSub
}

// parseFrameRate converts ffprobe's "25/1" rational into a float.
func parseFrameRate(r string) float32 {
	num, den, ok := strings.Cut(r, "/")
	if !ok {
		return 0
	}
	n, err1 := strconv.ParseFloat(num, 64)
	d, err2 := strconv.ParseFloat(den, 64)
	if err1 != nil || err2 != nil || d == 0 || n <= 0 {
		return 0
	}
	return float32(n / d)
}

func videoRangeOf(s mediaprobe.StreamInfo) (videoRange, rangeType string) {
	for _, sd := range s.SideDataList {
		if strings.Contains(sd.Type, "DOVI") || strings.Contains(sd.Type, "Dolby Vision") {
			return "HDR", "DOVI"
		}
	}
	switch s.ColorTransfer {
	case "smpte2084":
		return "HDR", "HDR10"
	case "arib-std-b67":
		return "HDR", "HLG"
	default:
		return "SDR", "SDR"
	}
}

func videoDisplayTitle(s mediaprobe.StreamInfo, rangeType string) string {
	parts := []string{}
	switch {
	case s.Height >= 2100:
		parts = append(parts, "4K")
	case s.Height >= 1000:
		parts = append(parts, "1080p")
	case s.Height >= 700:
		parts = append(parts, "720p")
	case s.Height > 0:
		parts = append(parts, fmt.Sprintf("%dp", s.Height))
	}
	parts = append(parts, strings.ToUpper(s.CodecName))
	if rangeType != "" && rangeType != "SDR" {
		parts = append(parts, rangeType)
	}
	return strings.Join(parts, " ")
}

func audioDisplayTitle(s mediaprobe.StreamInfo) string {
	lang := langName(s.Tags["language"])
	parts := []string{}
	if lang != "" {
		parts = append(parts, lang)
	}
	parts = append(parts, strings.ToUpper(s.CodecName))
	if s.ChannelLayout != "" {
		parts = append(parts, s.ChannelLayout)
	}
	title := strings.Join(parts, " - ")
	if d := s.Disposition; d != nil && d.Default == 1 {
		title += " (Default)"
	}
	return title
}

func subtitleDisplayTitle(s mediaprobe.StreamInfo) string {
	lang := langName(s.Tags["language"])
	if t := s.Tags["title"]; t != "" {
		if lang != "" {
			return lang + " - " + t
		}
		return t
	}
	if lang != "" {
		return lang + " - " + strings.ToUpper(s.CodecName)
	}
	return strings.ToUpper(s.CodecName)
}

// langName prettifies the common ISO 639-2 tags; unknown codes pass through.
func langName(code string) string {
	switch strings.ToLower(code) {
	case "eng":
		return "English"
	case "jpn":
		return "Japanese"
	case "dan":
		return "Danish"
	case "ger", "deu":
		return "German"
	case "fre", "fra":
		return "French"
	case "spa":
		return "Spanish"
	case "":
		return ""
	default:
		return code
	}
}

// capsFromProfile maps a Jellyfin DeviceProfile onto Heya's transcoder
// capability flags. DirectPlayProfiles say what the device decodes natively;
// CodecProfile VideoRangeType conditions say which HDR flavors survive
// display. Absent profile → conservative defaults (h264/mp4/aac), matching
// what DecideForHLS assumes for unknown clients.
func capsFromProfile(p *deviceProfile) transcoder.ClientCapabilities {
	caps := transcoder.DefaultClientCaps
	if p == nil {
		return caps
	}
	for _, dp := range p.DirectPlayProfiles {
		containers := strings.ToLower(dp.Container)
		if strings.Contains(containers, "mkv") {
			caps.SupportsMKV = true
		}
		if strings.Contains(containers, "webm") {
			caps.SupportsWebM = true
		}
		vc := strings.ToLower(dp.VideoCodec)
		if strings.Contains(vc, "hevc") || strings.Contains(vc, "h265") {
			caps.SupportsHEVC = true
			caps.SupportsHEVCHev1 = true
		}
		if strings.Contains(vc, "av1") {
			caps.SupportsAV1 = true
		}
		ac := strings.ToLower(dp.AudioCodec)
		if strings.Contains(ac, "flac") {
			caps.SupportsFLAC = true
		}
		if strings.Contains(ac, "opus") {
			caps.SupportsOpus = true
		}
		if strings.Contains(ac, "eac3") {
			caps.SupportsEAC3 = true
		}
		if strings.Contains(ac, "ac3") {
			caps.SupportsAC3 = true
		}
	}
	// HDR passthrough. A device that direct-plays HEVC Main 10 decodes the
	// 10-bit bitstream and passes the transfer function (PQ/HLG) straight to
	// the display — it does NOT need to tonemap. Jellyfin clients that handle
	// HDR express this by NOT restricting VideoRangeType, so the correct
	// default for an HEVC-capable client is "handles HDR10 + HLG" (forcing
	// these off led to an unnecessary, and on 4K failing, tonemap transcode).
	// DoVi stays opt-in — profile 5 needs explicit support.
	if caps.SupportsHEVC {
		caps.SupportsHDR10 = true
		caps.SupportsHLG = true
	}
	// An explicit VideoRangeType condition on an HDR-capable codec profile is
	// AUTHORITATIVE and can turn the default OFF: an EqualsAny condition is
	// the client's allow-list (a stream must match it to direct-play), so an
	// SDR-only decoder ("EqualsAny SDR") correctly disables passthrough and
	// gets a tonemap. NotEquals conditions are a deny-list. Range conditions
	// on SDR-only codecs (h264) are irrelevant to HDR and skipped.
	for _, cp := range p.CodecProfiles {
		if cp.Type != "" && !strings.EqualFold(cp.Type, "Video") {
			continue
		}
		codec := strings.ToLower(cp.Codec)
		if codec != "" && !strings.Contains(codec, "hevc") && !strings.Contains(codec, "h265") && !strings.Contains(codec, "av1") {
			continue
		}
		for _, c := range cp.Conditions {
			if !strings.EqualFold(c.Property, "VideoRangeType") {
				continue
			}
			v := strings.ToLower(c.Value)
			has := func(s string) bool { return strings.Contains(v, s) }
			isDoVi := has("dovi") || has("dolby")
			if strings.HasPrefix(strings.ToLower(c.Condition), "notequal") {
				// Deny-list: turn off exactly what's named.
				if has("hdr10") {
					caps.SupportsHDR10 = false
				}
				if has("hlg") {
					caps.SupportsHLG = false
				}
				if isDoVi {
					caps.SupportsDoVi = false
				}
				continue
			}
			// Allow-list (EqualsAny / Equals): the supported set is exactly
			// what's named — anything absent is unsupported.
			caps.SupportsHDR10 = has("hdr10")
			caps.SupportsHLG = has("hlg")
			caps.SupportsDoVi = isDoVi
		}
	}
	return caps
}

// audioCapsFromContainers parses the /Audio/{id}/universal Container param
// ("opus,webm|opus,mp3,aac,m4a|aac,flac,...") into AudioCaps. Entries may be
// "container|codec" pairs; both sides count as accepted format tokens.
func audioCapsFromContainers(list string) transcoder.AudioCaps {
	caps := transcoder.AudioCaps{}
	for _, entry := range strings.Split(strings.ToLower(list), ",") {
		for _, tok := range strings.Split(entry, "|") {
			switch strings.TrimSpace(tok) {
			case "mp3":
				caps.MP3 = true
			case "flac":
				caps.FLAC = true
			case "aac", "m4a", "m4b", "mp4":
				caps.AAC = true
			case "alac":
				caps.ALAC = true
			case "ogg", "oga", "vorbis", "webma":
				caps.Vorbis = true
			case "opus", "webm":
				caps.Opus = true
			case "wav", "pcm":
				caps.WavPCM = true
			}
		}
	}
	return caps
}

// capsQuery renders capability flags as the supports_* query params Heya's
// native stream endpoints parse — the glue that lets TranscodingUrl point
// straight at /api/stream/{id}/hls/master.m3u8.
func capsQuery(caps transcoder.ClientCapabilities) string {
	var b strings.Builder
	flag := func(name string, on bool) {
		if on {
			b.WriteString("&supports_")
			b.WriteString(name)
			b.WriteString("=1")
		}
	}
	flag("hevc", caps.SupportsHEVC)
	flag("hevc_hev1", caps.SupportsHEVCHev1)
	flag("av1", caps.SupportsAV1)
	flag("flac", caps.SupportsFLAC)
	flag("opus", caps.SupportsOpus)
	flag("ac3", caps.SupportsAC3)
	flag("eac3", caps.SupportsEAC3)
	flag("mkv", caps.SupportsMKV)
	flag("webm", caps.SupportsWebM)
	flag("hdr10", caps.SupportsHDR10)
	flag("hlg", caps.SupportsHLG)
	flag("dovi", caps.SupportsDoVi)
	return b.String()
}

func containerOf(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(strings.ToLower(pathExt(path)), "."))
	return ext
}

func pathExt(p string) string {
	if i := strings.LastIndex(p, "."); i >= 0 && i > strings.LastIndex(p, "/") {
		return p[i:]
	}
	return ""
}

func contentTypeForPath(path string) string {
	switch containerOf(path) {
	case "mp4", "m4v":
		return "video/mp4"
	case "mkv":
		return "video/x-matroska"
	case "webm":
		return "video/webm"
	case "avi":
		return "video/x-msvideo"
	case "mov":
		return "video/quicktime"
	case "ts":
		return "video/mp2t"
	case "flac":
		return "audio/flac"
	case "mp3":
		return "audio/mpeg"
	case "m4a", "m4b", "aac", "alac":
		return "audio/mp4"
	case "ogg", "oga":
		return "audio/ogg"
	case "opus":
		return "audio/ogg; codecs=opus"
	case "wav":
		return "audio/wav"
	default:
		return "application/octet-stream"
	}
}
