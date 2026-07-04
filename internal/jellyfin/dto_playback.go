package jellyfin

// Playback negotiation DTOs — hand-written subsets of Jellyfin's
// PlaybackInfoResponse / MediaSourceInfo / MediaStream.

type playbackInfoResponse struct {
	MediaSources  []mediaSourceInfo `json:"MediaSources"`
	PlaySessionID string            `json:"PlaySessionId"`
	ErrorCode     string            `json:"ErrorCode,omitempty"`
}

type mediaSourceInfo struct {
	Protocol                            string            `json:"Protocol"`
	ID                                  string            `json:"Id"`
	Path                                string            `json:"Path,omitempty"`
	Type                                string            `json:"Type"`
	Container                           string            `json:"Container,omitempty"`
	Size                                int64             `json:"Size,omitempty"`
	Name                                string            `json:"Name,omitempty"`
	IsRemote                            bool              `json:"IsRemote"`
	ETag                                string            `json:"ETag,omitempty"`
	RunTimeTicks                        int64             `json:"RunTimeTicks,omitempty"`
	ReadAtNativeFramerate               bool              `json:"ReadAtNativeFramerate"`
	IgnoreDts                           bool              `json:"IgnoreDts"`
	IgnoreIndex                         bool              `json:"IgnoreIndex"`
	GenPtsInput                         bool              `json:"GenPtsInput"`
	SupportsTranscoding                 bool              `json:"SupportsTranscoding"`
	SupportsDirectStream                bool              `json:"SupportsDirectStream"`
	SupportsDirectPlay                  bool              `json:"SupportsDirectPlay"`
	IsInfiniteStream                    bool              `json:"IsInfiniteStream"`
	RequiresOpening                     bool              `json:"RequiresOpening"`
	RequiresClosing                     bool              `json:"RequiresClosing"`
	RequiresLooping                     bool              `json:"RequiresLooping"`
	SupportsProbing                     bool              `json:"SupportsProbing"`
	VideoType                           string            `json:"VideoType,omitempty"`
	MediaStreams                        []mediaStream     `json:"MediaStreams"`
	MediaAttachments                    []any             `json:"MediaAttachments"`
	Formats                             []string          `json:"Formats"`
	Bitrate                             int64             `json:"Bitrate,omitempty"`
	RequiredHTTPHeaders                 map[string]string `json:"RequiredHttpHeaders"`
	UseMostCompatibleTranscodingProfile bool              `json:"UseMostCompatibleTranscodingProfile"`
	HasSegments                         bool              `json:"HasSegments"`
	TranscodingURL                      string            `json:"TranscodingUrl,omitempty"`
	TranscodingSubProtocol              string            `json:"TranscodingSubProtocol"`
	TranscodingContainer                string            `json:"TranscodingContainer,omitempty"`
	DefaultAudioStreamIndex             *int              `json:"DefaultAudioStreamIndex,omitempty"`
	DefaultSubtitleStreamIndex          *int              `json:"DefaultSubtitleStreamIndex,omitempty"`
}

