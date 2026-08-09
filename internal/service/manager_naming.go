package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const managerFileNamingKey = "manager_file_naming"

// ManagerFileNamingSettings owns output paths. Acquisition custom formats
// score releases; these templates only decide where accepted files land.
type ManagerFileNamingSettings struct {
	Movie      string `json:"movie"`
	TV         string `json:"tv"`
	DailyTV    string `json:"daily_tv"`
	Anime      string `json:"anime"`
	Music      string `json:"music"`
	MusicMulti string `json:"music_multi"`
}

type ManagerNamingTokenView struct {
	Token       string `json:"token"`
	Description string `json:"description"`
	Example     string `json:"example"`
}

type ManagerFileNamingView struct {
	Settings ManagerFileNamingSettings `json:"settings"`
	Tokens   []ManagerNamingTokenView  `json:"tokens"`
	Examples map[string]string         `json:"examples"`
}

func DefaultManagerFileNamingSettings() ManagerFileNamingSettings {
	return ManagerFileNamingSettings{
		Movie:      `{Movie CleanTitle} {(Release Year)} {imdb-{ImdbId}} {edition-{Edition Tags}} {[Custom Formats]}{[Quality Full]}{[MediaInfo 3D]}{[MediaInfo VideoDynamicRangeType]}{[Mediainfo AudioCodec}{ Mediainfo AudioChannels}]{MediaInfo AudioLanguagesAll}[{MediaInfo VideoBitDepth}bit][{Mediainfo VideoCodec}]{MediaInfo SubtitleLanguagesAll}{-Release Group}`,
		TV:         `{Series TitleYear} - S{season:00}E{episode:00} [{Custom Formats }{Quality Full}]{[MediaInfo VideoDynamicRangeType]}{[Mediainfo AudioCodec}{ Mediainfo AudioChannels]}{MediaInfo AudioLanguagesAll}[{MediaInfo VideoBitDepth}bit]{[MediaInfo VideoCodec]}{MediaInfo SubtitleLanguagesAll}{-Release Group}`,
		DailyTV:    `{Series TitleYear} - {Air-Date} [{Custom Formats }{Quality Full}]{[MediaInfo VideoDynamicRangeType]}{[Mediainfo AudioCodec}{ Mediainfo AudioChannels]}{MediaInfo AudioLanguagesAll}[{MediaInfo VideoBitDepth}bit]{[MediaInfo VideoCodec]}{MediaInfo SubtitleLanguagesAll}{-Release Group}`,
		Anime:      `{Series TitleYear} - S{season:00}E{episode:00} - {absolute:000} [{Custom Formats }{Quality Full}]{[MediaInfo VideoDynamicRangeType]}[{Mediainfo AudioCodec} {Mediainfo AudioChannels}]{MediaInfo AudioLanguagesAll}[{MediaInfo VideoBitDepth}bit]{[MediaInfo VideoCodec]}{MediaInfo SubtitleLanguagesAll}{-Release Group:10}`,
		Music:      `{Artist CleanName} - {Album Type} - {Release Year} - {Album CleanTitle}/{medium:00}{track:00} - {Track CleanTitle}`,
		MusicMulti: `{Artist CleanName} - {Album Type} - {Release Year} - {Album CleanTitle}/{medium:00}{track:00} - {Track CleanTitle}`,
	}
}

