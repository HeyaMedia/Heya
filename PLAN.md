# Kura — Implementation Plan

## Context

Kura ("traditional storehouse" in Japanese) is a self-hosted media server for movies, TV series, music, books, comics, podcasts, and internet radio. Built with a Go backend, Nuxt frontend, and PostgreSQL as the single datastore (data, job queue, search). No Redis, no MongoDB — just Postgres.

Michael is learning Go through this project. Claude writes the majority of the code. An existing TypeScript parser at ~/Private/yarr provides ~2000 lines of battle-tested parsing logic and ~2700+ test cases to port.

**Design principle**: CLI-first. Every feature is built as both a CLI command and an API endpoint simultaneously. The CLI and API share a common service layer, so the entire backend can be built and tested without a frontend.

## Tech Stack

| Concern | Choice | Notes |
|---------|--------|-------|
| HTTP Router | Go stdlib `net/http` | Go 1.22+ has pattern-based routing |
| CLI | `cobra` | Industry standard Go CLI framework |
| Database | `pgx/v5` + `sqlc` | Type-safe queries from SQL, no ORM |
| Migrations | `goose` | SQL-based, reversible |
| Job Queue | `River` | PG-based, transactional enqueueing, periodic jobs |
| File Watching | `fsnotify` | Cross-platform filesystem events |
| Logging | `zerolog` | Structured JSON, fast |
| Auth | `bcrypt` + session tokens | PG-backed sessions |
| Frontend | Nuxt 4 (SPA mode) | Browser-only, REST API consumption |
| Media Metadata | ffprobe | Shelling out, more reliable than native libs |
| Transcoding | ffmpeg | HLS output for browser-compatible streaming |
| External APIs | TMDB, MusicBrainz, OpenLibrary | Posters, descriptions, external IDs |

---

## Phase 1: Go Project Bootstrap ✅

**Goal**: Compilable Go project with health endpoint, PG connection, and dev tooling.

**Build**:
- `go.mod` init, directory structure, `.gitignore`
- Config loading from env vars (`internal/config/`)
- Structured logging with zerolog
- pgx connection pool + health check (`internal/database/`)
- HTTP server with `GET /api/health` (`internal/server/`)
- Docker Compose for PostgreSQL
- Makefile: `build`, `test`, `lint`, `migrate`

**Structure**:
```
cmd/kura/main.go
internal/config/config.go
internal/server/{server,routes,middleware,health}.go
internal/database/database.go
migrations/
docker-compose.yml
Makefile
```

**Success**: `curl localhost:8080/api/health` → `{"status":"ok","database":"connected"}`

**Size**: S

---

## Phase 2: CLI Framework + Service Layer

**Goal**: Cobra-based CLI with subcommand structure and a shared service layer that both CLI and API use. Every feature from this point forward gets a CLI command AND an API endpoint.

**Build**:
- `cobra` CLI framework with root command and subcommand groups
- Shared service layer pattern (`internal/service/`) that encapsulates all business logic
- Both CLI commands and API handlers call into services — neither contains business logic directly
- Root command starts the server (default behavior): `kura serve`
- CLI command groups (stubs to be filled in subsequent phases):
  - `kura serve` — start HTTP server + workers
  - `kura user <create|list|delete>` — user management
  - `kura library <add|list|scan|remove>` — library management
  - `kura parse <path>` — parse a filename or path (useful for testing/debugging)
  - `kura job <list|status>` — inspect background jobs
- Refactor main.go to use cobra root command
- Move HTTP server startup into `kura serve` subcommand

**Structure**:
```
cmd/kura/main.go           — cobra root command
cmd/kura/serve.go          — kura serve (HTTP + workers)
cmd/kura/user.go           — kura user subcommands
cmd/kura/library.go        — kura library subcommands
cmd/kura/parse.go          — kura parse subcommand
internal/service/           — shared service layer (business logic lives here)
```

**Success**: `kura --help` shows all command groups. `kura serve` starts the server. `kura parse "Dune.Part.Two.2024.2160p.UHD.BluRay.x265-B0MBARDiERS"` prints parsed output (once Phase 3 is done).

