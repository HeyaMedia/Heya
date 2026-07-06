package mediaprobe

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type Disposition struct {
	Default         int `json:"default"`
	Forced          int `json:"forced"`
	HearingImpaired int `json:"hearing_impaired"`
	VisualImpaired  int `json:"visual_impaired"`
	Comment         int `json:"comment"`
}

type MediaInfo struct {
	Format    FormatInfo   `json:"format"`
	Streams   []StreamInfo `json:"streams"`
	Duration  float64      `json:"duration"`
	Size      int64        `json:"size"`
	BitRate   int64        `json:"bit_rate"`
	Container string       `json:"container"`
	// StartTime is the parsed form of Format.StartTime (seconds). Non-zero
	// means the container's presentation timeline doesn't start at PTS 0 —
	// common for Blu-ray remuxes and other sources that inherit an absolute
	// PTS baseline from their transport-stream origin. No current playback
	// path in this codebase compensates for it (the HLS transcoder's
	// -copyts output preserves whatever the source carries), so this exists
	// to let future work detect/audit affected files rather than to drive
	// any behavior today.
	StartTime float64 `json:"start_time,omitempty"`
}

type FormatInfo struct {
	Filename       string `json:"filename"`
	FormatName     string `json:"format_name"`
	FormatLongName string `json:"format_long_name"`
	Duration       string `json:"duration"`
	// StartTime is ffprobe's raw format.start_time (seconds, as a decimal
	// string — may be negative). See MediaInfo.StartTime for the parsed form.
	StartTime string            `json:"start_time,omitempty"`
	Size      string            `json:"size"`
	BitRate   string            `json:"bit_rate"`
	Tags      map[string]string `json:"tags"`
}

type StreamInfo struct {
	Index              int    `json:"index"`
	CodecName          string `json:"codec_name"`
	CodecLongName      string `json:"codec_long_name"`
	CodecType          string `json:"codec_type"`
	CodecTagString     string `json:"codec_tag_string,omitempty"`
	Profile            string `json:"profile,omitempty"`
	Level              int    `json:"level,omitempty"`
	RFrameRate         string `json:"r_frame_rate,omitempty"`
	AvgFrameRate       string `json:"avg_frame_rate,omitempty"`
	Width              int    `json:"width,omitempty"`
	Height             int    `json:"height,omitempty"`
	PixFmt             string `json:"pix_fmt,omitempty"`
	BitsPerRawSample   string `json:"bits_per_raw_sample,omitempty"`
	ColorRange         string `json:"color_range,omitempty"`
	ColorSpace         string `json:"color_space,omitempty"`
	ColorTransfer      string `json:"color_transfer,omitempty"`
	ColorPrimaries     string `json:"color_primaries,omitempty"`
	FieldOrder         string `json:"field_order,omitempty"`
	SampleAspectRatio  string `json:"sample_aspect_ratio,omitempty"`
	DisplayAspectRatio string `json:"display_aspect_ratio,omitempty"`
	SampleRate         string `json:"sample_rate,omitempty"`
	Channels           int    `json:"channels,omitempty"`
	ChannelLayout      string `json:"channel_layout,omitempty"`
	BitRate            string `json:"bit_rate,omitempty"`
	Duration           string `json:"duration,omitempty"`
	// StartTime is ffprobe's raw per-stream start_time (seconds, decimal
	// string). Streams within the same file can disagree slightly (e.g. a
	// secondary audio/subtitle track a few ms earlier than the primary
	// video/audio pair) — kept raw per-stream rather than rolled into a
	// single MediaInfo-level value. See MediaInfo.StartTime for the
	// format-level (container) figure most callers actually want.
	StartTime    string            `json:"start_time,omitempty"`
	Disposition  *Disposition      `json:"disposition,omitempty"`
	Tags         map[string]string `json:"tags"`
	SideDataList []SideData        `json:"side_data_list,omitempty"`
}

