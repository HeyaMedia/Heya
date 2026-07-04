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

var videoQualityHeights = map[VideoQuality]int{
	Quality240p:  240,
	Quality360p:  360,
	Quality480p:  480,
	Quality720p:  720,
	Quality1080p: 1080,
	Quality1440p: 1440,
	Quality2160p: 2160,
	Quality4320p: 4320,
}

func (q VideoQuality) Height() int {
	return videoQualityHeights[q]
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
	"direct": {Name: "direct", VideoCodec: "copy", AudioCodec: "copy"},
	"remux":  {Name: "remux", VideoCodec: "copy", AudioCodec: "copy"},
	"4320p":  {Name: "4320p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 18, MaxBitrate: "60M", Preset: "medium", MaxHeight: 4320},
	"2160p":  {Name: "2160p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 20, MaxBitrate: "20M", Preset: "medium", MaxHeight: 2160},
	"1440p":  {Name: "1440p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 21, MaxBitrate: "14M", Preset: "medium", MaxHeight: 1440},
	"1080p":  {Name: "1080p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 22, MaxBitrate: "8M", Preset: "medium", MaxHeight: 1080},
	"720p":   {Name: "720p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 23, MaxBitrate: "4M", Preset: "fast", MaxHeight: 720},
	"480p":   {Name: "480p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 24, MaxBitrate: "2.5M", Preset: "fast", MaxHeight: 480},
	"360p":   {Name: "360p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 25, MaxBitrate: "1.4M", Preset: "fast", MaxHeight: 360},
	"240p":   {Name: "240p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 26, MaxBitrate: "700k", Preset: "fast", MaxHeight: 240},
	"audio":  {Name: "audio", VideoCodec: "", AudioCodec: "aac", MaxBitrate: "320k"},
}

func GetProfile(name string) (Profile, bool) {
	p, ok := Profiles[name]
	return p, ok
}
