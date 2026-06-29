package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/service"
	"github.com/stretchr/testify/assert"
)

// testAPI builds a Huma test API backed by a zero-valued service.App. Useful
// for operation-contract tests that don't need a live database — Huma runs
// input validation BEFORE the handler closure, so 400 / 401 / 405 paths can
// be exercised without ever touching app.X() methods. Tests that need real
// DB-backed responses (happy paths) should set up a Postgres fixture
// instead.
func testAPI(t *testing.T) humatest.TestAPI {
	t.Helper()
	mux := http.NewServeMux()
	api := BuildAPI(mux, &service.App{}, &config.Config{})
	return humatest.Wrap(t, api)
}

// statusOf is a one-liner reader for httptest results — the verbose
// `resp.Result().StatusCode` reads poorly inline.
func statusOf(r *httptest.ResponseRecorder) int { return r.Result().StatusCode }

func TestAuthLoginValidation(t *testing.T) {
	api := testAPI(t)

	t.Run("empty body rejected", func(t *testing.T) {
		resp := api.Post("/api/auth/login", map[string]any{})
		assert.Equal(t, http.StatusUnprocessableEntity, statusOf(resp),
			"missing required fields should fail Huma input validation")
	})

	t.Run("empty username rejected", func(t *testing.T) {
		resp := api.Post("/api/auth/login", map[string]any{
			"username": "",
			"password": "hunter2hunter2",
		})
		assert.Equal(t, http.StatusUnprocessableEntity, statusOf(resp),
			"minLength:1 on username should trip on empty string")
	})

	t.Run("oversize username rejected", func(t *testing.T) {
		long := make([]byte, 100)
		for i := range long {
			long[i] = 'a'
		}
		resp := api.Post("/api/auth/login", map[string]any{
			"username": string(long),
			"password": "hunter2hunter2",
		})
		assert.Equal(t, http.StatusUnprocessableEntity, statusOf(resp),
			"maxLength:64 on username should reject 100-byte string")
	})
}

func TestAuthRegisterValidation(t *testing.T) {
	api := testAPI(t)

	t.Run("missing email rejected", func(t *testing.T) {
		resp := api.Post("/api/auth/register", map[string]any{
			"username": "alice",
			"password": "hunter2hunter2",
		})
		assert.Equal(t, http.StatusUnprocessableEntity, statusOf(resp))
	})

	t.Run("short password rejected", func(t *testing.T) {
		resp := api.Post("/api/auth/register", map[string]any{
			"username": "alice",
			"email":    "alice@example.com",
			"password": "short",
		})
		assert.Equal(t, http.StatusUnprocessableEntity, statusOf(resp),
			"minLength:8 on password should reject 5-char string")
	})

	t.Run("malformed email rejected", func(t *testing.T) {
		resp := api.Post("/api/auth/register", map[string]any{
			"username": "alice",
			"email":    "not-an-email",
			"password": "hunter2hunter2",
		})
		assert.Equal(t, http.StatusUnprocessableEntity, statusOf(resp),
			"format:email on email field should reject bad format")
	})
}

func TestAuthMeRequiresBearer(t *testing.T) {
	api := testAPI(t)

	t.Run("no bearer returns 401", func(t *testing.T) {
		resp := api.Get("/api/auth/me")
		assert.Equal(t, http.StatusUnauthorized, statusOf(resp),
			"authMiddleware should reject requests without a bearer token before handler runs")
	})

	t.Run("empty bearer returns 401", func(t *testing.T) {
		resp := api.Get("/api/auth/me", "Authorization: Bearer ")
		assert.Equal(t, http.StatusUnauthorized, statusOf(resp),
			"extractHumaToken treats whitespace-only bearer as no token")
	})
}

func TestHumaAuthInjectsTokenContext(t *testing.T) {
	mux := http.NewServeMux()
	api := newHumaAPI(mux, fakeSessions{})
	huma.Register(api, secured(op(http.MethodGet, "/test-token", "test-token", "Token context test", "Test")),
		func(ctx context.Context, _ *struct{}) (*JSONOutput[map[string]string], error) {
			return noStoreJSON(map[string]string{"token": auth.TokenFromContext(ctx)}), nil
		})
	tapi := humatest.Wrap(t, api)

	resp := tapi.Get("/test-token", "Authorization: Bearer user-token")
	assert.Equal(t, http.StatusOK, statusOf(resp))
	assert.Contains(t, resp.Body.String(), "user-token")
}

func TestAllowsQueryTokenOnlyForBrowserTransports(t *testing.T) {
	allowed := []string{
		"/api/stream/{file_id}",
		"/api/stream/{file_id}/hls/master.m3u8",
		"/api/stream/{file_id}/hls/index.m3u8",
		"/api/stream/{file_id}/hls/{segment}",
		"/api/stream/{file_id}/subtitles/{index}",
		"/api/stream/{file_id}/trickplay/index.vtt",
		"/api/stream/{file_id}/trickplay/{filename}",
		"/api/music/tracks/{id}/stream",
		"/api/music/tracks/{id}/file/{track_file_id}",
		"/api/radio/stream",
		"/api/podcasts/episode/stream",
		"/api/ws",
	}
	for _, path := range allowed {
		t.Run(path, func(t *testing.T) {
			assert.True(t, allowsQueryToken(&huma.Operation{Path: path}))
		})
	}

	denied := []string{
		"/api/auth/me",
		"/api/media",
		"/api/music/tracks/{id}",
		"/api/stream/{file_id}/info",
		"/api/stream/{file_id}/transcode-status",
		"/api/stream/{file_id}/subtitles",
	}
	for _, path := range denied {
		t.Run(path, func(t *testing.T) {
			assert.False(t, allowsQueryToken(&huma.Operation{Path: path}))
		})
	}
}
