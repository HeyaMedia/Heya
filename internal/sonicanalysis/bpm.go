package sonicanalysis

import (
	"context"
	"fmt"
	"math"

	"gonum.org/v1/gonum/dsp/fourier"
)

// Tempo (BPM) detection — pure Go, no ML model.
//
// Algorithm: spectral-flux onset detection function (ODF) over a
// short STFT, then autocorrelation of the ODF to find the most
// dominant periodicity in the 50-200 BPM range. Soft Gaussian prior
// at 120 BPM mitigates octave errors.

const (
	bpmSampleRate = 16000
	bpmFrameSize  = 1024
	bpmHopSize    = 160 // → 100 Hz ODF rate
	bpmMinBPM     = 50
	bpmMaxBPM     = 200
)

// detectBPM runs the full audio → tempo pipeline. Returns the
// estimated BPM and a "confidence" score in [0,1] (peak strength
// relative to the autocorrelation noise floor).
//
// Currently only wired in via detectBPMFromPCM (called from the
// extractor); kept exported-style so the path-based variant remains
// available when callers need to skip the shared decoder.
//
//nolint:unused // staged for a future single-file CLI path
func detectBPM(ctx context.Context, audioPath string) (bpm float64, confidence float64, err error) {
	pcm, err := decodePCM(ctx, audioPath, bpmSampleRate)
	if err != nil {
		return 0, 0, fmt.Errorf("decode: %w", err)
	}
	return detectBPMFromPCM(pcm)
}

// detectBPMFromPCM is the underlying routine, exported into a
// separate function so the Analyzer can share one decode across
// BPM + key (both want 16 kHz mono PCM).
func detectBPMFromPCM(pcm []float32) (bpm float64, confidence float64, err error) {
	if len(pcm) < bpmSampleRate*4 {
		return 0, 0, fmt.Errorf("need at least 4 s of audio, got %.1f s",
			float64(len(pcm))/bpmSampleRate)
	}

	odf := spectralFluxODF(pcm)
	if len(odf) == 0 {
		return 0, 0, fmt.Errorf("empty onset envelope")
	}
	normalizeODF(odf)

	odfRate := float64(bpmSampleRate) / float64(bpmHopSize)
	minLag := int(math.Round(60.0 * odfRate / float64(bpmMaxBPM)))
	maxLag := int(math.Round(60.0 * odfRate / float64(bpmMinBPM)))
	if maxLag >= len(odf) {
		maxLag = len(odf) - 1
	}

	corr := autocorrelate(odf, minLag, maxLag)
	if len(corr) == 0 {
		return 0, 0, fmt.Errorf("no valid lag range (audio too short)")
	}

	const priorCenter = 120.0
	const priorSigma = 50.0
	weighted := make([]float64, len(corr))
	for i, c := range corr {
		lag := minLag + i
		bpmAtLag := 60.0 * odfRate / float64(lag)
		d := bpmAtLag - priorCenter
		w := math.Exp(-(d * d) / (2 * priorSigma * priorSigma))
		weighted[i] = c * w
	}

	peakIdx := 0
	peakVal := weighted[0]
	for i, v := range weighted {
		if v > peakVal {
			peakVal = v
			peakIdx = i
		}
	}
	peakLag := float64(minLag + peakIdx)

	if peakIdx > 0 && peakIdx < len(weighted)-1 {
		y0, y1, y2 := weighted[peakIdx-1], weighted[peakIdx], weighted[peakIdx+1]
		denom := y0 - 2*y1 + y2
		if denom != 0 {
			delta := 0.5 * (y0 - y2) / denom
			peakLag += delta
		}
	}

	bpm = 60.0 * odfRate / peakLag

	med := median(corr)
	confidence = 0
	if med > 0 {
		confidence = math.Min(1.0, (corr[peakIdx]/med-1.0)/2.0)
		if confidence < 0 {
			confidence = 0
		}
	}
	return bpm, confidence, nil
}

func spectralFluxODF(pcm []float32) []float64 {
	nFrames := 1 + (len(pcm)-bpmFrameSize)/bpmHopSize
	if nFrames < 2 {
		return nil
	}
	window := hannPeriodic(bpmFrameSize)
	fft := fourier.NewFFT(bpmFrameSize)
	nBins := bpmFrameSize/2 + 1
	frame := make([]float64, bpmFrameSize)
	cBuf := make([]complex128, nBins)
	prev := make([]float64, nBins)
	cur := make([]float64, nBins)
	odf := make([]float64, nFrames)

	for f := 0; f < nFrames; f++ {
		start := f * bpmHopSize
		for i := 0; i < bpmFrameSize; i++ {
			frame[i] = float64(pcm[start+i]) * window[i]
		}
		fft.Coefficients(cBuf, frame)
		for k := range cBuf {
			re, im := real(cBuf[k]), imag(cBuf[k])
			cur[k] = math.Sqrt(re*re + im*im)
		}
		if f > 0 {
			var sum float64
			for k := range cur {
				d := cur[k] - prev[k]
				if d > 0 {
					sum += d
				}
			}
			odf[f] = sum
		}
		prev, cur = cur, prev
	}
	return odf
}

func normalizeODF(odf []float64) {
	if len(odf) == 0 {
		return
	}
	var sum, sumSq float64
	for _, v := range odf {
		sum += v
		sumSq += v * v
	}
	n := float64(len(odf))
	mean := sum / n
	variance := sumSq/n - mean*mean
	std := math.Sqrt(variance)
	if std == 0 {
		return
	}
	inv := 1.0 / std
	for i, v := range odf {
		odf[i] = (v - mean) * inv
	}
}

func autocorrelate(odf []float64, minLag, maxLag int) []float64 {
	if maxLag < minLag || maxLag >= len(odf) {
		return nil
	}
	out := make([]float64, maxLag-minLag+1)
	for lag := minLag; lag <= maxLag; lag++ {
		var sum float64
		for t := 0; t+lag < len(odf); t++ {
			sum += odf[t] * odf[t+lag]
		}
		out[lag-minLag] = sum
	}
	return out
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := make([]float64, len(xs))
	copy(sorted, xs)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j] < sorted[j-1]; j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	return sorted[len(sorted)/2]
}
