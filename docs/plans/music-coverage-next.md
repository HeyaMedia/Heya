# Music coverage — next pass plan

> Archived on 2026-07-13. This plan describes the removed heya.media V1
> adapter and must not be used as an implementation guide. Canonical metadata
> now flows through the generated HeyaMetadata V2 client; see
> `../HeyaMetadata/HEYAMEDIA_V2_MIGRATION.md` and the current architecture and
> pipeline documentation.

Written 2026-05-25 to survive session compaction. Two tasks, each
self-contained. Read CLAUDE.md first, then this, then start.

---

## Task 1: Image quantity caps + remote gap-fill

### Goal

Every music asset slot has a target count. Local files fill it first;
heya.media downloads fill whatever's missing — up to the cap. Caps:

| Slot      | Cap          |
|-----------|--------------|
| poster    | 1            |
| backdrop  | up to 5 unique |
| logo      | 1            |
| banner    | 1            |
| clearart  | 1            |
| disc      | 1            |
| thumb     | 1            |

"Unique" means **URL string dedup** (cheapest layer that catches the
common case where heya.media surfaces the same Discogs URL twice).

When picking a remote poster when local is missing: **largest portrait
image** (`height > width`, then sort by height desc). Artist photos lean
portrait; the largest portrait is the cleanest hero shot.

### Current state

- `internal/worker/music_local_assets.go::detectLocalMusicAssets`
  - Walks the artist folder for `folder.jpg`/`backdrop*.jpg`/`logo.png`/...
  - Already caps singular slots at 1 (first match wins).
  - **Backdrops are uncapped** — pulls `backdrop.jpg` + every numbered
    `backdrop[N].jpg`, no upper bound.
  - Returns `musicLocalAssets{HasPoster, HasBackdrop, HasLogo, HasBanner}`
    — too thin for the new orchestration. Need per-slot **counts**, not
    bools.
- `internal/worker/enrich_worker.go::enrichMusic`
  - Calls `detectLocalMusicAssets`, then for **poster + backdrop only**
    queues a single `DownloadImageArgs` if local missing.
  - **Ignores `detail.ArtistImages`** — the full heya.media artist photo
    list captured into `MediaDetail`. Never queued for download.
  - Other slots (logo/banner/clearart/disc/thumb) have no remote fallback
    wired at all.

### Target behavior

```text
local := detectLocalMusicAssets(...)   // returns per-slot counts
remote := rankRemoteImages(detail)     // dedupe + classify by aspect

# Singular slots
for slot in [poster, logo, banner, clearart, thumb]:
    if local.Count(slot) == 0 and remote.HasCandidate(slot):
        queueDownload(slot, sort=0, url=remote.Best(slot))

# Disc art (we don't pull this from remote — heya.media doesn't have
# it. Local-only for now. Just keep the existing local detection.)

# Backdrops
have := local.BackdropCount
need := 5 - have
queued := 0
for url in remote.Backdrops:  # already dedupe'd by URL string
    if queued >= need: break
    if url in local.UsedURLs: continue   # paranoia
    queueDownload(backdrop, sort=have+queued, url=url)
    queued++
```

### Implementation steps

1. **Restructure local detector return**
   - File: `internal/worker/music_local_assets.go`
   - Change `musicLocalAssets` struct to carry **counts** per slot, not
     bools: `{Poster int, Backdrop int, Logo int, Banner int, Clearart int,
     Thumb int, Disc int}`. Also a `UsedURLs map[string]bool` for the
     dedup paranoia step (cheap to fill — local detector knows what it
     copied from where).
   - Cap local backdrops at 5 inside the detector — the
     `findNumberedExtras` loop currently writes all numbered variants.
     Truncate at 5 (including the primary).
   - Update all consumers (just `enrichMusic` AFAIK).

