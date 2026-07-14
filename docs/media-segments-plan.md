# Media segment architecture

Status: implemented for the HeyaMetadata V2 cutover (2026-07-13).

Community skip segments are Heya media-server behavior. They are deliberately
not fetched from HeyaMetadata and must not be added to its canonical metadata
API. Heya owns the direct TheIntroDB, SkipMeDB, and AniSkip clients, their
runtime-aware cache, candidate normalization, precedence, and local fallback.

## Lookup flow

`scan_media_segments_file` loads the local file/runtime and the media item's
provider evidence, then calls `internal/communitysegments.Service`:

- TheIntroDB uses movie/show provider IDs and optional
  `HEYA_THEINTRODB_API_KEY`;
- SkipMeDB receives the provider identity plus runtime;
- AniSkip uses a per-season MAL identity, episode number, and runtime. For
  anime addressed as TVDB series/season or AniDB, Heya lazily loads the weekly
  Fribb `anime-list-mini.json` bridge. Split-cour offsets select the correct
  MAL/AniList entry and renumber the episode within that entry; ordinary TV
  lookups never load this dump.

Providers are queried independently. One provider failing does not hide valid
candidates from another. If every attempted provider fails, the worker returns
a retryable error instead of caching a false global miss.

All responses normalize into `{source, type, start, end, confidence}`
candidates. The existing segment picker applies source precedence and safety
rules before rows reach `media_segments`.

## Runtime-aware cache

`community_segment_cache` is keyed by normalized media identity, episode
coordinates, and runtime rounded to one second. Entries are per source, not a
single aggregate blob, so an unhealthy provider can expire independently.
The anime ID bridge has its own persisted
`community_segment_anime_map_cache`, refreshed weekly and served stale if the
mapping host is temporarily unavailable.

Current horizons:

- successful hit: 30 days;
- successful empty result: 7 days;
- provider error: 1 hour;
- stale successful data may be served while a refresh fails.

Including runtime is required because SkipMeDB and AniSkip use duration to
select or fuzz-match candidate timings. A remux/cut with a materially different
runtime must not reuse timings from another edition.

## Local detection and precedence

Community lookup remains the fast path. When it yields no usable segment,
Heya's local detection workers may add black-frame or cross-episode audio
evidence. User-authored segments outrank community and detected values; a lower
priority source must not overwrite a stronger accepted row.

Community data is read-only in this cutover. Contribution/upload is not
implemented and would require an explicit provider/auth/product decision.

## Operational behavior

- Segment work uses its own River queues and can run independently of
  HeyaMetadata readiness.
- Disabling/stopping the old metadata service does not affect segment lookup.
- Provider URLs, response bodies, and keys are not written to logs.
- Tests under `internal/communitysegments` cover aggregation, runtime
  forwarding, direct MAL lookup, TVDB split-cour mapping, the ordinary-TV
  no-download gate, cache horizons, stale behavior, and all-provider failure.