var managerNamingTokens = []ManagerNamingTokenView{
	{Token: "{Movie Title}", Description: "Movie title", Example: "Dune: Part Two"},
	{Token: "{Movie CleanTitle}", Description: "Movie title safe for a filename", Example: "Dune Part Two"},
	{Token: "{Movie TitleThe}", Description: "Movie title with a leading article moved to the end", Example: "Batman, The"},
	{Token: "{Movie CleanTitleThe}", Description: "Filename-safe title with its article moved", Example: "Batman, The"},
	{Token: "{Movie OriginalTitle}", Description: "Movie title in its original language", Example: "Le fabuleux destin d'Amélie Poulain"},
	{Token: "{Movie CleanOriginalTitle}", Description: "Filename-safe original-language title", Example: "Le fabuleux destin dAmelie Poulain"},
	{Token: "{Movie TitleFirstCharacter}", Description: "First sortable character of the movie title", Example: "D"},
	{Token: "{Movie Certification}", Description: "Movie content certification", Example: "PG-13"},
	{Token: "{Movie Collection}", Description: "Movie collection title", Example: "Dune Collection"},
	{Token: "{Movie CollectionThe}", Description: "Collection title with its article moved", Example: "Godfather Collection, The"},
	{Token: "{Movie CleanCollectionThe}", Description: "Filename-safe collection title with its article moved", Example: "Godfather Collection, The"},
	{Token: "{Series Title}", Description: "Series title", Example: "Lioness"},
	{Token: "{Series CleanTitle}", Description: "Filename-safe series title", Example: "Lioness"},
	{Token: "{Series TitleYear}", Description: "Series title with its first-air year", Example: "Lioness (2023)"},
	{Token: "{Series CleanTitleYear}", Description: "Filename-safe series title with year", Example: "Lioness 2023"},
	{Token: "{Series TitleWithoutYear}", Description: "Series title with a trailing year removed", Example: "Lioness"},
	{Token: "{Series CleanTitleWithoutYear}", Description: "Filename-safe series title without year", Example: "Lioness"},
	{Token: "{Series TitleThe}", Description: "Series title with a leading article moved", Example: "Last of Us, The"},
	{Token: "{Series CleanTitleThe}", Description: "Filename-safe series title with its article moved", Example: "Last of Us, The"},
	{Token: "{Series TitleTheYear}", Description: "Article-sorted series title with year", Example: "Last of Us, The (2023)"},
	{Token: "{Series CleanTitleTheYear}", Description: "Filename-safe article-sorted title with year", Example: "Last of Us, The 2023"},
	{Token: "{Series TitleTheWithoutYear}", Description: "Article-sorted series title without its year", Example: "Last of Us, The"},
	{Token: "{Series CleanTitleTheWithoutYear}", Description: "Filename-safe article-sorted title without year", Example: "Last of Us, The"},
	{Token: "{Series TitleFirstCharacter}", Description: "First sortable character of the series title", Example: "L"},
	{Token: "{Series Year}", Description: "Series first-air year", Example: "2023"},
	{Token: "{season:00}", Description: "Two-digit season number", Example: "02"},
	{Token: "{episode:00}", Description: "Two-digit episode number", Example: "08"},
	{Token: "{Season}", Description: "Season number; supports .NET-style numeric padding", Example: "2"},
	{Token: "{Episode}", Description: "Episode number or multi-episode range", Example: "8"},
	{Token: "{absolute:000}", Description: "Three-digit absolute episode number", Example: "043"},
	{Token: "{Episode Title}", Description: "Episode title", Example: "The Compass Points Home"},
	{Token: "{Episode CleanTitle}", Description: "Filename-safe episode title", Example: "The Compass Points Home"},
	{Token: "{Air Date}", Description: "Episode air date; custom date formats are supported", Example: "2026 08 09"},
	{Token: "{Air-Date}", Description: "Daily episode air date", Example: "2026-08-09"},
	{Token: "{Artist Name}", Description: "Artist name", Example: "Sabrina Carpenter"},
	{Token: "{Artist CleanName}", Description: "Artist name safe for a folder", Example: "Sabrina Carpenter"},
	{Token: "{Artist NameThe}", Description: "Artist name with a leading article moved", Example: "Beatles, The"},
	{Token: "{Artist CleanNameThe}", Description: "Filename-safe artist name with its article moved", Example: "Beatles, The"},
	{Token: "{Artist NameFirstCharacter}", Description: "First sortable character of the artist name", Example: "S"},
	{Token: "{Artist Genre}", Description: "Primary artist genre", Example: "Pop"},
	{Token: "{Artist Disambiguation}", Description: "MusicBrainz artist disambiguation", Example: "US singer-songwriter"},
	{Token: "{Artist MbId}", Description: "MusicBrainz artist identifier", Example: "6aa40207-..."},
	{Token: "{Album Title}", Description: "Album title", Example: "emails i can't send"},
	{Token: "{Album Type}", Description: "Album, EP, Single, or other release type", Example: "Album"},
	{Token: "{Album CleanTitle}", Description: "Album title safe for a folder", Example: "emails i can't send"},
	{Token: "{Album TitleThe}", Description: "Album title with a leading article moved", Example: "Album, The"},
	{Token: "{Album CleanTitleThe}", Description: "Filename-safe album title with its article moved", Example: "Album, The"},
	{Token: "{Album Genre}", Description: "Primary album genre", Example: "Pop"},
	{Token: "{Album Disambiguation}", Description: "MusicBrainz album disambiguation", Example: "deluxe edition"},
	{Token: "{Album MbId}", Description: "MusicBrainz release-group identifier", Example: "a1b2c3d4-..."},
	{Token: "{Medium Name}", Description: "Medium or disc name", Example: "Bonus Disc"},
	{Token: "{Medium Format}", Description: "Medium format", Example: "CD"},
	{Token: "{medium:00}", Description: "Two-digit disc or medium number", Example: "01"},
	{Token: "{track:00}", Description: "Two-digit track number", Example: "02"},
	{Token: "{Track Title}", Description: "Track title", Example: "Vicious"},
	{Token: "{Track CleanTitle}", Description: "Track title safe for a filename", Example: "Vicious"},
	{Token: "{Track ArtistName}", Description: "Track artist name", Example: "Sabrina Carpenter"},
	{Token: "{Track ArtistCleanName}", Description: "Filename-safe track artist name", Example: "Sabrina Carpenter"},
	{Token: "{Track ArtistNameThe}", Description: "Track artist with its article moved", Example: "Beatles, The"},
	{Token: "{Track ArtistCleanNameThe}", Description: "Filename-safe article-sorted track artist", Example: "Beatles, The"},
	{Token: "{Track ArtistMbId}", Description: "MusicBrainz track artist identifier", Example: "6aa40207-..."},
	{Token: "{Release Year}", Description: "Four-digit release year", Example: "2022"},
	{Token: "{Quality Full}", Description: "Parsed source and resolution quality", Example: "WEBDL-1080p"},
	{Token: "{Quality Title}", Description: "Quality name without revision flags", Example: "WEBDL-1080p"},
	{Token: "{Quality Proper}", Description: "PROPER or REPACK revision", Example: "PROPER"},
	{Token: "{Quality Real}", Description: "REAL revision", Example: "REAL"},
	{Token: "{Custom Formats}", Description: "Matched acquisition custom-format labels", Example: "AMZN HDR10"},
	{Token: "{Custom Format}", Description: "Matched custom formats using Arr filter syntax", Example: "AMZN"},
	{Token: "{Release Group}", Description: "Parsed release group", Example: "NTb"},
	{Token: "{Release Hash}", Description: "Sonarr release hash when available", Example: "A1B2C3"},
	{Token: "{Original Title}", Description: "Original scene or release title", Example: "Dune.Part.Two.2024..."},
	{Token: "{Original Filename}", Description: "Original filename without extension", Example: "Dune.Part.Two.2024..."},
	{Token: "{ImdbId}", Description: "IMDb identifier when known", Example: "tt15239678"},
	{Token: "{TmdbId}", Description: "TMDB identifier when known", Example: "693134"},
	{Token: "{TvdbId}", Description: "TVDB series identifier when known", Example: "401239"},
	{Token: "{TvMazeId}", Description: "TVmaze series identifier when known", Example: "55546"},
	{Token: "{Edition Tags}", Description: "Movie edition label when known", Example: "IMAX"},
	{Token: "{Mediainfo VideoCodec}", Description: "Video codec from the file or release", Example: "x265"},
	{Token: "{MediaInfo Video}", Description: "Formatted video codec", Example: "x265"},
	{Token: "{MediaInfo VideoDynamicRangeType}", Description: "HDR format", Example: "HDR10"},
	{Token: "{Mediainfo AudioCodec}", Description: "Primary audio codec", Example: "EAC3"},
	{Token: "{MediaInfo Audio}", Description: "Formatted primary audio codec", Example: "EAC3"},
	{Token: "{Mediainfo AudioChannels}", Description: "Primary audio channel layout", Example: "5.1"},
	{Token: "{MediaInfo AudioBitRate}", Description: "Audio bitrate", Example: "921kbps"},
	{Token: "{MediaInfo AudioBitsPerSample}", Description: "Audio bit depth", Example: "24bit"},
	{Token: "{MediaInfo AudioSampleRate}", Description: "Audio sample rate", Example: "48.0kHz"},
	{Token: "{MediaInfo Simple}", Description: "Compact video and audio summary", Example: "x265 EAC3"},
	{Token: "{MediaInfo Full}", Description: "Video, audio, and language summary", Example: "x265 EAC3 [EN]"},
	{Token: "{MediaInfo 3D}", Description: "3D marker when the video is stereoscopic", Example: "3D"},
	{Token: "{MediaInfo VideoBitDepth}", Description: "Video bit depth", Example: "10"},
	{Token: "{MediaInfo AudioLanguagesAll}", Description: "All audio languages", Example: "[EN+DA]"},
	{Token: "{MediaInfo SubtitleLanguagesAll}", Description: "All subtitle languages", Example: "[EN+DA]"},
	{Token: "{ellipsis}", Description: "Literal ellipsis supported by Arr folder formats", Example: "..."},
}

