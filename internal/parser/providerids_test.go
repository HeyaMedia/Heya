package parser

import "testing"

func TestParseProviderIDs(t *testing.T) {
	cases := []struct {
		name             string
		in               string
		imdb, tmdb, tvdb string
	}{
		{"curly imdb", "A Goofy Movie (1995) {imdb-tt0113198} [Bluray-1080p][x264]-BHDStudio.mkv", "tt0113198", "", ""},
		{"curly tmdb", "Some Movie (2024) {tmdb-603}.mkv", "", "603", ""},
		{"curly tvdb", "Some Show (2024) {tvdb-81189}", "", "", "81189"},
		{"bracket imdbid equals", "The Matrix (1999) [imdbid=tt0133093].mkv", "tt0133093", "", ""},
		{"bracket tmdbid dash", "Movie [tmdbid-603].mkv", "", "603", ""},
		{"case insensitive, lowercased imdb", "Movie {IMDB-TT0113198}.mkv", "tt0113198", "", ""},
		{"all three", "X {imdb-tt0000001} {tmdb-2} {tvdb-3}", "tt0000001", "2", "3"},
		{"none", "A Goofy Movie (1995) [Bluray-1080p][x264]-BHDStudio.mkv", "", "", ""},
		{"imdb without tt is not matched", "Movie {imdb-12345}.mkv", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			imdb, tmdb, tvdb := ParseProviderIDs(c.in)
			if imdb != c.imdb || tmdb != c.tmdb || tvdb != c.tvdb {
				t.Errorf("ParseProviderIDs(%q) = (%q,%q,%q); want (%q,%q,%q)",
					c.in, imdb, tmdb, tvdb, c.imdb, c.tmdb, c.tvdb)
			}
		})
	}
}

// The ID may sit on the filename even when the release folder doesn't carry it
// (the common Radarr movie layout).
func TestParseStoragePathExtractsProviderID(t *testing.T) {
	p := ParseStoragePath("/storage/Movies/A Goofy Movie (1995)/A Goofy Movie (1995) {imdb-tt0113198} [Bluray-1080p][x264]-BHDStudio.mkv")
	if p.Release == nil {
		t.Fatal("expected a release parse")
	}
	if p.Release.ImdbID != "tt0113198" {
		t.Errorf("ImdbID = %q; want tt0113198", p.Release.ImdbID)
	}
}
