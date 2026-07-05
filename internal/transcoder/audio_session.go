package transcoder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
)

// AudioSessionManager produces transcoded copies of audio files the browser
// can't natively decode, at a caller-chosen AAC bitrate. Output is a
// fragmented MP4 — universal browser support and small enough that storage
// isn't a concern.
//
// Single-shot per (track_file_id, profile): the first request triggers an
// ffmpeg run that writes a tempfile, sync.Map keeps duplicate requests
// waiting on the same run rather than transcoding twice. Subsequent requests
// hit the cached file directly. Different bitrates for the same source file
// are cached independently — the profile string is part of the cache key.
//
// No HLS / no segments: progressive-download with byte-range serving is
// simpler and matches how hibiki's audio engine consumes URLs. HLS audio
// is overkill for a one-format-fits-all fallback.
type AudioSessionManager struct {
	cache *CacheManager

	mu       sync.Mutex
	inflight map[string]chan error
}

func NewAudioSessionManager(cache *CacheManager) *AudioSessionManager {
	return &AudioSessionManager{
		cache:    cache,
		inflight: make(map[string]chan error),
	}
}

// DefaultAudioBitrateKbps is the historical fallback bitrate used when a
// caller doesn't ask for a specific "quality" tier — the caps-based
// decision tree in the music stream handler, and Jellyfin's
// Audio/{id}/universal endpoint, both transcode at this rate.
const DefaultAudioBitrateKbps = 256

// allowedAudioBitratesKbps is the fixed set of AAC encode bitrates the
// session manager will produce. Kept as a closed set for two reasons: (1)
// the on-disk cache filename (`<id>_aac-<kbps>.m4a`) must stay bounded and
// predictable, and (2) it mirrors the "quality" tiers documented on the
// public stream API (aac-320/256/192/128) — no arbitrary value should leak
// into path construction or ffmpeg args.
var allowedAudioBitratesKbps = map[int]bool{320: true, 256: true, 192: true, 128: true}

// IsAllowedAudioBitrate reports whether kbps is one of the supported AAC
// encode tiers. Exposed so callers validate against the same source of
// truth EnsureAAC enforces, instead of duplicating the literal set.
func IsAllowedAudioBitrate(kbps int) bool {
	return allowedAudioBitratesKbps[kbps]
}

func audioProfile(bitrateKbps int) string {
	return fmt.Sprintf("aac-%d", bitrateKbps)
}

func (m *AudioSessionManager) outputPath(trackFileID int64, profile string) string {
	dir := filepath.Join(m.cache.BaseDir(), "audio")
	_ = os.MkdirAll(dir, 0o750)
	return filepath.Join(dir, fmt.Sprintf("%d_%s.m4a", trackFileID, profile))
}

// EnsureAAC makes sure an AAC fragmented MP4 at the given bitrate exists on
// disk for the given source path + track_file_id. Returns the cached path.
// Concurrent callers for the same (id, bitrate) will all wait on a single
// ffmpeg invocation. bitrateKbps must be one of IsAllowedAudioBitrate's set.
func (m *AudioSessionManager) EnsureAAC(ctx context.Context, trackFileID int64, sourcePath string, bitrateKbps int) (string, error) {
	if !IsAllowedAudioBitrate(bitrateKbps) {
		return "", fmt.Errorf("unsupported audio bitrate: %dk", bitrateKbps)
	}

	outPath := m.outputPath(trackFileID, audioProfile(bitrateKbps))

	if _, err := os.Stat(outPath); err == nil {
		return outPath, nil
	}

	m.mu.Lock()
	if ch, ok := m.inflight[outPath]; ok {
		m.mu.Unlock()
		select {
		case err, open := <-ch:
			if !open {
				err = nil
			}
			if err != nil {
				return "", err
			}
			return outPath, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	done := make(chan error, 1)
	m.inflight[outPath] = done
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.inflight, outPath)
		m.mu.Unlock()
		close(done)
	}()

	if err := m.runFFmpegAAC(ctx, sourcePath, outPath, bitrateKbps); err != nil {
		_ = os.Remove(outPath)
		done <- err
		return "", err
	}
	done <- nil
	return outPath, nil
}

