package sonicanalysis

import (
	"math"

	"gonum.org/v1/gonum/dsp/fourier"
)

// CLAP (LAION HTSAT-unfused) audio preprocessing — replicates
// transformers' ClapFeatureExtractor exactly.
//
// Pipeline:
//
//	pcm 48 kHz mono in [-1, 1]
//	  → length-fix (10 s = 480 000 samples):
//	      shorter: cyclic repeat-pad, then zero-pad the remainder
//	      longer:  center crop
//	  → reflect-pad both ends by nFFT/2
//	  → frame with periodic Hann (nFFT=1024, hop=480)
//	      (librosa STFT, center=True, pad_mode=reflect)
//	  → power spectrum (|FFT|²)
//	  → 64 Slaney-mel bands (50..14000 Hz, norm='slaney' / unit_tri)
//	  → 10 · log10(max(x, 1e-10))     (HF audio_utils.power_to_db)
//
// Output tensor shape: (1, 1, 1001, 64) flat row-major float32 →
// goes directly into the audio_model.onnx `input_features` input.
const (
	clapSampleRate = 48000
	clapNFFT       = 1024
	clapHopLength  = 480
	clapNumBands   = 64
	clapLowHz      = 50.0
	clapHighHz     = 14000.0
	clapClipLen    = 480000 // 10 s @ 48 kHz
	clapNumFrames  = 1001   // 1 + clipLen / hopLength
)

// hannPeriodic builds a length-N periodic Hann window:
//
//	w[n] = 0.5 - 0.5 * cos(2π n / N)
//
// Matches librosa's default `hann_window(N, periodic=True)`. NOTE:
// uses N in the denominator, not N-1; that's what makes it periodic
// (so DFT inversions are clean) vs the symmetric form Essentia uses.
func hannPeriodic(n int) []float64 {
	w := make([]float64, n)
	for i := 0; i < n; i++ {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n))
	}
	return w
}

// clapPrepareSamples turns arbitrary-length PCM into exactly
// `clapClipLen` samples. len ≥ clipLen → center-crop. len < clipLen
// → tile cyclically until full, then zero-pad any remainder. Cyclic
// repeat (vs zero-pad-only) keeps short clips from looking abruptly
// silent to the model.
func clapPrepareSamples(pcm []float32) []float32 {
	out := make([]float32, clapClipLen)
	if len(pcm) >= clapClipLen {
		off := (len(pcm) - clapClipLen) / 2
		copy(out, pcm[off:off+clapClipLen])
		return out
	}
	if len(pcm) == 0 {
		return out
	}
	nRepeat := clapClipLen / len(pcm)
	written := 0
	for r := 0; r < nRepeat; r++ {
		copy(out[written:], pcm)
		written += len(pcm)
	}
	return out
}

// clapMelSpec runs the full CLAP preprocessing chain. Returns a flat
// float32 slice of length 1 * 1 * 1001 * 64 ready to be copied into
// the audio_model `input_features` tensor.
func clapMelSpec(pcm []float32) []float32 {
	samples := clapPrepareSamples(pcm)
	window := hannPeriodic(clapNFFT)
	bank := melFilterBank(clapNumBands, clapNFFT, clapSampleRate, clapLowHz, clapHighHz)
	fft := fourier.NewFFT(clapNFFT)
	nFftBins := clapNFFT/2 + 1

	// Reflect-pad the signal (librosa's STFT center=True default).
	// Skip-the-edge reflection: padded[0] = samples[pad], not
	// samples[pad-1]; right-side: padded[pad+len] = samples[len-2],
	// not samples[len-1]. This matches numpy.pad(mode='reflect').
	pad := clapNFFT / 2
	padded := make([]float64, len(samples)+2*pad)
	for i, v := range samples {
		padded[pad+i] = float64(v)
	}
	for i := 0; i < pad; i++ {
		padded[i] = float64(samples[pad-i])
		padded[pad+len(samples)+i] = float64(samples[len(samples)-2-i])
	}

	complexBuf := make([]complex128, nFftBins)
	frame := make([]float64, clapNFFT)
	powerBuf := make([]float64, nFftBins)
	out := make([]float32, clapNumFrames*clapNumBands)

	for f := 0; f < clapNumFrames; f++ {
		start := f * clapHopLength
		for i := 0; i < clapNFFT; i++ {
			frame[i] = padded[start+i] * window[i]
		}
		fft.Coefficients(complexBuf, frame)
		for k := range complexBuf {
			re := real(complexBuf[k])
			im := imag(complexBuf[k])
			powerBuf[k] = re*re + im*im
		}
		melOff := f * clapNumBands
		for b := 0; b < clapNumBands; b++ {
			var sum float64
			row := bank[b]
			for k := range row {
				sum += powerBuf[k] * row[k]
			}
			if sum < 1e-10 {
				sum = 1e-10
			}
			out[melOff+b] = float32(10.0 * math.Log10(sum))
		}
	}
	return out
}
