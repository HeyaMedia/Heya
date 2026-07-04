package jellyfin

import "time"

// baseItemDto is the hand-written subset of Jellyfin's 153-field BaseItemDto
// that shipping clients actually read. Optional scalars are pointers/omitzero
// so absent stays absent (clients distinguish "no rating" from 0); slices and
// maps that clients index unconditionally are always emitted.
type baseItemDto struct {
	Name              string       `json:"Name"`
	OriginalTitle     string       `json:"OriginalTitle,omitempty"`
	ServerID          string       `json:"ServerId"`
	ID                string       `json:"Id"`
	Etag              string       `json:"Etag,omitempty"`
	DateCreated       time.Time    `json:"DateCreated,omitzero"`
	CanDelete         bool         `json:"CanDelete"`
	CanDownload       bool         `json:"CanDownload"`
	SortName          string       `json:"SortName,omitempty"`
	PremiereDate      time.Time    `json:"PremiereDate,omitzero"`
	EndDate           time.Time    `json:"EndDate,omitzero"`
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
	ProviderIds             map[string]string `json:"ProviderIds,omitempty"`
	ImageTags               map[string]string `json:"ImageTags"`
	BackdropImageTags       []string          `json:"BackdropImageTags"`
	PrimaryImageAspectRatio *float64          `json:"PrimaryImageAspectRatio,omitempty"`
}

type nameGuidPair struct {
	Name string `json:"Name"`
	ID   string `json:"Id"`
}

// userDataDto mirrors Jellyfin's UserItemDataDto. Key is an opaque per-item
// cache key clients persist; ours is the item id.
type userDataDto struct {
	PlaybackPositionTicks int64     `json:"PlaybackPositionTicks"`
	PlayCount             int32     `json:"PlayCount"`
	IsFavorite            bool      `json:"IsFavorite"`
	Played                bool      `json:"Played"`
	PlayedPercentage      *float64  `json:"PlayedPercentage,omitempty"`
	UnplayedItemCount     *int32    `json:"UnplayedItemCount,omitempty"`
	LastPlayedDate        time.Time `json:"LastPlayedDate,omitzero"`
	Key                   string    `json:"Key"`
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
