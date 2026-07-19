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
  lookup. A file whose size + mtime (µs-truncated — Postgres stores µs, stat
  returns ns; `libraryFileChanged`) match is skipped.
- **Unmatched files are parked.** Identities that land in
  unmatched/needs_review/rejected/ignored still get `library_files` rows
  (status `unmatched`, current size+mtime) at `process_scan` time, so they
  don't re-trigger a provider search every scan. Review actions (approve /
  assign / rematch) enqueue a forced scoped `process_scan` that bypasses
  change detection.
- **Moves relocate, not delete+create.** A new path matching a gone row by
  size + basename (or size + µs-mtime) keeps its `library_files` id — probe
  data, trickplay, segments, fingerprints, and `track_files` survive. Both
  owner scopes re-enter the pipeline, since naming carries identity.
- **NFO edits are detected by mtime, not by re-reading.** The walk sees each
  NFO's mtime for free in the dir listing; `library_nfo_dirs` records what was
  last seen per directory. On drift (edit/add/remove) the owning scope is
  marked changed and re-enters the pipeline — local-metadata changes land on
  the next scan without a force rescan.
- **No redundant ffprobe on re-apply.** `UpsertLibraryFile` clears
  `media_info`/`keyframes` only when size or µs-mtime actually changed;
  re-applies of unchanged files (force rescans, review re-identifies,
  relocated scopes) keep their probe artifacts.
- **First-pass matchers can address new files.** Immediately after local
  analysis, every primary/extra media file is observed in `library_files`
  with the exact size+mtime tuple used by the artifact. New/restored/changed
  rows stay `pending`; unchanged terminal rows stay terminal. This lets the
  inline Chromaprint/AcoustID fallback run during the first search instead of
  waiting for an apply or review-parking pass. Changed bytes invalidate probe,
  format, hash, trickplay, and segment-derived state at the same boundary.
- **Pending is a recoverable lease, not a perpetual change.** An unchanged
  pending tuple is ignored while its owner scope still has a live River stage
  job or parked metadata continuation. This prevents a scheduled kickoff from
  superseding a slow generation over and over. A real size/mtime change always
  supersedes immediately; when the job/continuation disappears, the pending
  tuple becomes eligible again so a crashed pipeline self-heals.
- **Deletions are soft, cleanup is manual — by design.** Kickoff soft-deletes
  rows for files gone from disk so the UI can show what's missing; media
  items themselves are only removed by the user-triggered
  `CleanupMissingMedia` pass (dashboard missing-count), never automatically.

`KickoffLibraryScanArgs.Force` bypasses the unchanged check and enqueues a full
library processing run.

## Scanner processing

`internal/scanner/`. The scanner runs the same phases from the CLI and the
queue. Queue workers split those phases so slow remote metadata calls do not
hold the whole library scan hostage:

- `process_scan`: local inventory/parse/identity only. Persists one narrow
  `analysis_result` artifact per identified entity, then enqueues each entity
  independently for remote search.
- `search_metadata`: resumes that exact analysis artifact, overlays any
  admin/manual decisions, runs HeyaMetadata search/discovery, and persists
  review identities, candidates, findings, and a `search_result` artifact.
- `fetch_metadata`: resumes that exact search artifact, overlays any
  admin/manual decisions made after search, fetches remote metadata, and
  persists a `fetch_result` artifact.
- `apply_metadata`: resumes the fetch artifact, materializes rows and
  `library_file_links`, and fans out post-apply jobs such as ffprobe, ratings,
  NFO saves, thumbnails, chromaprint, loudness, and sonic analysis.

Every owner scope has a monotonically increasing pipeline generation. Each
stage artifact records that generation and its exact predecessor; finalizing a
stage locks the current entity, validates the source snapshot again, writes
the scan/review projection, creates the next artifact, and advances the entity
in one transaction. An older worker therefore becomes a successful no-op
instead of overwriting a newer review or apply result. A retained artifact is
replayable only when its semantic pipeline revision is current and every
captured file still has the same size and µs-mtime. Removed NFO/artwork,
replaced audio, and older matcher artifacts trigger fresh owner-scope analysis.
The apply worker validates that snapshot before entering the domain apply and
again inside the movie/TV/book/music transaction immediately before commit;
a source changed during apply therefore rolls the catalog write back. The
post-commit finalizer validates once more and schedules corrective analysis for
the unavoidable filesystem/transaction boundary race.

The scanner emits structured events and records local
identities/candidates/findings for the admin review UI.

