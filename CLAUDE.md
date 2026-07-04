# Heya — Claude working notes

Self-hosted media server for movies, TV, music, and books. Go API + Postgres
+ embedded Nuxt SPA, shipped as a single binary.

For deeper context, see `docs/`:

| Doc                              | When to read                                                  |
| -------------------------------- | ------------------------------------------------------------- |
| [docs/architecture.md](docs/architecture.md) | Repo layout, request lifecycle, design choices    |
| [docs/development.md](docs/development.md)   | Daily dev, build, DB, tests, hitting the API, hooks, CI |
| [docs/pipeline.md](docs/pipeline.md)         | Match + enrich pipeline, queue config, HeyaMedia client |
| [docs/ui.md](docs/ui.md)                     | `App*` primitives, `surface.css`, FE conventions  |
| [docs/api-client.md](docs/api-client.md)     | Typed OpenAPI → TS client (`useHeya` / `$heya`)   |
| [docs/music-api.md](docs/music-api.md)       | `/api/music/*` route map and shape conventions    |
| [docs/eye.md](docs/eye.md)                   | Heya Eye — headless-Chrome UI debugger            |
| [docs/jellyfin.md](docs/jellyfin.md)         | Jellyfin-compatible API — arch, coverage, testing |
| [docs/cli.md](docs/cli.md)                   | Full CLI reference                                |
| [docs/tailscale.md](docs/tailscale.md)       | tsnet integration                                 |
| [docs/deployment.md](docs/deployment.md)     | Container images (base + CUDA/OpenVINO GPU variants), transcode + ONNX GPU |

## Toolchain (mandatory)

- **Go**: 1.26+ (`go.mod` pins minimum).
- **Bun**: the *only* JS package manager and runner. **Never** run `npm`,
  `pnpm`, `yarn`, `npx`. One-shot tooling is `bunx`. The lockfile of record
  is `web/bun.lock` — no `package-lock.json` exists.
- **mprocs**: dev supervisor + front door (`brew install mprocs`). Reads
  `mprocs.yaml` at repo root.
- **Docker** + `docker compose` for Postgres (port `5440`).
- Optional: `air` (used by `make dev`), `goose` for out-of-CLI migrations,
  `lefthook` + `golangci-lint` + `sqlc` + `govulncheck` for hooks/CI.

**Don't run `go build -o ./bin/heya …` during dev** — `air` handles rebuilds.
`go build ./...` is fine as a compile-check.

**Dev topology**: three processes supervised by **mprocs** (`mprocs.yaml`):

1. `heya dev-proxy` — the stable front door on `:8080`. A stdlib
   `httputil.ReverseProxy` that forwards `/api/*` (including the `/api/ws`
   WebSocket, which upgrades natively) to the backend on `:3050`, and
   everything else to Nuxt/Vite on `:3000`.
2. `heya serve --dev-backend` on `:3050` (API + WS only), hot-reloaded by `air`.
3. Nuxt/Vite dev server on `:3000` (HMR).

