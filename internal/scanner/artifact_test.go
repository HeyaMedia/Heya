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
		MovieMetadata: []MovieFetchPreview{{
			Key:        "title_year:dune|2021",
			ProviderID: "heya:movie:tmdb:438631",
			Title:      "Dune",
			Year:       "2021",
			Detail: &metadata.MediaDetail{
				Title:       "Dune",
				Year:        "2021",
				Description: "Spice must flow.",
				ExternalIDs: map[string]string{"tmdb": "438631"},
			},
		}},
	}

	data, err := marshalFetchArtifact(Options{ScopePaths: []string{"Dune (2021)"}}, result)
	require.NoError(t, err)

	loaded, err := unmarshalFetchArtifact(data)
	require.NoError(t, err)
	require.Nil(t, loaded.Inventory.Roots[0].FS)
	require.Equal(t, result.MovieMetadata[0].ProviderID, loaded.MovieMetadata[0].ProviderID)
	require.NotNil(t, loaded.MovieMetadata[0].Detail)
	require.Equal(t, "Spice must flow.", loaded.MovieMetadata[0].Detail.Description)
	require.Equal(t, "438631", loaded.MovieMetadata[0].Detail.ExternalIDs["tmdb"])
	require.True(t, fetchMetadataCoversAcceptedSearch(loaded, sqlc.Library{MediaType: sqlc.MediaTypeMovie}))
}

func TestScopedArtifactDropsOutOfScopeMovieData(t *testing.T) {
	result := Result{
		Inventory: Inventory{Roots: []InventoryRoot{{
			Root: "/media/movies",
			Files: []InventoryFile{{
				Root:    "/media/movies",
				Path:    "/media/movies/Dune (2021)/Dune.mkv",
				RelPath: "Dune (2021)/Dune.mkv",
				Name:    "Dune.mkv",
				Class:   ClassPrimaryMedia,
			}, {
				Root:    "/media/movies",
				Path:    "/media/movies/The Matrix (1999)/The Matrix.mkv",
				RelPath: "The Matrix (1999)/The Matrix.mkv",
				Name:    "The Matrix.mkv",
				Class:   ClassPrimaryMedia,
			}},
		}}},
		MovieMatches: []MovieMatch{{
			Key:   "title_year:dune|2021",
			Title: "Dune",
			Year:  "2021",
			Files: []string{"Dune (2021)/Dune.mkv"},
		}, {
			Key:   "title_year:matrix|1999",
			Title: "The Matrix",
			Year:  "1999",
			Files: []string{"The Matrix (1999)/The Matrix.mkv"},
		}},
		MovieSearch: []MovieSearchMatch{{
			Key:        "title_year:dune|2021",
			Accepted:   true,
			ProviderID: "heya:movie:tmdb:438631",
			Title:      "Dune",
			Year:       "2021",
		}, {
			Key:        "title_year:matrix|1999",
			Accepted:   true,
			ProviderID: "heya:movie:tmdb:603",
			Title:      "The Matrix",
			Year:       "1999",
		}},
		MovieMetadata: []MovieFetchPreview{{
			Key:        "title_year:dune|2021",
			ProviderID: "heya:movie:tmdb:438631",
			Title:      "Dune",
			Year:       "2021",
			Detail:     &metadata.MediaDetail{Title: "Dune", Year: "2021"},
		}, {
			Key:        "title_year:matrix|1999",
			ProviderID: "heya:movie:tmdb:603",
			Title:      "The Matrix",
			Year:       "1999",
			Detail:     &metadata.MediaDetail{Title: "The Matrix", Year: "1999"},
		}},
		MovieMaterialize: []MovieMaterializePreview{{
			Key:         "title_year:dune|2021",
			Title:       "Dune",
			Year:        "2021",
			ProviderID:  "heya:movie:tmdb:438631",
			FileActions: []MovieMaterializeFileAction{{RelPath: "Dune (2021)/Dune.mkv", Action: "create_library_file_and_attach"}},
		}, {
			Key:         "title_year:matrix|1999",
			Title:       "The Matrix",
			Year:        "1999",
			ProviderID:  "heya:movie:tmdb:603",
			FileActions: []MovieMaterializeFileAction{{RelPath: "The Matrix (1999)/The Matrix.mkv", Action: "create_library_file_and_attach"}},
		}},
	}

	data, err := marshalFetchArtifact(Options{ScopePaths: []string{"/media/movies/Dune (2021)"}}, result)
	require.NoError(t, err)

	loaded, err := unmarshalFetchArtifact(data)
	require.NoError(t, err)
	require.Len(t, loaded.Inventory.Roots, 1)
	require.Len(t, loaded.Inventory.Roots[0].Files, 1)
	require.Equal(t, "Dune (2021)/Dune.mkv", loaded.Inventory.Roots[0].Files[0].RelPath)
	require.Len(t, loaded.MovieMatches, 1)
	require.Equal(t, "title_year:dune|2021", loaded.MovieMatches[0].Key)
	require.Len(t, loaded.MovieSearch, 1)
	require.Equal(t, "title_year:dune|2021", loaded.MovieSearch[0].Key)
	require.Len(t, loaded.MovieMetadata, 1)
	require.Equal(t, "title_year:dune|2021", loaded.MovieMetadata[0].Key)
	require.Len(t, loaded.MovieMaterialize, 1)
	require.Equal(t, "title_year:dune|2021", loaded.MovieMaterialize[0].Key)
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

func TestFetchArtifactCoverageRequiresDetailForApply(t *testing.T) {
	result := Result{
		MovieSearch: []MovieSearchMatch{{
			Key:        "tmdb:584",
			Accepted:   true,
			ProviderID: "heya:movie:tmdb:584",
			Title:      "2 Fast 2 Furious",
			Year:       "2003",
		}},
		MovieMetadata: []MovieFetchPreview{{
			Key:        "tmdb:584",
			ProviderID: "heya:movie:tmdb:584",
			Title:      "2 Fast 2 Furious",
			Year:       "2003",
		}},
	}

	require.False(t, fetchMetadataCoversAcceptedSearch(result, sqlc.Library{MediaType: sqlc.MediaTypeMovie}))

	result.MovieMetadata[0].Detail = &metadata.MediaDetail{Title: "2 Fast 2 Furious", Year: "2003"}
	require.True(t, fetchMetadataCoversAcceptedSearch(result, sqlc.Library{MediaType: sqlc.MediaTypeMovie}))

	result.MovieMetadata[0].Detail = nil
	result.MovieMetadata[0].Error = "upstream failed"
	require.True(t, fetchMetadataCoversAcceptedSearch(result, sqlc.Library{MediaType: sqlc.MediaTypeMovie}))
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
