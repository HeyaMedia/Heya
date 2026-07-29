package formats

import (
	"strconv"
	"strings"

	"github.com/karbowiak/heya/internal/parser/video"
)

// Attrs are the parsed attributes of one release candidate — everything the
// spec matchers consult. Built from the release title (plus indexer-reported
// size); OriginalLanguage comes from the media item when the caller knows it.
type Attrs struct {
	Title            string
	Group            string
	Resolution       int
	Sources          []string
	Modifier         string
	Edition          string
	Languages        []string
	Year             int
	SizeBytes        int64
	ReleaseType      string
	OriginalLanguage string
}

var parserSourceNames = map[video.Source]string{
	video.SourceBLURAY:    "bluray",
	video.SourceWEBDL:     "webdl",
	video.SourceWEBRIP:    "webrip",
	video.SourceDVD:       "dvd",
	video.SourceTV:        "tv",
	video.SourceCAM:       "cam",
	video.SourceTELESYNC:  "telesync",
	video.SourceTELECINE:  "telecine",
	video.SourceWORKPRINT: "workprint",
	video.SourceSCREENER:  "screener",
	video.SourcePPV:       "ppv",
}

func resolutionHeight(r video.Resolution) int {
	if r == "" {
		return 0
	}
	height, err := strconv.Atoi(strings.TrimSuffix(string(r), "P"))
	if err != nil {
		return 0
	}
	return height
}

// ParseVideoRelease derives Attrs from a release title the way the arr apps
// parse candidate releases. isTV additionally derives the release type
// (single episode / multi / season pack).
func ParseVideoRelease(title string, sizeBytes int64, isTV bool) Attrs {
	attrs := Attrs{Title: title, SizeBytes: sizeBytes}

	quality := video.ParseQuality(title)
	attrs.Resolution = resolutionHeight(quality.Resolution)
	for _, source := range quality.Sources {
		if name, ok := parserSourceNames[source]; ok {
			attrs.Sources = append(attrs.Sources, name)
		}
	}
	switch quality.Modifier {
	case video.ModREMUX:
		attrs.Modifier = "remux"
	case video.ModBRDISK:
		attrs.Modifier = "brdisk"
	case video.ModRAWHD:
		attrs.Modifier = "rawhd"
	default:
		attrs.Modifier = "none"
	}

	attrs.Group = video.ParseGroup(title)
	attrs.Edition = strings.Join(video.ParseEdition(title).Flags(), " ")
	for _, lang := range video.ParseLanguage(title) {
		attrs.Languages = append(attrs.Languages, strings.ToLower(string(lang)))
	}

	if isTV {
		show := video.FilenameParseShow(title)
		if year, err := strconv.Atoi(show.Year); err == nil {
			attrs.Year = year
		}
		switch {
		case show.FullSeason || show.IsMultiSeason:
			attrs.ReleaseType = "season-pack"
		case len(show.EpisodeNumbers) > 1:
			attrs.ReleaseType = "multi"
		case len(show.EpisodeNumbers) == 1:
			attrs.ReleaseType = "single"
		}
	} else {
		movie := video.FilenameParseMovie(title)
		if year, err := strconv.Atoi(movie.Year); err == nil {
			attrs.Year = year
		}
	}

	return attrs
}

// LanguageAcceptable applies a profile's language gate to a release:
// 'any' passes everything, 'original' requires the media item's original
// audio (English when unknown), and a language name requires that language
// among the release's effective languages.
func LanguageAcceptable(profileLanguage string, attrs Attrs) bool {
	switch profileLanguage {
	case "", "any":
		return true
	case "original":
		original := attrs.OriginalLanguage
		if original == "" {
			original = "english"
		}
		return contains(attrs.effectiveLanguages(), original)
	default:
		return contains(attrs.effectiveLanguages(), profileLanguage)
	}
}

// effectiveLanguages resolves the languages a release is assumed to carry: a
// title with no language token is the original audio (scene convention), and
// original falls back to English when the caller didn't supply one.
func (a Attrs) effectiveLanguages() []string {
	original := a.OriginalLanguage
	if original == "" {
		original = "english"
	}
	if len(a.Languages) == 0 {
		return []string{original}
	}
	langs := make([]string, 0, len(a.Languages))
	for _, lang := range a.Languages {
		if lang == "original" {
			lang = original
		}
		langs = append(langs, lang)
	}
	return langs
}
