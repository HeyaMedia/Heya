package sonicanalysis

import (
	"context"
	"fmt"
	"math"

	"gonum.org/v1/gonum/dsp/fourier"
)

// Musical key detection — Krumhansl-Schmuckler (1990) algorithm in
// pure Go.
//
// Pipeline:
//  1. STFT magnitude spectrogram (long frame = 8192 for ~1.95 Hz
//     resolution, enough to separate adjacent semitones above ~C3)
//  2. Map each in-range bin to its nearest pitch class (C, C#, …)
//     using equal-temperament centered on A4=440 Hz
//  3. Per-frame chroma → sum-normalized → accumulated over all
//     frames into one 12-vector for the whole track
//  4. Correlate (cosine) against the 24 key profiles
//  5. Best correlation = detected key; gap to second-best = clarity

const (
	keyFFTSize = 8192   // 1.95 Hz/bin @ 16 kHz
	keyHopSize = 2048   // ~7.8 chroma frames/sec
	keyMinHz   = 65.0   // ~C2
	keyMaxHz   = 2100.0 // ~C7
)

// Krumhansl & Schmuckler (1990) key profiles for C-major and C-minor.
// Numbers are the relative durations each pitch class occupies in a
// typical tonal piece. For other roots, the profile is cyclically
// shifted.
var (
	profileMajor = [12]float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	profileMinor = [12]float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
)

// KeyResult is the output of detectKey — a strongly-typed Key plus
// diagnostic info. KeyResult.Key is the canonical value to persist;
// Clarity helps callers decide whether to display major/minor.
type KeyResult struct {
	Key         Key
	Clarity     float64 // gap to second-best in [0, 1]
	Correlation float64 // raw best cosine correlation
}

func (k KeyResult) String() string { return k.Key.String() }

// detectKey runs the full pipeline. Returns the detected key.
//
//nolint:unused // staged: single-file CLI variant; pipeline path uses detectKeyFromPCM
func detectKey(ctx context.Context, audioPath string) (*KeyResult, error) {
	pcm, err := decodePCM(ctx, audioPath, bpmSampleRate)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return detectKeyFromPCM(pcm)
}

// detectKeyFromPCM is the underlying routine — exposed so the
// Analyzer can share one 16 kHz decode across BPM + key.
func detectKeyFromPCM(pcm []float32) (*KeyResult, error) {
	if len(pcm) < keyFFTSize {
		return nil, fmt.Errorf("audio shorter than one analysis frame (%d s)",
			keyFFTSize/bpmSampleRate)
	}
	chroma := chromagram(pcm)
	return matchKey(chroma), nil
}

func chromagram(pcm []float32) [12]float64 {
	var total [12]float64
	nFrames := 1 + (len(pcm)-keyFFTSize)/keyHopSize
	if nFrames < 1 {
		return total
	}
	window := hannPeriodic(keyFFTSize)
	fft := fourier.NewFFT(keyFFTSize)
	nBins := keyFFTSize/2 + 1
	frame := make([]float64, keyFFTSize)
	cBuf := make([]complex128, nBins)
	binHz := float64(bpmSampleRate) / float64(keyFFTSize)

	pcOfBin := make([]int, nBins)
	for k := range pcOfBin {
		f := float64(k) * binHz
		if f < keyMinHz || f > keyMaxHz {
			pcOfBin[k] = -1
			continue
		}
		midi := 69.0 + 12.0*math.Log2(f/440.0)
		pc := int(math.Round(midi)) % 12
		if pc < 0 {
			pc += 12
		}
		pcOfBin[k] = pc
	}

	for f := 0; f < nFrames; f++ {
		start := f * keyHopSize
		for i := 0; i < keyFFTSize; i++ {
			frame[i] = float64(pcm[start+i]) * window[i]
		}
		fft.Coefficients(cBuf, frame)

		var perFrame [12]float64
		var sum float64
		for k, pc := range pcOfBin {
			if pc < 0 {
				continue
			}
			re, im := real(cBuf[k]), imag(cBuf[k])
			mag := math.Sqrt(re*re + im*im)
			perFrame[pc] += mag
			sum += mag
		}
		if sum > 0 {
			for i := range perFrame {
				total[i] += perFrame[i] / sum
			}
		}
	}
	return total
}

func matchKey(chroma [12]float64) *KeyResult {
	type scored struct {
		root int
		mode KeyMode
		corr float64
	}
	bestIdx := 0
	secondIdx := 1
	results := make([]scored, 0, 24)

	profiles := []struct {
		mode KeyMode
		prof [12]float64
	}{
		{KeyModeMajor, profileMajor},
		{KeyModeMinor, profileMinor},
	}

	for r := 0; r < 12; r++ {
		for _, p := range profiles {
			var rotated [12]float64
			for i := 0; i < 12; i++ {
				rotated[i] = p.prof[((i-r)%12+12)%12]
			}
			corr := cosineF64(chroma[:], rotated[:])
			results = append(results, scored{r, p.mode, corr})
		}
	}

	for i, s := range results {
		if s.corr > results[bestIdx].corr {
			secondIdx = bestIdx
			bestIdx = i
		} else if i != bestIdx && s.corr > results[secondIdx].corr {
			secondIdx = i
		}
	}

	best := results[bestIdx]
	second := results[secondIdx]
	clarity := 0.0
	if best.corr > 0 {
		clarity = (best.corr - second.corr) / best.corr
		if clarity < 0 {
			clarity = 0
		}
		if clarity > 1 {
			clarity = 1
		}
	}
	return &KeyResult{
		Key:         Key{Root: PitchClass(best.root), Mode: best.mode},
		Clarity:     clarity,
		Correlation: best.corr,
	}
}

func cosineF64(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
