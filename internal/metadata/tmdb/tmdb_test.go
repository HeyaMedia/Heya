package tmdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestProvider(t *testing.T, handler http.Handler) *Provider {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	p := NewProvider("test-token")
	p.BaseURL = ts.URL
	return p
}

func TestSearchMovies(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search/movie", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		resp := searchMovieResponse{
			Results: []movieResult{
				{ID: 693134, Title: "Dune: Part Two", ReleaseDate: "2024-02-27", Overview: "Paul Atreides unites...", PosterPath: "/poster.jpg"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	p := newTestProvider(t, mux)
	results, err := p.Search(context.Background(), metadata.KindMovie, metadata.SearchQuery{Title: "Dune", Year: "2024"})
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "Dune: Part Two", results[0].Title)
	assert.Equal(t, "2024", results[0].Year)
	assert.Equal(t, "movie:693134", results[0].ProviderID)
	assert.Equal(t, "tmdb", results[0].ProviderName)
}

func TestSearchTV(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search/tv", func(w http.ResponseWriter, r *http.Request) {
		resp := searchTVResponse{
			Results: []tvResult{
				{ID: 114410, Name: "Chainsaw Man", FirstAirDate: "2022-10-12"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	p := newTestProvider(t, mux)
	results, err := p.Search(context.Background(), metadata.KindTV, metadata.SearchQuery{Title: "Chainsaw Man"})
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "Chainsaw Man", results[0].Title)
	assert.Equal(t, "2022", results[0].Year)
	assert.Equal(t, "tv:114410", results[0].ProviderID)
}

func TestGetMovieDetail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/movie/693134", func(w http.ResponseWriter, r *http.Request) {
		d := movieDetail{
			ID:           693134,
			Title:        "Dune: Part Two",
			Overview:     "Paul Atreides unites with the Fremen.",
			ReleaseDate:  "2024-02-27",
			Runtime:      166,
			Tagline:      "Long live the fighters.",
			Budget:       190000000,
			Revenue:      714444358,
			VoteAverage:  8.2,
			VoteCount:    5000,
			PosterPath:   "/poster.jpg",
			BackdropPath: "/backdrop.jpg",
			Genres:       []genreEntry{{Name: "Science Fiction"}, {Name: "Adventure"}},
			Credits: creditsResponse{
				Cast: []castEntry{{Name: "Timothée Chalamet", Character: "Paul Atreides", Order: 0}},
				Crew: []crewEntry{{Name: "Denis Villeneuve", Job: "Director", Department: "Directing"}},
			},
			ExternalIDs: externalIDsResult{IMDBID: "tt15239678"},
		}
		json.NewEncoder(w).Encode(d)
	})

	p := newTestProvider(t, mux)
	detail, err := p.GetDetail(context.Background(), "movie:693134", nil)
	require.NoError(t, err)

	assert.Equal(t, "Dune: Part Two", detail.Title)
	assert.Equal(t, "2024", detail.Year)
	assert.Equal(t, 166, detail.RuntimeMinutes)
	assert.Equal(t, "Long live the fighters.", detail.Tagline)
	assert.Equal(t, int64(190000000), detail.Budget)
	assert.InDelta(t, 8.2, detail.Rating, 0.01)
	assert.Equal(t, []string{"Science Fiction", "Adventure"}, detail.Genres)
	require.Len(t, detail.Cast, 1)
	assert.Equal(t, "Timothée Chalamet", detail.Cast[0].Name)
	require.Len(t, detail.Crew, 1)
	assert.Equal(t, "Denis Villeneuve", detail.Crew[0].Name)
}

func TestFindByIMDB(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/find/tt15239678", func(w http.ResponseWriter, r *http.Request) {
		resp := findResponse{
			MovieResults: []movieResult{{ID: 693134}},
		}
		json.NewEncoder(w).Encode(resp)
	})

	p := newTestProvider(t, mux)
	kind, tmdbID, err := p.FindByIMDB(context.Background(), "tt15239678")
	require.NoError(t, err)
	assert.Equal(t, "movie", kind)
	assert.Equal(t, "693134", tmdbID)
}

func TestFindByIMDBTV(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/find/tt0903747", func(w http.ResponseWriter, r *http.Request) {
		resp := findResponse{
			TVResults: []tvResult{{ID: 1396}},
		}
		json.NewEncoder(w).Encode(resp)
	})

	p := newTestProvider(t, mux)
	kind, tmdbID, err := p.FindByIMDB(context.Background(), "tt0903747")
	require.NoError(t, err)
	assert.Equal(t, "tv", kind)
	assert.Equal(t, "1396", tmdbID)
}

func TestSupports(t *testing.T) {
	p := NewProvider("tok")
	assert.True(t, p.Supports(metadata.KindMovie))
	assert.True(t, p.Supports(metadata.KindTV))
	assert.False(t, p.Supports(metadata.KindMusic))
	assert.False(t, p.Supports(metadata.KindBook))
}

func TestProviderName(t *testing.T) {
	p := NewProvider("tok")
	assert.Equal(t, "tmdb", p.Name())
}

func TestGetDetailInvalidProviderID(t *testing.T) {
	p := NewProvider("tok")
	_, err := p.GetDetail(context.Background(), "invalid", nil)
	assert.Error(t, err)
}
