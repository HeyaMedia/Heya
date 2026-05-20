package heyamedia

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

const tmdbImageURL = "https://image.tmdb.org/t/p/original"

// TMDBProvider implements metadata.Provider and metadata.DirectLookupProvider
// by routing through HeyaMedia's TMDB proxy endpoints.
type TMDBProvider struct {
	client *Client
}

func NewTMDBProvider(c *Client) *TMDBProvider {
	return &TMDBProvider{client: c}
}

func (p *TMDBProvider) Name() string { return "tmdb" }

func (p *TMDBProvider) Supports(kind metadata.MediaKind) bool {
	return kind == metadata.KindMovie || kind == metadata.KindTV
}

func (p *TMDBProvider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	switch kind {
	case metadata.KindMovie:
		return p.searchMovies(ctx, query)
	case metadata.KindTV:
		return p.searchTV(ctx, query)
	default:
		return nil, fmt.Errorf("tmdb: unsupported kind %s", kind)
	}
}

func (p *TMDBProvider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	parts := strings.SplitN(providerID, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid tmdb provider ID: %s", providerID)
	}
	switch parts[0] {
	case "movie":
		return p.getMovieDetail(ctx, parts[1], opts)
	case "tv":
		return p.getTVDetail(ctx, parts[1], opts)
	default:
		return nil, fmt.Errorf("unknown tmdb kind: %s", parts[0])
	}
}

func (p *TMDBProvider) LookupByNFO(ctx context.Context, kind metadata.MediaKind, ids metadata.NFOIDs, opts *metadata.FetchOptions) (*metadata.MediaDetail, string, error) {
	tmdbID := ids.TMDBID

	if tmdbID == "" && ids.IMDBID != "" {
		foundKind, foundID, err := p.findByIMDB(ctx, ids.IMDBID)
		if err != nil {
			return nil, "", err
		}
		tmdbID = foundID
		if kind == metadata.KindMovie && foundKind == "tv" {
			kind = metadata.KindTV
		} else if kind == metadata.KindTV && foundKind == "movie" {
			kind = metadata.KindMovie
		}
	}

	if tmdbID == "" {
		return nil, "", fmt.Errorf("no TMDB or IMDB ID available")
	}

	switch kind {
	case metadata.KindMovie:
		detail, err := p.getMovieDetail(ctx, tmdbID, opts)
		if err != nil {
			return nil, "", err
		}
		return detail, "movie:" + tmdbID, nil
	case metadata.KindTV:
		detail, err := p.getTVDetail(ctx, tmdbID, opts)
		if err != nil {
			return nil, "", err
		}
		return detail, "tv:" + tmdbID, nil
	default:
		return nil, "", fmt.Errorf("TMDB does not support kind %s", kind)
	}
}

func (p *TMDBProvider) GetMovieDetailByID(ctx context.Context, tmdbID string) (*metadata.MediaDetail, error) {
	return p.getMovieDetail(ctx, tmdbID, nil)
}

func (p *TMDBProvider) GetTVDetailByID(ctx context.Context, tmdbID string) (*metadata.MediaDetail, error) {
	return p.getTVDetail(ctx, tmdbID, nil)
}

func (p *TMDBProvider) findByIMDB(ctx context.Context, imdbID string) (kind string, tmdbID string, err error) {
	var resp tmdbFindResponse
	if err := p.client.getJSON(ctx, "/api/v1/tmdb/find/"+imdbID, &resp); err != nil {
		return "", "", err
	}
	if len(resp.MovieResults) > 0 {
		return "movie", strconv.Itoa(resp.MovieResults[0].ID), nil
	}
	if len(resp.TVResults) > 0 {
		return "tv", strconv.Itoa(resp.TVResults[0].ID), nil
	}
	return "", "", fmt.Errorf("no TMDB result for IMDB %s", imdbID)
}

