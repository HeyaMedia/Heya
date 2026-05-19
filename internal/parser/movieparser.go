package parser

import (
	"fmt"

	"github.com/karbowiak/kura/internal/parser/video"
)

func canParseMovie(prepared PreparedSegment, mediaHint SceneMediaKind) bool {
	if mediaHint != MediaUnknown && mediaHint != MediaVideo {
		return false
	}
	if LooksLikeTvRelease(prepared.CleanedName) {
		return false
	}
	return LooksLikeVideoRelease(prepared.CleanedName)
}

func parseMovie(prepared PreparedSegment) *SceneReleaseParse {
	normalized := NormalizeVideoCandidate(prepared.CleanedName)
	parsed := video.FilenameParseMovie(normalized.Candidate)

	var sources []string
	for _, s := range parsed.Sources {
		sources = append(sources, string(s))
	}

	editionFlags := parsed.Edition.Flags()
	flags := append([]string{}, prepared.Flags...)
	flags = append(flags, normalized.VersionFlags...)
	flags = append(flags, editionFlags...)

	if parsed.Complete {
		flags = append(flags, "complete")
	}
	if parsed.Multi {
		flags = append(flags, "multi")
	}
	if parsed.Revision.Version > 1 {
		if parsed.Revision.Version == 2 {
			flags = append(flags, "proper")
		} else {
			flags = append(flags, fmt.Sprintf("revision-%d", parsed.Revision.Version))
		}
	}
	if parsed.Revision.Real > 0 {
		flags = append(flags, fmt.Sprintf("real-%d", parsed.Revision.Real))
	}

	title := trimStr(parsed.Title)
	group := normalized.AnimeGroup
	if group == "" {
		group = parsed.Group
	}

	score := ScoreVideoRelease(
		title, parsed.Year, group,
		string(parsed.Resolution),
		len(sources), string(parsed.VideoCodec),
		0, 0,
		normalized.ReleaseHash,
	)

	hasStrongSignal := parsed.Resolution != "" || parsed.VideoCodec != "" || len(sources) > 0 || parsed.Year != "" || normalized.AnimeGroup != ""

	if title == "" || score < 4 || !hasStrongSignal {
		return nil
	}

	codecs := CompactStringArray([]string{string(parsed.VideoCodec), string(parsed.AudioCodec)})

	codec := ""
	if len(codecs) > 0 {
		codec = codecs[0]
	}
	source := ""
	if len(sources) > 0 {
		source = sources[0]
	}

	return &SceneReleaseParse{
		Strategy:       StrategyVideoFilenameParser,
		RawName:        prepared.RawName,
		NormalizedName: normalized.Candidate,
		Media:          MediaVideo,
		Title:          title,
		Year:           parsed.Year,
		Group:          group,
		ReleaseHash:    normalized.ReleaseHash,
		Source:         source,
		Sources:        sources,
		Codec:          codec,
		Codecs:         codecs,
		Resolution:     string(parsed.Resolution),
		Flags:          dedupeFlags(flags),
		Seasons:        []int{},
		Episodes:       []int{},
		IsTv:           false,
		Score:          score,
	}
}
