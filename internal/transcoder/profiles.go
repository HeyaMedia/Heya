package transcoder

import "fmt"

type VideoQuality int

const (
	QualityOriginal VideoQuality = iota
	Quality240p
	Quality360p
	Quality480p
	Quality720p
	Quality1080p
	Quality1440p
	Quality2160p
	Quality4320p
)

type videoQualitySpec struct {
	Height     int
	AvgBitrate map[string]int64
	MaxBitrate map[string]int64
}

var videoQualitySpecs = map[VideoQuality]videoQualitySpec{
	Quality240p:  {Height: 240, AvgBitrate: map[string]int64{"h264": 400_000, "hevc": 300_000, "av1": 200_000}, MaxBitrate: map[string]int64{"h264": 700_000, "hevc": 500_000, "av1": 350_000}},
	Quality360p:  {Height: 360, AvgBitrate: map[string]int64{"h264": 800_000, "hevc": 600_000, "av1": 400_000}, MaxBitrate: map[string]int64{"h264": 1_400_000, "hevc": 1_000_000, "av1": 700_000}},
	Quality480p:  {Height: 480, AvgBitrate: map[string]int64{"h264": 1_400_000, "hevc": 1_000_000, "av1": 700_000}, MaxBitrate: map[string]int64{"h264": 2_500_000, "hevc": 1_800_000, "av1": 1_200_000}},
	Quality720p:  {Height: 720, AvgBitrate: map[string]int64{"h264": 2_800_000, "hevc": 2_000_000, "av1": 1_400_000}, MaxBitrate: map[string]int64{"h264": 4_000_000, "hevc": 3_000_000, "av1": 2_000_000}},
	Quality1080p: {Height: 1080, AvgBitrate: map[string]int64{"h264": 5_000_000, "hevc": 3_500_000, "av1": 2_500_000}, MaxBitrate: map[string]int64{"h264": 8_000_000, "hevc": 6_000_000, "av1": 4_000_000}},
	Quality1440p: {Height: 1440, AvgBitrate: map[string]int64{"h264": 9_000_000, "hevc": 6_000_000, "av1": 4_000_000}, MaxBitrate: map[string]int64{"h264": 14_000_000, "hevc": 10_000_000, "av1": 7_000_000}},
	Quality2160p: {Height: 2160, AvgBitrate: map[string]int64{"h264": 15_000_000, "hevc": 10_000_000, "av1": 7_000_000}, MaxBitrate: map[string]int64{"h264": 20_000_000, "hevc": 15_000_000, "av1": 10_000_000}},
	Quality4320p: {Height: 4320, AvgBitrate: map[string]int64{"h264": 40_000_000, "hevc": 25_000_000, "av1": 15_000_000}, MaxBitrate: map[string]int64{"h264": 60_000_000, "hevc": 40_000_000, "av1": 25_000_000}},
}

func (q VideoQuality) Height() int {
	if spec, ok := videoQualitySpecs[q]; ok {
		return spec.Height
	}
	return 0
}

func (q VideoQuality) AvgBitrate(codec string) int64 {
	if spec, ok := videoQualitySpecs[q]; ok {
		if br, ok := spec.AvgBitrate[codec]; ok {
			return br
		}
		return spec.AvgBitrate["h264"]
	}
	return 0
}

func (q VideoQuality) MaxBitrate(codec string) int64 {
	if spec, ok := videoQualitySpecs[q]; ok {
		if br, ok := spec.MaxBitrate[codec]; ok {
			return br
		}
		return spec.MaxBitrate["h264"]
	}
	return 0
}

func (q VideoQuality) String() string {
	if q == QualityOriginal {
		return "original"
	}
	return fmt.Sprintf("%dp", q.Height())
}

var qualityOrder = []VideoQuality{
	Quality4320p, Quality2160p, Quality1440p, Quality1080p,
	Quality720p, Quality480p, Quality360p, Quality240p,
}

func BuildBitrateLadder(sourceHeight int) []VideoQuality {
	var ladder []VideoQuality
	for _, q := range qualityOrder {
		if q.Height() <= sourceHeight {
			ladder = append(ladder, q)
		}
	}
	if len(ladder) == 0 {
		ladder = append(ladder, Quality240p)
	}
	return ladder
}

type AudioQuality int

const (
	AudioOriginal AudioQuality = iota
	Audio128k
	Audio192k
	Audio256k
	Audio384k
	Audio512k
)

func (a AudioQuality) Bitrate() int {
	switch a {
	case Audio128k:
		return 128_000
	case Audio192k:
		return 192_000
	case Audio256k:
		return 256_000
	case Audio384k:
		return 384_000
	case Audio512k:
		return 512_000
	default:
		return 0
	}
}

func (a AudioQuality) String() string {
	if a == AudioOriginal {
		return "original"
	}
	return fmt.Sprintf("%dk", a.Bitrate()/1000)
}

type Profile struct {
	Name       string
	VideoCodec string
	AudioCodec string
	CRF        int
	MaxBitrate string
	Preset     string
	MaxHeight  int
}

var Profiles = map[string]Profile{
	"direct":   {Name: "direct", VideoCodec: "copy", AudioCodec: "copy"},
	"remux":    {Name: "remux", VideoCodec: "copy", AudioCodec: "copy"},
	"4320p":    {Name: "4320p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 18, MaxBitrate: "60M", Preset: "medium", MaxHeight: 4320},
	"2160p":    {Name: "2160p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 20, MaxBitrate: "20M", Preset: "medium", MaxHeight: 2160},
	"1440p":    {Name: "1440p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 21, MaxBitrate: "14M", Preset: "medium", MaxHeight: 1440},
	"1080p":    {Name: "1080p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 22, MaxBitrate: "8M", Preset: "medium", MaxHeight: 1080},
	"720p":     {Name: "720p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 23, MaxBitrate: "4M", Preset: "fast", MaxHeight: 720},
	"480p":     {Name: "480p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 24, MaxBitrate: "2.5M", Preset: "fast", MaxHeight: 480},
	"360p":     {Name: "360p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 25, MaxBitrate: "1.4M", Preset: "fast", MaxHeight: 360},
	"240p":     {Name: "240p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 26, MaxBitrate: "700k", Preset: "fast", MaxHeight: 240},
	"audio":    {Name: "audio", VideoCodec: "", AudioCodec: "aac", MaxBitrate: "320k"},
}

func GetProfile(name string) (Profile, bool) {
	p, ok := Profiles[name]
	return p, ok
}

func QualityToProfile(q VideoQuality, hwAccel HwAccelConfig) Profile {
	codec := hwAccel.EncoderH264
	if codec == "" {
		codec = "libx264"
	}
	height := q.Height()
	maxBr := q.MaxBitrate("h264")

	preset := "medium"
	if height <= 480 {
		preset = "fast"
	}

	crf := 22
	switch {
	case height >= 2160:
		crf = 20
	case height >= 1440:
		crf = 21
	case height >= 1080:
		crf = 22
	case height >= 720:
		crf = 23
	case height >= 480:
		crf = 24
	default:
		crf = 25
	}

	return Profile{
		Name:       q.String(),
		VideoCodec: codec,
		AudioCodec: "aac",
		CRF:        crf,
		MaxBitrate: fmt.Sprintf("%d", maxBr),
		Preset:     preset,
		MaxHeight:  height,
	}
}
