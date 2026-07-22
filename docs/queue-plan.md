# Server-owned play queue

Build plan for moving the music queue out of the browser and into the
server, with every client live-mirroring it over the WS bus. Companion to
[docs/cast-plan.md](cast-plan.md) — cast Phase 3 (server-side auto-advance,
gapless) lands *on top of* this queue rather than growing a session-private
one.

## Why

- **Client shuffle is a lie.** `usePlayer.toggleShuffle` shuffles whatever
  page of tracks the client happened to load — "shuffle this genre" over a
  10k-track tag is random over the first fetch, not the set. True random
  needs the server.
- **Queues with thousands of tracks can't live in a Pinia array** (or cross
  the wire on every play click).
- **The queue should survive the client.** Start music on the phone, lock
  it, open any client 45 minutes later → see where the queue is, edit it.
- **Cast needs it anyway**: close-the-laptop auto-advance (cast Phase 3)
  requires the server to know what's next.

## Decisions (made)

| Decision | Choice | Why |
| --- | --- | --- |
| Queue scope | **One queue per user/device** (`UNIQUE(user_id, device_id)`) | Each renderer keeps its own context. Selecting another Heya client binds the controller to that device's queue without copying or merging queues. |
| Player model | **One active output per user** (Spotify Connect semantics) | Two tabs advancing one pointer is chaos. A second tab becomes a mirror/remote; its play button = "play here" (transfer) |
| Queue storage | **Materialize fully**, windowed reads | 10k rows is nothing for PG (`INSERT … SELECT … ORDER BY random()`); stable order makes repeat/reorder/up-next well-defined. No sampling/virtual queue in v1 |
| Client view | **Window around the pointer** (current ± ~50, paged) | Clients never hold the full queue; fixes the 10k-array problem outright |
| Live sync | **WS, per-user scoped and device-tagged** (`hub.PublishToUser`) | Every client receives its own user's events and applies only the selected target device's queue events. |
| Event shape | Thin `queue.changed` + `version` counter; refetch window on version gap | Live Interactivity pattern — lean invalidate+refetch, no CRDT ambitions |
| Cutover | **Full swap, no dual path** | Client-queue and server-queue side by side is permanent complexity. Local playback consumes the server queue too |
| Sequencing | **Queue swap first, cast binds after** | Prove the model with the daily-driver (local playback) before rewiring cast advance |
| Playlists | Same WS treatment (version + `playlist.changed` → invalidate) | Multi-client collab for the same user now; cross-user sharing is a later phase on the same rails |

## Data model

```sql
CREATE TABLE play_queues (
  id               BIGSERIAL PRIMARY KEY,
  user_id          BIGINT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  version          BIGINT NOT NULL DEFAULT 0,       -- bumped on EVERY mutation
  current_item_id  BIGINT,                          -- pointer into items
  position_seconds REAL NOT NULL DEFAULT 0,         -- coarse, heartbeat-fed
  playing          BOOLEAN NOT NULL DEFAULT false,
  repeat_mode      TEXT NOT NULL DEFAULT 'off',     -- off | all | one
  shuffled         BOOLEAN NOT NULL DEFAULT false,
  dj_mode          TEXT NOT NULL DEFAULT 'off',     -- off | echo | flow | voyage | encore | spotlight | timewarp
  dj_session       BIGINT NOT NULL DEFAULT 0,       -- invalidates in-flight DJ work
  source           JSONB,                           -- {kind, id, shuffle} provenance
  active_output    TEXT,                            -- 'local:<client_id>' | 'cast:<device_id>' | NULL
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE play_queue_items (
  id        BIGSERIAL PRIMARY KEY,
  queue_id  BIGINT NOT NULL REFERENCES play_queues(id) ON DELETE CASCADE,
  ord       BIGINT NOT NULL,                        -- sparse: n*1024, renumber lazily
  track_id  BIGINT NOT NULL,
  dj_session BIGINT NOT NULL DEFAULT 0,             -- 0 = user-owned
  dj_mode   TEXT NOT NULL DEFAULT '',
  UNIQUE (queue_id, ord)
);
```

- **Sparse `ord` keys** (gap 1024): reorder = one-row UPDATE into the gap;
  renumber the tail only when a gap exhausts. No 10k-row shifts per drag.
