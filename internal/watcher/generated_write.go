package watcher

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/karbowiak/heya/internal/generatedwrite"
)

// SuppressGeneratedWrite records one exact sidecar publication in the
// process-local fsnotify guard. Durable provenance is committed by the
// generatedwrite publisher, before this notification is attempted.
// Registration deliberately happens after the atomic rename: an fsnotify event
// may already be queued at that point, so event suppression is evaluated when
// the debounce window closes rather than only when the event first arrives.
func (m *Manager) SuppressGeneratedWrite(output generatedwrite.Output) error {
	if m == nil || !output.Written || !output.Attested || output.Path == "" {
		return nil
	}
	key, err := generatedWritePathKey(output.Path)
	if err != nil {
		return err
	}
	signature, err := observeGeneratedWrite(key)
	if err != nil {
		return fmt.Errorf("observe generated write: %w", err)
	}
	if signature.size != output.Size || signature.sha256 != output.SHA256 {
		return errors.New("generated sidecar changed before watcher registration")
	}
	now := m.generatedWriteNow()
	m.generatedWriteMu.Lock()
	if m.generatedWrites == nil {
		m.generatedWrites = make(map[string]generatedWriteSuppression)
	}
	m.pruneGeneratedWritesLocked(now)
	if len(m.generatedWrites) >= maxGeneratedWriteSuppressions {
		m.evictOldestGeneratedWriteLocked()
	}
	m.generatedWrites[key] = generatedWriteSuppression{
		signature: signature,
		expiresAt: now.Add(generatedWriteTTL),
		recorded:  now,
	}
	m.generatedWriteMu.Unlock()
	return nil
}

// shouldSuppressGeneratedEvent returns true only while path still names the
// exact bytes, size and mtime observed after Heya's publication. The record is
// retained until its short TTL so duplicate/late fsnotify events are quiet.
// Any subsequent user write invalidates it immediately and is scanned.
func (m *Manager) shouldSuppressGeneratedEvent(path string) bool {
	if m == nil || path == "" {
		return false
	}
	key, err := generatedWritePathKey(path)
	if err != nil {
		return false
	}
	now := m.generatedWriteNow()

	m.generatedWriteMu.Lock()
	record, ok := m.generatedWrites[key]
	if ok && !now.Before(record.expiresAt) {
		delete(m.generatedWrites, key)
		ok = false
	}
	m.generatedWriteMu.Unlock()
	if !ok {
		return false
	}

	current, err := observeGeneratedWrite(key)
	if err != nil || current != record.signature {
		m.generatedWriteMu.Lock()
		if latest, exists := m.generatedWrites[key]; exists && latest == record {
			delete(m.generatedWrites, key)
		}
		m.generatedWriteMu.Unlock()
		return false
	}

	// Registration could have been replaced or expired while the file was
	// hashed. Re-check under the lock before suppressing the event.
	m.generatedWriteMu.Lock()
	latest, exists := m.generatedWrites[key]
	valid := exists && latest == record && m.generatedWriteNow().Before(latest.expiresAt)
	if exists && !valid && latest == record {
		delete(m.generatedWrites, key)
	}
	m.generatedWriteMu.Unlock()
	return valid
}

func generatedWritePathKey(path string) (string, error) {
	return generatedwrite.CanonicalPath(path)
}

func observeGeneratedWrite(path string) (generatedWriteSignature, error) {
	file, err := os.Open(path) //nolint:gosec // path is the scanner-selected library sidecar
	if err != nil {
		return generatedWriteSignature{}, err
	}
	defer func() { _ = file.Close() }()

	before, err := file.Stat()
	if err != nil {
		return generatedWriteSignature{}, err
	}
	if !before.Mode().IsRegular() {
		return generatedWriteSignature{}, errors.New("generated sidecar is not a regular file")
	}
	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return generatedWriteSignature{}, err
	}
	after, err := file.Stat()
	if err != nil {
		return generatedWriteSignature{}, err
	}
	pathInfo, err := os.Stat(path)
	if err != nil {
		return generatedWriteSignature{}, err
	}
	if size != before.Size() || after.Size() != before.Size() ||
		!after.ModTime().Equal(before.ModTime()) || !os.SameFile(after, pathInfo) {
		return generatedWriteSignature{}, errors.New("generated sidecar changed while it was observed")
	}

	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return generatedWriteSignature{
		size:         size,
		modTimeNanos: after.ModTime().UnixNano(),
		sha256:       digest,
	}, nil
}

func (m *Manager) generatedWriteNow() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *Manager) pruneGeneratedWritesLocked(now time.Time) {
	for path, record := range m.generatedWrites {
		if !now.Before(record.expiresAt) {
			delete(m.generatedWrites, path)
		}
	}
}

func (m *Manager) evictOldestGeneratedWriteLocked() {
	var oldestPath string
	var oldestAt time.Time
	for path, record := range m.generatedWrites {
		if oldestPath == "" || record.recorded.Before(oldestAt) {
			oldestPath = path
			oldestAt = record.recorded
		}
	}
	if oldestPath != "" {
		delete(m.generatedWrites, oldestPath)
	}
}
