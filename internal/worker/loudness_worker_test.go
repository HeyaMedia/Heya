package worker

import (
	"math"
	"testing"
)

// Sample matches what ffmpeg 6/7 emits for `-af ebur128=peak=true -f null -`.
// Per-channel "Peak:" lines under "True peak:" let through the same way as
// loudgain handles it (first value wins).
const sampleEBUR128Output = `[Parsed_ebur128_0 @ 0x55556e6c5180] t: 218.5    M: -19.4 S: -19.0     I: -18.7 LUFS       LRA:  4.7 LU
[Parsed_ebur128_0 @ 0x55556e6c5180] Summary:

  Integrated loudness:
    I:         -14.52 LUFS
    Threshold: -24.61 LUFS

  Loudness range:
    LRA:         8.34 LU
    Threshold: -34.61 LUFS
    LRA low:   -17.61 LUFS
    LRA high:    -9.27 LUFS

  Sample peak:
    Peak:       -0.65 dBFS

  True peak:
    Peak:       -0.35 dBFS
`

func TestParseEBUR128(t *testing.T) {
	res, err := parseEBUR128(sampleEBUR128Output)
	if err != nil {
		t.Fatalf("parseEBUR128: %v", err)
	}
	cases := []struct {
		name string
		got  float64
		want float64
	}{
		{"integrated_lufs", res.IntegratedLUFS, -14.52},
		{"loudness_range_db", res.LoudnessRangeDB, 8.34},
		{"sample_peak_db", res.SamplePeakDB, -0.65},
		{"true_peak_db", res.TruePeakDB, -0.35},
	}
	for _, c := range cases {
		if math.Abs(c.got-c.want) > 0.01 {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestParseEBUR128MissingSummary(t *testing.T) {
	_, err := parseEBUR128("ffmpeg failed before summary block")
	if err == nil {
		t.Fatal("expected error when summary missing")
	}
}

func TestFFmpegConcatEscape(t *testing.T) {
	cases := map[string]string{
		"/music/track.flac":           "/music/track.flac",
		"/music/some album/01 a.flac": "/music/some album/01 a.flac",
		"/music/o'malley/track.flac":  `/music/o'\''malley/track.flac`,
	}
	for in, want := range cases {
		if got := ffmpegConcatEscape(in); got != want {
			t.Errorf("ffmpegConcatEscape(%q) = %q, want %q", in, got, want)
		}
	}
}
