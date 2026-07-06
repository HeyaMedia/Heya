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
	// AbsoluteEpisodes holds anime absolute episode numbers ("Series - 24 -
	// Title") that carry no season. It's kept separate from Seasons/Episodes so
	// the read path can remap the absolute number to a real season/episode via
	// tv_episodes.absolute_number — without colliding with genuine season-0
	// specials. Empty for normal SxxExx releases.
	AbsoluteEpisodes []int `json:"absoluteEpisodes,omitempty"`
	IsTv             bool  `json:"isTv"`
	Score            int   `json:"score"`

	// Provider IDs embedded in the release name / path (e.g. "{imdb-tt0113198}"
	// or "[tmdbid=603]"). Empty when absent. A strong-match signal threaded into
	// the matcher alongside NFO IDs. See ParseProviderIDs.
	ImdbID string `json:"imdbId,omitempty"`
	TmdbID string `json:"tmdbId,omitempty"`
	TvdbID string `json:"tvdbId,omitempty"`

	// Anime provider ids, parked on the series folder in the anime layout
	// ("Series Title {anidb-2662}"). AnidbID is the authoritative signal for
	// absolute-numbered anime; the matcher searches on it directly.
	AnidbID   string `json:"anidbId,omitempty"`
	AnilistID string `json:"anilistId,omitempty"`
	MalID     string `json:"malId,omitempty"`

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

	// AnimeContext is set when the segment lives under a path carrying an anime
	// id tag ({anidb-…} etc.). It relaxes TV parsing to accept bracket-less
	// absolute-numbered episodes ("Series - 24 - Title") and suppresses the
	// movie parser (an anime path is never a movie). See PathLooksLikeAnime.
	AnimeContext bool
}

type NormalizedVideoCandidate struct {
	Candidate    string
	AnimeGroup   string
	ReleaseHash  string
	AnimeEpisode int
	DerivedTitle string
	VersionFlags []string
}
