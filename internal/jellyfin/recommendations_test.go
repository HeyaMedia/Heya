package jellyfin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSuggestionSection pins the /Items/Suggestions media-type routing: the
// BaseItemKind `type` filter is authoritative, mediaType is a no-type fallback,
// and kinds Heya can't suggest for yield ok=false (empty page) rather than
// mis-typed movie results.
func TestSuggestionSection(t *testing.T) {
	cases := []struct {
		query   string
		section string
		ok      bool
	}{
		// Unfiltered → movies.
		{"", "movie", true},
		{"limit=5", "movie", true},

		// Specific BaseItemKind is authoritative.
		{"type=Movie", "movie", true},
		{"type=Series", "tv", true},
		{"type=Episode", "tv", true},
		{"type=Movie,Series", "movie", true}, // movie wins a mixed request
		{"type=series", "tv", true},          // value case-insensitive

		// Kinds Heya can't suggest → empty, even alongside mediaType=Video
		// (music videos / trailers are video-mediaType too).
		{"type=MusicVideo", "", false},
		{"type=Trailer", "", false},
		{"type=BoxSet", "", false},
		{"type=MusicVideo&mediaType=Video", "", false},
		{"type=Trailer&mediaType=Video", "", false},

		// type still wins when paired with mediaType=Video.
		{"type=Movie&mediaType=Video", "movie", true},
		{"type=Series&mediaType=Video", "tv", true},

		// No type → coarse mediaType decides, else default.
		{"mediaType=Video", "movie", true},
		{"mediaType=Audio", "", false},
		{"mediaType=Book", "", false},
		{"mediaType=Photo", "", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/Items/Suggestions?"+c.query, nil)
		section, ok := suggestionSection(r)
		if section != c.section || ok != c.ok {
			t.Errorf("suggestionSection(%q) = (%q, %v), want (%q, %v)", c.query, section, ok, c.section, c.ok)
		}
	}
}
