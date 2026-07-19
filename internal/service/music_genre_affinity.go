package service

import (
	"context"
	"fmt"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Genre-affinity plumbing for RadioRequest.GenreAffinity (music_radio.go).
// genre is currently read nowhere else in the mix/radio scoring path —
// albums.genres (text[], broad coverage) and track_facets.top_genres
// (jsonb [{name,score}], ~18% coverage) only fed browse shelves and LLM
// prose before this. A "genre profile" here is always a normalized
// distribution over genre name -> weight, summing to 1, built from both
// sources: album.genres contributes a flat weight per entry (no per-entry
// confidence in that source); top_genres contributes its classifier score
// (already 0..1).

// neutralGenreOverlap is the overlap assigned to a candidate with NO genre
// signal anywhere (neither album.genres nor track_facets.top_genres). It is
// deliberately not 0: with only ~18% top_genres coverage and album.genres
// itself sometimes empty, treating "no data" as "zero overlap" would let a
// high genre_affinity empty out the result for a large slice of the
// catalog. 0.35 keeps such candidates below any real partial match (which
// starts above 0 and can reach 1) while still letting them outrank a real
// disjoint-genre candidate at the score-adjustment stage — a car-crash-proof
// middle ground, not a claim that the track is 35% "on genre".
const neutralGenreOverlap = 0.35

// genreAffinityScoreScale and genreAffinityDropThreshold implement
// RadioRequest.GenreAffinity's contract:
//
//	score += genreAffinity * genreAffinityScoreScale * candidate.GenreOverlap
//
// Invariants (verified in music_recommendations_test.go):
//   - genreAffinity <= 0: the term above is skipped entirely (rankMusicRecommendationPool
//     guards it with `if genreAffinity > 0`) and buildMusicRecommendationPool
//     never even computes GenreOverlap — ranking is byte-identical to the
//     pre-genre-affinity code path. True additive no-op, not just "small".
//   - Any fixed genreAffinity > 0: the adjustment is strictly monotonic
//     increasing in GenreOverlap (it's a positive scale times overlap), so a
//     higher-overlap candidate never scores below an equal-base-score,
//     lower-overlap one.
//   - genreAffinity == 1: genreAffinityScoreScale (4.0) is large relative to
//     the other additive bonuses in this function (0.35..2.5), so a
//     zero-overlap candidate (adjustment 0) ranks below any positive-overlap
//     candidate of a similar base score — the overlap term dominates modest
//     score gaps.
//   - genreAffinity >= genreAffinityDropThreshold (0.75 — the start of the
//     Mix Builder slider's "Strict" band, so Strict actually enforces):
//     candidates with a REAL zero overlap (seed and candidate both have genre
//     data but share nothing — GenreOverlap == 0 exactly) are dropped from
//     the ranked pool entirely, but only once at least `limit` overlapping
//     candidates (GenreOverlap > 0, which includes neutralGenreOverlap)
//     remain in the eligible pool — a sparse pool still fills the mix rather
//     than coming back short. Additionally, when at least `limit` candidates
//     have a real positive overlap, the no-genre-data crowd (neutral
//     stand-in) is dropped too — neutral exists to protect sparse pools, not
//     to dilute a queue that has genuine matches to spare.
const (
	genreAffinityScoreScale    = 4.0
	genreAffinityDropThreshold = 0.75
)

// genreHistogramOverlap is the histogram-intersection kernel between two
// normalized genre distributions: sum, over every genre both sides carry,
// of the smaller of the two weights. Because both inputs sum to 1, the
// result is naturally in [0,1] — 1 for identical distributions, 0 for
// disjoint ones, and a smooth partial value in between. Either map may be
// nil/empty, in which case the overlap is 0 (the caller substitutes
// neutralGenreOverlap for "no candidate genre data" before this is called;
// an empty seed profile means genre_affinity is skipped upstream entirely).
func genreHistogramOverlap(seed, candidate map[string]float64) float64 {
	if len(seed) == 0 || len(candidate) == 0 {
		return 0
	}
	var overlap float64
	for genre, sw := range seed {
		if cw, ok := candidate[genre]; ok {
			if cw < sw {
				overlap += cw
			} else {
				overlap += sw
			}
		}
	}
	switch {
	case overlap < 0:
		return 0
	case overlap > 1:
		return 1
	default:
		return overlap
	}
}

// normalizeGenreWeights divides every entry by the sum so the result is a
// proper distribution (sums to 1) for genreHistogramOverlap. Returns nil for
// an empty or all-zero input — callers treat that as "no genre profile".
func normalizeGenreWeights(raw map[string]float64) map[string]float64 {
	if len(raw) == 0 {
		return nil
	}
	var total float64
	for _, w := range raw {
		total += w
	}
	if total <= 0 {
		return nil
	}
	out := make(map[string]float64, len(raw))
	for genre, w := range raw {
		if w <= 0 {
			continue
		}
		out[genre] = w / total
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// candidateGenreProfiles batch-fetches one normalized genre profile per
// track id in a single query (TrackGenreWeights) — never per-candidate
// round trips. Tracks absent from the result map have no genre signal at
// all from either source; buildMusicRecommendationPool substitutes
// neutralGenreOverlap for those.
func (a *App) candidateGenreProfiles(ctx context.Context, trackIDs []int64) (map[int64]map[string]float64, error) {
	out := make(map[int64]map[string]float64)
	if len(trackIDs) == 0 {
		return out, nil
	}
	rows, err := sqlc.New(a.db).TrackGenreWeights(ctx, trackIDs)
	if err != nil {
		return nil, err
	}
	raw := make(map[int64]map[string]float64)
	for _, r := range rows {
		if r.Genre == "" {
			continue
		}
		m := raw[r.TrackID]
		if m == nil {
			m = map[string]float64{}
			raw[r.TrackID] = m
		}
		m[r.Genre] += r.Weight
	}
	for id, m := range raw {
		if norm := normalizeGenreWeights(m); norm != nil {
			out[id] = norm
		}
	}
	return out, nil
}

// artistGenreProfile is the whole-discography genre-frequency signal for an
// artist-kind radio seed (ArtistGenreWeights): how many of the artist's
// albums carry each genre, unnormalized — the caller normalizes after
// merging with other seed sources so no single source dominates purely by
// having a bigger raw count.
func (a *App) artistGenreProfile(ctx context.Context, artistIDs []int64) (map[string]float64, error) {
	if len(artistIDs) == 0 {
		return nil, nil
	}
	rows, err := sqlc.New(a.db).ArtistGenreWeights(ctx, artistIDs)
	if err != nil {
		return nil, err
	}
	raw := make(map[string]float64, len(rows))
	for _, r := range rows {
		if r.Genre == "" {
			continue
		}
		raw[r.Genre] += r.Weight
	}
	return raw, nil
}

// radioSeedGenreProfile builds the union genre profile for a radio build's
// seed(s):
//   - artistIDs are the RESOLVED artists-table ids of the seed tracks
//     (BuildRadio's artistIDsForTracks output — never raw request ids, which
//     historically carried media-item ids and profiled the wrong artists).
//     They pull the whole discography's genre frequency (artistGenreProfile)
//     — an artist's range shouldn't collapse into whatever one
//     representative seed track's album carries.
//   - every resolved seed track contributes its own track+album genre signal
//     via candidateGenreProfiles — the same function candidates are scored
//     against, so seed and candidate weights are on one consistent scale.
//
// Each source is normalized on its own before merging so an artist with 40
// albums doesn't drown out a single resolved seed track purely by raw count;
// the merged total is renormalized once at the end.
func (a *App) radioSeedGenreProfile(ctx context.Context, artistIDs []int64, seedIDs []int64) (map[string]float64, error) {
	raw := map[string]float64{}
	merge := func(src map[string]float64) {
		for genre, w := range src {
			raw[genre] += w
		}
	}

	if len(artistIDs) > 0 {
		profile, err := a.artistGenreProfile(ctx, artistIDs)
		if err != nil {
			return nil, fmt.Errorf("artist genre profile: %w", err)
		}
		merge(normalizeGenreWeights(profile))
	}

	if len(seedIDs) > 0 {
		perTrack, err := a.candidateGenreProfiles(ctx, seedIDs)
		if err != nil {
			return nil, fmt.Errorf("seed track genre profile: %w", err)
		}
		for _, profile := range perTrack {
			merge(profile) // already normalized per track
		}
	}

	return normalizeGenreWeights(raw), nil
}
