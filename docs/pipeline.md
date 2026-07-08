# Scanner + enrich pipeline

The path from "file appears on disk" to "fully enriched media item" is split
into two phases: a **scanner** phase that inventories local files, maps local
identities, applies playable rows, and records review state; and an **enrich**
phase that refreshes heavy metadata/artwork on queue workers.

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
  when bytes change. NFO-only re-apply keeps `media_info`, so probe work tracks
  byte changes only.

`KickoffLibraryScanArgs.Force` bypasses the unchanged check and enqueues a full
library processing run.

## Scanner processing

`internal/scanner/`. The scanner runs the same phases from the CLI and the
queue. Queue workers split those phases so slow remote metadata calls do not
hold the whole library scan hostage:

- `process_scan`: local inventory/parse/identity + HeyaMedia search.
  Persists review identities, candidates, findings, and a `search_result`
  artifact.
- `fetch_metadata`: resumes that exact search artifact, overlays any
  admin/manual decisions made after search, fetches remote metadata, and
  persists a `fetch_result` artifact.
- `apply_metadata`: resumes the fetch artifact, materializes rows and
  `library_file_links`, and fans out post-apply jobs such as ffprobe, ratings,
  NFO saves, thumbnails, chromaprint, loudness, and sonic analysis.

Each stage can process a full library or a directory scope from the watcher.
The scanner emits structured events and records local
identities/candidates/findings for the admin review UI.

- **Scoring**: scanner search modules call HeyaMedia search, score candidates
  locally, auto-accept strong matches, and persist ambiguous/rejected cases for
  manual review.
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
- **Scanner pipeline** (`kickoff_library_scan`, `process_scan`,
  `fetch_metadata`, `apply_metadata`, `ffprobe`, `scan_keyframes`,
  `detect_local_assets`) has per-queue worker counts. The default scanner
  stages use 4 workers for `process_scan`, `fetch_metadata`, and
  `apply_metadata`; heavier file/analysis queues keep lower defaults.
  `kickoff_library_scan` is the fast inventory/change detector; it skips
  unchanged paths, soft-deletes missing paths, and enqueues
  `process_scan` for changed scopes.
- **Enrich pipeline** (`enrich_media_item`, `person_fetch`, `ratings_fetch`,
  `force_refresh_metadata`) is MaxWorkers=1 per kind for upstream rate-limit
  safety. The `enrich_media_item` queue keeps the priority-banded ordering:
  - **P1** = watcher/view (a user just touched a file or opened a detail page)
  - **P2** = movies + TV
  - **P3** = music + books
  - **P4** = analysis tier
- `process_scan` uses two priority bands: **P1** = watcher
  (`fsnotify`-discovered folder), **P2** = scheduled/manual library scan.
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
| `scan_libraries`       | `kickoff_library_scan`      | `process_scan` → `fetch_metadata` → `apply_metadata` |
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

The two music tasks (`scan_music_loudness`, `analyze_music_facets`, flagged
`Pump: true` in taskdefs) don't fan out one bounded batch and finish — their
kickoff is a **pump** that stays active for the whole run, snoozing between
wakes (`river.JobSnooze`, no attempt cost) and topping the work queue up wave
by wave (500 tracks / 200 albums; 1000 sonic tracks) until the backlog drains.
The pending set is swept in id order exactly once per run via cursors kept in
the kickoff job's metadata, so permanently-failing items can't churn. Because
the kickoff row stays active, its unique-while-active hold makes a cron firing
during a run coalesce (the window is skipped), and the row's `created_at` +
metadata survive restarts — an orphaned run is rescued on boot and resumes.

Runs have a **source**: "Run Now" / CLI triggers insert the kickoff with
`{"source":"manual"}` metadata (scheduler cron firings carry no source).
Manual runs drain the entire backlog and are exempt from max-runtime
enforcement; cron-started runs stop when the backlog drains *or* the task's
max-runtime window closes (or the task is disabled), whichever comes first —
the pump checks all of that on every wake and winds down gracefully. The
scheduler's enforcement loop skips pump tasks entirely while their kickoff is
alive (the pump owns its window; non-pump tasks keep the pre-pump enforcement
unchanged) and only reaps orphaned work jobs if the pump died. A "Run Now"
click during an active cron-started run upgrades that run to manual instead
of no-oping; the upgrade and the pump's completion are serialized through a
"finishing" claim on the kickoff row (`queueops.ClaimKickoffFinish`), so an
upgrade landing mid-wake either flips the run to a full manual drain or is
rejected and starts a fresh manual run — it can never be silently swallowed
by a completing pump. Cancelling a pump run stamps the scheduled_tasks row with
`stopped` from `service.CancelTask` (a snoozed kickoff is finalized directly,
so the pump can't stamp it itself). Before declaring the backlog drained, a
pump whose sweep skipped items (inserts that coalesced with jobs owned by
another task — e.g. a library scan's loudness hand-offs — or failed inserts)
re-runs the sweep once from zero, so work dropped by the other owner isn't
stranded past the cursor.

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
