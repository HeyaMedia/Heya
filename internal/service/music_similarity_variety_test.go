package service

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// disableSonicVariety pins the sonic tie-band to 0 for a test so the sonic DJ
// modes and seed radio keep their deterministic distance order (the variety
// shuffle is a no-op at band 0). Restores the production default on cleanup.
func disableSonicVariety(t *testing.T) {
	t.Helper()
	prev := sonicTieBand
	sonicTieBand = 0
	t.Cleanup(func() { sonicTieBand = prev })
}

func TestShuffleSonicBandsKeepsStandoutAndRotatesTies(t *testing.T) {
	key := func(f float64) float64 { return f }

	// A clear standout (own band) followed by a tight ~equal cluster.
	base := []float64{0.001, 0.100, 0.105, 0.110, 0.112}
	first := map[float64]bool{}
	for seed := int64(0); seed < 24; seed++ {
		items := append([]float64{}, base...)
		shuffleSonicBands(items, key, 0.02, rand.New(rand.NewSource(seed)))
		require.Equal(t, 0.001, items[0], "the standout must always lead")
		require.ElementsMatch(t, base, items, "shuffle must not add or drop items")
		// The cluster members never cross ahead of the standout.
		require.Greater(t, items[1], 0.05)
		first[items[1]] = true
	}
	require.Greater(t, len(first), 1, "the tie cluster should rotate across seeds")
}

func TestShuffleSonicBandsRespectsGapsAndNoOps(t *testing.T) {
	key := func(f float64) float64 { return f }

	// A gap larger than the band starts a new band, so order across the gap is
	// preserved even though each side may shuffle internally.
	items := []float64{0.10, 0.11, 0.50, 0.51}
	shuffleSonicBands(items, key, 0.02, rand.New(rand.NewSource(7)))
	require.ElementsMatch(t, []float64{0.10, 0.11}, items[:2], "first band stays first")
	require.ElementsMatch(t, []float64{0.50, 0.51}, items[2:], "second band stays second")

	// band <= 0 and a nil rng are both no-ops.
	ordered := []float64{0.1, 0.2, 0.3}
	noop := append([]float64{}, ordered...)
	shuffleSonicBands(noop, key, 0, rand.New(rand.NewSource(1)))
	require.Equal(t, ordered, noop)
	shuffleSonicBands(noop, key, 0.5, nil)
	require.Equal(t, ordered, noop)
}
