package fanart

import (
	"context"
	"fmt"
	"strconv"

	"github.com/karbowiak/heya/internal/metadata"
)

const defaultBaseURL = "http://webservice.fanart.tv/v3"

type Provider struct {
	client  *metadata.RateLimitedClient
	apiKey  string
	BaseURL string
}

func NewProvider(apiKey string) *Provider {
	client := metadata.NewRateLimitedClient(1.0, 1, "Heya/1.0")
	return &Provider{client: client, apiKey: apiKey, BaseURL: defaultBaseURL}
}

func (p *Provider) Name() string { return "fanart.tv" }

func (p *Provider) FetchArtwork(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string) ([]metadata.ArtworkResult, error) {
	tmdbID := externalIDs["tmdb"]
	if tmdbID == "" {
		return nil, nil
	}

	switch kind {
	case metadata.KindMovie:
		return p.fetchMovieArt(ctx, tmdbID)
	case metadata.KindTV:
		return p.fetchTVArt(ctx, tmdbID)
	default:
		return nil, nil
	}
}

func (p *Provider) fetchMovieArt(ctx context.Context, tmdbID string) ([]metadata.ArtworkResult, error) {
	u := fmt.Sprintf("%s/movies/%s?api_key=%s", p.BaseURL, tmdbID, p.apiKey)
	var resp movieResponse
	if err := p.client.GetJSON(ctx, u, &resp); err != nil {
		return nil, err
	}

	var results []metadata.ArtworkResult
	addImages(&results, resp.HDMovieLogo, "clearlogo")
	addImages(&results, resp.MovieLogo, "clearlogo")
	addImages(&results, resp.MovieBanner, "banner")
	addImages(&results, resp.MovieThumb, "landscape")
	addImages(&results, resp.MovieBackground, "fanart")
	addImages(&results, resp.MoviePoster, "poster")
	return results, nil
}

func (p *Provider) fetchTVArt(ctx context.Context, tmdbID string) ([]metadata.ArtworkResult, error) {
	u := fmt.Sprintf("%s/tv/%s?api_key=%s", p.BaseURL, tmdbID, p.apiKey)
	var resp tvResponse
	if err := p.client.GetJSON(ctx, u, &resp); err != nil {
		return nil, err
	}

	var results []metadata.ArtworkResult
	addImages(&results, resp.HDTVLogo, "clearlogo")
	addImages(&results, resp.ClearLogo, "clearlogo")
	addImages(&results, resp.TVBanner, "banner")
	addImages(&results, resp.TVThumb, "landscape")
	addImages(&results, resp.ShowBackground, "fanart")
	addImages(&results, resp.TVPoster, "poster")
	return results, nil
}

func addImages(results *[]metadata.ArtworkResult, images []artImage, assetType string) {
	for _, img := range images {
		likes, _ := strconv.Atoi(img.Likes)
		*results = append(*results, metadata.ArtworkResult{
			URL:       img.URL,
			AssetType: assetType,
			Language:  img.Lang,
			Likes:     likes,
		})
	}
}
