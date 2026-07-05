package worker

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Local Intro-Skipper-style skip-segment detection — the fallback pass for
// files the community databases (TheIntroDB / SkipMe.db / AniSkip) couldn't
// answer. Two independent techniques, both pure local signal:
//
//   - TV: chromaprint-fingerprint an intro-length window and a tail window
//     of every episode in a season, then look for a shared audio region
//     between episodes (the title sequence repeats near-verbatim; so does
//     a "next time on" / recap tail in some shows). A season needs at
//     least two pending episodes to compare — one file alone has nothing
//     to pair against.
//   - Movies: ffmpeg's blackdetect filter over the tail of the file finds
//     the black frame that typically separates the story from the credits
//     roll.
//
// Everything below the ffmpeg invocations (chromaprintWindowRaw,
// detectMovieCredits) is pure and independently testable — see
// segments_detect_test.go. The matching algorithm is a clean-room port from
// the parameters Intro-Skipper documents publicly (windowed audio
// fingerprint + Hamming-distance alignment scan), not a code copy.

// chromaprintPointDurationSecs is how much audio one chromaprint "point"
// (a uint32 in the raw fingerprint stream) covers. Chromaprint's TEST2
// algorithm — the default of both fpcalc and ffmpeg's chromaprint muxer —
// uses an 11025Hz sample rate, a 4096-sample analysis frame, and 2/3
// overlap between consecutive frames, so a new point lands every
// frameSize/sampleRate/3 seconds (~0.1238s).
const chromaprintPointDurationSecs = 4096.0 / 11025.0 / 3.0

// findSharedRegion neighborhood + similarity tolerances.
const (
	// chromaNeighborhoodRadius bounds how far a point value can drift and
	// still probe as a candidate alignment. Chromaprint points are already
	// a lossy 32-bit hash of the local spectrum, so exact equality is too
	// strict to find real matches — but a wide radius explodes the
	// candidate-shift count, so this stays tight.
	chromaNeighborhoodRadius = 2
	// chromaHammingTolerance is the max popcount of a^b for two points to
	// be considered the "same" audio moment.
	chromaHammingTolerance = 6
	// regionMaxGapSecs tolerates short non-matching stretches inside an
	// otherwise-contiguous shared region (a beat drop, a title card fade)
	// without splitting it into two shorter, individually-rejectable
	// regions.
	regionMaxGapSecs = 3.5
)

// findSharedRegion looks for the longest contiguous stretch of audio in a
// that also appears somewhere in b, operating entirely in chromaprint
// point-space. Returns the region's bounds in a's own timeline (seconds
// from the start of whatever window a was fingerprinted from).
//
// Algorithm:
//  1. Index b by point value -> last index holding that value.
//  2. For every point in a, probe b's index across a small integer
//     neighborhood of the point's value (chromaprint hashes are noisy, so
//     exact equality would miss real matches) and record the alignment
//     shift each hit implies (s = i - j).
//  3. For each distinct candidate shift, walk the overlapping range of a
//     and b at that alignment, flagging each pair whose Hamming distance
//     is within tolerance as "similar" and noting its time in a.
//  4. Merge similar timestamps into contiguous ranges (bridging gaps up to
//     regionMaxGapSecs), and keep the longest range across every shift
//     tried.
func findSharedRegion(a, b []uint32) (startSecs, endSecs float64, ok bool) {
	if len(a) == 0 || len(b) == 0 {
		return 0, 0, false
	}

	bIndex := make(map[uint32]int, len(b))
	for j, v := range b {
		bIndex[v] = j
	}

	shiftSeen := make(map[int]bool)
	shifts := make([]int, 0, 64)
	for i, v := range a {
		for d := -chromaNeighborhoodRadius; d <= chromaNeighborhoodRadius; d++ {
			probe := uint32(int64(v) + int64(d))
			j, hit := bIndex[probe]
			if !hit {
				continue
			}
			s := i - j
			if !shiftSeen[s] {
				shiftSeen[s] = true
				shifts = append(shifts, s)
			}
		}
	}

	var bestStart, bestEnd, bestLen float64
	found := false

	for _, s := range shifts {
		lo := 0
		if s > 0 {
			lo = s
		}
		hi := len(a)
		if len(b)+s < hi {
			hi = len(b) + s
		}
		if hi <= lo {
			continue
		}

		var times []float64
		for i := lo; i < hi; i++ {
			j := i - s
			if bits.OnesCount32(a[i]^b[j]) <= chromaHammingTolerance {
				times = append(times, float64(i)*chromaprintPointDurationSecs)
			}
		}
		if len(times) == 0 {
			continue
		}

		rangeStart := times[0]
		prev := times[0]
		flush := func() {
			// Pad the end by one point's duration: `prev` is the start
			// timestamp of the last matching point, not the end of the
			// audio it covers.
			if length := prev + chromaprintPointDurationSecs - rangeStart; length > bestLen {
				bestLen = length
				bestStart, bestEnd = rangeStart, prev+chromaprintPointDurationSecs
				found = true
			}
		}
		for _, t := range times[1:] {
			if t-prev > regionMaxGapSecs {
				flush()
				rangeStart = t
			}
			prev = t
		}
		flush()
	}

	return bestStart, bestEnd, found
}

