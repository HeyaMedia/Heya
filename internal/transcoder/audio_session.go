package transcoder

import (
	"context"
	"errors"
	"fmt"
	"os"
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
// Single-shot per (track_file_id, profile): the first request triggers a
// manager-owned ffmpeg run that writes a unique temporary file; duplicate
// requests wait on the same run rather than transcoding twice. Subsequent
// requests hit the cached file directly. Different bitrates for the same
// source file are cached independently — the profile string is part of the
// cache key.
//
// No HLS / no segments: progressive-download with byte-range serving is
// simpler and matches how hibiki's audio engine consumes URLs. HLS audio
// is overkill for a one-format-fits-all fallback.
type AudioSessionManager struct {
	cache *CacheManager

	runCtx context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	closed    bool
	inflight  map[string]*audioEncode
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeDone chan struct{}

	// encodeAAC is a seam for lifecycle/concurrency tests. Production uses
	// runFFmpegAAC; keeping the orchestration independent of os/exec also
	// makes its cancellation contract explicit.
	encodeAAC func(context.Context, string, string, int) error
}

// ErrAudioManagerClosed is returned when new audio work is requested after
// the manager has begun shutting down.
var ErrAudioManagerClosed = errors.New("audio session manager closed")

// audioEncode is one in-flight ffmpeg run shared by every caller asking for
// the same (track_file_id, profile). err is written exactly once, before done
// is closed, so any number of waiters observe the same outcome — a single
// buffered error value would hand the first waiter the failure and let the
// rest read a closed channel as success.
type audioEncode struct {
	done chan struct{}
	err  error
}

// AACFile is an open cached AAC output whose cache lease lasts exactly as
// long as the file handle. Closing it releases both the descriptor and the
// eviction pin, eliminating the EnsureAAC -> os.Open serving gap.
type AACFile struct {
	*os.File
	lease     *CacheLease
	closeOnce sync.Once
	closeErr  error
}

// Close is idempotent and releases the cache lease after the open descriptor
// has been closed.
func (f *AACFile) Close() error {
	if f == nil {
		return nil
	}
	f.closeOnce.Do(func() {
		if f.File != nil {
			f.closeErr = f.File.Close()
		}
		f.lease.Release()
	})
	return f.closeErr
}

func NewAudioSessionManager(cache *CacheManager) *AudioSessionManager {
	runCtx, cancel := context.WithCancel(context.Background())
	m := &AudioSessionManager{
		cache:     cache,
		runCtx:    runCtx,
		cancel:    cancel,
		inflight:  make(map[string]*audioEncode),
		closeDone: make(chan struct{}),
	}
	m.encodeAAC = m.runFFmpegAAC
	return m
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
	dir := filepath.Join(m.cache.BaseDir(), audioCacheNamespace)
	return filepath.Join(dir, fmt.Sprintf("%d_%s.m4a", trackFileID, profile))
}

// EnsureAAC makes sure an AAC fragmented MP4 at the given bitrate exists on
// disk for the given source path + track_file_id. Returns the cached path.
// Concurrent callers for the same (id, bitrate) will all wait on a single
// ffmpeg invocation. bitrateKbps must be one of IsAllowedAudioBitrate's set.
func (m *AudioSessionManager) EnsureAAC(ctx context.Context, trackFileID int64, sourcePath string, bitrateKbps int) (string, error) {
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return "", ErrAudioManagerClosed
	}
	if !IsAllowedAudioBitrate(bitrateKbps) {
		return "", fmt.Errorf("unsupported audio bitrate: %dk", bitrateKbps)
	}

	outPath := m.outputPath(trackFileID, audioProfile(bitrateKbps))

	if _, err := os.Stat(outPath); err == nil {
		// File mtime is the cache's access clock for item-level LRU.
		now := time.Now()
		_ = os.Chtimes(outPath, now, now)
		return outPath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat cached audio: %w", err)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return "", ErrAudioManagerClosed
	}
	if enc, ok := m.inflight[outPath]; ok {
		m.mu.Unlock()
		return m.waitForEncode(ctx, outPath, enc)
	}
	// Recheck after taking the manager lock. A just-finished encode may have
	// disappeared from inflight after publishing its final file between the
	// fast-path stat and this critical section.
	if _, err := os.Stat(outPath); err == nil {
		m.mu.Unlock()
		return outPath, nil
	} else if !os.IsNotExist(err) {
		m.mu.Unlock()
		return "", fmt.Errorf("stat cached audio: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o750); err != nil {
		m.mu.Unlock()
		return "", fmt.Errorf("create audio cache directory: %w", err)
	}
	enc := &audioEncode{done: make(chan struct{})}
	m.inflight[outPath] = enc
	m.wg.Add(1)
	m.mu.Unlock()

	// The manager, not the first HTTP request, owns shared work. Every caller
	// below is merely a waiter and can leave independently without cancelling
	// an encode another listener still needs.
	go m.runEncode(enc, sourcePath, outPath, bitrateKbps)
	return m.waitForEncode(ctx, outPath, enc)
}

// OpenAAC ensures an AAC output exists and opens it while holding one
// uninterrupted cache lease. Callers must Close the returned file after
// ServeContent completes. This is the serving API; EnsureAAC remains useful
// for producer orchestration/tests that only need a durable path.
func (m *AudioSessionManager) OpenAAC(ctx context.Context, trackFileID int64, sourcePath string, bitrateKbps int) (*AACFile, error) {
	if !IsAllowedAudioBitrate(bitrateKbps) {
		return nil, fmt.Errorf("unsupported audio bitrate: %dk", bitrateKbps)
	}
	outPath := m.outputPath(trackFileID, audioProfile(bitrateKbps))
	lease := m.cache.lease(outPath)

	path, err := m.EnsureAAC(ctx, trackFileID, sourcePath, bitrateKbps)
	if err != nil {
		lease.Release()
		return nil, err
	}
	file, err := os.Open(path) //nolint:gosec // path is derived from the owned cache root and numeric/profile keys
	if err != nil {
		lease.Release()
		return nil, fmt.Errorf("open cached audio: %w", err)
	}
	return &AACFile{File: file, lease: lease}, nil
}

func (m *AudioSessionManager) runEncode(enc *audioEncode, sourcePath, outPath string, bitrateKbps int) {
	defer m.wg.Done()

	err := m.encodeAAC(m.runCtx, sourcePath, outPath, bitrateKbps)
	if err != nil && m.runCtx.Err() != nil {
		err = fmt.Errorf("%w: %v", ErrAudioManagerClosed, err)
	}
	if err == nil {
		if _, statErr := os.Stat(outPath); statErr != nil {
			err = fmt.Errorf("audio encoder did not publish output: %w", statErr)
		}
	}

	// Publish the result before closing done. Channel close establishes the
	// happens-before edge that lets any number of waiters safely read enc.err.
	m.mu.Lock()
	enc.err = err
	if m.inflight[outPath] == enc {
		delete(m.inflight, outPath)
	}
	close(enc.done)
	m.mu.Unlock()
}

func (m *AudioSessionManager) waitForEncode(ctx context.Context, outPath string, enc *audioEncode) (string, error) {
	result := func() (string, error) {
		if enc.err != nil {
			return "", enc.err
		}
		return outPath, nil
	}

	// Prefer an already-published result over a simultaneous caller or manager
	// cancellation. This matters at shutdown when ffmpeg completed just before
	// the manager context was cancelled.
	select {
	case <-enc.done:
		return result()
	default:
	}

	select {
	case <-enc.done:
		return result()
	case <-ctx.Done():
		return "", ctx.Err()
	case <-m.runCtx.Done():
		select {
		case <-enc.done:
			return result()
		default:
			return "", ErrAudioManagerClosed
		}
	}
}

// Close prevents new encodes, cancels all manager-owned work, and waits for
// every encode goroutine to finish. It is idempotent; a caller whose context
// expires may call Close again later to continue waiting for cleanup.
func (m *AudioSessionManager) Close(ctx context.Context) error {
	m.closeOnce.Do(func() {
		// closed and WaitGroup.Add are synchronized by m.mu, so once this lock
		// is released no Add can race the Wait below.
		m.mu.Lock()
		m.closed = true
		m.cancel()
		m.mu.Unlock()

		go func() {
			m.wg.Wait()
			close(m.closeDone)
		}()
	})

	select {
	case <-m.closeDone:
		return nil
	default:
	}
	select {
	case <-m.closeDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
func (m *AudioSessionManager) runFFmpegAAC(ctx context.Context, source, out string, bitrateKbps int) (returnErr error) {
	// Generous timeout: ffmpeg AAC encode is roughly real-time-ish on a
	// modern CPU; a 10-min track lands in 10-30s. Cap at 10 min wall clock.
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := vfs.ValidateLocalPath(source); err != nil {
		return fmt.Errorf("audio input: %w", err)
	}

	// Reserve the tempfile and pin both names under the cache lock. Otherwise
	// the LRU walker can observe the just-created tempfile in the tiny window
	// before a separate pin call and remove it from under ffmpeg.
	m.cache.mu.Lock()
	tmp, err := reserveAtomicOutput(out)
	var pinned []string
	if err == nil {
		pinned = m.cache.pinLocked(out, tmp)
	}
	m.cache.mu.Unlock()
	if err != nil {
		return fmt.Errorf("reserve transcode output: %w", err)
	}
	releasePin := m.cache.releasePins(pinned)
	defer releasePin()
	// Remove the unpublished temporary output while it is still protected from
	// eviction; the pin is released by the previously registered defer.
	defer func() {
		if cleanupErr := removeTemporaryOutput(tmp); cleanupErr != nil {
			returnErr = errors.Join(returnErr, cleanupErr)
		}
	}()

	cmd := ffmpegCommandContext(runCtx,
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
