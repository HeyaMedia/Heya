package service

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

func TestSamplerAllowanceLadder(t *testing.T) {
	cases := map[int]int{
		0:                       3,
		samplerColdSignal - 1:   3,
		samplerColdSignal:       1,
		samplerSparseSignal - 1: 1,
		samplerSparseSignal:     0,
		samplerSparseSignal * 5: 0,
	}
	for signal, want := range cases {
		if got := samplerAllowance(signal); got != want {
			t.Errorf("samplerAllowance(%d) = %d, want %d", signal, got, want)
		}
	}
}

func samplerPoolFixture(n int, artists int) []sqlc.ListArtistTopTracksForMixRow {
	pool := make([]sqlc.ListArtistTopTracksForMixRow, 0, n)
	for i := 0; i < n; i++ {
		pool = append(pool, sqlc.ListArtistTopTracksForMixRow{
			TrackID:    int64(i + 1),
			ArtistID:   int64(i%artists + 1),
			TrackTitle: fmt.Sprintf("track-%d", i+1),
		})
	}
	return pool
}

func TestAssembleSamplerMixRespectsExcludeAndCaps(t *testing.T) {
	pool := samplerPoolFixture(60, 10)
	exclude := map[int64]bool{1: true, 2: true, 31: true}
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic test rng

	out := assembleSamplerMix(pool, 24, exclude, rng)
	if len(out) != 24 {
		t.Fatalf("expected a full 24-track mix from a 60-track pool, got %d", len(out))
	}
	artistCap := max(3, 24/6)
	counts := map[int64]int{}
	seen := map[int64]bool{}
	for _, tr := range out {
		if exclude[tr.TrackID] {
			t.Fatalf("excluded track %d leaked into the sampler mix", tr.TrackID)
		}
		if seen[tr.TrackID] {
			t.Fatalf("duplicate track %d in sampler mix", tr.TrackID)
		}
		seen[tr.TrackID] = true
		counts[tr.ArtistID]++
	}
	for artist, n := range counts {
		if n > artistCap {
			t.Fatalf("artist %d has %d tracks, above the %d cap", artist, n, artistCap)
		}
	}
}

func TestAssembleSamplerMixPullsFromDeepHalf(t *testing.T) {
	// Pool of 40: head = tracks 1-20 (popular), tail = 21-40 (deep cuts).
	// The interleave reserves ~1 in 4 slots for the deep half.
	pool := samplerPoolFixture(40, 40)
	rng := rand.New(rand.NewSource(7)) //nolint:gosec // deterministic test rng
	out := assembleSamplerMix(pool, 20, map[int64]bool{}, rng)

	deep := 0
	for _, tr := range out {
		if tr.TrackID > 20 {
			deep++
		}
	}
	if deep == 0 {
		t.Fatal("sampler mix never reached into the deep half of the pool")
	}
	if deep > len(out)/2 {
		t.Fatalf("deep-cut share too large: %d of %d", deep, len(out))
	}
}

func TestAssembleSamplerMixDeterministicPerSeed(t *testing.T) {
	pool := samplerPoolFixture(50, 12)
	a := assembleSamplerMix(pool, 15, map[int64]bool{}, rand.New(rand.NewSource(42))) //nolint:gosec // deterministic test rng
	b := assembleSamplerMix(pool, 15, map[int64]bool{}, rand.New(rand.NewSource(42))) //nolint:gosec // deterministic test rng
	if len(a) != len(b) {
		t.Fatalf("same seed produced different lengths: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].TrackID != b[i].TrackID {
			t.Fatalf("same seed diverged at slot %d: %d vs %d", i, a[i].TrackID, b[i].TrackID)
		}
	}
	c := assembleSamplerMix(pool, 15, map[int64]bool{}, rand.New(rand.NewSource(43))) //nolint:gosec // deterministic test rng
	same := len(a) == len(c)
	if same {
		for i := range a {
			if a[i].TrackID != c[i].TrackID {
				same = false
				break
			}
		}
	}
	if same {
		t.Fatal("different seeds produced an identical mix — rotation entropy is dead")
	}
}

func TestAssembleSamplerMixEmptyPool(t *testing.T) {
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // deterministic test rng
	if out := assembleSamplerMix(nil, 20, map[int64]bool{}, rng); len(out) != 0 {
		t.Fatalf("empty pool should produce an empty mix, got %d tracks", len(out))
	}
}
