package transcoder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

type Head struct {
	StartSeg int
	// CurrentSeg is the last segment this head has FLUSHED to disk. A fresh
	// head that hasn't produced anything yet sits at StartSeg-1 — that
	// distinction is load-bearing for needsNewHead, which treats any unready
	// segment <= CurrentSeg as "passed, will never arrive from this head".
	CurrentSeg int
	Cancel     context.CancelFunc
	Cmd        *exec.Cmd
	Done       chan struct{}
	// Err is the terminal failure for this exact head. It is written before
	// Done is closed and must only be read after observing Done, which gives
	// readers a race-free publication edge without another mutex.
	Err error
}

var (
	// ErrInvalidSegment identifies requests outside a session's segment range.
	ErrInvalidSegment = errors.New("invalid transcode segment")
	// ErrTranscodeFailed identifies a head that could not produce the requested
	// segment. The returned error retains a more specific stage/cause.
	ErrTranscodeFailed = errors.New("transcode failed")
	// ErrTranscodeSessionClosed identifies a request interrupted by terminal
	// session cleanup rather than a seek-driven head replacement.
	ErrTranscodeSessionClosed = errors.New("transcode session closed")
)

type segReady struct {
	once sync.Once
	ch   chan struct{}
}

func newSegReady() *segReady { return &segReady{ch: make(chan struct{})} }

func (r *segReady) markReady() { r.once.Do(func() { close(r.ch) }) }

type TranscodeSession struct {
	Key         string
	FilePath    string
	OutputDir   string
	SegExt      string
	segPathFmt  string
	Duration    float64
	TotalSegs   int
	SegmentEnds []float64
	Opts        TranscodeOpts
	builder     CommandBuilder
	cacheLease  *CacheLease

	mu         sync.Mutex
	closed     bool
	head       *Head
	segments   []*segReady
	LastAccess time.Time

	// progress is the latest ffmpeg telemetry block from the running head.
	// Zero-valued (Running=false) until the head starts emitting.
	progress ProgressStats

	// lastRequestedSeg is the segment index of the most recent player
	// request. It anchors the lead-cap throttle: once the encoder runs more
	// than LeadCapSeconds ahead of this point, the head is killed to stop
	// transcoding content the player isn't likely to need. Most-recent (not
	// all-time max) so the anchor follows the player back down after a
	// backward seek.
	lastRequestedSeg int

	// headStopReason explains why the most recent head exited. "" means the
	// head is still running (or none has run yet). The UI uses this to
	// distinguish "encoder paused because we're far enough ahead" from
	// "encoder finished" or "encoder killed by user action".
	headStopReason HeadStopReason
}

// HeadStopReason classifies why an encode head exited. Surfaced via the
// status endpoint so the UI can show useful state instead of just "stopped".
type HeadStopReason string

const (
	// StopReasonRunning — head is still encoding (or none has started).
	StopReasonRunning HeadStopReason = ""
	// StopReasonLeadCap — head produced LeadCapSeconds of buffer ahead of
	// the player and was throttled. Will respawn naturally on the next
	// segment request past where the head left off.
	StopReasonLeadCap HeadStopReason = "lead_cap"
	// StopReasonCompleted — head encoded into already-completed territory.
	StopReasonCompleted HeadStopReason = "completed"
	// StopReasonKilled — head was cancelled by killHead() (seek / shutdown).
	StopReasonKilled HeadStopReason = "killed"
	// StopReasonExited — head process exited on its own (EOF / error).
	StopReasonExited HeadStopReason = "exited"
)

// LeadCapSeconds is how far ahead of the most-recently-requested segment the
// encoder is allowed to run. When exceeded, the running head is cancelled.
// A new head spawns as soon as the player asks for a segment past where the
// old head left off (the existing seek-ahead path handles this naturally).
//
// 5 minutes is comfortable headroom for hls.js's default buffer (60s) plus
// quality-switch / seek overshoot, without burning encoder time on content
// the user may never reach.
const LeadCapSeconds = 300.0

// ProgressSnapshot returns a copy of the latest ffmpeg progress block. Safe
// for concurrent reads. UpdatedAt is the zero time when no data has arrived yet.
func (s *TranscodeSession) ProgressSnapshot() ProgressStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress
}

// HeadInfo describes the currently-running encode head (or the most recent
// one if it's stopped). StopReason is StopReasonRunning while the head is
// alive; on exit it's set by the head goroutine before Done is closed.
type HeadInfo struct {
	Running    bool
	StartSeg   int
	CurrentSeg int
	StopReason HeadStopReason
}