func (a *App) GetManagerFileNaming(ctx context.Context) ManagerFileNamingView {
	settings := DefaultManagerFileNamingSettings()
	if saved, ok := readSetting[ManagerFileNamingSettings](a, ctx, managerFileNamingKey); ok {
		settings = fillManagerNamingDefaults(saved, settings)
	}
	return managerFileNamingView(settings)
}

func (a *App) SaveManagerFileNaming(ctx context.Context, settings ManagerFileNamingSettings) (ManagerFileNamingView, error) {
	settings = fillManagerNamingDefaults(settings, DefaultManagerFileNamingSettings())
	for _, template := range []string{settings.Movie, settings.TV, settings.DailyTV, settings.Anime, settings.Music, settings.MusicMulti} {
		if strings.TrimSpace(template) == "" {
			return ManagerFileNamingView{}, errors.New("file naming templates cannot be empty")
		}
		if strings.Contains(template, "..") || strings.HasPrefix(template, "/") {
			return ManagerFileNamingView{}, errors.New("file naming templates must be relative and cannot contain '..'")
		}
	}
	if err := writeSetting(a, ctx, managerFileNamingKey, settings); err != nil {
		return ManagerFileNamingView{}, err
	}
	a.notifyManagerChanged(ctx, "file_naming")
	return managerFileNamingView(settings), nil
}