**Size**: S

---

## Phase 3: Media Filename Parser — Core Engine

**Goal**: Pure Go parser passing all ported test cases (~2700+ cases from yarr).

This is the largest single phase. We must re-implement `@ctrl/video-filename-parser` (~1400 lines of JS, largely regex) plus yarr's parser orchestration (~600 lines).

**Build**:

*Sub-phase 3a — Video filename parser (Go port of @ctrl/video-filename-parser):*
- `internal/parser/video/` package:
  - `parse.go` — main `FilenameParse(name, isTv)` function
  - `season.go` — 50+ regex patterns from Sonarr (most complex file)
  - `resolution.go`, `source.go`, `videocodec.go`, `audiocodec.go`
  - `audiochannels.go`, `edition.go`, `group.go`, `title.go`
  - `quality.go`, `language.go`, `simplify.go`

*Sub-phase 3b — Parser orchestration (Go port of yarr's Parser/):*
- `internal/parser/` package:
  - `types.go` — SceneReleaseParse, ParsedStorageEntry, PreparedSegment
  - `tvparser.go`, `movieparser.go`, `musicparser.go`, `bookparser.go`
  - `parser.go` — ParseStoragePath, FindBestReleaseCandidate
  - `scoring.go` — ScoreVideoRelease, ScoreAudioRelease
  - `utils.go` — PrepareSegment, DetectStatus, InferMediaKind, NormalizeVideoCandidate

*Sub-phase 3c — Test data port:*
- Copy JSON fixtures from yarr → `testdata/parser/`
- Table-driven Go tests reading JSON fixtures
- Upstream corpus validation with minimum parse ratios

*Sub-phase 3d — CLI integration:*
- `kura parse "Some.Release.Name"` — parse a single release name, print result as JSON/table
- `kura parse --path /some/directory` — parse all files in a directory, print report

**Regex porting risk**: `season.go` uses lookbehinds, backreferences, and duplicate named groups. Go's stdlib `regexp` doesn't support these. Use `github.com/dlclark/regexp2` for season.go specifically; stdlib `regexp` for everything else.

**Key scoring thresholds**:
- Video: score ≥ 4 AND strong signal (resolution OR codec OR sources OR episodes)
- Audio: score ≥ 3 AND (year OR group OR sources OR codecs OR catalog)

**Success**:
- All release-parsing.json cases pass (TV: 13, Movies: 9, Music: 14, Books: 1)
- Upstream corpus meets minimum ratios (varies by corpus, 20-85%)
- `kura parse "Babylon.5.S01E01.Midnight.on.the.Firing.Line.1080p.BluRay.x264-GRP"` → correct title, season, episode

**Size**: L

---

## Phase 4: Database Schema + Migrations

**Goal**: Complete schema for users, libraries, media items, watch history. sqlc-generated Go code.

**Build**:
- 10 goose migrations:
  1. `users` (id, username, password_hash, is_admin, timestamps)
  2. `sessions` (id, user_id, token, expires_at)
  3. `libraries` (id, name, media_type enum, paths text[], scan_interval, created_by)
  4. `media_items` (id, library_id, media_type enum, title, year, description, poster_path, external_ids jsonb, timestamps)
  5. `movies` (media_item_id FK, tmdb_id, imdb_id, runtime, tagline)
  6. `tv_series` + `tv_seasons` + `tv_episodes`
  7. `artists` + `albums` + `tracks`
  8. `books` + `authors`
  9. `library_files` (path, size, mtime, media_item_id FK, parse_result jsonb, status)
  10. `watch_history` (user_id, media_item_id, progress_seconds, completed, watched_at) + search indexes (GIN on tsvector)
- sqlc queries for all tables
- `sqlc.yaml` config

**Schema notes**:
- `media_items` is the shared base table; type-specific tables FK to it
- `library_files.parse_result` stores full SceneReleaseParse as jsonb for debugging
- `search_vector` tsvector column on media_items, populated by trigger, GIN-indexed
- River creates its own tables automatically

**Success**: `goose up` + `goose down` clean. `sqlc generate` produces valid Go. Integration test creates user → library → media item → query back.

**Size**: M — *Can run in parallel with Phase 3*

---

## Phase 5: Auth + User/Library Management

**Goal**: User registration, login, session auth, library CRUD — via both CLI and API.

**Build**:
- `internal/service/user.go` — user creation, authentication, session management
- `internal/service/library.go` — library CRUD, path validation
- `internal/auth/` — bcrypt hashing, session token generation, PG session store, auth middleware
- API endpoints:
  - `POST /api/auth/register` (first user = admin)
  - `POST /api/auth/login` → session token
  - `POST /api/auth/logout`
  - `GET /api/auth/me`
  - `POST /api/libraries`, `GET /api/libraries`, `GET/PUT/DELETE /api/libraries/:id`
- CLI commands:
  - `kura user create --username admin --password secret [--admin]`
  - `kura user list`
  - `kura user delete --username someone`
  - `kura library add --name "Movies" --type movie --path /Volumes/Storage/Movies/Foreign`
  - `kura library list`
  - `kura library remove --id 1`
- Integration tests against real PG

**Success**: `kura user create --username admin --password test --admin` + `kura library add --name Movies --type movie --path ./testdata/storage/Movies` both work. API equivalents return same results. 401 on unauthenticated API requests.

**Size**: M — *Can run in parallel with Phase 3*

---

## Phase 6: Library Scanning + File Discovery

**Goal**: Scan filesystem → run parser → persist results. The "scan a library and see what's in it" milestone.

**Build**:
- `internal/service/scanner.go` — filesystem walk, parser integration, upsert library_files
- API endpoints:
  - `POST /api/libraries/:id/scan` (synchronous initially)
  - `GET /api/libraries/:id/files` (paginated, with parse results)
  - `GET /api/libraries/:id/files/stats` (counts by media type, status)
- CLI commands:
  - `kura library scan --id 1` (or `--name "Movies"` or `--all`)
  - `kura library files --id 1 [--media video] [--status ready]`
  - `kura library stats --id 1`
- Concurrent walking with `errgroup`
- Smart re-scan: skip files with unchanged mtime/size

**Success**: `kura library scan --id 1` on test fixtures shows parsed entries. `kura library stats --id 1` shows correct counts. Re-scan detects new/deleted files.

**Size**: M — *Requires Phases 3, 4, 5*

---

## Phase 7: Background Jobs with River

**Goal**: Scans and metadata ops run asynchronously. Periodic re-scans at configured intervals.

**Build**:
- `internal/worker/` — River client, worker pool
- Job types:
  - `ScanLibraryJob` — full library scan
  - `MatchMediaJob` — match a library_file to external metadata (stub, filled Phase 8)
  - `ScanFileJob` — single file scan (used by watcher in Phase 9)
- River periodic jobs for auto re-scan per library.scan_interval
- API updates:
  - `POST /api/libraries/:id/scan` → 202 Accepted + job ID
  - `GET /api/jobs/:id` — job status
- CLI commands:
  - `kura job list [--status pending|running|completed|failed]`
  - `kura job status --id <job_id>`

**Success**: `kura library scan --id 1` enqueues async job. `kura job list` shows it. Periodic jobs fire at interval. Failed jobs retry.

**Size**: M

---

## Phase 8: Metadata Fetching + Media Matching

**Goal**: Parsed files get matched to TMDB/MusicBrainz/OpenLibrary. Rich media_items with posters and descriptions.

**Build**:
- `internal/metadata/tmdb/` — search by title+year, get details, get images
- `internal/metadata/musicbrainz/` — search releases by artist+title+year
- `internal/metadata/openlibrary/` — search by title/author
- `internal/service/matcher.go` — given SceneReleaseParse, find the right external ID
- `MatchMediaJob` fully implemented — creates/links media_items
- `PosterJob` — download artwork to `data/images/`
- Rate limiting (TMDB: 40/10s, MusicBrainz: 1/s)
- API — media browsing:
  - `GET /api/movies`, `GET /api/movies/:id`
  - `GET /api/tv`, `GET /api/tv/:id` (with seasons/episodes)
  - `GET /api/music`, `GET /api/music/:id` (with tracks)
  - `GET /api/images/:type/:id/poster`
  - `GET /api/search?q=...` (PG full-text search)
- CLI commands:
  - `kura media list [--type movie|tv|music|book]`
  - `kura media info --id <media_item_id>`
  - `kura media search "dune"`
  - `kura media match --library-id 1` (trigger matching for unmatched files)
  - `kura media refresh --id <media_item_id>` (re-fetch metadata)

**Success**: `kura library scan --id 1` + `kura media list --type movie` shows "Dune: Part Two" with TMDB data. `kura media search "dune"` finds it. No duplicates on re-scan.

**Size**: L

---

## Phase 9: Filesystem Watching (fsnotify)

**Goal**: Libraries auto-detect new files without manual scans.

**Build**:
- `internal/watcher/` — fsnotify per library path, event debouncing (2s default)
- File create/rename → enqueue `ScanFileJob`
- File delete → mark library_file deleted
- Dir create → add new watcher
- Watcher lifecycle tied to library CRUD (create/update/delete library → start/stop watchers)
- Graceful shutdown on SIGTERM
- API: `GET /api/libraries/:id/watcher` — status endpoint
- CLI: `kura library watch --id 1` — show watcher status

**Success**: Drop file into watched dir → appears in library within 5s. Delete → removed. Watchers resume after server restart.

**Size**: M — *Can run in parallel with Phase 8*

---

## Phase 10: Nuxt Frontend

**Goal**: Browser UI for registration, login, library management, media browsing with posters, and search.

**Build**:
- Nuxt project in `frontend/` (SPA mode, no SSR)
- Pages: login, register, dashboard, movies grid, movie detail, TV grid, series detail, music grid, album detail, library management, search results, user settings
- Composables: `useAuth()`, `useLibraries()`, `useMedia()`, `useSearch()`
- Poster card components, sidebar navigation
- Responsive layout (desktop + tablet)

**Success**: Full flow: register → login → create library → scan → browse movies with posters → search → find results.

**Size**: L

---

## Phase 11: Media Playback — Direct Play

**Goal**: Play media files in the browser with watch progress tracking.

**Build**:
- `internal/service/streaming.go` — file serving logic
- `internal/streaming/` — HTTP Range request handler for serving media files
- API endpoints:
  - `GET /api/stream/:file_id` (Range header support)
  - `POST /api/watch/:media_item_id/progress`
  - `GET /api/watch/continue` ("Continue Watching" list)
- CLI commands:
  - `kura watch history --user admin`
  - `kura watch continue --user admin`
- Frontend: HTML5 video/audio player components, progress auto-save every 10s
- "Continue Watching" section on dashboard

**Success**: Play MP4/MP3 in browser. Seek works via Range requests. Position persists. `kura watch history --user admin` shows watched items.

**Size**: M

---

## Phase 12: FFmpeg Transcoding Pipeline

**Goal**: On-the-fly and pre-transcoding of media files for browser-compatible playback. MKV, FLAC, and other formats that browsers can't play natively get transcoded to HLS.

**Build**:
- `internal/transcoder/` package:
  - `probe.go` — ffprobe wrapper: extract codec, container, resolution, audio channels, subtitle tracks
  - `transcode.go` — ffmpeg wrapper: transcode to HLS (h264/h265 + AAC in fMP4 segments)
  - `profiles.go` — transcoding profiles (direct play, remux, 1080p, 720p, audio-only)
  - `session.go` — manage active transcoding sessions (start, stop, progress)
  - `cache.go` — manage transcoded segment cache in `data/transcode/`
- `internal/service/playback.go` — decide per-file: direct play, remux, or transcode based on client capabilities
- Decision logic:
  - MP4 (h264 + AAC) → direct play
  - MKV (h264 + AAC) → remux to MP4 (fast, no re-encoding)
  - MKV (h265/HEVC) → transcode to HLS or direct play if client supports HEVC
  - FLAC → transcode to AAC on-the-fly, or direct play if browser supports
- API endpoints:
  - `GET /api/stream/:file_id/hls/master.m3u8` — HLS master playlist with quality variants
  - `GET /api/stream/:file_id/hls/:quality/:segment.m4s` — HLS segments
  - `GET /api/stream/:file_id/probe` — media file info (codecs, resolution, etc.)
  - `DELETE /api/transcode/cache` — clear transcoding cache
- CLI commands:
  - `kura transcode probe --file <path>` — show ffprobe info
  - `kura transcode test --file <path> --profile 1080p` — test transcode a file
  - `kura transcode cache --clear` — clear cache
  - `kura transcode cache --stats` — show cache size/usage
- Worker: `TranscodeJob` — background pre-transcoding for libraries marked as "pre-transcode"
- Frontend: update video player to use HLS.js for HLS playback, with quality selector

**Success**: Play an MKV file in the browser via HLS. Quality selector switches between profiles. `kura transcode probe --file some.mkv` shows codec info. Transcoding cache is managed and clearable.

**Size**: L

---

## Phase Summary

| # | Phase | Depends On | Size | Key Deliverable |
|---|-------|-----------|------|-----------------|
| 1 | Project Bootstrap ✅ | — | S | Health endpoint, PG connection |
| 2 | CLI Framework | 1 | S | Cobra CLI, service layer pattern |
| 3 | Filename Parser | 2 | L | Full parser, ~2700 test cases, `kura parse` |
| 4 | Database Schema | 1 | M | Migrations, sqlc code |
| 5 | Auth + User/Library | 4 | M | `kura user/library` + API endpoints |
| 6 | Library Scanning | 3,4,5 | M | `kura library scan` + API |
| 7 | Background Jobs | 6 | M | River queue, async scans, `kura job` |
| 8 | Metadata Fetching | 7,4 | L | TMDB/MusicBrainz, `kura media` |
| 9 | Filesystem Watching | 7,6 | M | Auto-detect new files |
| 10 | Nuxt Frontend | 5,8 | L | Full browsing UI |
| 11 | Media Playback | 8,10 | M | Direct play + watch history |
| 12 | FFmpeg Transcoding | 11 | L | HLS streaming, MKV support |

**Critical path**: 1 → 2 → 3 → 6 → 7 → 8 → 11 → 12

**Parallelizable**: Phases 3 and 4 can run after Phase 2. Phase 5 can run alongside Phase 3. Phase 9 can run alongside 8 or 10. Phase 10 can start once 5 and 8 are done.

---

## Known Risks

1. **Regex porting (Phase 3)**: season.go has 50+ patterns using lookbehinds, backreferences, and duplicate named groups. Go stdlib `regexp` doesn't support these. Mitigation: use `regexp2` for season.go.

2. **MKV browser playback (Phase 11)**: Most browsers can't play MKV natively. Direct play covers MP4/WebM only. Phase 12 (transcoding) closes this gap.

3. **FFmpeg dependency (Phase 12)**: Transcoding requires ffmpeg installed on the host. We shell out rather than use CGo bindings — simpler, more maintainable, and ffmpeg's CLI is well-documented.

4. **TMDB API key**: Required for Phase 8. Already provided.

---

## Key Source Files to Port from Yarr

| Yarr File | Go Target | Notes |
|-----------|-----------|-------|
| `server/Parser/index.ts` | `internal/parser/parser.go` | Main orchestrator |
| `server/Parser/utils.ts` | `internal/parser/utils.go`, `scoring.go` | Scoring, segment handling |
| `server/Parser/TVParser.ts` | `internal/parser/tvparser.go` | TV strategy |
| `server/Parser/MovieParser.ts` | `internal/parser/movieparser.go` | Movie strategy |
| `server/Parser/MusicParser.ts` | `internal/parser/musicparser.go` | Audio heuristic |
| `node_modules/@ctrl/video-filename-parser/` | `internal/parser/video/` | Full re-implementation |
| `tests/fixtures/storage/` | `testdata/storage/` | Filesystem fixtures |
| `tests/fixtures/scene-parser/` | `testdata/parser/` | JSON test datasets |