func (p *TMDBProvider) searchMovies(ctx context.Context, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	params := url.Values{"query": {query.Title}}
	if query.Year != "" {
		params.Set("year", query.Year)
	}
	if query.Language != "" {
		params.Set("language", query.Language)
	}

	var resp tmdbSearchMovieResponse
	if err := p.client.get(ctx, "/api/v1/tmdb/search/movie", params, &resp); err != nil {
		return nil, err
	}

	var results []metadata.SearchResult
	for i, r := range resp.Results {
		if i >= 10 {
			break
		}
		year := ""
		if len(r.ReleaseDate) >= 4 {
			year = r.ReleaseDate[:4]
		}
		results = append(results, metadata.SearchResult{
			ProviderID:   fmt.Sprintf("movie:%d", r.ID),
			ProviderName: "tmdb",
			Title:        r.Title,
			Year:         year,
			Description:  truncate(r.Overview, 300),
			PosterURL:    tmdbImageFor(r.PosterPath),
			RawData:      r,
		})
	}
	return results, nil
}

func (p *TMDBProvider) searchTV(ctx context.Context, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	params := url.Values{"query": {query.Title}}
	if query.Year != "" {
		params.Set("first_air_date_year", query.Year)
	}
	if query.Language != "" {
		params.Set("language", query.Language)
	}

	var resp tmdbSearchTVResponse
	if err := p.client.get(ctx, "/api/v1/tmdb/search/tv", params, &resp); err != nil {
		return nil, err
	}

	var results []metadata.SearchResult
	for i, r := range resp.Results {
		if i >= 10 {
			break
		}
		year := ""
		if len(r.FirstAirDate) >= 4 {
			year = r.FirstAirDate[:4]
		}
		results = append(results, metadata.SearchResult{
			ProviderID:   fmt.Sprintf("tv:%d", r.ID),
			ProviderName: "tmdb",
			Title:        r.Name,
			Year:         year,
			Description:  truncate(r.Overview, 300),
			PosterURL:    tmdbImageFor(r.PosterPath),
			RawData:      r,
		})
	}
	return results, nil
}

