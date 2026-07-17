# Architecture

Heya ships one Go binary with two long-running roles. `heya serve` owns Caddy,
the HTTP API, embedded Nuxt SPA, WebSockets, Tailscale, UPnP, and casting.
`heya worker` independently owns River execution, the scheduler, filesystem
watchers, queue recovery, and background models. Postgres remains the only
datastore and coordinates both processes — no Redis or external queue daemon.

```
                    one ./bin/heya artifact
         ┌──────────────────┴──────────────────┐
         │                                     │
┌────────▼──────────────┐             ┌────────▼──────────────┐
│ heya serve            │             │ heya worker           │
│ Caddy TLS + H1/H2/H3  │             │ River queue execution │
│ API + embedded SPA    │             │ scheduler + rescue    │
│ WS + network managers │             │ watchers + models     │
└────────┬──────────────┘             └────────┬──────────────┘
         │        internal/service/            │
         └──────────────────┬──────────────────┘
                            ▼
                  ┌───────────────────┐
                  │   Postgres 17     │
                  │ data + River queue│
                  │ events + heartbeat│
                  └───────────────────┘

External:
  • HeyaMetadata V2            — canonical metadata (TMDB, TVDB, AniDB, …)
  • Community segment APIs     — TheIntroDB, SkipMeDB, AniSkip
  • ffmpeg / ffprobe           — analysis + HLS transcoding
  • SMB shares (optional)      — library sources
```

## Repo layout

```
cmd/heya/           # CLI entrypoint (cobra)
clients/            # Generated HeyaMetadata V2 OpenAPI client (oapi-codegen)
internal/
  auth/             # bcrypt + session tokens (PG-backed)
  config/           # Env-only config loader (.env + .env.local), Field[T] provenance
  database/         # pgxpool + sqlc-generated queries
    sqlc/           # GENERATED — do not hand-edit
  eventhub/         # WebSocket event bus (real-time UI updates)
  images/           # poster/backdrop fetcher
  ingress/          # embedded Caddy topology, custom handler/listeners, metrics
  matcher/          # filename → media-item matching
  metadata/         # HeyaMetadata V2 adapter + local NFO evidence
  nfo/              # NFO file parser
  parser/           # ~/Private/yarr port — filename parser w/ 2700+ test cases
  podcastindex/     # Podcast Index API client + RSS parser
  radiobrowser/     # radio-browser.info client + ICY metadata
  saver/            # writes images/assets to data/
  scanner/          # filesystem scan + fsnotify watcher
  scheduler/        # 60s trigger loop that inserts kickoff River jobs when scheduled_tasks rows come due
  server/           # net/http handlers (Huma v2 + stdlib router)
  service/          # shared business layer used by CLI + HTTP
  slug/             # URL slug generation
  sonicanalysis/    # ML/DSP music analyzer (key, BPM, mood, CLAP embeddings)
  tailscale/        # tsnet wrapper — embeds a Tailscale node in the binary
  testutil/         # shared test helpers
  transcoder/       # ffmpeg HLS pipeline + decision matrix
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
      ui/           # Generic App* primitives — see docs/ui.md
      player/       # VideoPlayer (HLS + akarisub ASS rendering)
    pages/          # File-routed; movies/, tv/, music/, books/, settings/
    composables/    # useApi, useMedia, useUserState, useClientCaps, …
    layouts/        # default + auth layouts
    plugins/
    utils/
  public/logos/     # Rating-provider brand SVGs (CC0 — simple-icons + Wikimedia)
  shared/types/     # TypeScript types mirroring the Go API responses
  shared/api.openapi.json # Generated spec — see docs/api-client.md
  embed.go          # //go:embed dist/* — bundles the SPA into the Go binary
  dist/             # Built SPA assets (committed empty, populated by build)
testdata/           # Real-world filename fixtures for parser tests
tools/eye/          # Headless-Chrome UI debugger — see docs/eye.md
data/               # Runtime: posters, backdrops, postgres volume
docs/               # This directory
.env.example        # Catalogue of every supported env var (defaults + comments)
docker-compose.yml  # Postgres 17 on :5440
sqlc.yaml           # Codegen config
.air.toml           # Hot-reload config for `heya serve --dev-backend` on :3050
.air.worker.toml    # Independent hot-reload config for `heya worker`
mprocs.yaml         # Dev supervisor — proxy + API + worker + Nuxt
lefthook.yml        # Pre-commit hooks — see docs/development.md
```

## Design choices

### CLI-first

Every feature is built once in `internal/service/` and exposed through both a
CLI command (`./bin/heya …`) and an HTTP endpoint. The CLI can drive the
entire backend without the frontend, which keeps the service layer honest and
testable. There are no "API-only" features — if you can't do it from the CLI,
it doesn't exist.

### Embedded SPA, no SSR

