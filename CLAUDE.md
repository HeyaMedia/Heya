# Heya — Claude Working Notes

Self-hosted media server for movies, TV, music, and books. Go API + Postgres + embedded Nuxt SPA, shipped as a single binary.

## Toolchain (mandatory)

- **Go**: 1.26+ (`go.mod` pins minimum).
- **Bun**: the *only* JS package manager and runner used in this repo. Never run `npm`, `pnpm`, or `yarn`.
  - Install deps: `bun install` (inside `web/`)
  - Run scripts: `bun run dev`, `bun run build`
  - One-shot tooling: `bunx vue-tsc`, `bunx <tool>` — never `npx`.
- **Docker** + `docker compose` for Postgres (port `5440`, no host conflict with the default 5432).
- **Optional**: `air` for Go hot reload (used by `heya dev`), `goose` if invoking migrations outside the CLI.

The lockfile of record is `web/bun.lock`. There is no `package-lock.json` anywhere in the tree — bun is the only resolver.

## Layout

```
cmd/heya/           # CLI entrypoint (cobra)
internal/
  auth/             # bcrypt + session tokens (PG-backed)
  config/           # Env-only config loader (.env + .env.local), Field[T] provenance
  database/         # pgxpool + sqlc-generated queries
    sqlc/           # GENERATED — do not hand-edit
  eventhub/         # WebSocket event bus (real-time UI updates)
  images/           # poster/backdrop fetcher
  matcher/          # filename → media-item matching
  metadata/         # provider clients (heya.media aggregator + NFO)
  nfo/              # NFO file parser
  parser/           # ~/Private/yarr port — filename parser w/ 2700+ test cases
  saver/            # writes images/assets to data/
  scanner/          # filesystem scan + fsnotify watcher
  scheduler/        # periodic jobs (trickplay, thumbnails, library re-scan)
  server/           # net/http handlers (stdlib router, Go 1.22+ patterns)
  service/          # shared business layer used by CLI + HTTP
  slug/             # URL slug generation
  tailscale/        # tsnet wrapper — embeds a Tailscale node in the binary
  testutil/         # shared test helpers
  transcoder/       # ffmpeg HLS pipeline
  trickplay/        # scrubbing thumbnails (BIF/sprite generation)
  ui/               # lipgloss-based CLI UI (TUI dashboard, prompts)
  vfs/              # SMB + local filesystem abstraction
  watcher/          # filesystem change watching
  worker/           # River background jobs
migrations/         # goose SQL migrations (numbered 00001_*)
queries/            # sqlc input — SQL queries compiled to typed Go
web/
  app/              # Nuxt 4 SPA source
    components/
      detail/       # Shared hero blocks (MediaSynopsis, MediaCrewSummary,
                    # MediaKeywords, MediaStreamInfo, MediaRatings,
                    # MediaPlaybackPanel, PlaybackPrefs, EpisodeCard, …)
      metadata/     # MetadataEditor + dialog/manager
      ui/           # Generic primitives: Dropdown, Lightbox, Poster, Chip…
      player/       # VideoPlayer (HLS + akarisub ASS rendering)
    pages/          # File-routed; movies/, tv/, music/, books/, settings/
    composables/    # useApi, useMedia, useUserState, useClientCaps, …
    layouts/        # default + auth layouts
    plugins/
    utils/
  public/logos/     # Rating-provider brand SVGs (CC0 — simple-icons + Wikimedia)
  shared/types/     # TypeScript types mirroring the Go API responses
  embed.go          # //go:embed dist/* — bundles the SPA into the Go binary
  dist/             # Built SPA assets (committed empty, populated by build)
testdata/           # Real-world filename fixtures for parser tests
data/               # Runtime: posters, backdrops, postgres volume
.env.example        # Catalogue of every supported env var (defaults + comments)
docker-compose.yml  # Postgres 17 on :5440
sqlc.yaml           # Codegen config
.air.toml           # Hot-reload config for `heya dev`
```

Design principle: **CLI-first**. Every feature goes through `internal/service/`, so the CLI and HTTP server share the same code paths. The Go binary is self-contained — it embeds the built Nuxt assets via `web/embed.go`.

## Common workflows

### Daily dev

```bash
make db-up                 # start Postgres
./bin/heya dev             # spawns Go API (air hot-reload) + Nuxt dev server
```

Or the manual split:

```bash
cd web && bun run dev      # Nuxt on :3000
./bin/heya serve           # Go API on :8080
```

### Build for production

```bash
make build                 # builds frontend (bun), copies to web/dist/, builds Go binary
./bin/heya serve
```

The single `./bin/heya` binary serves both API and SPA — no separate frontend deployment.

### Database

