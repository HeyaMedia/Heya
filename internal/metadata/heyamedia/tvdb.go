package heyamedia

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

type TVDBProvider struct {
	client *Client
}

func NewTVDBProvider(c *Client) *TVDBProvider {
	return &TVDBProvider{client: c}
}

func (p *TVDBProvider) Name() string { return "tvdb" }

func (p *TVDBProvider) Supports(kind metadata.MediaKind) bool {
	return kind == metadata.KindTV || kind == metadata.KindMovie
}

func (p *TVDBProvider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	params := url.Values{"query": {query.Title}}
	switch kind {
	case metadata.KindTV:
		params.Set("type", "series")
	case metadata.KindMovie:
		params.Set("type", "movie")
	}
	if query.Year != "" {
		params.Set("year", query.Year)
	}
	if query.Language != "" {
		params.Set("language", query.Language)
	}

	var resp tvdbAPIResponse[[]tvdbSearchResult]
	if err := p.client.get(ctx, "/api/v1/tvdb/search", params, &resp); err != nil {
		return nil, err
	}

	var results []metadata.SearchResult
	for i, r := range resp.Data {
		if i >= 10 {
			break
		}
		providerID := tvdbBuildProviderID(r)
		if providerID == "" {
			continue
		}
		year := r.Year
		if year == "" && len(r.FirstAirTime) >= 4 {
			year = r.FirstAirTime[:4]
		}
		results = append(results, metadata.SearchResult{
			ProviderID:   providerID,
			ProviderName: "tvdb",
			Title:        r.Name,
			Year:         year,
			Description:  truncate(r.Overview, 300),
			PosterURL:    r.ImageURL,
		})
	}
	return results, nil
}

func (p *TVDBProvider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	parts := strings.SplitN(providerID, ":", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid tvdb provider ID: %s", providerID)
	}
	switch parts[1] {
	case "series":
		return p.getSeriesDetail(ctx, parts[2])
	case "movie":
		return p.getMovieDetail(ctx, parts[2])
	default:
		return nil, fmt.Errorf("unknown tvdb kind: %s", parts[1])
	}
}

func (p *TVDBProvider) LookupByNFO(ctx context.Context, kind metadata.MediaKind, ids metadata.NFOIDs, opts *metadata.FetchOptions) (*metadata.MediaDetail, string, error) {
	if ids.TVDBID == "" {
		return nil, "", fmt.Errorf("no TVDB ID available")
	}
	var providerID string
	switch kind {
	case metadata.KindTV:
		providerID = "tvdb:series:" + ids.TVDBID
	case metadata.KindMovie:
		providerID = "tvdb:movie:" + ids.TVDBID
	default:
		return nil, "", fmt.Errorf("TVDB does not support kind %s", kind)
	}
	detail, err := p.GetDetail(ctx, providerID, opts)
	if err != nil {
		return nil, "", err
	}
	return detail, providerID, nil
}

func (p *TVDBProvider) getSeriesDetail(ctx context.Context, id string) (*metadata.MediaDetail, error) {
	params := url.Values{"meta": {"episodes"}}
	var resp tvdbAPIResponse[tvdbSeriesExtended]
	if err := p.client.get(ctx, "/api/v1/tvdb/series/"+id, params, &resp); err != nil {
		return nil, err
	}
	s := resp.Data

	year := ""
	if len(s.FirstAired) >= 4 {
		year = s.FirstAired[:4]
	}

	genres := make([]string, len(s.Genres))
	for i, g := range s.Genres {
		genres[i] = g.Name
	}

	var networks []string
	if s.OriginalNetwork != nil {
		networks = append(networks, s.OriginalNetwork.Name)
	}
	if s.LatestNetwork != nil && (s.OriginalNetwork == nil || s.LatestNetwork.Name != s.OriginalNetwork.Name) {
		networks = append(networks, s.LatestNetwork.Name)
	}

	externalIDs := map[string]string{"tvdb": id}
	for _, r := range s.RemoteIDs {
		switch r.Type {
		case tvdbRemoteSourceIMDB:
			externalIDs["imdb"] = r.ID
		case tvdbRemoteSourceTMDB:
			externalIDs["tmdb"] = r.ID
		}
	}

	cast := tvdbConvertCharacters(s.Characters)
	airedSeasons := tvdbFilterAiredSeasons(s.Seasons)

	var seasons []metadata.SeasonDetail
	for _, sref := range airedSeasons {
		sd, err := p.getSeasonDetail(ctx, sref.ID)
		if err != nil {
			continue
		}
		var episodes []metadata.EpisodeDetail
		for _, ep := range sd.Episodes {
			episodes = append(episodes, metadata.EpisodeDetail{
				Number: ep.Number, Title: ep.Name, Overview: ep.Overview,
				StillURL: ep.Image, RuntimeMinutes: ep.Runtime, AirDate: ep.Aired,
			})
		}
		seasons = append(seasons, metadata.SeasonDetail{
			Number: sd.Number, Title: sd.Name, Overview: sd.Overview,
			PosterURL: sd.Image, Episodes: episodes,
		})
	}

	posterURL, backdropURL := tvdbExtractArtwork(s.Artworks)
	if posterURL == "" {
		posterURL = s.Image
	}

	totalEpisodes := 0
	for _, ss := range seasons {
		totalEpisodes += len(ss.Episodes)
	}

	return &metadata.MediaDetail{
		Title: s.Name, SortTitle: strings.ToLower(s.Name), Year: year,
		Description: s.Overview, PosterURL: posterURL, BackdropURL: backdropURL,
		ExternalIDs: externalIDs, Genres: genres,
		Rating: float64(s.Score) / 10.0, Status: s.Status.Name,
		FirstAirDate: s.FirstAired, LastAirDate: s.LastAired,
		OriginalName: s.OriginalName, OriginalLanguage: s.OriginalLanguage,
		Networks: networks, NumberOfSeasons: len(airedSeasons),
		NumberOfEpisodes: totalEpisodes, Popularity: float64(s.Score),
		Cast: cast, Seasons: seasons,
	}, nil
}

