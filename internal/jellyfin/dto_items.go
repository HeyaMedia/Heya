package jellyfin

import "time"

// baseItemDto is the hand-written subset of Jellyfin's 153-field BaseItemDto
// that shipping clients actually read. Optional scalars are pointers so
// absent stays absent (clients distinguish "no rating" from 0); slices and
// maps that clients index unconditionally are always emitted. Optional dates
// are pointers, NOT omitzero: the goccy codec ignores that tag and a
// serialized zero time renders as year 1 in clients ("2009 - 1901" on
// series cards — caught via jellyfin-web).
type baseItemDto struct {
	Name              string       `json:"Name"`
	OriginalTitle     string       `json:"OriginalTitle,omitempty"`
	ServerID          string       `json:"ServerId"`
	ID                string       `json:"Id"`
	Etag              string       `json:"Etag,omitempty"`
	DateCreated       *time.Time   `json:"DateCreated,omitempty"`
	CanDelete         bool         `json:"CanDelete"`
	CanDownload       bool         `json:"CanDownload"`
	SortName          string       `json:"SortName,omitempty"`
	PremiereDate      *time.Time   `json:"PremiereDate,omitempty"`
	EndDate           *time.Time   `json:"EndDate,omitempty"`
	Overview          string       `json:"Overview,omitempty"`
	Taglines          []string     `json:"Taglines"`
	Genres            []string     `json:"Genres"`
	CommunityRating   *float32     `json:"CommunityRating,omitempty"`
	RunTimeTicks      *int64       `json:"RunTimeTicks,omitempty"`
	ProductionYear    *int32       `json:"ProductionYear,omitempty"`
	IndexNumber       *int32       `json:"IndexNumber,omitempty"`
	ParentIndexNumber *int32       `json:"ParentIndexNumber,omitempty"`
	IsFolder          bool         `json:"IsFolder"`
	Type              string       `json:"Type"`
	ParentID          string       `json:"ParentId,omitempty"`
	UserData          *userDataDto `json:"UserData,omitempty"`
	ChildCount        *int32       `json:"ChildCount,omitempty"`
	Status            string       `json:"Status,omitempty"`

	SeriesName            string `json:"SeriesName,omitempty"`
	SeriesID              string `json:"SeriesId,omitempty"`
	SeasonID              string `json:"SeasonId,omitempty"`
	SeasonName            string `json:"SeasonName,omitempty"`
	SeriesPrimaryImageTag string `json:"SeriesPrimaryImageTag,omitempty"`

	Album                string         `json:"Album,omitempty"`
	AlbumID              string         `json:"AlbumId,omitempty"`
	AlbumPrimaryImageTag string         `json:"AlbumPrimaryImageTag,omitempty"`
	AlbumArtist          string         `json:"AlbumArtist,omitempty"`
	AlbumArtists         []nameGuidPair `json:"AlbumArtists,omitempty"`
	Artists              []string       `json:"Artists,omitempty"`
	ArtistItems          []nameGuidPair `json:"ArtistItems,omitempty"`

	CollectionType string `json:"CollectionType,omitempty"`

	MediaType               string            `json:"MediaType"`
	LocationType            string            `json:"LocationType"`
	ProviderIds             map[string]string `json:"ProviderIds"`
	ImageTags               map[string]string `json:"ImageTags"`
	BackdropImageTags       []string          `json:"BackdropImageTags"`
	PrimaryImageAspectRatio *float64          `json:"PrimaryImageAspectRatio,omitempty"`

	// Always-present arrays: upstream serializes these on every full dto and
	// jellyfin-web 10.8's detail page reads .length on them unguarded — their
	// absence hangs the page on a TypeError (found via the CDP console tap).
	People              []any          `json:"People"`
	Studios             []nameGuidPair `json:"Studios"`
	GenreItems          []nameGuidPair `json:"GenreItems"`
	Tags                []string       `json:"Tags"`
	ExternalUrls        []externalURL  `json:"ExternalUrls"`
	RemoteTrailers      []any          `json:"RemoteTrailers"`
	ProductionLocations []string       `json:"ProductionLocations"`
	LockedFields        []string       `json:"LockedFields"`

	// Scaffolding upstream emits on every dto — surfaced by the structural
	// diff against a real 10.11 server (tools jf-diff harness). Strict
	// decoders (Infuse) require several of these to exist.
	ChannelID                any                          `json:"ChannelId"` // always null for library items, like upstream
	PlayAccess               string                       `json:"PlayAccess"`
	EnableMediaSourceDisplay bool                         `json:"EnableMediaSourceDisplay"`
	LocalTrailerCount        int                          `json:"LocalTrailerCount"`
	SpecialFeatureCount      int                          `json:"SpecialFeatureCount"`
	DisplayPreferencesID     string                       `json:"DisplayPreferencesId"`
	LockData                 bool                         `json:"LockData"`
	ImageBlurHashes          map[string]map[string]string `json:"ImageBlurHashes"`
	Path                     string                       `json:"Path,omitempty"`
	DateLastMediaAdded       *time.Time                   `json:"DateLastMediaAdded,omitempty"`

	// Full-detail extras: playable video items carry their MediaSources on
	// /Items/{id}, exactly like upstream — Infuse builds its "can I play
	// this" decision from the detail response, not PlaybackInfo alone.
	// MediaStreams mirrors the primary source's streams at the top level,
	// again matching upstream's detail shape.
	MediaSources []mediaSourceInfo `json:"MediaSources,omitempty"`
	MediaStreams []mediaStream     `json:"MediaStreams,omitempty"`
	Container    string            `json:"Container,omitempty"`
	VideoType    string            `json:"VideoType,omitempty"`
	IsHD         *bool             `json:"IsHD,omitempty"`
	Width        int               `json:"Width,omitempty"`
	Height       int               `json:"Height,omitempty"`
	// No omitempty — empty slice/map must still serialize ([]/{}), which
	// omitempty would drop. Filled in done().
	Chapters  []any          `json:"Chapters"`
	Trickplay map[string]any `json:"Trickplay"`
}

type externalURL struct {
	Name string `json:"Name"`
	URL  string `json:"Url"`
}

type nameGuidPair struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
}

// userDataDto mirrors Jellyfin's UserItemDataDto. Key is an opaque per-item
// cache key clients persist; ours is the item id.
type userDataDto struct {
	PlaybackPositionTicks int64      `json:"PlaybackPositionTicks"`
	PlayCount             int32      `json:"PlayCount"`
	IsFavorite            bool       `json:"IsFavorite"`
	Played                bool       `json:"Played"`
	PlayedPercentage      *float64   `json:"PlayedPercentage,omitempty"`
	UnplayedItemCount     *int32     `json:"UnplayedItemCount,omitempty"`
	LastPlayedDate        *time.Time `json:"LastPlayedDate,omitempty"`
	ItemID                string     `json:"ItemId"`
	Key                   string     `json:"Key"`
}

const ticksPerSecond int64 = 10_000_000

func minutesToTicks(min int32) *int64 {
	if min <= 0 {
		return nil
	}
	t := int64(min) * 60 * ticksPerSecond
	return &t
}

func secondsToTicks(sec int32) int64 { return int64(sec) * ticksPerSecond }

var (
	aspectPoster = 2.0 / 3.0
	aspectStill  = 16.0 / 9.0
	aspectSquare = 1.0
)
