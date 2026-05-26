package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	gojson "github.com/goccy/go-json"
	"github.com/karbowiak/heya/internal/auth"
	"github.com/karbowiak/heya/internal/database/sqlc"
)

// goccyJSONFormat replaces Huma's encoding/json default with goccy/go-json,
// which is a drop-in faster encoder/decoder. Most of our payloads are
// type-stable lists (media items, tracks, jobs) where goccy's reflection
// caches pay off; bench /api/media/enriched?limit=5000 to see the delta.
//
// Marshal mirrors DefaultJSONFormat: SetEscapeHTML(false) so JSON-in-string
// fields (URLs, paths) don't get HTML-escaped — the response is consumed by
// fetch/openapi-fetch, not embedded into HTML.
var goccyJSONFormat = huma.Format{
	Marshal: func(w io.Writer, v any) error {
		enc := gojson.NewEncoder(w)
		enc.SetEscapeHTML(false)
		return enc.Encode(v)
	},
	Unmarshal: gojson.Unmarshal,
}

// newHumaAPI creates the Huma API on the supplied mux. Operations registered
// against the returned API are served by the same mux as the legacy stdlib
// handlers, so we can migrate route-by-route without breaking anything.
//
// The auth middleware reads each operation's Security list. When an operation
// declares the "bearer" scheme, we require a valid session token and inject
// the user into the request context. Operations without a Security list are
// public (login, health, image proxies, etc.).
//
// The admin middleware looks for the "admin" extension flag on the operation
// metadata and rejects non-admin users.
func newHumaAPI(mux *http.ServeMux, sessions auth.SessionLookup) huma.API {
	cfg := huma.DefaultConfig("Heya Media Server API", "1.0.0")
	cfg.Info.Description = "Self-hosted media server for movies, TV, music, and books."
	cfg.Info.Contact = &huma.Contact{Name: "Heya", URL: "https://heya.media"}
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer": { //nolint:gosec // G101 false positive: this is a security scheme definition, not a credential
			Type:         "http",
			Scheme:       "bearer",
			BearerFormat: "opaque session token",
		},
	}
	// /api/docs is served by scalarHandler instead — Huma's bundled stoplight
	// docs page is fine, but Scalar matches the existing UX.
	cfg.DocsPath = ""

	// Swap encoding/json for goccy/go-json on both legs. Same on-wire output,
	// noticeably faster for the list-heavy endpoints (enriched media, music
	// listings) that dominate the FE's payload bytes.
	cfg.Formats = map[string]huma.Format{
		"application/json": goccyJSONFormat,
		"json":             goccyJSONFormat,
	}

	api := humago.New(mux, cfg)
	api.UseMiddleware(authMiddleware(api, sessions))
	api.UseMiddleware(adminMiddleware(api))
	return api
}

type ctxKey string

const userCtxKey ctxKey = "huma.user"

// userFrom pulls the authenticated user injected by authMiddleware. Caller is
// responsible for only calling this from operations that require auth — the
// returned user is the zero value when middleware did not inject one.
func userFrom(ctx context.Context) sqlc.User {
	u, _ := ctx.Value(userCtxKey).(sqlc.User)
	return u
}

// authMiddleware enforces the per-operation "bearer" security scheme. Routes
// without a Security entry are treated as public. Token extraction matches
// the legacy auth.Middleware: Authorization header, session_token cookie, or
// ?token= query (the last form is required for <video src> / <img src> tags
// that can't carry custom headers).
//
// `sessions` may be nil — in that case every secured operation returns 401
// without ever touching a database. The spec-dump CLI and humatest fixtures
// rely on this to register the full route set without a live App.
func authMiddleware(api huma.API, sessions auth.SessionLookup) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if !requiresAuth(ctx.Operation()) {
			next(ctx)
			return
		}

		token := extractHumaToken(ctx)
		if token == "" || sessions == nil {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized")
			return
		}

		session, err := sessions.GetSessionByToken(ctx.Context(), token)
		if err != nil {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := sessions.GetUserByID(ctx.Context(), session.UserID)
		if err != nil {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "unauthorized")
			return
		}

		ctx = huma.WithValue(ctx, userCtxKey, user)
		next(ctx)
	}
}

// adminMiddleware enforces operations tagged with the "admin" extension. It
// runs after authMiddleware so the user is already in context when present.
func adminMiddleware(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if !isAdminOnly(ctx.Operation()) {
			next(ctx)
			return
		}
		user := userFrom(ctx.Context())
		if !user.IsAdmin {
			_ = huma.WriteErr(api, ctx, http.StatusForbidden, "admin access required")
			return
		}
		next(ctx)
	}
}

func requiresAuth(op *huma.Operation) bool {
	for _, scheme := range op.Security {
		if _, ok := scheme["bearer"]; ok {
			return true
		}
	}
	return false
}

