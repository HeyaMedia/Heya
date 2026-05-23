package transcoder

import (
	"bufio"
	"context"
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
)

type Head struct {
	StartSeg   int
	CurrentSeg int
	Cancel     context.CancelFunc
	Cmd        *exec.Cmd
	Done       chan struct{}
}

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

	mu         sync.Mutex
	head       *Head
	segments   []*segReady
	LastAccess time.Time

	// progress is the latest ffmpeg telemetry block from the running head.
	// Zero-valued (Running=false) until the head starts emitting.
	progress ProgressStats

	// lastRequestedSeg is the highest segment index any player request has
	// touched. It anchors the lead-cap throttle: once the encoder runs more
	// than LeadCapSeconds ahead of this point, the head is killed to stop
	// transcoding content the player isn't likely to need.
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
	return HeadInfo{
		Running:    running,
		StartSeg:   s.head.StartSeg,
		CurrentSeg: s.head.CurrentSeg,
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

func (s *TranscodeSession) SegmentDuration(idx int) float64 {
	if idx < 0 || idx >= len(s.SegmentEnds) {
		return 0
	}
	if idx == 0 {
		return s.SegmentEnds[0]
	}
	return s.SegmentEnds[idx] - s.SegmentEnds[idx-1]
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

func (s *TranscodeSession) IsSegmentReady(index int) bool {
	if index < 0 || index >= s.TotalSegs {
		return false
	}
	select {
	case <-s.segments[index].ch:
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
	case <-s.segments[index].ch:
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *TranscodeSession) RequestSegment(ctx context.Context, idx int) bool {
	if idx < 0 || idx >= s.TotalSegs {
		return false
	}

	s.mu.Lock()
	if idx > s.lastRequestedSeg {
		s.lastRequestedSeg = idx
	}
	s.mu.Unlock()

	if s.IsSegmentReady(idx) {
		return true
	}

	s.mu.Lock()
	if s.needsNewHead(idx) {
		s.killHead()
		s.spawnHead(idx)
	}
	s.mu.Unlock()

	return s.WaitForSegment(ctx, idx)
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
	dist := idx - s.head.CurrentSeg
	return dist > seekThresholdSegs
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
		StartSeg:   startSeg,
		CurrentSeg: startSeg,
		Cancel:     cancel,
		Done:       make(chan struct{}),
	}
	s.head = head
	s.headStopReason = StopReasonRunning

	go s.runHead(ctx, head, opts)
}

func (s *TranscodeSession) runHead(ctx context.Context, head *Head, opts TranscodeOpts) {
	defer close(head.Done)

	label := fmt.Sprintf("head_%d", head.StartSeg)

	cmd, err := s.builder.BuildHLSCommand(ctx, opts)
	if err != nil {
		log.Error().Err(err).Str("key", s.Key).Msg(label + " build command failed")
		return
	}
	head.Cmd = cmd

	os.WriteFile(filepath.Join(s.OutputDir, label+"_cmd.txt"),
		[]byte(s.builder.FormatCommand(cmd)+"\n"), 0644)

	logFile, _ := os.Create(filepath.Join(s.OutputDir, label+"_ffmpeg.log"))

	var smbCloser io.Closer
	if vfs.IsSMBPath(s.FilePath) {
		reader, closer, err := OpenSMBReader(s.FilePath)
		if err != nil {
			log.Error().Err(err).Str("key", s.Key).Msg("open smb for head")
			if logFile != nil {
				logFile.Close()
			}
			return
		}
		smbCloser = closer
		cmd.Stdin = reader
	}

	if logFile != nil {
		cmd.Stderr = logFile
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
			defer progressR.Close()
			progressReader(progressR, startedAt, func(apply func(*ProgressStats)) {
				s.mu.Lock()
				apply(&s.progress)
				s.mu.Unlock()
			})
		}()
	}

	log.Info().
		Str("key", s.Key).
		Str("file", s.FilePath).
		Int("start_seg", head.StartSeg).
		Float64("start_time", opts.StartTime).
		Int("audio", opts.AudioTrack).
		Str("video_codec", opts.Profile.VideoCodec).
		Str("audio_codec", opts.Profile.AudioCodec).
		Bool("fmp4", s.IsFMP4()).
		Msg(label + " starting")

	cleanup := func() {
		if smbCloser != nil {
			smbCloser.Close()
		}
		if logFile != nil {
			logFile.Close()
		}
		// Closing the write end signals EOF to progressReader. Errors are
		// fine — pipe may already be closed by the child process exiting.
		if progressW != nil {
			progressW.Close()
		}
		// Mark progress as not-running on head exit so the UI can show
		// "idle" instead of stale numbers.
		s.mu.Lock()
		s.progress.Running = false
		s.mu.Unlock()
	}

	logExit := func(cmdErr error) {
		if cmdErr != nil && ctx.Err() == nil {
			exitCode := 0
			if exitErr, ok := cmdErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			log.Warn().Err(cmdErr).Str("key", s.Key).Int("exit_code", exitCode).Msg(label + " failed")
		} else {
			log.Info().Str("key", s.Key).Int("last_seg", head.CurrentSeg).Msg(label + " finished")
		}
	}

	if s.IsFMP4() {
		s.runHeadFMP4(ctx, head, cmd, cleanup, logExit)
	} else {
		s.runHeadTS(ctx, head, cmd, cleanup, logExit)
	}
}

func (s *TranscodeSession) runHeadTS(ctx context.Context, head *Head, cmd *exec.Cmd, cleanup func(), logExit func(error)) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Error().Err(err).Str("key", s.Key).Msg("stdout pipe")
		cleanup()
		return
	}

	if err := cmd.Start(); err != nil {
		log.Error().Err(err).Str("key", s.Key).Msg("ffmpeg start failed")
		cleanup()
		return
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		segIdx := parseSegIdx(filepath.Base(line))
		if segIdx < 0 {
			continue
		}

		head.CurrentSeg = segIdx
		s.markSegmentReady(segIdx)

		if segIdx > head.StartSeg && s.segmentAlreadyDone(segIdx+1) {
			log.Info().Str("key", s.Key).Int("seg", segIdx).Msg("head reached completed territory, stopping")
			s.setStopReason(StopReasonCompleted)
			head.Cancel()
			break
		}

		// Lead-cap throttle: stop running ahead of the player.
		s.mu.Lock()
		exceeded := s.headExceedsLeadCap(head)
		lastReq := s.lastRequestedSeg
		s.mu.Unlock()
		if exceeded {
			log.Info().
				Str("key", s.Key).
				Int("seg", segIdx).
				Int("last_requested", lastReq).
				Float64("lead_cap_seconds", LeadCapSeconds).
				Msg("head exceeded lead cap, stopping")
			s.setStopReason(StopReasonLeadCap)
			head.Cancel()
			break
		}
	}

	cmdErr := cmd.Wait()
	// If we exited the scanner loop without setting a reason, it's because
	// ffmpeg ended its stdout (natural EOF / error / killed externally).
	s.setStopReasonIfRunning(StopReasonExited)
	cleanup()
	logExit(cmdErr)
}