// HeadSnapshot returns information about the head (current or most-recent).
// Running reflects whether the head goroutine is still active. StopReason is
// only meaningful when Running is false.
func (s *TranscodeSession) HeadSnapshot() HeadInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.head == nil {
		return HeadInfo{StopReason: s.headStopReason}
	}
	running := true
	select {
	case <-s.head.Done:
		running = false
	default:
	}
	cur := s.head.CurrentSeg
	if cur < s.head.StartSeg {
		// Fresh head, nothing flushed yet — report its start position
		// instead of the internal StartSeg-1 sentinel.
		cur = s.head.StartSeg
	}
	return HeadInfo{
		Running:    running,
		StartSeg:   s.head.StartSeg,
		CurrentSeg: cur,
		StopReason: s.headStopReason,
	}
}

// LastRequestedSegment returns the highest segment index any client has
// requested so far. Used by the status endpoint to show how far behind the
// player the encoder is.
func (s *TranscodeSession) LastRequestedSegment() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRequestedSeg
}

// ReadySegmentCount counts segments that have been marked ready.
func (s *TranscodeSession) ReadySegmentCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, sg := range s.segments {
		if sg != nil && isClosed(sg.ch) {
			count++
		}
	}
	return count
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func (s *TranscodeSession) Touch() {
	s.mu.Lock()
	s.LastAccess = time.Now()
	s.mu.Unlock()
}

func (s *TranscodeSession) SegmentPath(index int) string {
	return filepath.Join(s.OutputDir, fmt.Sprintf(s.segPathFmt, index))
}

func (s *TranscodeSession) SegmentStartTime(idx int) float64 {
	if idx <= 0 || idx > len(s.SegmentEnds) {
		return 0
	}
	return s.SegmentEnds[idx-1]
}

func (s *TranscodeSession) InitSegmentPath() string {
	return filepath.Join(s.OutputDir, "init.mp4")
}

func (s *TranscodeSession) HasInitSegment() bool {
	_, err := os.Stat(s.InitSegmentPath())
	return err == nil
}

func (s *TranscodeSession) IsFMP4() bool {
	return s.SegExt == ".m4s"
}

// segmentReadyChan returns the ready latch for a segment under the session
// mutex. Callers must go through this instead of touching s.segments directly:
// resetSegment swaps latch pointers at runtime, so unsynchronized slice reads
// would race.
func (s *TranscodeSession) segmentReadyChan(index int) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.segments[index].ch
}

func (s *TranscodeSession) IsSegmentReady(index int) bool {
	if index < 0 || index >= s.TotalSegs {
		return false
	}
	select {
	case <-s.segmentReadyChan(index):
		return true
	default:
		return false
	}
}

func (s *TranscodeSession) WaitForSegment(ctx context.Context, index int) bool {
	if index < 0 || index >= s.TotalSegs {
		return false
	}
	select {
	case <-s.segmentReadyChan(index):
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *TranscodeSession) segmentFileExists(idx int) bool {
	_, err := os.Stat(s.SegmentPath(idx))
	return err == nil
}

// resetSegment replaces a segment's ready latch with a fresh one. Used when a
// segment is marked ready but its file has vanished from disk (cache eviction,
// manual deletion): the closed latch would otherwise make every request serve
// a 404 forever with no way to trigger a re-encode.
func (s *TranscodeSession) resetSegment(idx int) {
	if idx < 0 || idx >= s.TotalSegs {
		return
	}
	s.mu.Lock()
	// Multiple requests can discover the same vanished file concurrently.
	// Only the first may replace the closed latch; replacing its fresh latch a
	// second time could strand a waiter on a channel the encoder will never
	// close.
	if isClosed(s.segments[idx].ch) {
		s.segments[idx] = newSegReady()
	}
	s.mu.Unlock()
}

// EnsureSegment starts or follows the appropriate encode head and waits for
// idx to become durable on disk. Unlike the historical boolean API, terminal
// command construction/start/exit failures are returned immediately instead
// of leaving the request blocked until its HTTP context expires.
func (s *TranscodeSession) EnsureSegment(ctx context.Context, idx int) error {
	if idx < 0 || idx >= s.TotalSegs {
		return fmt.Errorf("%w: %d", ErrInvalidSegment, idx)
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrTranscodeSessionClosed
	}
	s.lastRequestedSeg = idx
	s.mu.Unlock()

	if s.IsSegmentReady(idx) {
		if s.segmentFileExists(idx) {
			return nil
		}
		s.resetSegment(idx)
	}

	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return ErrTranscodeSessionClosed
		}
		for s.needsNewHead(idx) {
			if s.head != nil {
				// killHead drops s.mu while waiting for the head goroutine, so a
				// concurrent request may install its own head in that window.
				// Re-evaluate instead of spawning a second head over it.
				s.killHead()
				continue
			}
			s.spawnHead(idx)
		}
		head := s.head
		ready := s.segments[idx].ch
		s.mu.Unlock()

		select {
		case <-ready:
			if s.segmentFileExists(idx) {
				return nil
			}
			s.resetSegment(idx)
			continue

		case <-head.Done:
			// ffmpeg can flush the requested file immediately before exiting.
			// Always let a durable segment win over a terminal process result.
			if s.IsSegmentReady(idx) && s.segmentFileExists(idx) {
				return nil
			}
			if s.IsSegmentReady(idx) {
				s.resetSegment(idx)
			}

			s.mu.Lock()
			current := s.head
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return ErrTranscodeSessionClosed
			}
			if current != head {
				// A concurrent seek deliberately replaced this head. Follow the
				// replacement rather than reporting the obsolete cancellation.
				continue
			}
			if head.Err != nil {
				return head.Err
			}
			return fmt.Errorf("%w: encoder exited before segment %d", ErrTranscodeFailed, idx)

		case <-ctx.Done():
			// Prefer a segment that became durable concurrently with caller
			// cancellation; serving it is harmless and avoids a false failure.
			if s.IsSegmentReady(idx) && s.segmentFileExists(idx) {
				return nil
			}
			return ctx.Err()
		}
	}
}

