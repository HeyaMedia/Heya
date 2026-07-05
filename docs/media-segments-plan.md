# Media segments — intro / credit markers: research & plan

Research synthesis (2026-07-05) on adding Plex/Jellyfin-style skip-intro and
skip-credits markers to Heya: how the incumbents do it, what online pre-filled
sources exist, what Heya already has to build on, and the recommended split
between the Heya server and the heya.media aggregator.

**TL;DR recommendation**: do both, split by role — exactly the model Plex
ships and the Jellyfin community converged on independently:

1. **heya.media becomes the pre-filled fast path.** It already proxies
   TMDB/TVDB/etc; aggregating the community segment DBs (TheIntroDB,
   SkipMe.db, AniSkip) behind one endpoint is the same design principle.
   One aggregator caching upstream is also far politer to these small
   community projects than N Heya instances hitting them directly.
2. **The Heya server does local detection as the guaranteed-coverage
   fallback** (chromaprint cross-episode intro matching + black-frame
   credits detection). This is the only source we fully control, and it
   needs file access anyway. Nearly all the infrastructure already exists
   (see below).
3. Later: local detections get **contributed back** to heya.media
   (hash-keyed, anonymous), building a Heya-community marker corpus — the
   same plan already sketched for chromaprint submission
   (`migrations/00039_track_file_chromaprint.sql`).

---

## What the incumbents do

### Jellyfin — the architecture to steal

Jellyfin 10.10+ has a first-class **Media Segments** system. Core server owns
only the storage + API contract; all detection lives in plugins.

**Data model** (`MediaSegmentDto`): `{Id, ItemId, Type, StartTicks, EndTicks}`
plus a `SegmentProviderId` column in the DB row. Types:
`Unknown | Commercial | Preview | Recap | Outro | Intro` (no separate
"Credits" — credits are `Outro`).

**Provider interface** (`IMediaSegmentProvider`): `Name`, `Supports(item)`,
`GetMediaSegments(request)` (request carries the provider's own prior rows for
diffing), `Cleanup(itemId)`. Providers are per-library disable-able and
orderable. A scheduled "Media segment scan" runs every 12h over all local
episodes/movies; plugins that want immediacy push directly via a refresher.

**Client contract** (worth copying wholesale):
- One endpoint: `GET /MediaSegments/{itemId}?includeSegmentTypes=...` →
  `{Items: [...]}` sorted by start.
- A cheap `HasSegments` boolean on the playback-info payload so clients skip
  the fetch for unmarked items.
- Per-user, per-type action setting: `None | AskToSkip | Skip`. Defaults:
  Intro and Outro → `AskToSkip`, everything else `None`.
- Guard rails: segments < 1s never auto-skip, < 3s never prompt; a manual
  backward seek into a segment suppresses re-prompting; skip button
  auto-hides after ~8s.

**Intro Skipper plugin** (the flagship detector, GPL-3.0) — algorithm details,
all portable to Go with zero external libraries:

- **Fingerprint**: `ffmpeg -ss <start> -i <path> -to <dur> -ac 2 -f chromaprint
  -fp_format raw -` → stream of little-endian uint32 points, ~0.124 s/point
  (8.07 points/sec). Intro window = first `min(25% of runtime, 10 min)`;
  credits window symmetric from the end.
- **Cross-episode match** (per season, needs ≥2 episodes): build an inverted
  index `point → position` per episode; probe neighbor episodes at
  `point ± 2` to collect candidate alignment shifts; for each shift walk both
  arrays XOR-ing point pairs and popcounting — Hamming distance ≤ 6 of
  32 bits counts as similar; merge similar timestamps into contiguous ranges
  tolerating gaps ≤ 3.5 s; longest shared region = intro (earliest short
  region = recap). Region starting ≤5 s from t=0 snaps to 0.
- **Credits**: layered cheap-to-expensive chain, each analyzer only handling
  what the previous left unresolved:
  1. Chapter-name regexes (zero decode).
  2. Cross-episode chromaprint from the tail (TV only; movies have nothing to
     compare against).
  3. Black-frame binary search backward from EOF
     (`-vf blackframe=amount=50:threshold=28`, ≥85% black) narrowing to a
     ±4 s bound; alternative keyframe-density scan (`-skip_frame nokey`) with
     self-calibrating threshold; entropy/saturation fallback
     (`entropy,signalstats`) for non-black text-on-card credits.
