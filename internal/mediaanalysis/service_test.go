package mediaanalysis

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"golang.org/x/sync/singleflight"
)

// Matches ffmpeg 6/7 output for `-af ebur128=peak=true -f null -` and includes
// a progress line that must not override the final summary.
const sampleEBUR128Output = `[Parsed_ebur128_0 @ 0x55556e6c5180] t: 218.5 M: -19.4 S: -19.0 I: -18.7 LUFS LRA: 4.7 LU
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
	result, err := ParseEBUR128(sampleEBUR128Output)
	if err != nil {
		t.Fatalf("ParseEBUR128: %v", err)
	}
	tests := []struct {
		name string
		got  float64
		want float64
	}{
		{"integrated_lufs", result.IntegratedLUFS, -14.52},
		{"loudness_range_db", result.LoudnessRangeDB, 8.34},
		{"sample_peak_db", result.SamplePeakDB, -0.65},
		{"true_peak_db", result.TruePeakDB, -0.35},
	}
	for _, test := range tests {
		if math.Abs(test.got-test.want) > 0.01 {
			t.Errorf("%s: got %v, want %v", test.name, test.got, test.want)
		}
	}
}

func TestParseEBUR128MissingSummary(t *testing.T) {
	if _, err := ParseEBUR128("ffmpeg failed before summary block"); err == nil {
		t.Fatal("expected error when summary is absent")
	}
}

func TestServiceCloseWaitsForAdmittedWorkAndRejectsNewWork(t *testing.T) {
	service := New(context.Background(), nil)
	var group singleflight.Group
	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := service.run(context.Background(), &group, "work", func() (any, error) {
			close(started)
			<-release
			return nil, nil
		})
		result <- err
	}()
	<-started

	closed := make(chan struct{})
	go func() {
		service.Close()
		close(closed)
	}()
	<-service.ctx.Done()
	select {
	case <-closed:
		t.Fatal("Close returned before admitted work completed")
	default:
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatalf("admitted work failed: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not return after work completed")
	}

	if _, err := service.run(context.Background(), &group, "late", func() (any, error) {
		t.Fatal("work ran after Close")
		return nil, nil
	}); !errors.Is(err, ErrServiceClosed) {
		t.Fatalf("run after Close error = %v, want ErrServiceClosed", err)
	}
	service.Close()
}
