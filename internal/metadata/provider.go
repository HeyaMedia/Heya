package metadata

import "context"

type MediaKind string

const (
	KindMovie MediaKind = "movie"
	KindTV    MediaKind = "tv"
	KindMusic MediaKind = "music"
	KindBook  MediaKind = "book"
)

type SearchQuery struct {
	Title   string
	Year    string
	Artist  string
	Album   string
	Author  string
	ISBN    string
	Seasons []int
}

type SearchResult struct {
	ProviderID   string  `json:"provider_id"`
	ProviderName string  `json:"provider_name"`
	Title        string  `json:"title"`
	Year         string  `json:"year"`
	Description  string  `json:"description"`
	PosterURL    string  `json:"poster_url"`
	Confidence   float64 `json:"confidence"`
	RawData      any     `json:"-"`
}

type MediaDetail struct {
	Title       string            `json:"title"`
	SortTitle   string            `json:"sort_title"`
	Year        string            `json:"year"`
	Description string            `json:"description"`
	PosterURL   string            `json:"poster_url"`
	BackdropURL string            `json:"backdrop_url"`
	ExternalIDs map[string]string `json:"external_ids"`
	Genres      []string          `json:"genres"`
	Rating      float64           `json:"rating"`

	// Movie
	RuntimeMinutes      int      `json:"runtime_minutes,omitempty"`
	Tagline             string   `json:"tagline,omitempty"`
	ReleaseDate         string   `json:"release_date,omitempty"`
	OriginalTitle       string   `json:"original_title,omitempty"`
	OriginalLanguage    string   `json:"original_language,omitempty"`
	Budget              int64    `json:"budget,omitempty"`
	Revenue             int64    `json:"revenue,omitempty"`
	Popularity          float64  `json:"popularity,omitempty"`
	VoteCount           int      `json:"vote_count,omitempty"`
	ProductionCompanies []string `json:"production_companies,omitempty"`
	Cast                []CastMember `json:"cast,omitempty"`
	Crew                []CrewMember `json:"crew,omitempty"`

	// TV
	Status           string          `json:"status,omitempty"`
	FirstAirDate     string          `json:"first_air_date,omitempty"`
	LastAirDate      string          `json:"last_air_date,omitempty"`
	OriginalName     string          `json:"original_name,omitempty"`
	Networks         []string        `json:"networks,omitempty"`
	CreatedBy        []string        `json:"created_by,omitempty"`
	NumberOfSeasons  int             `json:"number_of_seasons,omitempty"`
	NumberOfEpisodes int             `json:"number_of_episodes,omitempty"`
	Seasons          []SeasonDetail  `json:"seasons,omitempty"`

	// Music
	ArtistName string        `json:"artist_name,omitempty"`
	AlbumTitle string        `json:"album_title,omitempty"`
	AlbumType  string        `json:"album_type,omitempty"`
	Label      string        `json:"label,omitempty"`
	Country    string        `json:"country,omitempty"`
	Barcode    string        `json:"barcode,omitempty"`
	TotalDiscs int           `json:"total_discs,omitempty"`
	Tags       []string      `json:"tags,omitempty"`
	CoverURL   string        `json:"cover_url,omitempty"`
	Tracks     []TrackDetail `json:"tracks,omitempty"`

	// Book
	AuthorName  string   `json:"author_name,omitempty"`
	ISBN        string   `json:"isbn,omitempty"`
	PageCount   int      `json:"page_count,omitempty"`
	Publisher   string   `json:"publisher,omitempty"`
	PublishDate string   `json:"publish_date,omitempty"`
	Subjects    []string `json:"subjects,omitempty"`
	Language    string   `json:"language,omitempty"`
	SeriesName  string   `json:"series_name,omitempty"`
	SeriesNum   int      `json:"series_num,omitempty"`
}

type CastMember struct {
	Name        string `json:"name"`
	Character   string `json:"character"`
	Order       int    `json:"order"`
	ProfilePath string `json:"profile_path"`
}

type CrewMember struct {
	Name       string `json:"name"`
	Job        string `json:"job"`
	Department string `json:"department"`
}

type SeasonDetail struct {
	Number    int             `json:"number"`
	Title     string          `json:"title"`
	Overview  string          `json:"overview"`
	PosterURL string          `json:"poster_url"`
	AirDate   string          `json:"air_date"`
	Episodes  []EpisodeDetail `json:"episodes"`
}

type EpisodeDetail struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	Overview       string `json:"overview"`
	StillURL       string `json:"still_url"`
	RuntimeMinutes int    `json:"runtime_minutes"`
	AirDate        string `json:"air_date"`
}

type TrackDetail struct {
	DiscNumber  int    `json:"disc_number"`
	TrackNumber int    `json:"track_number"`
	Title       string `json:"title"`
	DurationMs  int    `json:"duration_ms"`
}

type Provider interface {
	Name() string
	Supports(kind MediaKind) bool
	Search(ctx context.Context, kind MediaKind, query SearchQuery) ([]SearchResult, error)
	GetDetail(ctx context.Context, providerID string) (*MediaDetail, error)
}
