package sonicanalysis

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"

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
	return float32PCM(raw), nil
}

type decodedAnalysisAudio struct {
	PCM16     []float32
	CLAPClips [][]float32
}

// decodeAnalysisAudio decodes the source once and fans it out inside one
// ffmpeg filter graph. Full-track 16 kHz PCM is streamed through stdout for
// Discogs/BPM/key/waveform work. The 48 kHz branch is written to a temporary
// raw file so Go can read only the requested ten-second CLAP windows without
// retaining an entire high-rate track in memory.
//
// positions are normalized centers in [0,1]. Duplicate windows (common on
// tracks close to or shorter than ten seconds) are collapsed.
func decodeAnalysisAudio(ctx context.Context, path string, positions []float64, includePCM16 bool) (*decodedAnalysisAudio, error) {
	if err := vfs.ValidateLocalPath(path); err != nil {
		return nil, fmt.Errorf("audio input: %w", err)
	}
	clapFile, err := os.CreateTemp("", "heya-sonic-clap-*.f32")
	if err != nil {
		return nil, fmt.Errorf("create CLAP decode buffer: %w", err)
	}
	clapPath := clapFile.Name()
	if err := clapFile.Close(); err != nil {
		_ = os.Remove(clapPath)
		return nil, fmt.Errorf("close CLAP decode buffer: %w", err)
	}
	defer func() { _ = os.Remove(clapPath) }()

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := analysisDecodeArgs(path, clapPath, includePCM16)
	// G204: args contain a validated library path and a process-owned temporary
	// path. The binary name and filter graph are hardcoded.
	cmd := exec.CommandContext(runCtx, "ffmpeg", args...) //nolint:gosec
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	var raw16 []byte
	if includePCM16 {
		stdout, pipeErr := cmd.StdoutPipe()
		if pipeErr != nil {
			return nil, fmt.Errorf("open ffmpeg analysis stdout: %w", pipeErr)
		}
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start ffmpeg analysis decode: %w", err)
		}
		max16Bytes := int64(melSampleRate) * int64(MaxAnalysisDurationSeconds+60) * 4
		raw16, err = io.ReadAll(io.LimitReader(stdout, max16Bytes+1))
		if int64(len(raw16)) > max16Bytes {
			cancel()
			_ = cmd.Wait()
			return nil, fmt.Errorf("decoded 16 kHz PCM exceeds %d bytes", max16Bytes)
		}
		if err != nil {
			cancel()
			_ = cmd.Wait()
			return nil, fmt.Errorf("read ffmpeg analysis stdout: %w", err)
		}
		err = cmd.Wait()
	} else {
		err = cmd.Run()
	}
	if err != nil {
		return nil, fmt.Errorf("ffmpeg analysis decode: %w (stderr: %s)", err, stderr.String())
	}
	if len(raw16)%4 != 0 {
		return nil, fmt.Errorf("ffmpeg 16 kHz output not float32-aligned (%d bytes)", len(raw16))
	}

	clips, err := readCLAPWindows(clapPath, positions)
	if err != nil {
		return nil, err
	}
	result := &decodedAnalysisAudio{CLAPClips: clips}
	if includePCM16 {
		result.PCM16 = float32PCM(raw16)
	}
	return result, nil
}

func analysisDecodeArgs(path, clapPath string, includePCM16 bool) []string {
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-i", path}
	const resample = "filter_size=128:cutoff=0.97"
	if !includePCM16 {
		return append(args,
			"-map", "0:a:0",
			"-af", "aresample=48000:"+resample,
			"-ac", "1",
			"-f", "f32le",
			clapPath,
		)
	}
	filter := "[0:a:0]asplit=2[src16][src48];" +
		"[src16]aresample=16000:" + resample + ",aformat=sample_fmts=flt:channel_layouts=mono[pcm16];" +
		"[src48]aresample=48000:" + resample + ",aformat=sample_fmts=flt:channel_layouts=mono[pcm48]"
	return append(args,
		"-filter_complex", filter,
		"-map", "[pcm16]", "-f", "f32le", "pipe:1",
		"-map", "[pcm48]", "-f", "f32le", clapPath,
	)
}

func readCLAPWindows(path string, positions []float64) ([][]float32, error) {
	// G304: path is the private temporary file created by
	// decodeAnalysisAudio, never a request- or library-controlled path.
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("open CLAP decode buffer: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat CLAP decode buffer: %w", err)
	}
	size := info.Size()
	maxBytes := int64(clapSampleRate) * int64(MaxAnalysisDurationSeconds+60) * 4
	if size > maxBytes {
		return nil, fmt.Errorf("decoded 48 kHz PCM exceeds %d bytes", maxBytes)
	}
	if size%4 != 0 {
		return nil, fmt.Errorf("ffmpeg 48 kHz output not float32-aligned (%d bytes)", size)
	}
	totalSamples := size / 4
	if totalSamples == 0 {
		return nil, fmt.Errorf("ffmpeg decoded zero 48 kHz samples")
	}
	if len(positions) == 0 {
		positions = []float64{0.5}
	}

	windowSamples := int64(clapClipLen)
	if totalSamples < windowSamples {
		windowSamples = totalSamples
	}
	maxStart := totalSamples - windowSamples
	seen := make(map[int64]struct{}, len(positions))
	clips := make([][]float32, 0, len(positions))
	for _, position := range positions {
		if math.IsNaN(position) || math.IsInf(position, 0) {
			return nil, fmt.Errorf("invalid CLAP window position %s", strconv.FormatFloat(position, 'g', -1, 64))
		}
		position = max(0, min(1, position))
		center := int64(math.Round(float64(totalSamples) * position))
		start := center - windowSamples/2
		start = max(int64(0), min(maxStart, start))
		if _, ok := seen[start]; ok {
			continue
		}
		seen[start] = struct{}{}

		raw := make([]byte, windowSamples*4)
		if _, err := f.ReadAt(raw, start*4); err != nil && err != io.EOF {
			return nil, fmt.Errorf("read CLAP window at sample %d: %w", start, err)
		}
		clips = append(clips, float32PCM(raw))
	}
	if len(clips) == 0 {
		return nil, fmt.Errorf("no CLAP windows selected")
	}
	return clips, nil
}

func float32PCM(raw []byte) []float32 {
	out := make([]float32, len(raw)/4)
	for i := range out {
		bits := binary.LittleEndian.Uint32(raw[i*4 : i*4+4])
		out[i] = math.Float32frombits(bits)
	}
	return out
}
