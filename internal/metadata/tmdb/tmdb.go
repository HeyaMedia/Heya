package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

const (
	baseURL  = "https://api.themoviedb.org/3"
	imageURL = "https://image.tmdb.org/t/p/original"
)

type Provider struct {
	client  *metadata.RateLimitedClient
	token   string
	BaseURL string
}

func NewProvider(token string) *Provider {
	client := metadata.NewRateLimitedClient(4.0, 4, "Heya/1.0")
	return &Provider{client: client, token: token, BaseURL: baseURL}
}

func (p *Provider) Name() string { return "tmdb" }

func (p *Provider) Supports(kind metadata.MediaKind) bool {
	return kind == metadata.KindMovie || kind == metadata.KindTV
}

func (p *Provider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	switch kind {
	case metadata.KindMovie:
		return p.searchMovies(ctx, query)
	case metadata.KindTV:
		return p.searchTV(ctx, query)
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}

func (p *Provider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	parts := strings.SplitN(providerID, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid provider ID: %s", providerID)
	}
	kind := parts[0]
	id := parts[1]

	switch kind {
	case "movie":
		return p.getMovieDetail(ctx, id, opts)
	case "tv":
		return p.getTVDetail(ctx, id, opts)
	default:
		return nil, fmt.Errorf("unknown kind in provider ID: %s", kind)
	}
}

func (p *Provider) LookupByNFO(ctx context.Context, kind metadata.MediaKind, ids metadata.NFOIDs, opts *metadata.FetchOptions) (*metadata.MediaDetail, string, error) {
	tmdbID := ids.TMDBID

	if tmdbID == "" && ids.IMDBID != "" {
		foundKind, foundID, err := p.FindByIMDB(ctx, ids.IMDBID)
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

	var providerID string
	var detail *metadata.MediaDetail
	var err error

	switch kind {
	case metadata.KindMovie:
		providerID = "movie:" + tmdbID
		detail, err = p.getMovieDetail(ctx, tmdbID, opts)
	case metadata.KindTV:
		providerID = "tv:" + tmdbID
		detail, err = p.getTVDetail(ctx, tmdbID, opts)
	default:
		return nil, "", fmt.Errorf("TMDB does not support kind %s", kind)
	}

	if err != nil {
		return nil, "", err
	}
	return detail, providerID, nil
}

func (p *Provider) GetMovieDetailByID(ctx context.Context, tmdbID string) (*metadata.MediaDetail, error) {
	return p.getMovieDetail(ctx, tmdbID, nil)
}

func (p *Provider) GetTVDetailByID(ctx context.Context, tmdbID string) (*metadata.MediaDetail, error) {
	return p.getTVDetail(ctx, tmdbID, nil)
}

type findResponse struct {
	MovieResults []movieResult `json:"movie_results"`
	TVResults    []tvResult    `json:"tv_results"`
}

func (p *Provider) FindByIMDB(ctx context.Context, imdbID string) (kind string, tmdbID string, err error) {
	var resp findResponse
	params := url.Values{"external_source": {"imdb_id"}}
	if err := p.get(ctx, "/find/"+imdbID, params, &resp); err != nil {
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

func (p *Provider) searchMovies(ctx context.Context, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	params := url.Values{
		"query": {query.Title},
	}
	if query.Year != "" {
		params.Set("year", query.Year)
	}
	if query.Language != "" {
		params.Set("language", query.Language)
	}
	if query.Country != "" {
		params.Set("region", query.Country)
	}

	var resp searchMovieResponse
	if err := p.get(ctx, "/search/movie", params, &resp); err != nil {
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
			PosterURL:    imageURL + r.PosterPath,
			RawData:      r,
		})
	}
	return results, nil
}

func (p *Provider) searchTV(ctx context.Context, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	params := url.Values{
		"query": {query.Title},
	}
	if query.Year != "" {
		params.Set("first_air_date_year", query.Year)
	}
	if query.Language != "" {
		params.Set("language", query.Language)
	}

	var resp searchTVResponse
	if err := p.get(ctx, "/search/tv", params, &resp); err != nil {
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
			PosterURL:    imageURL + r.PosterPath,
			RawData:      r,
		})
	}
	return results, nil
}

