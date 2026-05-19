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
	ProductionCompanies []ProductionCompanyDetail `json:"production_companies,omitempty"`
	Cast                []CastMember              `json:"cast,omitempty"`
	Crew                []CrewMember              `json:"crew,omitempty"`
	Keywords            []KeywordDetail            `json:"keywords,omitempty"`
	Videos              []VideoDetail              `json:"videos,omitempty"`
	Certifications      []CertificationDetail      `json:"certifications,omitempty"`
	Recommendations     []RecommendationDetail     `json:"recommendations,omitempty"`
	Collection          *CollectionDetail           `json:"collection,omitempty"`
	Homepage            string                     `json:"homepage,omitempty"`
	SpokenLanguages     []string                   `json:"spoken_languages,omitempty"`
	OriginCountry       []string                   `json:"origin_country,omitempty"`
	MovieStatus         string                     `json:"movie_status,omitempty"`
	WikidataID          string                     `json:"wikidata_id,omitempty"`
	FacebookID          string                     `json:"facebook_id,omitempty"`
	InstagramID         string                     `json:"instagram_id,omitempty"`
	TwitterID           string                     `json:"twitter_id,omitempty"`

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
	AuthorName      string   `json:"author_name,omitempty"`
	AuthorBio       string   `json:"author_bio,omitempty"`
	AuthorBirthDate string   `json:"author_birth_date,omitempty"`
	AuthorDeathDate string   `json:"author_death_date,omitempty"`
	ISBN            string   `json:"isbn,omitempty"`
	PageCount       int      `json:"page_count,omitempty"`
	Publisher       string   `json:"publisher,omitempty"`
	PublishDate     string   `json:"publish_date,omitempty"`
	Subjects        []string `json:"subjects,omitempty"`
	Language        string   `json:"language,omitempty"`
	SeriesName      string   `json:"series_name,omitempty"`
	SeriesNum       int      `json:"series_num,omitempty"`

	// Music (extra)
	ArtistBio string `json:"artist_bio,omitempty"`
}

type ArtworkProvider interface {
	Name() string
	FetchArtwork(ctx context.Context, kind MediaKind, externalIDs map[string]string) ([]ArtworkResult, error)
}

type ArtworkResult struct {
	URL       string `json:"url"`
	AssetType string `json:"asset_type"`
	Language  string `json:"language,omitempty"`
	Likes     int    `json:"likes,omitempty"`
}

type CastMember struct {
	TmdbID      int    `json:"tmdb_id"`
	Name        string `json:"name"`
	Character   string `json:"character"`
	Order       int    `json:"order"`
	Gender      int    `json:"gender"`
	ProfilePath string `json:"profile_path"`
	Popularity  float64 `json:"popularity"`
}

type CrewMember struct {
	TmdbID      int    `json:"tmdb_id"`
	Name        string `json:"name"`
	Job         string `json:"job"`
	Department  string `json:"department"`
	Gender      int    `json:"gender"`
	ProfilePath string `json:"profile_path"`
}

type KeywordDetail struct {
	TmdbID int    `json:"tmdb_id"`
	Name   string `json:"name"`
}

type VideoDetail struct {
	TmdbKey     string `json:"tmdb_key"`
	Name        string `json:"name"`
	Site        string `json:"site"`
	Key         string `json:"key"`
	Type        string `json:"type"`
	Language    string `json:"language"`
	Official    bool   `json:"official"`
	PublishedAt string `json:"published_at,omitempty"`
}

type CertificationDetail struct {
	Country       string `json:"country"`
	Certification string `json:"certification"`
	ReleaseDate   string `json:"release_date,omitempty"`
	ReleaseType   int    `json:"release_type"`
}

type RecommendationDetail struct {
	TmdbID      int     `json:"tmdb_id"`
	Title       string  `json:"title"`
	PosterPath  string  `json:"poster_path"`
	MediaType   string  `json:"media_type"`
	VoteAverage float64 `json:"vote_average"`
	ReleaseDate string  `json:"release_date,omitempty"`
}

type CollectionDetail struct {
	TmdbID       int    `json:"tmdb_id"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
}

type ProductionCompanyDetail struct {
	TmdbID        int    `json:"tmdb_id"`
	Name          string `json:"name"`
	LogoPath      string `json:"logo_path"`
	OriginCountry string `json:"origin_country"`
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
	Number         int     `json:"number"`
	Title          string  `json:"title"`
	Overview       string  `json:"overview"`
	StillURL       string  `json:"still_url"`
	RuntimeMinutes int     `json:"runtime_minutes"`
	AirDate        string  `json:"air_date"`
	Rating         float64 `json:"rating,omitempty"`
	VoteCount      int     `json:"vote_count,omitempty"`
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

type NFOIDs struct {
	TMDBID string
	IMDBID string
	TVDBID string
	MBID   string
}

type DirectLookupProvider interface {
	Provider
	LookupByNFO(ctx context.Context, kind MediaKind, ids NFOIDs) (*MediaDetail, string, error)
}
