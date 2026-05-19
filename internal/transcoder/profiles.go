package transcoder

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
	"2160p":  {Name: "2160p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 20, MaxBitrate: "20M", Preset: "medium", MaxHeight: 2160},
	"1080p":  {Name: "1080p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 22, MaxBitrate: "8M", Preset: "medium", MaxHeight: 1080},
	"720p":   {Name: "720p", VideoCodec: "libx264", AudioCodec: "aac", CRF: 23, MaxBitrate: "4M", Preset: "fast", MaxHeight: 720},
	"audio":  {Name: "audio", VideoCodec: "", AudioCodec: "aac", MaxBitrate: "320k"},
}

func GetProfile(name string) (Profile, bool) {
	p, ok := Profiles[name]
	return p, ok
}