func isAdminOnly(op *huma.Operation) bool {
	if op.Metadata == nil {
		return false
	}
	v, ok := op.Metadata["admin"].(bool)
	return ok && v
}

func extractHumaToken(ctx huma.Context) string {
	if h := ctx.Header("Authorization"); h != "" {
		if strings.HasPrefix(h, "Bearer ") {
			return strings.TrimPrefix(h, "Bearer ")
		}
	}
	// Huma's Context doesn't expose Cookie directly — parse the Cookie header.
	if cookieHeader := ctx.Header("Cookie"); cookieHeader != "" {
		for _, part := range strings.Split(cookieHeader, ";") {
			kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(kv) == 2 && kv[0] == "session_token" && kv[1] != "" {
				return kv[1]
			}
		}
	}
	if t := ctx.Query("token"); t != "" {
		return t
	}
	return ""
}

// secured returns a copy of op with bearer auth required.
func secured(op huma.Operation) huma.Operation {
	op.Security = append(op.Security, map[string][]string{"bearer": nil})
	return op
}

// adminSecured returns a copy of op with bearer auth + admin gate.
func adminSecured(op huma.Operation) huma.Operation {
	op = secured(op)
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}
	op.Metadata["admin"] = true
	return op
}

// op is a tiny builder for the common case: method + path + summary + tag.
func op(method, path, opID, summary string, tag string) huma.Operation {
	return huma.Operation{
		OperationID: opID,
		Method:      method,
		Path:        path,
		Summary:     summary,
		Tags:        []string{tag},
	}
}

// --- Common input/output shapes -----------------------------------------

// Pagination is embedded in list inputs. Defaults match the historical
// behaviour of the manual parseInt32/parsePage helpers.
type Pagination struct {
	Limit  int32 `query:"limit" minimum:"1" maximum:"1000" default:"50" example:"50" doc:"Max results"`
	Offset int32 `query:"offset" minimum:"0" default:"0" example:"0" doc:"Results offset"`
}

// IDPath captures /{id} where id is a positive integer.
type IDPath struct {
	ID int64 `path:"id" minimum:"1" example:"42" doc:"Numeric ID"`
}

// SlugOrIDPath captures /{id} where id may be a numeric ID or URL slug.
type SlugOrIDPath struct {
	ID string `path:"id" maxLength:"200" example:"the-godfather" doc:"Numeric ID or slug"`
}

// StatusOutput is the canonical short ack response.
type StatusOutput struct {
	Body struct {
		Status string `json:"status" doc:"Action status"`
	}
}

func statusOK(status string) *StatusOutput {
	out := &StatusOutput{}
	out.Body.Status = status
	return out
}

// JSONOutput wraps any value as the typed Body for Huma. Prefer named typed
// structs over `JSONOutput[any]` so the generated OpenAPI is informative.
//
// The CacheControl header is empty by default (no header sent). Use the
// `cachedJSON` / `noStoreJSON` constructors to opt into caching headers, or
// set the field directly. ETag is computed and applied by the withETag
// middleware on the way out — handlers should leave it empty.
type JSONOutput[T any] struct {
	CacheControl string `header:"Cache-Control"`
	Body         T
}

// cachedJSON wraps body with `Cache-Control: private, max-age=<seconds>`.
// Use it on GET responses that vary per-user but tolerate short staleness
// (genre lists, search results, slow-moving metadata).
func cachedJSON[T any](body T, maxAgeSeconds int) *JSONOutput[T] {
	return &JSONOutput[T]{
		CacheControl: fmt.Sprintf("private, max-age=%d", maxAgeSeconds),
		Body:         body,
	}
}

// noStoreJSON wraps body with `Cache-Control: no-store`. Use for personal,
// rapidly changing, or sensitive responses (current user, job/queue state,
// activity feeds) where revalidation isn't safe.
func noStoreJSON[T any](body T) *JSONOutput[T] {
	return &JSONOutput[T]{
		CacheControl: "no-store",
		Body:         body,
	}
}

const scalarHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Heya API Reference</title>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <style>body { margin: 0; padding: 0; height: 100vh; }</style>
</head>
<body>
  <div id="app"></div>
  <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@latest/dist/browser/standalone.js"></script>
  <script>
    Scalar.createApiReference('#app', {
      url: '%s',
      theme: 'kepler',
      darkMode: true,
      hideModels: false,
      hideDownloadButton: false,
      authentication: {
        preferredSecurityScheme: 'bearer',
        http: { bearer: { token: '' } }
      }
    })
  </script>
</body>
</html>`

func scalarHandler(specURL string) http.HandlerFunc {
	page := fmt.Sprintf(scalarHTML, specURL)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}
}
