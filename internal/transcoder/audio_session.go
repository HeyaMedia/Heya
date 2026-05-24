package transcoder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// AudioSessionManager produces transcoded copies of audio files the browser
// can't natively decode. Output is a fragmented MP4 with AAC-256 audio —
// universal browser support and small enough that storage isn't a concern.
//
// Single-shot per (track_file_id, profile): the first request triggers an
// ffmpeg run that writes a tempfile, sync.Map keeps duplicate requests
// waiting on the same run rather than transcoding twice. Subsequent requests
// hit the cached file directly.
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

const audioProfileAAC256 = "aac-256"

func (m *AudioSessionManager) outputPath(trackFileID int64, profile string) string {
	dir := filepath.Join(m.cache.BaseDir(), "audio")
	_ = os.MkdirAll(dir, 0o750)
	return filepath.Join(dir, fmt.Sprintf("%d_%s.m4a", trackFileID, profile))
}

// EnsureAACMP4 makes sure an AAC-256 fragmented MP4 exists on disk for the
// given source path + track_file_id. Returns the cached path. Concurrent
// callers for the same id will all wait on a single ffmpeg invocation.
func (m *AudioSessionManager) EnsureAACMP4(ctx context.Context, trackFileID int64, sourcePath string) (string, error) {
	outPath := m.outputPath(trackFileID, audioProfileAAC256)

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

	if err := m.runFFmpegAAC256(ctx, sourcePath, outPath); err != nil {
		_ = os.Remove(outPath)
		done <- err
		return "", err
	}
	done <- nil
	return outPath, nil
}

// runFFmpegAAC256 invokes ffmpeg to produce a fragmented MP4 with AAC-256
// audio. faststart moves the moov atom to the front so the browser can begin
// playback without buffering the whole file first; empty_moov keeps the file
// progressive-friendly.
func (m *AudioSessionManager) runFFmpegAAC256(ctx context.Context, source, out string) error {
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
		"-b:a", "256k",
		"-movflags", "+faststart+empty_moov+frag_keyframe",
		"-f", "mp4",
		tmp,
	)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("ffmpeg aac transcode: %w", err)
	}
	if err := os.Rename(tmp, out); err != nil {
		return fmt.Errorf("rename transcode output: %w", err)
	}
	return nil
}

// Path returns the cache location for a given (id, profile). Used by tests
// and the cache maintenance path. Returns the path regardless of whether
// the file exists.
func (m *AudioSessionManager) Path(trackFileID int64) string {
	return m.outputPath(trackFileID, audioProfileAAC256)
}

// ErrNoFFmpeg is returned when ffmpeg isn't on $PATH.
var ErrNoFFmpeg = errors.New("ffmpeg not found in PATH")
