package video

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var simpleTitleRegex = regexp.MustCompile(`(?i)\s*(?:480[ip]|576[ip]|720[ip]|1080[ip]|2160[ip]|HVEC|[xh][\W_]?26[45]|DD\W?5\W1|[<>?*:|]|848x480|1280x720|1920x1080)(?:(?:8|10)b(?:it))?`)
var websitePrefixRegex = regexp.MustCompile(`(?i)^\[\s*[a-z]+(\.[a-z]+)+\s*\][- ]*|^www\.[a-z]+\.(?:com|net)[ -]*`)
var cleanTorrentPrefixRegex = regexp.MustCompile(`(?i)^\[(?:REQ)\]`)
var cleanTorrentSuffixRegex = regexp.MustCompile(`(?i)\[(?:ettv|rartv|rarbg|cttv)\]$`)
var commonSourcesRegex = regexp.MustCompile(`(?i)\b(Bluray|(?:dvdr?|BD)rip|HDTV|HDRip|TS|R5|CAM|SCR|(?:WEB|DVD)?\.?SCREENER|DiVX|xvid|web-?dl)\b`)

func SimplifyTitle(title string) string {
	simpleTitle := simpleTitleRegex.ReplaceAllString(title, "")
	simpleTitle = websitePrefixRegex.ReplaceAllString(simpleTitle, "")
	simpleTitle = cleanTorrentPrefixRegex.ReplaceAllString(simpleTitle, "")
	simpleTitle = cleanTorrentSuffixRegex.ReplaceAllString(simpleTitle, "")
	simpleTitle = commonSourcesRegex.ReplaceAllString(simpleTitle, "")
	simpleTitle = WebdlExp.ReplaceAllString(simpleTitle, "")

	codec1 := ParseVideoCodec(simpleTitle)
	if codec1.Source != "" {
		simpleTitle = strings.Replace(simpleTitle, codec1.Source, "", 1)
	}
	codec2 := ParseVideoCodec(simpleTitle)
	if codec2.Source != "" {
		simpleTitle = strings.Replace(simpleTitle, codec2.Source, "", 1)
	}

	return strings.TrimSpace(simpleTitle)
}

var requestInfoRegex = regexp.MustCompile(`(?i)\[.+?\]`)
var editionExp = regexp.MustCompile(`(?i)\b(?:(?:Extended\.|Ultimate\.)?(?:Director\.?s|Collector\.?s|Theatrical|Anniversary|The\.Uncut|DC|Ultimate|Final)[. ](?:Cut|Edition|Version)(?:[. ](?:Extended|Uncensored|Remastered|Unrated|Uncut|IMAX|Fan\.?Edit))?|Extended[. ](?:Cut|Edition|Version)|Special[. ]Edition|Despecialized|unrated|\d{2,3}(?:th)?\.Anniversary|(?:Uncensored|Remastered|Unrated|Uncut|IMAX|Fan\.?Edit|Edition|Restored|(?:2|3|4)in1)){1,3}`)
var languageCleanExp = regexp.MustCompile(`(?i)\b(?:TRUE\.?FRENCH|videomann|SUBFRENCH|PLDUB|MULTI)\b`)
var sceneGarbageExp = regexp.MustCompile(`\b(PROPER|REAL|READ\.NFO)`)
var numericTitleSuffixExp = regexp.MustCompile(`\.\.(\d+)[.\s]*$`)

func ReleaseTitleCleaner(title string) string {
	if title == "" || title == "(" {
		return ""
	}

	trimmed := strings.ReplaceAll(title, "_", " ")
	trimmed = requestInfoRegex.ReplaceAllString(trimmed, "")
	trimmed = strings.TrimSpace(trimmed)
	trimmed = commonSourcesRegex.ReplaceAllString(trimmed, "")
	trimmed = strings.TrimSpace(trimmed)
	trimmed = WebdlExp.ReplaceAllString(trimmed, "")
	trimmed = strings.TrimSpace(trimmed)
	trimmed = editionExp.ReplaceAllString(trimmed, "")
	trimmed = strings.TrimSpace(trimmed)
	trimmed = languageCleanExp.ReplaceAllString(trimmed, "")
	trimmed = strings.TrimSpace(trimmed)
	trimmed = regexp.MustCompile(`(?i)`+sceneGarbageExp.String()).ReplaceAllString(trimmed, "")
	trimmed = strings.TrimSpace(trimmed)

	numericSuffix := ""
	if loc := numericTitleSuffixExp.FindStringSubmatchIndex(trimmed); loc != nil {
		numericSuffix = "." + trimmed[loc[2]:loc[3]]
		trimmed = strings.TrimRight(strings.TrimSpace(trimmed[:loc[0]]), ". ")
	}

	languages := []string{
		"English", "French", "Spanish", "German", "Italian", "Danish", "Dutch",
		"Japanese", "Cantonese", "Mandarin", "Russian", "Polish", "Vietnamese",
		"Nordic", "Swedish", "Norwegian", "Finnish", "Turkish", "Portuguese",
		"Flemish", "Greek", "Korean", "Hungarian", "Persian", "Bengali",
		"Bulgarian", "Brazilian", "Hebrew", "Czech", "Ukrainian", "Catalan",
		"Chinese", "Thai", "Hindi", "Tamil", "Arabic", "Estonian", "Icelandic",
		"Latvian", "Lithuanian", "Romanian", "Slovak", "Serbian",
	}
	for _, lang := range languages {
		re := regexp.MustCompile(`\b` + strings.ToUpper(lang))
		trimmed = re.ReplaceAllString(trimmed, "")
		trimmed = strings.TrimSpace(trimmed)
	}

	if idx := strings.Index(trimmed, "  "); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if idx := strings.Index(trimmed, ".."); idx >= 0 {
		trimmed = trimmed[:idx]
	}

	parts := strings.Split(trimmed, ".")
	var result strings.Builder
	previousAcronym := false

	for n, part := range parts {
		var nextPart string
		if n+1 < len(parts) {
			nextPart = parts[n+1]
		}

		_, isNum := strconv.Atoi(part)
		if len(part) == 1 && strings.ToLower(part) != "a" && isNum != nil {
			result.WriteString(part)
			result.WriteByte('.')
			previousAcronym = true
		} else if strings.ToLower(part) == "a" && (previousAcronym || len(nextPart) == 1) {
			result.WriteString(part)
			result.WriteByte('.')
			previousAcronym = true
		} else {
			if previousAcronym {
				result.WriteByte(' ')
				previousAcronym = false
			}
			result.WriteString(part)
			result.WriteByte(' ')
		}
	}

	cleaned := strings.TrimFunc(result.String(), unicode.IsSpace)
	if numericSuffix != "" {
		cleaned = strings.TrimSpace(cleaned + " " + numericSuffix)
	}
	return cleaned
}
