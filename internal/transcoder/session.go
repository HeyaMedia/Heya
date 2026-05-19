package transcoder

import (
	"context"
	"fmt"
	"sync"
)

type TranscodeSession struct {
	Key      string
	FilePath string
	Profile  string
	OutputDir string
	Cancel   context.CancelFunc
	Done     chan struct{}
	Err      error
}

type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*TranscodeSession
	cache    *CacheManager
}

func NewSessionManager(cache *CacheManager) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*TranscodeSession),
		cache:    cache,
	}
}

func FormatKey(fileID int64, profile string) string {
	return fmt.Sprintf("%d:%s", fileID, profile)
}

func sessionKey(fileID int64, profile string) string {
	return FormatKey(fileID, profile)
}

func (m *SessionManager) GetOrStart(ctx context.Context, fileID int64, filePath string, profile Profile) (*TranscodeSession, error) {
	key := sessionKey(fileID, profile.Name)

	m.mu.RLock()
	if s, ok := m.sessions[key]; ok {
		m.mu.RUnlock()
		return s, nil
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[key]; ok {
		return s, nil
	}

	outputDir := m.cache.SegmentDir(key)

	transCtx, cancel := context.WithCancel(ctx)
	session := &TranscodeSession{
		Key:       key,
		FilePath:  filePath,
		Profile:   profile.Name,
		OutputDir: outputDir,
		Cancel:    cancel,
		Done:      make(chan struct{}),
	}
	m.sessions[key] = session

	go func() {
		defer close(session.Done)
		session.Err = TranscodeToHLS(transCtx, filePath, outputDir, profile)
		m.mu.Lock()
		delete(m.sessions, key)
		m.mu.Unlock()
	}()

	return session, nil
}

func (m *SessionManager) Cancel(fileID int64, profile string) {
	key := sessionKey(fileID, profile)
	m.mu.RLock()
	s, ok := m.sessions[key]
	m.mu.RUnlock()
	if ok {
		s.Cancel()
	}
}

func (m *SessionManager) Active() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}
