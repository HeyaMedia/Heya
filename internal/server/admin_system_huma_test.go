package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/jackc/pgx/v5"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/logbuf"
	"github.com/karbowiak/heya/internal/service"
	"github.com/stretchr/testify/assert"
)

// fakeSessions is a stand-in for the production SessionLookup. It recognises
// two well-known tokens: "admin-token" → admin user, "user-token" → regular
// user. Anything else returns pgx.ErrNoRows so the auth middleware can map
// it to 401 the same way it would in production.
type fakeSessions struct{}

func (f fakeSessions) GetSessionWithUserByToken(ctx context.Context, tokenHash string) (sqlc.GetSessionWithUserByTokenRow, error) {
	var session sqlc.Session
	switch tokenHash {
	case auth.TokenHash("admin-token"):
		session = sqlc.Session{ID: 1, UserID: 1, TokenHash: tokenHash, Kind: "session"}
	case auth.TokenHash("user-token"):
		session = sqlc.Session{ID: 2, UserID: 2, TokenHash: tokenHash, Kind: "session"}
	default:
		return sqlc.GetSessionWithUserByTokenRow{}, pgx.ErrNoRows
	}
	user, err := f.GetUserByID(ctx, session.UserID)
	if err != nil {
		return sqlc.GetSessionWithUserByTokenRow{}, err
	}
	return sqlc.GetSessionWithUserByTokenRow{Session: session, User: user}, nil
}

func (fakeSessions) GetUserByID(ctx context.Context, id int64) (sqlc.User, error) {
	switch id {
	case 1:
		return sqlc.User{ID: 1, Username: "admin", Email: "admin@example.com", IsAdmin: true}, nil
	case 2:
		return sqlc.User{ID: 2, Username: "alice", Email: "alice@example.com", IsAdmin: false}, nil
	}
	return sqlc.User{}, pgx.ErrNoRows
}

func (fakeSessions) TouchSession(ctx context.Context, token string) error { return nil }

// authedAPI builds a humatest API with a fake SessionLookup so secured
// operations can be exercised with synthetic admin / non-admin bearers.
// The injected App is still zero-valued — handlers that reach a real
// service method will panic on a nil pool. Tests here stay above that
// line: we only assert the 401 / 403 / 422 boundaries.
func authedAPI(t *testing.T) humatest.TestAPI {
	t.Helper()
	mux := http.NewServeMux()
	api := BuildAPI(mux, &service.App{}, &config.Config{}, WithSessions(fakeSessions{}))
	return humatest.Wrap(t, api)
}

// Compile-time assertion: fakeSessions must satisfy the SessionLookup contract
// exactly. If the interface grows a method, this line breaks the build before
// any test runs.
var _ auth.SessionLookup = fakeSessions{}

// adminRoutes lists every admin_system endpoint with a representative valid
// body so the 401 / 403 sweeps stay table-driven. Adding a new admin route
// without an entry here means it ships with no coverage — intentional.
var adminRoutes = []struct {
	name   string
	method string
	path   string
	body   map[string]any
}{
	{"system", http.MethodGet, "/api/admin/system", nil},
	{"storage", http.MethodGet, "/api/admin/storage", nil},
	{"storage/scan", http.MethodPost, "/api/admin/storage/scan", map[string]any{"library_id": 0}},
	{"db", http.MethodGet, "/api/admin/db", nil},
	{"doctor", http.MethodGet, "/api/admin/doctor", nil},
	{"listeners", http.MethodGet, "/api/admin/listeners", nil},
	{"log-level/get", http.MethodGet, "/api/admin/log-level", nil},
	{"log-level/put", http.MethodPut, "/api/admin/log-level", map[string]any{"level": "info"}},
	{"sessions/list", http.MethodGet, "/api/admin/sessions", nil},
	{"sessions/delete", http.MethodDelete, "/api/admin/sessions/1", nil},
	{"users/list", http.MethodGet, "/api/admin/users", nil},
	{"users/create", http.MethodPost, "/api/admin/users", map[string]any{
		"username": "alice", "email": "a@b.co", "password": "hunter2hunter2",
	}},
	{"users/delete", http.MethodDelete, "/api/admin/users/1", nil},
	{"users/role", http.MethodPatch, "/api/admin/users/1/role", map[string]any{"is_admin": true}},
	{"users/password", http.MethodPost, "/api/admin/users/1/password", map[string]any{
		"new_password": "hunter2hunter2",
	}},
	{"sonic/status", http.MethodGet, "/api/admin/sonicanalysis/status", nil},
	{"logs", http.MethodGet, "/api/logs", nil},
}

