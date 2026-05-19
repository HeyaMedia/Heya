package video

import "regexp"

type VideoCodec string

const (
	CodecX265 VideoCodec = "x265"
	CodecX264 VideoCodec = "x264"
	CodecH264 VideoCodec = "h264"
	CodecH265 VideoCodec = "h265"
	CodecWMV  VideoCodec = "WMV"
	CodecXVID VideoCodec = "xvid"
	CodecDVDR VideoCodec = "dvdr"
)

var codecExp = regexp.MustCompile(`(?i)(?P<x265>x265)|(?P<h265>h265)|(?P<x264>x264)|(?P<h264>h264)|(?P<wmv>WMV)|(?P<xvidhd>XvidHD)|(?P<xvid>X-?vid)|(?P<divx>divx)|(?P<hevc>HEVC)|(?P<dvdr>DVDR)\b`)

type VideoCodecResult struct {
	Codec  VideoCodec
	Source string
}

func ParseVideoCodec(title string) VideoCodecResult {
	match := codecExp.FindStringSubmatch(title)
	if match == nil {
		return VideoCodecResult{}
	}

	names := codecExp.SubexpNames()
	groups := make(map[string]string)
	for i, name := range names {
		if i > 0 && name != "" && match[i] != "" {
			groups[name] = match[i]
		}
	}

	if v, ok := groups["h264"]; ok {
		return VideoCodecResult{Codec: CodecH264, Source: v}
	}
	if v, ok := groups["h265"]; ok {
		return VideoCodecResult{Codec: CodecH265, Source: v}
	}
	if v, ok := groups["x265"]; ok {
		return VideoCodecResult{Codec: CodecX265, Source: v}
	}
	if v, ok := groups["hevc"]; ok {
		return VideoCodecResult{Codec: CodecX265, Source: v}
	}
	if v, ok := groups["x264"]; ok {
		return VideoCodecResult{Codec: CodecX264, Source: v}
	}
	if v, ok := groups["xvidhd"]; ok {
		return VideoCodecResult{Codec: CodecXVID, Source: v}
	}
	if v, ok := groups["xvid"]; ok {
		return VideoCodecResult{Codec: CodecXVID, Source: v}
	}
	if v, ok := groups["divx"]; ok {
		return VideoCodecResult{Codec: CodecXVID, Source: v}
	}
	if v, ok := groups["wmv"]; ok {
		return VideoCodecResult{Codec: CodecWMV, Source: v}
	}
	if v, ok := groups["dvdr"]; ok {
		return VideoCodecResult{Codec: CodecDVDR, Source: v}
	}

	return VideoCodecResult{}
}