// setStopReason unconditionally records the head's stop reason.
func (s *TranscodeSession) setStopReason(r HeadStopReason) {
	s.mu.Lock()
	s.headStopReason = r
	s.mu.Unlock()
}

// setStopReasonIfRunning only writes if the reason is still "running",
// preventing a generic-exit reason from clobbering a more specific one
// already set by the loop (lead cap, completed, killed).
func (s *TranscodeSession) setStopReasonIfRunning(r HeadStopReason) {
	s.mu.Lock()
	if s.headStopReason == StopReasonRunning {
		s.headStopReason = r
	}
	s.mu.Unlock()
}

func (s *TranscodeSession) runHeadFMP4(ctx context.Context, head *Head, cmd *exec.Cmd, cleanup func(), logExit func(error)) {
	if err := cmd.Start(); err != nil {
		log.Error().Err(err).Str("key", s.Key).Msg("ffmpeg start failed")
		cleanup()
		return
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	watcher, werr := fsnotify.NewWatcher()
	if werr == nil {
		defer watcher.Close()
		watcher.Add(s.OutputDir)
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
			<-waitDone
			s.reconcileSegmentsFromFS(head)
			// Reason was set by whoever called Cancel (lead cap / completed /
			// killHead). Default to "exited" only if nothing did.
			s.setStopReasonIfRunning(StopReasonExited)
			cleanup()
			logExit(nil)
			return

		case cmdErr := <-waitDone:
			s.reconcileSegmentsFromFS(head)
			s.setStopReasonIfRunning(StopReasonExited)
			cleanup()
			logExit(cmdErr)
			return

		case ev := <-fsEvents:
			if !ev.Has(fsnotify.Create) && !ev.Has(fsnotify.Rename) {
				continue
			}
			name := filepath.Base(ev.Name)
			if !strings.HasSuffix(name, ".m4s") {
				continue
			}
			idx := parseSegIdx(name)
			if idx < 1 {
				continue
			}
			// "next exists → previous is flushed"
			s.markSegmentReady(idx - 1)
			head.CurrentSeg = idx - 1
			if idx-1 > head.StartSeg && s.segmentAlreadyDone(idx+1) {
				log.Info().Str("key", s.Key).Int("seg", idx-1).Msg("head reached completed territory, stopping")
				s.setStopReason(StopReasonCompleted)
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
					Int("seg", idx-1).
					Int("last_requested", lastReq).
					Float64("lead_cap_seconds", LeadCapSeconds).
					Msg("head exceeded lead cap, stopping")
				s.setStopReason(StopReasonLeadCap)
				head.Cancel()
			}

		case <-fsErrs:
			// ignore watcher errors; ticker polling will catch up

		case <-ticker.C:
			s.reconcileSegmentsFromFS(head)
		}
	}
}