2. **New helper: rank + classify heya.media images**
   - Add to `internal/worker/music_local_assets.go` (or a new
     `music_remote_images.go`):
     ```go
     type remoteArtistImages struct {
         Poster   string   // largest portrait, "" if none
         Backdrops []string // largest landscapes, dedup'd by URL, ordered by width desc
         Logo     string   // heya.media doesn't have these yet, but plumb for future
         Banner   string
         Clearart string
         Thumb    string
     }

     func rankRemoteArtistImages(detail *metadata.MediaDetail) remoteArtistImages
     ```
   - Algorithm: walk `detail.ArtistImages`, classify by aspect:
     - `Height > Width` (or `aspect == "portrait"`) → poster candidate
     - `Width > Height * 1.2` (wide enough to be a backdrop) → backdrop
     - everything else → fallback poster candidate
   - Sort posters by height desc; sort backdrops by width desc; dedupe
     URLs (first occurrence wins after sort).
   - Top-level `detail.PosterURL` and `detail.BackdropURL` (from
     `resp.Poster` etc) are EXTRA candidates — include them in the input
     pool.

3. **Rewire `enrichMusic` orchestration**
   - File: `internal/worker/enrich_worker.go`
   - Replace the current "if !local.HasPoster ..." block with:
     ```go
     local := detectLocalMusicAssets(...)
     remote := rankRemoteArtistImages(detail)
     queueArtistArtworkGaps(ctx, client, item, local, remote)
     ```
   - `queueArtistArtworkGaps` lives next to the helpers. Walks the slot
     list, enqueues `DownloadImageArgs` with `SortOrder` set per slot.
     Logs at INFO so you can see "local=2, remote=3, queued=3 backdrops"
     after a refresh.

4. **DownloadImageWorker — verify sort_order handling**
   - File: `internal/worker/image_worker.go`
   - It already handles `SortOrder` for the `media_items.{poster,backdrop}_path`
     update (only `SortOrder==0` updates the column). No change needed
     — but **verify** the column update path doesn't break when local
     already wrote `sort_order=0` and a remote `sort_order=1+` tries to
     insert. The unique index is on `(media_item_id, asset_type,
     sort_order, local_path)` so different `sort_order` is fine.

5. **Test live**
   - Pick an artist with sparse local backdrops (e.g. one with only
     `backdrop1.jpg`).
   - `media refresh --id N`, wait for jobs to drain, check:
     ```sql
     SELECT asset_type, source, sort_order, local_path
     FROM media_assets WHERE media_item_id = N
     ORDER BY asset_type, sort_order;
     ```
   - Expect: 1 poster (local), up to 5 backdrops (local first, then
     remote fills), logo/banner/clearart/thumb at most 1 each.

### Gotchas

- **Album cover slot is separate** — it's per-album in `albums.cover_path`,
  not in `media_assets`. The existing `scanAlbumAssets` + `copyAlbumCover`
  +heya URL fallback already does local-first / remote-fill correctly.
  Don't touch it.
- **Disc art** lives at the per-album cache directory, not in
  `media_assets`. Stay scoped to the artist's `media_assets` table.
- The `media_assets` unique index `idx_media_assets_unique` is on
  `(media_item_id, asset_type, sort_order, local_path)`. Local and remote
  for the same slot at the same sort_order would collide if they
  resolved to the same `local_path` — but `DownloadImageWorker` writes
  to `data/images/music/{slug}/{asset_type}{N}.{ext}` and the local
  detector writes to `data/images/music/{slug}/{asset_type}.{ext}` (sort
  0) or `{asset_type}{N}.{ext}` (sort N). Risk: a remote download for
  sort 0 ends up with the same filename as a local sort 0. Local should
  always win; consider deleting the remote `media_assets` row + file when
  local also exists for the same sort_order. The existing single-asset
  cleanup in `writeAsset` already handles sort_order=0 — extend it to
  cover the broader case.
