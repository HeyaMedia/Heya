# API follow-ups plan (post-Huma adoption)

Working doc for the multi-phase API-quality work that follows the Huma migration. Phase A is done (gzip, lifetime context, multipart-to-Huma, deeper health endpoints). This doc covers B → D in enough detail to resume work after a context compaction.

Tasks #17 – #26 in TaskList map 1:1 to the items below.

---

## Phase B — API quality pass

Backend-only. The biggest grind. ~3-4 hours. Three sub-tasks that touch overlapping handlers, so we do them together per-file rather than in three full-codebase sweeps.

### B1 + B2: Typed JSON bodies + Cache-Control + ETag (task #17)

**Goal:** every handler returns a typed body (no `JSONOutput[any]`), and every cacheable endpoint emits proper cache headers + ETag.

**Per-handler decisions:**

For each handler currently returning `JSONOutput[any]`:

1. If the service method returns a sqlc row type → use that type directly: `JSONOutput[sqlc.Library]`, `JSONOutput[[]sqlc.MediaItem]`.
2. If the service method returns `map[string]any` or hand-built ad-hoc → either:
   - Convert the service method to return a typed struct (preferred for shared shapes), **or**
   - Define an inline body struct next to the handler (fine for single-use).
3. Shared view types go in `<domain>_views.go` (new file per domain). Single-use bodies stay inline.

**Cache-Control + ETag decision matrix:**

| Endpoint pattern | Cache-Control | ETag source |
|---|---|---|
| `/api/genres`, `/api/genres/{name}`, `/api/keywords/{name}`, `/api/collections*` | `private, max-age=60` | body hash |
| `/api/people/search`, `/api/studios/search` | `private, max-age=30` | body hash |
| `/api/recommendations`, `/api/music/home` | `private, max-age=120` | body hash |
| `/api/libraries`, `/api/libraries/{id}` | `private, max-age=10` | newest library `updated_at` |
| `/api/media/{id}`, `/api/media`, `/api/media/enriched` | `private, max-age=30` | item or list `updated_at` |
| `/api/me/*` (any per-user) | `private, no-store` | none |
| `/api/jobs/*`, `/api/tasks/*`, `/api/stats`, `/api/activity` | `no-store` | none |
| All POST/PUT/DELETE | none | none |
| `/api/health/*` | `no-store` | none |

**Implementation pattern:**

Add to `huma.go`:
```go
// cacheable wraps an op with Cache-Control + body-hash ETag.
func cacheable(o huma.Operation, maxAge int) huma.Operation { ... }

// noStore wraps an op with Cache-Control: no-store.
func noStore(o huma.Operation) huma.Operation { ... }
```

ETag implementation: response middleware that buffers the body, computes SHA-256, sets `ETag` header, responds `304` if the request's `If-None-Match` matches. Huma supports this via the response-transformer hook (`api.UseMiddleware` with a write-time tap).

**Files to edit (in order):**

1. `huma.go` — add the `cacheable()` / `noStore()` op builders + ETag middleware
2. `library_huma.go` — most CRUD-y, good warmup
3. `media_huma.go` — biggest file, most `[any]` usages
4. `music_huma.go` — many list-style handlers
5. `me_huma.go` — mostly `no-store`, easy
6. `jobs_huma.go` — all `no-store`
7. `metadata_editor_huma.go` — mostly POSTs
8. `opensubtitles_huma.go` — small
9. `admin_huma.go` — small
10. `stream_huma.go` — mixed
11. `system_huma.go` — `no-store` on health, default on watcher

**Convention reminder:** the FE doesn't change. `$fetch` in Nuxt handles 304s transparently via the browser cache. The only behavior change is bandwidth.

### B3: Input validation tightening (task #18)

Mechanical sweep across all `*_huma.go`. Add jsonschema validation tags where missing:

- `entity_type` everywhere → `enum:"media_item,episode,season,track,artist,album"`
- `media_type` everywhere → `enum:"movie,tv,music,book,comic,podcast,radio"`
- `subtitle_mode` (`/api/me/playback/{media_id}` body) → `enum:"off,forced,full"`
- `scope` in `/api/me/state` body → `enum:"movies,series,seasons,episodes"`
- Path `{name}` params (genres, keywords) → `pattern:"^[a-z0-9-]+$"`, `maxLength:"128"`
- Slug fields (`{artist_slug}`, `{album_slug}`) → `pattern:"^[a-z0-9-]+$"`, `maxLength:"200"`
- IDs that should be positive → `minimum:"1"` (most are missing this)
- Long-form text fields with implicit limits → `maxLength`
- `kind` parameters → enum the allowed values
- Body string fields used in queries → `maxLength` to prevent abuse

