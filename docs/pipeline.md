# Match + enrich pipeline

The path from "file appears on disk" to "fully enriched media item" is split
into two phases: a **match** phase that produces a stub from a single search
call, and an **enrich** phase that fans out the heavy detail work.

## Scan (change detection first)

`internal/scanner/`. A rescan of an already-imported library is designed to
cost near-zero I/O beyond the directory walk itself:

- **One preload query, not one per file.** `ListLibraryFilesForScan` loads the
  whole library into a map up front; the walk's known/changed check is a map
  lookup. A file whose size + mtime (µs-truncated) match is skipped.
- **NFO parsing is lazy.** Canonical NFOs (`tvshow/movie/artist/album.nfo`)
  are only opened when a new/changed file actually needs one (nearest-ancestor
  resolution, memoized per directory). An unchanged rescan opens zero NFOs.
- **NFO edits are detected by mtime, not by re-reading.** The walk sees each
  NFO's mtime for free in the dir listing; `library_nfo_dirs` records what was
  last applied per directory. On drift (edit/add/remove), only the files under
  that directory get their `parse_result` rebuilt (`ReapplyLibraryFileParse`)
  and re-enter the pipeline as `pending` — so local-metadata changes land on
  the next scan without a force rescan.
- **No redundant ffprobe on re-apply.** The walk upsert clears `media_info`
  when bytes change; `ProcessFile` skips the probe when `media_info` is still
  populated (NFO-only re-apply), so probe work tracks byte changes only.

`ScanOptions.ForceRescan` bypasses the unchanged check and re-upserts
everything (which also clears probe data → full re-probe + re-match).

## Match (search-only stub)

`internal/matcher/`. The scanner emits a parsed filename; the
`MetadataMatchWorker` calls HeyaMedia's `/api/v1/search` exactly once, scores
each hit locally, and on auto-match writes a stub `media_items` row containing
only what the search response carries (title, year, snippet → description,
image → poster URL, external_ids, `alt_titles`). No `GetDetail` call.
Sub-second per file. The item is now visible in the UI as a stub.

- **Scoring**: `internal/matcher/confidence.go::ScoreConfidence` (Levenshtein
  on normalized titles + year boost + substring-containment bonus for the
  "Title: Subtitle" pattern), then
  `internal/matcher/matcher.go::scoreBestTitle` projects that over
  `[primary, ...AltTitles]` and takes the max — that's how romaji filenames
  resolve against English canonical titles via HeyaMedia's `alt_titles[]`.
- **Threshold**: `MatchOptions.AutoMatchThreshold` (default `0.85`) —
  `internal/matcher/matcher.go::autoMatchThresholdFor` lowers it to `0.75`
  when the hit is `enriched` (HeyaMedia has it warm-cached and
  cross-confirmed).
- **Tuning probe**: `go test -v -run TestProbeAutoMatch ./internal/matcher/`
  exercises a 43-case corpus against a running HeyaMedia and reports the score
  distribution. Skips silently when HeyaMedia is unreachable.

## Enrich (unified queue, priority-banded)

`internal/worker/enrich_worker.go`. One worker kind,
`EnrichMediaItemArgs{ItemID, Source, Force}`, dispatches internally on
`media_type`:

- **Movies / TV / books**: `heya.GetDetail` →
  `Matcher.StoreEntityMetadata` (type-specific row + TV seasons/episodes) →
  `StoreRichMetadata` (cast/crew/keywords) → enqueues `DetectLocalAssetsArgs`
  (image pipeline) + `PersonFetch` + `RatingsFetch` + `SaveNFO`.
- **Music**: delegates to `Matcher.RefreshMusicArtist` (artist+album+track
  upsert from the discography payload) + optional `SaveMusicNFO`.

Each component stamps its `*_enriched_at` column on success
(`base / people / extras / images / structure`). The worker short-circuits on
`enrichment_status='complete'` unless `Force=true`, so redundant enqueues are
cheap. Hard failures write `last_enrich_error` and set status to `failed` —
surfaced in the tasks-page items modal.

