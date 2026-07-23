package sonicanalysis

import (
	"context"
	"errors"
	"math"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestConfigNormalizesPipelineLimits(t *testing.T) {
	defaults := (Config{}).normalize()
	if defaults.PreprocessAhead != DefaultPreprocessAhead {
		t.Fatalf("default preprocess ahead = %d, want %d", defaults.PreprocessAhead, DefaultPreprocessAhead)
	}
	if defaults.GPUWorkers != DefaultGPUWorkers {
		t.Fatalf("default GPU workers = %d, want %d", defaults.GPUWorkers, DefaultGPUWorkers)
	}

	clamped := (Config{
		PreprocessAhead: MaxPreprocessAhead + 10,
		GPUWorkers:      MaxGPUWorkers + 10,
	}).normalize()
	if clamped.PreprocessAhead != MaxPreprocessAhead || clamped.GPUWorkers != MaxGPUWorkers {
		t.Fatalf("normalized config = %d/%d, want %d/%d",
			clamped.PreprocessAhead, clamped.GPUWorkers, MaxPreprocessAhead, MaxGPUWorkers)
	}
}

func TestAnalyzerPipelineWorkers(t *testing.T) {
	a := NewAnalyzer(Config{PreprocessAhead: 10, GPUWorkers: 2})
	if got := a.PipelineWorkers(); got != 12 {
		t.Fatalf("PipelineWorkers() = %d, want 12", got)
	}
}

func TestAnalysisSlotHonorsCancellation(t *testing.T) {
	slots := make(chan struct{}, 1)
	if err := acquireAnalysisSlot(context.Background(), slots); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := acquireAnalysisSlot(ctx, slots); !errors.Is(err, context.Canceled) {
		t.Fatalf("second acquire error = %v, want context.Canceled", err)
	}
	releaseAnalysisSlot(slots)
}

func TestMeanCLAPEmbeddingsNormalizesThreeWindows(t *testing.T) {
	first := make([]float32, clapEmbedDim)
	center := make([]float32, clapEmbedDim)
	last := make([]float32, clapEmbedDim)
	first[0] = 1
	center[1] = 1
	last[0] = 1

	got, err := meanCLAPEmbeddings([][]float32{first, center, last})
	if err != nil {
		t.Fatalf("mean CLAP embeddings: %v", err)
	}
	wantNorm := float32(math.Sqrt(5))
	if diff := got[0] - 2/wantNorm; diff < -1e-6 || diff > 1e-6 {
		t.Fatalf("first dimension = %f, want %f", got[0], 2/wantNorm)
	}
	if diff := got[1] - 1/wantNorm; diff < -1e-6 || diff > 1e-6 {
		t.Fatalf("second dimension = %f, want %f", got[1], 1/wantNorm)
	}
}

func TestDecodeAnalysisAudioSharesOneFFmpegPass(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	audioPath := filepath.Join(t.TempDir(), "stereo.wav")
	generate := exec.Command(
		"ffmpeg",
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=30",
		"-ac", "2",
		audioPath,
	)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate stereo fixture: %v (%s)", err, output)
	}
	decoded, err := decodeAnalysisAudio(context.Background(), audioPath, clapTrackPositions, true)
	if err != nil {
		t.Fatalf("shared analysis decode: %v", err)
	}
	if len(decoded.PCM16) != 30*melSampleRate {
		t.Fatalf("16 kHz samples = %d, want %d", len(decoded.PCM16), 30*melSampleRate)
	}
	if len(decoded.CLAPClips) != 3 {
		t.Fatalf("CLAP clips = %d, want 3", len(decoded.CLAPClips))
	}
	for i, clip := range decoded.CLAPClips {
		if len(clip) != clapClipLen {
			t.Fatalf("CLAP clip %d samples = %d, want %d", i, len(clip), clapClipLen)
		}
	}
}