**Where to grep first:**
- `entity_type string` — favorites toggle, loved entity ops
- `enum:"` — search for existing enum tags to find what's NOT yet enumerated
- `path:"id"` — find every IDPath usage to confirm `minimum:1`

### B4: Examples pass (task #19)

Mechanical sweep. Add `example:"..."` tags. **Do this LAST in Phase B** — it's the final state of the OpenAPI spec before TS client generation, so examples flow through to the generated client docs.

Per `*_huma.go`:

- ID path params → `example:"42"`
- Slug params → `example:"the-godfather"` / `example:"miles-davis"`
- Search queries → `example:"godfather"` / `example:"jazz"`
- Body request examples — at minimum on `/api/auth/login`, `/api/auth/register`, `/api/me/lists` create, `/api/me/playlists` create, `/api/me/settings` PUT
- Enum fields — even though the enum lists allowed values, an example clarifies the intended canonical form

**Where examples matter MOST**: paths the FE uses heavily + paths a third-party integrator would hit first (auth, search, basic media listings).

---

## Phase C — TypeScript client generation

The big payoff for the FE. ~2-3 hours. Backend touchpoint is one new CLI command; the rest is wiring on the FE side.

### C1: `heya openapi-spec` CLI (task #20)

New cobra command. Required because CI shouldn't have to boot a server to dump the spec.

**Implementation sketch:**

`cmd/heya/cmd/openapi.go`:
```go
// Instantiate the same Huma API as serve.New() but bound to a throwaway
// mux. Don't start an HTTP listener. Call api.OpenAPI().MarshalJSON()
// (or YAML) and write to stdout / -o.
```

Tricky bit: `newHumaAPI` currently takes a `*service.App`. The spec doesn't need a live App (no DB connection), but the operation registrations call `app.X()` inside their handler closures — closures aren't invoked at registration time, so a nil App is fine as long as `app.X()` isn't called during spec generation.

But — `newHumaAPI` and the `register*Routes` calls *do* sometimes call app methods during registration (e.g., to determine which routes to mount). Need to audit. If anything's eager, refactor it to lazy.

Safer alternative: a minimal `New(ctx, cfg)` mode that wires only the pieces needed for routing without touching the DB. Could add a `service.NewSpecOnly(cfg)` helper that returns an App with everything nil.

**Flags:**
- `-o, --output` — file path (default: stdout)
- `--format yaml|json` — default json (since the generator expects json)

### C2: gen-api-client wiring (task #21)

**FE deps:**

```jsonc
// web/package.json devDependencies
"openapi-typescript": "^7.x",
"openapi-fetch": "^0.x"
```

**Generated artifacts:**

- `web/shared/api.openapi.json` — full spec, regenerated by Make target
- `web/shared/types/api.gen.ts` — TS types from the spec

**Both committed.** Reasoning: PR diffs surface API surface changes loudly. Drift detection in CI catches forgotten regenerations.

**Make target:**

```makefile
.PHONY: gen-api-client
gen-api-client:
	go run ./cmd/heya openapi-spec -o web/shared/api.openapi.json
	cd web && bunx openapi-typescript shared/api.openapi.json -o shared/types/api.gen.ts
```

**Lefthook hook:** if any `*_huma.go` changes, run `make gen-api-client` and fail if the regen produces a diff. Catches "I changed the API but didn't regenerate."

### C3: `useApiClient` composable (task #22)

New file `web/app/composables/useApiClient.ts`:

```ts
import createClient from 'openapi-fetch'
import type { paths } from '~/shared/types/api.gen'
import { useAuth } from '#imports'

let _client: ReturnType<typeof createClient<paths>> | null = null

export function useApiClient() {
  if (!_client) {
    _client = createClient<paths>({
      baseUrl: '',  // same-origin
    })
    _client.use({
      onRequest({ request }) {
        const { token } = useAuth()
        if (token.value) {
          request.headers.set('Authorization', `Bearer ${token.value}`)
        }
        return request
      },
      onResponse({ response }) {
        if (response.status === 401) {
          useAuth().logout()
        }
      },
    })
  }
  return _client
}
```

**Both `useApiClient()` (typed) and existing `useApi()`/`apiFetch()` (legacy) coexist.** Incremental migration; don't touch existing pages.

