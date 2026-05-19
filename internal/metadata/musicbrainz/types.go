package musicbrainz

type searchResponse struct {
	ReleaseGroups []releaseGroupResult `json:"release-groups"`
}

type releaseGroupResult struct {
	ID             string         `json:"id"`
	Title          string         `json:"title"`
	PrimaryType    string         `json:"primary-type"`
	FirstRelease   string         `json:"first-release-date"`
	ArtistCredit   []artistCredit `json:"artist-credit"`
	Score          int            `json:"score"`
}

type artistCredit struct {
	Artist artistRef `json:"artist"`
}

type artistRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SortName string `json:"sort-name"`
}

type releaseGroupDetail struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	PrimaryType  string         `json:"primary-type"`
	FirstRelease string         `json:"first-release-date"`
	ArtistCredit []artistCredit `json:"artist-credit"`
	Releases     []releaseRef   `json:"releases"`
	Genres       []genreTag     `json:"genres"`
	Tags         []genreTag     `json:"tags"`
}

type releaseRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Date     string `json:"date"`
	Country  string `json:"country"`
	Status   string `json:"status"`
	Barcode  string `json:"barcode"`
	LabelInfo []labelInfo `json:"label-info"`
}

type labelInfo struct {
	CatalogNumber string   `json:"catalog-number"`
	Label         labelRef `json:"label"`
}

type labelRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type genreTag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type releaseDetail struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Date     string       `json:"date"`
	Country  string       `json:"country"`
	Barcode  string       `json:"barcode"`
	Media    []mediaEntry `json:"media"`
	LabelInfo []labelInfo `json:"label-info"`
}

type mediaEntry struct {
	Position int          `json:"position"`
	Tracks   []trackEntry `json:"tracks"`
}

type trackEntry struct {
	Position  int          `json:"position"`
	Title     string       `json:"title"`
	Length    int          `json:"length"`
	Recording recordingRef `json:"recording"`
}

type recordingRef struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Length int    `json:"length"`
}

type mbArtistDetail struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Annotation string `json:"annotation"`
}

type coverArtResponse struct {
	Images []coverImage `json:"images"`
}

type coverImage struct {
	Image      string   `json:"image"`
	Front      bool     `json:"front"`
	Types      []string `json:"types"`
	Thumbnails struct {
		Large string `json:"large"`
		Small string `json:"small"`
	} `json:"thumbnails"`
}
