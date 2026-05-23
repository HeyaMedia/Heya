package service

import (
	"context"
	"fmt"
	"time"

	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

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

func (a *App) CreateSession(ctx context.Context, userID int64) (string, error) {
	token, err := auth.GenerateToken()
	if err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}

	q := sqlc.New(a.db)
	_, err = q.CreateSession(ctx, sqlc.CreateSessionParams{
		UserID:    userID,
		Token:     token,
		ExpiresAt: pgTimestamptz(time.Now().Add(30 * 24 * time.Hour)),
	})
	if err != nil {
		return "", fmt.Errorf("creating session: %w", err)
	}

	return token, nil
}

func (a *App) DeleteSession(ctx context.Context, token string) error {
	q := sqlc.New(a.db)
	return q.DeleteSession(ctx, token)
}

func (a *App) ListUsers(ctx context.Context) ([]sqlc.User, error) {
	q := sqlc.New(a.db)
	return q.ListUsers(ctx)
}

func (a *App) DeleteUser(ctx context.Context, username string) error {
	q := sqlc.New(a.db)

	user, err := q.GetUserByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("user not found: %s", username)
	}

	return q.DeleteUser(ctx, user.ID)
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
