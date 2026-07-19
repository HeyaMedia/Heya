# Jellyfin-compatible API

Heya can answer the Jellyfin client protocol, so stock Jellyfin apps —
Infuse, Streamyfin, Finamp, Findroid, jellyfin-web — can use Heya as their
server. The surface targets **Jellyfin 10.11.x** semantics (the last line
with the dynamic-HLS API; every shipping client baselines against it) and
advertises itself as `Jellyfin Server 10.11.11`.

## Enabling

Off by default. Two ways in, following the standard env-locks-UI provenance:

- Settings → Jellyfin API (admin) — live toggle, no restart.
- `HEYA_JELLYFIN_API_ENABLED=true` — locks the UI toggle.

Then add Heya's origin as a server in any Jellyfin app (for example
`http://localhost:8080`) and sign in with a normal Heya account. Sessions
minted by Jellyfin clients are ordinary Heya sessions: they appear under
Settings → Sessions and can be revoked there.

## Architecture

Everything lives in `internal/jellyfin/` (see the package comment in
`jellyfin.go` for the constraints). The high-order bits:

- **Root dispatch**: clients use the same origin as the Heya web app.
  `internal/server/server.go` routes the complete registered Jellyfin surface
  through the package's case-insensitive router. Canonical PascalCase misses
  receive a protocol JSON 404 instead of SPA HTML. When the toggle is off,
  protocol requests return 404 and the normal web app remains untouched.
- **Router**: case-insensitive (ASP.NET legacy — clients are sloppy) with
  the standard `/emby` prefix alias; literal segments beat param segments
  regardless of registration order. Patterns are byte-identical to the
  upstream spec. Jellyfin's `/Movies/Recommendations` is the only exact Heya
  page collision: canonical casing or Jellyfin request identity selects the
  API, while a plain lowercase `/movies/recommendations` navigation stays in
  Heya.
- **Ids**: Jellyfin GUIDs are a reversible encoding of (entity kind, int64
  row id) — `ids.go`. No mapping table; foreign GUIDs decode to 404s.
- **Auth**: `POST /Users/AuthenticateByName` mints a real Heya session.
  All four credential forms work: `Authorization: MediaBrowser Token=…`,
  `X-Emby-Authorization`, `X-Emby-Token`/`X-MediaBrowser-Token`, `?api_key=`.
- **Delivery trick**: the client's api_key IS a Heya session token, and the
  native stream endpoints accept `?token=` — so `TranscodingUrl` and
  subtitle `DeliveryUrl`s point straight at `/api/stream/{file}/...`,
  reusing the whole transcode-session stack with zero duplication. Only
  URLs clients construct themselves (`/Videos/{id}/stream`,
  `/Audio/{id}/universal`) have real handlers in the package.
- **Playstate**: `/Sessions/Playing*` maps ticks onto the same
  watch-progress and scrobble paths the web player uses, and mirrors into
  the live session store — Jellyfin playback shows up in Heya's activity
  panel and WS dashboard.
- **No huma, on purpose**: the generated TS client must not see this
  surface; the contract is the vendored upstream OpenAPI spec.

## Coverage

The vendored 10.11.11 spec (`internal/jellyfin/spec/`) is fully triaged in
`manifest.go`; tests fail on untriaged operations, on implemented claims
without a registered route, and on route patterns that don't exist in the
spec. Inspect with:

```bash
./bin/heya jellyfin coverage          # implemented + stubbed
./bin/heya jellyfin coverage --all    # everything, incl. planned/out-of-scope
```