func (p *TVDBProvider) getMovieDetail(ctx context.Context, id string) (*metadata.MediaDetail, error) {
	var resp tvdbAPIResponse[tvdbMovieExtended]
	if err := p.client.getJSON(ctx, "/api/v1/tvdb/movies/"+id, &resp); err != nil {
		return nil, err
	}
	m := resp.Data

	genres := make([]string, len(m.Genres))
	for i, g := range m.Genres {
		genres[i] = g.Name
	}

	externalIDs := map[string]string{"tvdb": id}
	for _, r := range m.RemoteIDs {
		switch r.Type {
		case tvdbRemoteSourceIMDB:
			externalIDs["imdb"] = r.ID
		case tvdbRemoteSourceTMDB:
			externalIDs["tmdb"] = r.ID
		}
	}

	cast := tvdbConvertCharacters(m.Characters)
	posterURL, backdropURL := tvdbExtractArtwork(m.Artworks)
	if posterURL == "" {
		posterURL = m.Image
	}

	return &metadata.MediaDetail{
		Title: m.Name, SortTitle: strings.ToLower(m.Name), Year: m.Year,
		Description: m.Overview, PosterURL: posterURL, BackdropURL: backdropURL,
		ExternalIDs: externalIDs, Genres: genres,
		Rating: float64(m.Score) / 10.0, RuntimeMinutes: m.Runtime,
		OriginalTitle: m.OriginalName, OriginalLanguage: m.OriginalLanguage,
		MovieStatus: m.Status.Name, Cast: cast,
	}, nil
}

