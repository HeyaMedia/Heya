# Cast — server-side playback to network receivers

Implementation plan for the Cast feature: pick a receiver in any Heya
client, and the **server** streams music to it — clients only send
control commands. Protocol research, live-validated recipes, and failure
modes live in [docs/casting-research.md](casting-research.md); read that
first. This doc is the build plan.

**Scope for now: AirPlay 2 via the `cliap2` subprocess, music only.**
Google Cast / DLNA / vendor URL-push are later providers behind the same
interface. Video is out of scope until the audio path is proven.

## Architecture

```
FE player (remote mode) ──HTTP──▶ /api/cast/* ──▶ service.App
CLI (heya cast …)        ──HTTP──▶      │
                                        ▼
                              internal/cast
                    ┌────────────┬──────────────┬─────────────┐
                    │ discovery  │ CastSession  │ providers   │
                    │ (mDNS)     │ (queue, pos, │ airplay:    │
                    │            │  volume, WS) │ cliap2 supv │
                    └────────────┴──────┬───────┴─────────────┘
                                        │ PCM (stdin) + FIFO (ctrl)
                                        ▼
                               cliap2 ──AirPlay 2──▶ receiver
```

- **The server is the player.** A `CastSession` owns queue, position,
  volume, and state; every client mirrors it via the existing
  `eventhub` WS bus. UI buttons are API calls against the session.
- **One session per device**; starting a new one on the same device
  replaces the old (clean stop first).
- **PCM is a continuous stream**: track boundaries are invisible to
  cliap2, so the server gets **true gapless** by simply writing the next
  track's PCM into the same stdin and updating metadata via the FIFO.
  Process respawn is the exception (device change, error recovery), not
  the per-track rule.

## Decisions (made)