// RequestSegment preserves the original boolean contract for callers that
// have not yet migrated to EnsureSegment. New integrations should retain the
// returned error so they can distinguish invalid requests, cancellation, and
// encoder failure.
func (s *TranscodeSession) RequestSegment(ctx context.Context, idx int) bool {
	return s.EnsureSegment(ctx, idx) == nil
}

// headExceedsLeadCap reports whether the encoder is running far enough ahead
// of the most-recently-requested segment to be wasted work. Compares in
// seconds because segment lengths are non-uniform on the copy-video path
// (keyframe-aligned), so "N segments ahead" wouldn't be a stable threshold.
// Callers must hold s.mu.
func (s *TranscodeSession) headExceedsLeadCap(head *Head) bool {
	if head == nil {
		return false
	}
	headTime := s.SegmentStartTime(head.CurrentSeg)
	reqTime := s.SegmentStartTime(s.lastRequestedSeg)
	return headTime-reqTime > LeadCapSeconds
}

const seekThresholdSegs = 10

func (s *TranscodeSession) needsNewHead(idx int) bool {
	if s.head == nil {
		return true
	}
	select {
	case <-s.head.Done:
		return true
	default:
	}
	// Only consulted for segments that are NOT ready (RequestSegment checks
	// readiness first). A head encodes strictly forward and CurrentSeg is its
	// last FLUSHED segment (StartSeg-1 when fresh), so an unready segment at
	// or behind CurrentSeg will never be produced by this head — whether it's
	// a backward seek behind the run's start or a segment whose file vanished
	// after the head passed it (reset latch). The head's own next segment is
	// CurrentSeg+1, which correctly falls through to the distance check.
	if idx <= s.head.CurrentSeg {
		return true
	}
	return idx-s.head.CurrentSeg > seekThresholdSegs
}

func (s *TranscodeSession) setHeadCurrent(head *Head, seg int) {
	s.mu.Lock()
	head.CurrentSeg = seg
	s.mu.Unlock()
}

func (s *TranscodeSession) headCurrent(head *Head) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return head.CurrentSeg
}

// killHead must be called with s.mu held. It detaches the head pointer,
// drops the mutex while waiting for the head goroutine to finish, then
// re-acquires the mutex. Dropping the mutex is essential to avoid deadlocks
// with the head goroutine, which may take s.mu (e.g. for Touch) before
// signalling Done.
func (s *TranscodeSession) killHead() {
	head := s.head
	s.head = nil
	if head == nil {
		return
	}
	// Mark as killed BEFORE we drop the mutex, so subsequent status reads
	// see the right reason even if the head goroutine sets its own first.
	s.headStopReason = StopReasonKilled
	head.Cancel()
	s.mu.Unlock()
	<-head.Done
	s.mu.Lock()
}

func (s *TranscodeSession) spawnHead(startSeg int) {
	opts := s.Opts
	opts.StartTime = s.SegmentStartTime(startSeg)
	opts.StartSegment = startSeg
	opts.OutputDir = s.OutputDir

	ctx, cancel := context.WithCancel(context.Background())
	head := &Head{
		StartSeg: startSeg,
		// Nothing flushed yet. Starting at startSeg would make needsNewHead
		// see the head's own first segment as already-passed and kill/spawn
		// in an infinite loop on the very request that spawned it.
		CurrentSeg: startSeg - 1,
		Cancel:     cancel,
		Done:       make(chan struct{}),
	}
	s.head = head
	s.headStopReason = StopReasonRunning

	go s.runHead(ctx, head, opts)
}

