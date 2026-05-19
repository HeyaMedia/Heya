package parser

import (
	"path"
	"regexp"
	"strings"
	"strconv"
)

var knownFileSuffixes = []string{
	".mkv.part", ".zip.001", ".!qb", ".tmp",
	".mkv", ".mp4", ".avi", ".mov", ".m4v", ".wmv",
	".flac", ".m4a", ".mp3", ".aac", ".wav", ".ogg", ".opus",
	".rar", ".r00", ".r01", ".zip", ".7z", ".001",
	".nfo", ".sfv", ".srr",
	".jpg", ".jpeg", ".png", ".gif", ".webp",
	".lrc", ".epub", ".pdf",
}

var audioExtensions = map[string]bool{
	".flac": true, ".m4a": true, ".mp3": true, ".aac": true,
	".wav": true, ".ogg": true, ".opus": true, ".lrc": true,
}

var videoExtensions = map[string]bool{
	".mkv": true, ".mkv.part": true, ".mp4": true, ".avi": true,
	".mov": true, ".m4v": true, ".wmv": true,
}

var bookExtensions = map[string]bool{
	".epub": true, ".pdf": true,
}

func IsMediaExtension(ext string) bool {
	ext = strings.ToLower(ext)
	return audioExtensions[ext] || videoExtensions[ext] || bookExtensions[ext]
}

func MediaKindForExtension(ext string) SceneMediaKind {
	ext = strings.ToLower(ext)
	if audioExtensions[ext] {
		return MediaAudio
	}
	if videoExtensions[ext] {
		return MediaVideo
	}
	if bookExtensions[ext] {
		return MediaBook
	}
	return MediaUnknown
}

var audioSourceTokens = []string{"WEB", "CD", "VINYL", "TAPE", "CASSETTE"}
var audioReleaseTokens = []string{"SINGLE", "EP", "ALBUM"}
var audioCodecTokens = []string{"FLAC", "MP3", "AAC", "ALAC", "M4A", "WAV", "OGG", "OPUS"}
var audioRevisionTokens = []string{"PROPER", "REPACK", "REPACK2", "RERIP", "RERIP2", "REAL", "V2", "V3"}
var audioQualityPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:16|24|32)BIT\b`),
	regexp.MustCompile(`(?i)\b\d{2}(?:[.\-]\d)?-?KHZ\b`),
}

type statusPrefix struct {
	prefix string
	flag   string
}

var statusPrefixes = []statusPrefix{
	{prefix: "_FAILED_", flag: "failed"},
	{prefix: "_UNPACK_", flag: "unpack"},
}

var skippedSegmentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^storage$`),
	regexp.MustCompile(`(?i)^downloads?$`),
	regexp.MustCompile(`(?i)^movies$`),
	regexp.MustCompile(`(?i)^tv$`),
	regexp.MustCompile(`(?i)^audio$`),
	regexp.MustCompile(`(?i)^books$`),
	regexp.MustCompile(`(?i)^music$`),
	regexp.MustCompile(`(?i)^foreign$`),
	regexp.MustCompile(`(?i)^soulseek$`),
	regexp.MustCompile(`(?i)^incomplete$`),
	regexp.MustCompile(`(?i)^sample$`),
	regexp.MustCompile(`(?i)^subs?$`),
	regexp.MustCompile(`(?i)^season \d+$`),
	regexp.MustCompile(`(?i)^specials$`),
	regexp.MustCompile(`(?i)^cd\d+$`),
	regexp.MustCompile(`(?i)^disc \d+$`),
}

var videoMarkerRE = regexp.MustCompile(`(?i)\b(?:S\d{1,2}E\d{1,3}|E\d{2,3}|(?:19|20)\d{2}|\d{3,4}p|WEB[-.]?DL|WEBRIP|BLURAY|BDRIP|HDRIP|DVDRIP|UHD|AMZN|DSNP|NF|H[ .]?26[45]|X26[45])\b`)
var tvMarkerRE = regexp.MustCompile(`(?i)\b(?:S\d{1,2}(?:E\d{1,3})?|E\d{2,3}|\d{1,2}x\d{1,3}|Season[ ._\-]?\d+)\b`)
var counterMatchRE = regexp.MustCompile(`(?:\w)\.(\d{1,2})$`)

