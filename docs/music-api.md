# Music API surface

All music endpoints live under `/api/music/*` and per-user music state under
`/api/me/*`. **Don't reintroduce top-level `/api/tracks` or `/api/albums`** —
the consolidation in early dev moved everything under the music prefix so the
shape doesn't collide with future non-music entities.

## Route map

| Group | Routes |
| --- | --- |
| **Browse** | `GET /api/music/{home,artists,albums,tracks}` (paginated), `GET /api/music/artists/{slug}` + `…/{slug}/{albums,tracks}` (per-artist), `GET /api/music/artists/{aslug}/albums/{bslug}` (album detail), `GET /api/music/tracks/{id}` (track + files + album/artist context — tracks have no slug, so this stays ID-addressed) |
| **Sonic similarity** | `GET /api/music/tracks/{id}/sonic-similar`, `GET /api/music/artists/{slug}/sonic-similar`, `GET /api/music/artists/{aslug}/albums/{bslug}/sonic-similar` (KNN on embeddings), `GET /api/music/artists/{slug}/similar` (metadata-based fallback via Last.fm/ListenBrainz) |
| **CLAP text search** | `GET /api/music/search-sonic?q=…` (free-form vibe prompt → tracks, gated on the CLAP text model being loaded) |
| **Browse-by-facet** | `GET /api/music/browse/{moods,genres,tempo}` (tile buckets) and `…/{moods/{mood},genres/{name},tempo/{band}}/tracks` (drilldown). Moods are the 9 canonical heads in `internal/sonicanalysis/musictheory.go`; tempo bands are fixed in `service/music_browse.go::tempoBands` |
| **Recommendations + mixes** | `GET /api/music/home/mixes-for-you` returns a rule-driven slate (`for_you`, `discovery`, `rediscovery`, `deep_cuts`, then artist mixes). Every mix has its own `slug`, `kind`, `description`, and playable rich-track rows. The shared engine blends explicit track/album/artist ratings, weak completed-listen affinity, CLAP KNN, localized similar-artist edges, provider top tracks, and enriched catalog popularity. |
| **Instant Radio** | `POST /api/music/radio` with `{seed: {kind, …}, limit, exclude_track_ids}` — seed kinds: `track`, `artist`, `album`, `text`, with optional multi-seed `seeds`. Uses the same recommendation candidates as generated mixes. Sonic KNN is preferred when facets exist; external related artists + provider top tracks provide the fallback for an unanalysed seed. Returns `{seed_track_id, tracks}`. |
| **Quick stations** | `GET /api/music/stations/{library-radio,deep-cuts,time-travel,random-album}`. Library Radio uses the shared For You policy and Deep Cuts targets unplayed catalog from known artists; both retain cold-library fallbacks. |
| **DJ Mix** | `GET /api/music/tracks/{id}/mix-to` — harmonically-compatible tracks. Constrained to Camelot-wheel adjacency (same position, relative key, or ±1 wheel position) AND ±5 BPM of the seed, ordered by embedding distance. `sonicanalysis.Key.CompatibleKeys()` computes the allowed (root, mode) set. |
| **Per-track binary** | `GET /api/music/tracks/{id}/stream` (auto-picks best playable + caps fallback to AAC mp4), `…/file/{tfid}` (bit-perfect), `…/lyrics`, `…/facets`, `…/waveform` |
| **Album cover** | `GET /api/music/artists/{aslug}/albums/{bslug}/cover` (local file or 302 to upstream) |
| **Loved + playlists** | `POST/DELETE /api/me/loved/{tracks,artists,albums}/{id}`, `GET …/ids`, paginated lists at `/api/me/loved/{plural}`; full playlist CRUD at `/api/me/playlists` |
| **Internet Radio** | `GET /api/radio/{top,search,countries,tags}` (cached radio-browser.info proxy), `GET /api/radio/stream?url=…` (streaming proxy with ICY metadata stripped + emitted to the event hub as `radio.icy`). User state: `GET/POST/DELETE /api/me/radio/favorites`, `POST /api/me/radio/play`, `GET /api/me/radio/recents`. Backend in `internal/radiobrowser/{client,icy}.go`. FE pages under `/music/radio/{index,countries,tags,favorites,recents}` with `RadioStationCard.vue` + `useRadioActions()` shared across them. |
| **Podcasts** | `GET /api/podcasts/{trending,search,categories,feed}` — Podcast Index proxy (needs `HEYA_PODCAST_INDEX_KEY`+`_SECRET`) + RSS parser (`gofeed`) for the per-feed detail page. User state: subscriptions CRUD + `/api/me/podcasts/{progress,continue}` for resume-able episodes. Episode audio proxied through `GET /api/podcasts/episode/stream?url=…` so CORS / auth are handled centrally. Backend in `internal/podcastindex/{client,feed}.go`. FE pages under `/music/podcasts/{index,categories,feed}` with `PodcastCard.vue` + `usePodcastActions()`. |
| **Playback emission** | `POST /api/me/playback` — **unified** endpoint for video progress and music lifecycle events. Body: `{entity_type, entity_id, position_seconds, total_seconds, completed, source, started_at_unix?}`. Server dispatches: `movie`/`episode` → upsert `user_watch_progress`; incomplete `track` → transient ListenBrainz/Last.fm now-playing; completed `track` → append `play_events` and submit permanent external scrobbles. FE calls via `recordPlayback()` in `composables/usePlaybackEvents.ts`; both `useVideoPlayer` and `usePlayer` flow through it. Podcast episodes use a separate `POST /api/me/podcasts/progress` because they don't have media_item IDs. |
| **Listening history** | `GET /api/me/recently-played` (deduped track rail), `GET /api/me/listening-stats` (top genres + mood averages + tempo histogram for the user) — both derived from `play_events`. |