The Nuxt frontend is built with `nuxi generate` (SPA mode, `ssr: false`),
copied into `web/dist/`, and embedded into the Go binary via
`//go:embed all:dist`. Deploying Heya uses one artifact in two roles — no Node
process or external reverse proxy to run. The AIO image supervises both roles
inside one container.

### Postgres for everything

User data, media metadata, watch state, and the [River](https://riverqueue.com)
background job queue all live in the same database. Cross-feature transactions
(e.g. "matched a file → enqueued metadata fetch → recorded asset URL") are one
ACID transaction. No queue / data split to keep in sync.

### Metadata is upstream

Heya never talks to canonical metadata providers directly. All metadata is
fetched through HeyaMetadata V2, whose committed OpenAPI contract generates
`clients/heyametadata` and whose adapter lives in
`internal/metadata/heyametadata`. Heya persists canonical UUID bindings and
consumes the gap-free V2 change cursor transactionally.

Benefits: rate-limit budgets aren't shared across Heya instances, one cache
serves many users, schema changes upstream don't propagate to Heya's matcher,
and the metadata server can run in any deployment topology. Community skip
segments are the deliberate exception: Heya owns TheIntroDB, SkipMeDB, and
AniSkip clients because segments are playback-server behavior.

### Transcoding decision matrix

Every play request hits a decision function that compares source streams
(container, video codec, audio codec, channel layout, HDR, bit depth, …)
against the requesting client's reported capabilities. The output is one of
`direct_play` / `remux` / `transcode`, with a list of *reasons* surfaced to
the UI so you can see *why* something is being transcoded. The matrix is
purely declarative — see `internal/transcoder/decision.go` and the JSON
fixtures under `internal/transcoder/testdata/`.

## Request lifecycle (movie playback example)

1. Browser opens `/movies/dune-2024` → SPA fetches `/api/media/dune-2024`.
2. `internal/service/media.go` resolves the slug, joins `movies`, `media_items`,
   `cast`, `crew`, `assets`, `ratings`, `external_ratings`, `keywords`, etc.,
   returns the aggregate JSON.
3. UI shows the page; user clicks Play.
4. SPA calls `/api/stream/{fileId}/info?caps=…` — `internal/service/streaming.go`
   ffprobes the file and runs the decision matrix against the client caps.
5. Response carries the playback decision + a stream URL.
6. For `direct_play`, the URL points at a range-supporting file handler. For
   `remux` / `transcode`, ffmpeg is invoked on demand and the response streams
   HLS (`internal/transcoder/transcode.go`).
7. The player records progress to `/api/watch/progress/{fileId}` periodically.

## Background workers

River jobs cover anything that mustn't block an HTTP response: filesystem
scans, metadata fetches, image downloads, trickplay/thumbnail generation,
asset persistence. They execute only in `heya worker`, so queue recovery or a
large backlog can never delay Caddy from accepting traffic. The scheduler
(`internal/scheduler/`) is a 60 s trigger
loop — when a `scheduled_tasks` row is enabled, in window, and due, it
inserts a `kickoff_*` River job; the kickoff worker walks candidates and
fans out one work job per item.

Concurrency rules of thumb (full table in [`pipeline.md`](./pipeline.md#queue-config)):

- One queue per worker kind — keeps cancellation simple and isolates each
  upstream's rate-limit budget.
- Scanner kickoff/process/search/fetch/apply queues are partitioned by library
  media type, so a large Music scan cannot starve Movies, TV, Anime, or Books.
  Local analysis and apply default to four workers **per media type**. Remote
  search/fetch submission and due poll queues default to 4 workers
  per media type; pending HeyaMetadata workflow IDs are persisted, so those
  workers submit ahead instead of waiting on upstream enrichment.
- Enrich queue is `MaxWorkers=1` per kind, with priority bands
  (P1 = watcher/view, P2 = movies+TV, P3 = music+books, P4 = analysis).
- Only `download_image` runs at `MaxWorkers=4` — it hits provider CDNs, not
  the source FS.
- River caps priority at 1..4; need ≥5 bands → another queue, not another
  priority.

Worker progress crosses Postgres `LISTEN`/`NOTIFY` to the serving process and
then streams to the UI over the WebSocket event bus (`internal/eventhub/`). A
durable heartbeat also exposes worker/watcher state after process restarts. The
full match → enrich pipeline (including
HeyaMetadata client structure, orphan-job rescue, and the progress event shape)
is documented in [`pipeline.md`](./pipeline.md).

## Tests

- `go test ./...` — unit tests, no DB
- `go test -count=1 ./...` — integration tests, requires Postgres on `:5440`
- `internal/transcoder/` ships JSON fixtures covering 20+ source codecs across
  10 client profiles; the decision matrix is exercised against all of them on
  every change.