func PrepareSegment(segment string) PreparedSegment {
	var flags []string
	cleanedName := strings.TrimSpace(segment)
	ext := getKnownFileSuffix(cleanedName)

	if ext != "" {
		cleanedName = cleanedName[:len(cleanedName)-len(ext)]
	}

	for _, sp := range statusPrefixes {
		if strings.HasPrefix(cleanedName, sp.prefix) {
			flags = append(flags, sp.flag)
			cleanedName = cleanedName[len(sp.prefix):]
		}
	}

	if loc := counterMatchRE.FindStringIndex(cleanedName); loc != nil {
		matchStr := cleanedName[loc[0]:]
		flags = append(flags, "retry")
		cleanedName = cleanedName[:len(cleanedName)-len(matchStr)+1]
	}

	if idx := strings.Index(cleanedName, "  "); idx > 0 {
		left := cleanedName[:idx]
		right := strings.TrimSpace(cleanedName[idx:])
		hasSeasonRight := regexp.MustCompile(`(?i)S\d{1,2}E\d{1,3}`).MatchString(right)
		hasSeasonLeft := regexp.MustCompile(`(?i)S\d{1,2}E\d{1,3}`).MatchString(left)
		if hasSeasonRight && !hasSeasonLeft {
			cleanedName = right
		}
	}

	return PreparedSegment{
		RawName:     segment,
		CleanedName: strings.TrimSpace(cleanedName),
		Extension:   ext,
		Flags:       flags,
	}
}

func DetectStatus(segments []string, leafSegment PreparedSegment) StorageParseStatus {
	hasFailedSegment := false
	hasUnpackSegment := false
	for _, seg := range segments {
		if strings.HasPrefix(seg, "_FAILED_") {
			hasFailedSegment = true
		}
		if strings.HasPrefix(seg, "_UNPACK_") {
			hasUnpackSegment = true
		}
	}

	for _, f := range leafSegment.Flags {
		if f == "failed" {
			return StatusFailed
		}
	}
	if hasFailedSegment {
		return StatusFailed
	}

	for _, f := range leafSegment.Flags {
		if f == "unpack" {
			return StatusUnpack
		}
	}
	if hasUnpackSegment {
		return StatusUnpack
	}

	lowerSegments := make([]string, len(segments))
	for i, s := range segments {
		lowerSegments[i] = strings.ToLower(s)
	}

	for _, ls := range lowerSegments {
		if ls == "incomplete" {
			return StatusPartial
		}
	}
	if leafSegment.Extension == ".mkv.part" || leafSegment.Extension == ".tmp" || leafSegment.Extension == ".!qb" {
		return StatusPartial
	}

	return StatusReady
}

func InferMediaKind(segments []string, extension string, release *SceneReleaseParse) SceneMediaKind {
	if release != nil {
		return release.Media
	}

	if extension != "" && audioExtensions[extension] {
		return MediaAudio
	}
	if extension != "" && videoExtensions[extension] {
		return MediaVideo
	}
	if extension != "" && bookExtensions[extension] {
		return MediaBook
	}

	for _, seg := range segments {
		lower := strings.ToLower(seg)
		switch lower {
		case "audio", "music", "soulseek":
			return MediaAudio
		case "movies", "tv", "foreign":
			return MediaVideo
		case "books":
			return MediaBook
		}
	}

	return MediaUnknown
}

func InferSegmentMediaHint(segments []string, index int, prepared PreparedSegment) SceneMediaKind {
	scoped := segments[:index+1]
	inferred := InferMediaKind(scoped, prepared.Extension, nil)
	if inferred != MediaUnknown {
		return inferred
	}

	if LooksLikeVideoRelease(prepared.CleanedName) {
		if LooksLikeAudioRelease(prepared.CleanedName) {
			return MediaUnknown
		}
		return MediaVideo
	}

	return MediaUnknown
}

