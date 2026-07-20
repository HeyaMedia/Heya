package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("a sufficiently long passphrase")
	require.NoError(t, err)
	assert.Contains(t, hash, "$argon2id$")
	assert.True(t, CheckPassword(hash, "a sufficiently long passphrase"))
	assert.False(t, NeedsPasswordRehash(hash))
}

func TestCheckPasswordWrong(t *testing.T) {
	hash, err := HashPassword("correct")
	require.NoError(t, err)
	assert.False(t, CheckPassword(hash, "wrong"))
}

func TestHashPasswordSaltUniqueness(t *testing.T) {
	h1, err := HashPassword("same")
	require.NoError(t, err)
	h2, err := HashPassword("same")
	require.NoError(t, err)
	assert.NotEqual(t, h1, h2)
}

func TestLegacyBcryptPasswordStillVerifiesAndNeedsUpgrade(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("legacy-password"), bcrypt.DefaultCost)
	require.NoError(t, err)
	assert.True(t, CheckPassword(string(hash), "legacy-password"))
	assert.True(t, NeedsPasswordRehash(string(hash)))
}

func TestValidateNewPassword(t *testing.T) {
	assert.ErrorIs(t, ValidateNewPassword("too short"), ErrPasswordPolicy)
	assert.NoError(t, ValidateNewPassword("this passphrase is long enough"))
}

func TestGenerateToken(t *testing.T) {
	tok, err := GenerateToken()
	require.NoError(t, err)
	assert.Len(t, tok, 64)

	tok2, err := GenerateToken()
	require.NoError(t, err)
	assert.NotEqual(t, tok, tok2)
}

type mockSessionLookup struct {
	session sqlc.Session
	user    sqlc.User
	err     error
}

func (m *mockSessionLookup) GetSessionWithUserByToken(_ context.Context, token string) (sqlc.GetSessionWithUserByTokenRow, error) {
	if m.err != nil {
		return sqlc.GetSessionWithUserByTokenRow{}, m.err
	}
	if token == m.session.TokenHash && m.session.UserID == m.user.ID {
		return sqlc.GetSessionWithUserByTokenRow{Session: m.session, User: m.user}, nil
	}
	// Mirror sqlc's actual behaviour: a `:one` query that returns no rows
	// surfaces as pgx.ErrNoRows, not a generic error. The middleware uses
	// errors.Is(err, pgx.ErrNoRows) to distinguish "session not found"
	// (401) from "DB unreachable" (503), so the mock has to be honest.
	return sqlc.GetSessionWithUserByTokenRow{}, pgx.ErrNoRows
}

func (m *mockSessionLookup) GetUserByID(_ context.Context, id int64) (sqlc.User, error) {
	if m.err != nil {
		return sqlc.User{}, m.err
	}
	if id == m.user.ID {
		return m.user, nil
	}
	return sqlc.User{}, pgx.ErrNoRows
}

func (m *mockSessionLookup) TouchSession(_ context.Context, _ string) error {
	return nil
}

func TestResolveSessionValidToken(t *testing.T) {
	mock := &mockSessionLookup{
		session: sqlc.Session{TokenHash: TokenHash("validtoken"), UserID: 42},
		user:    sqlc.User{ID: 42, Username: "alice"},
	}

	resolved, err := ResolveSession(context.Background(), mock, "validtoken")
	require.NoError(t, err)
	assert.Equal(t, "alice", resolved.User.Username)
	assert.Equal(t, "validtoken", resolved.Token)
}

func TestResolveSessionMissingToken(t *testing.T) {
	_, err := ResolveSession(context.Background(), &mockSessionLookup{}, "")
	assert.ErrorIs(t, err, ErrInvalidSession)
}

func TestResolveSessionInvalidToken(t *testing.T) {
	mock := &mockSessionLookup{
		session: sqlc.Session{TokenHash: TokenHash("validtoken"), UserID: 42},
		user:    sqlc.User{ID: 42},
	}
	_, err := ResolveSession(context.Background(), mock, "wrongtoken")
	assert.ErrorIs(t, err, ErrInvalidSession)
}

// A DB-error during session lookup (postgres down, query timeout, etc.)
// must NOT surface as ErrInvalidSession — the huma auth middleware maps
// ErrInvalidSession to 401 (FE logs the user out) and anything else to 503
// (session survives the backend blip). The mock returns pgx.ErrNoRows for
// unknown tokens because that's how sqlc `:one` queries report absence.
func TestResolveSessionDBErrorIsNotInvalidSession(t *testing.T) {
	mock := &mockSessionLookup{err: errors.New("connection refused")}
	_, err := ResolveSession(context.Background(), mock, "anything")
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidSession)
}
