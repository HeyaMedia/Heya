package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/karbowiak/heya/internal/database/sqlc"
)

type contextKey string

const (
	userContextKey  contextKey = "user"
	tokenContextKey contextKey = "session_token"
)

func Middleware(db SessionLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			resolved, err := ResolveSession(r.Context(), db, token)
			if err != nil {
				if errors.Is(err, ErrInvalidSession) {
					http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				} else {
					http.Error(w, `{"error":"session lookup failed"}`, http.StatusServiceUnavailable)
				}
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, resolved.User)
			ctx = context.WithValue(ctx, tokenContextKey, resolved.Token)

			TouchSessionAsync(db, resolved.Token)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserFromContext(ctx context.Context) (sqlc.User, bool) {
	user, ok := ctx.Value(userContextKey).(sqlc.User)
	return user, ok
}

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

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if c, err := r.Cookie("session_token"); err == nil {
		return c.Value
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	return ""
}
