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

	// Anime-specific id tags, near-universal on anime library folders
	// ("Series Title {anidb-2662}"). AniDB is the authoritative id for the
	// absolute-numbering libraries these anchor; AniList / MAL are carried too
	// so the matcher can fan out to whichever the aggregator knows.
	reAniDBID   = regexp.MustCompile(`(?i)[\[{](?:anidbid|anidb)[-=](\d{1,9})[\]}]`)
	reAniListID = regexp.MustCompile(`(?i)[\[{](?:anilistid|anilist)[-=](\d{1,9})[\]}]`)
	reMALID     = regexp.MustCompile(`(?i)[\[{](?:malid|mal|myanimelist)[-=](\d{1,9})[\]}]`)
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

// ParseAnimeIDs extracts embedded AniDB / AniList / MAL ids from a release name
// or path segment. Returns empty strings for any not present. These live on the
// series folder in the ubiquitous "Series Title {anidb-2662}" anime layout.
func ParseAnimeIDs(s string) (anidb, anilist, mal string) {
	if m := reAniDBID.FindStringSubmatch(s); m != nil {
		anidb = m[1]
	}
	if m := reAniListID.FindStringSubmatch(s); m != nil {
		anilist = m[1]
	}
	if m := reMALID.FindStringSubmatch(s); m != nil {
		mal = m[1]
	}
	return
}

// anyAnimeTagRE matches any of the anime id tags above; used to detect that a
// path lives under an anime library so bracket-less absolute-numbered episode
// files ("Series - 24 - Title.mkv") can be parsed as TV.
var anyAnimeTagRE = regexp.MustCompile(`(?i)[\[{](?:anidb|anilist|mal|myanimelist)(?:id)?[-=]\d{1,9}[\]}]`)

// PathLooksLikeAnime reports whether any segment of a path carries an anime id
// tag ({anidb-…} / {anilist-…} / {mal-…}). This is the signal that turns on
// bracket-less absolute-episode parsing for the whole path.
func PathLooksLikeAnime(segments []string) bool {
	for _, seg := range segments {
		if anyAnimeTagRE.MatchString(seg) {
			return true
		}
	}
	return false
}
