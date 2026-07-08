# Local-First Ingest — Implementation Plan (v2)

Status: revised after code-level verification. Not started. Working notes /
decisions also captured in Claude memory `project_local_first_ingest`.

> **v2 changes in one line:** the lifecycle is the *existing* `enrichment_status`
> (no new parallel enum); the hard work is making the enrich writers idempotent
> upserts that honor provenance, not the metadata editor; and several
> "we'll just materialize the chain" assumptions are blocked by real
> insert-only / dedup / probe-ordering defects that must be fixed first.

## Goal

Flip ingest from **match-then-create** to **create-from-local-signal, then
enrich**. Every scanned media file becomes a *visible entity immediately* from
the best local signal (NFO → embedded tags → filename parse), with local sidecar
assets (images/lyrics/subs) attached. Upstream metadata becomes an enrichment
pass layered on top, not a precondition for visibility.

### Why (today's behavior)

- A file that doesn't match remotely leaves only a `library_files` row — no
  `media_items`, no type-specific row. The library lists via INNER JOINs on
  `movies` / `tv_series` / `artists`, so unmatched/un-enriched items are
  invisible. Materializing the type-specific row in Phase 1 is what makes them
  appear; the badge is then a predicate over lifecycle state.
- "Has metadata" (enriched) and "has been ffprobed" (`media_info`) are already
  decoupled — the on-demand probe (`App.EnsureFileProbed`) handles the latter.
  This plan handles the former: making local identity enough to appear.

## Decisions (locked 2026-06-30)

1. **All media types** (movies, TV, music) in scope.
2. **Reconcile model: upgrade-in-place + slug-lock + edits-win.** One stable
   `media_items.id`/slug for an entity's life. Remote enrichment fills gaps;
   the slug locks at first publish; user edits are never clobbered by re-enrich.
3. **Enrichment unit = artist / TV series / movie.** Upstream only fetches these
   and returns children bundled (series→episodes, artist→albums→tracks). So
   enrichment reconciles bundled remote children against locally-materialized
   ones: link what we have files for, mark the rest *missing* (not deleted).

## Data model / provenance (the keystone)

**Reuse the existing `enrichment_status` lifecycle — do NOT add a parallel
`metadata_state` enum.** `enrichment_status` (migration `00017`,
`models.go:536-543`) is already the single source of truth: written by
`MarkEnrichComplete/Failed/Partial`/backfill, and read by the enrich-worker
idempotency gate (`enrich_worker.go:55`), the matcher
(`matcher.go:298`), view-promotion re-queue (`media.go:127`), the tasks
dashboard (`tasks.go:149-150,476-477,490-508`), and the API/FE
(`api.gen.ts:6241,6278`). A second lifecycle column would dual-write the exact
axis that drives idempotency, dashboard counts, and listing visibility, and
would drift. The plan's only genuinely new axes are *origin* and *provenance*,
captured below.

New / reused columns on `media_items` (numbered migration; wipe-and-rescan is
fine — see `feedback_no_backwards_compat`):

| Column | Status | Purpose |
| --- | --- | --- |
| `enrichment_status` | **reuse + 1 new value** | Lifecycle. Add `'local'` for "born from local signal, never confidently matched." Keep `pending` (matched stub awaiting enrich) / `partial` / `complete` / `failed`. |
| `provider_kind` | **reuse** | Origin axis. Set `'local'` / `'nfo'` for locally-created entities. This — not a lifecycle column — is what distinguishes a pure-local entity from a matched stub. |
| `field_provenance jsonb` | **new** | Per-field source map `{field: local\|remote\|user}`. Enrich writers overwrite only `local`/empty fields, never `user`. |
| `match_confidence real` | **new** | Search-stub fast-path score; `0` for pure-local. |
| ~~`local_identity_key text`~~ | **superseded (mig 00044)** | Originally a stored dedup key for NFO-less locals. Removed: dedup now keys on **natural identity** — `lower(btrim(title))\|year\|media_type` computed at query time (`FindMediaItemByIdentity`, backed by `idx_media_items_identity`), which covers enriched rows too and needs no column to maintain. |
| `slug_locked bool` | **new** | Set true at first publish; re-enrich never changes slug. |

Lifecycle mapping (so consumers stay authoritative):

