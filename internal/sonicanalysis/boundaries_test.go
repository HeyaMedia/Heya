package sonicanalysis

import "testing"

// makeStructuredSignal builds a 10s/8kHz mono signal with a clear shape:
// 1s silence intro → 5s body at 0.8 → 3s linear fade to 0 → 1s silence.
func makeStructuredSignal(sr int) []float32 {
	total := 10 * sr
	s := make([]float32, total)
	for i := 1 * sr; i < 6*sr; i++ {
		s[i] = 0.8
	}
	fadeStart, fadeEnd := 6*sr, 9*sr
	for i := fadeStart; i < fadeEnd; i++ {
		t := float64(i-fadeStart) / float64(fadeEnd-fadeStart)
		s[i] = float32(0.8 * (1 - t))
	}
	return s
}

func TestBoundariesFromPCM_Structured(t *testing.T) {
	sr := 8000
	b := boundariesFromPCM(makeStructuredSignal(sr), sr)
	if b == nil {
		t.Fatal("expected non-nil boundaries")
	}

	// Intro ends ~1s where the body starts.
	if b.IntroEndMs < 900 || b.IntroEndMs > 1100 {
		t.Errorf("IntroEndMs = %d, want ~1000", b.IntroEndMs)
	}
	// A 3s fade past the 60% mark should be detected (not the no-fade sentinel).
	if b.FadeStartMs >= 10000 {
		t.Errorf("FadeStartMs = %d, expected a fade to be detected (< duration)", b.FadeStartMs)
	}
	if b.FadeStartMs < 6000 {
		t.Errorf("FadeStartMs = %d, must be past the 60%% mark (>= 6000)", b.FadeStartMs)
	}
	// Trailing signal drops below -60 dBFS near the end of the fade (~9s).
	if b.SilenceStartMs < 8700 || b.SilenceStartMs > 9200 {
		t.Errorf("SilenceStartMs = %d, want ~9000", b.SilenceStartMs)
	}
	// Sanity ordering.
	if b.IntroEndMs >= b.SilenceStartMs {
		t.Errorf("expected IntroEndMs(%d) < SilenceStartMs(%d)", b.IntroEndMs, b.SilenceStartMs)
	}
	if b.OutroStartMs < 5000 {
		t.Errorf("OutroStartMs = %d, must be past the 50%% mark", b.OutroStartMs)
	}
}

func TestBoundariesFromPCM_Empty(t *testing.T) {
	if b := boundariesFromPCM(nil, 8000); b != nil {
		t.Errorf("expected nil for empty input, got %+v", b)
	}
}

func TestBoundariesFromPCM_AllSilence(t *testing.T) {
	b := boundariesFromPCM(make([]float32, 8000*3), 8000)
	if b == nil {
		t.Fatal("expected non-nil for all-silence")
	}
	// No audible content: intro/silence collapse to 0, peak == 0.
	if b.IntroEndMs != 0 {
		t.Errorf("IntroEndMs = %d, want 0 for all-silence", b.IntroEndMs)
	}
	if b.SilenceStartMs != 0 {
		t.Errorf("SilenceStartMs = %d, want 0 for all-silence", b.SilenceStartMs)
	}
}