type mediaStream struct {
	Codec                    string  `json:"Codec,omitempty"`
	Language                 string  `json:"Language,omitempty"`
	ColorTransfer            string  `json:"ColorTransfer,omitempty"`
	ColorPrimaries           string  `json:"ColorPrimaries,omitempty"`
	ColorSpace               string  `json:"ColorSpace,omitempty"`
	TimeBase                 string  `json:"TimeBase,omitempty"`
	Title                    string  `json:"Title,omitempty"`
	VideoRange               string  `json:"VideoRange,omitempty"`
	VideoRangeType           string  `json:"VideoRangeType,omitempty"`
	DisplayTitle             string  `json:"DisplayTitle,omitempty"`
	NalLengthSize            string  `json:"NalLengthSize,omitempty"`
	IsInterlaced             bool    `json:"IsInterlaced"`
	IsAVC                    bool    `json:"IsAVC"`
	IsAnamorphic             bool    `json:"IsAnamorphic"`
	AudioSpatialFormat       string  `json:"AudioSpatialFormat"`
	LocalizedExternal        string  `json:"LocalizedExternal,omitempty"`
	ReferenceFrameRate       float32 `json:"ReferenceFrameRate,omitempty"`
	BitRate                  int64   `json:"BitRate,omitempty"`
	BitDepth                 int     `json:"BitDepth,omitempty"`
	RefFrames                int     `json:"RefFrames,omitempty"`
	IsDefault                bool    `json:"IsDefault"`
	IsForced                 bool    `json:"IsForced"`
	IsHearingImpaired        bool    `json:"IsHearingImpaired"`
	Height                   int     `json:"Height,omitempty"`
	Width                    int     `json:"Width,omitempty"`
	AverageFrameRate         float32 `json:"AverageFrameRate,omitempty"`
	RealFrameRate            float32 `json:"RealFrameRate,omitempty"`
	Profile                  string  `json:"Profile,omitempty"`
	Type                     string  `json:"Type"`
	AspectRatio              string  `json:"AspectRatio,omitempty"`
	Index                    int     `json:"Index"`
	IsExternal               bool    `json:"IsExternal"`
	DeliveryMethod           string  `json:"DeliveryMethod,omitempty"`
	DeliveryURL              string  `json:"DeliveryUrl,omitempty"`
	IsExternalURL            bool    `json:"IsExternalUrl"`
	IsTextSubtitleStream     bool    `json:"IsTextSubtitleStream"`
	SupportsExternalStream   bool    `json:"SupportsExternalStream"`
	PixelFormat              string  `json:"PixelFormat,omitempty"`
	Level                    float64 `json:"Level,omitempty"`
	Channels                 int     `json:"Channels,omitempty"`
	SampleRate               int     `json:"SampleRate,omitempty"`
	ChannelLayout            string  `json:"ChannelLayout,omitempty"`
	LocalizedDefault         string  `json:"LocalizedDefault,omitempty"`
	LocalizedForced          string  `json:"LocalizedForced,omitempty"`
	LocalizedUndefined       string  `json:"LocalizedUndefined,omitempty"`
	LocalizedHearingImpaired string  `json:"LocalizedHearingImpaired,omitempty"`
}

// deviceProfile is the client-capability document clients POST to
// PlaybackInfo. Only the parts the caps mapping reads are modeled.
type deviceProfile struct {
	MaxStreamingBitrate int64 `json:"MaxStreamingBitrate"`
	DirectPlayProfiles  []struct {
		Container  string `json:"Container"`
		AudioCodec string `json:"AudioCodec"`
		VideoCodec string `json:"VideoCodec"`
		Type       string `json:"Type"`
	} `json:"DirectPlayProfiles"`
	TranscodingProfiles []struct {
		Container  string `json:"Container"`
		Type       string `json:"Type"`
		AudioCodec string `json:"AudioCodec"`
		VideoCodec string `json:"VideoCodec"`
		Protocol   string `json:"Protocol"`
	} `json:"TranscodingProfiles"`
	CodecProfiles []struct {
		Type       string `json:"Type"`
		Codec      string `json:"Codec"`
		Conditions []struct {
			Condition string `json:"Condition"`
			Property  string `json:"Property"`
			Value     string `json:"Value"`
		} `json:"Conditions"`
	} `json:"CodecProfiles"`
}

type playbackInfoRequest struct {
	DeviceProfile *deviceProfile `json:"DeviceProfile"`
}

// playbackReport is the shared body shape of the three playstate endpoints
// (PlaybackStartInfo / PlaybackProgressInfo / PlaybackStopInfo — we read the
// common subset).
type playbackReport struct {
	ItemID        string `json:"ItemId"`
	MediaSourceID string `json:"MediaSourceId"`
	PositionTicks int64  `json:"PositionTicks"`
	PlaySessionID string `json:"PlaySessionId"`
	IsPaused      bool   `json:"IsPaused"`
	Failed        bool   `json:"Failed"`
}