func (s *TranscodeSession) runHead(ctx context.Context, head *Head, opts TranscodeOpts) {
	var terminalErr error
	defer func() {
		// All exits — including command-build and process-start failures — must
		// publish a terminal result before waking segment waiters.
		s.setStopReasonIfRunning(head, StopReasonExited)
		head.Err = terminalErr
		close(head.Done)
	}()

	label := fmt.Sprintf("head_%d", head.StartSeg)
	// Explicit cache clears intentionally ignore leases. Recreate this still-
	// leased session directory before every head so playback can recover on
	// the next segment request instead of leaving ffmpeg pointed at a vanished
	// OutputDir.
	if err := os.MkdirAll(s.OutputDir, 0o750); err != nil {
		log.Error().Err(err).Str("key", s.Key).Msg("recreate transcode output directory")
		if ctx.Err() == nil {
			terminalErr = transcodeHeadError("create output directory", err)
		}
		return
	}
	if err := vfs.ValidateLocalPath(s.FilePath); err != nil {
		log.Error().Err(err).Str("key", s.Key).Msg("open media input for head")
		if ctx.Err() == nil {
			terminalErr = transcodeHeadError("open input", err)
		}
		return
	}
	opts.Input = s.FilePath

	cmd, err := s.builder.BuildHLSCommand(ctx, opts)
	if err != nil {
		log.Error().Err(err).Str("key", s.Key).Msg(label + " build command failed")
		if ctx.Err() == nil {
			terminalErr = transcodeHeadError("build command", err)
		}
		return
	}
	if cmd == nil {
		terminalErr = transcodeHeadError("build command", errors.New("builder returned nil command"))
		return
	}
	head.Cmd = cmd

	if err := os.WriteFile(filepath.Join(s.OutputDir, label+"_cmd.txt"),
		[]byte(s.builder.FormatCommand(cmd)+"\n"), 0o600); err != nil {
		log.Debug().Err(err).Str("key", s.Key).Msg("write ffmpeg command debug file")
	}

	logFilePath := filepath.Join(s.OutputDir, label+"_ffmpeg.log")
	logFile, logFileErr := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // generated cache path
	if logFileErr != nil {
		log.Debug().Err(logFileErr).Str("key", s.Key).Msg("create ffmpeg debug log")
	}
	stderr := newStderrTail(maxFFmpegStderrTail)

	if logFile != nil {
		cmd.Stderr = io.MultiWriter(logFile, stderr)
	} else {
		cmd.Stderr = stderr
	}

	// Capture -progress output on FD 3. ffmpeg writes structured key=value
	// blocks here that we parse for live fps/speed/bitrate telemetry.
	progressR, progressW, perr := os.Pipe()
	if perr != nil {
		log.Warn().Err(perr).Str("key", s.Key).Msg("progress pipe failed; continuing without telemetry")
		progressR = nil
		progressW = nil
	} else {
		cmd.ExtraFiles = append(cmd.ExtraFiles, progressW)
	}

	// Mark the session running and reset stats for this head.
	startedAt := time.Now()
	s.mu.Lock()
	s.progress = ProgressStats{Running: true, StartedAt: startedAt}
	s.mu.Unlock()

	if progressR != nil {
		go func() {
			defer func() { _ = progressR.Close() }()
			progressReader(progressR, startedAt, func(apply func(*ProgressStats)) {
				s.mu.Lock()
				apply(&s.progress)
				s.mu.Unlock()
			})
		}()
	}

	log.Info().
		Str("key", s.Key).
		Str("file", vfs.RedactPath(s.FilePath)).
		Int("start_seg", head.StartSeg).
		Float64("start_time", opts.StartTime).
		Int("audio", opts.AudioTrack).
		Str("video_codec", opts.Profile.VideoCodec).
		Str("audio_codec", opts.Profile.AudioCodec).
		Bool("fmp4", s.IsFMP4()).
		Str("debug_dir", s.OutputDir).
		Msg(label + " starting")

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			if logFile != nil {
				_ = logFile.Close()
			}
			// Closing the write end signals EOF to progressReader. Errors are
			// fine — pipe may already be closed by the child process exiting.
			if progressW != nil {
				_ = progressW.Close()
			}
			// Mark progress as not-running on head exit so the UI can show
			// "idle" instead of stale numbers.
			s.mu.Lock()
			s.progress.Running = false
			s.mu.Unlock()
		})
	}
	defer cleanup()

	logExit := func(cmdErr error) {
		if cmdErr != nil && ctx.Err() == nil {
			exitCode := 0
			if exitErr, ok := cmdErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			log.Warn().Err(cmdErr).
				Str("key", s.Key).
				Int("exit_code", exitCode).
				Str("stderr", strings.TrimSpace(stderr.String())).
				Str("ffmpeg_log", logFilePath).
				Msg(label + " failed")
		} else {
			log.Info().Str("key", s.Key).Int("last_seg", s.headCurrent(head)).Msg(label + " finished")
		}
	}

	terminalErr = s.runHeadWatch(ctx, head, cmd, cleanup, logExit)
}

