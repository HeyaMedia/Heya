package scanner

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/stretchr/testify/require"
)

func TestSearchArtifactRoundTripRestoresInventory(t *testing.T) {
	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{
			Root: "/media/movies",
			Files: []InventoryFile{{
				Root:    "/media/movies",
				Path:    "/media/movies/Dune (2021)/Dune.mkv",
				RelPath: "Dune (2021)/Dune.mkv",
				Name:    "Dune.mkv",
				Class:   ClassPrimaryMedia,
				Size:    123,
				MTime:   time.Unix(100, 0).UTC(),
			}},
		}}},
		MovieMatches: []MovieMatch{{
			Key:   "title_year:dune|2021",
			Title: "Dune",
			Year:  "2021",
			Files: []string{"Dune (2021)/Dune.mkv"},
		}},
		MovieSearch: []MovieSearchMatch{{
			Key:        "title_year:dune|2021",
			Accepted:   true,
			ProviderID: "heya:movie:tmdb:438631",
			Title:      "Dune",
			Year:       "2021",
		}},
	}

	data, err := marshalSearchArtifact(Options{ScopePaths: []string{"Dune (2021)"}}, result)
	require.NoError(t, err)

	loaded, err := unmarshalSearchArtifact(data)
	require.NoError(t, err)
	require.Len(t, loaded.Inventory.Roots, 1)
	require.Nil(t, loaded.Inventory.Roots[0].FS)
	require.Equal(t, result.Inventory.Roots[0].Files[0].Path, loaded.Inventory.Roots[0].Files[0].Path)
	require.Equal(t, result.MovieSearch[0].ProviderID, loaded.MovieSearch[0].ProviderID)
}

func TestFetchArtifactRoundTripRestoresMetadata(t *testing.T) {
	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{
			Root: "/media/movies",
			Files: []InventoryFile{{
				Root:    "/media/movies",
				Path:    "/media/movies/Dune (2021)/Dune.mkv",
				RelPath: "Dune (2021)/Dune.mkv",
				Name:    "Dune.mkv",
				Class:   ClassPrimaryMedia,
				Size:    123,
				MTime:   time.Unix(100, 0).UTC(),
			}},
		}}},
		MovieSearch: []MovieSearchMatch{{
			Key:        "title_year:dune|2021",
			Accepted:   true,
			ProviderID: "heya:movie:tmdb:438631",
			Title:      "Dune",
			Year:       "2021",
		}},
		MovieMetadata: []MovieFetchPreview{{
			Key:        "title_year:dune|2021",
			ProviderID: "heya:movie:tmdb:438631",
			Title:      "Dune",
			Year:       "2021",
		}},
	}

	data, err := marshalFetchArtifact(Options{ScopePaths: []string{"Dune (2021)"}}, result)
	require.NoError(t, err)

	loaded, err := unmarshalFetchArtifact(data)
	require.NoError(t, err)
	require.Nil(t, loaded.Inventory.Roots[0].FS)
	require.Equal(t, result.MovieMetadata[0].ProviderID, loaded.MovieMetadata[0].ProviderID)
	require.True(t, fetchMetadataCoversAcceptedSearch(loaded, sqlc.Library{MediaType: sqlc.MediaTypeMovie}))
}

func TestResumeSearchArtifactOverlaysManualDecision(t *testing.T) {
	result := Result{
		MovieSearch: []MovieSearchMatch{{
			Key:        "title_year:dune|2021",
			Accepted:   false,
			Reason:     "ambiguous_or_low_confidence",
			ProviderID: "",
			Query:      MovieSearchQuery{Title: "Dune", Year: "2021"},
			Candidates: []MovieSearchCandidate{{
				ProviderID: "heya:movie:tmdb:438631",
				Provider:   "heya",
				Title:      "Dune",
				Year:       "2021",
				Confidence: 0.84,
			}},
		}},
	}
	decisions := SearchDecisions{
		"title_year:dune|2021": {
			Status:     "accepted",
			ProviderID: "heya:movie:tmdb:438631",
			Provider:   "heya",
			Title:      "Dune",
			Year:       "2021",
		},
	}

	run := NewLibraryRun(sqlc.Library{MediaType: sqlc.MediaTypeMovie}, Options{}, io.Discard)
	run.result = result
	run.analyzed = true
	run.searchDecisions = decisions
	run.searchLoaded = true
	applySearchDecisionsToResult(&run.result, run.lib, decisions, run.sink)

	require.True(t, run.result.MovieSearch[0].Accepted)
	require.Equal(t, "accepted", run.result.MovieSearch[0].ManualDecision)
	require.Equal(t, "heya:movie:tmdb:438631", run.result.MovieSearch[0].ProviderID)
	require.Empty(t, run.result.MovieSearch[0].Reason)
}

