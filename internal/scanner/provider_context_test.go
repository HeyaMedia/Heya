package scanner

import (
	"context"
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/stretchr/testify/require"
)

type terminatingSearchProvider struct {
	err error
}

func (p terminatingSearchProvider) Search(context.Context, metadata.MediaKind, metadata.SearchQuery) ([]metadata.SearchResult, error) {
	return nil, p.err
}

type terminatingDetailProvider struct {
	err error
}

func (p terminatingDetailProvider) GetDetail(context.Context, string, *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	return nil, p.err
}

type terminatingMusicReleaseProvider struct {
	err error
}

func (p terminatingMusicReleaseProvider) GetDetail(context.Context, string, *metadata.FetchOptions) (*metadata.MediaDetail, error) {
	return &metadata.MediaDetail{CanonicalID: "artist", CanonicalKind: "artist", ArtistName: "Example"}, nil
}

func (p terminatingMusicReleaseProvider) ResolveReleaseGroup(context.Context, metadata.SearchQuery) (*metadata.MediaDetail, error) {
	return nil, p.err
}

func TestSearchDomainsPropagateProviderContextTermination(t *testing.T) {
	for _, operationErr := range []error{context.Canceled, context.DeadlineExceeded} {
		operationErr := operationErr
		name := "canceled"
		if operationErr == context.DeadlineExceeded {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			provider := terminatingSearchProvider{err: operationErr}
			t.Run("movie", func(t *testing.T) {
				results, err := SearchMovieMatches(context.Background(), []MovieMatch{{Key: "movie", Title: "Movie"}}, provider, &captureEmitter{}, 0)
				require.ErrorIs(t, err, operationErr)
				require.Empty(t, results, "context termination must not become a durable search_error row")
			})
			t.Run("book", func(t *testing.T) {
				results, err := SearchBookPlans(context.Background(), []BookPlan{{Key: "book", Title: "Book"}}, provider, &captureEmitter{}, 0)
				require.ErrorIs(t, err, operationErr)
				require.Empty(t, results, "context termination must not become a durable search_error row")
			})
			t.Run("tv", func(t *testing.T) {
				results, err := SearchTVMatches(context.Background(), []TVMatch{{Key: "tv", Title: "Show"}}, provider, &captureEmitter{}, 0)
				require.ErrorIs(t, err, operationErr)
				require.Empty(t, results, "context termination must not become a durable search_error row")
			})
		})
	}
}

func TestFetchDomainsPropagateProviderContextTermination(t *testing.T) {
	for _, operationErr := range []error{context.Canceled, context.DeadlineExceeded} {
		operationErr := operationErr
		name := "canceled"
		if operationErr == context.DeadlineExceeded {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			provider := terminatingDetailProvider{err: operationErr}
			t.Run("movie", func(t *testing.T) {
				previews, err := FetchMovieMetadataPreviews(context.Background(), []MovieSearchMatch{{Key: "movie", Accepted: true, ProviderID: "movie"}}, provider, &captureEmitter{})
				require.ErrorIs(t, err, operationErr)
				require.Empty(t, previews, "context termination must not become a durable fetch error preview")
			})
			t.Run("book", func(t *testing.T) {
				previews, err := FetchBookMetadataPreviews(context.Background(), []BookSearchMatch{{Key: "book", Accepted: true, ProviderID: "book"}}, provider, &captureEmitter{})
				require.ErrorIs(t, err, operationErr)
				require.Empty(t, previews, "context termination must not become a durable fetch error preview")
			})
			t.Run("tv", func(t *testing.T) {
				previews, err := FetchTVMetadataPreviews(context.Background(), []TVSearchMatch{{Key: "tv", Accepted: true, ProviderID: "tv"}}, []TVMatch{{Key: "tv", Title: "Show"}}, provider, &captureEmitter{})
				require.ErrorIs(t, err, operationErr)
				require.Empty(t, previews, "context termination must not become a durable fetch error preview")
			})
			t.Run("music", func(t *testing.T) {
				previews, err := FetchMusicMetadataPreviews(context.Background(), []MusicSearchMatch{{Key: "music", Accepted: true, ProviderID: "music", Artist: "Example"}}, []MusicArtistPlan{{Key: "music", Artist: "Example"}}, provider, &captureEmitter{})
				require.ErrorIs(t, err, operationErr)
				require.Len(t, previews, 1)
				require.Empty(t, previews[0].Error, "context termination must not become a durable fetch error preview")
			})
		})
	}
}

func TestMusicReleaseGroupResolutionPropagatesContextTermination(t *testing.T) {
	for _, operationErr := range []error{context.Canceled, context.DeadlineExceeded} {
		previews, err := FetchMusicMetadataPreviews(
			context.Background(),
			[]MusicSearchMatch{{Key: "music", Accepted: true, ProviderID: "music", Artist: "Example"}},
			[]MusicArtistPlan{{Key: "music", Artist: "Example", Albums: []MusicAlbumPlan{{Key: "album", Album: "Album", Artist: "Example"}}}},
			terminatingMusicReleaseProvider{err: operationErr},
			&captureEmitter{},
		)
		require.ErrorIs(t, err, operationErr)
		require.Len(t, previews, 1)
		require.Empty(t, previews[0].Error)
	}
}
