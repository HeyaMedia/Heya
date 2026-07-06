package parser

import (
	"sort"
	"strings"
)

type parserEntry struct {
	media    SceneMediaKind
	canParse func(PreparedSegment, SceneMediaKind) bool
	parse    func(PreparedSegment) *SceneReleaseParse
}

var parsers = []parserEntry{
	{media: MediaVideo, canParse: canParseTv, parse: parseTv},
	{media: MediaVideo, canParse: canParseMovie, parse: parseMovie},
	{media: MediaAudio, canParse: canParseMusic, parse: parseMusic},
	{media: MediaBook, canParse: canParseBook, parse: parseBook},
}

func ParseStoragePath(inputPath string) ParsedStorageEntry {
	normalizedPath := NormalizeInputPath(inputPath)
	segments := splitSegments(normalizedPath)

	var basename string
	if len(segments) > 0 {
		basename = segments[len(segments)-1]
	} else {
		basename = normalizedPath
	}

	leafSegment := PrepareSegment(basename)
	forcedHint := MediaUnknown
	if leafSegment.Extension != "" {
		forcedHint = MediaKindForExtension(leafSegment.Extension)
	}
	releaseCandidate := findBestReleaseCandidate(segments, forcedHint)
	media := InferMediaKind(segments, leafSegment.Extension, releaseFromCandidate(releaseCandidate))

	var entryType StorageEntryType
	if leafSegment.Extension != "" {
		entryType = EntryFile
	} else {
		entryType = EntryDirectory
	}

	var release *SceneReleaseParse
	var releaseSegment string
	if releaseCandidate != nil {
		release = releaseCandidate.release
		releaseSegment = releaseCandidate.segment
	}

	if release != nil {
		// Embedded provider IDs live in the release's own segment, the filename,
		// or the movie/show's immediate parent folder — the ubiquitous
		// "Title (Year) {tmdb-123}/Title (Year).mkv" form parks the id on the
		// folder while the leaf file (which wins the candidate tie) carries none.
		// Scan those three, not the whole path, so an unrelated ancestor dir's id
		// can't bleed in.
		idParts := []string{basename}
		if releaseSegment != "" && releaseSegment != basename {
			idParts = append(idParts, releaseSegment)
		}
		if entryType == EntryFile && len(segments) >= 2 {
			if parent := segments[len(segments)-2]; parent != basename && parent != releaseSegment {
				idParts = append(idParts, parent)
			}
		}
		idBlob := strings.Join(idParts, " ")
		release.ImdbID, release.TmdbID, release.TvdbID = ParseProviderIDs(idBlob)
		release.AnidbID, release.AnilistID, release.MalID = ParseAnimeIDs(idBlob)
	}

	if release != nil && release.Strategy == StrategyMusicCurated {
		if releaseCandidate.index > 0 {
			if _, disambig := splitArtistDisambiguator(segments[releaseCandidate.index-1]); disambig != "" {
				release.ArtistDisambiguation = disambig
			}
		}
		if entryType == EntryFile && releaseCandidate.index < len(segments)-1 && isTrackExtension(leafSegment.Extension) {
			if disc, track, title, ok := parseTrackFilename(basename); ok {
				release.DiscNumber = disc
				release.TrackNumber = track
				release.TrackTitle = title
				release.HasTrackInfo = true
			}
		}
	}

	return ParsedStorageEntry{
		InputPath:      inputPath,
		NormalizedPath: normalizedPath,
		Basename:       basename,
		StorageRoot:    GetStorageRoot(segments),
		Collection:     GetCollection(segments),
		EntryType:      entryType,
		Extension:      leafSegment.Extension,
		Status:         DetectStatus(segments, leafSegment),
		Media:          media,
		Release:        release,
		ReleaseSegment: releaseSegment,
	}
}

func ParseStoragePaths(inputPaths []string) []ParsedStorageEntry {
	results := make([]ParsedStorageEntry, len(inputPaths))
	for i, p := range inputPaths {
		results[i] = ParseStoragePath(p)
	}
	return results
}

func ParseSceneReleaseName(name string, mediaHint SceneMediaKind) *SceneReleaseParse {
	prepared := PrepareSegment(name)
	return parsePreparedRelease(prepared, mediaHint)
}

type releaseCandidate struct {
	release *SceneReleaseParse
	segment string
	index   int
}

func findBestReleaseCandidate(segments []string, forcedHint SceneMediaKind) *releaseCandidate {
	var best *releaseCandidate

	animeContext := PathLooksLikeAnime(segments)

	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if seg == "" || ShouldSkipSegment(seg) {
			continue
		}

		prepared := PrepareSegment(seg)
		if prepared.CleanedName == "" {
			continue
		}
		prepared.AnimeContext = animeContext

		hint := forcedHint
		if hint == MediaUnknown {
			hint = InferSegmentMediaHint(segments, i, prepared)
		}
		release := parsePreparedRelease(prepared, hint)
		if release == nil {
			continue
		}

		if best == nil || release.Score > best.release.Score || (release.Score == best.release.Score && i > best.index) {
			best = &releaseCandidate{
				release: release,
				segment: seg,
				index:   i,
			}
		}
	}

	return best
}

func parsePreparedRelease(prepared PreparedSegment, mediaHint SceneMediaKind) *SceneReleaseParse {
	var candidates []*SceneReleaseParse

	for _, p := range parsers {
		if !p.canParse(prepared, mediaHint) {
			continue
		}
		result := p.parse(prepared)
		if result != nil {
			candidates = append(candidates, result)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	return candidates[0]
}

func splitSegments(path string) []string {
	parts := strings.Split(path, "/")
	var result []string
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func releaseFromCandidate(rc *releaseCandidate) *SceneReleaseParse {
	if rc == nil {
		return nil
	}
	return rc.release
}