The front door is its own Go process *on purpose*: the backend restarts on
every code save (air's job), but tsnet must own a stable listener that doesn't
flap — so Tailscale lives in `dev-proxy`, which never restarts on a code/Vue
edit. The browser's HMR socket and any in-flight WS connection survive air
rebuilds. `make dev` reclaims ports `:8080`/`:3050`/`:3000` from any orphaned
run, then launches mprocs; quitting mprocs (`q` / Ctrl+C) tears all three down,
and pressing `r` on a pane restarts just that process. `make dev-front` /
`make dev-go` / `make dev-web` split them into separate terminals. You open
**http://localhost:8080** as before. Prod collapses back to a single binary
(`heya serve`, no flag, on `:8080` serves the embedded SPA + API + WS) — no
front door process.

## Design principle

**CLI-first.** Every feature goes through `internal/service/`, so the CLI and
HTTP server share the same code paths. The Go binary is self-contained — it
embeds the built Nuxt assets via `web/embed.go`. There are no "API-only"
features: if you can't do it from the CLI, it doesn't exist.

## Hard conventions

These are guardrails — bug-avoidance rules earned the hard way. Don't break
them without a discussion.

- **Work on `main`. No feature branches, no worktrees.** This is a solo,
  fast-moving repo where branch-per-feature and worktrees only add merge
  friction. Commit straight to `main` and push. Releases are cut by tagging
  `vX.Y.Z` (next is bumped from the latest `git tag`), which triggers the
  container build + deploy via `.github/workflows/container.yml`.
- **No backwards-compat shims while in active dev.** Schema changes ship as
  new numbered migrations; don't edit prior migrations in place. The user
  runs a consolidation pass before tagging an alpha release, so the churn is
  fine. When a change needs the table empty, `make db-reset` and re-add
  libraries — that's also fine until alpha.
- **The shared service layer is the source of truth.** Don't reach into
  `internal/database/sqlc` from handlers — go through `service/`.
- **Trickplay + thumbnails are kickoff-driven only.** Never trigger them
  inline from the scan pipeline. Trickplay defaults off per-library.
- **Image URLs are unconditional.** Always emit `/api/media/{id}/image/{type}`
  (or the `usePosterUrl` / `useBackdropUrl` / `useAlbumCoverUrl` composables)
  on the FE — don't gate on `poster_path` / `backdrop_path` / `cover_path`
  being non-empty. The endpoint walks `media_assets` first; an empty column
  doesn't mean no image. See [docs/ui.md](docs/ui.md#image-urls-are-unconditional)
  for the past bug.
- **Slugs are user-facing URLs.** Media items have a stable `slug` column;
  routes are `/movies/{slug}`, `/tv/{slug}`, etc. Albums are addressed by
  `(artist_slug, album_slug)`. Tracks have no slug → stay ID-addressed.
- **Frontend types track the API.** When a Go response shape changes,
  `make gen-api-client` regenerates `web/shared/api.openapi.json` and the
  derived TS types. The lefthook `openapi-drift` check blocks the commit if
  you forgot.
- **Heya Media aggregator** (`heya.media`) is the upstream metadata source;
  TMDB / TVDB / OMDb / MusicBrainz / OpenLibrary are reached through it, not
  directly. The only client is `internal/metadata/heyamedia/heya.go`.
- **Bun lifecycle scripts stay blocked.** Don't add `trustedDependencies` to
  `web/package.json` without a deliberate reviewed PR — lefthook + CI grep
  for the key and fail if it appears.
- **`/api/music/*` and `/api/me/*` are the music namespaces.** Don't
  reintroduce top-level `/api/tracks` or `/api/albums`.
- **Config provenance: env locks UI.** Every operational knob is loaded from
  env (`.env` → `.env.local` → process env, defaults applied last). Each
  field carries `Source ∈ {env, db, default}`. The Settings UI greys out any
  input whose source is `env`. Library identity
  (`HEYA_LIBRARY_<N>_*`) can be IaC-bootstrapped, but per-library tunables
  (trickplay, NFO, etc.) always stay DB/UI-editable.

## UI gotchas (must know before touching `web/`)

Full notes in [docs/ui.md](docs/ui.md). The four that bite repeatedly:

1. **Never call `useNuxtApp()` inside `computed()` or async bodies** — it
   silently hangs requests. Hoist `const { $heya } = useNuxtApp()` to
   script-setup top level.
2. **Scoped CSS doesn't reach portaled / child-rendered elements.** Rules
   that need to land on an `AppMenu` trigger or any portaled content go in
   an unscoped `<style>` block.
3. **Reka popovers ignore JS-dispatched events.** Clicks must be trusted
   (CDP `Input.dispatchMouseEvent`). `contextmenu` and `pointerenter` are
   exceptions. Use the Heya Eye `click` command to drive popovers in tests.
4. **An ancestor's `backdrop-filter` poisons a descendant's
   `backdrop-filter`** — the child renders ~30% opaque regardless of
   background opacity. Either drop the ancestor's filter or portal the child
   out of that subtree.

And one positive rule:

- **Reach for the shared `App*` primitive instead of hand-rolling.** Each
  wraps a reka-ui primitive and applies the surface chrome. Full table in
  [docs/ui.md](docs/ui.md#shared-app-primitives).

## Verification before claiming done

Type-check and compile-check are cheap and catch a lot. The visual layer needs
actual eyes.

- **Frontend**: `cd web && bunx vue-tsc --noEmit` before declaring done. The
  codebase is held at 0 errors; regressions show up clearly.
- **Go**: `go build ./...` after non-trivial changes (lefthook will catch it on
  commit, but find out earlier). Run the targeted test package if you touched
  one (`go test ./internal/parser/`).
- **Visual UI changes**: drive headless Chrome via `tools/eye/eye.ts` — see
  [docs/eye.md](docs/eye.md). Type-check passing doesn't prove a popover opens,
  a glassy panel composites, or contrast survives the page background. **Take
  a screenshot and *look at it***; don't trust tool output that says "found"
  or "200 OK" as evidence the thing rendered correctly.

## Tailscale (optional, additive)

Off by default. Flip on via `HEYA_TAILSCALE_ENABLED=true` or Settings →
Tailscale. When enabled, Heya joins the tailnet as its own node (default
hostname `heya`) and serves the same handler on tailnet `:80/:443` alongside
the LAN listener. HTTPS uses Tailscale-issued certs from MagicDNS. Funnel is
off by default. State lives in `data/tailscale/`. Full integration in
[docs/tailscale.md](docs/tailscale.md).

In dev the tsnet node lives in the stable `heya dev-proxy` process (not the
air-restarted backend); the backend drives it over a localhost control socket.
The DB-backed Settings toggle works in dev — give the dev node a distinct
identity (`HEYA_TAILSCALE_HOSTNAME=heya-dev`,
`HEYA_TAILSCALE_STATE_DIR=./data/tailscale-dev` in `.env.local`). See
[docs/tailscale.md](docs/tailscale.md#development).

## Helpful CLI subset

`./bin/heya --help` for the full tree. The ones used daily:

| Command                                    | Purpose                                              |
| ------------------------------------------ | ---------------------------------------------------- |
| `make dev`                                 | mprocs: dev-proxy :8080 + backend (air) :3050 + Nuxt :3000 — open :8080 |
| `heya serve`                               | Start the HTTP server (default `:8080`)              |
| `heya dashboard`                           | TUI: server state, queue, watchers                   |
| `heya api <method> <path> [body]`          | Auth'd HTTP client w/ token cache — see [docs/development.md](docs/development.md#hitting-the-local-api) |
| `heya library scan <id>`                   | Trigger a library scan                               |
| `heya media refresh <id\|slug>`            | Re-fetch metadata for a media item                   |
| `heya queue status` / `heya job list`      | Inspect background work                              |
| `heya analyze status` / `heya analyze reset` | Sonic-analysis pipeline                            |
| `heya migrate up` / `db:wipe`              | DB migration / wipe                                  |
| `heya config show`                         | Inspect config with per-field provenance             |

Full reference: [docs/cli.md](docs/cli.md).
