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

Stale `package.json` / `package-lock.json` files exist at repo root and inside `web/`. They predate the bun switch and aren't read by anything; safe to ignore (and probably worth deleting on the next cleanup pass). The lockfile of record is `web/bun.lock`.

## Layout

```
cmd/heya/           # CLI entrypoint (cobra)
internal/
  auth/             # bcrypt + session tokens (PG-backed)
  config/           # YAML + env config loader (heya.yaml)
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
heya.yaml           # Runtime config (DSN, host, port, data_dir)
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
| `config show \| set \| init \| path` | Inspect/edit `heya.yaml` |
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

Global flags: `--json` (machine output), `--no-color`.

## Conventions

- **No backwards-compat shims while in active dev.** If a migration or schema needs to change, edit the original migration in-place and wipe the DB (`make db-reset`). Don't add fixup migrations.
- **Trickplay + thumbnails are scheduler-driven only.** Never trigger them from the scan pipeline. Trickplay defaults off per-library.
- **The shared service layer is the source of truth.** Don't reach into `internal/database/sqlc` from handlers — go through `service/`.
- **Frontend types track the API.** When a Go response shape changes, update `web/shared/types/index.ts` to match.
- **Slugs are user-facing URLs.** Media items have a stable `slug` column; routes are `/movies/{slug}`, `/tv/{slug}`, etc.
- **Heya Media aggregator** (`heya.media`) is the upstream metadata source; TMDB / TVDB / OMDb / MusicBrainz / OpenLibrary are reached through it, not directly.

## Useful URLs at runtime

- `/api/health` — basic health probe
- `/api/docs` — Scalar-rendered OpenAPI 3.1 (auto-generated via Huma v2)
- `/` — SPA entry (embedded)
- `ws://…/api/events` — real-time event stream (scan progress, job updates)