- **Caching**: raw fingerprints/black-frame artifacts cached per
  `(item, mode, type, window)` with a **config-hash** so a settings tweak
  invalidates only affected entries — re-comparison without re-decoding.
- Key defaults: intro 15–120 s, TV credits 15–450 s, movie credits ≤ 900 s,
  parallelism 2, ffmpeg at below-normal priority. "Settled season" rescan:
  once a season stops receiving new episodes for 24 h, redo the O(n²)
  comparison with full-season context (cached fingerprints make this cheap).

**Other providers**: official Chapter Segments plugin (regex over embedded
chapter names — `intro|opening|^OP$`, `outro|closing|credits|ending|^ED$`,
`recap|previously on`, etc.); community EDL sidecar import/export. Gotcha:
the two EDL plugins disagree on the 3rd column (segment-type ordinal vs Kodi
action code 0=Cut/1=Mute/2=SceneMarker/3=CommercialBreak) — a bare `.edl` is
ambiguous; if we support it, pick one convention explicitly.

### Plex — proof the hybrid model works

- **Intro detection** (since 2020): per-season audio-fingerprint comparison,
  100% local, no cloud option. Ignores intros < 20 s or ending past the
  episode midpoint; needs ≥2 episodes.
- **Credits detection** (since 2023): frame-level ML (entropy + text
  detection + black frames, ~93% claimed accuracy) — and the **only
  vendor-run cloud marker service in the industry**: default mode is
  *"both, try online first"* — the server checks Plex's cloud for a match
  keyed by an **anonymous content hash** (no title/library info transmitted),
  computes locally on miss, and **contributes the result back**. It's a
  reactive crowdsourced cache, not vendor-precomputed data. Users can opt
  out (`local only`) or go read-only (`online only`).
- Voice activity data (VAD over the primary audio track) exists solely to
  power auto-sync of external subtitles — local only, 64-bit only. Chapter
  thumbnails and comskip-based DVR ad detection: local only.

### Emby

Fully local, no vendor cloud. Built-in intro **and** credits detection via
one audio-fingerprint mechanism (first 10 min fingerprinted, season-wide
comparison, ~80% self-reported accuracy), results stored as chapter markers
rather than typed segments. Community plugins bolt on OCR-based credits
detection and TheIntroDB lookups.

---

## Online pre-filled sources

The user hunch is right — but nuanced: **Plex's online source is proprietary
and closed; Jellyfin's are young community projects reached via plugins.**
A real non-anime ecosystem now exists (all emerged around Jellyfin's
MediaSegments API):

| Source | Keys | Coverage | Read auth | Data license | Notes |
|---|---|---|---|---|---|
| **TheIntroDB** (theintrodb.org) | TMDB-first, IMDB/TVDB fallback | ~49k timestamps, movies+TV | keyless | unclear | Biggest coverage; very active (plugin release 2026-07); Jellyfin/Emby/Kodi/Stremio plugins; `GET /segments`, `POST /segments/submit` |
| **SkipMe.db** (intro-skipper org) | TMDB/IMDB/TVDB/AniList + S/E | movies+TV | appears keyless | custom attribution clause | Companion to Intro Skipper: pulls community data down **and pushes locally-computed results up** — the exact bidirectional model we want |
| **SkipDB** (skipdb.tv) | IMDB + S/E + stream `duration` | ~18k segments / 9.5k episodes | keyless, "sensible limits" | **ODbL 1.0** + public dumps | Cleanest license; duration-aware matching handles different cuts |
| **IntroDB** (introdb.app) | IMDB + S/E | ~8.5k, TV-focused | keyless | unclear | Optimistic-publish + dispute model (riskier quality) |
| **AniSkip** (api.aniskip.com) | **MAL id** + episode + episodeLength | anime only | keyless | code MIT, data unclear | Types `op/ed/recap/mixed-*`; fuzzy-matches on episode length; would need MAL id resolution in heya.media |
| SponsorBlock | YouTube IDs | — | — | CC BY-NC-SA | Architecture reference only (hash-prefix privacy lookup, open dumps, vote weighting) |
| ChapterDB | — | frozen | — | — | Dead; read-only archive at chapterdb.plex.tv |