## Negative track IDs (radio + podcasts)

Radio station rows synthesize a negative `Track.id` (hashed from
`stationuuid`) to avoid colliding with music-library track ids. Podcast
episodes do the same. **Don't fire
`/api/music/tracks/{id}/{facets,waveform,lyrics}` when `track.id <= 0`** —
those endpoints 422 on negative inputs.

## Response shapes

Response envelopes are typed end-to-end. Tile lists use `{items: T[]}` with `T`
spelled out (`moodBucketsBody`, `trackResultsBody`, etc. in
`music_huma.go`) so the generated TS client preserves shape; paginated lists
use `service.MusicListPage[T]`.

Sonic-similar / sonic-search / radio / drilldown rows all share the **rich
track row shape**: `track_id, track_title, duration, disc/track number,
album_{id, title, slug, cover_path, year}, artist_{id, name, slug}`. Generated
as `sqlc.SimilarTracksByTrackRichRow`, `SimilarTracksByTextRichRow`,
`ListTracksByMoodRow`, etc. — same columns, different `WHERE`. When you add a
new "tracks filtered by X" query, mirror this shape so the FE keeps one row
component.

## Slug-first addressing

Anything with a stable slug is addressed by slug, not numeric ID, in the URL.
Artists use their `media_items.slug`; albums use the
`(artist_slug, album_slug)` pair (album slugs are unique within an artist, not
globally). Tracks have no slug so they stay ID-addressed.
`useAlbumCoverUrl(artistSlug, albumSlug)` is the canonical FE composable —
every list row already carries both fields, so call sites just pass them
through.

## Cache-Control bands

| Endpoint class                                          | Cache-Control          |
| ------------------------------------------------------- | ---------------------- |
| Browse listings                                         | `private, max-age=30`  |
| Music home                                              | `private, max-age=60`  |
| Per-track static (files / lyrics / facets / waveform)   | `private, max-age=300` |
| Sonic-similar                                           | `private, max-age=300` |
| Search                                                  | `private, max-age=60`  |
| Listening stats                                         | `private, max-age=60`  |
| Generated mixes                                         | `private, max-age=3600` |
| Radio (personal + ephemeral)                            | `no-store`             |