**The scan unit is the owner directory.** Kickoff is deliberately dumb: it
walks, diffs, and enqueues exactly one `process_scan` job per changed owner
unit — the artist dir for music, the author dir for books/audiobooks, the
movie dir or show dir for video (season/extras dirs promote up to their
owner), or a loose file that is its own unit. Everything smarter than the
directory structure happens downstream in that unit's own job: filename/NFO
parsing, the tag probe (music, inside the unit's job — never gating the
walk), identity grouping, and the provider search. Mixed directories
("Loose Tracks", loose fansubs) stay one scan unit; identify re-fans them
into per-identity work.

**Known units usually skip the search.** Every accepted identity persists in
`local_media_identities` with its provider id and decision provenance. Manual
approve/reject/ignore choices are durable across matcher upgrades. Automatic
accepts are reused only when their stored matcher revision equals the current
revision; changing normalization, scoring, or acceptance policy deliberately
reconsiders older automatic decisions once. The overlay short-circuits
*before* the HeyaMetadata call, so an unchanged accepted artist still costs
zero provider searches. A local key simultaneously claimed by more than one
owner scope bypasses the global overlay because applying one same-name decision
to multiple possible artists is unsafe. Root or multi-owner
scoped jobs (legacy batches, pruner requeues) re-fan into per-owner jobs
before running, so one slow unit can never hold up others and unique args
stay stable per unit across scans.

- **Scoring**: scanner search modules call HeyaMetadata search, score candidates
  locally, and apply V2's recommendation as a hard safety gate. Strong matches
  may auto-select; ambiguous/no-match results remain manual even when the local
  title scorer is high.
- **Music escalation**: name/release/provider evidence gets the cheap first
  attempt. If it is still ambiguous, independently fetched candidate details
  may converge hard IDs. Only then are up to three representative files
  fingerprinted/looked up through AcoustID. Multiple high-quality recordings
  must converge on one canonical artist; configuration and transient failures
  remain typed job failures rather than fake no-matches. Accepted acoustic
  evidence retains the exact file path, recording MBID, canonical recording
  UUID, score, and durations through materialize/apply. It may fill an empty
  track identity but can never replace a conflicting fetched or stored one.
- **Threshold**: `MatchOptions.AutoMatchThreshold` (default `0.85`) —
  `internal/matcher/matcher.go::autoMatchThresholdFor` lowers it to `0.75`
  when the hit is `enriched` (HeyaMetadata has it canonical and warm-cached and
  cross-confirmed).
- **Tuning probe**: `go test -v -run TestProbeAutoMatch ./internal/matcher/`
  exercises a 43-case corpus against a running HeyaMetadata and reports the score
  distribution. Skips silently when HeyaMetadata is unreachable.

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
  lets each external dependency (HeyaMetadata search, ratings, community segments)
  carry its own concurrency knob without contending with unrelated work.
- **Scanner pipeline** (`kickoff_library_scan`, `process_scan`,
  `search_metadata`, `fetch_metadata`, `apply_metadata`, `ffprobe`,
  `scan_keyframes`, `detect_local_assets`) has per-queue worker counts.
  `process_scan` now performs local analysis only and persists one narrow
  analysis artifact per owner entity. `search_metadata` resumes that artifact
  and submits canonical search/discovery without re-walking the library.
  Search submission, search polling, fetch/resolution submission, and fetch
  polling each default to 4 workers per media-type queue; local analysis and
  apply also default to 4.
  Every stage is partitioned by library media type
  (`*_movie`, `*_tv`, `*_anime`, `*_music`, `*_book`, etc.), and the configured
  worker count applies to every media-type queue. This prevents an older bulk
  Music fan-out from FIFO-starving later Anime/TV/Movie work. The unsuffixed
  kickoff queue remains the lightweight `Scan all` coordinator and fallback.
  `kickoff_library_scan` is the fast inventory/change detector; it skips
  unchanged paths, soft-deletes missing paths, and enqueues
  `process_scan` for changed scopes.
