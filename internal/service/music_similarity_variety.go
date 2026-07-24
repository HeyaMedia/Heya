package service

import (
	"math/rand"
	"time"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Sonic recommendations — seed radio, "play more of this", and every DJ mode
// that ranks by CLAP-embedding distance — used to be fully deterministic: the
// nearest neighbour of a seed never changes, so the same seed always produced
// the same "next track" and the same station. In a densely analysed library
// that is a poor experience, because the top neighbours are not a clear
// winner followed by also-rans: they are a big cluster of ~equally-similar
// tracks. Measured on prod, the 40 nearest of a typical seed all sit inside a
// ~0.01–0.03 cosine-distance sliver (i.e. all "~98% similar"). Any of them is
// an equally good pick.
//
// These helpers keep the quality bar — a genuinely closer track is never
// demoted below a genuinely worse one — while rotating which of the near-ties
// leads, so repeated builds differ. A real standout (its own distance band)
// still wins every time; only interchangeable ties are shuffled.

// sonicTieBand is the cosine-distance window, measured from each band's leader,
// within which candidates count as interchangeable near-ties and are shuffled
// together. Calibrated against real CLAP distances (top neighbours cluster
// inside ~0.01–0.03). It is a var, not a const, so ordering tests can set it to
// 0 for the old deterministic behaviour. Tunable: larger = more variety /
// looser "best", smaller = tighter to the exact nearest.
var sonicTieBand = 0.02

// explorationSeed yields fresh entropy for on-demand rotation. Overridable in
// tests for reproducible ordering. Deliberately NOT used by the day-stable
// home "Mixes for You" rails (which must not churn on every refetch) — only by
// seed radio / play-more / the DJ, which are meant to vary every invocation.
var explorationSeed = func() int64 { return time.Now().UnixNano() }

func newExplorationRng() *rand.Rand {
	return rand.New(rand.NewSource(explorationSeed())) //nolint:gosec // rotation, not crypto
}

// shuffleSonicBands reorders items in place. Items must already be sorted
// best-first by key (lower = more similar, e.g. cosine distance). Consecutive
// items whose key is within `band` of the band's leader form one band and are
// shuffled together; a larger gap starts a new band. Boundaries anchor on the
// band leader (not the previous item), so a gentle slope of distances does not
// chain the whole list into a single band. rng==nil or band<=0 is a no-op,
// which is how ordering tests keep the original deterministic order.
func shuffleSonicBands[T any](items []T, key func(T) float64, band float64, rng *rand.Rand) {
	if rng == nil || band <= 0 || len(items) < 2 {
		return
	}
	start := 0
	for start < len(items) {
		leader := key(items[start])
		end := start + 1
		for end < len(items) && key(items[end])-leader <= band {
			end++
		}
		if n := end - start; n > 1 {
			rng.Shuffle(n, func(i, j int) {
				items[start+i], items[start+j] = items[start+j], items[start+i]
			})
		}
		start = end
	}
}

// scoredTrackID pairs a track id with the closeness key that ranked it, so the
// DJ candidate generators can band-shuffle their sonic neighbours before
// dropping the distances.
type scoredTrackID struct {
	id   int64
	dist float64
}

// bandShuffleTrackIDs band-shuffles a distance-ranked (best-first) candidate
// list with fresh per-call entropy and returns just the ids, order preserved
// through the shuffle. Used by the sonic DJ modes so the same seed does not
// always insert the identical next track.
func bandShuffleTrackIDs(scored []scoredTrackID) []int64 {
	shuffleSonicBands(scored, func(s scoredTrackID) float64 { return s.dist }, sonicTieBand, newExplorationRng())
	ids := make([]int64, len(scored))
	for i, s := range scored {
		ids[i] = s.id
	}
	return ids
}

// bandShuffleAnalyzedLead band-shuffles the leading run of ids whose distance
// is non-nil — the analysed, sonically-ranked neighbours a NULLS-LAST query
// puts first — and appends the remaining ids (the unanalysed tail, already in
// the query's fallback order) untouched. Used by same-artist DJ modes whose
// pool mixes analysed and unanalysed tracks.
func bandShuffleAnalyzedLead(ids []int64, dists []*float64) []int64 {
	lead := 0
	for lead < len(dists) && dists[lead] != nil {
		lead++
	}
	scored := make([]scoredTrackID, lead)
	for i := 0; i < lead; i++ {
		scored[i] = scoredTrackID{id: ids[i], dist: *dists[i]}
	}
	shuffleSonicBands(scored, func(s scoredTrackID) float64 { return s.dist }, sonicTieBand, newExplorationRng())
	out := make([]int64, 0, len(ids))
	for _, s := range scored {
		out = append(out, s.id)
	}
	return append(out, ids[lead:]...)
}

// scoredMixNeighbor pairs a playable-track projection with its sonic distance
// so seed radio can band-shuffle nearest neighbours before they are scored.
type scoredMixNeighbor struct {
	row  sqlc.ListArtistTopTracksForMixRow
	dist float64
}