type SideData struct {
	Type                      string `json:"side_data_type"`
	Rotation                  int    `json:"rotation,omitempty"`
	DvVersionMajor            int    `json:"dv_version_major,omitempty"`
	DvVersionMinor            int    `json:"dv_version_minor,omitempty"`
	DvProfile                 int    `json:"dv_profile,omitempty"`
	DvLevel                   int    `json:"dv_level,omitempty"`
	DvBlSignalCompatibilityID int    `json:"dv_bl_signal_compatibility_id,omitempty"`
	RpuPresentFlag            int    `json:"rpu_present_flag,omitempty"`
	ElPresentFlag             int    `json:"el_present_flag,omitempty"`
	BlPresentFlag             int    `json:"bl_present_flag,omitempty"`
}

type output struct {
	Format  FormatInfo   `json:"format"`
	Streams []StreamInfo `json:"streams"`
}

type AudioFields struct {
	BitrateKbps  int32
	SampleRateHz int32
	BitDepth     int32
	Channels     int32
	Duration     int32
}

func Parse(data []byte) (*MediaInfo, error) {
	var probe output
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}

	filtered := make([]StreamInfo, 0, len(probe.Streams))
	for _, s := range probe.Streams {
		if s.CodecType == "attachment" {
			continue
		}
		filtered = append(filtered, s)
	}

	info := &MediaInfo{Format: probe.Format, Streams: filtered, Container: probe.Format.FormatName}
	PopulateNumericFields(info)
	return info, nil
}

func PrimaryAudio(info *MediaInfo) *StreamInfo {
	if info == nil {
		return nil
	}
	for i := range info.Streams {
		if info.Streams[i].CodecType == "audio" {
			return &info.Streams[i]
		}
	}
	return nil
}

func AudioFieldsFrom(info *MediaInfo, audio *StreamInfo) AudioFields {
	if info == nil || audio == nil {
		return AudioFields{}
	}
	bitrate := int32(ParseFloatString(audio.BitRate) / 1000)
	if bitrate == 0 && info.Format.BitRate != "" {
		bitrate = int32(ParseFloatString(info.Format.BitRate) / 1000)
	}
	duration := int32(info.Duration)
	if duration == 0 && audio.Duration != "" {
		duration = int32(ParseFloatString(audio.Duration))
	}
	return AudioFields{
		BitrateKbps:  bitrate,
		SampleRateHz: int32(ParseFloatString(audio.SampleRate)),
		BitDepth:     int32(ParseIntString(audio.BitsPerRawSample)),
		Channels:     int32(audio.Channels),
		Duration:     duration,
	}
}

func PopulateNumericFields(info *MediaInfo) {
	if info.Format.Duration != "" {
		if v, err := strconv.ParseFloat(info.Format.Duration, 64); err == nil {
			info.Duration = v
		}
	}
	if info.Format.StartTime != "" {
		if v, err := strconv.ParseFloat(info.Format.StartTime, 64); err == nil {
			info.StartTime = v
		}
	}
	if info.Format.Size != "" {
		if v, err := strconv.ParseInt(info.Format.Size, 10, 64); err == nil {
			info.Size = v
		}
	}
	if info.Format.BitRate != "" {
		if v, err := strconv.ParseInt(info.Format.BitRate, 10, 64); err == nil {
			info.BitRate = v
		}
	}
}

func RefinedQualityScore(format string, bitrateKbps, bitDepth, sampleRateHz int32) int {
	base := ExtensionQualityBase(format)
	switch strings.ToLower(strings.TrimPrefix(format, ".")) {
	case "flac", "alac", "wav":
		if bitDepth > 16 {
			base += int(bitDepth-16) * 30
		}
		if sampleRateHz > 48000 {
			base += int((sampleRateHz - 48000) / 1000)
		}
	default:
		base += int(bitrateKbps) * 4 / 10
	}
	return base
}

func ExtensionQualityBase(format string) int {
	switch strings.ToLower(strings.TrimPrefix(format, ".")) {
	case "flac":
		return 1000
	case "alac":
		return 950
	case "wav":
		return 900
	case "opus":
		return 500
	case "ogg":
		return 450
	case "aac":
		return 350
	case "m4a":
		return 300
	case "mp3":
		return 200
	}
	return 0
}

func ParseFloatString(s string) float64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func ParseIntString(s string) int64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
