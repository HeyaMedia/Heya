package metadata

type MediaKind string

const (
	KindMovie MediaKind = "movie"
	KindTV    MediaKind = "tv"
	KindMusic MediaKind = "music"
	KindBook  MediaKind = "book"
)

type SearchQuery struct {
	Title    string
	Year     string
	Artist   string
	Album    string
	Author   string
	ISBN     string
	Seasons  []int
	Language string
	Country  string
}

type FetchOptions struct {
	Language string
	Country  string
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

type TitleEntry struct {
	Title     string `json:"title"`
	Language  string `json:"language"`
	Country   string `json:"country,omitempty"`
	TitleType string `json:"type"`
	Source    string `json:"source,omitempty"`
}

type MediaDetail struct {
	Title        string            `json:"title"`
	SortTitle    string            `json:"sort_title"`
	Year         string            `json:"year"`
	Description  string            `json:"description"`
	Titles       []TitleEntry      `json:"titles,omitempty"`
	Overviews    map[string]string `json:"overviews,omitempty"`
	PosterURL    string            `json:"poster_url"`
	BackdropURL  string            `json:"backdrop_url"`
	ExternalIDs  map[string]string `json:"external_ids"`
	Genres       []string          `json:"genres"`
	Rating       float64           `json:"rating"`
	ProviderKind string            `json:"provider_kind,omitempty"`
	HeyaSlug     string            `json:"heya_slug,omitempty"`

	// Movie
	RuntimeMinutes      int                       `json:"runtime_minutes,omitempty"`
	Tagline             string                    `json:"tagline,omitempty"`
	ReleaseDate         string                    `json:"release_date,omitempty"`
	OriginalTitle       string                    `json:"original_title,omitempty"`
	OriginalLanguage    string                    `json:"original_language,omitempty"`
	Budget              int64                     `json:"budget,omitempty"`
	Revenue             int64                     `json:"revenue,omitempty"`
	Popularity          float64                   `json:"popularity,omitempty"`
	VoteCount           int                       `json:"vote_count,omitempty"`
	ProductionCompanies []ProductionCompanyDetail `json:"production_companies,omitempty"`
	Cast                []CastMember              `json:"cast,omitempty"`
	Crew                []CrewMember              `json:"crew,omitempty"`
	Keywords            []KeywordDetail           `json:"keywords,omitempty"`
	Videos              []VideoDetail             `json:"videos,omitempty"`
	Certifications      []CertificationDetail     `json:"certifications,omitempty"`
	Recommendations     []RecommendationDetail    `json:"recommendations,omitempty"`
	Collection          *CollectionDetail         `json:"collection,omitempty"`
	Homepage            string                    `json:"homepage,omitempty"`
	SpokenLanguages     []string                  `json:"spoken_languages,omitempty"`
	OriginCountry       []string                  `json:"origin_country,omitempty"`
	MovieStatus         string                    `json:"movie_status,omitempty"`

	// TV
	Status           string          `json:"status,omitempty"`
	FirstAirDate     string          `json:"first_air_date,omitempty"`
	LastAirDate      string          `json:"last_air_date,omitempty"`
	OriginalName     string          `json:"original_name,omitempty"`
	Networks         []NetworkDetail `json:"networks,omitempty"`
	CreatedBy        []CreatorDetail `json:"created_by,omitempty"`
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
	ArtistBio            string       `json:"artist_bio,omitempty"`
	Albums               []AlbumEntry `json:"albums,omitempty"`
	ArtistSortName       string       `json:"artist_sort_name,omitempty"`
	ArtistDisambiguation string       `json:"artist_disambiguation,omitempty"`
	ArtistNativeName     string       `json:"artist_native_name,omitempty"`
	ArtistNativeLanguage string       `json:"artist_native_language,omitempty"`
	ArtistCountry        string       `json:"artist_country,omitempty"`
	ArtistType           string       `json:"artist_type,omitempty"` // "Person" | "Group"
	ArtistGender         string       `json:"artist_gender,omitempty"`
	ArtistBeginDate      string       `json:"artist_begin_date,omitempty"`
	ArtistBeginYear      int          `json:"artist_begin_year,omitempty"`
	ArtistBirthplace     string       `json:"artist_birthplace,omitempty"`
}

type ArtworkResult struct {
	URL       string  `json:"url"`
	AssetType string  `json:"asset_type"`
	Language  string  `json:"language,omitempty"`
	Source    string  `json:"source,omitempty"`
	Likes     int     `json:"likes,omitempty"`
	Score     float64 `json:"score,omitempty"`
	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	Aspect    string  `json:"aspect,omitempty"`
}

type NetworkDetail struct {
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	Name        string            `json:"name"`
	LogoPath    string            `json:"logo_path,omitempty"`
	Country     string            `json:"country,omitempty"`
}

type CreatorDetail struct {
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	Name        string            `json:"name"`
}

// ProfileImage is a single profile / headshot image for a person, carrying
// the full set of attributes the upstream payload provides (source, size,
// score, etc.) so we can persist all of them per-person.
type ProfileImage struct {
	URL    string  `json:"url"`
	Source string  `json:"source,omitempty"`
	Aspect string  `json:"aspect,omitempty"`
	Width  int     `json:"width,omitempty"`
	Height int     `json:"height,omitempty"`
	Score  float64 `json:"score,omitempty"`
	Likes  int     `json:"likes,omitempty"`
}

type CastMember struct {
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	Name        string            `json:"name"`
	Character   string            `json:"character"`
	Order       int               `json:"order"`
	Gender      int               `json:"gender"`
	ProfilePath string            `json:"profile_path"`
	Profiles    []ProfileImage    `json:"profiles,omitempty"`
	Popularity  float64           `json:"popularity"`
	Source      string            `json:"source,omitempty"`
}

type CrewMember struct {
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	Name        string            `json:"name"`
	Job         string            `json:"job"`
	Department  string            `json:"department"`
	Gender      int               `json:"gender"`
	ProfilePath string            `json:"profile_path"`
	Profiles    []ProfileImage    `json:"profiles,omitempty"`
	Source      string            `json:"source,omitempty"`
}

type KeywordDetail struct {
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	Name        string            `json:"name"`
}

type VideoDetail struct {
	ProviderKey string `json:"provider_key"`
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
	Source        string `json:"source,omitempty"`
}

type RecommendationDetail struct {
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	Title       string            `json:"title"`
	PosterPath  string            `json:"poster_path"`
	MediaType   string            `json:"media_type"`
	VoteAverage float64           `json:"vote_average"`
	ReleaseDate string            `json:"release_date,omitempty"`
}

type CollectionDetail struct {
	ExternalIDs  map[string]string `json:"external_ids,omitempty"`
	Name         string            `json:"name"`
	Overview     string            `json:"overview"`
	PosterPath   string            `json:"poster_path"`
	BackdropPath string            `json:"backdrop_path"`
}

type ProductionCompanyDetail struct {
	ExternalIDs   map[string]string `json:"external_ids,omitempty"`
	Name          string            `json:"name"`
	LogoPath      string            `json:"logo_path"`
	OriginCountry string            `json:"origin_country"`
}

type SeasonDetail struct {
	Number        int             `json:"number"`
	Title         string          `json:"title"`
	Overview      string          `json:"overview"`
	PosterURL     string          `json:"poster_url"`
	AirDate       string          `json:"air_date"`
	EndDate       string          `json:"end_date,omitempty"`
	Status        string          `json:"status,omitempty"`
	AiredEpisodes int             `json:"aired_episodes,omitempty"`
	TmdbSeasonID  int             `json:"tmdb_season_id,omitempty"`
	TvdbSeasonID  int             `json:"tvdb_season_id,omitempty"`
	AnidbID       int             `json:"anidb_id,omitempty"`
	Episodes      []EpisodeDetail `json:"episodes"`
}

type EpisodeDetail struct {
	Number         int               `json:"number"`
	Title          string            `json:"title"`
	Titles         []TitleEntry      `json:"titles,omitempty"`
	Overview       string            `json:"overview"`
	Overviews      map[string]string `json:"overviews,omitempty"`
	StillURL       string            `json:"still_url"`
	RuntimeMinutes int               `json:"runtime_minutes"`
	AirDate        string            `json:"air_date"`
	Rating         float64           `json:"rating,omitempty"`
	VoteCount      int               `json:"vote_count,omitempty"`
	AbsoluteNumber int               `json:"absolute_number,omitempty"`
	IsSpecial      bool              `json:"is_special,omitempty"`
	EpisodeType    int               `json:"episode_type,omitempty"`
	TmdbID         int               `json:"tmdb_id,omitempty"`
	TvdbID         int               `json:"tvdb_id,omitempty"`
	Source         string            `json:"source,omitempty"`
}

type TrackDetail struct {
	DiscNumber  int    `json:"disc_number"`
	TrackNumber int    `json:"track_number"`
	Title       string `json:"title"`
	// Duration in seconds (heya.media's native unit).
	Duration      int    `json:"duration"`
	ISRC          string `json:"isrc,omitempty"`
	RecordingMBID string `json:"recording_mbid,omitempty"`
	PreviewURL    string `json:"preview_url,omitempty"`
}

// AlbumEntry is one album as returned in payload.albums on an artist lookup.
// Carries full track listing — no extra request needed to enrich tracks.
type AlbumEntry struct {
	Title       string            `json:"title"`
	Type        string            `json:"type"` // "album" | "single" | "ep" | "compilation"
	ReleaseDate string            `json:"release_date,omitempty"`
	Year        int               `json:"year,omitempty"`
	Label       string            `json:"label,omitempty"`
	CatalogNo   string            `json:"catalog_no,omitempty"`
	Country     string            `json:"country,omitempty"`
	Barcode     string            `json:"barcode,omitempty"`
	ISRCs       []string          `json:"isrcs,omitempty"`
	ExternalIDs map[string]string `json:"external_ids,omitempty"`
	TrackCount  int               `json:"track_count,omitempty"`
	Popularity  float64           `json:"popularity,omitempty"`
	CoverURL    string            `json:"cover_url,omitempty"`
	Tracks      []TrackDetail     `json:"tracks,omitempty"`
}

type NFOIDs struct {
	TMDBID string
	IMDBID string
	TVDBID string
	MBID   string
}

type RatingsData struct {
	Ratings   []ExternalRating `json:"ratings"`
	Awards    string           `json:"awards,omitempty"`
	BoxOffice string           `json:"box_office,omitempty"`
}

type ExternalRating struct {
	Source   string  `json:"source"`
	Value    string  `json:"value"`
	Score    float64 `json:"score"`
	Votes    int     `json:"votes,omitempty"`
	RawValue string  `json:"raw_value,omitempty"`
}
