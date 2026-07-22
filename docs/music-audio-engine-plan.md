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
| 14 | Album-continuity suppression: adjacent tracks on the same release and repeat-one loops are always gapless | `usePlayer.ts`, `engine/crossfade/albumAware.ts`, `EQPanel.vue` |
| 4 | **Fixed shuffle** — reorders the upcoming queue in place + `originalOrder` restore (also fixes prevTrack / Up-Next mismatch) | `usePlayer.ts` |
| 3 | **Fixed ReplayGain + real album/auto** — `off` truly disables; `track`/`album`/`auto` now use the right loudness. Album LUFS (already computed in Go) is exposed via the track-detail query (`al.integrated_lufs/true_peak_db` on `GetTrackDetailByID`); the player fetches `/api/music/tracks/{id}` (track loudness + boundaries + album loudness in one call), and `effectiveLoudness()` picks track vs album by mode (`auto` = track on shuffle, album otherwise), decided per track-load. Quality popover shows track + album LUFS and the applied gain with its source. | `queries/music.sql`, `usePlayer.ts`, `useAudioSettings.ts`, `PlaybarQuality.vue` |
| 12 | **Scrobble on listened time** — accumulates pause/seek-aware wall-clock, not raw position | `usePlayer.ts` |
| 2 | **OS MediaSession** — media keys, lock screen, artwork, position scrubber | `useMediaSession.ts` (new), `Playbar.vue` |
| 9 | **Crossfeed DSP block** (Meier-style headphone crossfeed) — *neither repo had it* | `engine/dsp/crossfeed.ts` (new), `useAudioEngine.ts`, `useAudioSettings.ts`, `usePlayer.ts`, `EQPanel.vue` |
| — | **Debug logging** — scoped, colour-coded console narration of the engine; default ON, toggle via `heyaAudio.debug(false)` (persisted) | `engine/debug.ts` (new) + instrumented `usePlayer.ts`, `useAudioEngine.ts`, `scheduler.ts`, `deckManager.ts` |
| — | **Click-free manual switch** — hot-swapping a track previously (a) flushed the old decode buffer as a garbled burst, and (b) clicked from the gain stepping to zero on a hard pause. Fixed in two layers: pause + `removeAttribute('src')` before assigning the new src (no buffer flush), and a Jellyfin-style ~60ms gain fade-out before the swap + fade-in on the new track (no discontinuity). | `deck.ts`, `useAudioEngine.ts` |
| 13 | **Smart crossfade** — Go RMS-envelope boundary detection (`boundaries.go`, ported + unit-tested) writes intro/outro/fade/silence ms to `track_files` (migration 00033) from the loudness worker (skip-if-present; backfills existing libraries via the widened pending query). Flows through `SELECT *` → `TrackFile` → `/files`. `usePlayer` lazily fetches+caches `/files` analysis (also fixes spotty normalization threading), builds `BoundaryHints`, and arms `SmartCrossfade` via `scheduler.setSmartTransitionPoint`. New **Smart** mode in settings + EQ panel; falls back to timed when a track lacks boundaries. | `internal/sonicanalysis/boundaries.go`(+test), `migrations/00033`, `queries/music.sql`, `loudness_worker.go`, `usePlayer.ts`, `useAudioSettings.ts`, `EQPanel.vue` |
| — | **Gapless no-clip rework** — gapless now swaps on the outgoing deck's natural `ended` (track plays in full → no clipped tail), to the already-buffered+normalized pending deck. The scheduler's early fire is reserved for crossfade only (which plays through its fade to the real end). Fixes the bug where firing 100ms early reliably lopped the tail off every track. | `scheduler.ts`, `usePlayer.ts`, `useAudioEngine.ts` |

### ✅ Done — quick wins, DSP chain, output devices, visualizer (2026-07-01)