// Acceptance bounds. Anything shorter is noise (a shared jingle sting, a
// coincidental few notes); anything longer is almost certainly a
// mis-alignment rather than a real title sequence or credits roll.
const (
	introMinSecs                 = 15.0
	introMaxSecs                 = 120.0
	introSnapToZeroThresholdSecs = 5.0

	creditsMinSecs = 15.0
	creditsMaxSecs = 450.0
)

// acceptIntroRegion validates a candidate intro region and snaps a
// near-zero start down to exactly 0 — a network bug or logo bumper before
// the true title sequence shouldn't shave a few seconds off the skip
// button's reach.
func acceptIntroRegion(startSecs, endSecs float64) (float64, float64, bool) {
	if length := endSecs - startSecs; length < introMinSecs || length > introMaxSecs {
		return 0, 0, false
	}
	if startSecs <= introSnapToZeroThresholdSecs {
		startSecs = 0
	}
	return startSecs, endSecs, true
}

// acceptCreditsRegion validates a candidate region found inside a tail
// window. The region itself isn't the final credits segment — the caller
// extends it to the end of the file — so this only gates length.
func acceptCreditsRegion(startSecs, endSecs float64) (float64, float64, bool) {
	if length := endSecs - startSecs; length < creditsMinSecs || length > creditsMaxSecs {
		return 0, 0, false
	}
	return startSecs, endSecs, true
}

// introWindowSecs returns the (start, duration) of the window to
// fingerprint for intro matching: the first min(25% of runtime, 600s).
// Files under a minute (stingers, extras misfiled as episodes) are
// skipped — there's no meaningful "intro" to find.
func introWindowSecs(durationSecs float64) (start, dur float64, ok bool) {
	if durationSecs < 60 {
		return 0, 0, false
	}
	return 0, math.Min(0.25*durationSecs, 600), true
}

// tailWindowSecs returns the (start, duration) of the window to
// fingerprint for credits matching: the last min(10% of runtime + 120s,
// 360s).
func tailWindowSecs(durationSecs float64) (start, dur float64, ok bool) {
	if durationSecs < 60 {
		return 0, 0, false
	}
	dur = math.Min(0.10*durationSecs+120, 360)
	if dur > durationSecs {
		dur = durationSecs
	}
	return durationSecs - dur, dur, true
}

// resolvedRegion is a detected segment window in the timeline of the
// window it was fingerprinted from (seconds from that window's own start,
// NOT the file's start — the caller applies the window offset).
type resolvedRegion struct {
	StartSecs float64
	EndSecs   float64
}

