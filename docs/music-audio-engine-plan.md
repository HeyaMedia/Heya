# Music Audio Engine — Heya vs Hibiki, and the plan to make it amazing

> Comparison of Heya's music audio engine against **Hibiki** (`~/Private/hibiki`),
> a sibling player descended from the same `plexmusicclient v2` Web Audio engine.
> Generated 2026-06-30. Sections 1–2 are the findings; section 3 tracks the work.

## 1. Verdict

Heya ported the *same* dual-deck Web Audio engine Hibiki uses — `app/engine/*`
(Deck, DeckManager, Scheduler, SignalChain, crossfade curves/strategies,
AnalyserBridge) is a near-byte-identical copy. In several places Heya is
**ahead** of Hibiki and must not be regressed toward it:

- **Loudness** is measured server-side in Go (EBU R128) at **track and album**
  level, and Heya correctly separates **true-peak** from **sample-peak**
  (`loudness_worker.go`) — Hibiki conflates them with a single `Peak:` match.
- **BPM + musical key** are pure-Go (Percival-style BPM, Krumhansl-Schmuckler
  key) with real confidence scores — no external binaries.
- An **ONNX genre/mood/embedding/CLAP** stack (`internal/sonicanalysis`) that
  Hibiki has nothing comparable to.
- The **static, server-precomputed waveform scrubber** (`MusicWaveform.vue` ←
  `waveform.go`) shows real song structure; Hibiki's live-FFT `SeekVisualizer`
  bars convey nothing about the track. **Keep Heya's.**

**The gap was never the engine — it was the consumer layer.** Heya replaced
Hibiki's Pinia `player.ts` store with a leaner `usePlayer.ts` and, in doing so,
**never wired the dual-deck transition machinery**. Gapless, crossfade,
smart-crossfade, prefetch, album-aware suppression, and pending-deck
normalization were all fully present but **dead code** — every track change was
a cold `stop→load→play` with an audible gap, and the crossfade controls in the
EQ panel were placebo.

## 2. Things NOT worth copying from Hibiki

