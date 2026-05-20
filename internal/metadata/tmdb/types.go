package tmdb

type searchMovieResponse struct {
	Page         int           `json:"page"`
	TotalResults int           `json:"total_results"`
	Results      []movieResult `json:"results"`
}

type movieResult struct {
	ID               int     `json:"id"`
	Title            string  `json:"title"`
	OriginalTitle    string  `json:"original_title"`
	OriginalLanguage string  `json:"original_language"`
	Overview         string  `json:"overview"`
	ReleaseDate      string  `json:"release_date"`
	PosterPath       string  `json:"poster_path"`
	BackdropPath     string  `json:"backdrop_path"`
	Popularity       float64 `json:"popularity"`
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int     `json:"vote_count"`
	GenreIDs         []int   `json:"genre_ids"`
}

type movieDetail struct {
	ID                  int                  `json:"id"`
	Title               string               `json:"title"`
	OriginalTitle       string               `json:"original_title"`
	OriginalLanguage    string               `json:"original_language"`
	Overview            string               `json:"overview"`
	Tagline             string               `json:"tagline"`
	Homepage            string               `json:"homepage"`
	Status              string               `json:"status"`
	ReleaseDate         string               `json:"release_date"`
	Runtime             int                  `json:"runtime"`
	Budget              int64                `json:"budget"`
	Revenue             int64                `json:"revenue"`
	Popularity          float64              `json:"popularity"`
	VoteAverage         float64              `json:"vote_average"`
	VoteCount           int                  `json:"vote_count"`
	PosterPath          string               `json:"poster_path"`
	BackdropPath        string               `json:"backdrop_path"`
	Genres              []genreEntry         `json:"genres"`
	ProductionCompanies []productionCo       `json:"production_companies"`
	SpokenLanguages     []spokenLanguage     `json:"spoken_languages"`
	OriginCountry       []string             `json:"origin_country"`
	Collection          *collectionRef       `json:"belongs_to_collection"`
	Credits             creditsResponse      `json:"credits"`
	ExternalIDs         externalIDsResult    `json:"external_ids"`
	Keywords            keywordsResponse     `json:"keywords"`
	Videos              videosResponse       `json:"videos"`
	ReleaseDates        releaseDatesResponse `json:"release_dates"`
	Recommendations     recommendResponse    `json:"recommendations"`
}

type searchTVResponse struct {
	Page         int        `json:"page"`
	TotalResults int        `json:"total_results"`
	Results      []tvResult `json:"results"`
}

type tvResult struct {
	ID               int     `json:"id"`
	Name             string  `json:"name"`
	OriginalName     string  `json:"original_name"`
	OriginalLanguage string  `json:"original_language"`
	Overview         string  `json:"overview"`
	FirstAirDate     string  `json:"first_air_date"`
	PosterPath       string  `json:"poster_path"`
	BackdropPath     string  `json:"backdrop_path"`
	Popularity       float64 `json:"popularity"`
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int     `json:"vote_count"`
	GenreIDs         []int   `json:"genre_ids"`
}

type tvDetail struct {
	ID               int                    `json:"id"`
	Name             string                 `json:"name"`
	OriginalName     string                 `json:"original_name"`
	OriginalLanguage string                 `json:"original_language"`
	Overview         string                 `json:"overview"`
	FirstAirDate     string                 `json:"first_air_date"`
	LastAirDate      string                 `json:"last_air_date"`
	Status           string                 `json:"status"`
	NumberOfSeasons  int                    `json:"number_of_seasons"`
	NumberOfEpisodes int                    `json:"number_of_episodes"`
	Popularity       float64                `json:"popularity"`
	VoteAverage      float64                `json:"vote_average"`
	VoteCount        int                    `json:"vote_count"`
	PosterPath       string                 `json:"poster_path"`
	BackdropPath     string                 `json:"backdrop_path"`
	Genres           []genreEntry           `json:"genres"`
	Networks         []networkEntry         `json:"networks"`
	CreatedBy        []creatorEntry         `json:"created_by"`
	Seasons          []seasonEntry          `json:"seasons"`
	Credits          creditsResponse        `json:"credits"`
	ExternalIDs      externalIDsResult      `json:"external_ids"`
	Keywords         tvKeywordsResponse     `json:"keywords"`
	Videos           videosResponse         `json:"videos"`
	ContentRatings   contentRatingsResponse `json:"content_ratings"`
	Recommendations  recommendResponse      `json:"recommendations"`
	ProductionCompanies []productionCo      `json:"production_companies"`
}

