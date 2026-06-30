package parser

import (
	"regexp"
	"strings"
)

// Provider-ID tags embedded in release names / path segments. Supports the two
// conventions seen in the wild:
//   - Radarr/Sonarr/Jellyfin curly form: {imdb-tt0113198} {tmdb-603} {tvdb-1234}
//   - Kodi/Jellyfin bracket form:        [imdbid=tt0113198] [tmdbid-603] [tvdbid=1234]
//
// IMDb IDs keep their "tt" prefix; TMDB/TVDB are bare digits.
var (
	reIMDBID = regexp.MustCompile(`(?i)[\[{](?:imdbid|imdb)[-=](tt\d{6,9})[\]}]`)
	reTMDBID = regexp.MustCompile(`(?i)[\[{](?:tmdbid|tmdb)[-=](\d{1,9})[\]}]`)
	reTVDBID = regexp.MustCompile(`(?i)[\[{](?:tvdbid|tvdb)[-=](\d{1,9})[\]}]`)
)

// ParseProviderIDs extracts embedded IMDb/TMDB/TVDB IDs from a release name or
// path segment. Returns empty strings for any not present. IMDb is lower-cased
// (the "tt" prefix is canonical); TMDB/TVDB are returned as-is.
func ParseProviderIDs(s string) (imdb, tmdb, tvdb string) {
	if m := reIMDBID.FindStringSubmatch(s); m != nil {
		imdb = strings.ToLower(m[1])
	}
	if m := reTMDBID.FindStringSubmatch(s); m != nil {
		tmdb = m[1]
	}
	if m := reTVDBID.FindStringSubmatch(s); m != nil {
		tvdb = m[1]
	}
	return
}