func ShouldSkipSegment(segment string) bool {
	for _, p := range skippedSegmentPatterns {
		if p.MatchString(segment) {
			return true
		}
	}
	return false
}

func LooksLikeVideoRelease(name string) bool {
	return videoMarkerRE.MatchString(name) || LooksLikeAnimeRelease(name)
}

func LooksLikeTvRelease(name string) bool {
	return tvMarkerRE.MatchString(name) || LooksLikeAnimeRelease(name)
}

func LooksLikeAudioRelease(name string) bool {
	audioRE := regexp.MustCompile(`(?i)\b(?:FLAC|MP3|AAC|ALAC|WAV|OGG|OPUS)\b`)
	videoCodecRE := regexp.MustCompile(`(?i)\b(?:x26[45]|H[. ]?26[45])\b`)
	return audioRE.MatchString(name) && !videoCodecRE.MatchString(name)
}

func LooksLikeAnimeRelease(name string) bool {
	animeRE := regexp.MustCompile(`(?i)^\[[^\]]+\].+?(?:\s|[._\-])-\s*\d{1,4}(?:v\d+)?(?:$|\s|\[|\()`)
	return animeRE.MatchString(name)
}

func NormalizeVideoCandidate(name string) NormalizedVideoCandidate {
	candidate := strings.TrimSpace(name)
	var animeGroup string
	var releaseHash string
	var versionFlags []string

	leadingGroupRE := regexp.MustCompile(`^\[([^\]]{1,32})\][\s._\-]*`)
	if m := leadingGroupRE.FindStringSubmatch(candidate); m != nil {
		groupToken := strings.TrimSpace(m[1])
		if isLikelyTaggedGroup(groupToken) {
			animeGroup = groupToken
			candidate = candidate[len(m[0]):]
		}
	}

	hashRE := regexp.MustCompile(`(?:[\s._\-]*(?:\[|\()([A-Fa-f0-9]{8})(?:\]|\)))\s*$`)
	for {
		m := hashRE.FindStringSubmatch(candidate)
		if m == nil {
			break
		}
		releaseHash = m[1]
		loc := hashRE.FindStringIndex(candidate)
		candidate = strings.TrimSpace(candidate[:loc[0]])
	}

	candidate = regexp.MustCompile(`\[\d{1,4}[-+]\d{1,4}\]`).ReplaceAllString(candidate, "")
	candidate = regexp.MustCompile(`(?i)\[(?:BDRip|DVDRip|WEBRip|Bluray|HDTV|HDRIP)[^\]]*\]`).ReplaceAllString(candidate, "")
	candidate = regexp.MustCompile(`(?i)\[(?:Dual Audio|Multi Audio|Fin|END|Complete|Batch|M2TS[^\]]*)\]`).ReplaceAllString(candidate, "")
	candidate = regexp.MustCompile(`\s{2,}`).ReplaceAllString(candidate, " ")
	candidate = strings.TrimSpace(candidate)

	versionRE := regexp.MustCompile(`(?i)(?:^|[\s._\-])v(\d{1,2})(?:$|[\s._\-])`)
	for _, m := range versionRE.FindAllStringSubmatch(candidate, -1) {
		if m[1] != "" {
			versionFlags = append(versionFlags, "v"+m[1])
		}
	}
	if len(versionFlags) > 0 {
		candidate = versionRE.ReplaceAllString(candidate, " ")
	}

	candidate = regexp.MustCompile(`\s{2,}`).ReplaceAllString(candidate, " ")
	candidate = strings.TrimSpace(candidate)

	animeEpisode := detectAnimeEpisodeNumber(candidate)
	var derivedTitle string
	if animeEpisode >= 0 {
		derivedTitle = deriveAnimeTitle(candidate)
	}

	return NormalizedVideoCandidate{
		Candidate:    candidate,
		AnimeGroup:   animeGroup,
		ReleaseHash:  releaseHash,
		AnimeEpisode: animeEpisode,
		DerivedTitle: derivedTitle,
		VersionFlags: dedupeFlags(versionFlags),
	}
}