func (p *TMDBProvider) getMovieDetail(ctx context.Context, id string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	params := url.Values{
		"append_to_response": {"credits,external_ids,keywords,release_dates,videos,recommendations"},
	}
	if opts != nil && opts.Language != "" {
		params.Set("language", opts.Language)
	}

	var d tmdbMovieDetail
	if err := p.client.get(ctx, "/api/v1/tmdb/movie/"+id, params, &d); err != nil {
		return nil, err
	}

	year := ""
	if len(d.ReleaseDate) >= 4 {
		year = d.ReleaseDate[:4]
	}

	genres := make([]string, len(d.Genres))
	for i, g := range d.Genres {
		genres[i] = g.Name
	}

	companies := make([]metadata.ProductionCompanyDetail, len(d.ProductionCompanies))
	for i, c := range d.ProductionCompanies {
		companies[i] = metadata.ProductionCompanyDetail{
			TmdbID: c.ID, Name: c.Name, LogoPath: c.LogoPath, OriginCountry: c.OriginCountry,
		}
	}

	cast := convertCast(d.Credits.Cast, 30)
	crew := convertCrew(d.Credits.Crew)

	keywords := make([]metadata.KeywordDetail, len(d.Keywords.Keywords))
	for i, k := range d.Keywords.Keywords {
		keywords[i] = metadata.KeywordDetail{TmdbID: k.ID, Name: k.Name}
	}

	var videos []metadata.VideoDetail
	for _, v := range d.Videos.Results {
		videos = append(videos, metadata.VideoDetail{
			TmdbKey: v.ID, Name: v.Name, Site: v.Site, Key: v.Key,
			Type: v.Type, Language: v.ISO639, Official: v.Official, PublishedAt: v.PublishedAt,
		})
	}

	var certs []metadata.CertificationDetail
	for _, rd := range d.ReleaseDates.Results {
		for _, r := range rd.ReleaseDates {
			if r.Certification != "" {
				certs = append(certs, metadata.CertificationDetail{
					Country: rd.Country, Certification: r.Certification,
					ReleaseDate: r.ReleaseDate, ReleaseType: r.Type,
				})
			}
		}
	}

	var recs []metadata.RecommendationDetail
	for _, r := range d.Recommendations.Results {
		recs = append(recs, metadata.RecommendationDetail{
			TmdbID: r.ID, Title: r.Title, PosterPath: r.PosterPath,
			MediaType: r.MediaType, VoteAverage: r.VoteAverage, ReleaseDate: r.ReleaseDate,
		})
	}

	var collection *metadata.CollectionDetail
	if d.Collection != nil && d.Collection.ID > 0 {
		collection = &metadata.CollectionDetail{
			TmdbID: d.Collection.ID, Name: d.Collection.Name,
			PosterPath: d.Collection.PosterPath, BackdropPath: d.Collection.BackdropPath,
		}
	}

	langs := make([]string, len(d.SpokenLanguages))
	for i, l := range d.SpokenLanguages {
		langs[i] = l.EnglishName
	}

	return &metadata.MediaDetail{
		Title:               d.Title,
		SortTitle:           strings.ToLower(d.Title),
		Year:                year,
		Description:         d.Overview,
		PosterURL:           tmdbImageFor(d.PosterPath),
		BackdropURL:         tmdbImageFor(d.BackdropPath),
		ExternalIDs:         map[string]string{"tmdb": id, "imdb": d.ExternalIDs.IMDBID},
		Genres:              genres,
		Rating:              d.VoteAverage,
		RuntimeMinutes:      d.Runtime,
		Tagline:             d.Tagline,
		ReleaseDate:         d.ReleaseDate,
		OriginalTitle:       d.OriginalTitle,
		OriginalLanguage:    d.OriginalLanguage,
		Budget:              d.Budget,
		Revenue:             d.Revenue,
		Popularity:          d.Popularity,
		VoteCount:           d.VoteCount,
		ProductionCompanies: companies,
		Cast:                cast,
		Crew:                crew,
		Keywords:            keywords,
		Videos:              videos,
		Certifications:      certs,
		Recommendations:     recs,
		Collection:          collection,
		Homepage:            d.Homepage,
		SpokenLanguages:     langs,
		OriginCountry:       d.OriginCountry,
		MovieStatus:         d.Status,
		WikidataID:          d.ExternalIDs.WikidataID,
		FacebookID:          d.ExternalIDs.FacebookID,
		InstagramID:         d.ExternalIDs.InstagramID,
		TwitterID:           d.ExternalIDs.TwitterID,
	}, nil
}

