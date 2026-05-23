package service

import "testing"

func TestParseIdentifyURL(t *testing.T) {
	cases := []struct {
		in       string
		provider string
		id       string
		ok       bool
	}{
		{"https://heya.media/heya_tv:oshi-no-ko-2023", "heya", "heya:oshi-no-ko-2023", true},
		{"http://localhost:3030/heya_tv:oshi-no-ko-2023", "heya", "heya:oshi-no-ko-2023", true},
		{"https://media.heya.test/heya_movie:dune-2021", "heya", "heya:dune-2021", true},
		{"heya_tv:oshi-no-ko-2023", "heya", "heya:oshi-no-ko-2023", true},
		{"heya:oshi-no-ko-2023", "heya", "heya:oshi-no-ko-2023", true},
		{"heya:tmdb:130636", "heya", "heya:tmdb:130636", true},
		{"https://www.themoviedb.org/tv/130636-oshi-no-ko", "heya", "heya:tmdb:130636", true},
		{"https://www.themoviedb.org/movie/438631-dune", "heya", "heya:tmdb:438631", true},
		{"https://www.thetvdb.com/series/421069", "heya", "heya:tvdb:421069", true},
		{"https://www.imdb.com/title/tt15398776/", "heya", "heya:imdb:tt15398776", true},
		{"Oshi no Ko", "", "", false},
		{"", "", "", false},
		{"https://heya.media/", "", "", false},
	}
	for _, c := range cases {
		gotProv, gotID, gotOK := parseIdentifyURL(c.in)
		if gotOK != c.ok || gotProv != c.provider || gotID != c.id {
			t.Errorf("parseIdentifyURL(%q) = (%q, %q, %v); want (%q, %q, %v)",
				c.in, gotProv, gotID, gotOK, c.provider, c.id, c.ok)
		}
	}
}
