package ingestv2

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

type fakeMovieDetailProvider struct {
	details map[string]*metadata.MediaDetail
	calls   []string
}

func (f *fakeMovieDetailProvider) GetDetail(_ context.Context, providerID string, _ *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	f.calls = append(f.calls, providerID)
	detail := f.details[providerID]
	if detail == nil {
		return nil, fmt.Errorf("missing detail")
	}
	return detail, nil
}

func TestFetchMovieMetadataPreviewsOnlyFetchesAcceptedMatches(t *testing.T) {
	provider := &fakeMovieDetailProvider{details: map[string]*metadata.MediaDetail{
		"heya:movie:tmdb:438631": {
			Title:          "Dune",
			Year:           "2021",
			Description:    "Spice must flow.",
			ExternalIDs:    map[string]string{"tmdb": "438631", "imdb": "tt1160419"},
			RuntimeMinutes: 155,
			Genres:         []string{"Science Fiction", "Adventure"},
			PosterURL:      "poster.webp",
			BackdropURL:    "backdrop.webp",
			Collection:     &metadata.CollectionDetail{Name: "Dune Collection"},
			Cast:           []metadata.CastMember{{Name: "Timothee Chalamet"}},
		},
	}}
	search := []MovieSearchMatch{
		{Accepted: true, Key: "tmdb:438631", ProviderID: "heya:movie:tmdb:438631", Title: "Dune", Year: "2021"},
		{Accepted: false, Key: "title_year:bad|2024", ProviderID: "heya:movie:tmdb:1", Title: "Bad", Year: "2024"},
	}

	emit := &captureEmitter{}
	previews, err := FetchMovieMetadataPreviews(context.Background(), search, provider, emit)
	if err != nil {
		t.Fatalf("fetch previews: %v", err)
	}
	if len(provider.calls) != 1 || provider.calls[0] != "heya:movie:tmdb:438631" {
		t.Fatalf("provider calls: %#v", provider.calls)
	}
	if len(previews) != 1 {
		t.Fatalf("previews: got %d, want 1", len(previews))
	}
	preview := previews[0]
	if preview.Title != "Dune" || preview.Year != "2021" {
		t.Fatalf("preview title/year: %#v", preview)
	}
	if preview.Collection != "Dune Collection" {
		t.Fatalf("collection: got %q", preview.Collection)
	}
	for _, field := range []string{"backdrop", "cast", "collection", "description", "external_ids", "genres", "poster", "runtime", "title", "year"} {
		if !contains(preview.WouldApply, field) {
			t.Fatalf("would_apply missing %q: %#v", field, preview.WouldApply)
		}
	}
	if !eventSeen(emit.events, "metadata.preview") {
		t.Fatalf("expected metadata.preview event")
	}
}