// fire dispatches an HTTP request through humatest using the right verb. The
// trailing strings become headers, so callers can attach a bearer.
func fire(api humatest.TestAPI, method, path string, body map[string]any, headers ...string) int {
	switch method {
	case http.MethodGet:
		args := append([]any{}, toAny(headers)...)
		return api.Get(path, args...).Result().StatusCode
	case http.MethodPost:
		args := append([]any{body}, toAny(headers)...)
		return api.Post(path, args...).Result().StatusCode
	case http.MethodPut:
		args := append([]any{body}, toAny(headers)...)
		return api.Put(path, args...).Result().StatusCode
	case http.MethodPatch:
		args := append([]any{body}, toAny(headers)...)
		return api.Patch(path, args...).Result().StatusCode
	case http.MethodDelete:
		args := append([]any{}, toAny(headers)...)
		return api.Delete(path, args...).Result().StatusCode
	}
	return 0
}

func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func TestAdminSystemRoutesRequireBearer(t *testing.T) {
	api := testAPI(t)
	for _, r := range adminRoutes {
		t.Run(r.name, func(t *testing.T) {
			code := fire(api, r.method, r.path, r.body)
			assert.Equal(t, http.StatusUnauthorized, code,
				"unauthenticated %s %s should hit the bearer gate before the handler", r.method, r.path)
		})
	}
}

func TestAdminSystemRoutesRequireAdmin(t *testing.T) {
	api := authedAPI(t)
	for _, r := range adminRoutes {
		t.Run(r.name, func(t *testing.T) {
			code := fire(api, r.method, r.path, r.body, "Authorization: Bearer user-token")
			assert.Equal(t, http.StatusForbidden, code,
				"non-admin %s %s should be rejected by adminMiddleware", r.method, r.path)
		})
	}
}

func TestLogStreamRequiresAdmin(t *testing.T) {
	mux := http.NewServeMux()
	api := BuildAPI(mux, &service.App{}, &config.Config{}, WithSessions(fakeSessions{}), WithLogBuffer(logbuf.New(1)))
	tapi := humatest.Wrap(t, api)

	code := tapi.Get("/api/logs/stream", "Authorization: Bearer user-token").Result().StatusCode
	assert.Equal(t, http.StatusForbidden, code)
}

// Input-validation tests use the admin bearer so we pass the gate, then
// trip on Huma's schema check. Tests stop at 422; running the handler
// would panic on the zero-valued App.

func TestAdminLogLevelEnumValidation(t *testing.T) {
	api := authedAPI(t)

	t.Run("unknown level rejected", func(t *testing.T) {
		code := fire(api, http.MethodPut, "/api/admin/log-level",
			map[string]any{"level": "verbose"},
			"Authorization: Bearer admin-token")
		assert.Equal(t, http.StatusUnprocessableEntity, code,
			"level enum should reject 'verbose'")
	})

	t.Run("empty level rejected", func(t *testing.T) {
		code := fire(api, http.MethodPut, "/api/admin/log-level",
			map[string]any{"level": ""},
			"Authorization: Bearer admin-token")
		assert.Equal(t, http.StatusUnprocessableEntity, code,
			"level enum should reject empty string")
	})
}

func TestAdminCreateUserValidation(t *testing.T) {
	api := authedAPI(t)
	hdr := "Authorization: Bearer admin-token"

	t.Run("short password rejected", func(t *testing.T) {
		code := fire(api, http.MethodPost, "/api/admin/users", map[string]any{
			"username": "alice", "email": "alice@example.com", "password": "short",
		}, hdr)
		assert.Equal(t, http.StatusUnprocessableEntity, code,
			"minLength:8 on password should reject 5-char string")
	})

	t.Run("malformed email rejected", func(t *testing.T) {
		code := fire(api, http.MethodPost, "/api/admin/users", map[string]any{
			"username": "alice", "email": "not-an-email", "password": "hunter2hunter2",
		}, hdr)
		assert.Equal(t, http.StatusUnprocessableEntity, code,
			"format:email should reject non-email")
	})

	t.Run("missing username rejected", func(t *testing.T) {
		code := fire(api, http.MethodPost, "/api/admin/users", map[string]any{
			"email": "alice@example.com", "password": "hunter2hunter2",
		}, hdr)
		assert.Equal(t, http.StatusUnprocessableEntity, code,
			"required:username should reject body without it")
	})
}

func TestAdminUserPasswordValidation(t *testing.T) {
	api := authedAPI(t)

	t.Run("short password rejected", func(t *testing.T) {
		code := fire(api, http.MethodPost, "/api/admin/users/1/password",
			map[string]any{"new_password": "short"},
			"Authorization: Bearer admin-token")
		assert.Equal(t, http.StatusUnprocessableEntity, code,
			"minLength:8 on new_password should reject 5-char string")
	})
}

func TestAdminSessionIDPathValidation(t *testing.T) {
	api := authedAPI(t)

	t.Run("zero id rejected", func(t *testing.T) {
		code := fire(api, http.MethodDelete, "/api/admin/sessions/0", nil,
			"Authorization: Bearer admin-token")
		assert.Equal(t, http.StatusUnprocessableEntity, code,
			"minimum:1 on session id should reject 0")
	})
}