type tvKeywordsResponse struct {
	Results []keywordEntry `json:"results"`
}

type contentRatingsResponse struct {
	Results []contentRatingEntry `json:"results"`
}

type contentRatingEntry struct {
	Country string `json:"iso_3166_1"`
	Rating  string `json:"rating"`
}

type seasonDetail struct {
	SeasonNumber int            `json:"season_number"`
	Name         string         `json:"name"`
	Overview     string         `json:"overview"`
	PosterPath   string         `json:"poster_path"`
	AirDate      string         `json:"air_date"`
	Episodes     []episodeEntry `json:"episodes"`
}

type episodeEntry struct {
	EpisodeNumber int     `json:"episode_number"`
	Name          string  `json:"name"`
	Overview      string  `json:"overview"`
	StillPath     string  `json:"still_path"`
	Runtime       int     `json:"runtime"`
	AirDate       string  `json:"air_date"`
	VoteAverage   float64 `json:"vote_average"`
}

type seasonEntry struct {
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	PosterPath   string `json:"poster_path"`
	AirDate      string `json:"air_date"`
	EpisodeCount int    `json:"episode_count"`
}

type genreEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type productionCo struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	LogoPath      string `json:"logo_path"`
	OriginCountry string `json:"origin_country"`
}

type spokenLanguage struct {
	EnglishName string `json:"english_name"`
	ISO639      string `json:"iso_639_1"`
	Name        string `json:"name"`
}

type networkEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type creatorEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type creditsResponse struct {
	Cast []castEntry `json:"cast"`
	Crew []crewEntry `json:"crew"`
}

type castEntry struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Character   string  `json:"character"`
	Order       int     `json:"order"`
	Gender      int     `json:"gender"`
	ProfilePath string  `json:"profile_path"`
	Popularity  float64 `json:"popularity"`
}

type crewEntry struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Job         string `json:"job"`
	Department  string `json:"department"`
	Gender      int    `json:"gender"`
	ProfilePath string `json:"profile_path"`
}

type externalIDsResult struct {
	IMDBID      string `json:"imdb_id"`
	TVDBID      int    `json:"tvdb_id"`
	WikidataID  string `json:"wikidata_id"`
	FacebookID  string `json:"facebook_id"`
	InstagramID string `json:"instagram_id"`
	TwitterID   string `json:"twitter_id"`
}

type keywordsResponse struct {
	Keywords []keywordEntry `json:"keywords"`
}

type keywordEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type videosResponse struct {
	Results []videoEntry `json:"results"`
}

type videoEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Key         string `json:"key"`
	Site        string `json:"site"`
	Type        string `json:"type"`
	ISO639      string `json:"iso_639_1"`
	Official    bool   `json:"official"`
	PublishedAt string `json:"published_at"`
}

type releaseDatesResponse struct {
	Results []releaseDateCountry `json:"results"`
}

type releaseDateCountry struct {
	Country      string             `json:"iso_3166_1"`
	ReleaseDates []releaseDateEntry `json:"release_dates"`
}

type releaseDateEntry struct {
	Certification string `json:"certification"`
	ReleaseDate   string `json:"release_date"`
	Type          int    `json:"type"`
}

type recommendResponse struct {
	Page    int              `json:"page"`
	Results []recommendEntry `json:"results"`
}

type recommendEntry struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Name        string  `json:"name"`
	PosterPath  string  `json:"poster_path"`
	MediaType   string  `json:"media_type"`
	VoteAverage float64 `json:"vote_average"`
	ReleaseDate string  `json:"release_date"`
}

type collectionRef struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
}

type imagesResponse struct {
	Backdrops []imageEntry `json:"backdrops"`
	Logos     []imageEntry `json:"logos"`
	Posters   []imageEntry `json:"posters"`
}

type imageEntry struct {
	FilePath string `json:"file_path"`
	Language string `json:"iso_639_1"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}