func TestFetchArtifactCoverageDetectsChangedDecision(t *testing.T) {
	result := Result{
		MovieSearch: []MovieSearchMatch{{
			Key:        "title_year:dune|2021",
			Accepted:   true,
			ProviderID: "heya:movie:tmdb:438631",
			Title:      "Dune",
			Year:       "2021",
			Candidates: []MovieSearchCandidate{{
				ProviderID: "heya:movie:tmdb:438631",
				Provider:   "heya",
				Title:      "Dune",
				Year:       "2021",
			}, {
				ProviderID: "heya:movie:tmdb:999",
				Provider:   "heya",
				Title:      "Dune: Wrong",
				Year:       "2021",
			}},
		}},
		MovieMetadata: []MovieFetchPreview{{
			Key:        "title_year:dune|2021",
			ProviderID: "heya:movie:tmdb:438631",
			Title:      "Dune",
			Year:       "2021",
		}},
	}
	decisions := SearchDecisions{
		"title_year:dune|2021": {
			Status:     "accepted",
			ProviderID: "heya:movie:tmdb:999",
			Provider:   "heya",
			Title:      "Dune: Wrong",
			Year:       "2021",
		},
	}

	applySearchDecisionsToResult(&result, sqlc.Library{MediaType: sqlc.MediaTypeMovie}, decisions, NewEventSink(Event{}))

	require.False(t, fetchMetadataCoversAcceptedSearch(result, sqlc.Library{MediaType: sqlc.MediaTypeMovie}))
}

func TestResumeSearchArtifactMissingDBIsNoop(t *testing.T) {
	run := NewLibraryRun(sqlc.Library{MediaType: sqlc.MediaTypeMovie}, Options{}, io.Discard)
	ok, err := run.ResumeSearchArtifact(context.Background(), 123)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestResumeFetchArtifactMissingDBIsNoop(t *testing.T) {
	run := NewLibraryRun(sqlc.Library{MediaType: sqlc.MediaTypeMovie}, Options{}, io.Discard)
	ok, err := run.ResumeFetchArtifact(context.Background(), 123)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestLoadedSearchArtifactCanFetchWithoutSearchProvider(t *testing.T) {
	fetcher := &fakeMovieDetailProvider{details: map[string]*metadata.MediaDetail{
		"heya:movie:tmdb:438631": {
			Title:          "Dune",
			Year:           "2021",
			Description:    "Spice must flow.",
			ExternalIDs:    map[string]string{"tmdb": "438631"},
			RuntimeMinutes: 155,
		},
	}}
	run := NewLibraryRun(sqlc.Library{MediaType: sqlc.MediaTypeMovie}, Options{
		MovieFetcher: fetcher,
	}, io.Discard)
	run.result = Result{
		MovieSearch: []MovieSearchMatch{{
			Key:        "title_year:dune|2021",
			Accepted:   true,
			ProviderID: "heya:movie:tmdb:438631",
			Title:      "Dune",
			Year:       "2021",
		}},
	}
	run.analyzed = true

	require.NoError(t, run.Run(context.Background(), PhaseFetch))
	require.Equal(t, []string{"heya:movie:tmdb:438631"}, fetcher.calls)
	require.Len(t, run.result.MovieMetadata, 1)
	require.Equal(t, "Dune", run.result.MovieMetadata[0].Title)
}
