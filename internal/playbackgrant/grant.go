// Package playbackgrant issues opaque, short-lived media credentials for
// native renderers. Grants are kept in memory and remain bound to the Heya
// authentication session that minted them, so logout/revocation immediately
// prevents new media requests without exposing the user's bearer token.
package playbackgrant

import (
	"context"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/karbowiak/heya/internal/auth"
)

const (
	HeaderName = "X-Heya-Playback-Grant"
	minTTL     = 5 * time.Minute
	maxTTL     = 12 * time.Hour
)

var ErrInvalidGrant = errors.New("invalid or expired native playback grant")

type record struct {
	userID           int64
	sessionTokenHash string
	scopePath        string
	subtree          bool
	expiresAt        time.Time
}

type Manager struct {
	mu     sync.Mutex
	grants map[string]record
	now    func() time.Time
}

func New() *Manager {
	return &Manager{grants: make(map[string]record), now: time.Now}
}

// Issue returns a random opaque credential. Only its SHA-256 hash is retained
// in memory; neither the raw grant nor the user's bearer token is stored.
func (m *Manager) Issue(userID int64, sessionToken, scopePath string, subtree bool, ttl time.Duration) (string, time.Time, error) {
	if userID <= 0 || sessionToken == "" || !validScopePath(scopePath) {
		return "", time.Time{}, ErrInvalidGrant
	}
	if ttl < minTTL {
		ttl = minTTL
	}
	if ttl > maxTTL {
		ttl = maxTTL
	}
	token, err := auth.GenerateToken()
	if err != nil {
		return "", time.Time{}, err
	}
	now := m.now()
	expiresAt := now.Add(ttl)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeExpiredLocked(now)
	m.grants[auth.TokenHash(token)] = record{
		userID:           userID,
		sessionTokenHash: auth.TokenHash(sessionToken),
		scopePath:        scopePath,
		subtree:          subtree,
		expiresAt:        expiresAt,
	}
	return token, expiresAt, nil
}

// Validate checks the opaque grant, exact/subtree path scope, expiry, and the
// continued existence of the authentication session that minted it.
func (m *Manager) Validate(ctx context.Context, sessions auth.SessionLookup, token, expectedPath string) (int64, error) {
	if sessions == nil || !validToken(token) || !validScopePath(expectedPath) {
		return 0, ErrInvalidGrant
	}
	key := auth.TokenHash(token)
	now := m.now()

	m.mu.Lock()
	rec, ok := m.grants[key]
	if ok && !now.Before(rec.expiresAt) {
		delete(m.grants, key)
		ok = false
	}
	m.mu.Unlock()
	if !ok || !pathAllowed(rec, expectedPath) {
		return 0, ErrInvalidGrant
	}

	session, err := sessions.GetSessionByToken(ctx, rec.sessionTokenHash)
	if err != nil || session.UserID != rec.userID {
		return 0, ErrInvalidGrant
	}
	if _, err := sessions.GetUserByID(ctx, rec.userID); err != nil {
		return 0, ErrInvalidGrant
	}
	return rec.userID, nil
}

func pathAllowed(rec record, expectedPath string) bool {
	if expectedPath == rec.scopePath {
		return true
	}
	return rec.subtree && strings.HasPrefix(expectedPath, strings.TrimRight(rec.scopePath, "/")+"/")
}

func validScopePath(value string) bool {
	if value == "" || len(value) > 2048 || value[0] != '/' || strings.ContainsAny(value, "\\?#") {
		return false
	}
	u, err := url.ParseRequestURI(value)
	return err == nil && u.Path == value && u.RawQuery == "" && u.Fragment == ""
}

func validToken(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (m *Manager) purgeExpiredLocked(now time.Time) {
	for key, rec := range m.grants {
		if !now.Before(rec.expiresAt) {
			delete(m.grants, key)
		}
	}
}
