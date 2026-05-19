package video

import "regexp"

type Channels string

const (
	ChannelsSeven  Channels = "7.1"
	ChannelsSix    Channels = "5.1"
	ChannelsStereo Channels = "stereo"
	ChannelsMono   Channels = "mono"
)

var channelExp = regexp.MustCompile(`(?i)\b(?P<eight>7.?[01])\b|\b(?P<six>6[\W]0(?:ch)?|5[\W][01](?:ch)?|5ch|6ch)\b|(?P<stereo>2[\W]0(?:ch)?|stereo)|(?P<mono>1[\W]0(?:ch)?|mono|1ch)`)

type ChannelsResult struct {
	Channels Channels
	Source   string
}

func ParseAudioChannels(title string) ChannelsResult {
	match := channelExp.FindStringSubmatch(title)
	if match == nil {
		return ChannelsResult{}
	}

	names := channelExp.SubexpNames()
	groups := make(map[string]string)
	for i, name := range names {
		if i > 0 && name != "" && match[i] != "" {
			groups[name] = match[i]
		}
	}

	if v, ok := groups["eight"]; ok {
		return ChannelsResult{Channels: ChannelsSeven, Source: v}
	}
	if v, ok := groups["six"]; ok {
		return ChannelsResult{Channels: ChannelsSix, Source: v}
	}
	if v, ok := groups["stereo"]; ok {
		return ChannelsResult{Channels: ChannelsStereo, Source: v}
	}
	if v, ok := groups["mono"]; ok {
		return ChannelsResult{Channels: ChannelsMono, Source: v}
	}

	return ChannelsResult{}
}
