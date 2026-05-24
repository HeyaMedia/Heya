package service

import (
	"testing"

	"github.com/karbowiak/heya/internal/metadata"
)

func TestParseIdentifyURL(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		hint     metadata.MediaKind
		provider string
		id       string
		ok       bool
	}{
		// heya.media v0.3.0 URL/shortcode: heya_<kind>:<provider>:<value>
		{"heya url with tmdb id", "https://heya.media/heya_tv:tmdb:130636", metadata.KindTV, "heya", "heya:tv:tmdb:130636", true},
		{"heya url with mbid", "https://heya.media/heya_artist:mbid:8dd98bdc-aaaa-bbbb-cccc-1234567890ab", metadata.KindMusic, "heya", "heya:artist:mbid:8dd98bdc-aaaa-bbbb-cccc-1234567890ab", true},
		{"heya shortcode tv", "heya_tv:tmdb:130636", metadata.KindTV, "heya", "heya:tv:tmdb:130636", true},
		{"heya shortcode movie", "heya_movie:imdb:tt15398776", metadata.KindMovie, "heya", "heya:movie:imdb:tt15398776", true},

		// Pre-built provider ID passthrough
		{"providerID passthrough", "heya:tv:tmdb:130636", metadata.KindTV, "heya", "heya:tv:tmdb:130636", true},

		// TMDB URLs — kind comes from the URL, hint ignored
		{"tmdb tv url", "https://www.themoviedb.org/tv/130636-oshi-no-ko", metadata.KindTV, "heya", "heya:tv:tmdb:130636", true},
		{"tmdb movie url", "https://www.themoviedb.org/movie/438631-dune", metadata.KindMovie, "heya", "heya:movie:tmdb:438631", true},

		// TheTVDB URLs — segment determines kind
		{"tvdb series", "https://www.thetvdb.com/series/421069", metadata.KindTV, "heya", "heya:tv:tvdb:421069", true},
		{"tvdb movies", "https://www.thetvdb.com/movies/12345", metadata.KindMovie, "heya", "heya:movie:tvdb:12345", true},

		// IMDb URLs — ambiguous, use hint
		{"imdb url with movie hint", "https://www.imdb.com/title/tt15398776/", metadata.KindMovie, "heya", "heya:movie:imdb:tt15398776", true},
		{"imdb url with tv hint", "https://www.imdb.com/title/tt15398776/", metadata.KindTV, "heya", "heya:tv:imdb:tt15398776", true},

		// Forms no longer supported (slug-only / missing kind)
		{"slug-only shortcode rejected", "heya:oshi-no-ko-2023", metadata.KindTV, "", "", false},
		{"old heya_kind slug rejected", "heya_tv:oshi-no-ko-2023", metadata.KindTV, "", "", false},
		{"old 3-part shortcode rejected", "heya:tmdb:130636", metadata.KindTV, "", "", false},

		// Non-URL / non-shortcode inputs
		{"plain text", "Oshi no Ko", metadata.KindTV, "", "", false},
		{"empty", "", metadata.KindTV, "", "", false},
		{"heya media root only", "https://heya.media/", metadata.KindTV, "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotProv, gotID, gotOK := parseIdentifyURL(c.in, c.hint)
			if gotOK != c.ok || gotProv != c.provider || gotID != c.id {
				t.Errorf("parseIdentifyURL(%q, %s) = (%q, %q, %v); want (%q, %q, %v)",
					c.in, c.hint, gotProv, gotID, gotOK, c.provider, c.id, c.ok)
			}
		})
	}
}
