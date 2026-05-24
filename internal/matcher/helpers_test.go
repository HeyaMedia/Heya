package matcher

import (
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
	"github.com/stretchr/testify/assert"
)

func TestSubstringTitleMatch(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"exact match not a substring case", "Dune", "Dune", false},
		{"single-word substring rejected", "Dune", "Dune Part Two", false},
		{"two-word substring accepted", "Demon Slayer", "Demon Slayer: Kimetsu no Yaiba", true},
		{"two-word substring (reverse direction)", "Demon Slayer: Kimetsu no Yaiba", "Demon Slayer", true},
		{"longer substring", "Kimetsu no Yaiba", "Demon Slayer: Kimetsu no Yaiba", true},
		{"two-word with article-prefix normalization", "Star Wars", "Star Wars: A New Hope", true},
		{"non-overlapping romaji vs English", "Shingeki no Kyojin", "Attack on Titan", false},
		{"empty inputs", "", "Anything", false},
		{"both empty", "", "", false},
		{"shared word but no substring", "Hero Academia", "My Hero Academia", true},
		{"colon-suffix case-insensitive", "demon slayer", "Demon Slayer: Kimetsu no Yaiba", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, substringTitleMatch(tt.a, tt.b))
		})
	}
}

func TestScoreBestTitle(t *testing.T) {
	t.Run("no alt titles falls back to primary", func(t *testing.T) {
		r := metadata.SearchResult{Title: "Dune", Year: "2021"}
		got := scoreBestTitle("Dune", r, "2021")
		assert.InDelta(t, 0.95, got, 0.01)
	})

	t.Run("alt-titles best wins when primary is far", func(t *testing.T) {
		r := metadata.SearchResult{
			Title:     "Attack on Titan",
			Year:      "2013",
			AltTitles: []string{"進撃の巨人", "Shingeki no Kyojin", "AoT"},
		}
		// Query in romaji should score against the alt-title, not the primary.
		got := scoreBestTitle("Shingeki no Kyojin", r, "2013")
		assert.InDelta(t, 0.95, got, 0.01, "should match Shingeki no Kyojin in alts at near-1.0 sim plus year boost")
	})

	t.Run("empty alt-titles entries are skipped", func(t *testing.T) {
		r := metadata.SearchResult{
			Title:     "Dune",
			Year:      "2021",
			AltTitles: []string{"", "", ""},
		}
		got := scoreBestTitle("Dune", r, "2021")
		assert.InDelta(t, 0.95, got, 0.01)
	})

	t.Run("year boost transfers across alt-title comparison", func(t *testing.T) {
		// Query matches alt-title perfectly; year matches the result's
		// year. Both bonuses should compose.
		r := metadata.SearchResult{
			Title:     "My Hero Academia",
			Year:      "2016",
			AltTitles: []string{"Boku no Hero Academia"},
		}
		got := scoreBestTitle("Boku no Hero Academia", r, "2016")
		assert.InDelta(t, 0.95, got, 0.01)
	})

	t.Run("alt-title with no match keeps primary's score", func(t *testing.T) {
		r := metadata.SearchResult{
			Title:     "Dune",
			Year:      "2021",
			AltTitles: []string{"Some Unrelated Thing"},
		}
		got := scoreBestTitle("Dune", r, "2021")
		assert.InDelta(t, 0.95, got, 0.01)
	})
}

func TestAutoMatchThresholdFor(t *testing.T) {
	t.Run("non-enriched returns base", func(t *testing.T) {
		got := autoMatchThresholdFor(metadata.SearchResult{Enriched: false}, 0.85)
		assert.Equal(t, 0.85, got)
	})

	t.Run("enriched lowers by 0.10", func(t *testing.T) {
		got := autoMatchThresholdFor(metadata.SearchResult{Enriched: true}, 0.85)
		assert.InDelta(t, 0.75, got, 0.001)
	})

	t.Run("enriched honors 0.6 floor", func(t *testing.T) {
		got := autoMatchThresholdFor(metadata.SearchResult{Enriched: true}, 0.65)
		// Would be 0.55 without the floor — must clamp to 0.6.
		assert.Equal(t, 0.6, got)
	})

	t.Run("non-enriched below floor is unchanged", func(t *testing.T) {
		got := autoMatchThresholdFor(metadata.SearchResult{Enriched: false}, 0.5)
		assert.Equal(t, 0.5, got)
	})
}
