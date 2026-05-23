package video

import "regexp"

type AudioCodec string

const (
	AudioMP3    AudioCodec = "MP3"
	AudioMP2    AudioCodec = "MP2"
	AudioDOLBY  AudioCodec = "Dolby Digital"
	AudioEAC3   AudioCodec = "Dolby Digital Plus"
	AudioAAC    AudioCodec = "AAC"
	AudioFLAC   AudioCodec = "FLAC"
	AudioDTS    AudioCodec = "DTS"
	AudioDTSHD  AudioCodec = "DTS-HD"
	AudioTRUEHD AudioCodec = "Dolby TrueHD"
	AudioOPUS   AudioCodec = "Opus"
	AudioVORBIS AudioCodec = "Vorbis"
	AudioPCM    AudioCodec = "PCM"
	AudioLPCM   AudioCodec = "LPCM"
)

var audioCodecExp = regexp.MustCompile(`(?i)\b(?P<mp3>(?:LAME\d+-?\d+)|(?:mp3))\b|\b(?P<mp2>mp2)\b|\b(?P<dolby>(?:Dolby)|(?:Dolby-?Digital)|(?:DD)|(?:AC3D?))\b|\b(?P<dolbyatmos>Dolby-?Atmos)\b|\b(?P<aac>AAC)(?:\d?.?\d?)(?:ch)?\b|\b(?P<eac3>(?:EAC3|DDP|DD\+))\b|\b(?P<flac>FLAC)\b|\b(?P<dtshd>DTS-?HD|DTS-?MA|DTS-X)\b|\b(?P<dts>DTS)\b|\b(?P<truehd>True-?HD)\b|\b(?P<opus>Opus)\b|\b(?P<vorbis>Vorbis)\b|\b(?P<pcm>PCM)\b|\b(?P<lpcm>LPCM)\b`)

type AudioCodecResult struct {
	Codec  AudioCodec
	Source string
}

func ParseAudioCodec(title string) AudioCodecResult {
	match := audioCodecExp.FindStringSubmatch(title)
	if match == nil {
		return AudioCodecResult{}
	}

	names := audioCodecExp.SubexpNames()
	groups := make(map[string]string)
	for i, name := range names {
		if i > 0 && name != "" && match[i] != "" {
			groups[name] = match[i]
		}
	}

	if v, ok := groups["aac"]; ok {
		return AudioCodecResult{Codec: AudioAAC, Source: v}
	}
	if v, ok := groups["dolbyatmos"]; ok {
		return AudioCodecResult{Codec: AudioEAC3, Source: v}
	}
	if v, ok := groups["dolby"]; ok {
		return AudioCodecResult{Codec: AudioDOLBY, Source: v}
	}
	if v, ok := groups["dtshd"]; ok {
		return AudioCodecResult{Codec: AudioDTSHD, Source: v}
	}
	if v, ok := groups["dts"]; ok {
		return AudioCodecResult{Codec: AudioDTS, Source: v}
	}
	if v, ok := groups["flac"]; ok {
		return AudioCodecResult{Codec: AudioFLAC, Source: v}
	}
	if v, ok := groups["truehd"]; ok {
		return AudioCodecResult{Codec: AudioTRUEHD, Source: v}
	}
	if v, ok := groups["mp3"]; ok {
		return AudioCodecResult{Codec: AudioMP3, Source: v}
	}
	if v, ok := groups["mp2"]; ok {
		return AudioCodecResult{Codec: AudioMP2, Source: v}
	}
	if v, ok := groups["pcm"]; ok {
		return AudioCodecResult{Codec: AudioPCM, Source: v}
	}
	if v, ok := groups["lpcm"]; ok {
		return AudioCodecResult{Codec: AudioLPCM, Source: v}
	}
	if v, ok := groups["opus"]; ok {
		return AudioCodecResult{Codec: AudioOPUS, Source: v}
	}
	if v, ok := groups["vorbis"]; ok {
		return AudioCodecResult{Codec: AudioVORBIS, Source: v}
	}
	if v, ok := groups["eac3"]; ok {
		return AudioCodecResult{Codec: AudioEAC3, Source: v}
	}

	return AudioCodecResult{}
}
