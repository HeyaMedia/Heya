# Development

Day-to-day workflows for building, running, and debugging Heya.

## First-time setup

```bash
git clone git@github.com:HeyaMedia/Heya.git
cd Heya
cp .env.example .env        # tweak the values you care about
make db-up                  # start Postgres on :5440
make build                  # frontend (bun) + Go binary → ./bin/heya
./bin/heya setup            # guided wizard — migrations, admin user, first library
./bin/heya worker           # terminal 1: background runtime
./bin/heya serve            # terminal 2: https://localhost:8080
```

Configuration lives in `.env` (see `.env.example` for every supported key,
layered as `.env` → `.env.local` → process env). Anything declared in env is
locked in the Settings UI with a tooltip naming the env var —
`HEYA_LIBRARY_<N>_*` even declares libraries declaratively for Docker/k8s.
Add libraries via the UI (Settings → Libraries) or CLI
(`./bin/heya library add …`); Heya scans, matches, and enriches from there.

## Daily dev

```bash
make db-up                 # start Postgres on :5440
make dev                   # mprocs: proxy + API + worker + Nuxt — open :8080
```

Prerequisite: `brew install mprocs`. `make dev` runs `make dev-stop` as a
preflight — it reaps every layer a previous run may have left behind (air,
`go run` wrappers, `tmp/heya*` binaries, and the `:8080`/`:3050`/`:3000`
listeners) — then launches mprocs (config in `mprocs.yaml`) with four procs:
`proxy`, `api`, `worker`, `web`. The `worker` pane is **down by default**
(`autostart: false`) — press `s` on it when a session actually needs
background work (scans, enrichment, transcode). Quitting mprocs (`q` /
Ctrl+C) tears the running procs down cleanly; press `r` on a pane to restart
just that process.

Keeping the worker down is a convenience, **not** a safety boundary for the
prod-mirror DB: the queue lives in the database, so manual jobs the API
inserts (scan/refresh triggers) are executed by *production's* worker, and
passive mode is what suppresses schema/bootstrap writes and enables the
image proxy. Don't treat "worker pane off" as a substitute for
`HEYA_PASSIVE_MODE`.

**Never clean up a dev stack by killing port listeners alone.** The air panes
are a `sh → go run → air → heya` chain and `go run` does not forward SIGTERM —
killing the listener orphans air, which then rebuilds and respawns backends on
every file save (the classic "30 stray `compile` processes eating the machine"
failure). `make dev-stop` is the one true cleanup.

Or split into four terminals for independent control:

```bash
make dev-front             # heya dev-proxy on :8080 (the front door)
make dev-go                # air → heya serve --dev-backend on :3050
make dev-worker            # second air → heya worker
make dev-web               # bun run dev on :3000
```

**Topology.** Four processes:

- `heya dev-proxy` — the stable front door on `:8080`. A stdlib
  `httputil.ReverseProxy` that forwards `/api/*` (HTTP + the `/api/ws`
  WebSocket, which upgrades natively), Jellyfin protocol routes, and
  OpenSubsonic `/rest/*` calls to the backend on `:3050`; Heya pages go to
  Nuxt/Vite on `:3000`.
- `heya serve --dev-backend` on `:3050` — API + WS only, hot-reloaded by air.
- `heya worker` — River, scheduler, watchers, and background services,
  independently hot-reloaded by a second Air process.
- Nuxt/Vite on `:3000` — HMR.