func fillManagerNamingDefaults(value, defaults ManagerFileNamingSettings) ManagerFileNamingSettings {
	if value.Movie == "" {
		value.Movie = defaults.Movie
	}
	if value.TV == "" {
		value.TV = defaults.TV
	}
	if value.DailyTV == "" {
		value.DailyTV = defaults.DailyTV
	}
	if value.Anime == "" {
		value.Anime = defaults.Anime
	}
	if value.Music == "" {
		value.Music = defaults.Music
	}
	if value.MusicMulti == "" {
		value.MusicMulti = defaults.MusicMulti
	}
	return value
}

func managerFileNamingView(settings ManagerFileNamingSettings) ManagerFileNamingView {
	sample := map[string]string{
		"Movie CleanTitle": "Dune Part Two", "Release Year": "2024", "ImdbId": "tt15239678",
		"Quality Full": "WEBDL-1080p", "Release Group": "NTb", "Series TitleYear": "Lioness (2023)",
		"season:00": "02", "episode:00": "08", "absolute:000": "043", "Air-Date": "2026-08-09",
		"Artist CleanName": "Sabrina Carpenter", "Album Type": "Album", "Album CleanTitle": "emails i can't send",
		"medium:00": "01", "track:00": "02", "Track CleanTitle": "Vicious",
	}
	return ManagerFileNamingView{Settings: settings, Tokens: managerNamingTokens, Examples: map[string]string{
		"movie": renderManagerFilename(settings.Movie, sample), "tv": renderManagerFilename(settings.TV, sample),
		"daily_tv": renderManagerFilename(settings.DailyTV, sample), "anime": renderManagerFilename(settings.Anime, sample),
		"music": renderManagerFilename(settings.Music, sample), "music_multi": renderManagerFilename(settings.MusicMulti, sample),
	}}
}

var namingExpression = regexp.MustCompile(`\{([^{}]+)\}`)
var nestedNamingExpression = regexp.MustCompile(`\{([^{}]*)\{([^{}]+)\}([^{}]*)\}`)

// renderManagerFilename implements Arr-style conditional wrappers: literal
// punctuation surrounding a token is emitted only when that token has data.
func renderManagerFilename(template string, facts map[string]string) string {
	for nestedNamingExpression.MatchString(template) {
		template = nestedNamingExpression.ReplaceAllStringFunc(template, func(expr string) string {
			match := nestedNamingExpression.FindStringSubmatch(expr)
			if len(match) != 4 || facts[match[2]] == "" {
				return ""
			}
			return match[1] + facts[match[2]] + match[3]
		})
	}
	keys := make([]string, 0, len(facts))
	for key := range facts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	out := namingExpression.ReplaceAllStringFunc(template, func(expr string) string {
		body := strings.TrimSuffix(strings.TrimPrefix(expr, "{"), "}")
		for _, key := range keys {
			if strings.EqualFold(body, key) && facts[key] != "" {
				return facts[key]
			}
		}
		for _, key := range keys {
			idx := strings.Index(strings.ToLower(body), strings.ToLower(key))
			if idx < 0 || facts[key] == "" {
				continue
			}
			value, suffix := facts[key], body[idx+len(key):]
			if strings.HasPrefix(suffix, ":") {
				format := strings.TrimPrefix(suffix, ":")
				if width, err := strconv.Atoi(format); err == nil {
					if strings.Trim(format, "0") == "" {
						if number, numberErr := strconv.Atoi(value); numberErr == nil {
							value = fmt.Sprintf("%0*d", len(format), number)
						}
					} else if len([]rune(value)) > width {
						value = string([]rune(value)[:width])
					}
					suffix = ""
				}
			}
			return body[:idx] + value + suffix
		}
		return ""
	})
	out = strings.ReplaceAll(out, "[]", "")
	out = regexp.MustCompile(`\[\s*\]`).ReplaceAllString(out, "")
	out = regexp.MustCompile(`\(\s*\)`).ReplaceAllString(out, "")
	if strings.Count(out, "[") != strings.Count(out, "]") {
		out = strings.NewReplacer("[", "", "]", "").Replace(out)
	}
	out = regexp.MustCompile(`[ \t]+`).ReplaceAllString(out, " ")
	out = regexp.MustCompile(` */ *`).ReplaceAllString(out, "/")
	return strings.TrimSpace(out)
}