func transcodeHeadError(stage string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrTranscodeFailed, stage)
	}
	// Multiple %w verbs let callers match both the stable category and a
	// builder/process-specific cause when that is useful to internal tests.
	return fmt.Errorf("%w: %s: %w", ErrTranscodeFailed, stage, err)
}

// setStopReason records a reason only while head is still the session's
// current head. An obsolete head can finish after a concurrent seek installs
// its replacement and must not overwrite the replacement's running status.
func (s *TranscodeSession) setStopReason(head *Head, r HeadStopReason) {
	s.mu.Lock()
	if s.head == head {
		s.headStopReason = r
	}
	s.mu.Unlock()
}

// setStopReasonIfRunning only writes if the reason is still "running",
// preventing a generic-exit reason from clobbering a more specific one
// already set by the loop (lead cap, completed, killed).
func (s *TranscodeSession) setStopReasonIfRunning(head *Head, r HeadStopReason) {
	s.mu.Lock()
	if s.head == head && s.headStopReason == StopReasonRunning {
		s.headStopReason = r
	}
	s.mu.Unlock()
}

// runHeadWatch starts ffmpeg and tracks segment production by watching the
// output directory. Both delivery formats run the hls muxer with
// `-hls_flags temp_file`: segments are written to seg_N.<ext>.tmp and renamed
// when fully flushed, so a final-named file appearing is complete and
// servable — requests can never observe a half-written segment.
func (s *TranscodeSession) runHeadWatch(ctx context.Context, head *Head, cmd *exec.Cmd, cleanup func(), logExit func(error)) error {
	if err := cmd.Start(); err != nil {
		log.Error().Err(err).Str("key", s.Key).Msg("ffmpeg start failed")
		cleanup()
		if ctx.Err() != nil {
			return nil
		}
		return transcodeHeadError("start encoder", err)
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	watcher, werr := fsnotify.NewWatcher()
	if werr == nil {
		if err := watcher.Add(s.OutputDir); err != nil {
			log.Warn().Err(err).Str("key", s.Key).Msg("watch transcode output directory; polling only")
			if closeErr := watcher.Close(); closeErr != nil {
				log.Debug().Err(closeErr).Str("key", s.Key).Msg("close unused transcode watcher")
			}
			watcher = nil
		} else {
			defer func() {
				if err := watcher.Close(); err != nil {
					log.Debug().Err(err).Str("key", s.Key).Msg("close transcode watcher")
				}
			}()
		}
	} else {
		log.Warn().Err(werr).Str("key", s.Key).Msg("fsnotify watcher unavailable, polling only")
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		var fsEvents <-chan fsnotify.Event
		var fsErrs <-chan error
		if watcher != nil {
			fsEvents = watcher.Events
			fsErrs = watcher.Errors
		}

		select {
		case <-ctx.Done():
			cmdErr := <-waitDone
			s.reconcileSegmentsFromFS(head)
			// Reason was set by whoever called Cancel (lead cap / completed /
			// killHead). Default to "exited" only if nothing did.
			s.setStopReasonIfRunning(head, StopReasonExited)
			cleanup()
			logExit(cmdErr)
			return nil

		case cmdErr := <-waitDone:
			s.reconcileSegmentsFromFS(head)
			s.setStopReasonIfRunning(head, StopReasonExited)
			cleanup()
			logExit(cmdErr)
			if ctx.Err() != nil {
				return nil
			}
			if cmdErr != nil {
				return transcodeHeadError("encoder exited", cmdErr)
			}
			return nil

		case ev := <-fsEvents:
			if !ev.Has(fsnotify.Create) && !ev.Has(fsnotify.Rename) {
				continue
			}
			name := filepath.Base(ev.Name)
			if !strings.HasSuffix(name, s.SegExt) {
				continue
			}
			idx := parseSegIdx(name)
			if idx < 0 {
				continue
			}
			// temp_file semantics: a final-named segment is fully flushed.
			s.markSegmentReady(idx)
			s.setHeadCurrent(head, idx)
			if idx > head.StartSeg && s.segmentAlreadyDone(idx+1) {
				log.Info().Str("key", s.Key).Int("seg", idx).Msg("head reached completed territory, stopping")
				s.setStopReason(head, StopReasonCompleted)
				head.Cancel()
			}
			// Lead-cap throttle (fMP4 path). Mirrors the TS path above.
			s.mu.Lock()
			exceeded := s.headExceedsLeadCap(head)
			lastReq := s.lastRequestedSeg
			s.mu.Unlock()
			if exceeded {
				log.Info().
					Str("key", s.Key).
					Int("seg", idx).
					Int("last_requested", lastReq).
					Float64("lead_cap_seconds", LeadCapSeconds).
					Msg("head exceeded lead cap, stopping")
				s.setStopReason(head, StopReasonLeadCap)
				head.Cancel()
			}

		case <-fsErrs:
			// ignore watcher errors; ticker polling will catch up

		case <-ticker.C:
			s.reconcileSegmentsFromFS(head)
		}
	}
}

// reconcileSegmentsFromFS scans the output directory and marks every segment
// whose file exists as ready. ffmpeg runs with hls_flags temp_file, so any
// final-named segment on disk is fully flushed (in-progress files carry a
// .tmp suffix and are skipped by the extension check).
//
// Only files actually present may be marked: the directory accumulates
// disjoint ranges from previous heads (earlier seek targets), so filling the
// whole span from StartSeg to the highest index on disk would mark
// never-encoded gap segments as ready — requests for those then 404 forever
// with no head respawn, dead-ending playback after a backward seek.
func (s *TranscodeSession) reconcileSegmentsFromFS(head *Head) {
	entries, err := os.ReadDir(s.OutputDir)
	if err != nil {
		return
	}
	present := make(map[int]bool, len(entries))
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, s.SegExt) {
			continue
		}
		idx := parseSegIdx(n)
		if idx < 0 {
			continue
		}
		present[idx] = true
		s.markSegmentReady(idx)
	}
	// Advance the head cursor along its own contiguous run only. The highest
	// index on disk may belong to an older head far ahead of this one; using
	// it would fake forward progress, breaking seek detection and tripping
	// the lead cap on a head that just spawned behind the player.
	cur := head.StartSeg
	for present[cur] {
		cur++
	}
	last := cur - 1
	if last >= head.StartSeg {
		s.mu.Lock()
		if last > head.CurrentSeg {
			head.CurrentSeg = last
		}
		s.mu.Unlock()
	}
}