The front door is a **separate Go process on purpose**. Go has no in-process
hot reload, so the backend must restart on every code save (air's job) — but
the browser-facing port must not flap with it. Because the front door stays
put across air rebuilds and Nuxt restarts, the browser's HMR socket and any
in-flight WS connection never see the backend churn. The previous "Go fronts
Nuxt" / "Nuxt fronts Go" setups both had the front-door process restart on
every rebuild, which is what caused the ECONNRESET cascade. The dev-proxy
does exactly one thing — proxy — and holds no business logic. Editing proxy
code → press `r` on the `proxy` pane in mprocs (or `make dev-front`).

**Tailscale and remote access are production-only.** Under `--dev-backend`
neither manager is constructed; the API reports them unavailable and the
Settings UI says so. To exercise them locally, run a real production-mode
server against the local docker DB on a scratch port:

```bash
HEYA_DATABASE_URL="postgres://heya:heya@localhost:5440/heya?sslmode=disable" \
HEYA_PORT=18080 HEYA_DATA_DIR=/tmp/heya-nettest ./bin/heya serve
```

That production-mode listener is HTTPS-only and includes HTTP/3. Its local CA
root is `/tmp/heya-nettest/caddy/pki/authorities/local/root.crt`; Heya's own
CLI loads it automatically.

Don't run `go build -o ./bin/heya ./cmd/heya` by hand during dev — `air` rebuilds on save. `go build ./...` is fine as a compile-check.

## Build for production

```bash
make build                 # builds frontend (bun) → web/dist/ → Go binary
./bin/heya worker           # background runtime
./bin/heya serve            # Caddy + API + embedded SPA
```

The single `./bin/heya` artifact serves both the API and SPA via
`//go:embed dist/*` in `web/embed.go`, but production runs its `serve` and
`worker` roles as separate processes. No separate frontend deploy is needed.

## Hitting the local API

`./bin/heya api <method> <path> [body]` issues an authenticated request to the
running server. First call logs in (default `admin/admin`, override with
`--user`/`--pass` or `HEYA_API_USER`/`HEYA_API_PASS`), caches the bearer token
under the OS user config dir, and reuses it. A 401 automatically clears the
cache, re-logs in, and retries once.

| OS    | Token cache path                                                       |
| ----- | ---------------------------------------------------------------------- |
| macOS | `~/Library/Application Support/heya/cli-token`                         |
| Linux | `$XDG_CONFIG_HOME/heya/cli-token` (or `~/.config/heya/cli-token`)      |

```bash
export HEYA_API_BASE_URL=http://localhost:8080  # make dev only
./bin/heya api get /api/health
./bin/heya api get /api/music/artists -q limit=5
./bin/heya api get /api/media/42                            # path interpolation isn't done — pass the resolved path
./bin/heya api post /api/users '{"username":"bob","email":"b@x","password":"hunter22"}'
cat patch.json | ./bin/heya api patch /api/media/42 -
./bin/heya api get /api/tracks/123/stream --raw > out.flac  # binary endpoints need --raw
```

The explicit HTTP base above is for `make dev`'s plaintext dev-proxy. Against a
normal `heya serve`, the CLI defaults to `https://localhost:8080` and trusts
the persisted Heya/Caddy root in `HEYA_DATA_DIR` in addition to system roots.

Body sources: positional JSON literal, `@file`, or `-` for stdin. Query params
via `-q key=value` (repeatable, URL-encoded). Pretty-prints JSON responses by
default; `--raw` streams bytes verbatim. Non-2xx → status + body to stderr,
exit 1.

**Dev-mode caveat**: in dev, the dev-proxy routes `/api/*`, registered
Jellyfin calls, and `/rest/*` to Go; Heya page routes go to Nuxt. A typo like
`/api/nonexisten` reaches Go, which 404s with JSON — that's the easy case. But
if you mistype `/api` itself (`/ap/foo`), the dev-proxy treats it as a page and
Nuxt returns the SPA HTML shell with HTTP 200. If you see `<!DOCTYPE html>`
instead of JSON, check the path.

## Database

```bash
make db-up                 # postgres only
make db-reset              # drops + recreates db, seeds an admin user
make reset                 # full wipe — includes data/* (images, transcodes)
./bin/heya migrate up      # apply pending application + River migrations
./bin/heya migrate down    # roll back one
./bin/heya migrate status  # show applied/pending
./bin/heya db:wipe         # wipe media tables but keep users
```

While in active dev, ship schema changes as **new numbered migrations** — don't
edit prior migrations in place. When a change needs the table empty,
`make db-reset` and re-add libraries. A consolidation pass happens before the
alpha tag, so the churn is fine.

## Developing against the production database (passive mode)

To build UI/views against real data, point local dev at the production Postgres
and run the backend in **passive mode** so it can't damage prod:

```bash
# .env.local (gitignored, layered over .env)
HEYA_DATABASE_URL=postgres://heya:PASSWORD@knas:5432/heya?sslmode=disable
HEYA_PASSIVE_MODE=true
```

Then `make dev` as usual — open http://localhost:8080.

**Why passive mode is mandatory here, not optional.** River's job queue lives
*inside* the same Postgres. `heya serve` never consumes it, but the normal dev
supervisor also launches `heya worker`; without the passive guard that process
would pull production jobs and scan `/storage/...` paths that are not mounted
on the laptop. Passive mode (`internal/config`, enforced by the worker command
and service bootstrap) disables everything that writes or touches disk:

- auto-migrate — won't alter prod's schema to match your branch
- `HEYA_ADMIN_*` / `HEYA_LIBRARY_*` env bootstrap — won't overwrite prod users/libraries
- the dedicated worker stays alive for mprocs but does not initialize River,
  filesystem watchers, schedules, sonic analysis, or orphan recovery

What still runs: the HTTP/API/WS server and the read-only dashboard emitters, so
the UI is live over real data.

**Caveats worth knowing:**

- **Be on prod's migration version.** Auto-migrate is skipped, so if your branch
  adds columns your sqlc queries reference, those queries fail locally. Build
  views on a branch whose schema already matches prod.
- **The API can still write.** Passive mode only stops *background* work — a UI
  action that POSTs (edit metadata, mark watched) hits the real DB. For a hard
  wall, connect with a read-only Postgres role (note: auth/session writes then
  fail too).
- **Triggering a scan from the dev UI runs on prod.** Local enqueues a job; with
  no local workers it's picked up by knas's real worker — which *does* have the
  files. Harmless, but not local.
- **Images come from prod's data dir.** The public image endpoints serve files
  from the server's `HEYA_DATA_DIR`, which isn't on your laptop. Set
  `HEYA_IMAGE_PROXY_URL` to prod's base URL (e.g. `https://heya.example.ts.net`)
  and, in passive mode, those endpoints reverse-proxy the identical path to prod
  so posters/backdrops/covers render. Leave it empty and images 404 locally.
- **Network.** knas must be reachable (LAN or tailnet) on the Postgres port.

## sqlc codegen

After editing files under `queries/` or `migrations/`:

```bash
sqlc generate              # rewrites internal/database/sqlc/*.sql.go
```

Generated files have a `// Code generated by sqlc` header — never edit them by
hand. The lefthook `sqlc generate` check runs on commit and fails if the staged
tree diverges from a fresh regeneration.

## Tests

```bash
make test                  # full suite (needs postgres up)
make test-unit             # short, no DB
go test ./internal/parser/ # one package
```

## Type-checking the frontend

```bash
cd web && bun run typecheck
```

Run this before declaring frontend work done. The codebase is held at 0 errors
— regressions show up clearly.

## Useful URLs at runtime

| URL                       | Purpose                                                  |
| ------------------------- | -------------------------------------------------------- |
| `/api/health`             | Basic health probe                                       |
| `/api/docs`               | Scalar-rendered OpenAPI 3.1 (auto-generated via Huma v2) |
| `/api/config/sources`     | Per-field provenance map (admin-only)                    |
| `/api/tailscale/status`   | Current tsnet state (only useful when Tailscale is on)   |
| `/`                       | SPA entry (embedded)                                     |
| `ws://…/api/events`       | Real-time event stream (scan progress, jobs, `tailscale.status`) |

## Git hooks (lefthook)

A pre-commit gate is configured in `lefthook.yml`. After cloning:

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

| Check                         | Runs when                              | What it gates                                           |
| ----------------------------- | -------------------------------------- | ------------------------------------------------------- |
| `bun run typecheck`           | `.vue` / `.ts` / `.d.ts` under `web/`  | TypeScript 7 and Vue SFC type errors stay at 0          |
| `gofmt -l` (staged files)     | any `.go` changed                      | Blocks unformatted Go                                   |
| `golangci-lint --new-from-rev=HEAD` | any `.go` changed                | Blocks **new** lint issues (errcheck, gosec, staticcheck, …). Pre-existing baseline isn't enforced. |
| `go build ./cmd/heya`         | any `.go` changed                      | Proves the binary still compiles                        |
| `sqlc generate` + diff        | `queries/`, `migrations/`, `sqlc.yaml` | Catches forgotten `sqlc generate` regeneration          |
| `openapi-drift`               | any `*_huma.go` changed                | Catches forgotten `make gen-api-client` regeneration    |
| `trustedDependencies` guard   | `web/package.json` changed             | Blocks adding `trustedDependencies` (bun lifecycle policy) |

Wall-clock cost on a clean tree: ~5–8 s. If a hook blocks a commit, fix the
issue and retry — don't bypass with `--no-verify`.

Dry-run the full hook against the whole tree without committing:

```bash
lefthook run pre-commit --all-files
```

The linter set lives in `.golangci.yml`. Generated sqlc code under
`internal/database/sqlc/` is excluded from lint.

## CI

`.github/workflows/ci.yml` runs four parallel jobs on every push to `main` and
every PR:

| Job          | What it does                                                                                                              |
| ------------ | ------------------------------------------------------------------------------------------------------------------------- |
| **frontend** | `bun install --frozen-lockfile` (catches stale `bun.lock`) → `bun run typecheck` → `bun audit` (npm CVE scan)             |
| **go-static**| `gofmt -l` → `golangci-lint --new-from-rev=origin/main` (PR-diff lint) → `go build ./...` → `sqlc generate` + drift check |
| **go-test**  | Spins up Postgres 17 service container → applies application + River migrations → `go test -race -count=1 ./...`       |
| **security** | `govulncheck ./...` against the Go vuln DB → `osv-scanner` across the whole repo (covers npm + Go via OSV.dev)            |

CI is the tier that can't be bypassed with `--no-verify`. Configure GitHub
branch protection on `main` to require all four jobs green before merge — that's
the actual safety net.

## Bun lifecycle-script policy

Bun **blocks all dependency lifecycle scripts by default** — `postinstall` /
`preinstall` / `install` scripts from any installed package never run unless
that package is listed in `trustedDependencies` in `package.json`. We keep that
field absent on purpose.

Current dep tree has exactly two packages (`esbuild`, `@parcel/watcher`) that
*declare* install scripts. Both ship prebuilt platform binaries, so blocking
the scripts costs nothing.

Enforcement:

- `web/bunfig.toml` documents the policy and pins lockfile behavior.
- `lefthook` and CI both grep `web/package.json` for `"trustedDependencies"` and
  fail if it appears. If you ever genuinely need to allow a dep's script,
  remove the guard *deliberately* in a reviewed PR — don't add the field
  silently.

## Code style

- `.editorconfig` locks indent / EOL / trailing-whitespace across editors. Go
  uses tabs; everything else uses 2-space indent; SQL uses 4-space.