- Phase-1 materialized, no confident match → `enrichment_status='local'`,
  `provider_kind ∈ {local, nfo}`.
- Search-stub with a confident external id, not yet fully fetched →
  `enrichment_status='pending'` (existing semantics).
- The transient **"searching"** state is **not persisted** — derive it from an
  active River enrich job, or `matched_at IS NOT NULL AND
  enrichment_status='pending'`. A persisted "searching" value would drift.
- Update predicates to treat `'local'` like `'pending'` where they mean
  "not yet enriched": matcher (`matcher.go:298`), worker gate
  (`enrich_worker.go:55`), view-promotion (`media.go:127`), and the tasks
  dashboard counts/labels (`tasks.go`).

**Edits-win is enforced at the enrich writers, not the editor.** The editor is
only the *producer* of `user` provenance; the parties that actually overwrite
fields are the enrich paths (see Phase 2). The metadata editor
(`metadata_editor.go:60-221`) gets the net-new job of stamping
`field_provenance[field]='user'` for every field it patches (and a music branch,
which it currently lacks).

The "missing" concept already exists for music
(`ListMissingMedia`/`missing_count`, see `reference_music_missing_cleanup`) and
extends to TV via the read-time episode map (see Phase 1, TV note) — **not** via
a new `library_files.episode_id` column.

## Critical code-level prerequisites (must land with Phase 0/1)

These are verified defects that silently break "materialize then upgrade-in-place."
None are optional.

1. **Movie/TV type rows are insert-only — make them upserts.**
   `CreateMovie`/`CreateTVSeries` are `INSERT … ON CONFLICT (media_item_id) DO
   NOTHING RETURNING *` (`queries/movies.sql:5`, `queries/tv.sql:6`). As `:one`,
   the suppressed RETURNING yields `pgx.ErrNoRows`; `createTVSeries`/`createMovie`
   then early-return (`persistence.go:111-113,421-423`) and the error is swallowed
   at `persistence.go:654` (`_ =`). Today this is masked because enrich does the
   *first* insert. Local-first inverts that, so Phase-2 `StoreEntityMetadata`
   hits the conflict and writes **nothing** to the movie/`tv_series` columns (and
   for TV, the entire season/episode tree built inside `createTVSeries`). Fix:
   convert to `ON CONFLICT (media_item_id) DO UPDATE` with field-level
   preserve-non-empty *and* a provenance guard (skip `user` fields), or fall back
   to `UpdateMovie`/`UpdateTVSeries` on `ErrNoRows`. Mirror
   `queries/music.sql` `UpdateArtistEnrichedFields` CASE-WHEN style. **Stop
   swallowing the error at `persistence.go:654` — at minimum log it.**
   (Cast/crew/keywords are *not* affected: `StoreRichMetadata`,
   `enrich_worker.go:130`, is a separate call.)

2. **TV season/episode children are plain inserts — make them upserts.**
   `CreateTVSeason`/`CreateTVEpisode` (`queries/tv.sql:22-25,39-42`) have no
   `ON CONFLICT`; collisions against `UNIQUE(series_id,season_number)` /
   `UNIQUE(season_id,episode_number)` are caught with `log.Warn()+continue`
   (`persistence.go:452-455,481-484`), so re-enrich never updates existing
   rows and silently drops upstream titles/overviews/stills/air-dates. There is
   **no** `UpdateTVSeason` query at all today — this is net-new SQL. Add
   `UpsertTVSeason` (`ON CONFLICT (series_id,season_number) DO UPDATE`) and
   `UpsertTVEpisode` (`ON CONFLICT (season_id,episode_number) DO UPDATE`), fill
   only empty/`local` fields, honor the provenance lock. Also bring the
   metadata-editor refresh path (`metadata_editor.go:541`) — which delete-recreates
   rich metadata but skips seasons/episodes entirely — into the same reconcile
   model. This is what makes the existing TV-debounce "pull new seasons/episodes"
   claim (`matcher.go:274-283`, currently dead) actually work.

