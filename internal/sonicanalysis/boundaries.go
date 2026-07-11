package sonicanalysis

import (
	"context"
	"math"
)

// Structural boundary detection — finds the intro/outro/fade/silence transition
// points of a track from its RMS energy envelope. The player's smart crossfade
// uses these to align a transition with the music's natural shape (start the
// fade where the track is already dying, not at a fixed N seconds before the
// end). Algorithm is the broadcast-auto-DJ RMS-envelope approach (Liquidsoap /
// Mixxx style); ported 1:1 from the reference TS implementation so the constants
// and edge-cases match.

const (
	boundaryWindowSec  = 0.1 // 100ms RMS windows
	boundaryWindowMs   = 100 // boundaryWindowSec * 1000, as an int for ms math
	silenceThresholdDB = -60.0
)

// Boundaries holds the detected transition points, in milliseconds from the
// start of the track. Any field may legitimately equal the track length (e.g.
// a hard-ending track has fadeStart == end).
type Boundaries struct {
	IntroEndMs     int // first window ≥10% of peak RMS — where the real audio starts
	OutroStartMs   int // last window ≥10% of peak RMS (past the 50% mark)
	FadeStartMs    int // start of a sustained end fade (≥2s decline past 60%)
	SilenceStartMs int // where the trailing signal drops below -60 dBFS
}

// DetectBoundaries decodes the file to a low-rate mono signal and computes its
// structural boundaries. Cheap single pass on top of the 8kHz decode — a few
// hundred ms per track. Returns nil (no error) for empty/too-short audio.
func DetectBoundaries(ctx context.Context, audioPath string) (*Boundaries, error) {
	envelope, err := AnalyzePlaybackEnvelope(ctx, audioPath)
	if err != nil {
		return nil, err
	}
	return envelope.Boundaries, nil
}

// boundariesFromPCM is the pure DSP core, separated for unit testing.
func boundariesFromPCM(samples []float32, sampleRate int) *Boundaries {
	if len(samples) == 0 {
		return nil
	}
	windowSize := int(float64(sampleRate) * boundaryWindowSec)
	if windowSize <= 0 {
		return nil
	}
	numWindows := len(samples) / windowSize
	if numWindows == 0 {
		return nil
	}
	durationMs := int(math.Round(float64(len(samples)) / float64(sampleRate) * 1000))

	rms := make([]float64, numWindows)
	var peak float64
	for i := 0; i < numWindows; i++ {
		start := i * windowSize
		var sum float64
		for j := 0; j < windowSize; j++ {
			s := float64(samples[start+j])
			sum += s * s
		}
		rms[i] = math.Sqrt(sum / float64(windowSize))
		if rms[i] > peak {
			peak = rms[i]
		}
	}

	return &Boundaries{
		IntroEndMs:     findIntroEnd(rms, peak),
		OutroStartMs:   findOutroStart(rms, peak, durationMs),
		FadeStartMs:    detectFadeOut(rms, peak, durationMs),
		SilenceStartMs: findSilenceStart(rms),
	}
}

func findIntroEnd(rms []float64, peak float64) int {
	if peak == 0 {
		return 0
	}
	threshold := peak * 0.1
	for i, v := range rms {
		if v >= threshold {
			return i * boundaryWindowMs
		}
	}
	return 0
}

func findOutroStart(rms []float64, peak float64, durationMs int) int {
	if peak == 0 {
		return durationMs
	}
	threshold := peak * 0.1
	minPositionMs := float64(durationMs) * 0.5
	for i := len(rms) - 1; i >= 0; i-- {
		if rms[i] >= threshold {
			outroMs := (i + 1) * boundaryWindowMs
			if float64(outroMs) < minPositionMs {
				return durationMs
			}
			return outroMs
		}
	}
	return durationMs
}

// detectFadeOut scans backward for a sustained run of decreasing windows below
// 5% of peak — the signature of an end fade — and returns where that run began,
// provided it starts past 60% of the track. Returns the track end when there's
// no clean fade (hard endings, live cuts).
func detectFadeOut(rms []float64, peak float64, durationMs int) int {
	if peak == 0 {
		return durationMs
	}
	threshold := peak * 0.05
	minFadeWindows := int(math.Ceil(2.0 / boundaryWindowSec)) // ≥2s of fade
	minPositionMs := float64(durationMs) * 0.6

	consecutiveDecreasing := 0
	fadeCandidate := -1

	// emit reports the fade start if the current run qualifies.
	emit := func() (int, bool) {
		if consecutiveDecreasing >= minFadeWindows && fadeCandidate != -1 {
			startMs := (fadeCandidate - consecutiveDecreasing + 1) * boundaryWindowMs
			if float64(startMs) >= minPositionMs {
				return startMs, true
			}
		}
		return 0, false
	}

	for i := len(rms) - 1; i >= 1; i-- {
		if rms[i] < threshold {
			if ms, ok := emit(); ok {
				return ms
			}
			consecutiveDecreasing = 0
			fadeCandidate = -1
			continue
		}
		if rms[i] < rms[i-1] {
			consecutiveDecreasing++
			if fadeCandidate == -1 {
				fadeCandidate = i
			}
		} else {
			if ms, ok := emit(); ok {
				return ms
			}
			consecutiveDecreasing = 0
			fadeCandidate = -1
		}
	}
	if ms, ok := emit(); ok {
		return ms
	}
	return durationMs
}

func findSilenceStart(rms []float64) int {
	silenceLinear := math.Pow(10, silenceThresholdDB/20) // -60 dBFS ≈ 0.001
	for i := len(rms) - 1; i >= 0; i-- {
		if rms[i] >= silenceLinear {
			return (i + 1) * boundaryWindowMs
		}
	}
	return 0
}
