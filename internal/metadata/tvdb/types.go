package tvdb

type apiResponse[T any] struct {
	Status string `json:"status"`
	Data   T      `json:"data"`
}

type searchResult struct {
	ObjectID     string `json:"objectID"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	Year         string `json:"year"`
	Type         string `json:"type"`
	TvdbID       string `json:"tvdb_id"`
	PrimaryType  string `json:"primary_type"`
	ImageURL     string `json:"image_url"`
	Slug         string `json:"slug"`
	FirstAirTime string `json:"first_air_time"`
}

type seriesExtended struct {
	ID               int              `json:"id"`
	Name             string           `json:"name"`
	OriginalName     string           `json:"originalName"`
	Overview         string           `json:"overview"`
	Image            string           `json:"image"`
	FirstAired       string           `json:"firstAired"`
	LastAired        string           `json:"lastAired"`
	Status           seriesStatus     `json:"status"`
	OriginalLanguage string           `json:"originalLanguage"`
	Genres           []genreRef       `json:"genres"`
	Seasons          []seasonRef      `json:"seasons"`
	Characters       []characterRef   `json:"characters"`
	Artworks         []artworkRef     `json:"artworks"`
	RemoteIDs        []remoteIDRef    `json:"remoteIds"`
	OriginalNetwork  *companyRef      `json:"originalNetwork"`
	LatestNetwork    *companyRef      `json:"latestNetwork"`
	Score            int              `json:"score"`
}

type movieExtended struct {
	ID               int            `json:"id"`
	Name             string         `json:"name"`
	OriginalName     string         `json:"originalName"`
	Overview         string         `json:"overview"`
	Image            string         `json:"image"`
	Year             string         `json:"year"`
	Runtime          int            `json:"runtime"`
	Status           seriesStatus   `json:"status"`
	OriginalLanguage string         `json:"originalLanguage"`
	Genres           []genreRef     `json:"genres"`
	Characters       []characterRef `json:"characters"`
	Artworks         []artworkRef   `json:"artworks"`
	RemoteIDs        []remoteIDRef  `json:"remoteIds"`
	Score            int            `json:"score"`
}

type seasonRef struct {
	ID     int    `json:"id"`
	Number int    `json:"number"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	Type   struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"type"`
}

type seasonExtended struct {
	ID       int          `json:"id"`
	Number   int          `json:"number"`
	Name     string       `json:"name"`
	Overview string       `json:"overview"`
	Image    string       `json:"image"`
	Year     string       `json:"year"`
	Episodes []episodeRef `json:"episodes"`
}

type episodeRef struct {
	ID            int     `json:"id"`
	Name          string  `json:"name"`
	Overview      string  `json:"overview"`
	Image         string  `json:"image"`
	Number        int     `json:"number"`
	SeasonNumber  int     `json:"seasonNumber"`
	Runtime       int     `json:"runtime"`
	Aired         string  `json:"aired"`
}

type characterRef struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	PeopleID    int    `json:"peopleId"`
	PersonName  string `json:"personName"`
	PersonImgURL string `json:"personImgURL"`
	Type        int    `json:"type"`
	Sort        int    `json:"sort"`
	IsFeatured  bool   `json:"isFeatured"`
	URL         string `json:"url"`
	Image       string `json:"image"`
}

type artworkRef struct {
	ID       int    `json:"id"`
	Image    string `json:"image"`
	Type     int    `json:"type"`
	Language string `json:"language"`
	Score    int    `json:"score"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type remoteIDRef struct {
	ID         string `json:"id"`
	Type       int    `json:"type"`
	SourceName string `json:"sourceName"`
}

type genreRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type companyRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type seriesStatus struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

const (
	remoteSourceIMDB = 2
	remoteSourceTMDB = 12

	artworkTypePoster    = 2
	artworkTypeBackground = 3
	artworkTypeBanner    = 1
	artworkTypeIcon      = 5
	artworkTypeClearArt  = 22
	artworkTypeClearLogo = 23
)
