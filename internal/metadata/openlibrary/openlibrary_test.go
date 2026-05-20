package openlibrary

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
	p := NewProvider()
	p.BaseURL = ts.URL
	return p
}

func TestSearchByTitle(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/search.json", func(w http.ResponseWriter, r *http.Request) {
		resp := searchResponse{
			NumFound: 1,
			Docs: []searchDoc{
				{
					Key:              "/works/OL45883W",
					Title:            "Dune",
					AuthorName:       []string{"Frank Herbert"},
					FirstPublishYear: 1965,
					CoverI:           8226862,
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	p := newTestProvider(t, mux)
	results, err := p.Search(context.Background(), metadata.KindBook, metadata.SearchQuery{
		Title:  "Dune",
		Author: "Frank Herbert",
	})
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "Frank Herbert - Dune", results[0].Title)
	assert.Equal(t, "1965", results[0].Year)
	assert.Equal(t, "openlibrary", results[0].ProviderName)
	assert.Contains(t, results[0].PosterURL, "8226862")
}

func TestSearchByISBN(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/isbn/9780441013593.json", func(w http.ResponseWriter, r *http.Request) {
		resp := isbnResponse{
			Title:  "Dune",
			Works:  []struct{ Key string `json:"key"` }{{Key: "/works/OL45883W"}},
			Covers: []int{8226862},
		}
		json.NewEncoder(w).Encode(resp)
	})

	p := newTestProvider(t, mux)
	results, err := p.Search(context.Background(), metadata.KindBook, metadata.SearchQuery{ISBN: "9780441013593"})
	require.NoError(t, err)
	require.Len(t, results, 1)

	assert.Equal(t, "Dune", results[0].Title)
	assert.InDelta(t, 0.99, results[0].Confidence, 0.01)
}

func TestGetDetail(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/works/OL45883W.json", func(w http.ResponseWriter, r *http.Request) {
		work := workDetail{
			Key:         "/works/OL45883W",
			Title:       "Dune",
			Description: "A desert planet epic.",
			Covers:      []int{8226862},
			Subjects:    []string{"Science fiction", "Space"},
			Authors:     []authorRef{{Author: struct{ Key string `json:"key"` }{Key: "/authors/OL34221A"}}},
		}
		json.NewEncoder(w).Encode(work)
	})
	mux.HandleFunc("/authors/OL34221A.json", func(w http.ResponseWriter, r *http.Request) {
		author := authorDetail{
			Name:      "Frank Herbert",
			BirthDate: "October 8, 1920",
			DeathDate: "February 11, 1986",
		}
		json.NewEncoder(w).Encode(author)
	})
	mux.HandleFunc("/works/OL45883W/editions.json", func(w http.ResponseWriter, r *http.Request) {
		resp := editionsResponse{
			Entries: []editionEntry{{
				Title:         "Dune",
				NumberOfPages: 412,
				Publishers:    []string{"Ace Books"},
				PublishDate:   "August 2, 2005",
				ISBN13:        []string{"9780441013593"},
				Languages:     []struct{ Key string `json:"key"` }{{Key: "/languages/eng"}},
			}},
		}
		json.NewEncoder(w).Encode(resp)
	})

	p := newTestProvider(t, mux)
	detail, err := p.GetDetail(context.Background(), "openlibrary:/works/OL45883W", nil)
	require.NoError(t, err)

	assert.Equal(t, "Dune", detail.Title)
	assert.Equal(t, "A desert planet epic.", detail.Description)
	assert.Equal(t, "Frank Herbert", detail.AuthorName)
	assert.Equal(t, "9780441013593", detail.ISBN)
	assert.Equal(t, 412, detail.PageCount)
	assert.Equal(t, "Ace Books", detail.Publisher)
	assert.Equal(t, "eng", detail.Language)
	assert.Contains(t, detail.Subjects, "Science fiction")
	assert.Contains(t, detail.PosterURL, "8226862")
}

func TestSupports(t *testing.T) {
	p := NewProvider()
	assert.True(t, p.Supports(metadata.KindBook))
	assert.False(t, p.Supports(metadata.KindMovie))
	assert.False(t, p.Supports(metadata.KindTV))
	assert.False(t, p.Supports(metadata.KindMusic))
}

func TestProviderName(t *testing.T) {
	p := NewProvider()
	assert.Equal(t, "openlibrary", p.Name())
}

func TestGetDetailInvalidProviderID(t *testing.T) {
	p := NewProvider()
	_, err := p.GetDetail(context.Background(), "invalid", nil)
	assert.Error(t, err)
}

func TestExtractText(t *testing.T) {
	assert.Equal(t, "hello", extractText("hello"))
	assert.Equal(t, "world", extractText(map[string]interface{}{"value": "world"}))
	assert.Equal(t, "", extractText(42))
	assert.Equal(t, "", extractText(nil))
}

func TestPickBestEdition(t *testing.T) {
	editions := []editionEntry{
		{Title: "no isbn", NumberOfPages: 100},
		{Title: "has isbn", NumberOfPages: 200, ISBN13: []string{"978123"}},
		{Title: "also isbn", NumberOfPages: 300, ISBN13: []string{"978456"}},
	}
	best := pickBestEdition(editions)
	assert.Equal(t, "has isbn", best.Title)
}
