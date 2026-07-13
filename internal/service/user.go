package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

var ErrRegistrationClosed = errors.New("registration is closed")

func (a *App) CreateUser(ctx context.Context, username, email, password string, isAdmin bool) (sqlc.User, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return sqlc.User{}, fmt.Errorf("hashing password: %w", err)
	}

	q := sqlc.New(a.db)

	count, err := q.CountUsers(ctx)
	if err != nil {
		return sqlc.User{}, fmt.Errorf("counting users: %w", err)
	}
	if count == 0 {
		isAdmin = true
	}

	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		IsAdmin:      isAdmin,
	})
	if err != nil {
		return sqlc.User{}, fmt.Errorf("creating user: %w", err)
	}

	return user, nil
}

func (a *App) RegisterFirstUser(ctx context.Context, username, email, password string) (sqlc.User, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return sqlc.User{}, fmt.Errorf("hashing password: %w", err)
	}

	tx, err := a.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return sqlc.User{}, fmt.Errorf("begin registration: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "LOCK TABLE users IN EXCLUSIVE MODE"); err != nil {
		return sqlc.User{}, fmt.Errorf("lock users: %w", err)
	}

	q := sqlc.New(tx)
	count, err := q.CountUsers(ctx)
	if err != nil {
		return sqlc.User{}, fmt.Errorf("counting users: %w", err)
	}
	if count > 0 {
		return sqlc.User{}, ErrRegistrationClosed
	}

	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		IsAdmin:      true,
	})
	if err != nil {
		return sqlc.User{}, fmt.Errorf("creating user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.User{}, fmt.Errorf("commit registration: %w", err)
	}
	return user, nil
}

func (a *App) Authenticate(ctx context.Context, username, password string) (sqlc.User, error) {
	q := sqlc.New(a.db)

	user, err := q.GetUserByUsername(ctx, username)
	if err != nil {
		return sqlc.User{}, fmt.Errorf("invalid credentials")
	}

	if !auth.CheckPassword(user.PasswordHash, password) {
		return sqlc.User{}, fmt.Errorf("invalid credentials")
	}

	return user, nil
}

// CreateAuthSession is the login/register session-mint path. Browser and CLI
// logins land here. Sessions live 30 days; user_agent is captured for the
// "My sessions" page; ip is best-effort (caller passes "" if not derivable).
func (a *App) CreateAuthSession(ctx context.Context, userID int64, userAgent, ip string) (string, error) {
	token, err := auth.GenerateToken()
	if err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}

	q := sqlc.New(a.db)
	_, err = q.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:    userID,
		TokenHash: auth.TokenHash(token),
		ExpiresAt: pgTimestamptz(time.Now().Add(30 * 24 * time.Hour)),
		Kind:      "session",
		Name:      pgText(""),
		UserAgent: pgText(userAgent),
		Ip:        pgText(ip),
	})
	if err != nil {
		return "", fmt.Errorf("creating session: %w", err)
	}

	return token, nil
}

func (a *App) DeleteSession(ctx context.Context, token string) error {
	q := sqlc.New(a.db)
	return q.DeleteSession(ctx, auth.TokenHash(token))
}

// ChangePassword verifies the current password before swapping the hash.
// Returns an error sentinel matched by the HTTP layer to surface a 401
// for a wrong current password vs 500 for a hashing/DB problem.
var ErrWrongPassword = fmt.Errorf("current password is incorrect")

func (a *App) ChangePassword(ctx context.Context, userID int64, currentPassword, newPassword string) error {
	q := sqlc.New(a.db)
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("user lookup: %w", err)
	}
	if !auth.CheckPassword(user.PasswordHash, currentPassword) {
		return ErrWrongPassword
	}
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hashing: %w", err)
	}
	return q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
		ID:           userID,
		PasswordHash: hash,
	})
}