## Queue config

In `internal/worker/worker.go`:

- **One queue per worker kind.** No queue is shared across kinds — keeps
  cancellation simple (cancel-by-kind cancels exactly the work it should), and
  lets each external dependency (HeyaMedia search, TMDB, ratings providers)
  carry its own concurrency knob without contending with unrelated work.
- **Scanner pipeline** (`process_file`, `ffprobe`, `detect_local_assets`,
  `metadata_match`, `kickoff_library_scan`) is **MaxWorkers=1 end-to-end** —
  protects the source filesystem / SMB share from concurrent IO during a scan.
- **Enrich pipeline** (`enrich_media_item`, `person_fetch`, `ratings_fetch`,
  `force_refresh_metadata`) is MaxWorkers=1 per kind for upstream rate-limit
  safety. The `enrich_media_item` queue keeps the priority-banded ordering:
  - **P1** = watcher/view (a user just touched a file or opened a detail page)
  - **P2** = movies + TV
  - **P3** = music + books
  - **P4** = analysis tier
- `process_file` uses two priority bands: **P1** = watcher (`fsnotify`-discovered),
  **P2** = scan.
- `download_image` is **MaxWorkers=4** — the lone exception, since downloads
  hit provider CDNs (not the source FS). Everything else is 1.
- River caps priority at **1..4 (hardcoded)**. Need ≥5 bands → introduce
  another queue, not another priority.
- `RescueStuckJobsAfter: 30 * time.Minute` — backstop above the slowest
  legitimate job; lower numbers preempt slow-but-healthy artist enriches.
- HeyaMedia HTTP client timeout: 5 minutes
  (`internal/metadata/heyamedia/client.go`) — worst-case ceiling per call,
  callers can cancel sooner via ctx.

## Enqueue API

`internal/worker/enqueue.go` — single source of truth, every caller goes
through one of these:

- `EnqueueEnrich(ctx, rc, itemID, mediaType, source)` — scheduled, scan, etc.
- `EnqueueEnrichForce(...)` — user clicked "refresh metadata"; bypasses the
  `complete` short-circuit.
- `EnqueueEnrichBatch(..., batchLibID, batchTotal, batchPos)` — post-scan
  fan-outs that want progress events.
- `EnqueueEnrichTx(ctx, itemID, mediaType, source)` — for callers already
  inside a River worker (pulls the client out of ctx).

**View-promotion**: `service.GetMediaDetail` calls
`EnqueueEnrich(..., EnrichSourceView)` for any item not at
`enrichment_status='complete'`, lifting that single item to priority 1 ahead of
any background work.

## Scheduled tasks

The 60 s trigger loop in `internal/scheduler/scheduler.go` inserts a `kickoff_*`
River job when a `scheduled_tasks` row is enabled, in window, and due. The
kickoff worker (`internal/worker/kickoff_workers.go`) walks candidates and fans
out one work job per item.

Same pattern for all six scheduled tasks:

| Task                   | Kickoff kind                | Per-item kind        |
| ---------------------- | --------------------------- | -------------------- |
| `scan_libraries`       | `kickoff_library_scan`      | `process_file`       |
| `refresh_stale_items`  | `kickoff_refresh_stale`     | `enrich_media_item`  |
| `scan_music_loudness`  | `kickoff_music_loudness`    | `scan_track_loudness`|
| `generate_trickplay`   | `kickoff_trickplay`         | `trickplay_file`     |
| `generate_thumbnails`  | `kickoff_thumbnails`        | `thumbnail_extra`    |
| `analyze_music_facets` | `kickoff_sonic_analysis`    | `analyze_track_facets` |

`internal/taskdefs` is the single registry for the kinds (kickoff + per-item
workers) each scheduled task drives. Kickoff workers stamp `scheduled_task_id`
into every child job; `/api/tasks/{id}/cancel`, runtime counts, and max-runtime
enforcement use that marker so watcher/manual/view jobs sharing the same worker
kinds are left alone.