func (p *TVDBProvider) getSeasonDetail(ctx context.Context, seasonID int) (*tvdbSeasonExtended, error) {
	var resp tvdbAPIResponse[tvdbSeasonExtended]
	if err := p.client.getJSON(ctx, "/api/v1/tvdb/seasons/"+strconv.Itoa(seasonID), &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func tvdbBuildProviderID(r tvdbSearchResult) string {
	id := r.TvdbID
	if id == "" {
		id = r.ObjectID
	}
	if id == "" {
		return ""
	}
	switch r.Type {
	case "movie":
		return "tvdb:movie:" + id
	default:
		return "tvdb:series:" + id
	}
}

func tvdbFilterAiredSeasons(seasons []tvdbSeasonRef) []tvdbSeasonRef {
	var aired []tvdbSeasonRef
	for _, s := range seasons {
		if s.Type.Type == "official" && s.Number > 0 {
			aired = append(aired, s)
		}
	}
	if len(aired) == 0 {
		for _, s := range seasons {
			if s.Number > 0 {
				aired = append(aired, s)
			}
		}
	}
	return aired
}

func tvdbConvertCharacters(chars []tvdbCharacterRef) []metadata.CastMember {
	var cast []metadata.CastMember
	for _, c := range chars {
		if c.PersonName == "" {
			continue
		}
		cast = append(cast, metadata.CastMember{
			Name: c.PersonName, Character: c.Name, Order: c.Sort, ProfilePath: c.PersonImgURL,
		})
		if len(cast) >= 30 {
			break
		}
	}
	return cast
}

func tvdbExtractArtwork(artworks []tvdbArtworkRef) (poster, backdrop string) {
	var bestPoster, bestBackdrop tvdbArtworkRef
	for _, a := range artworks {
		switch a.Type {
		case tvdbArtworkTypePoster:
			if a.Score > bestPoster.Score || bestPoster.Image == "" {
				bestPoster = a
			}
		case tvdbArtworkTypeBackground:
			if a.Score > bestBackdrop.Score || bestBackdrop.Image == "" {
				bestBackdrop = a
			}
		}
	}
	return bestPoster.Image, bestBackdrop.Image
}

// --- TVDB response types ---

type tvdbAPIResponse[T any] struct {
	Status string `json:"status"`
	Data   T      `json:"data"`
}

type tvdbSearchResult struct {
	ObjectID     string `json:"objectID"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	Year         string `json:"year"`
	Type         string `json:"type"`
	TvdbID       string `json:"tvdb_id"`
	ImageURL     string `json:"image_url"`
	FirstAirTime string `json:"first_air_time"`
}

type tvdbSeriesExtended struct {
	ID               int                  `json:"id"`
	Name             string               `json:"name"`
	OriginalName     string               `json:"originalName"`
	Overview         string               `json:"overview"`
	Image            string               `json:"image"`
	FirstAired       string               `json:"firstAired"`
	LastAired        string               `json:"lastAired"`
	Status           tvdbStatus           `json:"status"`
	OriginalLanguage string               `json:"originalLanguage"`
	Genres           []tvdbGenreRef       `json:"genres"`
	Seasons          []tvdbSeasonRef      `json:"seasons"`
	Characters       []tvdbCharacterRef   `json:"characters"`
	Artworks         []tvdbArtworkRef     `json:"artworks"`
	RemoteIDs        []tvdbRemoteIDRef    `json:"remoteIds"`
	OriginalNetwork  *tvdbCompanyRef      `json:"originalNetwork"`
	LatestNetwork    *tvdbCompanyRef      `json:"latestNetwork"`
	Score            int                  `json:"score"`
}

type tvdbMovieExtended struct {
	ID               int                `json:"id"`
	Name             string             `json:"name"`
	OriginalName     string             `json:"originalName"`
	Overview         string             `json:"overview"`
	Image            string             `json:"image"`
	Year             string             `json:"year"`
	Runtime          int                `json:"runtime"`
	Status           tvdbStatus         `json:"status"`
	OriginalLanguage string             `json:"originalLanguage"`
	Genres           []tvdbGenreRef     `json:"genres"`
	Characters       []tvdbCharacterRef `json:"characters"`
	Artworks         []tvdbArtworkRef   `json:"artworks"`
	RemoteIDs        []tvdbRemoteIDRef  `json:"remoteIds"`
	Score            int                `json:"score"`
}

type tvdbSeasonRef struct {
	ID     int    `json:"id"`
	Number int    `json:"number"`
	Type   struct {
		Type string `json:"type"`
	} `json:"type"`
}

type tvdbSeasonExtended struct {
	ID       int              `json:"id"`
	Number   int              `json:"number"`
	Name     string           `json:"name"`
	Overview string           `json:"overview"`
	Image    string           `json:"image"`
	Episodes []tvdbEpisodeRef `json:"episodes"`
}

type tvdbEpisodeRef struct {
	Name    string `json:"name"`
	Overview string `json:"overview"`
	Image   string `json:"image"`
	Number  int    `json:"number"`
	Runtime int    `json:"runtime"`
	Aired   string `json:"aired"`
}

type tvdbCharacterRef struct {
	Name         string `json:"name"`
	PersonName   string `json:"personName"`
	PersonImgURL string `json:"personImgURL"`
	Sort         int    `json:"sort"`
}

type tvdbArtworkRef struct {
	Image string `json:"image"`
	Type  int    `json:"type"`
	Score int    `json:"score"`
}

type tvdbRemoteIDRef struct {
	ID   string `json:"id"`
	Type int    `json:"type"`
}

type tvdbGenreRef struct {
	Name string `json:"name"`
}

type tvdbCompanyRef struct {
	Name string `json:"name"`
}

type tvdbStatus struct {
	Name string `json:"name"`
}

const (
	tvdbRemoteSourceIMDB = 2
	tvdbRemoteSourceTMDB = 12

	tvdbArtworkTypePoster     = 2
	tvdbArtworkTypeBackground = 3
)
