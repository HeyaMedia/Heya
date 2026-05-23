package parser

type StorageEntryType string

const (
	EntryFile      StorageEntryType = "file"
	EntryDirectory StorageEntryType = "directory"
)

type StorageParseStatus string

const (
	StatusReady   StorageParseStatus = "ready"
	StatusFailed  StorageParseStatus = "failed"
	StatusPartial StorageParseStatus = "partial"
	StatusUnpack  StorageParseStatus = "unpack"
)

type SceneMediaKind string

const (
	MediaVideo   SceneMediaKind = "video"
	MediaAudio   SceneMediaKind = "audio"
	MediaBook    SceneMediaKind = "book"
	MediaUnknown SceneMediaKind = "unknown"
)

type SceneParserStrategy string

const (
	StrategyVideoFilenameParser SceneParserStrategy = "video-filename-parser"
	StrategyAudioHeuristic      SceneParserStrategy = "audio-heuristic"
	StrategyMusicCurated        SceneParserStrategy = "music-curated"
	StrategyBookHeuristic       SceneParserStrategy = "book-heuristic"
)

type SceneReleaseParse struct {
	Strategy       SceneParserStrategy `json:"strategy"`
	RawName        string              `json:"rawName"`
	NormalizedName string              `json:"normalizedName"`
	Media          SceneMediaKind      `json:"media"`
	Title          string              `json:"title"`
	Year           string              `json:"year,omitempty"`
	Group          string              `json:"group,omitempty"`
	ReleaseHash    string              `json:"releaseHash,omitempty"`
	Source         string              `json:"source,omitempty"`
	Sources        []string            `json:"sources"`
	Codec          string              `json:"codec,omitempty"`
	Codecs         []string            `json:"codecs"`
	Resolution     string              `json:"resolution,omitempty"`
	Catalog        string              `json:"catalog,omitempty"`
	Flags          []string            `json:"flags"`
	Seasons        []int               `json:"seasons"`
	Episodes       []int               `json:"episodes"`
	IsTv           bool                `json:"isTv"`
	Score          int                 `json:"score"`

	Artist               string `json:"artist,omitempty"`
	ArtistDisambiguation string `json:"artistDisambiguation,omitempty"`
	Album                string `json:"album,omitempty"`
	ReleaseKind          string `json:"releaseKind,omitempty"`
	DiscNumber           int    `json:"discNumber,omitempty"`
	TrackNumber          int    `json:"trackNumber,omitempty"`
	TrackTitle           string `json:"trackTitle,omitempty"`
	HasTrackInfo         bool   `json:"hasTrackInfo,omitempty"`
}

type ParsedStorageEntry struct {
	InputPath      string             `json:"inputPath"`
	NormalizedPath string             `json:"normalizedPath"`
	Basename       string             `json:"basename"`
	StorageRoot    string             `json:"storageRoot,omitempty"`
	Collection     string             `json:"collection,omitempty"`
	EntryType      StorageEntryType   `json:"entryType"`
	Extension      string             `json:"extension,omitempty"`
	Status         StorageParseStatus `json:"status"`
	Media          SceneMediaKind     `json:"media"`
	Release        *SceneReleaseParse `json:"release,omitempty"`
	ReleaseSegment string             `json:"releaseSegment,omitempty"`
}

type PreparedSegment struct {
	RawName     string
	CleanedName string
	Extension   string
	Flags       []string
}

type NormalizedVideoCandidate struct {
	Candidate    string
	AnimeGroup   string
	ReleaseHash  string
	AnimeEpisode int
	DerivedTitle string
	VersionFlags []string
}