| # | Item | Files |
|---|------|-------|
| 6 | **Sleep timer** — end-of-track / timed stop | `useSleepTimer.ts`, `SleepTimer.vue` (new), `usePlayer.ts` |
| 7 | **Global transport hotkeys** + `?` help modal | `useGlobalHotkeys.ts`, `HotkeyHelp.vue` (new) |
| 15 | **DSP chain reorder + per-block toggles** — user-reorderable effect blocks (EQ/crossfeed) between pinned normalization head + limiter tail; independent toggles; limiter safety switch | `useAudioSettings.ts` (`DspChainState`, `moveDspBlock`, `setLimiterEnabled`), `usePlayer.ts` (`reorderBlocks`), `EQPanel.vue` (Effects tab "Signal chain") |
| — | **EQ panel tabs + real EQ hookup** — split into Equalizer / Playback / Effects / Output tabs; fixed the master EQ toggle (reka `SwitchRoot` uses `modelValue`, not `checked` — `AppSwitch` was silently no-op) and the HMR bridge desync that left the engine unwired | `EQPanel.vue`, `AppSwitch.vue`, `useAudioSettings.ts`, `usePlayer.ts` |
| 16 | **Output-device picker + per-device EQ profiles** — enumerate `audiooutput`, route via `setSinkId`, store a per-device `AudioProfile` (EQ + crossfeed) in localStorage, re-apply on select/hot-plug (500ms-debounced `devicechange`). Opt-in (no surprise flat reset), labels revealed on demand (no forced mic prompt), graceful Safari/Firefox fallback | `useAudioDevices.ts` (new), `useAudioSettings.ts` (`AudioProfile`, `applyAudioProfile`/`currentAudioProfile`), `EQPanel.vue` (Output tab), `shared/types/audio.ts` |
| 10 | **Canvas visualizers** — log-frequency spectrum bars + oscilloscope off the wired `analyserBridge`; a compact `mini` variant is the live playbar VU meter (doubles as the visualizer entry button) | `VisualizerSpectrum.vue` (new) |
| 20 | **Butterchurn/Milkdrop WebGL visualizer + immersive fullscreen** — `butterchurn` + `butterchurn-presets` (dynamic import, client-only), taps `analyserBridge.analyserNode`; preset nav / random / favorite / auto-cycle; render-scale dial; auto-hiding controls + transport; `v` opens, `←/→/r/Esc` drive it | `VisualizerMilkdrop.vue`, `VisualizerFullscreen.vue` (new), `useVisualizer.ts` (new), `Playbar.vue`, `NowPlayingView.vue`, `music.vue`, `useGlobalHotkeys.ts` |
| 21 | **Fullscreen overhaul + preset browser** (2026-07-01) — hibiki-style bottom command bar (track + interactive seek + transport + preset controls + mode pills + native fullscreen), a slide-in preset browser (search, All/Liked/Recent tabs, favorites floated to top, per-row heart), a **VU** mode (stereo LED meter), a **liked-only** cycle/nav toggle (random + sequential + `←/→` draw only from favorites), and richer keys (`[ ]` preset, `o` browser, `t` random, `f` native fullscreen, `1–4` mode) | `VisualizerFullscreen.vue`, `VisualizerPresetBrowser.vue` (new), `VisualizerSpectrum.vue` (VU), `VisualizerMilkdrop.vue` (liked-only pool), `useVisualizer.ts` (`vu`, `likedOnly`), `HotkeyHelp.vue` |
| — | **Spectrum DSP rewrite** (2026-07-01, ✅ verified by eye) — killed the pegged-bass artifact: fftSize 2048→8192 (~5.4 Hz/bin resolves the low end), log-frequency bands mapped by Hz (interpolate-then-max, no bin duplication), absolute −80…−20 dB window (silence clamps to 0, no frame-normalization phantom bass) | `analyserBridge.ts`, `VisualizerSpectrum.vue` |
| — | **Volume polish** (2026-07-01) — volume + mute persist across reloads (localStorage `heya_player_volume_v1`); scroll-wheel over either volume control nudges ±5; **hold** play/pause 3s = stop playback + clear queue — after 1s the button flips to a stop icon and a red ring closes clockwise from 12 o'clock over the final 2s, then fires (trailing click suppressed) | `usePlayer.ts`, `Playbar.vue`, `NowPlayingView.vue`, `Icon.vue` (`stop`) |

**Needs verification by eye** (2026-07-01 batch): Output tab lists real devices + switching routes audio + profiles persist/reapply; Milkdrop renders (WebGL2) and reacts to audio; scope mode draws; playbar mini-VU animates with playback; auto-hide + preset hotkeys feel right. (Spectrum + VU confirmed working.)

Repeat-one is now a seamless gapless loop. Pending-deck normalization is applied
before a crossfade so the incoming track plays at the right level during overlap.

**Needs verification by ear/eye** (can't be type-checked): gapless has no gap;
crossfade fades smoothly; album segues stay gapless; crossfeed audibly narrows
the stage on headphones; MediaSession shows on the OS lock screen.

### ⏭ Remaining backlog

| # | Item | Impact | Effort | Notes |
|---|------|--------|--------|-------|
| 25 | **Render-scale control** in the visualizer (auto-cycle interval/mode already surfaced in the preset browser) | Low | S | `useVisualizer.setRenderScale` exists; add a small slider (GPU cost dial) to the preset browser or a settings popover. |
| 11 | Lyrics offset (±5s) + shared `useLyrics` composable | Med | S | Heya's binary-search active-line is good; just add offset + dedupe. |
| 17 | Track-info popover (codec/bitrate/norm-gain readout) | Med | S | |
| 22 | ISO-226 loudness compensation block | Med | M | New `dsp/loudnessCompensation.ts`, recompute on volume change. |
| 24 | Configurable normalization target LUFS (currently hardcoded -18) | Low | S | |
| 23 | Codec-support diagnostics panel (consume the orphaned `useCodecSupport.ts`) | Low | S | |
| — | Extra fullscreen modes (starfield, particle) | Low | M | `VisualizerSpectrum` variant pattern makes new canvas modes cheap; add to `VisMode` + `MODES`. |

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
