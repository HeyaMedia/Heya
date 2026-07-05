package worker

import (
	"encoding/binary"
	"math"
	"testing"
)

// hashPoint deterministically maps an arbitrary integer id to a
// spread-out uint32 "chromaprint point" value. Multiplying by a large odd
// constant and XORing a second one gives avalanche-like mixing — nearby
// ids don't produce nearby (or bit-similar) outputs — so distinct id
// ranges behave like unrelated audio without any reliance on math/rand.
func hashPoint(id int) uint32 {
	h := uint32(id) * 2654435761
	return h ^ 0x9E3779B9
}

func noisePoints(base, n int) []uint32 {
	out := make([]uint32, n)
	for i := 0; i < n; i++ {
		out[i] = hashPoint(base + i)
	}
	return out
}

func concatPoints(parts ...[]uint32) []uint32 {
	var out []uint32
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestFindSharedRegionDetectsRunAtDifferentOffsets(t *testing.T) {
	const runLen = 200 // 200 * ~0.1238s ≈ 24.8s
	run := make([]uint32, runLen)
	for i := range run {
		run[i] = hashPoint(i)
	}

	// a and b share `run` verbatim but at different offsets, padded with
	// noise drawn from disjoint id ranges (100000s vs 300000s) so nothing
	// outside the run can coincidentally look alike.
	const aBefore, aAfter = 50, 50
	const bBefore, bAfter = 80, 50
	a := concatPoints(noisePoints(100000, aBefore), run, noisePoints(200000, aAfter))
	b := concatPoints(noisePoints(300000, bBefore), run, noisePoints(400000, bAfter))

	start, end, ok := findSharedRegion(a, b)
	if !ok {
		t.Fatal("expected a shared region to be found")
	}
	wantStart := float64(aBefore) * chromaprintPointDurationSecs
	wantEnd := float64(aBefore+runLen) * chromaprintPointDurationSecs
	if math.Abs(start-wantStart) > 1.0 {
		t.Errorf("start = %.3fs, want ~%.3fs", start, wantStart)
	}
	if math.Abs(end-wantEnd) > 1.0 {
		t.Errorf("end = %.3fs, want ~%.3fs", end, wantEnd)
	}
}

func TestFindSharedRegionShortRunRejectedByIntroBounds(t *testing.T) {
	const runLen = 80 // ~9.9s — under the 15s intro floor
	run := make([]uint32, runLen)
	for i := range run {
		run[i] = hashPoint(i)
	}
	a := concatPoints(noisePoints(1000, 30), run)
	b := concatPoints(noisePoints(2000, 30), run)

	start, end, ok := findSharedRegion(a, b)
	if !ok {
		t.Fatal("expected findSharedRegion to detect the short run")
	}
	if length := end - start; length >= introMinSecs {
		t.Fatalf("test setup bug: run length %.2fs is not actually under the intro floor", length)
	}
	if _, _, accepted := acceptIntroRegion(start, end); accepted {
		t.Errorf("a %.2fs region should be rejected by the %vs intro floor", end-start, introMinSecs)
	}
}

func TestFindSharedRegionHammingTolerance(t *testing.T) {
	const n = 200
	base := make([]uint32, n)
	for i := range base {
		base[i] = hashPoint(i)
	}

	// XORing every point by a fixed mask leaves the Hamming distance
	// between a[i] and b[i] exactly popcount(mask), regardless of the
	// underlying value (a^(a^mask) == mask). Index 0 is left as an exact
	// duplicate ("anchor") so the discovery phase — which matches on raw
	// value proximity (±2), not Hamming distance — can find the shift=0
	// alignment; the Hamming check under test only governs the walk phase
	// that follows.
	mutate := func(mask uint32) []uint32 {
		out := make([]uint32, n)
		for i, v := range base {
			if i == 0 {
				out[i] = v
				continue
			}
			out[i] = v ^ mask
		}
		return out
	}

	const mask6 = 0x3F // popcount 6 — at the tolerance boundary, must still match
	within := mutate(mask6)
	start, end, ok := findSharedRegion(base, within)
	if !ok {
		t.Fatal("expected a region with <=6 differing bits per point")
	}
	if length := end - start; length < float64(n-5)*chromaprintPointDurationSecs {
		t.Errorf("6-bit-differing points should still match across nearly the whole run, got %.2fs", length)
	}

	const mask10 = 0x3FF // popcount 10 — past the tolerance, must not match
	beyond := mutate(mask10)
	start2, end2, ok2 := findSharedRegion(base, beyond)
	// The unmutated anchor at index 0 still yields a trivial one-point
	// match, so a region is still found — the assertion is that it stays
	// tiny (just the anchor) instead of spanning the mutated run.
	if !ok2 {
		t.Fatal("expected the anchor point's trivial match to still register")
	}
	if length := end2 - start2; length >= float64(n/2)*chromaprintPointDurationSecs {
		t.Errorf("10-bit-differing points should not sustain a long match, got %.2fs", length)
	}
}

// buildGapTestPair returns two equal-length point sequences that are
// identical (Hamming 0, trivially discoverable at shift=0) everywhere
// except a dissimilar stretch [gapStart, gapStart+gapLen) where b is
// XORed by a heavily-differing mask.
func buildGapTestPair(totalPoints, gapStart, gapLen int) (a, b []uint32) {
	a = make([]uint32, totalPoints)
	b = make([]uint32, totalPoints)
	for i := 0; i < totalPoints; i++ {
		v := hashPoint(i)
		a[i] = v
		if i >= gapStart && i < gapStart+gapLen {
			b[i] = v ^ 0xFFFF // popcount 16 — far past the tolerance
		} else {
			b[i] = v
		}
	}
	return a, b
}

func TestFindSharedRegionGapMerging(t *testing.T) {
	const before, after = 100, 100
	// A merged region spans from the first to the last matching point
	// INCLUSIVE of the tolerated gap — the gap itself is a real (if
	// non-matching) stretch of the shared segment, not something excised
	// from it — so the merged length covers the whole before+gap+after run.
	wantHalfLen := float64(before) * chromaprintPointDurationSecs

	// ~2.6s gap (21 points) — comfortably inside the 3.5s tolerance, the
	// two halves must merge into one region spanning the full run.
	const mergeGap = 20
	wantMergedLen := float64(before+mergeGap+after) * chromaprintPointDurationSecs
	a, b := buildGapTestPair(before+mergeGap+after, before, mergeGap)
	start, end, ok := findSharedRegion(a, b)
	if !ok {
		t.Fatal("expected a region")
	}
	if got := end - start; math.Abs(got-wantMergedLen) > 1.0 {
		t.Errorf("a ~2.6s gap should merge into one %.2fs region, got %.2fs", wantMergedLen, got)
	}

	// ~5.1s gap (41 points) — past the 3.5s tolerance, the run splits;
	// findSharedRegion returns only the longest piece, which should be
	// about one half's length, not the combined length.
	const splitGap = 40
	wantWronglyMergedLen := float64(before+splitGap+after) * chromaprintPointDurationSecs
	a2, b2 := buildGapTestPair(before+splitGap+after, before, splitGap)
	start2, end2, ok2 := findSharedRegion(a2, b2)
	if !ok2 {
		t.Fatal("expected a region")
	}
	got2 := end2 - start2
	if math.Abs(got2-wantHalfLen) > 1.0 {
		t.Errorf("a ~5.1s gap should split into two ~%.2fs regions, got %.2fs", wantHalfLen, got2)
	}
	if got2 >= wantWronglyMergedLen-1.0 {
		t.Errorf("a ~5.1s gap must not merge into the full %.2fs region, got %.2fs", wantWronglyMergedLen, got2)
	}
}

func TestAcceptIntroRegionBounds(t *testing.T) {
	cases := []struct {
		end    float64
		wantOK bool
	}{
		{14.9, false},
		{15.0, true},
		{120.0, true},
		{120.1, false},
	}
	for _, c := range cases {
		if _, _, ok := acceptIntroRegion(0, c.end); ok != c.wantOK {
			t.Errorf("acceptIntroRegion(0, %v) ok=%v, want %v", c.end, ok, c.wantOK)
		}
	}
}

func TestAcceptIntroRegionSnapsNearZeroStart(t *testing.T) {
	if start, _, ok := acceptIntroRegion(5.0, 30.0); !ok || start != 0 {
		t.Errorf("start<=5s should snap to 0, got start=%v ok=%v", start, ok)
	}
	if start, _, ok := acceptIntroRegion(5.1, 30.0); !ok || start != 5.1 {
		t.Errorf("start>5s should not snap, got start=%v ok=%v", start, ok)
	}
}

func TestAcceptCreditsRegionBounds(t *testing.T) {
	cases := []struct {
		end    float64
		wantOK bool
	}{
		{14.9, false},
		{15.0, true},
		{450.0, true},
		{450.1, false},
	}
	for _, c := range cases {
		if _, _, ok := acceptCreditsRegion(0, c.end); ok != c.wantOK {
			t.Errorf("acceptCreditsRegion(0, %v) ok=%v, want %v", c.end, ok, c.wantOK)
		}
	}
}

func TestIntroWindowSecs(t *testing.T) {
	if _, _, ok := introWindowSecs(59); ok {
		t.Error("files under 60s should be skipped")
	}
	if start, dur, ok := introWindowSecs(200); !ok || start != 0 || dur != 50 {
		t.Errorf("200s file: got start=%v dur=%v ok=%v, want 0/50/true (25%% of 200)", start, dur, ok)
	}
	if _, dur, ok := introWindowSecs(10000); !ok || dur != 600 {
		t.Errorf("long file should cap the intro window at 600s, got %v ok=%v", dur, ok)
	}
}

func TestTailWindowSecs(t *testing.T) {
	if _, _, ok := tailWindowSecs(59); ok {
		t.Error("files under 60s should be skipped")
	}
	// 10% of 1000s + 120s = 220s.
	if start, dur, ok := tailWindowSecs(1000); !ok || dur != 220 || start != 780 {
		t.Errorf("1000s file: got start=%v dur=%v ok=%v, want start=780 dur=220", start, dur, ok)
	}
	// Long file: capped at 360s.
	if start, dur, ok := tailWindowSecs(10000); !ok || dur != 360 || start != 9640 {
		t.Errorf("long file should cap the tail window at 360s, got start=%v dur=%v ok=%v", start, dur, ok)
	}
}

func TestParseChromaprintRaw(t *testing.T) {
	want := []uint32{0x00000000, 0x12345678, 0xFFFFFFFF, 0xDEADBEEF}
	buf := make([]byte, len(want)*4)
	for i, v := range want {
		binary.LittleEndian.PutUint32(buf[i*4:], v)
	}
	got, err := parseChromaprintRaw(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d points, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("point %d = %#x, want %#x", i, got[i], want[i])
		}
	}
}

