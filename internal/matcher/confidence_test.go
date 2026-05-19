package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