**Trickplay + thumbnails are kickoff-driven only.** Never trigger them inline
from the scan pipeline. Trickplay defaults off per-library.

## Progress over WebSocket

Every worker pushes its current item to `task.progress` events via
`worker.TaskProgressBroadcaster` (constructed in `service/app.go`, threaded
through `worker.Config.Progress`). Two channels merge into the same event type:

- **Per-worker** — each work worker calls
  `progress.SetCurrentByKind(kind, label)` at the top of `Work()`. Carries
  `{task_id, state: "running", current_item, item_kind}`. `kind → task_id` is
  resolved via the inverted `internal/taskdefs` registry.
- **Periodic** — `eventhub/periodic.go::taskProgressTicker` runs every 2 s and
  emits `{task_id, state, pending, running}` per scheduled task from a
  `river_job` count scoped by `scheduled_task_id` for scheduled tasks, and by
  kind for synthetic buckets.

The FE merges in `useEventBus.ts::task.progress` — counts overwrite without
touching `current_item` and vice versa, so the dict always carries the latest
of both halves.

## Orphan-job rescue at startup

`app.RescueOrphanedJobsAtStartup` runs in `cmd/heya/cmd/serve.go` **before**
`app.StartWorkers`. It flips every `state='running'` row back to `'available'`,
unconditionally — at boot, no worker in this process has started yet, so every
running row is by definition an orphan from a prior unclean exit (air reload
mid-job, OS kill, etc.).

Without this, those rows sit until River's periodic rescuer catches them after
`RescueStuckJobsAfter` (30 min), which is long enough to make MaxWorkers=N look
violated after every dev hot-reload.

Past bug: 4 `analyze_track_facets` rows appeared "concurrent" after rapid `air`
reloads even though MaxWorkers=1 on `sonic_analysis` was honored — each row was
attributed to a different dead `attempted_by` client.

## HeyaMedia client structure

- **`clients/heyamedia/client.gen.go`** — typed Go client generated by
  `oapi-codegen` from `clients/heyamedia/openapi-3.0.json` (committed spec
  snapshot). Don't hand-edit; `make gen-heyamedia-client` refreshes. Spec is
  fetched from `$(HEYAMEDIA_URL)/api/openapi-3.0.json`.
- **`internal/metadata/heyamedia/heya.go`** — `HeyaProvider` orchestration:
  search, fetch-by-kind-id, lookup-by-NFO, similar-artists, person. Wraps
  `gen.ClientWithResponses`; no hand-rolled HTTP.
- **`internal/metadata/heyamedia/mappers.go`** — per-kind mappers
  (`mapArtistDoc` / `mapMovieDoc` / `mapTvDoc` / `mapBookDoc` / `mapPersonDoc`)
  translating generated DocBody structs into `metadata.MediaDetail`. Cast,
  crew, keywords, seasons, artist relations all live here.
- **`internal/metadata/heyamedia/pointers.go`** — `strPtr` / `intPtr64` /
  `mapStr` / `strs` nil-safe helpers; the generated types are
  pointer-everywhere.
- **`internal/metadata/heyamedia/client.go`** — thin wrapper holding the
  generated client; 5-minute HTTP timeout backstop for cold artist enriches.
- **Golden tests**:
  `internal/metadata/heyamedia/mapdetail_golden_test.go` + `testdata/*.json` +
  `*.detail.golden.json` snapshot a real heya.media response per kind.
  Regenerate with `go test ... -update-golden` after intentional mapping
  changes, then diff the golden to confirm only intended fields moved.

### HeyaMedia response shape

- Top-level `ids` carries native numeric types (`tmdb: 1429` int); payload
  `external_ids` is consistently `map[string]string`. The generated
  `ExternalIDsDTO` handles the int side; the legacy `flexIDs` hand-decoder was
  retired in the refactor.
- `alt_titles[]` is the union of every locale variant for the hit. Flows
  through `metadata.SearchResult.AltTitles` and gets scored alongside the
  primary `Title`.
