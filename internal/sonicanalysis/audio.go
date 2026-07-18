package sonicanalysis

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os/exec"

	"github.com/karbowiak/heya/internal/vfs"
)

// decodePCM shells ffmpeg to decode `path` to a `sampleRate`-Hz mono
// float32 little-endian PCM stream and returns the samples in [-1, 1].
//
// Sample-rate choice depends on which model the caller is feeding:
//   - Discogs-EffNet: 16000 Hz
//   - LAION-CLAP HTSAT: 48000 Hz
//
// Resampling uses ffmpeg's swresample with `filter_size=128` (vs
// default 32) to widen the polyphase sinc enough that high-frequency
// content matches Essentia/libsamplerate closely.
//
// PRODUCTION TODO: the container build of ffmpeg will include
// `--enable-libsoxr`. At that point switch to
//
//	aresample=resampler=soxr:precision=28
//
// (bit-equivalent quality to libsamplerate SRC_SINC_MEDIUM_QUALITY,
// which is what Essentia uses via `MonoLoader(resampleQuality=4)`).
// Detect availability via `ffmpeg -resamplers` once at startup; fall
// back to swr+filter_size=128 if soxr isn't compiled in.
func decodePCM(ctx context.Context, path string, sampleRate int) ([]float32, error) {
	maxBytes := int64(sampleRate) * int64(MaxAnalysisDurationSeconds+60) * 4
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := vfs.ValidateLocalPath(path); err != nil {
		return nil, fmt.Errorf("audio input: %w", err)
	}
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-i", path,
		"-map", "0:a:0",
		"-af", "aresample=filter_size=128:cutoff=0.97",
		"-ac", "1",
		"-ar", fmt.Sprintf("%d", sampleRate),
		"-f", "f32le",
		"-",
	}
	// G204: args contain the audio path which originates from a library
	// scan, not user input. Binary name is hardcoded.
	cmd := exec.CommandContext(runCtx, "ffmpeg", args...) //nolint:gosec // G204: server-controlled args, hardcoded binary
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	raw, readErr := io.ReadAll(io.LimitReader(stdout, maxBytes+1))
	if int64(len(raw)) > maxBytes {
		// Cancel before Wait so a child blocked while writing the bounded stdout
		// pipe cannot hold this analysis open.
		cancel()
		_ = cmd.Wait()
		return nil, fmt.Errorf("decoded PCM exceeds %d bytes", maxBytes)
	}
	if readErr != nil {
		cancel()
		_ = cmd.Wait()
		return nil, fmt.Errorf("read ffmpeg stdout: %w", readErr)
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		return nil, fmt.Errorf("ffmpeg: %w (stderr: %s)", waitErr, stderr.String())
	}
	if len(raw)%4 != 0 {
		return nil, fmt.Errorf("ffmpeg output not float32-aligned (%d bytes)", len(raw))
	}
	out := make([]float32, len(raw)/4)
	for i := range out {
		bits := binary.LittleEndian.Uint32(raw[i*4 : i*4+4])
		out[i] = math.Float32frombits(bits)
	}
	return out, nil
}
