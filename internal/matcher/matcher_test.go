package matcher

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
	"github.com/karbowiak/heya/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	name     string
	kinds    []metadata.MediaKind
	results  []metadata.SearchResult
	detail   *metadata.MediaDetail
	nfoData  *metadata.MediaDetail
	nfoID    string
	searchOK bool
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Supports(kind metadata.MediaKind) bool {
	for _, k := range m.kinds {
		if k == kind {
			return true
		}
	}
	return false
}

func (m *mockProvider) Search(_ context.Context, _ metadata.MediaKind, _ metadata.SearchQuery) ([]metadata.SearchResult, error) {
	m.searchOK = true
	return m.results, nil
}

func (m *mockProvider) GetDetail(_ context.Context, _ string) (*metadata.MediaDetail, error) {
	return m.detail, nil
}

func (m *mockProvider) LookupByNFO(_ context.Context, _ metadata.MediaKind, _ metadata.NFOIDs) (*metadata.MediaDetail, string, error) {
	if m.nfoData != nil {
		return m.nfoData, m.nfoID, nil
	}
	return nil, "", assert.AnError
}

func TestBuildSearchQueryMovie(t *testing.T) {
	parsed := parser.ParsedStorageEntry{
		Release: &parser.SceneReleaseParse{
			Title: "Dune Part Two",
			Year:  "2024",
		},
	}
	q := buildSearchQuery(parsed, metadata.KindMovie)
	assert.Equal(t, "Dune Part Two", q.Title)
	assert.Equal(t, "2024", q.Year)
}

func TestBuildSearchQueryTV(t *testing.T) {
	parsed := parser.ParsedStorageEntry{
		Release: &parser.SceneReleaseParse{
			Title:   "Breaking Bad",
			Year:    "2008",
			Seasons: []int{1, 2},
		},
	}
	q := buildSearchQuery(parsed, metadata.KindTV)
	assert.Equal(t, "Breaking Bad", q.Title)
	assert.Equal(t, []int{1, 2}, q.Seasons)
}

func TestBuildSearchQueryMusic(t *testing.T) {
	parsed := parser.ParsedStorageEntry{
		Release: &parser.SceneReleaseParse{
			Title: "Radiohead - OK Computer",
			Year:  "1997",
		},
	}
	q := buildSearchQuery(parsed, metadata.KindMusic)
	assert.Equal(t, "Radiohead", q.Artist)
	assert.Equal(t, "OK Computer", q.Album)
}

func TestBuildSearchQueryBook(t *testing.T) {
	parsed := parser.ParsedStorageEntry{
		Release: &parser.SceneReleaseParse{
			Title:       "Frank Herbert - Dune",
			ReleaseHash: "9780441013593",
		},
	}
	q := buildSearchQuery(parsed, metadata.KindBook)
	assert.Equal(t, "Frank Herbert", q.Author)
	assert.Equal(t, "Dune", q.Title)
	assert.Equal(t, "9780441013593", q.ISBN)
}

func TestBuildSearchQueryEmptyRelease(t *testing.T) {
	parsed := parser.ParsedStorageEntry{}
	q := buildSearchQuery(parsed, metadata.KindMovie)
	assert.Empty(t, q.Title)
}

func TestSortByConfidence(t *testing.T) {
	results := []metadata.SearchResult{
		{Title: "low", Confidence: 0.3},
		{Title: "high", Confidence: 0.9},
		{Title: "mid", Confidence: 0.6},
	}
	sortByConfidence(results)

	assert.Equal(t, "high", results[0].Title)
	assert.Equal(t, "mid", results[1].Title)
	assert.Equal(t, "low", results[2].Title)
}

func TestParseFileResultWrapper(t *testing.T) {
	data := map[string]any{
		"parsed": parser.ParsedStorageEntry{
			InputPath: "test/path.mkv",
			Release: &parser.SceneReleaseParse{
				Title: "Test Movie",
				Year:  "2024",
			},
		},
		"nfo": map[string]any{
			"TMDBID": "12345",
			"IMDBID": "tt0000001",
		},
	}
	b, _ := json.Marshal(data)
	parsed, nfoIDs := parseFileResult(b)

	assert.Equal(t, "Test Movie", parsed.Release.Title)
	require.NotNil(t, nfoIDs)
	assert.Equal(t, "12345", nfoIDs.TMDBID)
	assert.Equal(t, "tt0000001", nfoIDs.IMDBID)
}

func TestParseFileResultRawFormat(t *testing.T) {
	data := parser.ParsedStorageEntry{
		InputPath: "movie.mkv",
		Release: &parser.SceneReleaseParse{
			Title: "Raw Movie",
		},
	}
	b, _ := json.Marshal(data)
	parsed, nfoIDs := parseFileResult(b)

	assert.Equal(t, "Raw Movie", parsed.Release.Title)
	assert.Nil(t, nfoIDs)
}

func TestMediaTypeToKind(t *testing.T) {
	assert.Equal(t, metadata.KindMovie, MediaTypeToKind(sqlc.MediaTypeMovie))
	assert.Equal(t, metadata.KindTV, MediaTypeToKind(sqlc.MediaTypeTv))
	assert.Equal(t, metadata.KindMusic, MediaTypeToKind(sqlc.MediaTypeMusic))
	assert.Equal(t, metadata.KindBook, MediaTypeToKind(sqlc.MediaTypeBook))
}

func TestMediaTypeFromProvider(t *testing.T) {
	assert.Equal(t, "movie", mediaTypeFromProvider("tmdb"))
	assert.Equal(t, "music", mediaTypeFromProvider("musicbrainz"))
	assert.Equal(t, "book", mediaTypeFromProvider("openlibrary"))
	assert.Equal(t, "movie", mediaTypeFromProvider("unknown"))
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 5))
	assert.Equal(t, "ab", truncate("abcde", 2))
	assert.Equal(t, "", truncate("", 5))
}