// reconcileSegmentsFromFS scans the output directory and marks all complete
// segments as ready. "Complete" means: file exists AND (next file exists OR
// ffmpeg has exited). Called periodically (defense-in-depth) and on exit.
func (s *TranscodeSession) reconcileSegmentsFromFS(head *Head) {
	entries, err := os.ReadDir(s.OutputDir)
	if err != nil {
		return
	}
	maxIdx := -1
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".m4s") {
			continue
		}
		if strings.HasSuffix(n, ".tmp") {
			continue
		}
		idx := parseSegIdx(n)
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	// Check if ffmpeg is still running; if so, only mark up to maxIdx-1.
	// On the exit path (head.Done not yet closed but we're inside the cleanup
	// flow), we mark through maxIdx.
	cutoff := maxIdx
	if head.Cmd != nil && head.Cmd.ProcessState == nil {
		cutoff = maxIdx - 1
	}
	for i := head.StartSeg; i <= cutoff && i < s.TotalSegs; i++ {
		s.markSegmentReady(i)
	}
	if maxIdx > head.CurrentSeg {
		head.CurrentSeg = maxIdx
	}
}

// markSegmentReady is safe to call concurrently and without holding s.mu.
// sync.Once ensures the channel is closed exactly once even under races.
func (s *TranscodeSession) markSegmentReady(idx int) {
	if idx >= 0 && idx < s.TotalSegs {
		s.segments[idx].markReady()
	}
}

func (s *TranscodeSession) segmentAlreadyDone(idx int) bool {
	if idx < 0 || idx >= s.TotalSegs {
		return false
	}
	select {
	case <-s.segments[idx].ch:
		return true
	default:
		return false
	}
}

func (s *TranscodeSession) ReadyCount() int {
	count := 0
	for i := 0; i < s.TotalSegs; i++ {
		select {
		case <-s.segments[i].ch:
			count++
		default:
		}
	}
	return count
}

func (s *TranscodeSession) AllDone() bool {
	return s.ReadyCount() == s.TotalSegs
}