func (p *TMDBProvider) getTVDetail(ctx context.Context, id string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	params := url.Values{
		"append_to_response": {"credits,external_ids,keywords,videos,content_ratings,recommendations"},
	}
	if opts != nil && opts.Language != "" {
		params.Set("language", opts.Language)
	}

	var d tmdbTVDetail
	if err := p.client.get(ctx, "/api/v1/tmdb/tv/"+id, params, &d); err != nil {
		return nil, err
	}

	year := ""
	if len(d.FirstAirDate) >= 4 {
		year = d.FirstAirDate[:4]
	}

	genres := make([]string, len(d.Genres))
	for i, g := range d.Genres {
		genres[i] = g.Name
	}

	networks := make([]string, len(d.Networks))
	for i, n := range d.Networks {
		networks[i] = n.Name
	}

	createdBy := make([]string, len(d.CreatedBy))
	for i, c := range d.CreatedBy {
		createdBy[i] = c.Name
	}

	cast := convertCast(d.Credits.Cast, 30)
	crew := convertCrew(d.Credits.Crew)

	keywords := make([]metadata.KeywordDetail, len(d.Keywords.Results))
	for i, k := range d.Keywords.Results {
		keywords[i] = metadata.KeywordDetail{TmdbID: k.ID, Name: k.Name}
	}

	var videos []metadata.VideoDetail
	for _, v := range d.Videos.Results {
		videos = append(videos, metadata.VideoDetail{
			TmdbKey: v.ID, Name: v.Name, Site: v.Site, Key: v.Key,
			Type: v.Type, Language: v.ISO639, Official: v.Official, PublishedAt: v.PublishedAt,
		})
	}

	var certs []metadata.CertificationDetail
	for _, cr := range d.ContentRatings.Results {
		if cr.Rating != "" {
			certs = append(certs, metadata.CertificationDetail{
				Country: cr.Country, Certification: cr.Rating,
			})
		}
	}

	var recs []metadata.RecommendationDetail
	for _, r := range d.Recommendations.Results {
		title := r.Title
		if title == "" {
			title = r.Name
		}
		recs = append(recs, metadata.RecommendationDetail{
			TmdbID: r.ID, Title: title, PosterPath: r.PosterPath,
			MediaType: r.MediaType, VoteAverage: r.VoteAverage, ReleaseDate: r.ReleaseDate,
		})
	}

	companies := make([]metadata.ProductionCompanyDetail, len(d.ProductionCompanies))
	for i, c := range d.ProductionCompanies {
		companies[i] = metadata.ProductionCompanyDetail{
			TmdbID: c.ID, Name: c.Name, LogoPath: c.LogoPath, OriginCountry: c.OriginCountry,
		}
	}

	idInt, _ := strconv.Atoi(id)
	var seasons []metadata.SeasonDetail
	for _, s := range d.Seasons {
		if s.SeasonNumber == 0 {
			continue
		}

		sd, err := p.getSeasonDetail(ctx, idInt, s.SeasonNumber)
		if err != nil {
			continue
		}

		var episodes []metadata.EpisodeDetail
		for _, ep := range sd.Episodes {
			episodes = append(episodes, metadata.EpisodeDetail{
				Number:         ep.EpisodeNumber,
				Title:          ep.Name,
				Overview:       ep.Overview,
				StillURL:       tmdbImageFor(ep.StillPath),
				RuntimeMinutes: ep.Runtime,
				AirDate:        ep.AirDate,
				Rating:         ep.VoteAverage,
			})
		}

		seasons = append(seasons, metadata.SeasonDetail{
			Number:    sd.SeasonNumber,
			Title:     sd.Name,
			Overview:  sd.Overview,
			PosterURL: tmdbImageFor(sd.PosterPath),
			AirDate:   sd.AirDate,
			Episodes:  episodes,
		})
	}

	return &metadata.MediaDetail{
		Title:               d.Name,
		SortTitle:           strings.ToLower(d.Name),
		Year:                year,
		Description:         d.Overview,
		PosterURL:           tmdbImageFor(d.PosterPath),
		BackdropURL:         tmdbImageFor(d.BackdropPath),
		ExternalIDs:         map[string]string{"tmdb": id, "imdb": d.ExternalIDs.IMDBID, "tvdb": strconv.Itoa(d.ExternalIDs.TVDBID)},
		Genres:              genres,
		Rating:              d.VoteAverage,
		Status:              d.Status,
		FirstAirDate:        d.FirstAirDate,
		LastAirDate:         d.LastAirDate,
		OriginalName:        d.OriginalName,
		OriginalLanguage:    d.OriginalLanguage,
		Networks:            networks,
		CreatedBy:           createdBy,
		NumberOfSeasons:     d.NumberOfSeasons,
		NumberOfEpisodes:    d.NumberOfEpisodes,
		Popularity:          d.Popularity,
		VoteCount:           d.VoteCount,
		Cast:                cast,
		Crew:                crew,
		Keywords:            keywords,
		Videos:              videos,
		Certifications:      certs,
		Recommendations:     recs,
		ProductionCompanies: companies,
		Seasons:             seasons,
	}, nil
}

func (p *TMDBProvider) getSeasonDetail(ctx context.Context, tvID, seasonNum int) (*tmdbSeasonDetail, error) {
	var sd tmdbSeasonDetail
	path := fmt.Sprintf("/api/v1/tmdb/tv/%d/season/%d", tvID, seasonNum)
	if err := p.client.getJSON(ctx, path, &sd); err != nil {
		return nil, err
	}
	return &sd, nil
}

// TMDBArtworkProvider fetches images from TMDB via HeyaMedia.
type TMDBArtworkProvider struct {
	client *Client
}

