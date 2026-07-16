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

func TestAlbumEBUR128Args(t *testing.T) {
	// Paths ride in argv untouched — no manifest, no quoting layer. The old
	// concat-demuxer manifest rune-truncated non-ASCII paths (♡ → 'a', exit
	// 254) and choked on mixed-codec albums (exit 69); both classes are
	// covered by passing paths verbatim and normalizing per input.
	paths := []string{
		"/music/ano/LoliRockyunRobo♡.flac",
		"/music/ano/ちゅ、多様性。/01. o'malley.mp3",
	}
	args := albumEBUR128Args(paths)

	for i, p := range paths {
		found := false
		for j, a := range args {
			if a == "-i" && j+1 < len(args) && args[j+1] == p {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("input %d: path %q missing from argv %q", i, p, args)
		}
	}

	fc := ""
	for j, a := range args {
		if a == "-filter_complex" && j+1 < len(args) {
			fc = args[j+1]
			break
		}
	}
	want := "[0:a:0]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo[a0];" +
		"[1:a:0]aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo[a1];" +
		"[a0][a1]concat=n=2:v=0:a=1,ebur128=peak=true"
	if fc != want {
		t.Errorf("filter_complex:\n got %q\nwant %q", fc, want)
	}
}
