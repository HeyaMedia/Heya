package omdb

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

const baseURL = "https://www.omdbapi.com/"

type Provider struct {
	client *metadata.RateLimitedClient
	apiKey string
}

func NewProvider(apiKey string) *Provider {
	client := metadata.NewRateLimitedClient(1.5, 2, "Heya/1.0")
	return &Provider{client: client, apiKey: apiKey}
}

func (p *Provider) Name() string { return "omdb" }

func (p *Provider) Supports(kind metadata.MediaKind) bool {
	return kind == metadata.KindMovie || kind == metadata.KindTV
}

// --- Provider interface (basic search + detail) ---

func (p *Provider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	params := url.Values{
		"apikey": {p.apiKey},
		"s":      {query.Title},
	}
	switch kind {
	case metadata.KindMovie:
		params.Set("type", "movie")
	case metadata.KindTV:
		params.Set("type", "series")
	}
	if query.Year != "" {
		params.Set("y", query.Year)
	}

	var resp omdbSearchResponse
	if err := p.get(ctx, params, &resp); err != nil {
		return nil, err
	}
	if resp.Response != "True" {
		return nil, nil
	}

	var results []metadata.SearchResult
	for i, r := range resp.Search {
		if i >= 10 {
			break
		}
		results = append(results, metadata.SearchResult{
			ProviderID:   "omdb:" + r.ImdbID,
			ProviderName: "omdb",
			Title:        r.Title,
			Year:         r.Year,
			PosterURL:    cleanPoster(r.Poster),
		})
	}
	return results, nil
}

func (p *Provider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	parts := strings.SplitN(providerID, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid omdb provider ID: %s", providerID)
	}
	imdbID := parts[1]

	params := url.Values{
		"apikey": {p.apiKey},
		"i":      {imdbID},
		"plot":   {"full"},
	}

	var resp omdbResponse
	if err := p.get(ctx, params, &resp); err != nil {
		return nil, err
	}
	if resp.Response != "True" {
		return nil, fmt.Errorf("omdb: %s", resp.Error)
	}

	runtime := 0
	if mins := strings.TrimSuffix(resp.Runtime, " min"); mins != resp.Runtime {
		runtime, _ = strconv.Atoi(mins)
	}

	genres := strings.Split(resp.Genre, ", ")

	return &metadata.MediaDetail{
		Title:          resp.Title,
		SortTitle:      strings.ToLower(resp.Title),
		Year:           resp.Year,
		Description:    resp.Plot,
		PosterURL:      cleanPoster(resp.Poster),
		ExternalIDs:    map[string]string{"imdb": resp.ImdbID},
		Genres:         genres,
		Rating:         parseFloat(resp.ImdbRating),
		RuntimeMinutes: runtime,
	}, nil
}

// --- RatingsProvider interface ---

func (p *Provider) FetchRatings(ctx context.Context, externalIDs map[string]string) (*metadata.RatingsData, error) {
	imdbID := externalIDs["imdb"]
	if imdbID == "" {
		return nil, nil
	}

	params := url.Values{
		"apikey": {p.apiKey},
		"i":      {imdbID},
	}

	var resp omdbResponse
	if err := p.get(ctx, params, &resp); err != nil {
		return nil, err
	}
	if resp.Response != "True" {
		return nil, nil
	}

	data := &metadata.RatingsData{
		Awards:    resp.Awards,
		BoxOffice: resp.BoxOffice,
	}

	for _, r := range resp.Ratings {
		er := metadata.ExternalRating{
			Source: normalizeSource(r.Source),
			Value:  r.Value,
			Score:  parseScore(r.Source, r.Value),
		}
		if er.Source != "" {
			data.Ratings = append(data.Ratings, er)
		}
	}

	if resp.Metascore != "" && resp.Metascore != "N/A" {
		found := false
		for _, r := range data.Ratings {
			if r.Source == "metacritic" {
				found = true
				break
			}
		}
		if !found {
			score, _ := strconv.ParseFloat(resp.Metascore, 64)
			data.Ratings = append(data.Ratings, metadata.ExternalRating{
				Source: "metacritic",
				Value:  resp.Metascore + "/100",
				Score:  score,
			})
		}
	}

	return data, nil
}

func (p *Provider) get(ctx context.Context, params url.Values, result any) error {
	u := baseURL + "?" + params.Encode()
	return p.client.GetJSON(ctx, u, result)
}

func normalizeSource(source string) string {
	switch source {
	case "Internet Movie Database":
		return "imdb"
	case "Rotten Tomatoes":
		return "rotten_tomatoes"
	case "Metacritic":
		return "metacritic"
	default:
		return strings.ToLower(strings.ReplaceAll(source, " ", "_"))
	}
}

func parseScore(source, value string) float64 {
	switch source {
	case "Internet Movie Database":
		v := strings.TrimSuffix(value, "/10")
		f, _ := strconv.ParseFloat(v, 64)
		return f * 10
	case "Rotten Tomatoes":
		v := strings.TrimSuffix(value, "%")
		f, _ := strconv.ParseFloat(v, 64)
		return f
	case "Metacritic":
		v := strings.Split(value, "/")[0]
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func cleanPoster(u string) string {
	if u == "N/A" || u == "" {
		return ""
	}
	return u
}
