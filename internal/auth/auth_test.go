package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("secret123")
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.True(t, CheckPassword(hash, "secret123"))
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

func (m *mockSessionLookup) GetSessionByToken(_ context.Context, token string) (sqlc.Session, error) {
	if m.err != nil {
		return sqlc.Session{}, m.err
	}
	if token == m.session.TokenHash {
		return m.session, nil
	}
	// Mirror sqlc's actual behaviour: a `:one` query that returns no rows
	// surfaces as pgx.ErrNoRows, not a generic error. The middleware uses
	// errors.Is(err, pgx.ErrNoRows) to distinguish "session not found"
	// (401) from "DB unreachable" (503), so the mock has to be honest.
	return sqlc.Session{}, pgx.ErrNoRows
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

func TestMiddlewareValidToken(t *testing.T) {
	mock := &mockSessionLookup{
		session: sqlc.Session{TokenHash: TokenHash("validtoken"), UserID: 42},
		user:    sqlc.User{ID: 42, Username: "alice"},
	}

	var gotUser sqlc.User
	var gotOK bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotOK = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(mock)(inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer validtoken")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, gotOK)
	assert.Equal(t, "alice", gotUser.Username)
}

func TestMiddlewareMissingToken(t *testing.T) {
	mock := &mockSessionLookup{}
	handler := Middleware(mock)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestMiddlewareInvalidToken(t *testing.T) {
	mock := &mockSessionLookup{
		session: sqlc.Session{TokenHash: TokenHash("validtoken"), UserID: 42},
		user:    sqlc.User{ID: 42},
	}
	handler := Middleware(mock)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrongtoken")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

// A DB-error during session lookup (postgres down, query timeout, etc.)
// must NOT be reported as 401 — the FE would log the user out for a
// transient backend blip. Returning 503 keeps the session intact so the
// next request can succeed once the backend recovers.
func TestMiddlewareDBErrorReturns503(t *testing.T) {
	mock := &mockSessionLookup{err: errors.New("connection refused")}
	handler := Middleware(mock)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

func TestMiddlewareCookieToken(t *testing.T) {
	mock := &mockSessionLookup{
		session: sqlc.Session{TokenHash: TokenHash("cookietoken"), UserID: 1},
		user:    sqlc.User{ID: 1, Username: "bob"},
	}

	var gotUser sqlc.User
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, _ = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := Middleware(mock)(inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: "cookietoken"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "bob", gotUser.Username)
}