func NewTMDBArtworkProvider(c *Client) *TMDBArtworkProvider {
	return &TMDBArtworkProvider{client: c}
}

func (p *TMDBArtworkProvider) Name() string { return "tmdb" }

func (p *TMDBArtworkProvider) FetchArtwork(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string) ([]metadata.ArtworkResult, error) {
	tmdbID := externalIDs["tmdb"]
	if tmdbID == "" {
		return nil, nil
	}

	var path string
	switch kind {
	case metadata.KindMovie:
		path = "/api/v1/tmdb/movie/" + tmdbID + "/images"
	case metadata.KindTV:
		path = "/api/v1/tmdb/tv/" + tmdbID + "/images"
	default:
		return nil, nil
	}

	var resp tmdbImagesResponse
	if err := p.client.getJSON(ctx, path, &resp); err != nil {
		return nil, err
	}

	var results []metadata.ArtworkResult
	for _, img := range resp.Backdrops {
		results = append(results, metadata.ArtworkResult{
			URL: tmdbImageURL + img.FilePath, AssetType: "backdrop", Language: img.Language,
		})
	}
	for _, img := range resp.Logos {
		results = append(results, metadata.ArtworkResult{
			URL: tmdbImageURL + img.FilePath, AssetType: "clearlogo", Language: img.Language,
		})
	}
	for _, img := range resp.Posters {
		results = append(results, metadata.ArtworkResult{
			URL: tmdbImageURL + img.FilePath, AssetType: "poster", Language: img.Language,
		})
	}
	return results, nil
}

// PersonDetail for use by PersonFetchWorker.
type PersonDetail struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Biography    string   `json:"biography"`
	Birthday     string   `json:"birthday"`
	Deathday     string   `json:"deathday"`
	PlaceOfBirth string   `json:"place_of_birth"`
	AlsoKnownAs  []string `json:"also_known_as"`
	Gender       int      `json:"gender"`
	ProfilePath  string   `json:"profile_path"`
	Homepage     string   `json:"homepage"`
	Popularity   float64  `json:"popularity"`
	ImdbID       string   `json:"imdb_id"`
	ExternalIDs  struct {
		ImdbID      string `json:"imdb_id"`
		WikidataID  string `json:"wikidata_id"`
		FacebookID  string `json:"facebook_id"`
		InstagramID string `json:"instagram_id"`
		TwitterID   string `json:"twitter_id"`
	} `json:"external_ids"`
}

