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

// TestArtistNameAcceptable pins the name-similarity gate that stops enrichment
// from accepting a wildly-wrong match (the Avicii→Alicia Keys class of fusion).
// Fixtures are real names pulled from the knas dataset.
func TestArtistNameAcceptable(t *testing.T) {
	accept := [][2]string{
		{"Avicii", "Avicii"},                          // exact
		{"Charli xcx", "Charli XCX"},                  // casing
		{"Charli XCX", "Charli xcx (English singer)"}, // casing + parenthetical
		{"HANABIE", "花冷え。"},                           // romaji ↔ kana transliteration
		{"Beyonce", "Beyoncé"},                        // accent fold
		{"Above & Beyond", "Above & Beyond"},          // fixed-name duo, self-match
	}
	for _, c := range accept {
		assert.True(t, artistNameAcceptable(c[0], c[1]), "expected %q ~ %q to be accepted", c[0], c[1])
	}

	reject := [][2]string{
		{"Avicii", "Alicia Keys"},                            // the headline fusion
		{"Babymetal", "Bring Me the Horizon"},                // totally different
		{"Bonnie McKee", "Britney Spears"},                   //
		{"April Kry", "Carrie Underwood"},                    //
		{"Alien Ant Farm", "ATLiens"},                        //
		{"Braids", "BROODS"},                                 // single-token near-miss
		{"ARTBAT", "Artemas"},                                //
		{"Anna Nalick", "Anya Nami"},                         //
		{"AJ Mitchell", "Aly and AJ"},                        //
		{"Charly Lownoise & Mental Theo", "Charly Lownoise"}, // collaboration → member
	}
	for _, c := range reject {
		assert.False(t, artistNameAcceptable(c[0], c[1]), "expected %q ~ %q to be rejected", c[0], c[1])
	}
}

func TestDisambiguationConflict(t *testing.T) {
	conflict := [][2]string{
		{"techno", "Japanese vocalist"},            // the two "Ado"s
		{"hardstyle DJ", "drum and bass producer"}, // no shared significant token
	}
	for _, c := range conflict {
		assert.True(t, disambiguationConflict(c[0], c[1]), "expected %q vs %q to conflict", c[0], c[1])
	}

	noConflict := [][2]string{
		{"Japanese vocalist", "Japanese singer"}, // share "japanese" → paraphrase
		{"", "Japanese vocalist"},                // one empty → no signal
		{"techno", ""},                           //
		{"Electronic duo from Portland", "Electronic artist born in Hawaii"}, // share "electronic"
		{"band", "act"}, // both sub-4-char → nothing significant
	}
	for _, c := range noConflict {
		assert.False(t, disambiguationConflict(c[0], c[1]), "expected %q vs %q to NOT conflict", c[0], c[1])
	}
}

func TestIsSyntheticMBID(t *testing.T) {
	assert.True(t, isSyntheticMBID("dddddddd-dddd-dddd-dddd-ddd513923292"))
	assert.True(t, isSyntheticMBID("DDDDDDDD-DDDD-DDDD-DDDD-DDD513923292"))  // case-insensitive
	assert.False(t, isSyntheticMBID("c85cfd6b-b1e9-4a50-bd55-eb725f04f7d5")) // real Alicia Keys MBID
	assert.False(t, isSyntheticMBID(""))
}