### C4: Document + migrate one demo page (task #23)

**CLAUDE.md update**: add a section explaining the typed client + that `api.gen.ts` is generated (don't hand-edit).

**Migrate one trivial page** — `web/app/pages/settings.vue` is the smallest candidate. Convert one or two `apiFetch` calls to `useApiClient` to demonstrate the pattern. Don't do bulk migration here.

**Add to `web/shared/types/index.ts`** header comment: "API response types live in `api.gen.ts` (generated). This file is for project-level types only."

### C5: CI drift check (task #24)

Modify `.github/workflows/ci.yml`, `frontend` job. Before vue-tsc:

```yaml
- name: Regenerate API client
  run: make gen-api-client
- name: Check for drift
  run: git diff --exit-code web/shared/api.openapi.json web/shared/types/api.gen.ts
```

If drift, fail with a message about running `make gen-api-client` locally.

---

## Phase D — Performance + coverage

Independent items. Drip in whenever.

### D1: goccy/go-json switch (task #25)

~5 LOC change in `huma.go`. Need to set Huma's JSON encoder/decoder.

**Investigation needed:** Huma exposes `Format`s on `huma.API`. The default is registered via `huma.DefaultJSONFormat` (or similar). Need to either:

a) Replace it via `cfg.Formats["application/json"] = huma.Format{...}` using goccy's encoder/decoder
b) Wrap goccy as a Huma "marshaler" via the config

Look in `/Users/karbowiak/go/pkg/mod/github.com/danielgtaylor/huma/v2@v2.38.0/format*.go` for the right hook.

**Verify:**
- All tests still pass
- Quick before/after `wrk` or `hey` against `/api/media/enriched?limit=5000` to confirm the win

### D2: Huma operation tests (task #26)

Splittable across multiple sessions. Use `humatest.New(t)` from `github.com/danielgtaylor/huma/v2/humatest`.

**Per domain (suggested order):**

1. `auth_huma_test.go` — login/register/logout/me — happy + 401 + validation
2. `library_huma_test.go` — CRUD round-trip, scan trigger
3. `me_huma_test.go` — favorites toggle, watch progress, lists CRUD
4. `media_huma_test.go` — list, get, search

For each: minimum 3 tests per operation (happy, auth, validation). ~30 min per file.

These don't replace e2e tests but give contract-level confidence — every handler validates its inputs and returns its declared shape.

---

## Order summary

```
Phase A ✅ done
  A1 gzip
  A2 background goroutine lifetime
  A3 multipart upload to Huma
  A4 deeper health endpoints

Phase B — backend grind
  B1+B2 typed bodies + Cache-Control + ETag        ← longest single task
  B3 input validation tightening
  B4 examples pass                                  ← last in B (spec freezes here for C)

Phase C — TS client
  C1 heya openapi-spec CLI
  C2 gen-api-client wiring + lefthook
  C3 useApiClient composable
  C4 document + migrate one page
  C5 CI drift check

Phase D — perf + tests
  D1 goccy/go-json
  D2 humatest coverage (ongoing)
```

## Explicitly deferred

These came up during planning but aren't in scope:

- Logging revamp + request IDs (worth doing, separate project — touches every package)
- CORS hardening (part of a future security pass with rate limits + audit log)
- Rate limiting (LAN-only deployment, not urgent)
- Pagination/streaming for huge lists (only if it becomes a real problem)
- AsyncAPI for WebSocket events (premature)
- Prometheus metrics (premature)
- Audit logging for admin ops (security pass)
- HTTP/2 (already works via tsnet HTTPS listener; LAN HTTP/1.1 is intentional)

## Conventions established during this work

- Huma operation files: `<domain>_huma.go` for registrations, `<domain>_views.go` for shared view types (post-B1)
- Per-op security: bare `op()` is public, `secured(op(...))` requires bearer, `adminSecured(op(...))` requires admin
- Background goroutines that outlive the request use `app.LifetimeContext()`, never `context.Background()`
- Binary responses: `binaryOp()` declares the content type honestly (`application/octet-stream` or specific type)
- Multipart: use `huma.MultipartFormFiles[T]` with `FormFile` field; read string form fields from `RawBody.Form.Value[...]`
- Streaming responses (SSE/HLS/WS): `wrapStream(legacyHandler)` delegates via `humago.Unwrap` so we keep Range/Hijacker/Flusher
- Frontend type drift is caught by CI (post-C5) running `make gen-api-client` and `git diff --exit-code`
