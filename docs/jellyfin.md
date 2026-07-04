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

Then add a server in any Jellyfin app using Heya's address and sign in with
a normal Heya account. Sessions minted by Jellyfin clients are ordinary Heya
sessions: they appear under Settings → Sessions and can be revoked there.

## Architecture

Everything lives in `internal/jellyfin/` (see the package comment in
`jellyfin.go` for the constraints). The high-order bits:

- **Mount**: wraps the SPA catch-all in `internal/server/server.go`. When
  the toggle is off, or a path isn't claimed, requests fall through to the
  SPA untouched. The dev proxy forwards claimed paths to the backend via
  `jellyfin.ClaimsPath` (`cmd/heya/cmd/devproxy.go`).
- **Router**: case-insensitive (ASP.NET legacy — clients are sloppy) with
  the `/emby` prefix alias; literal segments beat param segments regardless
  of registration order. Patterns are byte-identical to the upstream spec.
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
- `bun tools/jellyfin-smoke.ts [base] [user] [pw]` — 48-assertion protocol
  walk (discovery → login → browse → PlaybackInfo → stream bytes →
  playstate → resume → favorites → websocket → logout) against a running
  server. CI-able against a seeded dev DB.
- Real-client: jellyfin-web (served standalone, pointed at Heya) driven via
  Heya Eye. Verified end-to-end 2026-07: login, home rails (Continue
  Watching / Latest), series → season → episode browse, item detail,
  favorites, and actual `<video>` playback with progress reporting.

## Client matrix

| Client | Status | Notes |
| --- | --- | --- |
| jellyfin-web 10.8 | ✅ verified | login, home, browse, detail, playback, playstate |
| @jellyfin-protocol smoke | ✅ 48/48 | tools/jellyfin-smoke.ts |
| Infuse | 🔜 expected OK | direct play + byte ranges (no transcode needed per Firecore) |
| Streamyfin / Findroid | 🔜 untested | 10.10+ API users |
| Finamp | 🔜 untested | universal audio + lyrics implemented |

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
