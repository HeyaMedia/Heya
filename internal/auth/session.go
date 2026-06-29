package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

var ErrInvalidSession = errors.New("invalid session")

type SessionLookup interface {
	GetSessionByToken(ctx context.Context, tokenHash string) (sqlc.Session, error)
	GetUserByID(ctx context.Context, id int64) (sqlc.User, error)
	TouchSession(ctx context.Context, tokenHash string) error
}

type SessionResolution struct {
	Session sqlc.Session
	User    sqlc.User
	Token   string
}

func ResolveSession(ctx context.Context, db SessionLookup, token string) (SessionResolution, error) {
	if token == "" || db == nil {
		return SessionResolution{}, ErrInvalidSession
	}
	session, err := db.GetSessionByToken(ctx, TokenHash(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SessionResolution{}, ErrInvalidSession
		}
		return SessionResolution{}, fmt.Errorf("session lookup failed: %w", err)
	}
	user, err := db.GetUserByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SessionResolution{}, ErrInvalidSession
		}
		return SessionResolution{}, fmt.Errorf("user lookup failed: %w", err)
	}
	return SessionResolution{Session: session, User: user, Token: token}, nil
}

func TouchSessionAsync(db SessionLookup, token string) {
	if db == nil || token == "" {
		return
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = db.TouchSession(bgCtx, TokenHash(token))
	}()
}