// pairRegions resolves each entry's region by comparing its fingerprint
// against every other entry's, nearest ordinal number first (episode
// numbers, in practice), accepting the first match both sides agree
// satisfies accept. Entries with no fingerprint (nil points) or already
// resolved by an earlier pair are left alone. A resolved pair is removed
// from further consideration on neither side — each entry only needs one
// partner — but any leftover unresolved entry can still pair with another
// leftover in a later iteration.
func pairRegions(points [][]uint32, ordinals []int, accept func(start, end float64) (float64, float64, bool)) []*resolvedRegion {
	n := len(points)
	out := make([]*resolvedRegion, n)
	for i := 0; i < n; i++ {
		if out[i] != nil || points[i] == nil {
			continue
		}
		for _, j := range nearestByOrdinal(i, ordinals) {
			if out[j] != nil || points[j] == nil {
				continue
			}
			aStart, aEnd, ok := findSharedRegion(points[i], points[j])
			if !ok {
				continue
			}
			aAccStart, aAccEnd, ok := accept(aStart, aEnd)
			if !ok {
				continue
			}
			bStart, bEnd, ok := findSharedRegion(points[j], points[i])
			if !ok {
				continue
			}
			bAccStart, bAccEnd, ok := accept(bStart, bEnd)
			if !ok {
				continue
			}
			out[i] = &resolvedRegion{StartSecs: aAccStart, EndSecs: aAccEnd}
			out[j] = &resolvedRegion{StartSecs: bAccStart, EndSecs: bAccEnd}
			break
		}
	}
	return out
}

// nearestByOrdinal returns every other index sorted by closeness of
// ordinals[j] to ordinals[i] — episode 4 tries episode 5 and 3 before 1 or
// 12, since adjacent episodes are the most likely to share a rerun intro
// and the cheapest to get right when a season has outliers (specials,
// two-parters with a different cold open).
func nearestByOrdinal(i int, ordinals []int) []int {
	type cand struct{ idx, dist int }
	cands := make([]cand, 0, len(ordinals)-1)
	for j := range ordinals {
		if j == i {
			continue
		}
		d := ordinals[j] - ordinals[i]
		if d < 0 {
			d = -d
		}
		cands = append(cands, cand{j, d})
	}
	sort.Slice(cands, func(a, b int) bool { return cands[a].dist < cands[b].dist })
	out := make([]int, len(cands))
	for k, c := range cands {
		out[k] = c.idx
	}
	return out
}

// parseChromaprintRaw decodes ffmpeg's raw chromaprint muxer output (a
// straight sequence of little-endian uint32 points, no header) into point
// values.
func parseChromaprintRaw(b []byte) ([]uint32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("chromaprint raw output not a multiple of 4 bytes (%d)", len(b))
	}
	out := make([]uint32, len(b)/4)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(b[i*4 : i*4+4])
	}
	return out, nil
}

// chromaprintMuxerAvailable probes once per process whether this host's
// ffmpeg has the chromaprint muxer. Unlike chromaprintFile's
// detectFpMethod, there's no fpcalc fallback here: fpcalc can't seek to an
// arbitrary window, only fingerprint from the start of a file, which is
// useless for a tail-window credits scan. If the muxer is missing, local
// detection stays a no-op (season/movie workers skip stamping
// segments_detected_at so a future ffmpeg upgrade retries automatically).
var chromaprintMuxerAvailable = sync.OnceValue(func() bool {
	probe := exec.Command("ffmpeg", "-hide_banner", "-h", "muxer=chromaprint")
	var out bytes.Buffer
	probe.Stdout = &out
	probe.Stderr = &out
	if err := probe.Run(); err == nil && strings.Contains(out.String(), "Muxer chromaprint") {
		return true
	}
	log.Warn().Msg("segments detect: ffmpeg chromaprint muxer unavailable — local intro/credits detection disabled until upgraded")
	return false
})

// chromaprintWindowRaw fingerprints a bounded window of one audio file,
// returning the raw chromaprint points. Unlike chromaprintFile (which
// always starts at 0), this seeks to an arbitrary offset — needed to
// fingerprint a tail window without decoding the whole file.
func chromaprintWindowRaw(ctx context.Context, path string, startSecs, durSecs float64) ([]uint32, error) {
	cmd := exec.CommandContext(ctx, //nolint:gosec // path comes from library_files we control; ffmpeg binary is fixed
		"ffmpeg",
		"-nostdin", "-nostats", "-hide_banner",
		"-ss", fmt.Sprintf("%.3f", startSecs),
		"-t", fmt.Sprintf("%.3f", durSecs),
		"-i", path,
		"-vn", "-sn", "-dn", "-map", "0:a:0",
		"-ac", "2",
		"-f", "chromaprint", "-fp_format", "raw", "-",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg chromaprint window: %w (stderr: %s)", err, truncate(stderr.String(), 300))
	}
	return parseChromaprintRaw(stdout.Bytes())
}

