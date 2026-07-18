package sonicanalysis

import (
	"context"
	"fmt"

	"golang.org/x/sync/singleflight"
)

// Waveform generation — produces N max-absolute-peak buckets across
// a track's duration, suitable for rendering as a horizontal playbar
// strip.
//
// Decoded at 8 kHz mono — the visual peak envelope is identical to
// full-rate PCM since we're taking the maximum absolute value per
// bucket and a typical bucket covers >100 samples even at this rate.
//
// Output: []float32 of length N, range [0, 1].

const (
	waveformSampleRate = 8000
	waveformDefaultN   = 2000
)

var playbackEnvelopeAnalysis singleflight.Group

// PlaybackEnvelope contains the two cheap artifacts derived from the same
// low-rate mono decode.
type PlaybackEnvelope struct {
	Waveform   []float32
	Boundaries *Boundaries
}

// AnalyzePlaybackEnvelope decodes once at 8 kHz and derives waveform peaks and
// smart-crossfade boundaries from the same PCM. Concurrent callers share it.
func AnalyzePlaybackEnvelope(ctx context.Context, audioPath string) (*PlaybackEnvelope, error) {
	value, err, _ := playbackEnvelopeAnalysis.Do(audioPath, func() (any, error) {
		pcm, decodeErr := decodePCM(ctx, audioPath, waveformSampleRate)
		if decodeErr != nil {
			return nil, fmt.Errorf("decode: %w", decodeErr)
		}
		waveform, waveformErr := waveformFromPCM(pcm, waveformDefaultN)
		if waveformErr != nil {
			return nil, waveformErr
		}
		return &PlaybackEnvelope{
			Waveform:   waveform,
			Boundaries: boundariesFromPCM(pcm, waveformSampleRate),
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*PlaybackEnvelope), nil
}

// ComputeWaveform generates the standard persisted waveform without loading
// any Sonic/CLAP models. It is safe to use from playback's on-demand path.
func ComputeWaveform(ctx context.Context, audioPath string) ([]float32, error) {
	envelope, err := AnalyzePlaybackEnvelope(ctx, audioPath)
	if err != nil {
		return nil, err
	}
	return envelope.Waveform, nil
}

// waveformFromPCM is the underlying routine, exposed so the Analyzer
// can share one decode across multiple steps. Expects PCM at any
// rate (only the count of samples matters for bucketing).
func waveformFromPCM(pcm []float32, n int) ([]float32, error) {
	if n <= 0 {
		n = waveformDefaultN
	}
	if len(pcm) == 0 {
		return nil, fmt.Errorf("decoded zero samples")
	}
	if len(pcm) < n {
		out := make([]float32, n)
		for i, v := range pcm {
			if v < 0 {
				v = -v
			}
			out[i] = v
		}
		return out, nil
	}

	out := make([]float32, n)
	bucketSize := float64(len(pcm)) / float64(n)
	for i := 0; i < n; i++ {
		start := int(float64(i) * bucketSize)
		end := int(float64(i+1) * bucketSize)
		if end > len(pcm) {
			end = len(pcm)
		}
		var peak float32
		for _, v := range pcm[start:end] {
			a := v
			if a < 0 {
				a = -a
			}
			if a > peak {
				peak = a
			}
		}
		out[i] = peak
	}
	return out, nil
}
