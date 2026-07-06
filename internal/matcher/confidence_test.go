package matcher

import (
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/stretchr/testify/assert"
)

func TestHasClearGap(t *testing.T) {
	res := func(title string, conf float64) metadata.SearchResult {
		return metadata.SearchResult{Title: title, Confidence: conf}
	}

	tests := []struct {
		name    string
		query   string
		results []metadata.SearchResult
		want    bool
	}{
		{
			// The production regression: the real series is the top hit at 0.95,
			// but companion shows sharing the "House of the Dragon" prefix score
			// 0.90 — a 0.05 gap that used to reject the match and fork a new
			// series for every fresh episode. The exact-title winner must survive.
			name:  "exact-title winner survives a thin gap vs prefix-sharing companions",
			query: "House of the Dragon",
			results: []metadata.SearchResult{
				res("House of the Dragon", 0.95),
				res("Enter the House of the Dragon", 0.90),
				res("The Official Game of Thrones Podcast: House of the Dragon", 0.90),
			},
			want: true,
		},
		{
			// Two real same-title hits AND a close differently-titled companion:
			// the exact-title bypass must NOT fire (two exact matches = genuine
			// ambiguity), and the thin gap keeps it in manual review.
			name:  "two genuine same-title hits stay ambiguous",
			query: "The Thing",
			results: []metadata.SearchResult{
				res("The Thing", 0.95),
				res("The Thing", 0.93),
				res("The Thing Prequel", 0.90),
			},
			want: false,
		},
		{
			name:    "single candidate is always clear",
			query:   "Whatever",
			results: []metadata.SearchResult{res("Something Else", 0.60)},
			want:    true,
		},
		{
			name:  "wide gap to a differently-titled runner-up is clear",
			query: "Foo",
			results: []metadata.SearchResult{
				res("Foo Bar", 0.95),
				res("Baz", 0.70),
			},
			want: true,
		},
		{
			name:  "thin gap with no exact-title match stays ambiguous",
			query: "Foo",
			results: []metadata.SearchResult{
				res("Foo Bar", 0.95),
				res("Foo Baz", 0.90),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasClearGap(tt.results, tt.query))
		})
	}
}

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"The Matrix", "matrix"},
		{"A Beautiful Mind", "beautiful mind"},
		{"An Officer and a Gentleman", "officer and a gentleman"},
		{"Dune: Part Two", "dune part two"},
		{"  Spaces  Everywhere  ", "spaces everywhere"},
		{"UPPER CASE", "upper case"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, NormalizeTitle(tt.input))
		})
	}
}

func TestStringSimilarity(t *testing.T) {
	assert.Equal(t, 1.0, StringSimilarity("exact", "exact"))
	assert.Equal(t, 1.0, StringSimilarity("The Matrix", "Matrix"))
	assert.Equal(t, 0.0, StringSimilarity("", "something"))
	assert.Equal(t, 0.0, StringSimilarity("something", ""))

	sim := StringSimilarity("Dune Part Two", "Dune Part 2")
	assert.Greater(t, sim, 0.7)
}

func TestLevenshtein(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		{"kitten", "sitting", 3},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		{"a", "b", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"→"+tt.b, func(t *testing.T) {
			assert.Equal(t, tt.expected, levenshtein(tt.a, tt.b))
		})
	}
}

func TestScoreConfidence(t *testing.T) {
	exact := ScoreConfidence("Dune", "Dune", "2024", "2024")
	assert.InDelta(t, 0.95, exact, 0.01)

	yearOff := ScoreConfidence("Dune", "Dune", "2024", "2023")
	assert.Greater(t, yearOff, 0.85)
	assert.Less(t, yearOff, exact)

	noYear := ScoreConfidence("Dune", "Dune", "", "")
	assert.InDelta(t, 0.85, noYear, 0.01)

	partial := ScoreConfidence("Dune Part Two", "Dune", "2024", "2024")
	assert.Less(t, partial, exact)
	assert.Greater(t, partial, 0.0)
}

func TestScoreConfidenceCapAtOne(t *testing.T) {
	score := ScoreConfidence("Test", "Test", "2024", "2024")
	assert.LessOrEqual(t, score, 1.0)
}
