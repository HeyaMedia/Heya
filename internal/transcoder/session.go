package transcoder

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/karbowiak/heya/internal/vfs"
	"github.com/rs/zerolog/log"
)

type TranscodeSession struct {
	Key        string
	FilePath   string
	OutputDir  string
	SegExt     string
	Cancel     context.CancelFunc
	Cmd        *exec.Cmd
	Stdin      io.WriteCloser
	Done       chan struct{}
	Err        error
	StartTime  float64
	LastAccess time.Time

	mu        sync.Mutex
	throttled bool
}

func (s *TranscodeSession) Pause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.throttled {
		return
	}
	if s.Stdin != nil {
		s.Stdin.Write([]byte("p"))
	} else if s.Cmd != nil && s.Cmd.Process != nil {
		s.Cmd.Process.Signal(syscall.SIGSTOP)
	}
	s.throttled = true
}

func (s *TranscodeSession) Resume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.throttled {
		return
	}
	if s.Stdin != nil {
		s.Stdin.Write([]byte("u"))
	} else if s.Cmd != nil && s.Cmd.Process != nil {
		s.Cmd.Process.Signal(syscall.SIGCONT)
	}
	s.throttled = false
}

func (s *TranscodeSession) Touch() {
	s.mu.Lock()
	s.LastAccess = time.Now()
	s.mu.Unlock()
}

func (s *TranscodeSession) SegmentPath(index int) string {
	return filepath.Join(s.OutputDir, fmt.Sprintf("seg_%04d%s", index, s.SegExt))
}

func (s *TranscodeSession) IsSegmentReady(index int) bool {
	if _, err := os.Stat(s.SegmentPath(index)); err != nil {
		return false
	}
	select {
	case <-s.Done:
		return true
	default:
	}
	if _, err := os.Stat(s.SegmentPath(index + 1)); err == nil {
		return true
	}
	return false
}

func (s *TranscodeSession) WaitForSegment(ctx context.Context, index int) bool {
	for {
		if s.IsSegmentReady(index) {
			return true
		}
		select {
		case <-s.Done:
			if _, err := os.Stat(s.SegmentPath(index)); err == nil {
				return true
			}
			return false
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *TranscodeSession) Kill() {
	s.Cancel()
	if s.Cmd != nil && s.Cmd.Process != nil {
		s.Cmd.Process.Signal(os.Interrupt)
	}
}

type SessionManager struct {
	cache   *CacheManager
	hwAccel HwAccelConfig

	mu       sync.Mutex
	sessions map[string]*TranscodeSession
}

func NewSessionManager(cache *CacheManager, hwAccel HwAccelConfig) *SessionManager {
	cache.Clear()
	sm := &SessionManager{
		cache:    cache,
		hwAccel:  hwAccel,
		sessions: make(map[string]*TranscodeSession),
	}
	go sm.cleanupLoop()
	return sm
}

func FormatKey(fileID int64, audioTrack int, startTime float64, sessionID string) string {
	if sessionID != "" {
		return fmt.Sprintf("%d:a%d:t%.0f:%s", fileID, audioTrack, startTime, sessionID)
	}
	return fmt.Sprintf("%d:a%d:t%.0f", fileID, audioTrack, startTime)
}

func (m *SessionManager) HWAccel() HwAccelConfig {
	return m.hwAccel
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

func (m *SessionManager) GetOrCreate(fileID int64, filePath string, opts TranscodeOpts, sessionID string) *TranscodeSession {
	key := FormatKey(fileID, opts.AudioTrack, opts.StartTime, sessionID)

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

	outputDir := m.cache.SegmentDir(key)
	os.MkdirAll(outputDir, 0755)

	ctx, cancel := context.WithCancel(context.Background())
	session := &TranscodeSession{
		Key:        key,
		FilePath:   filePath,
		OutputDir:  outputDir,
		SegExt:     ".ts",
		Cancel:     cancel,
		Done:       make(chan struct{}),
		StartTime:  opts.StartTime,
		LastAccess: time.Now(),
	}

	m.sessions[key] = session
	m.mu.Unlock()

	go m.runTranscode(ctx, session, opts)

	return session
}

func (m *SessionManager) runTranscode(ctx context.Context, session *TranscodeSession, opts TranscodeOpts) {
	defer close(session.Done)

	args := buildHLSArgs(opts, session.OutputDir)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	var smbCloser io.Closer
	if vfs.IsSMBPath(session.FilePath) {
		reader, closer, err := OpenSMBReader(session.FilePath)
		if err != nil {
			session.Err = fmt.Errorf("open smb: %w", err)
			return
		}
		smbCloser = closer
		cmd.Stdin = reader
	} else {
		stdinPipe, err := cmd.StdinPipe()
		if err == nil {
			session.Stdin = stdinPipe
		}
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr
	session.Cmd = cmd

	log.Info().
		Str("key", session.Key).
		Str("file", session.FilePath).
		Float64("start", opts.StartTime).
		Int("audio", opts.AudioTrack).
		Msg("starting HLS transcode")

	if err := cmd.Start(); err != nil {
		session.Err = fmt.Errorf("ffmpeg start: %w", err)
		if smbCloser != nil {
			smbCloser.Close()
		}
		return
	}

	session.Err = cmd.Wait()

	if smbCloser != nil {
		smbCloser.Close()
	}

	if session.Err != nil && ctx.Err() != nil {
		session.Err = nil
	}

	if session.Err != nil {
		log.Warn().Err(session.Err).Str("key", session.Key).Str("stderr", stderr.String()).Msg("transcode failed")
	} else {
		log.Info().Str("key", session.Key).Msg("transcode finished")
	}
}

func (m *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		for key, s := range m.sessions {
			s.mu.Lock()
			idle := time.Since(s.LastAccess)
			s.mu.Unlock()

			select {
			case <-s.Done:
				if idle > 60*time.Second {
					os.RemoveAll(s.OutputDir)
					delete(m.sessions, key)
				}
			default:
				if idle > 60*time.Second {
					log.Info().Str("key", key).Msg("killing idle transcode")
					s.Kill()
					delete(m.sessions, key)
				} else {
					m.throttleCheck(s)
				}
			}
		}
		m.mu.Unlock()
	}
}

func (m *SessionManager) throttleCheck(s *TranscodeSession) {
	segCount := 0
	for i := 0; ; i++ {
		if _, err := os.Stat(s.SegmentPath(i)); err != nil {
			break
		}
		segCount = i + 1
	}

	transcodedSeconds := float64(segCount) * 6.0
	s.mu.Lock()
	idle := time.Since(s.LastAccess)
	throttled := s.throttled
	s.mu.Unlock()

	if idle > 3*time.Second && transcodedSeconds > 60 && !throttled {
		log.Debug().Str("key", s.Key).Float64("transcoded_s", transcodedSeconds).Msg("throttling transcode")
		s.Pause()
	}
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
