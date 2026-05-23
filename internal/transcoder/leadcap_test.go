package transcoder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// makeLeadCapSession builds a session with 6s segments out to a long duration
// so we can exercise the lead-cap predicate without spinning up ffmpeg.
func makeLeadCapSession(segCount int) *TranscodeSession {
	ends := make([]float64, segCount)
	for i := range ends {
		ends[i] = float64(i+1) * 6.0
	}
	return &TranscodeSession{
		TotalSegs:   segCount,
		SegmentEnds: ends,
	}
}

func TestHeadExceedsLeadCap_NoHead(t *testing.T) {
	s := makeLeadCapSession(200)
	assert.False(t, s.headExceedsLeadCap(nil))
}

func TestHeadExceedsLeadCap_WithinBudget(t *testing.T) {
	s := makeLeadCapSession(200)
	s.lastRequestedSeg = 10 // player at ~60s
	// Head at seg 40 → ~240s. Lead = 180s < 300s cap.
	head := &Head{CurrentSeg: 40}
	assert.False(t, s.headExceedsLeadCap(head))
}

func TestHeadExceedsLeadCap_AtBoundary(t *testing.T) {
	s := makeLeadCapSession(200)
	s.lastRequestedSeg = 10 // ~60s
	// Head at seg 60 → 60 * 6 = 360s start time. Lead = 360 - 60 = 300s.
	// The predicate is strict `>` so exactly 300s should NOT trip yet.
	head := &Head{CurrentSeg: 60}
	assert.False(t, s.headExceedsLeadCap(head))
}

func TestHeadExceedsLeadCap_OverCap(t *testing.T) {
	s := makeLeadCapSession(200)
	s.lastRequestedSeg = 10
	// Head at seg 61 → 366s. Lead = 306s > 300s cap.
	head := &Head{CurrentSeg: 61}
	assert.True(t, s.headExceedsLeadCap(head))
}

// When the player jumps far ahead, the head that was running falls behind.
// The lead cap should NOT fire — the player is moving away from the head,
// not the other way around.
func TestHeadExceedsLeadCap_PlayerAhead(t *testing.T) {
	s := makeLeadCapSession(200)
	s.lastRequestedSeg = 100      // player at 600s
	head := &Head{CurrentSeg: 20} // head at 120s — behind the player
	assert.False(t, s.headExceedsLeadCap(head))
}