func NormalizeInputPath(inputPath string) string {
	slashed := strings.ReplaceAll(inputPath, "\\", "/")
	return path.Clean(slashed)
}

func GetStorageRoot(segments []string) string {
	storageIndex := -1
	for i, seg := range segments {
		if strings.ToLower(seg) == "storage" {
			storageIndex = i
			break
		}
	}

	if storageIndex == -1 {
		if len(segments) > 0 {
			return segments[0]
		}
		return ""
	}

	if storageIndex+1 < len(segments) {
		return segments[storageIndex+1]
	}
	return ""
}

func GetCollection(segments []string) string {
	storageIndex := -1
	for i, seg := range segments {
		if strings.ToLower(seg) == "storage" {
			storageIndex = i
			break
		}
	}

	if storageIndex == -1 {
		if len(segments) > 1 {
			return segments[1]
		}
		return ""
	}

	if storageIndex+2 < len(segments) {
		return segments[storageIndex+2]
	}
	return ""
}

func ScoreVideoRelease(title, year, group string, resolution string, sourceCount int, videoCodec string, seasonCount, episodeCount int, releaseHash string) int {
	score := 0
	if title != "" {
		score++
	}
	if year != "" {
		score++
	}
	if group != "" {
		score++
	}
	if resolution != "" {
		score++
	}
	if sourceCount > 0 {
		score++
	}
	if videoCodec != "" {
		score++
	}
	if seasonCount > 0 {
		score++
	}
	if episodeCount > 0 {
		score += 2
	}
	if releaseHash != "" {
		score++
	}
	return score
}

func ScoreAudioRelease(title, year, group string, sourceCount, codecCount int, hasCatalog bool, releaseFlagCount, qualityFlagCount int) int {
	score := 0
	if title != "" {
		score++
	}
	if year != "" {
		score++
	}
	if group != "" {
		score++
	}
	if sourceCount > 0 {
		score++
	}
	if codecCount > 0 {
		score++
	}
	if hasCatalog {
		score++
	}
	if releaseFlagCount > 0 {
		score++
	}
	if qualityFlagCount > 0 {
		score++
	}
	return score
}

func IsLikelySceneGroup(token string) bool {
	normalized := regexp.MustCompile(`[^A-Za-z0-9_]`).ReplaceAllString(token, "")
	upper := strings.ToUpper(normalized)

	if normalized == "" || len(normalized) < 2 {
		return false
	}
	if !regexp.MustCompile(`[A-Za-z]`).MatchString(normalized) {
		return false
	}

	skipTokens := append(append(audioSourceTokens, audioReleaseTokens...), audioCodecTokens...)
	for _, t := range skipTokens {
		if upper == t {
			return false
		}
	}
	return true
}

func ExtractCatalogs(value string) []string {
	parenRE := regexp.MustCompile(`\(([^)]+)\)`)
	bracketRE := regexp.MustCompile(`\[([^\]]+)\]`)
	yearRE := regexp.MustCompile(`^(?:19|20)\d{2}$`)

	seen := map[string]bool{}
	var catalogs []string

	for _, matches := range parenRE.FindAllStringSubmatch(value, -1) {
		entry := strings.TrimSpace(matches[1])
		if entry != "" && !yearRE.MatchString(entry) && !seen[entry] {
			seen[entry] = true
			catalogs = append(catalogs, entry)
		}
	}
	for _, matches := range bracketRE.FindAllStringSubmatch(value, -1) {
		entry := strings.TrimSpace(matches[1])
		if entry != "" && !yearRE.MatchString(entry) && !seen[entry] {
			seen[entry] = true
			catalogs = append(catalogs, entry)
		}
	}

	return catalogs
}

func CollectLooseTokens(value string, tokens []string) []string {
	var result []string
	for _, token := range tokens {
		if HasLooseToken(value, token) {
			result = append(result, token)
		}
	}
	return result
}

