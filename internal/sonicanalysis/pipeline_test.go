package sonicanalysis

import (
	"context"
	"errors"
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