TMDB/TVDB/Trakt/OpenSubtitles have **no** timestamp data (Trakt only has
boolean `after_credits`/`during_credits` stinger flags).

**Caveats**: none of these publish numeric rate limits or a formal ToS for
aggregator use; all are small single-maintainer projects with real longevity
risk. Which is precisely the argument for putting the integration in
heya.media rather than in every Heya install: one cache upstream, graceful
degradation to local detection, and maintainer outreach happens once.

---

## What Heya already has (from codebase survey)

- **The chromaprint pump is the exact template.**
  `internal/worker/fingerprint_worker.go` — ffmpeg-chromaprint-muxer with
  fpcalc fallback, 120 s window, pump/kickoff wiring, `fingerprinted_at`
  NULL-pending sentinel. Prod jellyfin-ffmpeg7 has the chromaprint muxer;
  detection is PATH-based everywhere.
- **Pump pattern**: `internal/taskdefs/registry.go` +
  `kickoff_workers.go` pump mechanics (snooze loop, `river_job.metadata`
  state, `uniqueWhileActive`). A new task = one registry entry + one
  worker clone; zero new HTTP endpoints (generic `/api/tasks` surface).
- **ffmpeg wrappers** for every needed primitive: trickplay/thumbnail code is
  the shape for a `blackdetect`/`blackframe` pass; `internal/transcoder/`
  has probe/progress plumbing.
- **Jellyfin compat**: `GET /MediaSegments/{itemId}` is already **stubbed**
  (`internal/jellyfin/jellyfin.go:267`, manifest `opStubbed`). Implementing
  segments natively flips one stub → every Jellyfin client gets skip
  buttons for free.
- **FE pattern**: `useTrickplay.ts` (fetch-by-fileId + token) is the sibling
  shape for a `useMediaSegments.ts`; `VideoPlayer.vue` has no
  chapter/marker UI today — a conditional "Skip intro" overlay slots into
  the existing controls.
- **HeyaMedia client**: generated OpenAPI client; a segments lookup mirrors
  the `SimilarArtists` best-effort-GET shape. No submit-upstream pattern
  exists yet (chromaprint migration comment is the only mention) — that's
  new work shared with the fingerprint-corpus plan.
- **Gaps**: ffprobe runs `-show_format -show_streams` only — **embedded
  chapters aren't captured** (need `-show_chapters` + reprobe/backfill for
  the chapter provider). TV modeling: episodes are `tv_episodes` rows, not
  media_items; `library_files` point at the *series* media_item with S/E in
  `parse_result` JSONB — season-sibling gathering needs a new query.

---

## Recommended architecture

### Data model

New table, 1:N per file (a file can have intro + recap + credits + preview):

```sql
media_segments (
  id            BIGSERIAL PRIMARY KEY,
  library_file_id BIGINT NOT NULL REFERENCES library_files(id) ON DELETE CASCADE,
  segment_type  TEXT NOT NULL,   -- intro | outro | recap | preview | commercial
  start_ms      INTEGER NOT NULL,
  end_ms        INTEGER NOT NULL,
  source        TEXT NOT NULL,   -- chapter | chromaprint | blackframe | community | manual
  confidence    REAL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
)
-- plus library_files.segments_analyzed_at TIMESTAMPTZ (NULL = pending sentinel,
-- mirroring fingerprinted_at / loudness_analyzed_at)
```

`source` matters for precedence (manual > community > chromaprint >
blackframe > chapter) and for the future contribute-back filter (only upload
locally-computed, never re-upload community data).

### Detection (Heya server, pump-driven)

Layered cheap-to-expensive, Intro-Skipper style; each layer only touches
files the previous left unresolved:

1. **Chapter names** — regex over embedded chapters (after adding
   `-show_chapters`). Near-free.