- **Don't touch `local.HasPoster / HasBackdrop / HasLogo / HasBanner`
  consumers in `enrichMusic`** — there's a final log line that reads
  these bools. Update it to read counts.

### Files inventory

| File | Change |
|------|--------|
| `internal/worker/music_local_assets.go` | counts struct, cap backdrops at 5, return `UsedURLs` map |
| `internal/worker/enrich_worker.go` | replace gap-fill logic with orchestrated walk |
| `internal/worker/music_local_assets.go` (or new file) | `rankRemoteArtistImages` + `queueArtistArtworkGaps` |
| `internal/worker/image_worker.go` | verify only — no expected change |

---

## Task 2: Refactor `heya.go` onto the generated client (big bang)

### Goal

Drop the hand-rolled HTTP client + JSON structs in
`internal/metadata/heyamedia/heya.go`. Use `clients/heyamedia/client.gen.go`
for all upstream calls. Keep the `HeyaProvider` public API stable so
matcher/worker callers don't change.

User confirmed **big bang** — one pass, not phased.

### Current state inventory

- `internal/metadata/heyamedia/heya.go` (~1300 lines):
  - Hand-rolled HTTP `client` with retry, exponential backoff, in-memory
    cache.
  - ~30 hand-rolled JSON structs: `heyaItemResponse`, `heyaPayload`,
    `heyaArtwork`, `heyaArtworkEntry`, `heyaSeasonEntry`,
    `heyaAlbumEntry`, `heyaAlbumTrackEntry`, `heyaArtistURL`,
    `heyaArtistRelation`, `heyaTopTrack`, `heyaSimilarArtist`,
    `heyaArtistCredit`, `heyaCastEntry`, `heyaCrewEntry`, `heyaVideo`,
    `heyaStudio`, `flexIDs`, `heyaRating`, `heyaCR`, `heyaTitleEntry`,
    `heyaKeyword`, `heyaCreator`, `heyaNetwork`, `heyaRecEntry`,
    `HeyaArtworkItem`, plus helper structs in `convertProfileURLs` etc.
  - `mapDetail(resp *heyaItemResponse) *metadata.MediaDetail` — the
    monster converter, ~340 lines covering movie/TV/artist branches.
  - `mapHeyaAlbums`, `mapHeyaAlbumTracks`, `mapHeyaArtistCredits`,
    `mapHeyaRelations` — submap helpers.
  - Public `HeyaProvider` methods: `Search`, `SearchArtistBest`,
    `LookupByNFO`, `FetchByKindID`, `SimilarArtists`, `GetDetail`,
    `lookupByExternalIDs`, `searchHits`.
  - Internal: `fetchKindID`, `getJSON`, `get` (raw HTTP), client config.

- `clients/heyamedia/client.gen.go` (~80KB):
  - `ClientInterface` + concrete `Client` with `Get*WithResponse` methods
    for every endpoint.
  - Full typed response structs: `ArtistDocBody`, `MovieDocBody`,
    `TvDocBody`, `BookDocBody`, `PersonDocBody`, `SearchOutputBody`,
    `SimilarArtistResponse`, etc.
  - Nested types: `ArtistDetail`, `Album`, `Track`, `ArtworkItem`,
    `TopTrack`, `SimilarArtist`, `ArtistMember`, `ArtistURL`, `ArtworkResult`,
    `ArtistCredit`, `Cast`, `Crew`, `Video`, etc.
  - Pointer-everywhere style: nullable fields are `*string`, `*int64`,
    `*[]string`, `*map[string]string`. Need nil guards everywhere.

### Target state

`heya.go` becomes ~300 lines:
- A `HeyaProvider` struct wrapping `heyamediaclient.ClientWithResponses`.
- Each public method delegates to the generated client, then maps the
  typed response to `metadata.MediaDetail` via a new `mapDetail` that
  takes the generated types as input.
- Retry/backoff + in-memory cache: a custom `http.RoundTripper` wrapper
  passed to the generated client via `WithHTTPClient` (or whatever
  oapi-codegen named the option).