func TestParseChromaprintRawRejectsPartialTrailingBytes(t *testing.T) {
	if _, err := parseChromaprintRaw([]byte{1, 2, 3}); err == nil {
		t.Error("expected an error for a byte length that isn't a multiple of 4")
	}
}

func TestParseBlackDetectIntervals(t *testing.T) {
	stderr := `ffmpeg version 6.0 Copyright (c) 2000-2023 the FFmpeg developers
  built with Apple clang version 14.0.3
Input #0, matroska,webm, from 'movie.mkv':
  Duration: 00:08:00.00, start: 0.000000, bitrate: 5000 kb/s
Stream #0:0: Video: h264, yuv420p, 1920x1080, 23.98 fps
[blackdetect @ 0x600002a1c000] black_start:12.5 black_end:14.2 black_duration:1.7
frame=  100 fps= 25 q=-0.0 size=N/A time=00:00:04.16 bitrate=N/A speed=25.9x
[blackdetect @ 0x600002a1c000] black_start:245.834 black_end:246.876 black_duration:1.042
[blackdetect @ 0x600002a1c000] black_start:401.0 black_end:404.5 black_duration:3.5
frame=  200 fps= 30 q=-0.0 size=N/A time=00:08:00.00 bitrate=N/A speed=31.9x
video:0kB audio:0kB subtitle:0kB other streams:0kB global headers:0kB muxing overhead: unknown
`
	got := parseBlackDetectIntervals(stderr)
	want := []blackInterval{
		{Start: 12.5, End: 14.2, Duration: 1.7},
		{Start: 245.834, End: 246.876, Duration: 1.042},
		{Start: 401.0, End: 404.5, Duration: 3.5},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d intervals, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("interval %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestPickCreditsStart(t *testing.T) {
	// windowStart=1000, duration=1500: a qualifying interval must end
	// between 600s and 1470s absolute (remaining 30-900s).
	intervals := []blackInterval{
		{Start: 5, End: 6},     // absEnd=1006, remaining=494 — qualifies
		{Start: 400, End: 402}, // absEnd=1402, remaining=98 — qualifies, LATER — should win
		{Start: 495, End: 496}, // absEnd=1496, remaining=4 — too close to EOF, rejected
	}
	start, ok := pickCreditsStart(intervals, 1000, 1500)
	if !ok {
		t.Fatal("expected a qualifying interval")
	}
	if start != 1402 {
		t.Errorf("should pick the LAST qualifying interval, got %v want 1402", start)
	}
}

func TestPickCreditsStartNoneQualify(t *testing.T) {
	intervals := []blackInterval{
		{Start: 1, End: 2},     // absEnd=2, remaining=998 — too far from EOF
		{Start: 990, End: 995}, // absEnd=995, remaining=5 — too close to EOF
	}
	if _, ok := pickCreditsStart(intervals, 0, 1000); ok {
		t.Error("expected no interval to qualify")
	}
}

func TestPairRegionsPrefersNearestOrdinal(t *testing.T) {
	const runLen = 150 // ~18.6s — inside the 15-120s intro accept bounds
	run := make([]uint32, runLen)
	for i := range run {
		run[i] = hashPoint(i)
	}

	// Four "episodes": 1 and 2 share `run`; 3 and 4 share a different run.
	// Episode 2 is nearer episode 1 numerically than 3 or 4 are, so pairing
	// should resolve (1,2) and (3,4), not cross-pair. Noise prefixes are
	// drawn from widely-spaced, disjoint id ranges so they can't
	// accidentally look alike to each other or to either run.
	otherRun := make([]uint32, runLen)
	for i := range otherRun {
		otherRun[i] = hashPoint(1_000_000 + i)
	}
	points := [][]uint32{
		concatPoints(noisePoints(10_000_000, 10), run),
		concatPoints(noisePoints(20_000_000, 10), run),
		concatPoints(noisePoints(30_000_000, 10), otherRun),
		concatPoints(noisePoints(40_000_000, 10), otherRun),
	}
	ordinals := []int{1, 2, 3, 4}

	resolved := pairRegions(points, ordinals, acceptIntroRegion)
	for i, r := range resolved {
		if r == nil {
			t.Fatalf("episode %d should have resolved a region", i+1)
		}
	}
	// Episodes 1 and 2 both anchor on `run` starting at index 10, so their
	// resolved regions should agree.
	if math.Abs(resolved[0].StartSecs-resolved[1].StartSecs) > 1.0 {
		t.Errorf("episodes 1/2 regions should align: %+v vs %+v", resolved[0], resolved[1])
	}
	if math.Abs(resolved[2].StartSecs-resolved[3].StartSecs) > 1.0 {
		t.Errorf("episodes 3/4 regions should align: %+v vs %+v", resolved[2], resolved[3])
	}
}

func TestPairRegionsLeavesUnfingerprintedEpisodeUnresolved(t *testing.T) {
	const runLen = 150 // ~18.6s — inside the 15-120s intro accept bounds
	run := make([]uint32, runLen)
	for i := range run {
		run[i] = hashPoint(i)
	}
	points := [][]uint32{
		concatPoints(noisePoints(10_000_000, 5), run),
		nil, // fingerprint extraction failed for this episode
		concatPoints(noisePoints(20_000_000, 5), run),
	}
	resolved := pairRegions(points, []int{1, 2, 3}, acceptIntroRegion)
	if resolved[1] != nil {
		t.Error("an episode with no fingerprint must stay unresolved")
	}
	if resolved[0] == nil || resolved[2] == nil {
		t.Error("episodes 1 and 3 should still pair with each other")
	}
}