- **History stays in the table** — the pointer moves past played items, so
  "Played" and prev-track work naturally. Prune history beyond ~200 items
  on advance.
- `version` is the concurrency spine: every mutation bumps it inside the
  same transaction; WS events carry it; a client that sees a gap (or is
  reconnecting) refetches its window. Mutations may carry
  `expected_version` for optimistic concurrency where it matters (reorder).
- **Sources materialize server-side**: album / artist / playlist / mix /
  genre-tag / explicit track list — each reuses the SQL its list endpoint
  already has, `ORDER BY random()` when shuffled. Re-shuffle = re-materialize
  the un-played remainder. The `source` column is provenance for
  "re-shuffle", "append more like this", and later radio mode.

## API (`/api/me/queue` — the queue is personal, so it lives in the `me` namespace)

| Route | Purpose |
| --- | --- |
| `GET    /api/me/queue?around=current\|<ord>&limit=100` | Meta (version, pointer, position, transport, total, source) + item window |
| `POST   /api/me/queue` | Replace: `{source \| track_ids, start_track_id?, shuffle?}` → materialize + point + play |
| `POST   /api/me/queue/items` | Append / play-next: `{track_ids, at: 'end'\|'next'}` |
| `DELETE /api/me/queue/items/{id}` | Remove one upcoming item |
| `POST   /api/me/queue/items/{id}/move` | `{after_item_id \| first}` — sparse-key reorder |
| `POST   /api/me/queue/jump` | `{item_id}` — pointer jump |
| `POST   /api/me/queue/advance` | `{from_item_id, reason: 'ended'\|'skip'\|'prev'}` — renderer reports; idempotent (from guards double-fires) |
| `POST   /api/me/queue/dj` | `{mode}` — choose a DJ or turn it off; switching removes only future DJ-owned items |
| `POST   /api/me/queue/shuffle` / `…/repeat` | Mode flips; shuffle reshuffles/restores the upcoming slice server-side |
| `POST   /api/me/queue/heartbeat` | `{client_id, position_seconds, playing}` — coarse position + renderer liveness (~15s while playing) |
| `POST   /api/me/queue/claim` | `{output: 'local:<client_id>' \| 'cast:<device_id>'}` — become the active output; everyone else drops to mirror |
| `DELETE /api/me/queue` | Clear |

WS: one per-user event, `queue.changed {device_id, version, kind: 'replaced'|'items'|'pointer'|'transport'|'output', current_item_id, position_seconds, playing, dj_mode, active_output}`.
Common cases (pointer move, transport flip) apply straight from the
payload; anything structural (`replaced`, `items`) triggers a window
refetch. CLI mutations reach the serve process's hub via the existing
LISTEN/NOTIFY relay — CLI goes over HTTP anyway (same rule as cast).

## Phase A — queue service + API ✅ SHIPPED 2026-07-13

Built as planned (migration 00025, `queries/play_queue.sql`,
`internal/service/queue.go`, `/api/me/queue` routes, per-user
`queue.changed` events) with two field notes:

- **Historical note:** the first release temporarily accepted both
  `tracks.library_file_id` and `track_files`. Migration 00057 redirected and
  removed the stale direct links; materializers now use `track_files` only.
- **The CLI is `heya player`** (`show/play/add/next/skip/shuffle/
  repeat/clear`) — `heya queue` was already taken by the River job queue.
- `src_ord` (rank in the source's natural order, captured at
  materialization) is what makes shuffle-off restore the original order
  without re-querying the source.

Originally verified with 7 integration tests over a real DB (advance idempotency +
repeat modes, shuffle/unshuffle restore, play-next gap math + dedupe,
move-around-pointer, claim/heartbeat rejection, history pruning at
exactly 200), plus a live smoke against the dev server: shuffled album
materialization, skip, stale-double-fire no-op, clear.

## Phase B — the FE swap (the big lift) ✅ CORE SHIPPED 2026-07-13

Landed as a **compatibility facade** instead of a 40-file sweep:
`usePlayer.queue` became a writable computed over the `useQueue` windowed
mirror — the setter stages a `tracks`-source replace that the play(track)
call following every `queue.value = tracks` finalizes with the right
start track. All existing play-context call sites work unchanged and are
server-backed immediately; upgrading contexts to semantic sources
(album/genre — task for the >window cases) is incremental follow-up.
Field notes:

- **Radio streams and podcast episodes aren't music-track rows** — any
  list containing them flips the facade to a LOCAL queue (the old array
  behavior, kept for exactly this). Podcasts use negative synthetic ids.
- **Nothing incidental may clear the server queue.** An empty
  `queue.value = []` assignment only resets local state; deletion happens
  on the labeled hold-to-stop gesture and the panel's Clear. (First
  version cleared server-side and something in page teardown nuked the
  queue on reload.)
- Advance/skip report `from_item_id` fire-and-forget after the local
  engine transition — gapless stays renderer-local, the server pointer
  follows, double-fires no-op.
- Verified live: album play → server materialization via an untouched
  call site → tab claims output → heartbeats → hard reload → full
  restore (queue window, current track, position) → queue panel renders
  played/up-next from the window.

Original scope notes below.

- New `useQueue` store: window + meta + version, optimistic mutations with
  rollback, WS reconcile, window paging as the user scrolls the panel
  (virtualized — vue-virtual-scroller is already a dep).
- `usePlayer` transport keeps its shape; `peekNextTrack()` reads
  `window[current+1]`, so deck preloading / gapless / crossfade machinery
  is untouched. `handleEnded` → optimistic pointer move + `POST advance`.
- **Call-site sweep**: every "play this" context (album, artist, playlist,
  mix, tag/genre, track list) stops shipping arrays and posts a source
  descriptor + start track. This is the wide-but-shallow part.
- Multi-tab: tabs get a `client_id`; a non-active tab renders the mirror
  with a "Playing on <output> — play here" affordance; play-here = claim +
  local render from the server position. Claimed-away tabs pause their
  engine on the `output` event.
- Scrobbling stays renderer-side (existing `/api/me/playback` path) for
  local output.

## Phase C — cast binds to the queue

Cast Phase 3, reshaped: `CastSession` consumes the user queue
server-internally (advance on feeder EOF), replacing Phase 2's
client-driven `castTrackEnded` advance. Gapless continuous-stdin feed
(next ffmpeg into the same cliap2 stdin + SENDMETA metadata flush) —
**validate with a two-track harness first**; fall back to
respawn-per-track if the boundary glitches. `startCastTo` becomes
`claim('cast:<device>')`; disconnect claims back to local with the
handoff position the server already knows. Kills `localHandoff` and the
FE advance-ownership machinery from cast Phase 2.

## Queue DJs

DJs are persisted queue-insertion strategies, shared by every controller of
the same device queue. Echo, Encore, and Voyage decorate user-owned tracks;
Flow, Spotlight, and Timewarp continuously maintain a two-track runway. Every
generated item carries its DJ session and mode, so switching or disabling a DJ
deletes only its future contributions and never the listener's queue or played
history. Queue replacement invalidates the session and turns the DJ off.

Recommendation work runs outside the queue transaction. The commit rechecks
the queue, current item, mode, and monotonically increasing session before it
can insert anything, making a late result from an old DJ harmless.

## Phase D — playlists go live-collab

Same rails, much smaller: playlist mutations already run through the
service layer — add `version` + emit per-user `playlist.changed`, FE
invalidates the Pinia Colada playlist queries (pattern:
`cache-invalidation.client.ts`). That gives same-user multi-client sync
now. Cross-user sharing later = `playlist_members` ACL + emitting to each
member (the event fan-out is the easy half; the UI/permissions model is
the real work, deliberately deferred).

## Later / out of scope for now

- Cross-user shared playlists (Phase D groundwork)
- Multiple named queues per user (explicitly not doing this)
- Jellyfin/Subsonic compat queues (their clients own playback; untouched)

## Risks

- **Every transport mutation becomes a round trip.** LAN/tailnet is fine;
  optimistic UI hides the rest. Offline PWA queue editing degrades —
  accepted, Heya is server-centric.
- **The Phase B sweep touches most play sites.** Mitigate by landing the
  `useQueue` store + one context (album page) first, then sweeping.
- **Two-tab races** (both post advance): `from_item_id` idempotency guard
  + single active output make double-fires no-ops.
- **Migration numbering races** with parallel sessions (known gotcha) —
  claim the migration number at branch start.
