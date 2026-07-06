package parser

import (
	"fmt"

	"github.com/karbowiak/heya/internal/parser/video"
)

func canParseTv(prepared PreparedSegment, mediaHint SceneMediaKind) bool {
	if mediaHint != MediaUnknown && mediaHint != MediaVideo {
		return false
	}
	// Under an anime-tagged path, let bracket-less "Series - 24 - Title" files
	// through — parseTv still requires an actual episode/strong signal to emit a
	// release, so a bare folder segment falls out on its own.
	if prepared.AnimeContext {
		return true
	}
	return LooksLikeTvRelease(prepared.CleanedName)
}

func parseTv(prepared PreparedSegment) *SceneReleaseParse {
	normalized := NormalizeVideoCandidate(prepared.CleanedName)
	parsed := video.FilenameParseShow(normalized.Candidate)

	var sources []string
	for _, s := range parsed.Sources {
		sources = append(sources, string(s))
	}

	editionFlags := parsed.Edition.Flags()

	seasons := make([]int, len(parsed.Seasons))
	copy(seasons, parsed.Seasons)

	episodes := make([]int, len(parsed.EpisodeNumbers))
	copy(episodes, parsed.EpisodeNumbers)

	var absoluteEpisodes []int

	switch {
	case prepared.AnimeContext && normalized.AnimeEpisode >= 0:
		// Absolute-numbered anime ("Series - 24 - Title"): record the absolute
		// number and clear season/episode. season.go can misread a trailing
		// title digit as a season ("Yamato 2 - 24" -> 2x24); we ignore that and
		// let the read path resolve absolute -> real season/episode via
		// tv_episodes.absolute_number. Kept out of Seasons so it never collides
		// with genuine season-0 specials.
		absoluteEpisodes = []int{normalized.AnimeEpisode}
		seasons = nil
		episodes = nil
	case len(episodes) == 0 && normalized.AnimeEpisode >= 0:
		episodes = []int{normalized.AnimeEpisode}
	}

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

	title := normalized.DerivedTitle
	if title == "" {
		title = parsed.Title
	}
	title = trimStr(title)

	group := normalized.AnimeGroup
	if group == "" {
		group = parsed.Group
	}

	score := ScoreVideoRelease(
		title, parsed.Year, group,
		string(parsed.Resolution),
		len(sources), string(parsed.VideoCodec),
		len(seasons), len(episodes)+len(absoluteEpisodes),
		normalized.ReleaseHash,
	)

	// A clean anime library file ("Series - 24 - Title.mkv") carries no
	// scene tokens and scores only title+episode (=3), below the cutoff. The
	// {anidb-…} tag on its folder is itself an unambiguous identity signal, so
	// let it stand in for the missing tokens.
	if prepared.AnimeContext && (len(episodes) > 0 || len(absoluteEpisodes) > 0) {
		score += 2
	}

	hasStrongSignal := parsed.Resolution != "" || parsed.VideoCodec != "" || len(sources) > 0 || len(episodes) > 0 || len(absoluteEpisodes) > 0 || normalized.AnimeGroup != ""

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
		Strategy:         StrategyVideoFilenameParser,
		RawName:          prepared.RawName,
		NormalizedName:   normalized.Candidate,
		Media:            MediaVideo,
		Title:            title,
		Year:             parsed.Year,
		Group:            group,
		ReleaseHash:      normalized.ReleaseHash,
		Source:           source,
		Sources:          sources,
		Codec:            codec,
		Codecs:           codecs,
		Resolution:       string(parsed.Resolution),
		Flags:            dedupeFlags(flags),
		Seasons:          seasons,
		Episodes:         episodes,
		AbsoluteEpisodes: absoluteEpisodes,
		IsTv:             true,
		Score:            score,
	}
}

func trimStr(s string) string {
	result := s
	for len(result) > 0 && (result[0] == ' ' || result[len(result)-1] == ' ') {
		if result[0] == ' ' {
			result = result[1:]
		}
		if len(result) > 0 && result[len(result)-1] == ' ' {
			result = result[:len(result)-1]
		}
	}
	return result
}