| Decision | Choice | Why |
| --- | --- | --- |
| Sender binary | cliap2 only (skip cliraop) | AP2 live-validated on RX-V6A; clean GPL-2.0 (cliraop has no license); one binary to supervise |
| Binary distribution | `go:embed` all 4 platform builds (~184 KB each), extract to `<DataDir>/cast/bin/` on first use | Keeps single-binary story; follows `internal/llm/local.go` `serverDir()` precedent; GPL-2.0 as separate-process aggregate, ship a SOURCES note pinning the upstream commit |
| PCM format | s16le / 44.1 kHz / stereo | What the sessions negotiated in testing; 48 k via cliap2 config file later if wanted |
| Seek | `ACTION=FLUSH` + refeed same stdin from new offset; fall back to respawn if flush proves unreliable | Avoids session churn; respawn path validated live either way |
| Volume model | Session volume = stream volume (`VOLUME=` via FIFO). The receiver's own knob is downstream amp gain — independent, not mirrored in v1 | Backchannel is Phase 4 |
| Scrobbles | Server-side: `app.RecordPlayback(ctx, userID, PlaybackEvent{…, Source: "cast"})` at the same 30 s threshold; FE scrobbling disabled in remote mode | One source of truth, no double-count |
| Config | `cast.enabled` knob mirroring `internal/service/subsonic_settings.go` (env-lockable, DB/UI editable) | Established provenance pattern |
| mDNS library | `grandcat/zeroconf` first (browse **and** register in one dep — register needed for DACP in Phase 4); `hashicorp/mdns` (go2tv's pick) as fallback | New dependency either way (repo has none) |
| ffmpeg | Bare `ffmpeg` from `$PATH`, as everywhere else in the repo | Container symlinks jellyfin-ffmpeg onto the standard names; no config knob exists or is needed |

## Phase 1 — `internal/cast` core + CLI ✅ SHIPPED 2026-07-10

Built as planned with three deviations discovered during live testing:

- **PCM feeding is real-time (`ffmpeg -re`), not unthrottled** —
  cliap2's `ACTION=PAUSE` only pauses stdin intake, so an unthrottled
  feeder makes pause a no-op (whole track already buffered). `-re` also
  eliminates the EOF-before-commence hazard.
- **Session pause = freeze position + stop transport; resume =
  respawn at position** (instant silence, ~2-3 s resume, reuses the
  proven seek path). The FIFO ACTION=PAUSE drains ~4-5 s of primed
  buffer before going quiet — unusable as UI pause.
- **Containers compile cliap2 from source** (Dockerfile.cpu build
  stage, runtime deps self-computed via ldd→dpkg): the CI binaries
  target Bookworm sonames, the image is trixie. `ensureCliap2` prefers
  a $PATH binary; the go:embed copies serve dev/bare-metal.

Verified: unit tests over captured stderr transcripts + an env-gated
live hardware test (`HEYA_CAST_LIVE_TEST=1 go test ./internal/cast/
-run TestLiveAirplay`) covering discovery → play → volume → pause →
resume → seek → stop against the RX-V6A, plus `heya cast devices`
through the full dev-proxy/API stack. Backchannel + queue remain
Phases 3/4; FE hookup is Phase 2.

**Deliverable: `heya cast play <track> --to anlæg` plays on the Yamaha,
with metadata on its display.**

New package `internal/cast/`:

- `discovery.go` — continuous zeroconf browse of `_airplay._tcp`
  (TXT must be passed to cliap2 **verbatim**; it needs `deviceid=`).
  Maintains a device cache: name, host, IP, port, TXT, last-seen.
- `binary.go` — embed + extract cliap2 per platform (llm/local.go
  shape). Verify extraction with `--version` (binary prints and exits).
- `supervisor.go` — the session process lifecycle. Skeleton from
  `internal/llm/local.go` (mutex-guarded spawn, stderr tail, done
  channel); rules from casting-research.md baked in:
  - invocation ALWAYS includes `--latency` (the commence-wedge fix),
    `--ntpstart` = `--ntp` output + 7 s lead (uint64 math — no shell),
    fresh command FIFO per session;
  - stderr state machine: `DACP ID set to` → spawned;
    `device_activate_cb (status 2)` → connected;
    `event_play_start` → **streaming** (nothing else counts as playing);
    `Pause at` / `Restarted at` / `end of stream reached` /
    `closed RTSP connection` / pairing `[ LOG]` errors → state edges.
    Parser is a pure function over lines → unit-testable without a
    receiver;
  - FIFO writes gated on the streaming state (pre-roll writes wedge the
    session thread); metadata bundle = `TITLE/ARTIST/ALBUM/DURATION/
    ARTWORK/PROGRESS` + `ACTION=SENDMETA`;
  - failure: no `event_play_start` within `lead + establishment + 2 s`
    → SIGTERM (never SIGKILL — TEARDOWN must go out), retry once with
    backoff, hard ceiling 25 s (beat the receiver's ~31 s idle
    timeout);
  - shutdown: `ACTION=STOP`, wait for exit, SIGTERM after grace.
- `feed.go` — ffmpeg decode to stdin: `ffmpeg -i <path> -vn -f s16le
  -ar 44100 -ac 2 -` via `exec.CommandContext` + `StdoutPipe` (pattern:
  `internal/transcoder/session.go`). Per-track ffmpeg processes writing
  sequentially into cliap2's persistent stdin (gapless). `-ss <sec>` for
  seek refeed. Short clips (< pre-roll) need `-re` or padding — see
  research doc EOF-before-commence. v1 limitation: local paths only
  (SMB sources rejected, same as `transcodePrimaryAndServe`).
- `session.go` — `CastSession`: device, user, queue (v1: single track;
  Phase 3: full queue), position clock (server-derived: track start
  time + elapsed, corrected on pause/seek), volume, state. Emits
  `eventhub` events on every transition.
- Service wiring: `App.Cast()` accessor like `App.AudioSessions()`;
  session registry keyed by device ID.
- `eventhub`: new `EventCastState` with thin payload (session ID,
  device, track ID, state, position, volume) — FE treats it as an
  invalidate trigger + position tick, per the Live Interactivity
  pattern. CLI-initiated changes reach the serve process's hub via the
  existing `relay.go` LISTEN/NOTIFY bridge only if the CLI mutates
  directly — but see below: CLI goes over HTTP, so this is moot.
- CLI `cmd/heya/cmd/cast.go`:
  - `heya cast devices` — service-direct (`withApp`), one-shot browse.
  - `heya cast play|pause|resume|seek|volume|stop|status` — thin HTTP
    client against the **running server** (the `api.go` token pattern).
    Playback state must live in the serve process; a CLI-process
    session would die with the terminal. This is the one place the
    CLI-first rule means "CLI drives the server's API" rather than
    "CLI links the service layer".
- Config: `internal/service/cast_settings.go` mirroring
  `subsonic_settings.go` (`cast.enabled`, default on — discovery is a
  passive mDNS browse).

Tests: stderr-parser unit tests from the captured run logs (happy path,
commence-wedge, EOF-before-commence, RTSP-close — all in the scratchpad
transcripts, copy the interesting lines into testdata). Live smoke =
the CLI against Anlæg.

## Phase 2 — API + UI hookup ✅ SHIPPED 2026-07-13

Built as planned with three deviations found during implementation:

- **Transport routing lives in `usePlayer`, not a third engine variant.**
  The audio engine is a module singleton and `usePlayer` wires its
  callbacks/watches exactly once (`engineWired`) — swapping engine
  instances at runtime would strand that wiring. Instead the player's
  transport actions (`play/pause/seek/setVolume/stop/prev`) check
  `useCastStore().engaged` and become `/api/cast/*` calls; the queue,
  shuffle, repeat, and advance logic runs unchanged on top. Deck arming
  and prefetch are skipped while engaged.
- **The picker is the global topbar button** (the placeholder
  `topbar-cast-btn` became `CastButton.vue`), not a playbar control —
  casting is app-wide, the playbar is music-only. The playbar just hides
  the browser-quality readout while the output is remote.
- **`POST /api/cast/sessions` gained `start_seconds`** so engaging a
  device mid-track hands playback off at the current position (and
  disconnect hands it back the same way via a local cold-load handoff).

Also of note: `plugins/cast-live.client.ts` defers its boot fetches to
`app:mounted` — plugins load alphabetically, so a `$heya` call during
setup would run before `heyaApi.client.ts` registers the bearer-token
hook and 401-logout the user. Sessions are per-track server-side, so the
FE keeps a separate `engagedDeviceId` that survives between sessions;
new sessions reuse the last known device volume (first engage caps at
30). Advance ownership: only the tab that started the current play
drives the queue on natural end. WS reconnect re-adopts the session
snapshot. Verified: vue-tsc clean, cast unit tests green, Eye-driven
picker → engage → disconnect against live discovery (Yamaha RX-V6A +
AppleTVs found).

**Deliverable: Cast button in the web player; controls stay in sync
across every open client.**

- `internal/server/cast_huma.go` → `registerCastRoutes` (+ one line in
  `BuildAPI()`; `radio_huma.go` is the size/shape reference):
  - `GET  /api/cast/devices`
  - `POST /api/cast/sessions` `{device_id, track_id}` (Phase 3 adds
    `queue`)
  - `GET  /api/cast/sessions/{id}` / `GET /api/cast/sessions` (active)
  - `POST /api/cast/sessions/{id}/pause|resume|stop`
  - `POST /api/cast/sessions/{id}/seek` `{seconds}`
  - `POST /api/cast/sessions/{id}/volume` `{level}`
  - `make gen-api-client` after (openapi-drift hook enforces).
- FE (`web/`):
  - `plugins/cast-live.client.ts` — subscribe `EventCastState` via
    `useEventBus`, update cast session state + invalidate queries
    (exact shape of `watch-live.client.ts`).
  - Device picker: `AppMenu` off a Cast icon in the player bar; lists
    `GET /api/cast/devices`; picking one starts/transfers the session.
  - **Remote output mode**: third engine variant beside
    `createEngine`/`createDirectEngine` in `useAudioEngine.ts` — same
    interface (`play/pause/resume/seek/setVolume/transition`), but the
    methods call the cast API and `position` is fed from WS ticks
    instead of the audio clock. `usePlayer.ts` (queue, shuffle, repeat,
    track-advance) keeps working untouched on top — engine selection in
    `ensureEngine()` (`usePlayer.ts:241-268`). Disable the FE scrobble
    path when remote (server records with `source: "cast"`).
  - v2 limitation (accepted until Phase 3): track auto-advance is
    client-driven — the tab must stay open for the next track to start.

## Phase 3 — server-owned queue

**Deliverable: start an album, close the laptop, the album finishes.**

- `POST /api/cast/sessions` accepts a queue snapshot (track IDs +
  start index); session advances on track EOF (stderr `end of stream
  reached` for the *feeder*, not the session — with gapless feeding the
  boundary is the feeder-process exit, at which point the next ffmpeg
  spawns against the same stdin and a fresh metadata bundle goes down
  the FIFO).
- Queue mutations (add/remove/reorder/jump) become session API calls;
  `usePlayer.ts` queue ops route to the API when in remote mode.
- Server-side `RecordPlayback` per track (30 s threshold, `completed`
  on natural EOF).
- Repeat/shuffle live server-side with the queue.

## Phase 4 — backchannel (receiver → server)

Live test 2026-07-10 proved cliap2 v1.5 has **no** backchannel (zero
DACP callbacks, zero event-channel messages — see research doc). Two
independent tracks, both provider-side:

1. **DACP listener (generic AirPlay):** advertise `_dacp._tcp` under
   the DACP-ID we already pass to cliap2 (zeroconf register) + a tiny
   HTTP handler for `/ctrl-int/1/pause|play|playpause|volumeup|…` →
   session commands. Needs a live test — unknown whether the RX-V6A
   sends anything.
2. **Vendor state sources (per-brand plugins):** small interface —
   `Match(device) bool` (by mDNS TXT `manufacturer=`/`model=`),
   `Watch(ctx, device) <-chan StateUpdate` (volume, transport) — folded
   into the session and broadcast like any other change. First impl:
   **Yamaha MusicCast (YXC)** — local HTTP API + UDP event
   subscription, testable on the house RX-V6A. Sony / NAD / Panasonic /
   Denon HEOS / BluOS are community-contribution targets once the
   interface is stable: implement blind from vendor docs, mark
   untested, let owners verify. Document the interface + one worked
   example in `docs/` for contributors.

## Phase 5 — later providers & polish (unscoped)

- Google Cast v2 provider (pure Go, URL-pull — receiver fetches
  `/api/music/tracks/{id}/stream?token=…` with a scoped short-lived
  cast token instead of a session token).
- DLNA AVTransport provider (goupnp), WiiM/vendor URL-push.
- Multi-device / multiroom sync (coordinated cliap2 instances — MA
  proves it's possible; real work).
- HomeKit-managed speakers (Home-app access gotcha), password
  receivers (`--password`), artwork over FIFO (`ARTWORK=` URL).
- Sub-entity: cast video (different transport entirely — Cast/DLNA
  territory, not AirPlay-audio).

## Risks / open questions

- **In-band seek (FLUSH + refeed)** is designed-but-untested; respawn
  is the proven fallback. Test early in Phase 1.
- **Gapless continuous-stdin feeding** is architecturally sound (pipe
  input is how librespot/shairport feed OwnTone) but untested with
  cliap2 specifically; if track transitions glitch, fall back to
  respawn-per-track (2 s gap) and revisit.
- **macOS dev deps**: the CI binary links brew dylibs (ffmpeg, libplist,
  confuse, zlib, libiconv, …). Fine for the dev box (documented in
  research doc); the embedded-binary story must eventually build
  static-ish or vendor the dylib set — container (Debian) builds are
  the deployment target and match the CI artifacts' Bookworm ABI.
- **SMB-sourced tracks** can't feed ffmpeg by path in v1 (same
  restriction as the AAC transcode path). Revisit with the vfs streaming
  work.
- **Upstream divergence**: we intentionally do not report the
  commence-wedge bugs upstream; pin the exact cliairplay commit we
  embed and re-validate the stderr contract on any bump.