// maxFFmpegStderrTail bounds how much of ffmpeg's stderr we keep around for
// diagnostics. A failed encode normally logs a handful of lines, but a
// pathological run (corrupt source, a codec spamming warnings) could
// otherwise grow unbounded for the life of the process — stderrTail keeps
// only the most recent bytes.
const maxFFmpegStderrTail = 4 << 10 // 4 KiB

// stderrTail is an io.Writer that retains only the last max bytes written
// to it. Only one goroutine ever writes to it — os/exec copies a non-*os.File
// Stderr in its own goroutine and Cmd.Wait/Run blocks until that copy
// finishes — so no locking is needed.
type stderrTail struct {
	buf []byte
	max int
}

func newStderrTail(max int) *stderrTail {
	return &stderrTail{max: max}
}

func (t *stderrTail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *stderrTail) String() string {
	return strings.TrimSpace(string(t.buf))
}

// runFFmpegAAC invokes ffmpeg to produce a fragmented MP4 at the requested
// AAC bitrate. faststart moves the moov atom to the front so the browser can
// begin playback without buffering the whole file first; empty_moov keeps
// the file progressive-friendly.
func (m *AudioSessionManager) runFFmpegAAC(ctx context.Context, source, out string, bitrateKbps int) error {
	// Generous timeout: ffmpeg AAC encode is roughly real-time-ish on a
	// modern CPU; a 10-min track lands in 10-30s. Cap at 10 min wall clock.
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	tmp := out + ".tmp"
	cmd := exec.CommandContext(runCtx, //nolint:gosec // source comes from library_files; ffmpeg binary is fixed
		"ffmpeg",
		"-nostdin", "-nostats", "-hide_banner",
		"-y",
		"-i", source,
		"-vn", // strip any embedded cover/visual track
		"-map_metadata", "0",
		"-c:a", "aac",
		"-b:a", fmt.Sprintf("%dk", bitrateKbps),
		"-movflags", "+faststart+empty_moov+frag_keyframe",
		"-f", "mp4",
		tmp,
	)
	stderr := newStderrTail(maxFFmpegStderrTail)
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		tail := stderr.String()
		log.Error().Err(err).
			Str("component", "audio_transcode").
			Str("source", vfs.RedactPath(source)).
			Int("bitrate_kbps", bitrateKbps).
			Str("stderr", tail).
			Msg("ffmpeg aac transcode failed")
		return fmt.Errorf("ffmpeg aac transcode: %w (stderr: %s)", err, tail)
	}
	if err := os.Rename(tmp, out); err != nil {
		return fmt.Errorf("rename transcode output: %w", err)
	}
	return nil
}

// isLossyAudioFormat reports whether a track_files.format value represents
// a lossy codec. Mirrors the lossless bucket used by
// mediaprobe.RefinedQualityScore (flac/alac/wav get a bit-depth/sample-rate
// bonus there; everything else is scored by bitrate) — duplicated as a
// small switch here rather than imported, since transcoder intentionally
// has no dependency on the mediaprobe/database packages.
func isLossyAudioFormat(format string) bool {
	switch strings.ToLower(format) {
	case "flac", "alac", "wav":
		return false
	}
	return true
}

// ShouldTranscodeForTier decides, for an explicit "quality" tier request,
// whether a file that's already natively playable by the client should
// still be re-encoded to the requested AAC bitrate.
//
// Lossless sources (FLAC/ALAC/WAV) always get shaped to the tier — there's
// real headroom to trade away. For a lossy source, re-encoding "up" gains
// nothing perceptually (a 128k MP3 re-wrapped as 256k AAC is still a 128k
// MP3 with padding) — so within a small margin (+16 kbps) of the requested
// tier we report false and let the caller serve the file direct instead of
// burning CPU/disk on a transcode nobody benefits from.
func ShouldTranscodeForTier(format string, bitrateKbps, tierKbps int) bool {
	if !isLossyAudioFormat(format) {
		return true
	}
	return bitrateKbps > tierKbps+16
}

// ErrNoFFmpeg is returned when ffmpeg isn't on $PATH.
var ErrNoFFmpeg = errors.New("ffmpeg not found in PATH")
