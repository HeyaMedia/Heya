package server

import (
	"testing"

	"github.com/karbowiak/heya/internal/transcoder"
	"github.com/stretchr/testify/require"
)

func TestTranscodeSessionStateReportsSuspendedHeadAsThrottled(t *testing.T) {
	running, state := transcodeSessionState(
		transcoder.HeadInfo{Suspended: true, StopReason: transcoder.StopReasonLeadCap},
		transcoder.ProgressStats{},
	)
	require.False(t, running)
	require.Equal(t, "throttled", state)
}
