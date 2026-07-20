package service

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestJellyfinCredentialLifecycle exercises the PIN end-to-end against a real
// DB: mint, read-back, login via password AND via PIN, rotation invalidating
// the old PIN, and revocation restoring password-only login.
func TestJellyfinCredentialLifecycle(t *testing.T) {
	pool := testutil.SetupDB(t)
	ctx := context.Background()
	app := &App{db: pool, lifetimeCtx: ctx}

	const password = "correct horse battery staple"
	hash, err := auth.HashPassword(password)
	require.NoError(t, err)
	var userID int64
	_, _ = pool.Exec(ctx, `DELETE FROM users WHERE username = 'jf-pin-test'`)
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO users (username, email, password_hash) VALUES ('jf-pin-test', 'jf-pin@test.local', $1) RETURNING id`,
		hash).Scan(&userID))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	// No credential yet: read-back distinguishes it, password login works,
	// and an arbitrary PIN-shaped guess does not.
	_, err = app.GetJellyfinCredential(ctx, userID)
	require.ErrorIs(t, err, ErrJellyfinNoCredential)
	_, err = app.AuthenticateJellyfin(ctx, "jf-pin-test", password)
	require.NoError(t, err)
	_, err = app.AuthenticateJellyfin(ctx, "jf-pin-test", "123456")
	require.Error(t, err)

	// Mint: 6 digits, readable back.
	cred, err := app.RotateJellyfinCredential(ctx, userID)
	require.NoError(t, err)
	require.Regexp(t, regexp.MustCompile(`^\d{6}$`), cred.PIN)
	got, err := app.GetJellyfinCredential(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, cred.PIN, got.PIN)

	// Both secrets now sign in; junk still doesn't.
	u, err := app.AuthenticateJellyfin(ctx, "jf-pin-test", cred.PIN)
	require.NoError(t, err)
	require.Equal(t, userID, u.ID)
	_, err = app.AuthenticateJellyfin(ctx, "jf-pin-test", password)
	require.NoError(t, err)
	_, err = app.AuthenticateJellyfin(ctx, "jf-pin-test", "999999")
	require.Error(t, err)

	// Rotation kills the old PIN (unless the fresh one collides — 1e-6).
	rotated, err := app.RotateJellyfinCredential(ctx, userID)
	require.NoError(t, err)
	if rotated.PIN != cred.PIN {
		_, err = app.AuthenticateJellyfin(ctx, "jf-pin-test", cred.PIN)
		require.Error(t, err)
	}
	_, err = app.AuthenticateJellyfin(ctx, "jf-pin-test", rotated.PIN)
	require.NoError(t, err)

	// Revocation: PIN dead, password untouched.
	require.NoError(t, app.RevokeJellyfinCredential(ctx, userID))
	_, err = app.GetJellyfinCredential(ctx, userID)
	require.ErrorIs(t, err, ErrJellyfinNoCredential)
	_, err = app.AuthenticateJellyfin(ctx, "jf-pin-test", rotated.PIN)
	require.Error(t, err)
	_, err = app.AuthenticateJellyfin(ctx, "jf-pin-test", password)
	require.NoError(t, err)

	// The PIN is jellyfin-only: the native login path must reject it.
	if _, err := app.RotateJellyfinCredential(ctx, userID); err == nil {
		fresh, _ := app.GetJellyfinCredential(ctx, userID)
		_, err = app.Authenticate(ctx, "jf-pin-test", fresh.PIN)
		require.Error(t, err, "native login must not accept the jellyfin PIN")
	}
}

func TestNewJellyfinPIN(t *testing.T) {
	seen := map[string]bool{}
	for range 32 {
		pin, err := newJellyfinPIN()
		require.NoError(t, err)
		require.Regexp(t, regexp.MustCompile(`^\d{6}$`), pin)
		seen[pin] = true
	}
	// 32 draws from 1e6 colliding into a single value would mean rand is
	// broken, not unlucky.
	require.Greater(t, len(seen), 1)
}

// Guard the sentinel wiring: a DB error must not masquerade as "no credential".
func TestJellyfinCredentialErrIdentity(t *testing.T) {
	require.False(t, errors.Is(ErrJellyfinNoCredential, ErrSubsonicNoCredential))
}
