package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