// markSegmentReady is safe to call concurrently. The latch pointer is read
// under s.mu (it can be swapped by resetSegment); sync.Once ensures the
// channel is closed exactly once even under races.
func (s *TranscodeSession) markSegmentReady(idx int) {
	if idx < 0 || idx >= s.TotalSegs {
		return
	}
	s.mu.Lock()
	sg := s.segments[idx]
	s.mu.Unlock()
	sg.markReady()
}

func (s *TranscodeSession) segmentAlreadyDone(idx int) bool {
	return s.IsSegmentReady(idx)
}

func (s *TranscodeSession) Kill() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.killHead()
}

func parseSegIdx(name string) int {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	parts := strings.Split(name, "_")
	if len(parts) >= 2 {
		var n int
		if _, err := fmt.Sscanf(parts[len(parts)-1], "%d", &n); err == nil {
			return n
		}
	}
	return -1
}

// SessionManager

type SessionManager struct {
	cache   *CacheManager
	hwAccel *HwAccelProvider // resolves lazily on first HWAccel() call
	builder CommandBuilder

	mu          sync.Mutex
	closed      bool
	sessions    map[string]*TranscodeSession
	createWG    sync.WaitGroup     // admitted under mu; Close sets closed before Wait
	creating    singleflight.Group // deduplicate concurrent creation for the same session key
	cleanupStop chan struct{}
	cleanupDone chan struct{}
	cleanupOnce sync.Once
	closeOnce   sync.Once
}

func NewSessionManager(cache *CacheManager, hwAccel *HwAccelProvider, builder CommandBuilder) *SessionManager {
	sm := &SessionManager{
		cache:       cache,
		hwAccel:     hwAccel,
		builder:     builder,
		sessions:    make(map[string]*TranscodeSession),
		cleanupStop: make(chan struct{}),
		cleanupDone: make(chan struct{}),
	}
	go sm.cleanupLoop()
	return sm
}

func (m *SessionManager) Close() {
	if m == nil {
		return
	}
	m.closeOnce.Do(func() {
		// Publish the terminal state and detach every visible session before
		// waiting on process teardown. A createSession already computing outside
		// this lock will observe closed before it can reserve a cache directory or
		// publish itself into the map.
		m.mu.Lock()
		m.closed = true
		sessions := make([]*TranscodeSession, 0, len(m.sessions))
		for key, session := range m.sessions {
			delete(m.sessions, key)
			sessions = append(sessions, session)
		}
		m.mu.Unlock()

		m.cleanupOnce.Do(func() { close(m.cleanupStop) })
		<-m.cleanupDone
		// A create may already have published its replacement and be reaping the
		// evicted ffmpeg outside m.mu. Join it before declaring process/cache
		// ownership fully closed.
		m.createWG.Wait()

		m.disposeDetachedSessions(sessions)
	})
}

