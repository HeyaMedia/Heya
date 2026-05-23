package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlannedSegmentTimes_WithKeyframes(t *testing.T) {
	kf := &Keyframes{
		IFrames:  []float64{0, 10.2, 20.5, 30.8, 41.0},
		Duration: 45.0,
	}
	ends := PlannedSegmentTimes(kf, 45.0, 6.0)
	// AV1-style 10s keyframes — each gap is >= 4.5s (75% of 6s)
	assert.Equal(t, []float64{10.2, 20.5, 30.8, 41.0, 45.0}, ends)
}

func TestPlannedSegmentTimes_NoKeyframes(t *testing.T) {
	ends := PlannedSegmentTimes(nil, 30.0, 6.0)
	assert.Equal(t, []float64{6, 12, 18, 24, 30}, ends)
}

func TestPlannedSegmentTimes_EmptyKeyframes(t *testing.T) {
	kf := &Keyframes{IFrames: []float64{}, Duration: 30.0}
	ends := PlannedSegmentTimes(kf, 30.0, 6.0)
	assert.Equal(t, []float64{6, 12, 18, 24, 30}, ends)
}

func TestPlannedSegmentTimes_ShortFile(t *testing.T) {
	ends := PlannedSegmentTimes(nil, 3.5, 6.0)
	assert.Equal(t, []float64{3.5}, ends)
}

func TestKeyframesToSegmentTimesBasic(t *testing.T) {
	kf := &Keyframes{
		IFrames:  []float64{0, 2.0, 4.0, 6.0, 8.0, 10.0, 12.0, 14.0, 16.0},
		Duration: 16.0,
	}
	times := KeyframesToSegmentTimes(kf, 4.0)
	assert.Equal(t, []float64{4.0, 8.0, 12.0, 16.0}, times)
}

func TestKeyframesToSegmentTimesRespectsMinDuration(t *testing.T) {
	kf := &Keyframes{
		IFrames:  []float64{0, 1.0, 2.0, 3.0, 4.0, 5.0, 6.0},
		Duration: 6.0,
	}
	times := KeyframesToSegmentTimes(kf, 3.0)
	assert.Equal(t, []float64{3.0, 6.0}, times)
}

func TestKeyframesToSegmentTimesEmpty(t *testing.T) {
	assert.Nil(t, KeyframesToSegmentTimes(nil, 4.0))
	assert.Nil(t, KeyframesToSegmentTimes(&Keyframes{}, 4.0))
}

func TestKeyframesToSegmentTimesSingle(t *testing.T) {
	kf := &Keyframes{IFrames: []float64{0, 5.0}, Duration: 5.0}
	times := KeyframesToSegmentTimes(kf, 4.0)
	assert.Equal(t, []float64{5.0}, times)
}

func TestKeyframesToSegmentTimesShortFile(t *testing.T) {
	kf := &Keyframes{IFrames: []float64{0, 1.0, 2.0}, Duration: 2.0}
	times := KeyframesToSegmentTimes(kf, 4.0)
	assert.Empty(t, times)
}

func TestAudioSegmentTimes(t *testing.T) {
	times := AudioSegmentTimes(10.0, 4.0)
	assert.Equal(t, []float64{4.0, 8.0}, times)
}

func TestAudioSegmentTimesZeroDuration(t *testing.T) {
	assert.Nil(t, AudioSegmentTimes(0, 4.0))
	assert.Nil(t, AudioSegmentTimes(-1, 4.0))
}

func TestAudioSegmentTimesDefaultInterval(t *testing.T) {
	times := AudioSegmentTimes(10.0, 0)
	assert.Equal(t, []float64{4.0, 8.0}, times)
}
