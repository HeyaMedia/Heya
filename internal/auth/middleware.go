package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/karbowiak/kura/internal/database/sqlc"
)

type contextKey string

const userContextKey contextKey = "user"

type SessionLookup interface {
	GetSessionByToken(ctx context.Context, token string) (sqlc.Session, error)
	GetUserByID(ctx context.Context, id int64) (sqlc.User, error)
}

func Middleware(db SessionLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			session, err := db.GetSessionByToken(r.Context(), token)
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			user, err := db.GetUserByID(r.Context(), session.UserID)
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserFromContext(ctx context.Context) (sqlc.User, bool) {
	user, ok := ctx.Value(userContextKey).(sqlc.User)
	return user, ok
}

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if c, err := r.Cookie("session_token"); err == nil {
		return c.Value
	}
	return ""
}