func TestFetchTVMetadataPreviewsMapsPlannedEpisodes(t *testing.T) {
	provider := &fakeMovieDetailProvider{details: map[string]*metadata.MediaDetail{
		"heya:tv:tmdb:1396": {
			Title:        "Breaking Bad",
			Year:         "2008",
			Description:  "A chemistry teacher makes choices.",
			ExternalIDs:  map[string]string{"tmdb": "1396", "tvdb": "81189"},
			Genres:       []string{"Drama"},
			PosterURL:    "poster.webp",
			BackdropURL:  "backdrop.webp",
			Status:       "Ended",
			FirstAirDate: "2008-01-20",
			LastAirDate:  "2013-09-29",
			Networks:     []metadata.NetworkDetail{{Name: "AMC"}},
			Artwork:      []metadata.ArtworkResult{{URL: "logo.webp", AssetType: "logo"}},
			Cast:         []metadata.CastMember{{Name: "Bryan Cranston"}},
			Crew:         []metadata.CrewMember{{Name: "Vince Gilligan", Job: "Creator"}},
			Seasons: []metadata.SeasonDetail{{
				Number: 1,
				Title:  "Season 1",
				Episodes: []metadata.EpisodeDetail{
					{Number: 1, Title: "Pilot"},
					{Number: 2, Title: "Cat's in the Bag..."},
				},
			}, {
				Number: 2,
				Title:  "Season 2",
				Episodes: []metadata.EpisodeDetail{
					{Number: 1, Title: "Seven Thirty-Seven"},
				},
			}},
		},
	}}
	search := []TVSearchMatch{
		{Accepted: true, Key: "tmdb:1396", ProviderID: "heya:tv:tmdb:1396", Title: "Breaking Bad", Year: "2008"},
		{Accepted: true, Key: "title:breaking bad", ProviderID: "heya:tv:tmdb:1396", Title: "Breaking Bad", Year: "2008"},
		{Accepted: false, Key: "title:poker face", ProviderID: "heya:tv:tmdb:120998", Title: "Poker Face", Year: "2023"},
	}
	matches := []TVMatch{
		{
			Key:   "tmdb:1396",
			Title: "Breaking Bad",
			Year:  "2008",
			Files: []string{
				"Breaking Bad (2008)/Season 01/Breaking.Bad.S01E01.mkv",
				"Breaking Bad (2008)/Season 01/Breaking.Bad.S01E02.mkv",
				"Breaking Bad (2008)/Season 01/Breaking.Bad.S01E99.mkv",
			},
			Episodes: []TVEpisodeRef{
				{Season: 1, Episode: 1},
				{Season: 1, Episode: 2},
				{Season: 1, Episode: 99},
			},
		},
		{
			Key:   "title:breaking bad",
			Title: "Breaking Bad",
			Files: []string{"Loose/Breaking.Bad.S02E01.mkv"},
			Episodes: []TVEpisodeRef{
				{Season: 2, Episode: 1},
			},
		},
	}

	emit := &captureEmitter{}
	previews, err := FetchTVMetadataPreviews(context.Background(), search, matches, provider, emit)
	if err != nil {
		t.Fatalf("fetch TV previews: %v", err)
	}
	if len(provider.calls) != 1 || provider.calls[0] != "heya:tv:tmdb:1396" {
		t.Fatalf("provider calls: %#v", provider.calls)
	}
	if len(previews) != 1 {
		t.Fatalf("previews: got %d, want 1", len(previews))
	}

	preview := previews[0]
	if preview.Title != "Breaking Bad" || preview.Year != "2008" {
		t.Fatalf("preview title/year: %#v", preview)
	}
	if preview.Seasons != 2 || preview.RemoteEpisodes != 3 {
		t.Fatalf("remote counts: seasons=%d episodes=%d", preview.Seasons, preview.RemoteEpisodes)
	}
	if preview.PlannedEpisodes != 4 || preview.MappedEpisodes != 3 {
		t.Fatalf("mapped episodes: got %d/%d, want 3/4", preview.MappedEpisodes, preview.PlannedEpisodes)
	}
	if preview.PlannedFiles != 4 || preview.LocalIdentities != 2 {
		t.Fatalf("local counts: files=%d identities=%d", preview.PlannedFiles, preview.LocalIdentities)
	}
	if len(preview.MissingEpisodes) != 1 || preview.MissingEpisodes[0].Episode != 99 {
		t.Fatalf("missing episodes: %#v", preview.MissingEpisodes)
	}
	for _, field := range []string{"backdrop", "cast", "crew", "description", "episodes", "external_ids", "first_air_date", "genres", "last_air_date", "networks", "poster", "seasons", "status", "title", "year"} {
		if !contains(preview.WouldApply, field) {
			t.Fatalf("would_apply missing %q: %#v", field, preview.WouldApply)
		}
	}
	if !eventSeen(emit.events, "metadata.preview") {
		t.Fatalf("expected metadata.preview event")
	}

	var report bytes.Buffer
	WriteReport(&report, sqlc.Library{ID: 2, Name: "DevTV", MediaType: sqlc.MediaTypeTv}, Result{
		TVMatches:  matches,
		TVSearch:   search,
		TVMetadata: previews,
	}, emit.events)
	for _, want := range []string{
		"Metadata fetched:      1/1",
		"Metadata fetch preview",
		"Breaking Bad (2008) provider=heya:tv:tmdb:1396",
		"mapped=3/4",
		"local_identities=2",
		"missing=S01E99",
	} {
		if !strings.Contains(report.String(), want) {
			t.Fatalf("TV metadata report missing %q:\n%s", want, report.String())
		}
	}
}