// AuthSessionView is the redacted shape returned to the user — token is
// never exposed; the FE only needs identity + activity metadata for the
// "My sessions" panel.
type AuthSessionView struct {
	ID         int64      `json:"id"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	UserAgent  string     `json:"user_agent,omitempty"`
	IP         string     `json:"ip,omitempty"`
	Current    bool       `json:"current"`
}

func (a *App) ListAuthSessions(ctx context.Context, userID int64, currentToken string) ([]AuthSessionView, error) {
	q := sqlc.New(a.db)
	rows, err := q.ListUserSessionsByKind(ctx, sqlc.ListUserSessionsByKindParams{
		UserID: userID,
		Kind:   "session",
	})
	if err != nil {
		return nil, err
	}
	currentHash := auth.TokenHash(currentToken)
	out := make([]AuthSessionView, 0, len(rows))
	for _, s := range rows {
		view := AuthSessionView{
			ID:         s.ID,
			CreatedAt:  s.CreatedAt.Time,
			LastSeenAt: s.LastSeenAt.Time,
			UserAgent:  s.UserAgent.String,
			IP:         s.Ip.String,
			Current:    s.TokenHash == currentHash,
		}
		if s.ExpiresAt.Valid {
			t := s.ExpiresAt.Time
			view.ExpiresAt = &t
		}
		out = append(out, view)
	}
	return out, nil
}

func (a *App) RevokeAuthSession(ctx context.Context, userID, sessionID int64) error {
	q := sqlc.New(a.db)
	return q.DeleteUserSessionByID(ctx, sqlc.DeleteUserSessionByIDParams{
		ID:     sessionID,
		UserID: userID,
	})
}

func (a *App) RevokeOtherAuthSessions(ctx context.Context, userID int64, currentToken string) error {
	q := sqlc.New(a.db)
	return q.DeleteUserOtherSessions(ctx, sqlc.DeleteUserOtherSessionsParams{
		UserID:    userID,
		TokenHash: auth.TokenHash(currentToken),
	})
}

// ApiTokenView is what list returns. The plaintext token is only returned
// from CreateApiToken (CreateApiTokenResult.PlaintextToken) and is never
// retrievable after — a lost token must be rotated.
type ApiTokenView struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type CreateApiTokenResult struct {
	ApiTokenView
	PlaintextToken string `json:"token"`
}

func (a *App) ListApiTokens(ctx context.Context, userID int64) ([]ApiTokenView, error) {
	q := sqlc.New(a.db)
	rows, err := q.ListUserSessionsByKind(ctx, sqlc.ListUserSessionsByKindParams{
		UserID: userID,
		Kind:   "api_token",
	})
	if err != nil {
		return nil, err
	}
	out := make([]ApiTokenView, 0, len(rows))
	for _, s := range rows {
		view := ApiTokenView{
			ID:         s.ID,
			Name:       s.Name.String,
			CreatedAt:  s.CreatedAt.Time,
			LastSeenAt: s.LastSeenAt.Time,
		}
		if s.ExpiresAt.Valid {
			t := s.ExpiresAt.Time
			view.ExpiresAt = &t
		}
		out = append(out, view)
	}
	return out, nil
}

// CreateApiToken mints a new long-lived token. expiresIn = 0 means "never
// expires" — the sessions row gets a NULL expires_at, which GetSessionByToken
// treats as always-valid.
func (a *App) CreateApiToken(ctx context.Context, userID int64, name string, expiresIn time.Duration) (CreateApiTokenResult, error) {
	token, err := auth.GenerateToken()
	if err != nil {
		return CreateApiTokenResult{}, fmt.Errorf("generating token: %w", err)
	}

	var expiresAt pgtype.Timestamptz
	if expiresIn > 0 {
		expiresAt = pgTimestamptz(time.Now().Add(expiresIn))
	}

	q := sqlc.New(a.db)
	row, err := q.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:    userID,
		TokenHash: auth.TokenHash(token),
		ExpiresAt: expiresAt,
		Kind:      "api_token",
		Name:      pgText(name),
		UserAgent: pgText(""),
		Ip:        pgText(""),
	})
	if err != nil {
		return CreateApiTokenResult{}, fmt.Errorf("creating token: %w", err)
	}

	result := CreateApiTokenResult{
		ApiTokenView: ApiTokenView{
			ID:         row.ID,
			Name:       row.Name.String,
			CreatedAt:  row.CreatedAt.Time,
			LastSeenAt: row.LastSeenAt.Time,
		},
		PlaintextToken: token,
	}
	if row.ExpiresAt.Valid {
		t := row.ExpiresAt.Time
		result.ExpiresAt = &t
	}
	return result, nil
}

func (a *App) RevokeApiToken(ctx context.Context, userID, tokenID int64) error {
	q := sqlc.New(a.db)
	return q.DeleteUserSessionByID(ctx, sqlc.DeleteUserSessionByIDParams{
		ID:     tokenID,
		UserID: userID,
	})
}

func (a *App) ListUsers(ctx context.Context) ([]sqlc.User, error) {
	q := sqlc.New(a.db)
	return q.ListUsers(ctx)
}

// AdminSessionView is the admin-only roster shape — includes the owning
// username + role and the session kind (browser session vs api_token), but
// never the token bytes themselves.
type AdminSessionView struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Username   string     `json:"username"`
	IsAdmin    bool       `json:"is_admin"`
	Kind       string     `json:"kind"`
	Name       string     `json:"name,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt time.Time  `json:"last_seen_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	UserAgent  string     `json:"user_agent,omitempty"`
	IP         string     `json:"ip,omitempty"`
}

