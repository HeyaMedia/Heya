package sonicanalysis

import (
	"math"

	"gonum.org/v1/gonum/dsp/fourier"
)

// Mel-spectrogram preprocessing for Essentia's TensorflowInputMusiCNN
// (which is what TensorflowPredictEffnetDiscogs uses internally).
// Every parameter here is locked to what the model was trained on.
//
// Pipeline:
//
//	pcm 16 kHz mono
//	  → FrameCutter (frameSize=512, hopSize=256, startFromZero=false)
//	  → Hann window (no normalization, symmetric)
//	  → FFT(512) → magnitude → power
//	  → TriangularBands (96 Slaney-mel bands, unit_tri norm, type=power)
//	  → UnaryOperator: log10(10000*x + 1)
const (
	melSampleRate = 16000
	melFrameSize  = 512
	melHopSize    = 256
	melNumBands   = 96
	melLowHz      = 0.0
	melHighHz     = 8000.0
	melLogScale   = 10000.0
	melLogShift   = 1.0
)

// slaneyHzToMel returns the Slaney-Auditory-Toolbox mel value for a
// frequency in Hz. Linear 0..1000 Hz (slope 3/200), log above 1000 Hz
// with logStep = ln(6.4)/27.
func slaneyHzToMel(hz float64) float64 {
	const linearBreak = 1000.0
	if hz < linearBreak {
		return hz * (3.0 / 200.0)
	}
	logStep := math.Log(6.4) / 27.0
	return 15.0 + math.Log(hz/linearBreak)/logStep
}

func slaneyMelToHz(mel float64) float64 {
	if mel < 15.0 {
		return mel * (200.0 / 3.0)
	}
	logStep := math.Log(6.4) / 27.0
	return 1000.0 * math.Exp((mel-15.0)*logStep)
}

// hannWindow returns a length-N symmetric Hann window:
//
//	w[n] = 0.5 - 0.5 * cos(2π n / (N-1))
//
// Matches Essentia's Windowing(type=hann, normalized=false, symmetric=true).
func hannWindow(n int) []float64 {
	w := make([]float64, n)
	denom := float64(n - 1)
	for i := 0; i < n; i++ {
		w[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/denom)
	}
	return w
}

// melFilterBank builds the 96 triangular Slaney-mel filters in Hz
// space, normalized with unit_tri (slaney) — coefficients divided by
// the theoretical triangle area (2 / (rightHz - leftHz)) so that each
// filter's response to a flat power spectrum is independent of its
// bandwidth.
//
// Returns an (nBands × nFftBins) coefficient matrix where nFftBins =
// fftSize/2 + 1 (257 for fftSize=512).
func melFilterBank(nBands, fftSize, sampleRate int, lowHz, highHz float64) [][]float64 {
	nBins := fftSize/2 + 1

	lowMel := slaneyHzToMel(lowHz)
	highMel := slaneyHzToMel(highHz)
	boundariesHz := make([]float64, nBands+2)
	for i := range boundariesHz {
		t := float64(i) / float64(nBands+1)
		boundariesHz[i] = slaneyMelToHz(lowMel + t*(highMel-lowMel))
	}

	binHz := float64(sampleRate) / float64(fftSize)
	filt := make([][]float64, nBands)
	for b := 0; b < nBands; b++ {
		leftHz := boundariesHz[b]
		centerHz := boundariesHz[b+1]
		rightHz := boundariesHz[b+2]
		area := 0.5 * (rightHz - leftHz)
		norm := 1.0
		if area > 0 {
			norm = 1.0 / area
		}
		row := make([]float64, nBins)
		leftSlope := 1.0 / (centerHz - leftHz)
		rightSlope := 1.0 / (rightHz - centerHz)
		for k := 0; k < nBins; k++ {
			f := float64(k) * binHz
			switch {
			case f < leftHz, f > rightHz:
				row[k] = 0
			case f <= centerHz:
				row[k] = (f - leftHz) * leftSlope * norm
			default:
				row[k] = (rightHz - f) * rightSlope * norm
			}
		}
		filt[b] = row
	}
	return filt
}

// melSpec converts 16 kHz mono PCM into a (nFrames, 96) mel-power
// log-compressed spectrogram, matching Essentia's preprocessor for
// Discogs-EffNet exactly.
//
// Returns the spectrogram as a flat row-major float32 slice plus the
// frame count.
func melSpec(pcm []float32) (spec []float32, nFrames int) {
	frame := make([]float64, melFrameSize)
	window := hannWindow(melFrameSize)
	bank := melFilterBank(melNumBands, melFrameSize, melSampleRate, melLowHz, melHighHz)
	fft := fourier.NewFFT(melFrameSize)
	complexBuf := make([]complex128, melFrameSize/2+1)
	powerBuf := make([]float64, melFrameSize/2+1)

	// FrameCutter, startFromZero=false: first frame centered at sample 0,
	// so starts at sample -(frameSize/2) with the negative indices
	// zero-padded. Frame i is centered at i*hopSize.
	const halfFrame = melFrameSize / 2
	nSamples := len(pcm)
	if nSamples == 0 {
		return nil, 0
	}
	remaining := nSamples - halfFrame
	if remaining < 0 {
		nFrames = 1
	} else {
		nFrames = 1 + int(math.Ceil(float64(remaining)/float64(melHopSize)))
	}

	spec = make([]float32, nFrames*melNumBands)

	for f := 0; f < nFrames; f++ {
		start := f*melHopSize - halfFrame
		for i := 0; i < melFrameSize; i++ {
			srcIdx := start + i
			if srcIdx < 0 || srcIdx >= nSamples {
				frame[i] = 0
			} else {
				frame[i] = float64(pcm[srcIdx]) * window[i]
			}
		}
		fft.Coefficients(complexBuf, frame)
		for k := range complexBuf {
			re := real(complexBuf[k])
			im := imag(complexBuf[k])
			powerBuf[k] = re*re + im*im
		}
		base := f * melNumBands
		for b := 0; b < melNumBands; b++ {
			var sum float64
			row := bank[b]
			for k := range row {
				sum += powerBuf[k] * row[k]
			}
			spec[base+b] = float32(math.Log10(melLogScale*sum + melLogShift))
		}
	}
	return spec, nFrames
}