- All hand-rolled structs deleted.

### Implementation steps

1. **Set up the generated client**
   - In `heya.go` (or new `client.go`), construct a
     `heyamediaclient.NewClientWithResponses(baseURL, opts...)`.
   - Build a custom `http.Client` with the retry transport stack:
     ```go
     transport := &retryTransport{
         next: &cachingTransport{
             next: http.DefaultTransport,
             cache: lru.New(256),
         },
         maxAttempts: 3,
         baseDelay:   time.Second,
     }
     httpClient := &http.Client{Transport: transport, Timeout: 5*time.Minute}
     ```
   - Pass via `heyamediaclient.WithHTTPClient(httpClient)`.

2. **Migrate `FetchByKindID`**
   - Current shape: `func FetchByKindID(ctx, kind, id) (*metadata.MediaDetail, providerID string, err)`
   - New body:
     ```go
     switch kind {
     case "artist":
         resp, err := p.gen.GetArtistByIdWithResponse(ctx, id, nil)
         if err != nil { return nil, "", err }
         if resp.JSON200 == nil { return nil, "", upstreamErr(resp) }
         return mapArtistDoc(resp.JSON200), providerID(kind, id), nil
     case "movie": ...
     case "tv": ...
     case "book": ...
     }
     ```
   - New `mapArtistDoc(body *heyamediaclient.ArtistDocBody) *metadata.MediaDetail`
     replaces the artist branch of the old mapDetail.

3. **Migrate `Search` + `SearchArtistBest`**
   - `GetSearchWithResponse(ctx, &heyamediaclient.GetSearchParams{Q: q, Type: ...})`
   - Map `SearchOutputBody.Results` to local `SearchHit` (or just expose
     the generated type directly — caller is in the matcher, which
     consumes `Score`, `Enriched`, `Image`, `ExternalIDs`, etc.).
   - `SearchArtistBest` stays the same logic (search → pick highest
     score) but reads the typed results.

4. **Migrate `SimilarArtists`**
   - `GetSimilarArtistWithResponse(ctx, params)` returns
     `*SimilarArtistResponse`. Map to local type or pass through.

5. **Migrate `LookupByNFO` + `lookupByExternalIDs`**
   - These iterate provider IDs and call `fetchKindID`. After step 2
     `fetchKindID` is gone — replace with `FetchByKindID` directly (or
     `p.gen.GetArtistById*` etc).

6. **Write the new `mapDetail` family**
   - `mapArtistDoc(*heyamediaclient.ArtistDocBody) *metadata.MediaDetail`
   - `mapMovieDoc(*heyamediaclient.MovieDocBody) *metadata.MediaDetail`
   - `mapTvDoc(*heyamediaclient.TvDocBody) *metadata.MediaDetail`
   - `mapBookDoc(*heyamediaclient.BookDocBody) *metadata.MediaDetail`
   - All the per-branch logic that was in the monolithic `mapDetail`
     goes here. The output `*metadata.MediaDetail` stays the same shape
     so callers don't change.
   - Helpers for the `*string`/`*int64`/`*[]T` pointer dance:
     ```go
     func strPtr(p *string) string {
         if p == nil { return "" }
         return *p
     }
     func intPtr64(p *int64) int64 {
         if p == nil { return 0 }
         return *p
     }
     func strs(p *[]string) []string {
         if p == nil { return nil }
         return *p
     }
     // etc.
     ```
   - Put helpers in a separate `pointers.go` next to `heya.go`.