func (c *Client) GetPersonDetail(ctx context.Context, tmdbID int) (*PersonDetail, error) {
	var d PersonDetail
	params := url.Values{"append_to_response": {"external_ids"}}
	if err := c.get(ctx, fmt.Sprintf("/api/v1/tmdb/person/%d", tmdbID), params, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func convertCast(entries []tmdbCastEntry, limit int) []metadata.CastMember {
	var result []metadata.CastMember
	for i, c := range entries {
		if i >= limit {
			break
		}
		result = append(result, metadata.CastMember{
			TmdbID: c.ID, Name: c.Name, Character: c.Character, Order: c.Order,
			Gender: c.Gender, ProfilePath: tmdbImageFor(c.ProfilePath), Popularity: c.Popularity,
		})
	}
	return result
}

func convertCrew(entries []tmdbCrewEntry) []metadata.CrewMember {
	var result []metadata.CrewMember
	for _, c := range entries {
		result = append(result, metadata.CrewMember{
			TmdbID: c.ID, Name: c.Name, Job: c.Job, Department: c.Department,
			Gender: c.Gender, ProfilePath: tmdbImageFor(c.ProfilePath),
		})
	}
	return result
}

func tmdbImageFor(path string) string {
	if path == "" {
		return ""
	}
	return tmdbImageURL + path
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// --- TMDB response types ---

type tmdbFindResponse struct {
	MovieResults []tmdbMovieResult `json:"movie_results"`
	TVResults    []tmdbTVResult    `json:"tv_results"`
}

type tmdbSearchMovieResponse struct {
	Results []tmdbMovieResult `json:"results"`
}

type tmdbMovieResult struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Overview    string  `json:"overview"`
	ReleaseDate string  `json:"release_date"`
	PosterPath  string  `json:"poster_path"`
	Popularity  float64 `json:"popularity"`
	VoteAverage float64 `json:"vote_average"`
}

type tmdbSearchTVResponse struct {
	Results []tmdbTVResult `json:"results"`
}

type tmdbTVResult struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Overview     string  `json:"overview"`
	FirstAirDate string  `json:"first_air_date"`
	PosterPath   string  `json:"poster_path"`
	Popularity   float64 `json:"popularity"`
	VoteAverage  float64 `json:"vote_average"`
}

type tmdbMovieDetail struct {
	ID                  int                      `json:"id"`
	Title               string                   `json:"title"`
	OriginalTitle       string                   `json:"original_title"`
	OriginalLanguage    string                   `json:"original_language"`
	Overview            string                   `json:"overview"`
	Tagline             string                   `json:"tagline"`
	Homepage            string                   `json:"homepage"`
	Status              string                   `json:"status"`
	ReleaseDate         string                   `json:"release_date"`
	Runtime             int                      `json:"runtime"`
	Budget              int64                    `json:"budget"`
	Revenue             int64                    `json:"revenue"`
	Popularity          float64                  `json:"popularity"`
	VoteAverage         float64                  `json:"vote_average"`
	VoteCount           int                      `json:"vote_count"`
	PosterPath          string                   `json:"poster_path"`
	BackdropPath        string                   `json:"backdrop_path"`
	Genres              []tmdbGenre              `json:"genres"`
	ProductionCompanies []tmdbProductionCo       `json:"production_companies"`
	SpokenLanguages     []tmdbSpokenLanguage     `json:"spoken_languages"`
	OriginCountry       []string                 `json:"origin_country"`
	Collection          *tmdbCollectionRef       `json:"belongs_to_collection"`
	Credits             tmdbCredits              `json:"credits"`
	ExternalIDs         tmdbExternalIDs          `json:"external_ids"`
	Keywords            tmdbKeywordsResponse     `json:"keywords"`
	Videos              tmdbVideosResponse       `json:"videos"`
	ReleaseDates        tmdbReleaseDatesResponse `json:"release_dates"`
	Recommendations     tmdbRecommendResponse    `json:"recommendations"`
}

type tmdbTVDetail struct {
	ID                  int                       `json:"id"`
	Name                string                    `json:"name"`
	OriginalName        string                    `json:"original_name"`
	OriginalLanguage    string                    `json:"original_language"`
	Overview            string                    `json:"overview"`
	FirstAirDate        string                    `json:"first_air_date"`
	LastAirDate         string                    `json:"last_air_date"`
	Status              string                    `json:"status"`
	NumberOfSeasons     int                       `json:"number_of_seasons"`
	NumberOfEpisodes    int                       `json:"number_of_episodes"`
	Popularity          float64                   `json:"popularity"`
	VoteAverage         float64                   `json:"vote_average"`
	VoteCount           int                       `json:"vote_count"`
	PosterPath          string                    `json:"poster_path"`
	BackdropPath        string                    `json:"backdrop_path"`
	Genres              []tmdbGenre               `json:"genres"`
	Networks            []tmdbNetwork             `json:"networks"`
	CreatedBy           []tmdbCreator             `json:"created_by"`
	Seasons             []tmdbSeasonEntry         `json:"seasons"`
	Credits             tmdbCredits               `json:"credits"`
	ExternalIDs         tmdbExternalIDs           `json:"external_ids"`
	Keywords            tmdbTVKeywordsResponse    `json:"keywords"`
	Videos              tmdbVideosResponse        `json:"videos"`
	ContentRatings      tmdbContentRatingsResponse `json:"content_ratings"`
	Recommendations     tmdbRecommendResponse     `json:"recommendations"`
	ProductionCompanies []tmdbProductionCo        `json:"production_companies"`
}

type tmdbSeasonDetail struct {
	SeasonNumber int               `json:"season_number"`
	Name         string            `json:"name"`
	Overview     string            `json:"overview"`
	PosterPath   string            `json:"poster_path"`
	AirDate      string            `json:"air_date"`
	Episodes     []tmdbEpisodeEntry `json:"episodes"`
}

type tmdbEpisodeEntry struct {
	EpisodeNumber int     `json:"episode_number"`
	Name          string  `json:"name"`
	Overview      string  `json:"overview"`
	StillPath     string  `json:"still_path"`
	Runtime       int     `json:"runtime"`
	AirDate       string  `json:"air_date"`
	VoteAverage   float64 `json:"vote_average"`
}

type tmdbSeasonEntry struct {
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	PosterPath   string `json:"poster_path"`
}

type tmdbGenre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tmdbProductionCo struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	LogoPath      string `json:"logo_path"`
	OriginCountry string `json:"origin_country"`
}