func CollectPatternTokens(value string, patterns []*regexp.Regexp) []string {
	seen := map[string]bool{}
	var tokens []string

	for _, p := range patterns {
		for _, m := range p.FindAllString(value, -1) {
			if !seen[m] {
				seen[m] = true
				tokens = append(tokens, m)
			}
		}
	}
	return tokens
}

func RemoveLooseToken(value, token string) string {
	escaped := regexp.QuoteMeta(token)
	re := regexp.MustCompile(`(?i)(?:^|[\s._\-\[\]()])` + escaped + `(?:$|[\s._\-\[\]()])`)
	return re.ReplaceAllString(value, " ")
}

func NormalizeAudioTitle(value string) string {
	r := value
	r = regexp.MustCompile(`[\s._\-]*[._\-][\s._\-]*[._\-][\s._\-]*`).ReplaceAllString(r, " - ")
	r = strings.ReplaceAll(r, "_", " ")
	r = strings.ReplaceAll(r, ".", " ")
	r = regexp.MustCompile(`\s*-\s*`).ReplaceAllString(r, " - ")
	r = regexp.MustCompile(`\(\s*\)`).ReplaceAllString(r, " ")
	r = regexp.MustCompile(`\[\s*\]`).ReplaceAllString(r, " ")
	r = regexp.MustCompile(`\s{2,}`).ReplaceAllString(r, " ")
	r = regexp.MustCompile(`\s+-\s*$`).ReplaceAllString(r, "")
	r = strings.TrimLeft(r, "-")
	r = strings.TrimRight(r, "-")
	return strings.TrimSpace(r)
}

func HasLooseToken(value, token string) bool {
	escaped := regexp.QuoteMeta(token)
	re := regexp.MustCompile(`(?i)(?:^|[\s._\-\[\]()])` + escaped + `(?:$|[\s._\-\[\]()])`)
	return re.MatchString(value)
}

func CompactStringArray(values []string) []string {
	var result []string
	for _, v := range values {
		if v != "" {
			result = append(result, v)
		}
	}
	return result
}

func dedupeFlags(flags []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, f := range flags {
		lower := strings.ToLower(f)
		if !seen[lower] {
			seen[lower] = true
			result = append(result, lower)
		}
	}
	return result
}

func getKnownFileSuffix(name string) string {
	lower := strings.ToLower(name)
	for _, suffix := range knownFileSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return suffix
		}
	}
	return ""
}

func isLikelyTaggedGroup(token string) bool {
	if token == "" || len(token) > 32 {
		return false
	}
	if !regexp.MustCompile(`[A-Za-z]`).MatchString(token) {
		return false
	}
	return !regexp.MustCompile(`(?i)(?:1080p|720p|2160p|web|bluray|mkv|aac|flac)`).MatchString(token)
}

func detectAnimeEpisodeNumber(value string) int {
	simplified := strings.ReplaceAll(strings.ReplaceAll(value, ".", " "), "_", " ")
	re := regexp.MustCompile(`(?i)^(.+?)\s-\s*(\d{1,4})(?:v\d+)?(?:$|\s|\[|\()`)
	m := re.FindStringSubmatch(simplified)
	if m == nil || m[2] == "" {
		return -1
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return -1
	}
	return n
}

func deriveAnimeTitle(value string) string {
	simplified := strings.ReplaceAll(strings.ReplaceAll(value, ".", " "), "_", " ")
	re := regexp.MustCompile(`(?i)^(.+?)\s-\s*\d{1,4}(?:v\d+)?(?:$|\s|\[|\()`)
	m := re.FindStringSubmatch(simplified)
	if m == nil || m[1] == "" {
		return ""
	}
	result := regexp.MustCompile(`\s{2,}`).ReplaceAllString(m[1], " ")
	result = regexp.MustCompile(`\s+-\s*$`).ReplaceAllString(result, "")
	return strings.TrimSpace(result)
}

func escapeRegExp(value string) string {
	return regexp.QuoteMeta(value)
}