- Hibiki's live `SeekVisualizer` as the scrubber (Heya's precomputed waveform is better).
- "Fixing" the true-peak regex toward Hibiki (Hibiki's is the buggy one).
- Aubio/keyfinder external binaries (Heya's pure-Go analysis already matches/beats them).
- Multi-track HTTP prefetch depth >1 against `/stream` (risks speculative server transcodes).

## 3. Backlog & status

Ranked by (audible/UX impact) ÷ effort. `S/M/L` = small/medium/large.

### ✅ Done — "player core revival" + leapfrog (2026-06-30)

| # | Item | Files |
|---|------|-------|
| 1 | Prefetch next track onto the pending deck (`loadNext`, depth 1) | `usePlayer.ts` |
| 5 | **Gapless** playback — `setOnTransitionPoint` → scheduler hard-cut swap | `usePlayer.ts` |
| 8 | **Time-based crossfade** end-to-end (`TimeBasedCrossfade` → `engine.transition`) | `usePlayer.ts` |
| 14 | Album-aware crossfade suppression + settings model (`albumAware`) + UI toggle | `useAudioSettings.ts`, `usePlayer.ts`, `EQPanel.vue` |
| 4 | **Fixed shuffle** — reorders the upcoming queue in place + `originalOrder` restore (also fixes prevTrack / Up-Next mismatch) | `usePlayer.ts` |
| 3 | **Fixed ReplayGain + real album/auto** — `off` truly disables; `track`/`album`/`auto` now use the right loudness. Album LUFS (already computed in Go) is exposed via the track-detail query (`al.integrated_lufs/true_peak_db` on `GetTrackDetailByID`); the player fetches `/api/music/tracks/{id}` (track loudness + boundaries + album loudness in one call), and `effectiveLoudness()` picks track vs album by mode (`auto` = track on shuffle, album otherwise), decided per track-load. Quality popover shows track + album LUFS and the applied gain with its source. | `queries/music.sql`, `usePlayer.ts`, `useAudioSettings.ts`, `PlaybarQuality.vue` |
| 12 | **Scrobble on listened time** — accumulates pause/seek-aware wall-clock, not raw position | `usePlayer.ts` |
| 2 | **OS MediaSession** — media keys, lock screen, artwork, position scrubber | `useMediaSession.ts` (new), `Playbar.vue` |
| 9 | **Crossfeed DSP block** (Meier-style headphone crossfeed) — *neither repo had it* | `engine/dsp/crossfeed.ts` (new), `useAudioEngine.ts`, `useAudioSettings.ts`, `usePlayer.ts`, `EQPanel.vue` |
| — | **Debug logging** — scoped, colour-coded console narration of the engine; default ON, toggle via `heyaAudio.debug(false)` (persisted) | `engine/debug.ts` (new) + instrumented `usePlayer.ts`, `useAudioEngine.ts`, `scheduler.ts`, `deckManager.ts` |
| — | **Click-free manual switch** — hot-swapping a track previously (a) flushed the old decode buffer as a garbled burst, and (b) clicked from the gain stepping to zero on a hard pause. Fixed in two layers: pause + `removeAttribute('src')` before assigning the new src (no buffer flush), and a Jellyfin-style ~60ms gain fade-out before the swap + fade-in on the new track (no discontinuity). | `deck.ts`, `useAudioEngine.ts` |
| 13 | **Smart crossfade** — Go RMS-envelope boundary detection (`boundaries.go`, ported + unit-tested) writes intro/outro/fade/silence ms to `track_files` (migration 00033) from the loudness worker (skip-if-present; backfills existing libraries via the widened pending query). Flows through `SELECT *` → `TrackFile` → `/files`. `usePlayer` lazily fetches+caches `/files` analysis (also fixes spotty normalization threading), builds `BoundaryHints`, and arms `SmartCrossfade` via `scheduler.setSmartTransitionPoint`. New **Smart** mode in settings + EQ panel; falls back to timed when a track lacks boundaries. | `internal/sonicanalysis/boundaries.go`(+test), `migrations/00033`, `queries/music.sql`, `loudness_worker.go`, `usePlayer.ts`, `useAudioSettings.ts`, `EQPanel.vue` |
| — | **Gapless no-clip rework** — gapless now swaps on the outgoing deck's natural `ended` (track plays in full → no clipped tail), to the already-buffered+normalized pending deck. The scheduler's early fire is reserved for crossfade only (which plays through its fade to the real end). Fixes the bug where firing 100ms early reliably lopped the tail off every track. | `scheduler.ts`, `usePlayer.ts`, `useAudioEngine.ts` |

Repeat-one is now a seamless gapless loop. Pending-deck normalization is applied
before a crossfade so the incoming track plays at the right level during overlap.

**Needs verification by ear/eye** (can't be type-checked): gapless has no gap;
crossfade fades smoothly; album segues stay gapless; crossfeed audibly narrows
the stage on headphones; MediaSession shows on the OS lock screen.

### ⏭ Remaining backlog

| # | Item | Impact | Effort | Notes |
|---|------|--------|--------|-------|
| 10 | **Canvas visualizers** (spectrum/oscilloscope/VU/starfield) | High | M | Port Hibiki `FullscreenVisualizer.vue` `drawCanvas()` off the already-wired `analyserBridge`. Zero new deps. Wire into `NowPlayingView.vue`. |
| 6 | Sleep timer | Med | S | New `SleepTimerPopover.vue` → `player.stop()`. |
| 7 | Global transport hotkeys | Med | S | New `useGlobalHotkeys.ts` (Heya `seek()` is 0–1). |
| 16 | Output-device picker + per-device EQ profiles | Med | M | `context.ts` already ports `setAudioSinkId` (dead) — wire it + `devicechange`. |
| 15 | DSP chain reorder + independent per-block toggles | Med | M | Engine supports `reorderBlocks`/`toggleBlock`; settings layer collapses them. |
| 11 | Lyrics offset (±5s) + shared `useLyrics` composable | Med | S | Heya's binary-search active-line is good; just add offset + dedupe. |
| 17 | Track-info popover (codec/bitrate/norm-gain readout) | Med | S | |
| 22 | ISO-226 loudness compensation block | Med | M | New `dsp/loudnessCompensation.ts`, recompute on volume change. |
| 20 | Butterchurn/Milkdrop WebGL visualizer + preset browser | High | L | `bun add butterchurn butterchurn-presets`; dynamic import; `connectAudio(analyserBridge.analyserNode)`. Do after #10. |
| 24 | Configurable normalization target LUFS (currently hardcoded -18) | Low | S | |
| 23 | Codec-support diagnostics panel (consume the orphaned `useCodecSupport.ts`) | Low | S | |

### Known follow-up nits
- **Gapless has a tiny residual gap** (the pending element's `play()` latency on
  `ended`, ~tens of ms) — no clip, far better than a cold reload, but not
  sample-accurate. True zero-gap gapless needs decoding both tracks to
  `AudioBuffer`s and scheduling the next on the `AudioContext` clock — a sizable
  engine rewrite away from `MediaElementSource`. Revisit only if the seam is
  audible on segue-heavy albums.
- During a **crossfade** the incoming deck's gain ramps to unity (1.0), not to
  the user's volume, because the deck `gainNode` serves double duty as volume +
  fade gain (shared with Hibiki). Gapless (the default) is unaffected. A clean
  fix separates a volume gain from the crossfade gain in the engine.
- `prefetch.ts` (`PrefetchQueue` LRU) remains unused — single-track `loadNext`
  is the right depth. Either repurpose for `/file/{id}` warming or delete.
