package cast

import "strings"

// cliap2's stderr is the transport's source of truth: its exit code and
// internal "playing" status are meaningless (the player happily plays to
// nowhere on a bad device registration — see docs/casting-research.md).
// This classifier is a pure function over log lines so the state
// machine is unit-testable against captured transcripts.

type stderrEvent int

const (
	evNone stderrEvent = iota
	// evConnected — RTSP session established, encrypted, device accepted
	// the stream setup. Line: "Callback from AirPlay 2 device <name> to
	// device_activate_cb (status 2)".
	evConnected
	// evPlayStart — audio is actually being sent. The ONLY trustworthy
	// "it plays" marker. Line: "player: event_play_start()".
	evPlayStart
	// evPaused / evResumed — confirmations of FIFO ACTION=PAUSE/PLAY
	// (also fire if playback is interrupted/restarted internally).
	evPaused
	evResumed
	// evEndOfStream — the feeder's PCM drained to EOF; the track ended
	// naturally.
	evEndOfStream
	// evRTSPClosed — the device hung up. With zero keep-alives this is
	// the +31s idle timeout, i.e. the visible symptom of a session that
	// never commenced.
	evRTSPClosed
	// evDeviceFailed — cliap2 declared the output dead ("The AirPlay 2
	// device '<name>' failed").
	evDeviceFailed
	// evNTPTooSoon — non-fatal: the start deadline was too tight and
	// initial audio will be trimmed. Supervisor should widen the lead
	// next time, not fail.
	evNTPTooSoon
	// evBufferLow — output buffer underrun warnings ("put delay
	// detected"); repeated occurrences precede an internal restart.
	evBufferLow
	// evAuthFailed — pairing/verification/cipher errors. Hard failure:
	// retrying without new credentials will not help.
	evAuthFailed
)

func classifyStderrLine(line string) stderrEvent {
	switch {
	case strings.Contains(line, "device_activate_cb (status 2)"):
		return evConnected
	case strings.Contains(line, "event_play_start()"):
		return evPlayStart
	case strings.Contains(line, "Pause at"):
		return evPaused
	case strings.Contains(line, "Restarted at"):
		return evResumed
	case strings.Contains(line, "end of stream reached"):
		return evEndOfStream
	case strings.Contains(line, "closed RTSP connection"):
		return evRTSPClosed
	case strings.Contains(line, "device") && strings.Contains(line, "' failed"):
		return evDeviceFailed
	case strings.Contains(line, "ntpstart time too soon"):
		return evNTPTooSoon
	case strings.Contains(line, "put delay detected"):
		return evBufferLow
	case strings.Contains(line, "Pair verify result error"),
		strings.Contains(line, "Ciphering setup error"),
		strings.Contains(line, "Unsupported authentication"),
		strings.Contains(line, "requires password"),
		strings.Contains(line, "Verification step"):
		return evAuthFailed
	}
	return evNone
}
