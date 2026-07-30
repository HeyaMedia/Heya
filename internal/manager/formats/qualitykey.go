package formats

import (
	"regexp"
	"strings"
)

// QualityKey maps parsed video release attrs onto the manager quality-ladder
// vocabulary ("webdl-1080p", "remux-2160p", "dvd", …). Empty string means the
// release doesn't land anywhere in the ladder — callers treat that as
// unmapped, never as some invented nearest quality.
func QualityKey(attrs Attrs) string {
	switch attrs.Modifier {
	case "brdisk":
		return "br-disk"
	case "rawhd":
		return "raw-hd"
	case "remux":
		switch attrs.Resolution {
		case 2160:
			return "remux-2160p"
		case 1080:
			return "remux-1080p"
		case 720:
			// No 720p remux quality exists in the arr vocabulary either;
			// they parse as Bluray-720p.
			return "bluray-720p"
		}
		return ""
	}

	source := ""
	for _, s := range attrs.Sources {
		switch s {
		case "bluray", "webdl", "webrip", "tv", "dvd":
			source = s
		}
		if source != "" {
			break
		}
	}

	if source == "dvd" {
		return "dvd"
	}

	res := ""
	switch attrs.Resolution {
	case 2160:
		res = "2160p"
	case 1080:
		res = "1080p"
	case 720:
		res = "720p"
	case 576:
		res = "576p"
	case 480:
		res = "480p"
	}

	switch source {
	case "bluray":
		if res != "" {
			return "bluray-" + res
		}
		return ""
	case "webdl", "webrip":
		switch res {
		case "2160p", "1080p", "720p", "480p":
			return source + "-" + res
		}
		return ""
	case "tv":
		switch res {
		case "2160p", "1080p", "720p":
			return "hdtv-" + res
		case "576p", "480p", "":
			return "sdtv"
		}
	}
	// No recognized source: cam/telesync/screener and friends have no ladder
	// slot on purpose, and a bare unresolved name maps nowhere.
	return ""
}

// Music quality tokens are parsed straight off the release title — the scene
// music parser normalizes titles but doesn't classify bitrate/mode vocabulary.
var (
	musicBitDepthRE = regexp.MustCompile(`(?i)\b(24[ ._-]?bit|24/(?:44|48|88|96|176|192))\b`)
	musicTokenRE    = regexp.MustCompile(`(?i)\b(flac|alac|ape|wavpack|wav|aac|ogg|vorbis|mp3|320|v0|256|v2|192|q10)\b`)
)

// MusicQualityKey maps a music release title onto the music ladder
// ("flac-24", "mp3-320", …). Empty string = unmapped. Codec tokens are
// checked before bare bitrate tokens so "AAC 320" lands on aac-320, not
// mp3-320; a bare bitrate with no codec assumes MP3 (scene convention).
func MusicQualityKey(title string) string {
	tokens := map[string]bool{}
	for _, m := range musicTokenRE.FindAllString(title, -1) {
		tokens[strings.ToLower(m)] = true
	}
	switch {
	case tokens["flac"] && musicBitDepthRE.MatchString(title):
		return "flac-24"
	case tokens["flac"]:
		return "flac"
	case tokens["alac"]:
		return "alac"
	case tokens["ape"]:
		return "ape"
	case tokens["wavpack"]:
		return "wavpack"
	case tokens["aac"]:
		switch {
		case tokens["320"]:
			return "aac-320"
		case tokens["256"]:
			return "aac-256"
		}
		// AAC without an explicit bitrate is ambiguous between the two
		// ladder slots — unmapped rather than guessed.
		return ""
	case tokens["ogg"] || tokens["vorbis"]:
		if tokens["q10"] {
			return "ogg-vorbis-q10"
		}
		return ""
	case tokens["wav"]:
		return "wav"
	case tokens["mp3"] || tokens["320"] || tokens["v0"] || tokens["256"] || tokens["v2"] || tokens["192"]:
		switch {
		case tokens["320"]:
			return "mp3-320"
		case tokens["v0"]:
			return "mp3-v0"
		case tokens["256"]:
			return "mp3-256"
		case tokens["v2"]:
			return "mp3-v2"
		case tokens["192"]:
			return "mp3-192"
		}
		return ""
	}
	return ""
}

var bookExtRE = regexp.MustCompile(`(?i)\b(epub|azw3|mobi|pdf|cbz)\b`)

// BookQualityKey maps an ebook release title onto the book ladder. Audiobook
// formats are deliberately unmapped in v1 — acquisition doesn't serve them.
func BookQualityKey(title string) string {
	m := bookExtRE.FindString(title)
	if m == "" {
		return ""
	}
	return strings.ToLower(m)
}