- **Durable remote hand-off.** HeyaMetadata `202` discovery and resolution
  identifiers are stored in `metadata_resolution_workflows`. The submission
  job parks one compact row in `scanner_metadata_continuations` and completes,
  immediately freeing submission capacity for another entity. Discovery waits
  are event-driven: once a minute `sync_metadata_workflow_events` reads the
  gap-free HeyaMetadata completion feed, persists only IDs already known to
  this server, and transactionally wakes their parked rows while advancing the
  cursor. The small inbox closes the race where an event lands just before the
  continuation is parked; repeated sequences and unknown workflow kinds/IDs
  are harmless. Completed-at-submission discoveries are never parked.
  A five-second sweeper promotes at most 100 ready continuations at a time via
  one batched River insert onto `search_metadata_poll_*` /
  `fetch_metadata_poll_*`. Search discoveries retain a jittered 30–40 minute
  reconciliation check as a correctness backstop; transient submissions with
  no workflow ID retain the adaptive one-to-five-minute retry, and longer
  provider `Retry-After` values always win. Resolution jobs continue through
  that bounded reconciliation path until their workflow family joins the feed.
  A five-minute lease plus River's active-args uniqueness makes the hand-off
  crash-safe without stacking duplicates. During the rollout the same sweeper
  adopts at most 10,000 legacy scheduled poll jobs per tick, so an old backlog
  drains out of River without one enormous migration transaction.
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
- HeyaMetadata HTTP client timeout: 3 minutes
  (`internal/metadata/heyametadata/client.go`) — discovery and resolution
  `202` resources are polled by the durable workflow instead of holding one
  provider request open indefinitely.

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
| `scan_libraries`       | `kickoff_library_scan`      | `process_scan` → `search_metadata` → `fetch_metadata` → `apply_metadata` |
| `refresh_stale_items`  | retired compatibility no-op | —                    |
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

## HeyaMetadata V2 client structure

- **`clients/heyametadata/openapi.yaml`** and **`client.gen.go`** are the
  committed V2 contract snapshot and generated transport. Run
  `make gen-heyametadata-client`; `make check-heyametadata-client` proves drift.
- **`internal/metadata/heyametadata/client.go`** wraps typed health, entity,
  image, ratings, relations, top-track, release, change-feed, and workflow-event
  operations.
- **`workflow.go`** owns opaque selected IDs plus durable
  search → discovery → resolution/job polling state. Request-scoped provider
  credentials are headers only and never enter workflow rows or cache keys;
  polling honors `Retry-After` and uses bounded exponential jitter.
- **`models.go` / `mappers.go`** decode polymorphic canonical documents into
  Heya's transitional relational writer shape. Canonical UUID, kind, schema
  version, and projection version remain separate from provider evidence.
- **`provider.go`** is the compatibility facade consumed by scanner, matcher,
  and enrich workers. An exact acceptable local canonical hit is the fast path;
  fuzzy-only results remain review candidates and do not suppress discovery.
  There is no fallback to V1 provider fan-out.
- **`metadata_changes_worker.go`** consumes the gap-free cursor in pages of
  500. River refresh inserts and cursor advancement share one pgx transaction.
  The same worker gradually binds pre-V2 local rows that still have only
  provider evidence.
- **`metadata_scope_worker.go`** routes independently fetched child scopes
  from that same feed. Artist `top_tracks` is the first scope: its own
  projection checkpoint makes a successful empty list durable and ensures a
  parent projection version cannot falsely suppress it. The dispatcher/feed
  are shared with movies, TV, books, recordings, and people; future child
  scopes do not need another cursor.
- **`metadata_workflow_events_worker.go`** consumes the independent workflow
  cursor in pages of 500. It filters the global stream against locally known
  discovery IDs and commits the inbox wake plus cursor in one transaction;
  replay, stream resets, and repeated run IDs remain idempotent.

### Identity and auto-selection

- Canonical IDs use opaque `heyametadata:v2:entity:<uuid>` provider tokens in
  scanner artifacts; unresolved selections carry a base64-encoded resolution
  object until accepted.
- V2 `recommendation` and per-field `evidence` survive scanner persistence.
  A downstream title scorer cannot override `requires_review`.
- `strong_match` may auto-select. `likely_match` needs multiple structured
  corroborating hints, while audiobooks stay manual. `ambiguous` and
  `no_match` are never automatic.
- Canonical bindings cover media items, artists, release groups, recordings,
  people, authors, seasons, and episodes. Provider IDs remain evidence only.
- Artist top-track refreshes retain provider ranking/metrics and canonical
  recording UUIDs. Newer successful observations replace the provider snapshot;
  identical content only advances freshness, older observations are ignored,
  and endpoint failure preserves the last known ranking while scheduling its
  own bounded retry independently of the general artist refresh.

### Images and refresh

Opaque image IDs map to `/api/v2/images/{id}`. Heya treats its configured
metadata origin as an explicit trusted downloader source (including optional
bearer auth), while arbitrary NFO/database URLs retain SSRF-safe dialing. A
first `202` response is polled until bytes are available.

Normal reads rely on HeyaMetadata stale-while-revalidate. Migration 00031
disables the blind `refresh_stale_items` schedule; the old worker remains a
no-op solely so already-queued pre-cutover River jobs can drain safely.
