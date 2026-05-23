package parser

import (
	"regexp"
	"strconv"
	"strings"
)

func canParseMusic(_ PreparedSegment, mediaHint SceneMediaKind) bool {
	return mediaHint == MediaAudio || mediaHint == MediaUnknown
}

func parseMusic(prepared PreparedSegment) *SceneReleaseParse {
	if curated := parseMusicCurated(prepared); curated != nil {
		return curated
	}
	return parseMusicScene(prepared)
}

var (
	curatedTrackRejectRE = regexp.MustCompile(`^\d{2,4}\s*-\s*`)
	curatedYearRE        = regexp.MustCompile(`^(?:19|20)\d{2}$`)
	trackFourDigitRE     = regexp.MustCompile(`^(\d{2})(\d{2})\s*-\s*(.+)$`)
	trackTwoDigitRE      = regexp.MustCompile(`^(\d{2})\s*-\s*(.+)$`)
	artistDisambigRE     = regexp.MustCompile(`^(.*?)\s*\(([^()]+)\)\s*$`)
	sceneMarkerRE        = regexp.MustCompile(`(?i)\b(?:FLAC|MP3|AAC|ALAC|WAV|OGG|OPUS|WEB|WEBFLAC|WEBMP3|WEBAAC|WEBALAC|BLURAY|VINYL|CASSETTE|BIT|KHZ|REPACK|RERIP|PROPER|xpost)\b`)
	sceneCatalogRE       = regexp.MustCompile(`[\[(][A-Z][A-Z0-9_]{2,}[\])]`)
	sceneYearBracketRE   = regexp.MustCompile(`[\[(](?:19|20)\d{2}[\])]`)
)

func parseMusicCurated(prepared PreparedSegment) *SceneReleaseParse {
	name := strings.TrimSpace(prepared.CleanedName)
	if name == "" || !strings.Contains(name, " - ") {
		return nil
	}
	if curatedTrackRejectRE.MatchString(name) {
		return nil
	}

	if structured := parseCuratedStructured(name); structured != nil {
		structured.RawName = prepared.RawName
		structured.NormalizedName = prepared.CleanedName
		structured.Flags = append([]string{}, prepared.Flags...)
		return structured
	}

	if prepared.Extension != "" {
		return nil
	}
	if sceneMarkerRE.MatchString(name) || sceneCatalogRE.MatchString(name) || sceneYearBracketRE.MatchString(name) {
		return nil
	}

	sparse := parseCuratedSparse(name)
	if sparse == nil {
		return nil
	}
	sparse.RawName = prepared.RawName
	sparse.NormalizedName = prepared.CleanedName
	sparse.Flags = append([]string{}, prepared.Flags...)
	return sparse
}

func parseCuratedStructured(name string) *SceneReleaseParse {
	parts := strings.SplitN(name, " - ", 4)
	if len(parts) < 4 {
		return nil
	}
	artist := strings.TrimSpace(parts[0])
	kindToken := strings.TrimSpace(parts[1])
	year := strings.TrimSpace(parts[2])
	album := strings.TrimSpace(parts[3])

	kind := strings.ToLower(kindToken)
	if kind != "album" && kind != "ep" && kind != "single" {
		return nil
	}
	if !curatedYearRE.MatchString(year) {
		return nil
	}
	if artist == "" || album == "" {
		return nil
	}

	return &SceneReleaseParse{
		Strategy:    StrategyMusicCurated,
		Media:       MediaAudio,
		Title:       album,
		Artist:      artist,
		Album:       album,
		ReleaseKind: kind,
		Year:        year,
		Sources:     []string{},
		Codecs:      []string{},
		Seasons:     []int{},
		Episodes:    []int{},
		Score:       50,
	}
}

func parseCuratedSparse(name string) *SceneReleaseParse {
	idx := strings.Index(name, " - ")
	if idx < 0 {
		return nil
	}
	artist := strings.TrimSpace(name[:idx])
	album := strings.TrimSpace(name[idx+3:])
	if artist == "" || album == "" {
		return nil
	}

	return &SceneReleaseParse{
		Strategy: StrategyMusicCurated,
		Media:    MediaAudio,
		Title:    album,
		Artist:   artist,
		Album:    album,
		Sources:  []string{},
		Codecs:   []string{},
		Seasons:  []int{},
		Episodes: []int{},
		Score:    25,
	}
}

func splitArtistDisambiguator(folderName string) (artist, disambiguation string) {
	name := strings.TrimSpace(folderName)
	if m := artistDisambigRE.FindStringSubmatch(name); m != nil {
		return strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
	}
	return name, ""
}

func parseTrackFilename(basename string) (disc, track int, title string, ok bool) {
	name := basename
	if idx := strings.LastIndex(name, "."); idx > 0 {
		name = name[:idx]
	}
	name = strings.TrimSpace(name)

	if m := trackFourDigitRE.FindStringSubmatch(name); m != nil {
		d, _ := strconv.Atoi(m[1])
		t, _ := strconv.Atoi(m[2])
		if d == 0 {
			d = 1
		}
		return d, t, strings.TrimSpace(m[3]), true
	}
	if m := trackTwoDigitRE.FindStringSubmatch(name); m != nil {
		t, _ := strconv.Atoi(m[1])
		return 1, t, strings.TrimSpace(m[2]), true
	}
	return 0, 0, "", false
}

func isTrackExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".flac", ".m4a", ".mp3", ".aac", ".wav", ".ogg", ".opus":
		return true
	}
	return false
}