func (a *App) ListAllSessionsForAdmin(ctx context.Context) ([]AdminSessionView, error) {
	q := sqlc.New(a.db)
	rows, err := q.ListAllSessionsForAdmin(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AdminSessionView, 0, len(rows))
	for _, r := range rows {
		v := AdminSessionView{
			ID:         r.ID,
			UserID:     r.UserID,
			Username:   r.Username,
			IsAdmin:    r.IsAdmin,
			Kind:       r.Kind,
			Name:       r.Name.String,
			CreatedAt:  r.CreatedAt.Time,
			LastSeenAt: r.LastSeenAt.Time,
			UserAgent:  r.UserAgent.String,
			IP:         r.Ip.String,
		}
		if r.ExpiresAt.Valid {
			t := r.ExpiresAt.Time
			v.ExpiresAt = &t
		}
		out = append(out, v)
	}
	return out, nil
}

func (a *App) RevokeAnySession(ctx context.Context, sessionID int64) error {
	q := sqlc.New(a.db)
	return q.DeleteSessionByIDAdmin(ctx, sessionID)
}

func (a *App) DeleteUser(ctx context.Context, username string) error {
	q := sqlc.New(a.db)

	user, err := q.GetUserByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %s", username)
	}

	if err := q.DeleteUser(ctx, user.ID); err != nil {
		return err
	}
	a.stopCastSessionsForUser(user.ID)
	return nil
}

func (a *App) ResetPassword(ctx context.Context, username, newPassword string) error {
	q := sqlc.New(a.db)

	user, err := q.GetUserByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %s", username)
	}

	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	return q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
		ID:           user.ID,
		PasswordHash: hash,
	})
}

// DeleteUserByID is the admin-console variant of DeleteUser — no username
// lookup, no "user not found" check (admin already has the row).
func (a *App) DeleteUserByID(ctx context.Context, userID int64) error {
	q := sqlc.New(a.db)
	if err := q.DeleteUser(ctx, userID); err != nil {
		return err
	}
	a.stopCastSessionsForUser(userID)
	return nil
}

// SetUserAdmin flips the is_admin flag without touching username/email.
// Uses UpdateUser since there's no narrower setter; reads the row first so
// username + email don't get clobbered.
func (a *App) SetUserAdmin(ctx context.Context, userID int64, isAdmin bool) (sqlc.User, error) {
	q := sqlc.New(a.db)
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return sqlc.User{}, fmt.Errorf("user not found")
	}
	updated, err := q.UpdateUser(ctx, sqlc.UpdateUserParams{
		ID:       userID,
		Username: user.Username,
		Email:    user.Email,
		IsAdmin:  isAdmin,
	})
	if err != nil {
		return sqlc.User{}, err
	}
	if !a.CastAccessAllowed(updated.ID, updated.IsAdmin) {
		a.stopCastSessionsForUser(updated.ID)
	}
	return updated, nil
}

// ResetPasswordByID is the admin-only password reset — no current-password
// check, scoped by ID so the admin console doesn't need to round-trip through
// username lookup.
func (a *App) ResetPasswordByID(ctx context.Context, userID int64, newPassword string) error {
	q := sqlc.New(a.db)
	hash, err := auth.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hashing: %w", err)
	}
	return q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
		ID:           userID,
		PasswordHash: hash,
	})
}
