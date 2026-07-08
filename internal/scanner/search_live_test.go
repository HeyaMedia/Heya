package scanner

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/metadata/heyamedia"
)

func TestMovieSearchAgainstHeyaMedia(t *testing.T) {
	baseURL := os.Getenv("HEYA_MEDIA_URL")
	if baseURL == "" {
		t.Skip("HEYA_MEDIA_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	matches := []MovieMatch{
		{
			Key:         "tmdb:438631",
			KeyType:     "tmdb",
			Title:       "Dune",
			Year:        "2021",
			ExternalIDs: map[string]string{"tmdb": "438631"},
		},
		{
			Key:         "imdb:tt0133093",
			KeyType:     "imdb",
			Title:       "The Matrix",
			Year:        "1999",
			ExternalIDs: map[string]string{"imdb": "tt0133093"},
		},
		{
			Key:     "title_year:avatar the way of water|2022",
			KeyType: "title_year",
			Title:   "Avatar The Way of Water",
			Year:    "2022",
		},
		{
			Key:         "imdb:tt0103644",
			KeyType:     "imdb",
			Title:       "Alien",
			Year:        "1992",
			Aliases:     []string{"Alien³"},
			ExternalIDs: map[string]string{"imdb": "tt0103644"},
		},
		{
			Key:     "title_year:jackass presents bad grandpa 5|2014",
			KeyType: "title_year",
			Title:   "Jackass Presents Bad Grandpa .5",
			Year:    "2014",
		},
	}

	provider := heyamedia.NewHeyaProvider(heyamedia.NewClient(baseURL))
	results, err := SearchMovieMatches(ctx, matches, provider, &captureEmitter{})
	if err != nil {
		t.Fatalf("search movie matches: %v", err)
	}
	if len(results) != len(matches) {
		t.Fatalf("results: got %d, want %d", len(results), len(matches))
	}
	for _, result := range results {
		if !result.Accepted {
			t.Fatalf("%s should have accepted a heya.media search candidate: %#v", result.Key, result)
		}
		if result.ProviderID == "" {
			t.Fatalf("%s accepted without provider id: %#v", result.Key, result)
		}
	}
}

func TestMovieFetchPreviewAgainstHeyaMedia(t *testing.T) {
	baseURL := os.Getenv("HEYA_MEDIA_URL")
	if baseURL == "" {
		t.Skip("HEYA_MEDIA_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	provider := heyamedia.NewHeyaProvider(heyamedia.NewClient(baseURL))
	previews, err := FetchMovieMetadataPreviews(ctx, []MovieSearchMatch{
		{Accepted: true, Key: "tmdb:438631", ProviderID: "heya:movie:tmdb:438631", Title: "Dune", Year: "2021"},
	}, provider, &captureEmitter{})
	if err != nil {
		t.Fatalf("fetch movie metadata previews: %v", err)
	}
	if len(previews) != 1 {
		t.Fatalf("previews: got %d, want 1", len(previews))
	}
	preview := previews[0]
	if preview.Error != "" {
		t.Fatalf("preview error: %s", preview.Error)
	}
	if preview.Title == "" || preview.Year == "" || len(preview.WouldApply) == 0 {
		t.Fatalf("preview missing expected metadata: %#v", preview)
	}
}