func parseMusicScene(prepared PreparedSegment) *SceneReleaseParse {
	workingName := prepared.CleanedName
	workingName = regexp.MustCompile(`(?i)\bWEBFLAC\b`).ReplaceAllString(workingName, "WEB FLAC")
	workingName = regexp.MustCompile(`(?i)\bWEBMP3\b`).ReplaceAllString(workingName, "WEB MP3")
	workingName = regexp.MustCompile(`(?i)\bWEBAAC\b`).ReplaceAllString(workingName, "WEB AAC")
	workingName = regexp.MustCompile(`(?i)\bWEBALAC\b`).ReplaceAllString(workingName, "WEB ALAC")

	flags := append([]string{}, prepared.Flags...)

	metaPrefixRE := regexp.MustCompile(`^\d{2,3}[-_. ]+`)
	if m := metaPrefixRE.FindString(workingName); m != "" {
		workingName = workingName[len(m):]
	}

	xpostRE := regexp.MustCompile(`(?i)(?:[-_. ]xpost)$`)
	if xpostRE.MatchString(workingName) {
		flags = append(flags, "xpost")
		workingName = xpostRE.ReplaceAllString(workingName, "")
	}

	groupRE := regexp.MustCompile(`(?:[-.])([A-Za-z0-9_]{2,})$`)
	var group string
	if m := groupRE.FindStringSubmatch(workingName); m != nil {
		groupToken := m[1]
		if IsLikelySceneGroup(groupToken) {
			group = groupToken
			workingName = workingName[:len(workingName)-len(m[0])]
		}
	}

	releaseKindSource := workingName

	yearRE := regexp.MustCompile(`\b(?:19|20)\d{2}\b`)
	yearMatches := yearRE.FindAllStringIndex(workingName, -1)
	var year string
	if len(yearMatches) > 0 {
		last := yearMatches[len(yearMatches)-1]
		year = workingName[last[0]:last[1]]
		workingName = workingName[:last[0]] + " " + workingName[last[1]:]
	}

	catalogs := ExtractCatalogs(workingName)
	var catalog string
	if len(catalogs) > 0 {
		catalog = catalogs[0]
	}

	titleSource := workingName
	for _, cat := range catalogs {
		escaped := escapeRegExp(cat)
		titleSource = regexp.MustCompile(`(?i)\(`+escaped+`\)`).ReplaceAllString(titleSource, " ")
		titleSource = regexp.MustCompile(`(?i)\[`+escaped+`\]`).ReplaceAllString(titleSource, " ")
	}

	sources := CollectLooseTokens(titleSource, audioSourceTokens)
	releaseKinds := collectReleaseKinds(releaseKindSource)
	revisionFlags := lowerAll(CollectLooseTokens(titleSource, audioRevisionTokens))
	codecs := CollectLooseTokens(titleSource, audioCodecTokens)
	qualityFlags := lowerAll(CollectPatternTokens(titleSource, audioQualityPatterns))

	removeTokens := make([]string, 0)
	removeTokens = append(removeTokens, sources...)
	for _, rk := range releaseKinds {
		removeTokens = append(removeTokens, strings.ToUpper(rk))
	}
	for _, rf := range revisionFlags {
		removeTokens = append(removeTokens, strings.ToUpper(rf))
	}
	removeTokens = append(removeTokens, codecs...)

	for _, token := range removeTokens {
		titleSource = RemoveLooseToken(titleSource, token)
	}

	for _, qf := range qualityFlags {
		titleSource = regexp.MustCompile(`(?i)`+regexp.QuoteMeta(qf)).ReplaceAllString(titleSource, " ")
	}

	title := NormalizeAudioTitle(titleSource)

	score := ScoreAudioRelease(
		title, year, group,
		len(sources), len(codecs),
		catalog != "",
		len(releaseKinds)+len(revisionFlags),
		len(qualityFlags),
	)

	if title == "" || score < 3 || (year == "" && group == "" && len(sources) == 0 && len(codecs) == 0 && catalog == "") {
		return nil
	}

	normalizedFlags := dedupeFlags(append(append(append(flags, releaseKinds...), revisionFlags...), qualityFlags...))

	source := ""
	if len(sources) > 0 {
		source = sources[0]
	}
	codec := ""
	compactCodecs := CompactStringArray(codecs)
	if len(compactCodecs) > 0 {
		codec = compactCodecs[0]
	}

	return &SceneReleaseParse{
		Strategy:       StrategyAudioHeuristic,
		RawName:        prepared.RawName,
		NormalizedName: prepared.CleanedName,
		Media:          MediaAudio,
		Title:          title,
		Year:           year,
		Group:          group,
		Source:         source,
		Sources:        sources,
		Codec:          codec,
		Codecs:         codecs,
		Catalog:        catalog,
		Flags:          normalizedFlags,
		Seasons:        []int{},
		Episodes:       []int{},
		IsTv:           false,
		Score:          score,
	}
}

func collectReleaseKinds(value string) []string {
	suffixToken := `(?:WEB|CD|VINYL|TAPE|CASSETTE|FLAC|MP3|AAC|ALAC|M4A|WAV|OGG|OPUS|(?:19|20)\d{2})`
	patterns := []struct {
		token   string
		pattern *regexp.Regexp
	}{
		{"single", regexp.MustCompile(`(?i)(?:^|[\s._\-])SINGLE(?:$|[\s._\-]+` + suffixToken + `)`)},
		{"ep", regexp.MustCompile(`(?i)(?:^|[\s._\-])EP(?:$|[\s._\-]+` + suffixToken + `)`)},
		{"album", regexp.MustCompile(`(?i)(?:^|[\s._\-])ALBUM(?:$|[\s._\-]+` + suffixToken + `)`)},
	}

	var result []string
	for _, p := range patterns {
		if p.pattern.MatchString(value) {
			result = append(result, p.token)
		}
	}
	return result
}

func lowerAll(ss []string) []string {
	result := make([]string, len(ss))
	for i, s := range ss {
		result[i] = strings.ToLower(s)
	}
	return result
}
