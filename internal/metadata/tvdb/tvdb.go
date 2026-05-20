package tvdb

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

const baseURL = "https://api4.thetvdb.com/v4"

type Provider struct {
	client *metadata.RateLimitedClient
	auth   *tokenManager
}

func NewProvider(apiKey string) *Provider {
	client := metadata.NewRateLimitedClient(2.0, 2, "Heya/1.0")
	return &Provider{
		client: client,
		auth:   newTokenManager(apiKey, baseURL),
	}
}

func (p *Provider) Name() string { return "tvdb" }

func (p *Provider) Supports(kind metadata.MediaKind) bool {
	return kind == metadata.KindTV || kind == metadata.KindMovie
}

func (p *Provider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
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

	var resp apiResponse[[]searchResult]
	if err := p.get(ctx, "/search", params, &resp); err != nil {
		return nil, err
	}

	var results []metadata.SearchResult
	for i, r := range resp.Data {
		if i >= 10 {
			break
		}

		providerID := buildProviderID(r)
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

func (p *Provider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	parts := strings.SplitN(providerID, ":", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid tvdb provider ID: %s", providerID)
	}
	kind := parts[1]
	id := parts[2]

	switch kind {
	case "series":
		return p.getSeriesDetail(ctx, id, opts)
	case "movie":
		return p.getMovieDetail(ctx, id, opts)
	default:
		return nil, fmt.Errorf("unknown tvdb kind: %s", kind)
	}
}

func (p *Provider) LookupByNFO(ctx context.Context, kind metadata.MediaKind, ids metadata.NFOIDs, opts *metadata.FetchOptions) (*metadata.MediaDetail, string, error) {
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

func (p *Provider) getSeriesDetail(ctx context.Context, id string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	var resp apiResponse[seriesExtended]
	params := url.Values{"meta": {"episodes"}}
	if err := p.get(ctx, "/series/"+id+"/extended", params, &resp); err != nil {
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
		case remoteSourceIMDB:
			externalIDs["imdb"] = r.ID
		case remoteSourceTMDB:
			externalIDs["tmdb"] = r.ID
		}
	}

	cast := convertCharacters(s.Characters)

	airedSeasons := filterAiredSeasons(s.Seasons)

	var seasons []metadata.SeasonDetail
	for _, sref := range airedSeasons {
		sd, err := p.getSeasonDetail(ctx, sref.ID)
		if err != nil {
			continue
		}

		var episodes []metadata.EpisodeDetail
		for _, ep := range sd.Episodes {
			episodes = append(episodes, metadata.EpisodeDetail{
				Number:         ep.Number,
				Title:          ep.Name,
				Overview:       ep.Overview,
				StillURL:       ep.Image,
				RuntimeMinutes: ep.Runtime,
				AirDate:        ep.Aired,
			})
		}

		seasons = append(seasons, metadata.SeasonDetail{
			Number:    sd.Number,
			Title:     sd.Name,
			Overview:  sd.Overview,
			PosterURL: sd.Image,
			Episodes:  episodes,
		})
	}

	posterURL, backdropURL := extractArtwork(s.Artworks)
	if posterURL == "" {
		posterURL = s.Image
	}

	totalEpisodes := 0
	for _, ss := range seasons {
		totalEpisodes += len(ss.Episodes)
	}

	return &metadata.MediaDetail{
		Title:            s.Name,
		SortTitle:        strings.ToLower(s.Name),
		Year:             year,
		Description:      s.Overview,
		PosterURL:        posterURL,
		BackdropURL:      backdropURL,
		ExternalIDs:      externalIDs,
		Genres:           genres,
		Rating:           float64(s.Score) / 10.0,
		Status:           s.Status.Name,
		FirstAirDate:     s.FirstAired,
		LastAirDate:      s.LastAired,
		OriginalName:     s.OriginalName,
		OriginalLanguage: s.OriginalLanguage,
		Networks:         networks,
		NumberOfSeasons:  len(airedSeasons),
		NumberOfEpisodes: totalEpisodes,
		Popularity:       float64(s.Score),
		Cast:             cast,
		Seasons:          seasons,
	}, nil
}

func (p *Provider) getMovieDetail(ctx context.Context, id string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	var resp apiResponse[movieExtended]
	if err := p.get(ctx, "/movies/"+id+"/extended", nil, &resp); err != nil {
		return nil, err
	}
	m := resp.Data

	year := m.Year
	if year == "" && len(m.Name) > 0 {
		year = ""
	}

	genres := make([]string, len(m.Genres))
	for i, g := range m.Genres {
		genres[i] = g.Name
	}

	externalIDs := map[string]string{"tvdb": id}
	for _, r := range m.RemoteIDs {
		switch r.Type {
		case remoteSourceIMDB:
			externalIDs["imdb"] = r.ID
		case remoteSourceTMDB:
			externalIDs["tmdb"] = r.ID
		}
	}

	cast := convertCharacters(m.Characters)

	posterURL, backdropURL := extractArtwork(m.Artworks)
	if posterURL == "" {
		posterURL = m.Image
	}

	return &metadata.MediaDetail{
		Title:            m.Name,
		SortTitle:        strings.ToLower(m.Name),
		Year:             year,
		Description:      m.Overview,
		PosterURL:        posterURL,
		BackdropURL:      backdropURL,
		ExternalIDs:      externalIDs,
		Genres:           genres,
		Rating:           float64(m.Score) / 10.0,
		RuntimeMinutes:   m.Runtime,
		OriginalTitle:    m.OriginalName,
		OriginalLanguage: m.OriginalLanguage,
		MovieStatus:      m.Status.Name,
		Cast:             cast,
	}, nil
}

func (p *Provider) getSeasonDetail(ctx context.Context, seasonID int) (*seasonExtended, error) {
	var resp apiResponse[seasonExtended]
	if err := p.get(ctx, "/seasons/"+strconv.Itoa(seasonID)+"/extended", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (p *Provider) get(ctx context.Context, path string, params url.Values, result any) error {
	token, err := p.auth.getToken(ctx)
	if err != nil {
		return err
	}

	u := baseURL + path
	if params != nil {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		p.auth.mu.Lock()
		p.auth.token = ""
		p.auth.mu.Unlock()
		return fmt.Errorf("tvdb: unauthorized (token expired)")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("tvdb %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func buildProviderID(r searchResult) string {
	tvdbID := r.TvdbID
	if tvdbID == "" {
		tvdbID = r.ObjectID
	}
	if tvdbID == "" {
		return ""
	}

	switch r.Type {
	case "series":
		return "tvdb:series:" + tvdbID
	case "movie":
		return "tvdb:movie:" + tvdbID
	default:
		return "tvdb:series:" + tvdbID
	}
}

func filterAiredSeasons(seasons []seasonRef) []seasonRef {
	var aired []seasonRef
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

func convertCharacters(chars []characterRef) []metadata.CastMember {
	var cast []metadata.CastMember
	for _, c := range chars {
		if c.PersonName == "" {
			continue
		}
		cast = append(cast, metadata.CastMember{
			Name:        c.PersonName,
			Character:   c.Name,
			Order:       c.Sort,
			ProfilePath: c.PersonImgURL,
		})
		if len(cast) >= 30 {
			break
		}
	}
	return cast
}

func extractArtwork(artworks []artworkRef) (poster, backdrop string) {
	var bestPoster, bestBackdrop artworkRef
	for _, a := range artworks {
		switch a.Type {
		case artworkTypePoster:
			if a.Score > bestPoster.Score || bestPoster.Image == "" {
				bestPoster = a
			}
		case artworkTypeBackground:
			if a.Score > bestBackdrop.Score || bestBackdrop.Image == "" {
				bestBackdrop = a
			}
		}
	}
	return bestPoster.Image, bestBackdrop.Image
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