Statuses: `implemented` (real behavior), `stubbed` (the same "feature
absent/disabled" answers a stock Jellyfin gives — LiveTV off, no plugins…),
`planned` (future work), `out_of_scope` (no Heya equivalent).

## Testing

- `go test ./internal/jellyfin/` — unit + coverage-manifest tests.
- `bun tools/jellyfin-conformance.ts` — a black-box port of the official
  server's integration suite (`jellyfin/jellyfin`,
  `tests/Jellyfin.Server.Integration.Tests` @ release-10.11.z): same test
  names, same requests, same expected statuses. Runs against any live
  server, so the same suite validates both sides:

  ```bash
  bun tools/jellyfin-conformance.ts                       # Heya (:8080, admin/admin)
  JF_URL=https://jf.example JF_USER=u JF_PASS=p \
    bun tools/jellyfin-conformance.ts                     # real Jellyfin oracle
  ```

  Current score: **71 pass / 0 fail on both Heya and a real Jellyfin**
  (23 skips: wizard tests need a pristine server, test-plugin and /Encoder
  echo tests need upstream's compiled-in test assembly, and mutating tests
  — library/user create-delete — only run with `JF_ALLOW_MUTATIONS=1`,
  never against production). Heya answers mutations with validated 403s
  ("not allowed"), never a lying 204, so the mutation-gated tests are
  expected to fail against Heya by design.
- `bun tools/jellyfin-smoke.ts [base] [user] [pw]` — 48-assertion protocol
  walk (discovery → login → browse → PlaybackInfo → stream bytes →
  playstate → resume → favorites → websocket → logout) against a running
  server. CI-able against a seeded dev DB.
- Real-client: jellyfin-web (served standalone, pointed at Heya) driven via
  Heya Eye. Verified end-to-end 2026-07: login, home rails (Continue
  Watching / Latest), series → season → episode browse, item detail,
  favorites, and actual `<video>` playback with progress reporting.

Dev-topology note: in `make dev`, the front door is a pure shim: `/api/*`,
Jellyfin requests, and `/rest/*` go to the Go backend; Heya pages go to Nuxt.
Run Jellyfin protocol tests against `http://127.0.0.1:8080` (or the backend
directly at `http://127.0.0.1:3050`).

## Client matrix

| Client | Status | Notes |
| --- | --- | --- |
| jellyfin-web 10.8 | ✅ verified | login, home, browse, detail, playback, playstate |
| @jellyfin-protocol smoke | ✅ 48/48 | tools/jellyfin-smoke.ts |
| upstream integration suite (ported) | ✅ 71/71 | tools/jellyfin-conformance.ts — same score as real Jellyfin |
| Infuse | ✅ verified | add server, browse, all image types, HDR direct play; episode lists need `fields=MediaSources` |
| Streamyfin / Findroid | 🔜 untested | 10.10+ API users |
| Finamp | 🔜 untested | universal audio + lyrics implemented |
| Jellyfin Media Player | ❌ won't support | JMP ships no UI of its own — it loads `{server}/web/index.html` (the server-hosted jellyfin-web) in an old embedded Chromium. Supporting it means Heya hosting a second, foreign web app; deliberately declined. |

## Known gaps

- No per-user library ACLs (Heya has none) — every user gets an all-access
  policy; `is_admin` gates the admin endpoints.
- Video play counts are 0/1 (derived from the completed flag).
- `/Items` param grid: genre/year/person/studio filters are accepted but
  ignored (logged at debug — point a client at the server and the log is
  the worklist). Same for playlist CRUD and QuickConnect (`planned` in the
  manifest).
- Episode images resolve via the series' `media_assets` labels
  (`s{n}e{m}` stills, `season-{n}` posters) — series without enriched
  assets fall back to series art.

## Differential testing against real Jellyfin

`tools/jellyfin-diff.ts` compares Heya's responses *structurally* (key
presence + JSON types — what strict decoders actually break on) against a
real Jellyfin over the client call sequence: auth → views → movie grid →
detail → PlaybackInfo → series → seasons → episodes → NextUp → resume.
Both servers are env-configured (never hardcode credentials):

```bash
JF_REAL_URL=https://jf.example JF_REAL_USER=u JF_REAL_PASS=p \
JF_HEYA_URL=http://127.0.0.1:8080 bun tools/jellyfin-diff.ts
```

This harness caught the Infuse breakers: `null` where upstream emits empty
arrays, ETag 304s upstream never sends, MediaSources/MediaStreams missing
from item detail, and `fields=MediaSources` being ignored on episode lists
(which broke Infuse's entire show page).
