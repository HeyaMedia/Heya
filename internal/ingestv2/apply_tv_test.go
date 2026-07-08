package ingestv2

import (
	"encoding/json"
	"testing"
)

func TestTVLibraryFileParseResultCarriesEpisodeArrays(t *testing.T) {
	raw := tvLibraryFileParseResult(TVMaterializePreview{
		Key:        "tmdb:1396",
		ProviderID: "heya:tv:tmdb:1396",
		Title:      "Breaking Bad",
		Year:       "2008",
	}, TVPlan{
		Title:    "Breaking Bad",
		Year:     "2008",
		Season:   1,
		Episodes: []int{1, 2},
	}, "Breaking Bad (2008)/Season 01/Breaking.Bad.S01E01-E02.mkv")

	var parsed struct {
		Scanner    string `json:"scanner"`
		ProviderID string `json:"provider_id"`
		Parsed     struct {
			Release struct {
				Title    string `json:"title"`
				Year     string `json:"year"`
				IsTv     bool   `json:"isTv"`
				Seasons  []int  `json:"seasons"`
				Episodes []int  `json:"episodes"`
			} `json:"release"`
		} `json:"parsed"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse result JSON: %v", err)
	}
	if parsed.Scanner != "ingestv2" || parsed.ProviderID != "heya:tv:tmdb:1396" {
		t.Fatalf("scanner/provider: %#v", parsed)
	}
	if parsed.Parsed.Release.Title != "Breaking Bad" || parsed.Parsed.Release.Year != "2008" || !parsed.Parsed.Release.IsTv {
		t.Fatalf("release identity: %#v", parsed.Parsed.Release)
	}
	if len(parsed.Parsed.Release.Seasons) != 1 || parsed.Parsed.Release.Seasons[0] != 1 {
		t.Fatalf("seasons: %#v", parsed.Parsed.Release.Seasons)
	}
	if len(parsed.Parsed.Release.Episodes) != 2 || parsed.Parsed.Release.Episodes[0] != 1 || parsed.Parsed.Release.Episodes[1] != 2 {
		t.Fatalf("episodes: %#v", parsed.Parsed.Release.Episodes)
	}
}
