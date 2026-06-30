package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCollaborationCollapsed locks the precision gate that stops enrichment
// from fusing a collaboration folder ("A & B") onto a single member, while
// still letting fixed-name duos that map to themselves through.
func TestCollaborationCollapsed(t *testing.T) {
	tests := []struct {
		name    string
		local   string
		matched string
		want    bool
	}{
		// The bug: a duo search returns a lone member → reject.
		{"duo collapses to member", "Charly Lownoise & Mental Theo", "Charly Lownoise", true},
		{"feat collapses to lead", "Eminem feat. Rihanna", "Eminem", true},
		{"ft collapses to lead", "Calvin Harris ft. Dua Lipa", "Calvin Harris", true},
		{"vs collapses to one side", "Darude vs. Sander", "Darude", true},

		// Fixed-name duos that map to themselves keep markers on both sides → allow.
		{"self-match keeps ampersand", "Chase & Status", "Chase & Status", false},
		{"self-match keeps ampersand 2", "Simon & Garfunkel", "Simon & Garfunkel", false},

		// Single artists are never gated, regardless of the match.
		{"single artist exact", "Daft Punk", "Daft Punk", false},
		{"single artist drift", "Bjork", "Björk", false},

		// Names that merely contain separator-like substrings must not trip it.
		{"ampersand without spaces is not a separator", "AT&T", "AT&T", false},
		{"plus without spaces is not a separator", "C+C Music Factory", "C+C Music Factory", false},
		{"comma is not a collaboration separator", "Tyler, the Creator", "Tyler, the Creator", false},
		{"the word and is not a separator", "Hall and Oates", "Hall and Oates", false},
		{"ft inside a word is not a separator", "Daft Punk", "Daft", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, collaborationCollapsed(tt.local, tt.matched))
		})
	}
}

func TestIsCollaborationName(t *testing.T) {
	collab := []string{
		"Charly Lownoise & Mental Theo",
		"Above & Beyond",
		"Eminem feat. Rihanna",
		"Jay-Z ft. Alicia Keys",
		"A featuring B",
		"X vs Y",
		"X versus Y",
		"Brooks + Bangs",
	}
	for _, n := range collab {
		assert.True(t, isCollaborationName(n), "expected %q to read as a collaboration", n)
	}

	solo := []string{
		"Charly Lownoise",
		"Daft Punk",
		"AT&T",
		"C+C Music Factory",
		"Tyler, the Creator",
		"Hall and Oates",
		"Left Eye",
		"Jay-Z",
	}
	for _, n := range solo {
		assert.False(t, isCollaborationName(n), "expected %q to read as a single artist", n)
	}
}