3. **Empty-`external_ids` containment dedup mislinks NFO-less files.**
   `GetMediaItemByExternalID` is `WHERE library_id=$1 AND external_ids @> $2`
   with no `ORDER BY` (`queries/library_files.sql:95-97`); `'{}'::jsonb @> '{}'`
   matches **every** object row, so a pure-local stub (empty `external_ids`,
   `matcher.go:331-351`) links onto an arbitrary existing `media_item`. The
   filename-only branch currently early-returns on no provider ID
   (`matcher.go:444-448`); the plan removes that guard, routing empty-ID stubs
   straight into the broken lookup (the retry branch at `persistence.go:49-56`
   re-runs the same `{}` lookup). Fix in `createOrLinkMediaItem`:
   - If marshaled `external_ids` is `"{}"`/`"null"`/`len==0`, **skip**
     `GetMediaItemByExternalID` entirely (and the retry lookup) and go straight
     to `CreateMediaItem`.
   - Dedup NFO-less locals on natural identity (`FindMediaItemByIdentity`,
     normalized `title|year|media_type`) instead of containment.
   - Harden the SQL itself: `AND $2::jsonb <> '{}'::jsonb` + explicit `ORDER BY`
     so a future caller can't reintroduce the mislink.

4. **Music identity needs probe data.** The current scanner calls the injected
   synchronous probe path (`worker.ProbeFile`) during music analysis when tags
   are needed, so artist/album/track identity can use embedded tags without
   waiting for a separate ffprobe worker. If we later split metadata fetching
   further, keep that property: local music identity must not depend on an
   unordered background probe finishing first. (Acceptable alternatives:
   audio-scoped reorder where an ffprobe worker enqueues music scanner work on
   success, or scanner River-snooze/retry when `media_info` is empty — but the
   synchronous local probe is lowest-churn and matches the plan's
   "synchronous-local signal" framing.)

## Pipeline phases

- **Phase 0 — Foundations.** The migration above (`field_provenance`,
  `match_confidence`, `slug_locked`, `enrichment_status
  += 'local'`; the once-planned `local_identity_key` was later dropped for
  natural-identity dedup, mig 00044) + a `field_provenance` read/write helper + the **upsert
  prerequisites #1 and #2** (movie/TV/series/season/episode get-or-upsert SQL).
  No ingest-order change yet; this is the substrate Phase 2 needs.