```bash
make db-up                 # postgres only
make db-reset              # drops + recreates db, seeds an admin user
make reset                 # full wipe — includes data/* (images, transcodes)
./bin/heya migrate up      # apply pending migrations
./bin/heya migrate down    # roll back one
./bin/heya migrate status  # show applied/pending
./bin/heya db:wipe         # wipe media tables but keep users
```

### sqlc codegen

After editing files under `queries/` or `migrations/`:

```bash
sqlc generate              # rewrites internal/database/sqlc/*.sql.go
```

Generated files have a `// Code generated by sqlc` header — never edit them by hand.

### Tests

```bash
make test                  # full suite (needs postgres up)
make test-unit             # short, no DB
go test ./internal/parser/ # one package
```

### Type-checking the frontend

```bash
cd web && bunx vue-tsc --noEmit
```

Run this before declaring frontend work done. The codebase is held at 0 errors — regressions show up clearly.

### Typed API client (OpenAPI → TypeScript)

The Go server's Huma operations generate an OpenAPI 3.1 spec. We commit that spec **and** the typed TS client derived from it so PRs surface API-surface changes loudly.

```bash
make gen-api-client          # regenerate web/shared/api.openapi.json + api.gen.ts
./bin/heya openapi-spec      # dump just the spec (use -o file to write)
```

- **`web/shared/api.openapi.json`** — full spec, regenerated by the Make target.
- **`web/shared/types/api.gen.ts`** — TypeScript types derived from the spec by `openapi-typescript`. Don't hand-edit; the file regenerates from scratch.
- **`web/app/composables/useApiClient.ts`** — typed `openapi-fetch` client; attaches the bearer token and handles 401 → logout the same way `useApi`/`apiFetch` did.

Prefer `useApiClient()` for new code. Existing `useApi()` / `apiFetch()` call sites still work; migrate incrementally. See `web/app/pages/settings/server.vue` for the canonical example.

The lefthook `openapi-drift` hook regenerates the client whenever a `*_huma.go` file changes and blocks the commit if the regenerated artifacts differ from what's staged — equivalent to the sqlc drift check.

### Git hooks (lefthook)

A pre-commit hook gate is configured in `lefthook.yml`. After cloning:

```bash
brew install lefthook       # one-time
lefthook install            # installs .git/hooks/pre-commit
```

You also need a few Go tools the hooks call out to:

```bash
brew install golangci-lint sqlc
go install golang.org/x/vuln/cmd/govulncheck@latest
# Make sure $(go env GOPATH)/bin is on your PATH.
```

The hook runs (in parallel) on every `git commit`:

| Check | Runs when | What it gates |
| --- | --- | --- |
| `bunx vue-tsc --noEmit` | any `.vue` / `.ts` / `.d.ts` changed under `web/` | Frontend type errors stay at 0 |
| `gofmt -l` (staged files) | any `.go` changed | Blocks unformatted Go |
| `golangci-lint --new-from-rev=HEAD` | any `.go` changed | Blocks **new** lint issues (errcheck, gosec, staticcheck, unused, ineffassign, …). Pre-existing baseline isn't enforced yet. |
| `go build ./cmd/heya` | any `.go` changed | Proves the binary still compiles |
| `sqlc generate` + diff | `queries/`, `migrations/`, or `sqlc.yaml` changed | Catches forgotten `sqlc generate` regeneration |

Wall-clock cost on a clean tree: ~5–8s. If a hook blocks a commit, fix the issue and retry — don't bypass with `--no-verify`.

To dry-run the full hook against the whole tree without committing:

```bash
lefthook run pre-commit --all-files
```

The linter set lives in `.golangci.yml`. Generated sqlc code under `internal/database/sqlc/` is excluded from lint.

### CI

`.github/workflows/ci.yml` runs four parallel jobs on every push to `main` and every PR:

| Job | What it does |
| --- | --- |
| **frontend** | `bun install --frozen-lockfile` (catches stale `bun.lock`) → `bunx vue-tsc --noEmit` → `bun audit` (npm CVE scan) |
| **go-static** | `gofmt -l` → `golangci-lint --new-from-rev=origin/main` (PR-diff lint) → `go build ./...` → `sqlc generate` + drift check |
| **go-test** | Spins up Postgres 17 service container → applies migrations via goose → `go test -race -count=1 ./...` |
| **security** | `govulncheck ./...` against the Go vuln DB → `osv-scanner` across the whole repo (covers npm + Go via OSV.dev — catches malware reports and CVEs that haven't propagated yet) |

The CI tier is the one that can't be bypassed with `--no-verify`. Configure GitHub branch protection on `main` to require all four jobs green before merge — that's the actual safety net.

### Code style

- `.editorconfig` locks indent / EOL / trailing-whitespace across editors. Go uses tabs; everything else uses 2-space indent; SQL uses 4-space.

### Bun lifecycle-script policy

Bun **blocks all dependency lifecycle scripts by default** — `postinstall` / `preinstall` / `install` scripts from any installed package never run unless that package is listed in `trustedDependencies` in `package.json`. We keep that field absent on purpose.

Current dep tree has exactly two packages (`esbuild`, `@parcel/watcher`) that *declare* install scripts. Both ship prebuilt platform binaries, so blocking the scripts costs nothing.

Enforcement:

- `web/bunfig.toml` documents the policy and pins lockfile behavior.
- `lefthook` and CI both grep `web/package.json` for `"trustedDependencies"` and fail if it appears. If you ever genuinely need to allow a dep's script, remove the guard *deliberately* in a reviewed PR — don't add the field silently.

## CLI overview

`./bin/heya --help` for the full list. Frequently used:

| Command | Purpose |
| --- | --- |
| `serve` | Start the HTTP server (default port 8080) |
| `dev` | Run Go + Nuxt dev servers concurrently |
| `dashboard` | Full-screen TUI showing server state, queue, watchers |
| `setup` | Guided first-time configuration |
| `config show` | Inspect current configuration with provenance (env/db/default per field) |
| `library` | CRUD on media libraries + trigger scans |
| `media` | Browse and manage matched media items |
| `parse` | Test the filename parser against a path |
| `queue` | Inspect/manage the River job queue |
| `job list \| status` | Background job details |
| `migrate up \| down \| status \| reset` | Goose wrapper |
| `db:wipe` | Drop media tables (keeps users) |
| `user` | User CRUD (create admin: `user create … --admin`) |
| `transcode` | ffmpeg utilities (probe, test pipeline) |
| `studios` | Studio/network logo management |
| `tailscale status \| logout` | Inspect / reset the embedded tsnet node |

Global flags: `--json` (machine output), `--no-color`.

## Match + enrich pipeline

The path from "file appears on disk" to "fully enriched media item" is split into two phases:

**Match (search-only stub)** — `internal/matcher/`. The scanner emits a parsed filename; the `MetadataMatchWorker` calls HeyaMedia's `/api/v1/search` exactly once, scores each hit locally, and on auto-match writes a stub `media_items` row containing only what the search response carries (title, year, snippet → description, image → poster URL, external_ids, `alt_titles`). No `GetDetail` call. Sub-second per file. The item is now visible in the UI as a stub.

- Scoring: `internal/matcher/confidence.go::ScoreConfidence` (Levenshtein on normalized titles + year boost + substring-containment bonus for the "Title: Subtitle" pattern), then `internal/matcher/matcher.go::scoreBestTitle` projects that over `[primary, ...AltTitles]` and takes the max — that's how romaji filenames resolve against English canonical titles via HeyaMedia's `alt_titles[]`.
- Threshold: `MatchOptions.AutoMatchThreshold` (default `0.85`) — `internal/matcher/matcher.go::autoMatchThresholdFor` lowers it to 0.75 when the hit is `enriched` (HeyaMedia has it warm-cached and cross-confirmed).
- Tuning probe: `go test -v -run TestProbeAutoMatch ./internal/matcher/` exercises a 43-case corpus against a running HeyaMedia and reports the score distribution. Skips silently when HeyaMedia is unreachable.

**Enrich (unified queue, priority-banded)** — `internal/worker/enrich_worker.go`. One worker kind, `EnrichMediaItemArgs{ItemID, Source, Force}`, dispatches internally on `media_type`:
- Movies / TV / books: `heya.GetDetail` → `Matcher.StoreEntityMetadata` (type-specific row + TV seasons/episodes) → `StoreRichMetadata` (cast/crew/keywords) → enqueues `DetectLocalAssetsArgs` (image pipeline) + `PersonFetch` + `RatingsFetch` + `SaveNFO`.
- Music: delegates to `Matcher.RefreshMusicArtist` (artist+album+track upsert from the discography payload) + optional `SaveMusicNFO`.
- Each component stamps its `*_enriched_at` column on success (`base / people / extras / images / structure`). The worker short-circuits on `enrichment_status='complete'` unless `Force=true`, so redundant enqueues are cheap. Hard failures write `last_enrich_error` and set status to `failed` — surfaced in the tasks-page items modal.

**Queue config** (`internal/worker/worker.go`):
- All enrich jobs run on the **`metadata`** queue, `MaxWorkers=1`. Single-flight serializes HeyaMedia calls so cold artist enriches (~120s under upstream rate-limit backoff) don't pile up.
- Priority bands (River hardcodes a 1..4 cap — not configurable): **1**=watcher / view (user just dropped a file or opened a detail page), **2**=movies + TV, **3**=music + books.
- `RescueStuckJobsAfter: 10 * time.Minute` — backstop above the slowest legitimate job; lower numbers preempt slow-but-healthy artist enriches.
- HeyaMedia HTTP client timeout: 5 minutes (`internal/metadata/heyamedia/client.go`) — worst-case ceiling per call, callers can cancel sooner via ctx.

**Enqueue API** (`internal/worker/enqueue.go`) — single source of truth, every caller goes through one of these:
- `EnqueueEnrich(ctx, rc, itemID, mediaType, source)` — scheduled, scan, etc.
- `EnqueueEnrichForce(...)` — user clicked "refresh metadata"; bypasses the `complete` short-circuit.
- `EnqueueEnrichBatch(..., batchLibID, batchTotal, batchPos)` — post-scan fan-outs that want progress events.
- `EnqueueEnrichTx(ctx, itemID, mediaType, source)` — for callers already inside a River worker (pulls the client out of ctx).
- View-promotion: `service.GetMediaDetail` calls `EnqueueEnrich(..., EnrichSourceView)` for any item not at `enrichment_status='complete'`, lifting that single item to priority 1 ahead of any background work.

**Scheduled task** — `refresh_stale_items` (`internal/scheduler/refresh_metadata_task.go`) walks every media_item past its library's `MetadataRefreshDays` window and enqueues an enrich for all four media types. Replaces the pre-refactor `refresh_metadata` (non-music) + `refresh_music_artists` (music) split.

**HeyaMedia response shape gotchas** (`internal/metadata/heyamedia/heya.go`):
- `external_ids` arrives as `{"tmdb": 1429}` (numeric), not the old `{"tmdb": "1429"}`. The `flexIDs` map type coerces both forms to canonical strings on decode.
- `alt_titles[]` is the union of every locale variant for the hit. Flows through `metadata.SearchResult.AltTitles` and gets scored alongside the primary `Title`.

## Conventions

- **No backwards-compat shims while in active dev.** Schema changes ship as new numbered migrations; don't edit prior migrations in place. The user runs a consolidation pass before tagging an alpha release, so the churn is fine. When the schema change needs the table empty, `make db-reset` and re-add libraries — that's also fine until alpha.
- **Trickplay + thumbnails are scheduler-driven only.** Never trigger them from the scan pipeline. Trickplay defaults off per-library.
- **The shared service layer is the source of truth.** Don't reach into `internal/database/sqlc` from handlers — go through `service/`.
- **Frontend types track the API.** When a Go response shape changes, update `web/shared/types/index.ts` to match.
- **Slugs are user-facing URLs.** Media items have a stable `slug` column; routes are `/movies/{slug}`, `/tv/{slug}`, etc.
- **Heya Media aggregator** (`heya.media`) is the upstream metadata source; TMDB / TVDB / OMDb / MusicBrainz / OpenLibrary are reached through it, not directly.
- **Tailscale (tsnet) is optional and additive.** Off by default — flip on via `HEYA_TAILSCALE_ENABLED=true` or Settings → Tailscale. When enabled, Heya joins the tailnet as its own node (default hostname `heya`) and serves the same handler on tailnet :80/:443 alongside the LAN listener. HTTPS uses Tailscale-issued certs from the node's MagicDNS name. Funnel is off by default — flip it on in the UI to expose Heya on the public internet. Auth keys come from `HEYA_TAILSCALE_AUTHKEY` (preferred for unattended boot) or the interactive login URL shown in the UI on first start. State lives in `data/tailscale/`. UI toggles persist to `system_settings`; env-set fields show as locked in the UI.

- **Config provenance (env locks UI).** Every operational knob is loaded from env (`.env` → `.env.local` → process env, defaults applied last). Each field carries a `Source ∈ {env, db, default}`. `/api/config/sources` returns the per-field provenance map; the Vue settings panels disable any input whose source is `env` with a tooltip naming the env var. Library identity (name/paths/media_type) can be declared via `HEYA_LIBRARY_<N>_*` for IaC-style bootstrap — per-library tunables (trickplay, NFO, etc.) always stay DB/UI-editable. Admin bootstrap via `HEYA_ADMIN_USERNAME` + `HEYA_ADMIN_PASSWORD` applies only on first boot when no admin exists.

## Useful URLs at runtime

- `/api/health` — basic health probe
- `/api/docs` — Scalar-rendered OpenAPI 3.1 (auto-generated via Huma v2)
- `/api/config/sources` — per-field provenance map (admin-only)
- `/api/tailscale/status` — current tsnet state (only useful when tailscale is enabled)
- `/` — SPA entry (embedded)
- `ws://…/api/events` — real-time event stream (scan progress, job updates, `tailscale.status`)