7. **Delete hand-rolled structs**
   - After all maps + calls are migrated, every reference to
     `heyaItemResponse`, `heyaPayload`, `heyaArtwork`, etc. should be
     gone. Run `grep -rn "heyaPayload\|heyaItemResponse\|heyaArtwork" .`
     to confirm. Delete the type definitions.
   - Also delete `convertProfileURLs`, `flexIDs`, etc. The generated
     client's `*map[string]string` for ExternalIDs supersedes
     `flexIDs` (which handled the int-vs-string coercion).
     **Verification needed**: the live heya.media response — does it
     still emit int IDs (`tmdb: 1234`) or string IDs (`tmdb: "1234"`)?
     If int, the generated client will JSON-decode error. Check the
     OpenAPI spec: `ExternalIDsDTO.tmdb` etc. — if they're typed as
     `string`, we're fine. If `integer`, we need a custom unmarshal step
     (or a JSON `Number` field).

8. **Delete the hand-rolled HTTP layer**
   - `getJSON`, `get`, `searchHits` raw-HTTP body, retry loops — all
     subsumed by the generated client + retry transport.

9. **Build, test, ship**
   - `go build ./...` after each step (or use a feature branch).
   - Run the existing matcher tests (`internal/matcher/probe_*_test.go`)
     — they hit live heya.media so they validate the end-to-end shape.
   - Force-refresh a music + a movie + a TV item: verify
     `metadata.MediaDetail` populated correctly downstream.

### Files inventory

| File | Action |
|------|--------|
| `internal/metadata/heyamedia/heya.go` | rewrite (~300 lines, was ~1300) |
| `internal/metadata/heyamedia/pointers.go` | NEW — helper extractors |
| `internal/metadata/heyamedia/retry_transport.go` | NEW — retry + cache RoundTripper |
| `internal/metadata/heyamedia/heya_test.go` (if exists) | adjust |
| `internal/matcher/probe_*_test.go` | should pass unchanged |

### Gotchas

- **Pointer-everywhere**: every field access in the generated types
  needs a nil check. The helper extractors handle most of it, but watch
  for `*[]X` where the slice itself can be nil even when the field is
  present (`"genres": null`).
- **Time fields**: `ArtistDocBody.EnrichedAt` is `time.Time` (not `*time.Time`).
  Other timestamps may be pointers. Don't assume one or the other.
- **ID coercion** (`flexIDs`): the hand-rolled code accepted both string
  and integer IDs on `external_ids` because Discogs/Apple/Deezer used to
  mix shapes. Generated client uses `*map[string]string` — if heya.media
  emits ints, decode fails. Test against live API after migration. If
  this breaks, the cleanest fix is to write a custom UnmarshalJSON for
  the response type (or open an issue upstream to fix the spec).
- **Endpoint naming**: oapi-codegen names operations from the operation
  IDs in the spec. Skim `client.gen.go` for the actual function names —
  it may be `GetApiV1ArtistId` or `GetArtistById` depending on how the
  spec tags them. Cross-reference with the spec's `operationId`.
- **Tests against live API** — keep them as integration tests that
  skip when heya.media is unreachable (existing pattern in
  `probe_test.go`). Add a `TestMapArtistDocFromFixture` test that
  decodes the committed `clients/heyamedia/openapi.json` example
  response (or a saved JSON sample) — that one runs offline.

### Out of scope (don't do in this pass)

- Refactoring `internal/matcher/music_refresh.go` to consume generated
  types directly. Keep the matcher consuming `metadata.MediaDetail` so
  the migration stays inside `heyamedia/`.
- Switching to the generated TS client on the frontend. The FE doesn't
  call heya.media directly (everything proxies through Heya's API).
- Adding new endpoints to heya.media. Spec is frozen for this task.

---

## Session restart prompt

To resume work from this plan, paste this into a fresh session:

> Continue from `docs/plans/music-coverage-next.md`. Start with Task 1
> (image caps + remote gap-fill); ship that fully, then Task 2 (big-bang
> refactor of `internal/metadata/heyamedia/heya.go` onto
> `clients/heyamedia/client.gen.go`). Don't touch the matcher's
> consumption of `metadata.MediaDetail` — the contract stays stable.