func (p *Provider) getMovieDetail(ctx context.Context, id string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	var d movieDetail
	params := url.Values{
		"append_to_response": {"credits,external_ids,keywords,release_dates,videos,recommendations"},
	}
	if opts != nil && opts.Language != "" {
		params.Set("language", opts.Language)
	}
	if err := p.get(ctx, "/movie/"+id, params, &d); err != nil {
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
		PosterURL:           imageURLFor(d.PosterPath),
		BackdropURL:         imageURLFor(d.BackdropPath),
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

func (p *Provider) getTVDetail(ctx context.Context, id string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	var d tvDetail
	params := url.Values{
		"append_to_response": {"credits,external_ids,keywords,videos,content_ratings,recommendations"},
	}
	if opts != nil && opts.Language != "" {
		params.Set("language", opts.Language)
	}
	if err := p.get(ctx, "/tv/"+id, params, &d); err != nil {
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
				Country:       cr.Country,
				Certification: cr.Rating,
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

	var seasons []metadata.SeasonDetail
	for _, s := range d.Seasons {
		if s.SeasonNumber == 0 {
			continue
		}

		sd, err := p.getSeasonDetail(ctx, id, s.SeasonNumber)
		if err != nil {
			continue
		}

		var episodes []metadata.EpisodeDetail
		for _, ep := range sd.Episodes {
			episodes = append(episodes, metadata.EpisodeDetail{
				Number:         ep.EpisodeNumber,
				Title:          ep.Name,
				Overview:       ep.Overview,
				StillURL:       imageURLFor(ep.StillPath),
				RuntimeMinutes: ep.Runtime,
				AirDate:        ep.AirDate,
				Rating:         ep.VoteAverage,
			})
		}

		seasons = append(seasons, metadata.SeasonDetail{
			Number:    sd.SeasonNumber,
			Title:     sd.Name,
			Overview:  sd.Overview,
			PosterURL: imageURLFor(sd.PosterPath),
			AirDate:   sd.AirDate,
			Episodes:  episodes,
		})
	}

	return &metadata.MediaDetail{
		Title:            d.Name,
		SortTitle:        strings.ToLower(d.Name),
		Year:             year,
		Description:      d.Overview,
		PosterURL:        imageURLFor(d.PosterPath),
		BackdropURL:      imageURLFor(d.BackdropPath),
		ExternalIDs:      map[string]string{"tmdb": id, "imdb": d.ExternalIDs.IMDBID, "tvdb": strconv.Itoa(d.ExternalIDs.TVDBID)},
		Genres:           genres,
		Rating:           d.VoteAverage,
		Status:           d.Status,
		FirstAirDate:     d.FirstAirDate,
		LastAirDate:      d.LastAirDate,
		OriginalName:     d.OriginalName,
		OriginalLanguage: d.OriginalLanguage,
		Networks:         networks,
		CreatedBy:        createdBy,
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

func (p *Provider) getSeasonDetail(ctx context.Context, tvID string, seasonNum int) (*seasonDetail, error) {
	var sd seasonDetail
	path := fmt.Sprintf("/tv/%s/season/%d", tvID, seasonNum)
	if err := p.get(ctx, path, nil, &sd); err != nil {
		return nil, err
	}
	return &sd, nil
}

func (p *Provider) get(ctx context.Context, path string, params url.Values, result any) error {
	u := p.BaseURL + path
	if params != nil {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TMDB %s: HTTP %d", path, resp.StatusCode)
	}

	return decodeJSON(resp.Body, result)
}

func convertCast(entries []castEntry, limit int) []metadata.CastMember {
	var result []metadata.CastMember
	for i, c := range entries {
		if i >= limit {
			break
		}
		result = append(result, metadata.CastMember{
			TmdbID:      c.ID,
			Name:        c.Name,
			Character:   c.Character,
			Order:       c.Order,
			Gender:      c.Gender,
			ProfilePath: imageURLFor(c.ProfilePath),
			Popularity:  c.Popularity,
		})
	}
	return result
}

func convertCrew(entries []crewEntry) []metadata.CrewMember {
	var result []metadata.CrewMember
	for _, c := range entries {
		result = append(result, metadata.CrewMember{
			TmdbID:      c.ID,
			Name:        c.Name,
			Job:         c.Job,
			Department:  c.Department,
			Gender:      c.Gender,
			ProfilePath: imageURLFor(c.ProfilePath),
		})
	}
	return result
}

func (p *Provider) FetchArtwork(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string) ([]metadata.ArtworkResult, error) {
	tmdbID := externalIDs["tmdb"]
	if tmdbID == "" {
		return nil, nil
	}

	var path string
	switch kind {
	case metadata.KindMovie:
		path = "/movie/" + tmdbID + "/images"
	case metadata.KindTV:
		path = "/tv/" + tmdbID + "/images"
	default:
		return nil, nil
	}

	var resp imagesResponse
	if err := p.get(ctx, path, nil, &resp); err != nil {
		return nil, err
	}

	var results []metadata.ArtworkResult
	for _, img := range resp.Backdrops {
		results = append(results, metadata.ArtworkResult{
			URL:       imageURL + img.FilePath,
			AssetType: "backdrop",
			Language:  img.Language,
		})
	}
	for _, img := range resp.Logos {
		results = append(results, metadata.ArtworkResult{
			URL:       imageURL + img.FilePath,
			AssetType: "clearlogo",
			Language:  img.Language,
		})
	}
	for _, img := range resp.Posters {
		results = append(results, metadata.ArtworkResult{
			URL:       imageURL + img.FilePath,
			AssetType: "poster",
			Language:  img.Language,
		})
	}
	return results, nil
}

func imageURLFor(path string) string {
	if path == "" {
		return ""
	}
	return imageURL + path
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
