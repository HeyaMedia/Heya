package heyamedia

import (
	"context"
	"fmt"

	gen "github.com/karbowiak/heya/clients/heyamedia"
)

// Skip-segment lookups against heya.media's community aggregation
// (TheIntroDB + SkipMe.db + AniSkip). heya.media returns every candidate
// with per-source provenance and the runtime each marker was authored
// against; picking a winner (duration gate) is the caller's job — only
// we know the actual file runtime.

// SegmentCandidate is one community marker candidate, milliseconds
// throughout. EndMs nil means "runs to end of media". DurationMs is the
// runtime the marker was authored against (0 = unknown — TheIntroDB
// does its release-cut matching server-side from the duration we pass).
type SegmentCandidate struct {
	Type        string // intro | recap | credits | preview | commercial
	StartMs     int64
	EndMs       *int64
	DurationMs  int64
	Submissions int
	Source      string // theintrodb | skipmedb | aniskip
}

// MovieSegments fetches segment candidates for a movie. providerID is a
// heya.media path id like "tmdb:603" or "imdb:tt0133093". found=false
// is a genuine "the community has nothing (yet)".
func (p *HeyaProvider) MovieSegments(ctx context.Context, providerID string, durationMs int64) ([]SegmentCandidate, bool, error) {
	params := &gen.MovieSegmentsParams{}
	if durationMs > 0 {
		params.DurationMs = &durationMs
	}
	resp, err := p.client.gen.MovieSegmentsWithResponse(ctx, providerID, params)
	if err != nil {
		return nil, false, fmt.Errorf("heya movie segments: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, false, upstreamErr("movie segments", resp.StatusCode(), resp.Body)
	}
	return mapSegmentDTOs(resp.JSON200.Segments), resp.JSON200.Found, nil
}

// EpisodeSegments fetches segment candidates for one TV episode.
func (p *HeyaProvider) EpisodeSegments(ctx context.Context, providerID string, season, episode int, durationMs int64) ([]SegmentCandidate, bool, error) {
	params := &gen.TvEpisodeSegmentsParams{}
	if durationMs > 0 {
		params.DurationMs = &durationMs
	}
	resp, err := p.client.gen.TvEpisodeSegmentsWithResponse(ctx, providerID, int64(season), int64(episode), params)
	if err != nil {
		return nil, false, fmt.Errorf("heya episode segments: %w", err)
	}
	if resp.JSON200 == nil {
		return nil, false, upstreamErr("episode segments", resp.StatusCode(), resp.Body)
	}
	return mapSegmentDTOs(resp.JSON200.Segments), resp.JSON200.Found, nil
}

func mapSegmentDTOs(in *[]gen.SegmentDTO) []SegmentCandidate {
	if in == nil {
		return nil
	}
	out := make([]SegmentCandidate, 0, len(*in))
	for _, d := range *in {
		c := SegmentCandidate{
			Type:    string(d.Type),
			StartMs: d.StartMs,
			EndMs:   d.EndMs,
			Source:  string(d.Source),
		}
		if d.DurationMs != nil {
			c.DurationMs = *d.DurationMs
		}
		if d.Submissions != nil {
			c.Submissions = int(*d.Submissions)
		}
		out = append(out, c)
	}
	return out
}

// SegmentProviderID picks the id heya.media segment lookups key on, in
// the databases' own precedence order (tmdb > imdb > tvdb). externalIDs
// is a media_items.external_ids map. Empty when the item carries none
// of the usable ids.
func SegmentProviderID(externalIDs map[string]string) string {
	for _, key := range []string{"tmdb", "imdb", "tvdb"} {
		if v := externalIDs[key]; v != "" {
			return key + ":" + v
		}
	}
	return ""
}