func FormatKey(fileID int64, audioTrack int, sessionID string) string {
	if sessionID != "" {
		return fmt.Sprintf("%d:a%d:%s", fileID, audioTrack, sessionID)
	}
	return fmt.Sprintf("%d:a%d", fileID, audioTrack)
}

func (m *SessionManager) HWAccel() HwAccelConfig {
	return m.hwAccel.Get()
}

// ConfigureHWAccel applies a new mode to sessions created from this point on.
// Running sessions already hold an immutable resolved config.
func (m *SessionManager) ConfigureHWAccel(configured string) {
	if m != nil && m.hwAccel != nil {
		m.hwAccel.Configure(configured)
	}
}

func (m *SessionManager) GetExisting(fileID int64) *TranscodeSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := fmt.Sprintf("%d:", fileID)
	for _, s := range m.sessions {
		if strings.HasPrefix(s.Key, prefix) {
			return s
		}
	}
	return nil
}

// GetExistingSession resolves the exact HLS session selected by the manifest
// query. Segment URLs carry audio + sid so concurrent users (or two quality
// choices for the same file) cannot accidentally read another session's
// output merely because they share a file ID.
func (m *SessionManager) GetExistingSession(fileID int64, audioTrack int, sessionID string) *TranscodeSession {
	return m.existingSession(FormatKey(fileID, audioTrack, sessionID))
}

// computeCopyVideoSegmentEnds determines HLS segment boundaries for a
// copy-video session (video is stream-copied, so cuts can only land on
// existing keyframes — the same constraint applies whether delivery is fMP4
// or MPEG-TS). scan_keyframes persists muxer-exact boundaries in the keyframe
// artifact. Legacy/unanalysed rows fall back immediately to the keyframe
// heuristic; playback never scans the complete media file synchronously.
func computeCopyVideoSegmentEnds(duration float64, kf *Keyframes) []float64 {
	if HasExactHLSBoundaries(kf) {
		return append([]float64(nil), kf.HLSSegmentEnds...)
	}
	return PlannedSegmentTimes(kf, duration, SegmentDuration)
}

func (m *SessionManager) GetOrCreate(ctx context.Context, fileID int64, filePath string, opts TranscodeOpts, sessionID string, duration float64, kf *Keyframes) *TranscodeSession {
	key := FormatKey(fileID, opts.AudioTrack, sessionID)

	if s := m.existingSession(key); s != nil {
		return s
	}

	// The playlist, init-segment and first media-segment requests arrive
	// concurrently. Coalesce them so they all share one resulting session.
	value, _, _ := m.creating.Do(key, func() (any, error) {
		if !m.beginCreate() {
			// Preserve the historical return shape after Close. createSession sees
			// the terminal flag and builds only an unpublished closed value.
			return m.createSession(ctx, fileID, key, filePath, opts, duration, kf), nil
		}
		defer m.createWG.Done()
		return m.createSession(ctx, fileID, key, filePath, opts, duration, kf), nil
	})
	return value.(*TranscodeSession)
}

func (m *SessionManager) beginCreate() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	m.createWG.Add(1)
	return true
}

