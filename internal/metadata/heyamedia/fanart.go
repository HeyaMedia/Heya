package heyamedia

import (
	"context"
	"fmt"
	"strconv"

	"github.com/karbowiak/heya/internal/metadata"
)

type FanartProvider struct {
	client *Client
}

func NewFanartProvider(c *Client) *FanartProvider {
	return &FanartProvider{client: c}
}

func (p *FanartProvider) Name() string { return "fanart.tv" }

func (p *FanartProvider) FetchArtwork(ctx context.Context, kind metadata.MediaKind, externalIDs map[string]string) ([]metadata.ArtworkResult, error) {
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

func (p *FanartProvider) fetchMovieArt(ctx context.Context, tmdbID string) ([]metadata.ArtworkResult, error) {
	var resp fanartMovieResponse
	if err := p.client.getJSON(ctx, fmt.Sprintf("/api/v1/fanart/movies/%s", tmdbID), &resp); err != nil {
		return nil, err
	}

	var results []metadata.ArtworkResult
	fanartAddImages(&results, resp.HDMovieLogo, "clearlogo")
	fanartAddImages(&results, resp.MovieLogo, "clearlogo")
	fanartAddImages(&results, resp.MovieBanner, "banner")
	fanartAddImages(&results, resp.MovieThumb, "landscape")
	fanartAddImages(&results, resp.MovieBackground, "fanart")
	fanartAddImages(&results, resp.MoviePoster, "poster")
	return results, nil
}

func (p *FanartProvider) fetchTVArt(ctx context.Context, tmdbID string) ([]metadata.ArtworkResult, error) {
	var resp fanartTVResponse
	if err := p.client.getJSON(ctx, fmt.Sprintf("/api/v1/fanart/tv/%s", tmdbID), &resp); err != nil {
		return nil, err
	}

	var results []metadata.ArtworkResult
	fanartAddImages(&results, resp.HDTVLogo, "clearlogo")
	fanartAddImages(&results, resp.ClearLogo, "clearlogo")
	fanartAddImages(&results, resp.TVBanner, "banner")
	fanartAddImages(&results, resp.TVThumb, "landscape")
	fanartAddImages(&results, resp.ShowBackground, "fanart")
	fanartAddImages(&results, resp.TVPoster, "poster")
	return results, nil
}

func fanartAddImages(results *[]metadata.ArtworkResult, images []fanartImage, assetType string) {
	for _, img := range images {
		likes, _ := strconv.Atoi(img.Likes)
		*results = append(*results, metadata.ArtworkResult{
			URL: img.URL, AssetType: assetType, Language: img.Lang, Likes: likes,
		})
	}
}

// --- Fanart response types ---

type fanartMovieResponse struct {
	HDMovieLogo     []fanartImage `json:"hdmovielogo"`
	MovieLogo       []fanartImage `json:"movielogo"`
	MoviePoster     []fanartImage `json:"movieposter"`
	MovieBackground []fanartImage `json:"moviebackground"`
	MovieBanner     []fanartImage `json:"moviebanner"`
	MovieThumb      []fanartImage `json:"moviethumb"`
}

type fanartTVResponse struct {
	HDTVLogo       []fanartImage `json:"hdtvlogo"`
	TVPoster       []fanartImage `json:"tvposter"`
	TVBanner       []fanartImage `json:"tvbanner"`
	ShowBackground []fanartImage `json:"showbackground"`
	TVThumb        []fanartImage `json:"tvthumb"`
	ClearLogo      []fanartImage `json:"clearlogo"`
}

type fanartImage struct {
	URL   string `json:"url"`
	Lang  string `json:"lang"`
	Likes string `json:"likes"`
}