type tmdbSpokenLanguage struct {
	EnglishName string `json:"english_name"`
}

type tmdbNetwork struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tmdbCreator struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tmdbCredits struct {
	Cast []tmdbCastEntry `json:"cast"`
	Crew []tmdbCrewEntry `json:"crew"`
}

type tmdbCastEntry struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Character   string  `json:"character"`
	Order       int     `json:"order"`
	Gender      int     `json:"gender"`
	ProfilePath string  `json:"profile_path"`
	Popularity  float64 `json:"popularity"`
}

type tmdbCrewEntry struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Job         string `json:"job"`
	Department  string `json:"department"`
	Gender      int    `json:"gender"`
	ProfilePath string `json:"profile_path"`
}

type tmdbExternalIDs struct {
	IMDBID      string `json:"imdb_id"`
	TVDBID      int    `json:"tvdb_id"`
	WikidataID  string `json:"wikidata_id"`
	FacebookID  string `json:"facebook_id"`
	InstagramID string `json:"instagram_id"`
	TwitterID   string `json:"twitter_id"`
}

type tmdbKeywordsResponse struct {
	Keywords []tmdbKeyword `json:"keywords"`
}

type tmdbTVKeywordsResponse struct {
	Results []tmdbKeyword `json:"results"`
}

type tmdbKeyword struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tmdbVideosResponse struct {
	Results []tmdbVideoEntry `json:"results"`
}

type tmdbVideoEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Key         string `json:"key"`
	Site        string `json:"site"`
	Type        string `json:"type"`
	ISO639      string `json:"iso_639_1"`
	Official    bool   `json:"official"`
	PublishedAt string `json:"published_at"`
}

type tmdbReleaseDatesResponse struct {
	Results []tmdbReleaseDateCountry `json:"results"`
}

type tmdbReleaseDateCountry struct {
	Country      string                `json:"iso_3166_1"`
	ReleaseDates []tmdbReleaseDateEntry `json:"release_dates"`
}

type tmdbReleaseDateEntry struct {
	Certification string `json:"certification"`
	ReleaseDate   string `json:"release_date"`
	Type          int    `json:"type"`
}

type tmdbContentRatingsResponse struct {
	Results []tmdbContentRating `json:"results"`
}

type tmdbContentRating struct {
	Country string `json:"iso_3166_1"`
	Rating  string `json:"rating"`
}

type tmdbRecommendResponse struct {
	Results []tmdbRecommendEntry `json:"results"`
}

type tmdbRecommendEntry struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Name        string  `json:"name"`
	PosterPath  string  `json:"poster_path"`
	MediaType   string  `json:"media_type"`
	VoteAverage float64 `json:"vote_average"`
	ReleaseDate string  `json:"release_date"`
}

type tmdbCollectionRef struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
}

type tmdbImagesResponse struct {
	Backdrops []tmdbImageEntry `json:"backdrops"`
	Logos     []tmdbImageEntry `json:"logos"`
	Posters   []tmdbImageEntry `json:"posters"`
}

type tmdbImageEntry struct {
	FilePath string `json:"file_path"`
	Language string `json:"iso_639_1"`
}