func (m *SessionManager) createSession(ctx context.Context, fileID int64, key, filePath string, opts TranscodeOpts, duration float64, kf *Keyframes) *TranscodeSession {
	// A session may have appeared between GetOrCreate's fast-path check and
	// this singleflight function starting.
	if s := m.existingSession(key); s != nil {
		return s
	}

	// Compute segment boundaries outside the manager lock. This is now only a
	// cheap read of persisted analysis (or the in-memory heuristic fallback),
	// but keeping construction outside the lock also lets unrelated files
	// create sessions concurrently.
	var ends []float64
	if opts.Profile.VideoCodec == "copy" {
		ends = computeCopyVideoSegmentEnds(duration, kf)
	} else {
		ends = fixedIntervalBoundaries(duration, SegmentDuration)
	}
	totalSegs := len(ends)
	if totalSegs < 1 {
		ends = []float64{1}
		totalSegs = 1
	}

	segments := make([]*segReady, totalSegs)
	for i := range segments {
		segments[i] = newSegReady()
	}

	segExt := ".ts"
	segPathFmt := "seg_%04d.ts"
	if opts.UseFMP4 {
		segExt = ".m4s"
		segPathFmt = "seg_%d.m4s"
	}

	session := &TranscodeSession{
		Key:         key,
		FilePath:    filePath,
		SegExt:      segExt,
		segPathFmt:  segPathFmt,
		Duration:    duration,
		TotalSegs:   totalSegs,
		SegmentEnds: ends,
		Opts:        opts,
		builder:     m.builder,
		segments:    segments,
		LastAccess:  time.Now(),
	}

	m.mu.Lock()
	// Re-check: a concurrent caller may have raced us to create this exact
	// session while we were computing boundaries without the lock held.
	if s, ok := m.sessions[key]; ok {
		s.Touch()
		m.mu.Unlock()
		return s
	}
	if m.closed {
		// The manager may have closed while this singleflight call was computing
		// segment boundaries. Return a terminal, unpublished session: callers
		// already in flight receive the normal typed closed-session error, and no
		// cache directory or ffmpeg process can outlive SessionManager.Close.
		session.closed = true
		m.mu.Unlock()
		return session
	}
	// Heya deliberately admits only one live HLS session per source file for
	// now. Session IDs make segment/status routing exact, but they are supplied
	// by the client and therefore must not silently become an unbounded ffmpeg
	// concurrency control. A future multi-viewer implementation should replace
	// this eviction with explicit global/per-principal admission.
	prefix := fmt.Sprintf("%d:", fileID)
	var evicted []*TranscodeSession
	for existingKey, existing := range m.sessions {
		if strings.HasPrefix(existingKey, prefix) {
			delete(m.sessions, existingKey)
			evicted = append(evicted, existing)
		}
	}
	// Directory creation and the eviction pin are one CacheManager critical
	// section, nested inside the map insertion critical section. The session's
	// OutputDir is therefore protected before it becomes visible to callers
	// and stays protected for its entire map lifetime.
	lease, reserveErr := m.cache.reserveSegmentDir(key)
	session.OutputDir = lease.Path()
	session.cacheLease = lease
	m.sessions[key] = session
	m.mu.Unlock()

	m.disposeDetachedSessions(evicted)
	if reserveErr != nil {
		// Preserve the historical constructor contract (GetOrCreate returns a
		// session); runHead retries mkdir and reports a typed transcode error if
		// the storage failure is persistent.
		log.Warn().Err(reserveErr).Str("key", key).Msg("reserve transcode output directory")
	}

	log.Info().
		Str("key", key).
		Str("file", vfs.RedactPath(filePath)).
		Int("total_segs", totalSegs).
		Float64("duration", duration).
		Bool("fmp4", opts.UseFMP4).
		Bool("keyframes", kf != nil).
		Msg("session created")

	return session
}

// disposeDetachedSessions finishes sessions already removed from m.sessions.
// Process reaping stays outside m.mu, but same-key directory removal must be
// atomic with createSession publication: a client can retry an evicted sid
// while Kill waits, recreating the exact key-derived cache path.
func (m *SessionManager) disposeDetachedSessions(detached []*TranscodeSession) {
	for _, session := range detached {
		session.Kill()
	}
	if len(detached) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, session := range detached {
		current, recreated := m.sessions[session.Key]
		if !recreated || current.OutputDir != session.OutputDir {
			_ = os.RemoveAll(session.OutputDir)
		}
		// A same-key replacement has its own incremented cache pin, so releasing
		// this exact old lease cannot expose the replacement to LRU eviction.
		session.cacheLease.Release()
	}
}

// existingSession returns the already-live session for key, if any, touching
// its LastAccess. Split out of GetOrCreate so the fast, common "already
// playing" path takes the manager lock only briefly, before the (potentially
// slow) boundary computation for a genuinely new session.
func (m *SessionManager) existingSession(key string) *TranscodeSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[key]; ok {
		s.Touch()
		return s
	}
	return nil
}

func (m *SessionManager) cleanupLoop() {
	defer close(m.cleanupDone)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
		case <-m.cleanupStop:
			return
		}
		var idle []*TranscodeSession
		m.mu.Lock()
		for key, s := range m.sessions {
			s.mu.Lock()
			idleFor := time.Since(s.LastAccess)
			s.mu.Unlock()

			if idleFor > 2*time.Minute {
				log.Info().Str("key", key).Dur("idle", idleFor).Msg("cleaning up idle session")
				delete(m.sessions, key)
				idle = append(idle, s)
			}
		}
		m.mu.Unlock()

		m.disposeDetachedSessions(idle)

		// Enforce the on-disk cache size cap. Without this the configured
		// maxSizeGB was never applied (EvictLRU had no caller), so the
		// transcoded-audio dir grew until the disk filled on a long-uptime pod.
		// Run outside m.mu: EvictLRU takes its own lock and does disk IO, and
		// must not block GetOrCreate/GetExisting behind the manager mutex.
		if m.cache != nil {
			m.mu.Lock()
			live := make(map[string]bool, len(m.sessions))
			for _, s := range m.sessions {
				live[s.OutputDir] = true
			}
			m.mu.Unlock()
			if err := m.cache.EvictLRU(live); err != nil {
				log.Warn().Err(err).Msg("transcode cache eviction failed")
			}
		}
	}
}
