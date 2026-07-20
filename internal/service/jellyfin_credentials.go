package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Jellyfin credentials: one optional server-generated PIN per user.
//
// The Jellyfin login is username+password on a TV remote — miserable with a
// real password. Each user can mint a short numeric PIN that the Jellyfin
// surface (and ONLY the Jellyfin surface) accepts in place of the account
// password. Server-minted (never user-chosen, so it can't leak a reused
// password), retrievable so the user can read it off the Settings page, and
// rotatable/revocable without touching the real password. The native login
// and every other surface keep requiring the full password.
//
// Raw pgx (not sqlc) on purpose, same as subsonic_credentials.go: this file
// is the only reader/writer of jellyfin_credentials, and keeping it
// codegen-free means concurrent work on other query files can't collide with
// a regenerated sqlc package.

// ErrJellyfinNoCredential distinguishes "user never minted a Jellyfin PIN"
// from "wrong PIN" so the API layer can hint at the fix.
var ErrJellyfinNoCredential = errors.New("no jellyfin pin provisioned")

// JellyfinCredential is the per-user PIN view. PIN is included — the whole
// point is that the user can read it back to type into a TV client.
type JellyfinCredential struct {
	UserID     int64      `json:"user_id"`
	PIN        string     `json:"pin"`
	CreatedAt  time.Time  `json:"created_at"`
	RotatedAt  time.Time  `json:"rotated_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// newJellyfinPIN mints a 6-digit PIN (leading zeros kept). Six digits, not
// four: the PIN rides the login endpoint, so the guess space is the only
// thing between a guessed username and a session — 1e6 plus the login
// throttle in internal/jellyfin is fine, 1e4 is not.
func newJellyfinPIN() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generating jellyfin pin: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// GetJellyfinCredential returns the user's PIN credential, or
// ErrJellyfinNoCredential when none exists.
func (a *App) GetJellyfinCredential(ctx context.Context, userID int64) (JellyfinCredential, error) {
	var c JellyfinCredential
	var lastUsed *time.Time
	err := a.db.QueryRow(ctx, `
		SELECT user_id, pin, created_at, rotated_at, last_used_at
		FROM jellyfin_credentials WHERE user_id = $1
	`, userID).Scan(&c.UserID, &c.PIN, &c.CreatedAt, &c.RotatedAt, &lastUsed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return JellyfinCredential{}, ErrJellyfinNoCredential
		}
		return JellyfinCredential{}, fmt.Errorf("get jellyfin credential: %w", err)
	}
	c.LastUsedAt = lastUsed
	return c, nil
}

// RotateJellyfinCredential creates (or replaces) the user's PIN and returns
// it. Rotation invalidates the old PIN immediately; sessions already minted
// with it stay alive (they're normal Heya sessions, revocable in Settings).
func (a *App) RotateJellyfinCredential(ctx context.Context, userID int64) (JellyfinCredential, error) {
	pin, err := newJellyfinPIN()
	if err != nil {
		return JellyfinCredential{}, err
	}
	var c JellyfinCredential
	var lastUsed *time.Time
	err = a.db.QueryRow(ctx, `
		INSERT INTO jellyfin_credentials (user_id, pin)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE
		  SET pin = EXCLUDED.pin, rotated_at = now(), last_used_at = NULL
		RETURNING user_id, pin, created_at, rotated_at, last_used_at
	`, userID, pin).Scan(&c.UserID, &c.PIN, &c.CreatedAt, &c.RotatedAt, &lastUsed)
	if err != nil {
		return JellyfinCredential{}, fmt.Errorf("rotate jellyfin credential: %w", err)
	}
	c.LastUsedAt = lastUsed
	return c, nil
}

// RevokeJellyfinCredential deletes the user's PIN; the full account password
// keeps working on the Jellyfin surface.
func (a *App) RevokeJellyfinCredential(ctx context.Context, userID int64) error {
	_, err := a.db.Exec(ctx, `DELETE FROM jellyfin_credentials WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("revoke jellyfin credential: %w", err)
	}
	return nil
}

// AuthenticateJellyfin verifies a Jellyfin login: the real account password
// first (bcrypt, same as Authenticate), then the user's Jellyfin PIN when
// one is provisioned. Failures are indistinguishable on purpose. Only the
// Jellyfin surface calls this — the PIN buys nothing anywhere else.
func (a *App) AuthenticateJellyfin(ctx context.Context, username, password string) (sqlc.User, error) {
	q := sqlc.New(a.db)
	user, err := q.GetUserByUsername(ctx, username)
	if err != nil {
		auth.CheckDummyPassword(password)
		return sqlc.User{}, fmt.Errorf("invalid credentials")
	}
	if auth.CheckPassword(user.PasswordHash, password) {
		rehashUserPassword(ctx, q, &user, password)
		return user, nil
	}
	cred, err := a.GetJellyfinCredential(ctx, user.ID)
	if err == nil && subtle.ConstantTimeCompare([]byte(cred.PIN), []byte(password)) == 1 {
		a.TouchJellyfinCredential(user.ID)
		return user, nil
	}
	return sqlc.User{}, fmt.Errorf("invalid credentials")
}

// TouchJellyfinCredential stamps last_used_at asynchronously — called on
// successful PIN auth; failure is inconsequential.
func (a *App) TouchJellyfinCredential(userID int64) {
	a.startBackground(func() {
		ctx, cancel := context.WithTimeout(a.LifetimeContext(), 2*time.Second)
		defer cancel()
		_, _ = a.db.Exec(ctx, `UPDATE jellyfin_credentials SET last_used_at = now() WHERE user_id = $1`, userID)
	})
}
