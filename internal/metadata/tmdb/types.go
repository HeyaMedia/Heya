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
	ID                  int               `json:"id"`
	Title               string            `json:"title"`
	OriginalTitle       string            `json:"original_title"`
	OriginalLanguage    string            `json:"original_language"`
	Overview            string            `json:"overview"`
	Tagline             string            `json:"tagline"`
	ReleaseDate         string            `json:"release_date"`
	Runtime             int               `json:"runtime"`
	Budget              int64             `json:"budget"`
	Revenue             int64             `json:"revenue"`
	Popularity          float64           `json:"popularity"`
	VoteAverage         float64           `json:"vote_average"`
	VoteCount           int               `json:"vote_count"`
	PosterPath          string            `json:"poster_path"`
	BackdropPath        string            `json:"backdrop_path"`
	Genres              []genreEntry      `json:"genres"`
	ProductionCompanies []productionCo    `json:"production_companies"`
	Credits             creditsResponse   `json:"credits"`
	ExternalIDs         externalIDsResult `json:"external_ids"`
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
	ID               int               `json:"id"`
	Name             string            `json:"name"`
	OriginalName     string            `json:"original_name"`
	OriginalLanguage string            `json:"original_language"`
	Overview         string            `json:"overview"`
	FirstAirDate     string            `json:"first_air_date"`
	LastAirDate      string            `json:"last_air_date"`
	Status           string            `json:"status"`
	NumberOfSeasons  int               `json:"number_of_seasons"`
	NumberOfEpisodes int               `json:"number_of_episodes"`
	Popularity       float64           `json:"popularity"`
	VoteAverage      float64           `json:"vote_average"`
	VoteCount        int               `json:"vote_count"`
	PosterPath       string            `json:"poster_path"`
	BackdropPath     string            `json:"backdrop_path"`
	Genres           []genreEntry      `json:"genres"`
	Networks         []networkEntry    `json:"networks"`
	CreatedBy        []creatorEntry    `json:"created_by"`
	Seasons          []seasonEntry     `json:"seasons"`
	Credits          creditsResponse   `json:"credits"`
	ExternalIDs      externalIDsResult `json:"external_ids"`
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
	ID   int    `json:"id"`
	Name string `json:"name"`
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
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Character   string `json:"character"`
	Order       int    `json:"order"`
	ProfilePath string `json:"profile_path"`
}

type crewEntry struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Job        string `json:"job"`
	Department string `json:"department"`
}

type externalIDsResult struct {
	IMDBID string `json:"imdb_id"`
	TVDBID int    `json:"tvdb_id"`
}
