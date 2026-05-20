package heyamedia

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/metadata"
)

type OMDBProvider struct {
	client *Client
}

func NewOMDBProvider(c *Client) *OMDBProvider {
	return &OMDBProvider{client: c}
}

func (p *OMDBProvider) Name() string { return "omdb" }

func (p *OMDBProvider) Supports(kind metadata.MediaKind) bool {
	return kind == metadata.KindMovie || kind == metadata.KindTV
}

func (p *OMDBProvider) FetchRatings(ctx context.Context, externalIDs map[string]string) (*metadata.RatingsData, error) {
	imdbID := externalIDs["imdb"]
	if imdbID == "" {
		return nil, nil
	}

	var resp omdbResponse
	if err := p.client.getJSON(ctx, "/api/v1/omdb/"+imdbID, &resp); err != nil {
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
			Source: omdbNormalizeSource(r.Source),
			Value:  r.Value,
			Score:  omdbParseScore(r.Source, r.Value),
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

// Search implements metadata.Provider for OMDB (used as secondary search).
func (p *OMDBProvider) Search(ctx context.Context, kind metadata.MediaKind, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	params := url.Values{"s": {query.Title}}
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
	if err := p.client.get(ctx, "/api/v1/omdb/search", params, &resp); err != nil {
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
			PosterURL:    omdbCleanPoster(r.Poster),
		})
	}
	return results, nil
}

func (p *OMDBProvider) GetDetail(ctx context.Context, providerID string, opts *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	parts := strings.SplitN(providerID, ":", 2)
	if len(parts) != 2 {
		return nil, nil
	}

	params := url.Values{"plot": {"full"}}
	var resp omdbResponse
	if err := p.client.get(ctx, "/api/v1/omdb/"+parts[1], params, &resp); err != nil {
		return nil, err
	}
	if resp.Response != "True" {
		return nil, nil
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
		PosterURL:      omdbCleanPoster(resp.Poster),
		ExternalIDs:    map[string]string{"imdb": resp.ImdbID},
		Genres:         genres,
		Rating:         omdbParseFloat(resp.ImdbRating),
		RuntimeMinutes: runtime,
	}, nil
}

func omdbNormalizeSource(source string) string {
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

func omdbParseScore(source, value string) float64 {
	switch source {
	case "Internet Movie Database":
		f, _ := strconv.ParseFloat(strings.TrimSuffix(value, "/10"), 64)
		return f * 10
	case "Rotten Tomatoes":
		f, _ := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
		return f
	case "Metacritic":
		f, _ := strconv.ParseFloat(strings.Split(value, "/")[0], 64)
		return f
	}
	return 0
}

func omdbParseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func omdbCleanPoster(u string) string {
	if u == "N/A" || u == "" {
		return ""
	}
	return u
}

// --- OMDB response types ---

type omdbResponse struct {
	Response  string       `json:"Response"`
	Error     string       `json:"Error"`
	Title     string       `json:"Title"`
	Year      string       `json:"Year"`
	Runtime   string       `json:"Runtime"`
	Genre     string       `json:"Genre"`
	Plot      string       `json:"Plot"`
	Awards    string       `json:"Awards"`
	Poster    string       `json:"Poster"`
	Ratings   []omdbRating `json:"Ratings"`
	Metascore string       `json:"Metascore"`
	ImdbRating string      `json:"imdbRating"`
	ImdbID    string       `json:"imdbID"`
	BoxOffice string       `json:"BoxOffice"`
}

type omdbRating struct {
	Source string `json:"Source"`
	Value  string `json:"Value"`
}

type omdbSearchResponse struct {
	Response string             `json:"Response"`
	Search   []omdbSearchResult `json:"Search"`
}

type omdbSearchResult struct {
	Title  string `json:"Title"`
	Year   string `json:"Year"`
	ImdbID string `json:"imdbID"`
	Poster string `json:"Poster"`
}