- **Phase 1 — Local extraction.** A "local identity resolver": per file/dir,
  produce a canonical entity descriptor from NFO + embedded tags + filename.
  - Extract **filename-embedded provider IDs** — `{imdb-tt\d+}`,
    `{tmdb-\d+}`, `{tvdb-\d+}` plus Jellyfin/Kodi `[tmdbid=…]`/`[imdbid=…]` — in
    the video parser. These are **not captured anywhere today** (zero matches in
    `internal/parser/`); surface them as new ID fields on
    `SceneReleaseParse`/`ParsedStorageEntry` and thread them through
    `scanner.go`'s `parseData` wrapper and `matcher.go` `parseFileResult` so the
    strong-ID path (`matcher.go:108` → `tryNFOLookup`) treats them like
    NFO-derived IDs. Until this lands, filename IDs are **not** available signal —
    only NFO-sidecar IDs are wired.
  - Materialize per type, marked `enrichment_status='local'`, `provider_kind ∈
    {local,nfo}`, with empty-identity dedup short-circuit (#3); re-scan/collision
    dedup is by natural identity (`FindMediaItemByIdentity`). Attach local assets
    to `media_assets`.
    - **Movies:** the full `media_items` + `movies` row (the NFO-with-IDs stub
      path `stubDetailFromNFO` generalizes to NFO-without-IDs and filename-only).
    - **TV:** materialize the **series `media_items` row only** — do **not**
      create local `tv_seasons`/`tv_episodes`. There is no durable file↔episode
      link in the schema (`library_files.media_item_id` points at the *series*);
      file→episode association is derived at read time via `BuildEpisodeFileMap`
      keyed on `s{n}e{m}` (`media.go:814-841`, `episode_watch.go:195-200`).
      Episodes stay remote-enriched rows; reconciliation (Phase 2) operates over
      that read-time map. (This is the verified-correct grain; minting local
      episode rows collides with the non-idempotent enrich writer and has no
      column to persist the file link.)
    - **Music:** the full `artist→album→track→track_file` chain (it has real
      per-file FKs — `tracks.library_file_id`, `track_files`, `00010`). See the
      track-collapse guards below.
- **Phase 2 — Enrichment + reconciliation, with edits-win enforced at the
  writers.** Queue an upstream fetch per local entity. Confident remote hit →
  upgrade-in-place via the Phase-0 upserts (fill `local`/empty fields, add remote
  assets, `enrichment_status='complete'`, keep id + locked slug).
  - **Net-new base-field writes (movies/TV):** `enrichGeneric` writes **no**
    base `media_items` fields today (`enrich_worker.go:74-186` only calls
    `StoreEntityMetadata`/`StoreRichMetadata`/slug/timestamps). Add UPDATE logic
    that fills only `local`/empty `media_items.title/year/description` and never
    `user`-locked fields.
  - **Highest-risk site — music re-enrich is remote-wins:**
    `UpdateArtistEnrichedFields`/`UpdateAlbumEnrichedFields`/`UpdateTrackFromEnrichment`
    (`queries/music.sql:32-65`) are `name = CASE WHEN $3 != '' THEN $3 ELSE name
    END` — they clobber user-renamed artists/albums/tracks on every re-enrich, a
    direct edits-win violation and a silent data-loss class. Rework them (and the
    per-row change detection in `RefreshMusicArtist`) to skip `user`-provenance
    fields. `UpdateMediaItemExternalIds` is merge-not-overwrite (lower risk) but
    should still honor locks.
  - **Reconcile bundled children: link-or-mark-missing.**
    - TV: reconcile upstream `MediaDetail.Seasons` against the read-time
      `s{n}e{m}` file map — mark remote episodes available/missing — using the
      Phase-0 `UpsertTVSeason`/`UpsertTVEpisode`. (The "missing" delete-on-disappear
      machinery is *not* reused for remote-bundled episodes.)
    - Music: link tracks we have files for, mark the rest missing.
  - **Manual-edit guard, not just preserve-non-empty.** A `DO UPDATE` that copies
    a *different* non-empty upstream value will overwrite a user edit. The
    provenance/`slug_locked` check is what protects edits — COALESCE-on-non-empty
    alone trades the silent no-op bug for a silent overwrite-edits bug.
  - No-NFO/low-confidence stays `enrichment_status='local'`, surfaced in
    "needs review".
  - **Search-stub fast path (no NFO):** search heya.media → confident hit
    attaches the external id + initial fields right away (`enrichment_status='pending'`,
    `match_confidence` set) → full fetch queued behind it.
- **Phase 3 — Listing + UI.** Visibility is a predicate over the existing
  fields, **not** a new enum: type-row materialization (Phase 1) satisfies the
  library INNER JOINs, and the badge is `enrichment_status != 'complete'`.
  Settings toggle "show items pending metadata" (default on). Fix the FE
  foot-guns (the `year` → `"null"` subtitle at `movies/index.vue:67` and
  `tv/index.vue:67`; relax the `EnrichedMediaView` TS type to carry
  `enrichment_status`/`provider_kind`).
- **Phase 4 — Merge / slug.** When enrichment reveals two local stubs are one
  entity, merge (reuse `music_merge` logic). Slug lock-on-publish + a manual
  "fix slug" action.

## Per-type extraction notes

- **Movies/TV:** NFO-with-IDs already stubs a `media_items` row
  (`matcher.go:436-482`, `stubDetailFromNFO`) — generalize to NFO-without-IDs and
  filename-only. Filename provider IDs are a Phase-1 parser task (above), not
  existing signal. TV episodes are remote rows reconciled over the read-time
  file map; do not mint local episode rows.
- **Music:** a music `media_item` IS the artist; materialize the whole
  `artist→album→track→track_file` chain (all NOT NULL title/number/FKs) from the
  probed tags (after #4) + folder structure + `*.nfo`. Two collapse guards are
  mandatory, both at **Phase-1 materialization** (irreversible later):
  1. **Track-number collapse.** The matcher passes `track_number=0` for every
     filename-unparseable file (`music.go:327-339`), and `GetOrCreateTrack` is
     `ON CONFLICT (album_id,disc_number,track_number) DO UPDATE`
     (`queries/music.sql:256-265`) — so all such files dedupe onto one
     `(album, disc 1, track 0)` row while keeping distinct `track_files`,
     destroying song identity before enrich. Parse `TRCK`/`track`/`tracknumber`
     and `TPOS`/`disc` from `media_info.format.tags`, **split the `N/total` form
     before `atoi`** (`"5/12"` → 5, not 0), precedence **NFO > embedded tag >
     filename**. When no number is resolvable, assign **sequential synthetic
     per-release-directory numbers** (stable on enumeration order) so distinct
     files never share a key.
  2. **"Unknown" fusion.** `artists` has **no `library_id`**; its uniqueness is
     global (`uq_artists_name_disambig`, `00003:22`) — contrast media_items,
     whose identity is library-scoped. A literal shared "Unknown
     Artist"/"Unknown Album" therefore fuses *every* low-info file across *all*
     libraries into one artist→album→track (data loss), not just hides them.
     **Do not funnel.** Default to current behavior: empty artist/album →
     leave as plain `library_files` marked unmatched / "needs review", no
     artist/album row created (`music.go:223-228,292-298`). If a visible bucket
     is wanted, use **per-directory bucketing** (release-dir folder as album,
     parent dir as artist) so distinct folders stay distinct; reserve a literal
     "Unknown" only for truly directoryless flat files. The guard belongs in
     Phase 1, not Phase 3 (a UI filter can't undo upsert-time fusion). Document
     the residual cross-library collision risk on identical folder names (e.g.
     two "Various Artists" folders) given global artist uniqueness; scope artist
     uniqueness by `library_id` only if that isolation matters.

## Test strategy

- **L1 — DB-less unit** (extend `testdata/parser/*`, `internal/nfo`, + new tag
  fixtures): pure "local signal → entity descriptor". The bulk of new logic.
  Must include: a tag-only / filename-unparseable folder asserting **N distinct
  tracks** materialize; a `TRCK="N/total"` fixture; filename provider-ID
  extraction (`{imdb-tt…}`, `[tmdbid=…]`).
- **L2 — transaction-rollback integration** (`tx,_:=pool.Begin; defer
  tx.Rollback; sqlc.New(pool).WithTx(tx)`): materialize + enrich-upgrade +
  reconcile + merge, persisting nothing. Must include the regression guards:
  - Two distinct NFO-less movies (empty `external_ids`, different titles) →
    **two** `media_items.id` (`IsNew=true` both), not one shared row.
  - Edit a track/album/TV-episode title, then re-enrich → the **edit survives**
    (provenance lock), and a *new* upstream episode **appears** (idempotent TV
    upsert + reconcile).
- **L3 — real e2e** against `fulldata/` (now seeded): local-active dev server
  (`heya_dev`), scan, eyeball via Heya Eye. Sample set: 3 Doors Down + Ado
  (CJK slug test) + 3 Body Problem (S01E01–02) + A Goofy Movie.

## Resolved questions

- **Field-provenance granularity →** per-field `jsonb` map
  (`field_provenance: {field: local|remote|user}`). Required, not optional: the
  enrich writers (movie/TV upserts, the three music `Update*EnrichedFields`)
  must check provenance per field to skip `user` values, which a coarse
  `locked_fields []text` could also do — but the jsonb map also records the
  positive `local`/`remote` source needed to decide "fill only empty/local," so
  it is the right grain.
- **Slug stability with a wrong local title →** manual "fix slug" is sufficient.
  Slug locks (`slug_locked=true`) at first publish (first `complete`, or any
  user edit), and re-enrich never changes it.
- **"Unknown Artist" UX →** resolved: no shared literal bucket (it fuses
  globally). Keep sub-threshold files as unmatched `library_files` in "needs
  review"; optional per-directory bucketing if a visible grouping is wanted.
- **File-set vs remote tracklist numbering disagreement →** resolved by the
  Phase-1 synthetic per-directory numbering (no `(album,disc,track)` collisions)
  plus Phase-2 link-or-mark-missing reconciliation; mark surplus remote tracks
  missing rather than collapsing local files.

## Explicitly out of scope / rejected

- Adding a parallel `metadata_state` enum (duplicates `enrichment_status`).
- Adding a `library_files.episode_id` / `episode_files` join table as a Phase-1
  prerequisite — TV file↔episode association stays the read-time `s{n}e{m}` map;
  the "missing" delete-on-disappear logic is not applied to remote-bundled
  episodes.
