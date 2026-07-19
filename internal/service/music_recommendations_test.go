package service

import (
	"strings"
	"testing"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

func recommendationCandidate(trackID, artistID int64, title string, score float64, known bool) musicRecommendationCandidate {
	return musicRecommendationCandidate{
		Track: sqlc.ListArtistTopTracksForMixRow{
			TrackID: trackID, ArtistID: artistID, TrackTitle: title, Duration: 180,
		},
		Score: score,
		State: musicCandidateState{TrackKnown: known, Affinity: map[bool]float64{true: 8}[known]},
	}
}

func TestMusicAffinityIgnoresSkipsAndKeepsCompletionWeak(t *testing.T) {
	if strings.Contains(musicAffinityCTE, "listened_seconds") {
		t.Fatal("affinity must not infer taste from skip/listen position")
	}
	if !strings.Contains(musicAffinityCTE, "pe.completed") || !strings.Contains(musicAffinityCTE, "LEAST(2.0") {
		t.Fatal("completed plays must be the only bounded implicit signal")
	}
	for _, table := range []string{"user_track_ratings", "user_album_ratings", "user_artist_ratings"} {
		if !strings.Contains(musicVetoFilter, table) {
			t.Fatalf("veto filter missing %s", table)
		}
	}
}

func TestSelectMusicRecommendationsBlendsFamiliarAndFresh(t *testing.T) {
	var candidates []musicRecommendationCandidate
	for i := int64(1); i <= 6; i++ {
		candidates = append(candidates, recommendationCandidate(i, i, "known", float64(20-i), true))
	}
	for i := int64(7); i <= 10; i++ {
		candidates = append(candidates, recommendationCandidate(i, i, "fresh", float64(20-i), false))
	}
	out := selectMusicRecommendations(candidates, 10, recommendForYou, nil)
	if len(out) != 10 {
		t.Fatalf("expected 10 tracks, got %d", len(out))
	}
	known := 0
	for _, track := range out[:5] {
		if track.TrackID <= 6 {
			known++
		}
	}
	if known != 3 {
		t.Fatalf("first blend block should be 3 familiar + 2 fresh, got %d familiar", known)
	}
}

func TestSelectMusicRecommendationsDedupesSongVersions(t *testing.T) {
	candidates := []musicRecommendationCandidate{
		recommendationCandidate(1, 10, "Same Song", 5, false),
		recommendationCandidate(2, 10, "Same Song (Remastered)", 4, false),
		recommendationCandidate(3, 20, "Other", 3, false),
	}
	out := selectMusicRecommendations(candidates, 3, recommendRadio, nil)
	if len(out) != 2 || out[0].TrackID != 1 || out[1].TrackID != 3 {
		t.Fatalf("recording version dedupe failed: %#v", out)
	}
}

func TestMusicCandidateModeEligibility(t *testing.T) {
	now := time.Now()
	fresh := recommendationCandidate(1, 1, "fresh", 1, false)
	if !musicCandidateEligible(fresh, recommendDiscovery, now) {
		t.Fatal("unknown track should be eligible for discovery")
	}
	known := recommendationCandidate(2, 2, "known", 1, true)
	known.State.LastPlayed = now.Add(-90 * 24 * time.Hour)
	if musicCandidateEligible(known, recommendDiscovery, now) {
		t.Fatal("known track must not enter discovery")
	}
	if !musicCandidateEligible(known, recommendRediscover, now) {
		t.Fatal("old positive track should enter rediscovery")
	}
	known.State.LastPlayed = now.Add(-7 * 24 * time.Hour)
	if musicCandidateEligible(known, recommendRediscover, now) {
		t.Fatal("recent track must not enter rediscovery")
	}
}

func TestMusicRotationJitterIsStableAndScoped(t *testing.T) {
	a := musicRotationJitter(1, 2, "for_you", 10)
	if a != musicRotationJitter(1, 2, "for_you", 10) {
		t.Fatal("same daily key must be stable")
	}
	if a == musicRotationJitter(1, 2, "for_you", 11) {
		t.Fatal("day bucket should rotate candidate jitter")
	}
}

// TestRankMusicRecommendationPoolGenreAffinityZeroIsNoOp covers invariant
// (a) from genreAffinityScoreScale's doc comment: genre_affinity == 0 must
// leave ranking byte-identical regardless of GenreOverlap. Flip GenreOverlap
// from best-to-worst to worst-to-best between two otherwise-identical runs —
// if the knob were doing anything at 0, the output order would change.
func TestRankMusicRecommendationPoolGenreAffinityZeroIsNoOp(t *testing.T) {
	build := func(overlaps [3]float64) []musicRecommendationCandidate {
		pool := []musicRecommendationCandidate{
			recommendationCandidate(1, 1, "a", 10, false),
			recommendationCandidate(2, 2, "b", 9, false),
			recommendationCandidate(3, 3, "c", 8, false),
		}
		for i := range pool {
			pool[i].GenreOverlap = overlaps[i]
		}
		return pool
	}

	out := rankMusicRecommendationPool(build([3]float64{1.0, 0.0, 0.5}), 1, recommendRadio, 3, nil, 0, 0)
	flipped := rankMusicRecommendationPool(build([3]float64{0.0, 1.0, 0.5}), 1, recommendRadio, 3, nil, 0, 0)

	if len(out) != 3 || len(flipped) != 3 {
		t.Fatalf("expected 3 tracks both runs, got %d and %d", len(out), len(flipped))
	}
	for i := range out {
		if out[i].TrackID != flipped[i].TrackID {
			t.Fatalf("genre_affinity=0 must ignore GenreOverlap entirely: %v vs %v", out, flipped)
		}
	}
	if out[0].TrackID != 1 || out[1].TrackID != 2 || out[2].TrackID != 3 {
		t.Fatalf("expected base-score order 1,2,3, got %v", out)
	}
}

// TestRankMusicRecommendationPoolHighGenreAffinityFavorsOverlap covers
// invariant (b): a high genre_affinity ranks an overlapping candidate above
// a zero-overlap one even when the zero-overlap candidate has a modestly
// higher base score.
func TestRankMusicRecommendationPoolHighGenreAffinityFavorsOverlap(t *testing.T) {
	pool := []musicRecommendationCandidate{
		recommendationCandidate(1, 1, "zero-overlap, higher base score", 10.5, false),
		recommendationCandidate(2, 2, "overlapping, slightly lower base score", 10.0, false),
	}
	pool[0].GenreOverlap = 0
	pool[1].GenreOverlap = 0.6

	out := rankMusicRecommendationPool(pool, 1, recommendRadio, 2, nil, 0, 1.0)
	if len(out) != 2 || out[0].TrackID != 2 {
		t.Fatalf("genre_affinity=1 should rank the overlapping candidate first, got %v", out)
	}
}

// TestRankMusicRecommendationPoolDropsZeroOverlapWhenPoolIsRich covers the
// genre_affinity >= genreAffinityDropThreshold drop rule: a real zero-overlap
// candidate (genre data present, shares nothing) is dropped once enough
// overlapping candidates remain to still fill the requested limit.
func TestRankMusicRecommendationPoolDropsZeroOverlapWhenPoolIsRich(t *testing.T) {
	pool := []musicRecommendationCandidate{recommendationCandidate(1, 1, "zero", 20, false)}
	pool[0].GenreOverlap = 0
	pool[0].GenreDataKnown = true
	for i := int64(2); i <= 5; i++ {
		c := recommendationCandidate(i, i, "overlap", float64(15-i), false)
		c.GenreOverlap = 0.4
		c.GenreDataKnown = true
		pool = append(pool, c)
	}

	out := rankMusicRecommendationPool(pool, 1, recommendRadio, 3, nil, 0, 0.95)
	for _, tr := range out {
		if tr.TrackID == 1 {
			t.Fatalf("zero-overlap candidate should be dropped once enough overlapping candidates satisfy the limit: %v", out)
		}
	}
}

// TestRankMusicRecommendationPoolStrictShedsNeutralWhenRealMatchesSuffice:
// candidates with NO genre data (neutral stand-in overlap) are also dropped
// under strict genre affinity once enough REAL positive-overlap candidates
// can fill the limit on their own.
func TestRankMusicRecommendationPoolStrictShedsNeutralWhenRealMatchesSuffice(t *testing.T) {
	pool := []musicRecommendationCandidate{recommendationCandidate(1, 1, "neutral", 20, false)}
	pool[0].GenreOverlap = neutralGenreOverlap // no genre data anywhere
	for i := int64(2); i <= 6; i++ {
		c := recommendationCandidate(i, i, "real", float64(15-i), false)
		c.GenreOverlap = 0.5
		c.GenreDataKnown = true
		pool = append(pool, c)
	}

	out := rankMusicRecommendationPool(pool, 1, recommendRadio, 3, nil, 0, 0.8)
	for _, tr := range out {
		if tr.TrackID == 1 {
			t.Fatalf("neutral no-data candidate should be shed when real matches can fill the limit: %v", out)
		}
	}

	// Converse: with too few real matches, the neutral candidate survives.
	sparse := []musicRecommendationCandidate{pool[0], pool[1]}
	out = rankMusicRecommendationPool(sparse, 1, recommendRadio, 3, nil, 0, 0.8)
	if len(out) != 2 {
		t.Fatalf("sparse real-match pool must keep the neutral candidate: %v", out)
	}
}

// TestRankMusicRecommendationPoolKeepsZeroOverlapWhenPoolIsSparse is the
// converse of the drop test: dropping must not empty the result when there
// aren't enough overlapping candidates to fill the limit.
func TestRankMusicRecommendationPoolKeepsZeroOverlapWhenPoolIsSparse(t *testing.T) {
	pool := []musicRecommendationCandidate{
		recommendationCandidate(1, 1, "zero", 10, false),
		recommendationCandidate(2, 2, "overlap", 9, false),
	}
	pool[1].GenreOverlap = 0.4

	out := rankMusicRecommendationPool(pool, 1, recommendRadio, 3, nil, 0, 0.95)
	if len(out) != 2 {
		t.Fatalf("sparse pool must still fill from the zero-overlap candidate: %v", out)
	}
}

func TestGenreHistogramOverlap(t *testing.T) {
	seed := map[string]float64{"rock": 0.6, "pop": 0.4}
	if got := genreHistogramOverlap(seed, map[string]float64{"rock": 0.6, "pop": 0.4}); got < 0.999 {
		t.Fatalf("identical distributions should overlap ~1, got %v", got)
	}
	if got := genreHistogramOverlap(seed, map[string]float64{"metal": 1.0}); got != 0 {
		t.Fatalf("disjoint genres should overlap 0, got %v", got)
	}
	if got := genreHistogramOverlap(seed, nil); got != 0 {
		t.Fatalf("nil candidate profile should overlap 0, got %v", got)
	}
	got := genreHistogramOverlap(seed, map[string]float64{"rock": 0.3, "jazz": 0.7})
	if got <= 0 || got >= 1 {
		t.Fatalf("partial overlap should be strictly between 0 and 1, got %v", got)
	}
}

func TestNormalizeGenreWeights(t *testing.T) {
	norm := normalizeGenreWeights(map[string]float64{"rock": 3, "pop": 1})
	var total float64
	for _, w := range norm {
		total += w
	}
	if total < 0.999 || total > 1.001 {
		t.Fatalf("normalized weights should sum to 1, got %v (%v)", total, norm)
	}
	if norm["rock"] <= norm["pop"] {
		t.Fatalf("relative weighting should survive normalization: %v", norm)
	}
	if normalizeGenreWeights(nil) != nil {
		t.Fatal("empty input should normalize to nil")
	}
	if normalizeGenreWeights(map[string]float64{"x": 0}) != nil {
		t.Fatal("all-zero input should normalize to nil")
	}
}