func (s *TranscodeSession) IsIdle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.head == nil {
		return true
	}
	select {
	case <-s.head.Done:
		return true
	default:
		return false
	}
}

func (s *TranscodeSession) Kill() {
	s.mu.Lock()
	defer s.mu.Unlock()
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

	mu       sync.Mutex
	sessions map[string]*TranscodeSession
}

func NewSessionManager(cache *CacheManager, hwAccel *HwAccelProvider, builder CommandBuilder) *SessionManager {
	cache.Clear()
	sm := &SessionManager{
		cache:    cache,
		hwAccel:  hwAccel,
		builder:  builder,
		sessions: make(map[string]*TranscodeSession),
	}
	go sm.cleanupLoop()
	return sm
}

// Builder returns the CommandBuilder used by this session manager.
func (m *SessionManager) Builder() CommandBuilder {
	return m.builder
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

func (m *SessionManager) KillForFile(fileID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := fmt.Sprintf("%d:", fileID)
	for key, s := range m.sessions {
		if strings.HasPrefix(key, prefix) {
			s.Kill()
			delete(m.sessions, key)
		}
	}
}

func (m *SessionManager) GetOrCreate(fileID int64, filePath string, opts TranscodeOpts, sessionID string, duration float64, kf *Keyframes) *TranscodeSession {
	key := FormatKey(fileID, opts.AudioTrack, sessionID)

	m.mu.Lock()
	if s, ok := m.sessions[key]; ok {
		s.Touch()
		m.mu.Unlock()
		return s
	}

	prefix := fmt.Sprintf("%d:", fileID)
	for k, s := range m.sessions {
		if strings.HasPrefix(k, prefix) {
			s.Kill()
			delete(m.sessions, k)
		}
	}

	var ends []float64
	if opts.UseFMP4 && opts.Profile.VideoCodec == "copy" {
		ends = PlannedSegmentTimes(kf, duration, SegmentDuration)
	} else {
		ends = fixedIntervalBoundaries(duration, SegmentDuration)
	}
	totalSegs := len(ends)
	if totalSegs < 1 {
		ends = []float64{1}
		totalSegs = 1
	}

	outputDir := m.cache.SegmentDir(key)
	os.MkdirAll(outputDir, 0755)

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
		OutputDir:   outputDir,
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

	m.sessions[key] = session
	m.mu.Unlock()

	log.Info().
		Str("key", key).
		Str("file", filePath).
		Int("total_segs", totalSegs).
		Float64("duration", duration).
		Bool("fmp4", opts.UseFMP4).
		Bool("keyframes", kf != nil).
		Msg("session created")

	return session
}

func (m *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		for key, s := range m.sessions {
			s.mu.Lock()
			idle := time.Since(s.LastAccess)
			s.mu.Unlock()

			if idle > 2*time.Minute {
				log.Info().Str("key", key).Dur("idle", idle).Msg("cleaning up idle session")
				s.Kill()
				os.RemoveAll(s.OutputDir)
				delete(m.sessions, key)
			}
		}
		m.mu.Unlock()
	}
}

func OpenSMBReader(smbPath string) (io.Reader, io.Closer, error) {
	lastSlash := strings.LastIndex(smbPath, "/")
	if lastSlash < 0 {
		return nil, nil, fmt.Errorf("invalid smb path: %s", smbPath)
	}

	source, err := vfs.Open(smbPath[:lastSlash])
	if err != nil {
		return nil, nil, fmt.Errorf("open smb dir: %w", err)
	}

	f, err := source.FS.Open(smbPath[lastSlash+1:])
	if err != nil {
		source.Close()
		return nil, nil, fmt.Errorf("open smb file: %w", err)
	}

	closer := &multiCloser{closeFuncs: []func() error{f.Close, source.Close}}
	return f.(io.Reader), closer, nil
}

type multiCloser struct {
	closeFuncs []func() error
}

func (mc *multiCloser) Close() error {
	for _, fn := range mc.closeFuncs {
		fn()
	}
	return nil
}
