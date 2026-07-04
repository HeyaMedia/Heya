package auth

import (
	"context"
)

type contextKey string

const tokenContextKey contextKey = "session_token"

// TokenFromContext returns the session token backing the current request.
// Used by handlers that need to operate on or about the current session
// (e.g. "is this the device I'm signed in on?", "sign out other devices").
func TokenFromContext(ctx context.Context) string {
	tok, _ := ctx.Value(tokenContextKey).(string)
	return tok
}

func ContextWithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenContextKey, token)
}
