# Architecture

Heya is a single Go process that owns everything: the HTTP API, the Nuxt SPA
(embedded at build time), and the background workers. Postgres is the only
datastore — no Redis, no Mongo, no separate job queue daemon.

```
┌─────────────────────────────────────────────────────────────────────┐
│                        ./bin/heya  (one process)                    │
│                                                                     │
│   ┌─────────────┐  ┌──────────────┐  ┌─────────────────────────┐    │
│   │ HTTP server │  │ Embedded SPA │  │ River workers           │    │
│   │ (net/http,  │  │ //go:embed   │  │ scan / match / metadata │    │
│   │  Go 1.22+)  │  │   dist/      │  │ assets / transcode      │    │
│   └──────┬──────┘  └──────┬───────┘  └────────────┬────────────┘    │
│          │                │                       │                 │
│          └────────────────┼───────────────────────┘                 │
│                           ▼                                         │
│              ┌──────────────────────────┐                           │
│              │ internal/service/  (shared business layer for CLI    │
│              │                    and HTTP — single source of       │
│              │                    truth for every feature)          │
│              └────────────┬─────────────┘                           │
│                           ▼                                         │
│              ┌──────────────────────────┐                           │
│              │ internal/database/sqlc/  │  (generated query layer)  │
│              └────────────┬─────────────┘                           │
└───────────────────────────┼─────────────────────────────────────────┘
                            ▼
                  ┌───────────────────┐
                  │   Postgres 17     │  ← data + River queue + sessions
                  └───────────────────┘

External:
  • HeyaMedia/metadata-server  — all upstream metadata (TMDB, TVDB, AniDB, …)
  • ffmpeg / ffprobe           — analysis + HLS transcoding
  • SMB shares (optional)      — library sources
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
`//go:embed all:dist`. Deploying Heya is one binary — no Node process to run
or babysit, no reverse proxy needed. The same pattern Jellyfin, Navidrome,
Emby, and friends use. See the user-facing trade-off discussion in
[CLAUDE.md](../CLAUDE.md#code-style).

### Postgres for everything

User data, media metadata, watch state, and the [River](https://riverqueue.com)
background job queue all live in the same database. Cross-feature transactions
(e.g. "matched a file → enqueued metadata fetch → recorded asset URL") are one
ACID transaction. No queue / data split to keep in sync.

### Metadata is upstream

Heya never talks to TMDB/TVDB/AniDB/etc. directly. All upstream metadata is
fetched through [`HeyaMedia/metadata-server`](https://github.com/HeyaMedia/metadata-server)
(a separate Go service that aggregates and normalizes those sources behind one
JSON API). Heya's `internal/metadata/heyamedia/heya.go` is the only client.

Benefits: rate-limit budgets aren't shared across Heya instances, one cache
serves many users, schema changes upstream don't propagate to Heya's matcher,
and the metadata server can run in any deployment topology (own machine, LAN,
or the hosted `https://heya.media`).

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
asset persistence. The scheduler (`internal/scheduler/`) enqueues recurring
work (periodic library re-scan, trickplay backfill, stale metadata refresh).

Real-time progress for those jobs streams to the UI via the WebSocket event
bus (`internal/eventhub/`).

## Tests

- `go test ./...` — unit tests, no DB
- `go test -count=1 ./...` — integration tests, requires Postgres on `:5440`
- `internal/transcoder/` ships JSON fixtures covering 20+ source codecs across
  10 client profiles; the decision matrix is exercised against all of them on
  every change.
