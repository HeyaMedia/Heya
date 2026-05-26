package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

type contextKey string

const (
	userContextKey  contextKey = "user"
	tokenContextKey contextKey = "session_token"
)

type SessionLookup interface {
	GetSessionByToken(ctx context.Context, token string) (sqlc.Session, error)
	GetUserByID(ctx context.Context, id int64) (sqlc.User, error)
	TouchSession(ctx context.Context, token string) error
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
				// Distinguish "session not found / expired" (genuine 401 — the
				// user's token is invalid, the FE should kick them to /login)
				// from a database error (postgres unreachable, query timeout,
				// etc.) which should NOT be reported as unauthorized — that
				// would log the user out for a transient backend blip.
				if errors.Is(err, pgx.ErrNoRows) {
					http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				} else {
					http.Error(w, `{"error":"session lookup failed"}`, http.StatusServiceUnavailable)
				}
				return
			}

			user, err := db.GetUserByID(r.Context(), session.UserID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				} else {
					http.Error(w, `{"error":"user lookup failed"}`, http.StatusServiceUnavailable)
				}
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, user)
			ctx = context.WithValue(ctx, tokenContextKey, token)

			// Fire-and-forget the last_seen_at bump on a detached context so
			// the response isn't held up by the write. We DELIBERATELY use
			// context.Background here — the request context cancels as
			// soon as the response is written, but the touch should still
			// complete. The SQL throttle in TouchSession means most of
			// these are no-op UPDATEs.
			//nolint:gosec // G118 false positive — detached ctx is intentional
			go func(tok string) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = db.TouchSession(bgCtx, tok)
			}(token)

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
