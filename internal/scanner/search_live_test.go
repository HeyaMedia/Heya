package scanner

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	heyametadata "github.com/karbowiak/heya/internal/metadata/heyametadata"
)

func TestMovieSearchAgainstHeyaMetadata(t *testing.T) {
	baseURL := os.Getenv("HEYA_METADATA_URL")
	if baseURL == "" {
		t.Skip("HEYA_METADATA_URL not set")
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
			Key:         "imdb:tt0103644",
			KeyType:     "imdb",
			Title:       "Alien",
			Year:        "1992",
			Aliases:     []string{"Alien³"},
			ExternalIDs: map[string]string{"imdb": "tt0103644"},
		},
	}

	client, err := heyametadata.NewClient(baseURL, os.Getenv("HEYA_METADATA_API_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	provider := heyametadata.NewHeyaProvider(client)
	results, err := SearchMovieMatches(ctx, matches, provider, &captureEmitter{}, 0)
	if err != nil {
		t.Fatalf("search movie matches: %v", err)
	}
	if len(results) != len(matches) {
		t.Fatalf("results: got %d, want %d", len(results), len(matches))
	}
	for _, result := range results {
		if !result.Accepted {
			if len(result.Candidates) == 0 || !result.Candidates[0].RequiresReview {
				t.Fatalf("%s was neither safely accepted nor returned for required review: %#v", result.Key, result)
			}
			continue
		}
		if result.ProviderID == "" {
			t.Fatalf("%s accepted without provider id: %#v", result.Key, result)
		}
	}
}

func TestTVSearchAndFetchAgainstHeyaMetadata(t *testing.T) {
	baseURL := os.Getenv("HEYA_METADATA_URL")
	if baseURL == "" {
		t.Skip("HEYA_METADATA_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	client, err := heyametadata.NewClient(baseURL, os.Getenv("HEYA_METADATA_API_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	provider := heyametadata.NewHeyaProvider(client)
	matches := []TVMatch{{
		Key:         "tmdb:1396",
		KeyType:     "tmdb",
		Title:       "Breaking Bad",
		Year:        "2008",
		ExternalIDs: map[string]string{"tmdb": "1396", "imdb": "tt0903747"},
		Episodes:    []TVEpisodeRef{{Season: 1, Episode: 1}},
		Files:       []string{"Breaking Bad (2008)/Season 01/Breaking.Bad.S01E01.mkv"},
	}}

	search, err := SearchTVMatches(ctx, matches, provider, &captureEmitter{}, 0)
	if err != nil {
		t.Fatalf("search TV matches: %v", err)
	}
	if len(search) != 1 || !search[0].Accepted {
		t.Fatalf("TV match was not safely accepted: %#v", search)
	}
	if !strings.HasPrefix(search[0].ProviderID, "heyametadata:v2:entity:") {
		t.Fatalf("TV identifier discovery did not return a canonical Heya UUID: %q", search[0].ProviderID)
	}

	previews, err := FetchTVMetadataPreviews(ctx, search, matches, provider, &captureEmitter{})
	if err != nil {
		t.Fatalf("fetch TV metadata: %v", err)
	}
	if len(previews) != 1 || previews[0].Error != "" || previews[0].Title == "" {
		t.Fatalf("TV resolution did not materialize: %#v", previews)
	}
}

func TestMovieFetchPreviewAgainstHeyaMetadata(t *testing.T) {
	baseURL := os.Getenv("HEYA_METADATA_URL")
	if baseURL == "" {
		t.Skip("HEYA_METADATA_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client, err := heyametadata.NewClient(baseURL, os.Getenv("HEYA_METADATA_API_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	provider := heyametadata.NewHeyaProvider(client)
	matches := []MovieMatch{{
		Key: "tmdb:438631", KeyType: "tmdb", Title: "Dune", Year: "2021",
		ExternalIDs: map[string]string{"tmdb": "438631", "imdb": "tt1160419"},
	}}
	search, err := SearchMovieMatches(ctx, matches, provider, &captureEmitter{}, 0)
	if err != nil {
		t.Fatalf("search movie metadata: %v", err)
	}
	if len(search) != 1 || !search[0].Accepted || !strings.HasPrefix(search[0].ProviderID, "heyametadata:v2:entity:") {
		t.Fatalf("movie identifier discovery did not return a canonical entity: %#v", search)
	}
	previews, err := FetchMovieMetadataPreviews(ctx, []MovieSearchMatch{
		search[0],
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