2. **Community lookup via heya.media** — during/after enrich, keyed by
   TMDB/IMDB/TVDB id + S/E + runtime (duration tolerance for cut
   differences). Free CPU-wise.
3. **Chromaprint cross-episode** intro/credits for TV — port Intro
   Skipper's algorithm (inverted index → shift candidates → XOR/popcount →
   range merge; parameters above). Self-contained, no external deps beyond
   ffmpeg we already require.
4. **Black-frame credits** for movies + TV remainder — binary search
   backward with `blackframe`, entropy fallback later if needed.

One new pump (`scan_media_segments`), cloned from
`KickoffMusicFingerprintWorker`; season-grouped work items for the
cross-episode phase (loudness's two-phase track→album pattern is the
template if we want "wait for season, then compare").

### API + FE

- Native: `GET /api/stream/{fileId}/segments` (or under the media item —
  decide with the FE wiring), plus a `has_segments` flag on whatever payload
  the player already loads.
- Player: `useMediaSegments.ts` composable + skip-button overlay in
  `VideoPlayer.vue`; per-user per-type action (`none/ask/skip`, default
  Intro+Outro → ask) with Jellyfin's guard rails (<1 s never skip, <3 s
  never prompt, backward-seek suppression, auto-hide).
- Jellyfin compat: implement `GET /MediaSegments/{itemId}` (ms → ticks ×
  10 000), set `HasSegments` on MediaSourceInfo, flip manifest to
  `opImplemented`.

### heya.media role

- **Phase A (read)**: one endpoint, e.g.
  `GET /segments?tmdb=…&imdb=…&season=…&episode=…&duration=…`, aggregating
  TheIntroDB + SkipMe.db + SkipDB (+ AniSkip for anime once MAL mapping
  exists), normalized to one shape, cached hard. Heya-side: one new
  generated-client call in the enrich path.
- **Phase B (write, later)**: authenticated submission endpoint (shared
  auth/API-key work with the planned chromaprint corpus). Accept only
  `source ∈ {chromaprint, blackframe, manual}` uploads, keyed by content
  identity + runtime, anonymous. This is Plex's model with an open corpus.
- Before Phase A ships as a hard dependency: contact TheIntroDB/SkipMe.db
  maintainers about aggregator-scale reads (none document rate limits).
  SkipDB's ODbL + public dump also allows bulk import instead of live
  proxying — likely the safest first upstream.

### Suggested phasing

1. **Schema + service + native API + player skip button**, fed by the
   chapter provider (add `-show_chapters` to ffprobe + backfill reprobe) and
   manual editing. Small, ships end-to-end value.
2. **Local detection pump**: chromaprint cross-episode intros for TV, then
   black-frame credits. Port of Intro Skipper's core loops.
3. **Jellyfin compat**: real `/MediaSegments/{itemId}` + `HasSegments`.
4. **heya.media read path** (community fast-path, detection pump skips
   files satisfied by community data).
5. **Contribute-back** once heya.media auth/API keys exist (joint effort
   with chromaprint corpus).

Out of scope here but adjacent (each its own follow-up): chapter thumbnails
(cheap ffmpeg stills at chapter points — we have trickplay already, which
covers the scrubber use case), voice-activity data for subtitle auto-sync
(separate analysis pipeline; Plex uses it only for that).

## Open questions

- Segment storage keyed by `library_file_id`: correct for detection, but
  community data is keyed by *episode identity* — resolution happens at
  lookup time (file → parse_result S/E → tv_episodes → ids). Fine, but the
  duration-tolerance policy for mismatched cuts needs deciding (SkipDB's
  `duration` param is the model; ±5 s? scale timestamps?).
- Movies: intro markers are meaningless; credits-only. Per-library toggle
  (default: TV = intro+credits, movies = credits)?
- EDL sidecar import/export: cheap win for *arr/Kodi interop, but pick one
  column convention and document it.
- GPL note: Intro Skipper is GPL-3.0 — we port the *algorithm* (parameters,
  approach — not copyrightable) with a clean-room Go implementation, not a
  translation of their C# source.
