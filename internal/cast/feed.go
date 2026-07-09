package cast

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
)

// pcmFormat is what every current transport consumes: cliap2's default
// input (44.1 kHz / 16-bit / stereo, signed little-endian). Receivers
// negotiate this rate in the AirPlay SETUP, so we decode everything to
// it rather than configuring per-source rates.
const (
	pcmSampleRate = 44100
	pcmChannels   = 2
)

// pcmFeeder decodes one track to raw PCM on stdout. ffmpeg is resolved
// from $PATH like everywhere else in the repo (the container symlinks
// jellyfin-ffmpeg onto the standard names).
type pcmFeeder struct {
	cmd *exec.Cmd
	out io.ReadCloser
}

// newPCMFeeder prepares (does not start) an ffmpeg decode of track from
// its StartAt offset. `-ss` before `-i` seeks on the demuxer — fast and
// accurate enough for music.
func newPCMFeeder(ctx context.Context, track TrackInfo) (*pcmFeeder, error) {
	// -re paces the decode at playback rate. Load-bearing, not an
	// optimization: cliap2's ACTION=PAUSE only stops *reading stdin* —
	// with an unthrottled feeder the entire track is already inside its
	// buffer within seconds and pause/resume become no-ops. Real-time
	// feeding keeps the in-flight buffer near the latency window so
	// intake-pause starves the player into a clean suspend (and short
	// clips can no longer hit EOF before the pre-roll ends).
	args := []string{"-hide_banner", "-loglevel", "error", "-re"}
	if track.StartAt > 0 {
		args = append(args, "-ss", strconv.Itoa(track.StartAt))
	}
	args = append(args,
		"-i", track.Path,
		"-vn", // m4a/flac cover-art streams would otherwise confuse -f s16le
		"-f", "s16le",
		"-ar", strconv.Itoa(pcmSampleRate),
		"-ac", strconv.Itoa(pcmChannels),
		"-",
	)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("cast: ffmpeg stdout pipe: %w", err)
	}
	return &pcmFeeder{cmd: cmd, out: out}, nil
}

func (f *pcmFeeder) start() error { return f.cmd.Start() }

// stop kills the decoder; safe to call after natural exit.
func (f *pcmFeeder) stop() {
	if f.cmd.Process != nil {
		_ = f.cmd.Process.Kill()
	}
	_, _ = io.Copy(io.Discard, f.out) // unblock a Wait stuck on pipe buffers
	_ = f.cmd.Wait()
}
