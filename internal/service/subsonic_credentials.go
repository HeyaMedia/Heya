package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// Subsonic credentials: one server-generated app password per user.
//
// Subsonic's token auth is t = md5(password + salt) — verifying it requires
// the server to know the shared secret in clear, which Heya's bcrypt login
// hashes cannot answer. The standard solution (Navidrome, Gonic, Astiga...)
// is a dedicated per-user secret: random, server-minted (never user-chosen,
// so it can't leak a reused password), retrievable for the md5 check, and
// rotatable without touching the real account password. The same secret is
// accepted as the OpenSubsonic `apiKey`.
//
// Raw pgx (not sqlc) on purpose: this file is the only reader/writer of
// subsonic_credentials, and keeping it codegen-free means concurrent work on
// other query files can't collide with a regenerated sqlc package.

// ErrSubsonicNoCredential distinguishes "user never provisioned a Subsonic
// password" from "wrong password" so the API layer can hint at the fix.
var ErrSubsonicNoCredential = errors.New("no subsonic credential provisioned")

// SubsonicCredential is the per-user credential view. Secret is included —
// the whole point is that the user can read it back to type into a client.
type SubsonicCredential struct {
	UserID     int64      `json:"user_id"`
	Secret     string     `json:"secret"`
	CreatedAt  time.Time  `json:"created_at"`
	RotatedAt  time.Time  `json:"rotated_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// subsonicSecretAlphabet deliberately avoids look-alikes (0/O, 1/l/I) —
// users hand-type this into phone apps.
const subsonicSecretAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"

func newSubsonicSecret() (string, error) {
	const n = 20
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating subsonic secret: %w", err)
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = subsonicSecretAlphabet[int(b)%len(subsonicSecretAlphabet)]
	}
	return string(out), nil
}

// GetSubsonicCredential returns the user's credential, or
// ErrSubsonicNoCredential when none exists.
func (a *App) GetSubsonicCredential(ctx context.Context, userID int64) (SubsonicCredential, error) {
	var c SubsonicCredential
	var lastUsed *time.Time
	err := a.db.QueryRow(ctx, `
		SELECT user_id, secret, created_at, rotated_at, last_used_at
		FROM subsonic_credentials WHERE user_id = $1
	`, userID).Scan(&c.UserID, &c.Secret, &c.CreatedAt, &c.RotatedAt, &lastUsed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SubsonicCredential{}, ErrSubsonicNoCredential
		}
		return SubsonicCredential{}, fmt.Errorf("get subsonic credential: %w", err)
	}
	c.LastUsedAt = lastUsed
	return c, nil
}

// RotateSubsonicCredential creates (or replaces) the user's credential with
// a freshly generated secret and returns it. Rotation invalidates every
// client configured with the old secret.
func (a *App) RotateSubsonicCredential(ctx context.Context, userID int64) (SubsonicCredential, error) {
	secret, err := newSubsonicSecret()
	if err != nil {
		return SubsonicCredential{}, err
	}
	var c SubsonicCredential
	var lastUsed *time.Time
	err = a.db.QueryRow(ctx, `
		INSERT INTO subsonic_credentials (user_id, secret)
		VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE
		  SET secret = EXCLUDED.secret, rotated_at = now(), last_used_at = NULL
		RETURNING user_id, secret, created_at, rotated_at, last_used_at
	`, userID, secret).Scan(&c.UserID, &c.Secret, &c.CreatedAt, &c.RotatedAt, &lastUsed)
	if err != nil {
		return SubsonicCredential{}, fmt.Errorf("rotate subsonic credential: %w", err)
	}
	c.LastUsedAt = lastUsed
	return c, nil
}

// RevokeSubsonicCredential deletes the user's credential; Subsonic clients
// stop authenticating immediately.
func (a *App) RevokeSubsonicCredential(ctx context.Context, userID int64) error {
	_, err := a.db.Exec(ctx, `DELETE FROM subsonic_credentials WHERE user_id = $1`, userID)
	if err != nil {
		return fmt.Errorf("revoke subsonic credential: %w", err)
	}
	return nil
}

// SubsonicAuthByUsername resolves a username to (user row, subsonic secret)
// for u+p / u+t+s verification. Distinguishes unknown user / missing
// credential (both ErrSubsonicNoCredential — indistinguishable to clients on
// purpose) from real DB errors.
func (a *App) SubsonicAuthByUsername(ctx context.Context, username string) (sqlc.User, string, error) {
	q := sqlc.New(a.db)
	user, err := q.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.User{}, "", ErrSubsonicNoCredential
		}
		return sqlc.User{}, "", fmt.Errorf("subsonic user lookup: %w", err)
	}
	cred, err := a.GetSubsonicCredential(ctx, user.ID)
	if err != nil {
		return sqlc.User{}, "", err
	}
	return user, cred.Secret, nil
}

// SubsonicAuthBySecret resolves an OpenSubsonic apiKey (= the credential
// secret) straight to its user.
func (a *App) SubsonicAuthBySecret(ctx context.Context, secret string) (sqlc.User, error) {
	if secret == "" {
		return sqlc.User{}, ErrSubsonicNoCredential
	}
	var userID int64
	err := a.db.QueryRow(ctx, `
		SELECT user_id FROM subsonic_credentials WHERE secret = $1
	`, secret).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.User{}, ErrSubsonicNoCredential
		}
		return sqlc.User{}, fmt.Errorf("subsonic apikey lookup: %w", err)
	}
	user, err := sqlc.New(a.db).GetUserByID(ctx, userID)
	if err != nil {
		return sqlc.User{}, fmt.Errorf("subsonic apikey user: %w", err)
	}
	return user, nil
}

// TouchSubsonicCredential stamps last_used_at asynchronously — called on
// successful auth; failure is inconsequential.
func (a *App) TouchSubsonicCredential(userID int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = a.db.Exec(ctx, `UPDATE subsonic_credentials SET last_used_at = now() WHERE user_id = $1`, userID)
	}()
}