// ---------------------------------------------------------------------------
// Movie credits — ffmpeg blackdetect
// ---------------------------------------------------------------------------

const (
	// movieCreditsWindowSecs bounds how much of the file's tail ffmpeg
	// actually has to decode; credits never start earlier than this on any
	// normal-length feature.
	movieCreditsWindowSecs = 480.0
	// A qualifying black interval must leave at least this much runtime
	// after it — shorter than this, it's more likely the very last shot
	// fading to black than a story/credits cut.
	movieCreditsMinRemainingSecs = 30.0
	// ...and no more than this — anything further out isn't the credits
	// cut, it's a black transition mid-scene.
	movieCreditsMaxRemainingSecs = 900.0
)

var reBlackInterval = regexp.MustCompile(`black_start:([\d.]+)\s+black_end:([\d.]+)\s+black_duration:([\d.]+)`)

// blackInterval is one blackdetect hit, all times relative to whatever
// window ffmpeg was scanning (seconds).
type blackInterval struct {
	Start, End, Duration float64
}

// parseBlackDetectIntervals extracts every black_start/black_end/
// black_duration triple ffmpeg's blackdetect filter writes to stderr, in
// the order it reported them (chronological).
func parseBlackDetectIntervals(stderrOutput string) []blackInterval {
	matches := reBlackInterval.FindAllStringSubmatch(stderrOutput, -1)
	out := make([]blackInterval, 0, len(matches))
	for _, m := range matches {
		start, err1 := strconv.ParseFloat(m[1], 64)
		end, err2 := strconv.ParseFloat(m[2], 64)
		dur, err3 := strconv.ParseFloat(m[3], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		out = append(out, blackInterval{Start: start, End: end, Duration: dur})
	}
	return out
}

// detectMovieCredits runs ffmpeg's blackdetect filter over the tail
// movieCreditsWindowSecs of a movie and picks the credits cut: the LAST
// black interval whose end leaves between movieCreditsMinRemainingSecs and
// movieCreditsMaxRemainingSecs of runtime. Returns ok=false (not an error)
// when no interval qualifies — most movies have zero or several
// black-frame transitions that don't fit the window, and that's a normal
// outcome, not a failure.
func detectMovieCredits(ctx context.Context, path string, durationSecs float64) (creditsStartSecs float64, ok bool, err error) {
	windowStart := durationSecs - movieCreditsWindowSecs
	if windowStart < 0 {
		windowStart = 0
	}

	execCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(execCtx, //nolint:gosec // path comes from library_files we control; ffmpeg binary is fixed
		"ffmpeg",
		"-nostdin", "-nostats", "-hide_banner",
		"-ss", fmt.Sprintf("%.3f", windowStart),
		"-i", path,
		"-vf", "blackdetect=d=0.5:pix_th=0.10",
		"-an", "-sn", "-dn",
		"-f", "null", "-",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return 0, false, fmt.Errorf("ffmpeg blackdetect: %w (stderr: %s)", runErr, truncate(stderr.String(), 500))
	}

	absEnd, ok := pickCreditsStart(parseBlackDetectIntervals(stderr.String()), windowStart, durationSecs)
	return absEnd, ok, nil
}

// pickCreditsStart is the pure selection half of detectMovieCredits: given
// the black intervals ffmpeg found (relative to windowStart) and the
// file's total duration, picks the LAST interval whose end leaves between
// movieCreditsMinRemainingSecs and movieCreditsMaxRemainingSecs of runtime
// — the credits cut, not the final fade-to-black or a black transition
// mid-scene. Returns ok=false when nothing qualifies.
func pickCreditsStart(intervals []blackInterval, windowStart, durationSecs float64) (creditsStartSecs float64, ok bool) {
	for i := len(intervals) - 1; i >= 0; i-- {
		absEnd := windowStart + intervals[i].End
		remaining := durationSecs - absEnd
		if remaining >= movieCreditsMinRemainingSecs && remaining <= movieCreditsMaxRemainingSecs {
			return absEnd, true
		}
	}
	return 0, false
}
