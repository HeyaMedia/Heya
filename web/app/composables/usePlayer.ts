import { useAudioEngine } from '~/composables/useAudioEngine'
import { resumeContext } from '~/engine/context'
import { shouldSuppressCrossfade } from '~/engine/crossfade/albumAware'
import { SmartCrossfade } from '~/engine/crossfade/smartCrossfade'
import type { BoundaryHints, TransitionPlan } from '~/engine/crossfade/strategy'
import { TimeBasedCrossfade } from '~/engine/crossfade/timeBased'
import { alog } from '~/engine/debug'

// Track shape consumed by the player UI. `stream_url` is what the engine
// actually hits — derived from the track row in the caller (Phase A list
// endpoints set this to `/api/music/tracks/{id}/stream`).
export interface Track {
  id: number
  title: string
  artist: string
  album: string
  duration: number
  stream_url?: string
  track_file_id?: number
  album_id?: number
  artist_id?: number
  artist_slug?: string
  album_slug?: string
  poster?: string
  loved?: boolean
  // Origin label for the scrobble's `source` field — 'queue' | 'radio' |
  // 'album' | 'playlist' | 'search' | 'browse' | 'similar' | ''. Free-form;
  // analytics on /api/me/listening-stats can group by this.
  source?: string
  // True when this row is a live ICY stream (internet radio). The player
  // disables shuffle/repeat/prev/next and shows a "LIVE" badge for these.
  isStream?: boolean
  // Per-track replay-gain inputs. When present and replay-gain is on,
  // engine.setActiveNormalization applies a gain so playback hits the engine's
  // -18 LUFS target. NULL or missing => track plays at the file's native level.
  integrated_lufs?: number | null
  true_peak_db?: number | null
  // False when the track's file is gone from disk. The player refuses to play
  // or enqueue these; list pages should pre-filter, but this is the backstop.
  available?: boolean
}

// Last.fm-style scrobble threshold: a track counts as "played" once the user
// has *heard* at least this many seconds, OR the track has ended (whichever
// comes first). We accumulate wall-clock listened time, not raw position, so
// seeking forward past 30s never fakes a play.
const SCROBBLE_MIN_SECONDS = 30

// --- Client-only transition coordination (singletons) ----------------------
// These coordinate between prepareTransition() (which preloads the pending
// deck + arms the scheduler) and handleTransition() (fired by the scheduler
// ~100ms before a gapless cut, or `duration` seconds before a crossfade).
// They live at module scope so every usePlayer() closure and the once-wired
// engine callbacks share the same values. Client-only; SSR never touches them.
let transitioning = false
let prefetchedTrackId: number | null = null
let pendingNext: Track | null = null
let pendingMode: 'gapless' | 'crossfade' = 'gapless'
let pendingPlan: TransitionPlan | null = null
// Wall-clock seconds actually heard of the current track (pause/seek-aware).
let listenedSeconds = 0
let lastTickTime = 0
// Monotonic token bumped on each play(track). Captured before play()'s awaited
// analysis fetch so a stale request that resolves late can detect it's been
// superseded by a newer one and bail instead of clobbering the active deck.
let playGeneration = 0

// Minimal view of the real engine — useAudioEngine() returns a union with an
// SSR stub that lacks the DSP-block accessors. Everything here is only reached
// behind import.meta.client + engineWired, where the real instance is live.
type EngineWithScheduler = ReturnType<typeof useAudioEngine> & {
  scheduler?: {
    setMode: (m: 'gapless' | 'crossfade') => void
    setCrossfadeDuration: (s: number) => void
    setSmartTransitionPoint: (s: number | null) => void
  }
}

// Per-track playback analysis (loudness + structural boundaries) the player
// pulls from /api/music/tracks/{id} on demand and caches for the session. This
// is the single source of truth for normalization + smart-crossfade timing —
// list endpoints thread loudness inconsistently and never carry boundaries, so
// the player fetches it itself rather than depending on every list site. The
// track-detail endpoint also carries album loudness, which `album`/`auto`
// replay-gain modes need.
interface PlaybackData {
  integrated_lufs: number | null
  true_peak_db: number | null
  album_lufs: number | null
  album_peak: number | null
  fade_start_ms: number | null
  outro_start_ms: number | null
  silence_start_ms: number | null
  intro_end_ms: number | null
}
const playbackDataCache = new Map<number, PlaybackData | null>()

function toNum(v: unknown): number | null {
  if (v == null) return null
  const n = typeof v === 'number' ? v : Number.parseFloat(String(v))
  return Number.isFinite(n) ? n : null
}

// Fetch the primary file's loudness + boundaries + album loudness for a track.
// $heya is grabbed before the first await (the useNuxtApp-after-await hang trap).
// Returns null on any failure (un-analyzed track, 404) so callers fall back.
async function fetchTrackPlayback(trackId: number): Promise<PlaybackData | null> {
  if (trackId <= 0) return null
  try {
    const { $heya } = useNuxtApp()
    const detail = (await $heya('/api/music/tracks/{id}', { path: { id: trackId } })) as Record<string, unknown> & { files?: Array<Record<string, unknown>> }
    const f = detail.files?.[0] ?? {}
    return {
      integrated_lufs: toNum(f.integrated_lufs),
      true_peak_db: toNum(f.true_peak_db),
      album_lufs: toNum(detail.album_integrated_lufs),
      album_peak: toNum(detail.album_true_peak_db),
      fade_start_ms: toNum(f.fade_start_ms),
      outro_start_ms: toNum(f.outro_start_ms),
      silence_start_ms: toNum(f.silence_start_ms),
      intro_end_ms: toNum(f.intro_end_ms),
    }
  } catch {
    return null
  }
}

export function usePlayer() {
  const playing = useState('player_playing', () => false)
  const currentTrack = useState<Track | null>('player_track', () => null)
  const position = useState('player_position', () => 0)
  const duration = useState('player_duration', () => 0)
  const volume = useState('player_volume', () => 80)
  const muted = useState('player_muted', () => false)
  const shuffled = useState('player_shuffled', () => false)
  const repeatMode = useState<'off' | 'all' | 'one'>('player_repeat', () => 'off')
  const queue = useState<Track[]>('player_queue', () => [])
  // Pre-shuffle ordering, captured when shuffle turns on so it can be restored
  // (with already-played tracks kept up front) when shuffle turns off.
  const originalOrder = useState<Track[]>('player_original_order', () => [])
  const queueOpen = useState('player_queue_open', () => false)
  const lyricsOpen = useState('player_lyrics_open', () => false)
  const engineWired = useState('player_engine_wired', () => false)
  // Tracks the last track ID we already scrobbled this session so the listened
  // watcher, handleTransition, and handleEnded don't double-fire for one play.
  const scrobbledTrackId = useState<number | null>('player_scrobbled_track', () => null)
  // Sleep-timer "stop at end of track" flag. Owned by useSleepTimer; handleEnded
  // honors it (pause instead of advancing). Shared via useState to avoid an
  // import cycle between the player and the sleep timer.
  const sleepAtTrackEnd = useState('player_sleep_at_end', () => false)

  const settings = useAudioSettings()

  // Engine creation touches AudioContext, which the browser refuses to
  // instantiate before a user gesture. Defer it to the first play() call so
  // the autoplay-policy warning never fires on mount.
  function ensureEngine() {
    const e = useAudioEngine()
    if (import.meta.client && !engineWired.value) {
      engineWired.value = true
      e.setOnEnded(() => handleEnded())
      e.setOnError(() => { playing.value = false })
      // The scheduler fires this at the transition point (gapless: ~100ms
      // before end; crossfade: `duration` before end). Without it the entire
      // dual-deck gapless/crossfade machinery is inert and every track change
      // is a cold reload with an audible gap.
      e.setOnTransitionPoint(() => { void handleTransition() })
      watch(e.isPlaying, (v) => { playing.value = v })
      watch(e.currentTime, (t) => {
        position.value = t
        // Accumulate genuinely-heard time: only count small forward deltas,
        // so seeks (big jumps) and the 0-reset on track change don't inflate it.
        const dt = t - lastTickTime
        lastTickTime = t
        if (playing.value && dt > 0 && dt < 2) listenedSeconds += dt
        const tr = currentTrack.value
        if (tr && tr.id > 0 && scrobbledTrackId.value !== tr.id && listenedSeconds >= SCROBBLE_MIN_SECONDS) {
          scrobbledTrackId.value = tr.id
          void scrobbleTrack(tr, Math.floor(listenedSeconds), false)
        }
      })
      watch(e.duration, (v) => {
        if (Number.isFinite(v) && v > 0) duration.value = v
      })
      e.setVolume(muted.value ? 0 : volume.value / 100)
    }
    // Register the settings→engine bridge. Deliberately OUTSIDE the engineWired
    // guard and idempotent: a hot reload of useAudioSettings resets its
    // module-level applyToEngineFn to null while engineWired (a useState)
    // survives, so gating this on engineWired would permanently strand the
    // bridge after any HMR edit — EQ/crossfeed/replay-gain toggles would then
    // silently no-op. registerEngineBridge only (re)wires when the fn is missing.
    if (import.meta.client) {
      settings.registerEngineBridge(() => {
        applyAudioSettingsToEngine(e, settings)
        applyActiveNorm(e, currentTrack.value)
        prepareTransition()
      })
    }
    return e
  }

  // Scrobble through the unified /api/me/playback endpoint. Music tracks land
  // in the play_events history log server-side; videos go through the same
  // helper but with entity_type 'movie'/'episode' (see useVideoPlayer).
  async function scrobbleTrack(track: Track, listenedSecs: number, completed: boolean) {
    if (track.id <= 0) return
    alog('scrobble', `"${track.title}" — ${listenedSecs}s heard, completed=${completed}`)
    await recordPlayback({
      entity_type: 'track',
      entity_id: track.id,
      position_seconds: listenedSecs,
      total_seconds: track.duration || 0,
      completed,
      source: track.source ?? '',
    })
  }

  // --- Normalization (replay gain) -----------------------------------------
  // Mode lives in audio settings:
  //   off    => native level, no gain
  //   track  => each track's own EBU R128 gain toward the -18 LUFS target
  //   album  => the album's gain applied to every track, preserving the
  //             mastered inter-track dynamics (loud songs stay louder)
  //   auto   => track gain when shuffled, album gain otherwise
  // The decision is made per track-load (shuffling mid-track doesn't retro-
  // actively re-level the current track, which would jump the volume).
  function playbackOf(track: Track): PlaybackData {
    const cached = playbackDataCache.get(track.id)
    if (cached) return cached
    return {
      integrated_lufs: track.integrated_lufs ?? null,
      true_peak_db: track.true_peak_db ?? null,
      album_lufs: null,
      album_peak: null,
      fade_start_ms: null,
      outro_start_ms: null,
      silence_start_ms: null,
      intro_end_ms: null,
    }
  }
  // The (integrated, truePeak) pair to feed the engine for `track`, honoring the
  // replay-gain mode. Album/auto fall back to the track measurement when album
  // loudness hasn't been computed for the record yet.
  function effectiveLoudness(track: Track): { lufs: number; peak: number } | null {
    const mode = settings.replayGain.value.mode
    if (mode === 'off') return null
    const d = playbackOf(track)
    const useAlbum = mode === 'album' || (mode === 'auto' && !shuffled.value)
    if (useAlbum && d.album_lufs != null && d.album_peak != null) {
      return { lufs: d.album_lufs, peak: d.album_peak }
    }
    if (d.integrated_lufs != null && d.true_peak_db != null) {
      return { lufs: d.integrated_lufs, peak: d.true_peak_db }
    }
    return null
  }
  function applyActiveNorm(e: ReturnType<typeof useAudioEngine>, track: Track | null) {
    const eff = track ? effectiveLoudness(track) : null
    if (eff) e.setActiveNormalization(eff.lufs, eff.peak)
    else e.resetActiveNormalization()
  }
  function applyPendingNorm(e: ReturnType<typeof useAudioEngine>, track: Track) {
    const eff = effectiveLoudness(track)
    if (eff) e.setPendingNormalization(eff.lufs, eff.peak)
    else e.resetPendingNormalization()
  }

  // Build the outgoing-track boundary hints for smart crossfade. Returns
  // undefined when no boundaries are known (SmartCrossfade then times-falls-back).
  function boundaryHintsFor(track: Track): BoundaryHints | undefined {
    const d = playbackOf(track)
    if (d.fade_start_ms == null && d.outro_start_ms == null && d.silence_start_ms == null) {
      return undefined
    }
    const endMs = (track.duration || 0) * 1000
    return {
      outgoing: {
        fadeStartMs: d.fade_start_ms ?? 0,
        outroStartMs: d.outro_start_ms ?? 0,
        silenceStartMs: d.silence_start_ms ?? endMs,
      },
    }
  }

  // Fetch + cache analysis for a track if we haven't yet. Returns true when it
  // actually fetched (so callers can re-arm with the new data). Cached null
  // (un-analyzed) still counts as "known" — we don't refetch.
  async function ensurePlaybackData(trackId: number): Promise<boolean> {
    if (trackId <= 0 || playbackDataCache.has(trackId)) return false
    const data = await fetchTrackPlayback(trackId)
    playbackDataCache.set(trackId, data)
    return true
  }

  // The <audio> element can't carry an Authorization header, so the session
  // token has to ride in the query string. The auth middleware already
  // accepts ?token= as an alternative to Bearer.
  //
  // For /stream URLs (smart endpoint that picks best playable + transcodes
  // if needed) we also append the audio caps so the server can match what
  // this browser will actually decode. /file/{id} URLs are bit-perfect and
  // don't need the caps decoration.
  function resolveStreamUrl(t: Track): string | undefined {
    const base = t.stream_url ?? (t.id > 0 ? `/api/music/tracks/${t.id}/stream` : undefined)
    if (!base) return undefined

    const params = new URLSearchParams()
    const { token } = useAuth()
    if (token.value) params.set('token', token.value)

    // Smart-pick endpoint? Decorate with audio caps so the server can pick a
    // file the browser supports (or fall back to AAC-256 transcode).
    if (import.meta.client && base.endsWith('/stream')) {
      const caps = useClientCaps()
      for (const [key, val] of Object.entries(caps)) {
        if (key.startsWith('supports_') && val) params.set(key, '1')
      }
    }

    const sep = base.includes('?') ? '&' : '?'
    return params.toString() ? `${base}${sep}${params.toString()}` : base
  }

  // --- Transition orchestration --------------------------------------------
  // The deterministic next track in queue order (NOT a random pick) so the
  // preloaded pending deck matches what will actually play. repeat-one returns
  // the current track for a seamless gapless loop.
  function peekNextTrack(): Track | null {
    if (!queue.value.length) return null
    if (repeatMode.value === 'one') return currentTrack.value
    const idx = currentIndex.value
    if (idx < 0) return null
    const next = queue.value[idx + 1]
    if (next) return next
    return repeatMode.value === 'all' ? (queue.value[0] ?? null) : null
  }

  // Arm the next transition from the data we already have (synchronous): choose
  // gapless / timed-crossfade / smart, set the scheduler accordingly, and
  // preload the pending deck. Called after every play / advance / queue or
  // settings change, and re-run by ensureAnalysisAndArm once analysis lands.
  function armSync() {
    if (!import.meta.client || !engineWired.value) return
    const e = ensureEngine() as EngineWithScheduler
    pendingNext = null
    pendingMode = 'gapless'
    pendingPlan = null

    const cur = currentTrack.value
    if (!cur || cur.isStream) { e.scheduler?.setSmartTransitionPoint(null); return }

    const next = peekNextTrack()
    pendingNext = next
    if (!next) {
      e.scheduler?.setSmartTransitionPoint(null)
      alog('xfade', `arm: no next track (end of queue, repeat ${repeatMode.value})`)
      return
    }

    const cf = settings.crossfade.value
    let mode: 'gapless' | 'crossfade' | 'smart' = cf.mode
    let suppressed = false
    if ((mode === 'crossfade' || mode === 'smart') && cf.albumAware && next.id !== cur.id) {
      const same = shouldSuppressCrossfade(
        { albumId: cur.album_id, albumName: cur.album },
        { albumId: next.album_id, albumName: next.album },
      )
      if (same) { mode = 'gapless'; suppressed = true }
    }

    const curDur = cur.duration || e.duration.value || 0
    if (mode === 'smart' && curDur > 0) {
      // SmartCrossfade aligns the fade to the outgoing track's structure
      // (fade/outro/silence). Without boundaries it times-falls-back internally.
      pendingPlan = new SmartCrossfade(cf.durationSeconds).computeTransition(curDur, next.duration || 0, boundaryHintsFor(cur))
      pendingMode = 'crossfade'
      e.scheduler?.setSmartTransitionPoint(pendingPlan.startTimeSeconds)
    } else if (mode === 'crossfade' && curDur > 0) {
      pendingPlan = new TimeBasedCrossfade(cf.durationSeconds).computeTransition(curDur)
      pendingMode = 'crossfade'
      e.scheduler?.setSmartTransitionPoint(null)
      e.scheduler?.setMode('crossfade')
      e.scheduler?.setCrossfadeDuration(pendingPlan.durationSeconds)
    } else {
      pendingMode = 'gapless'
      e.scheduler?.setSmartTransitionPoint(null)
      e.scheduler?.setMode('gapless')
    }
    alog('xfade', `arm: next "${next.title}" → ${pendingMode === 'gapless' ? 'gapless' : mode}${suppressed ? ' (album-aware → gapless)' : ''}${pendingPlan ? ` start=${pendingPlan.startTimeSeconds.toFixed(1)}s dur=${pendingPlan.durationSeconds.toFixed(1)}s` : ''}`)

    // Buffer the next track onto the pending deck (streams excepted) and level
    // it. applyPendingNorm runs on EVERY arm — not just the first preload — so
    // the re-arm after the async /files fetch actually applies the now-known
    // loudness to the pending deck before a crossfade swaps it in. (Buffering
    // the deck only needs to happen once, hence its own guard.)
    if (!next.isStream) {
      applyPendingNorm(e, next)
      const url = resolveStreamUrl(next)
      if (url && prefetchedTrackId !== next.id) {
        prefetchedTrackId = next.id
        void e.loadNext(url).catch((err) => {
          alog('xfade', `preload FAILED for "${next.title}" — will cold-play`, err)
          if (prefetchedTrackId === next.id) prefetchedTrackId = null
        })
      }
    }
  }

  // Ensure the current + next tracks have their analysis fetched, then re-arm if
  // anything new arrived. Decoupled from armSync so the transition is armed
  // instantly (timed/gapless) and upgraded to correct normalization + smart
  // timing the moment /files responds. Idempotent — the cache stops re-fetching,
  // so the `changed` guard prevents an arm→fetch→arm loop.
  async function ensureAnalysisAndArm() {
    if (!import.meta.client || !engineWired.value) return
    const cur = currentTrack.value
    const next = peekNextTrack()
    let changed = false
    if (cur && !cur.isStream) changed = (await ensurePlaybackData(cur.id)) || changed
    if (next && !next.isStream && next.id !== cur?.id) changed = (await ensurePlaybackData(next.id)) || changed
    if (changed) {
      applyActiveNorm(ensureEngine(), currentTrack.value)
      armSync()
    }
  }

  // Arm immediately from known data, then fetch any missing analysis and re-arm.
  function prepareTransition() {
    armSync()
    void ensureAnalysisAndArm()
  }

  // Fired by the scheduler `crossfadeDuration` before the end — CROSSFADE ONLY.
  // The outgoing deck keeps playing (and fading) through to its real end during
  // the overlap, so nothing is clipped. Gapless is NOT handled here: pausing the
  // outgoing deck early would lop off its tail; it swaps on `ended` instead (see
  // handleEnded). Falls back to a cold play if the pending deck wasn't ready.
  async function handleTransition() {
    if (!import.meta.client || transitioning) return
    if (pendingMode !== 'crossfade') return // gapless handled on `ended`
    const e = ensureEngine()
    const cur = currentTrack.value
    if (!cur || cur.isStream) return
    const next = pendingNext
    if (!next) return

    transitioning = true
    const preloaded = prefetchedTrackId === next.id
    alog('xfade', `CROSSFADE → "${next.title}"${preloaded ? ' (preloaded ✓)' : ' (cold fallback — pending not ready)'}`)
    try {
      // The outgoing track is at ≈completion (it plays through the fade to its
      // real end). Scrobble it now — its deck `ended` won't fire post-swap.
      if (cur.id > 0 && scrobbledTrackId.value !== cur.id) {
        scrobbledTrackId.value = cur.id
        void scrobbleTrack(cur, Math.floor(listenedSeconds || cur.duration), true)
      }

      if (preloaded) {
        // 'timed' routes the pending deck through the signal chain so EQ/limiter
        // apply during the overlap.
        await e.transition('timed', pendingPlan ?? undefined)
      } else {
        const url = resolveStreamUrl(next)
        if (!url) { transitioning = false; return }
        applyActiveNorm(e, next)
        await e.play(url)
      }
      advanceCurrentTo(next)
    } catch (err) {
      alog('xfade', 'crossfade threw — falling back to plain skip', err)
      transitioning = false
      try { await skipToNextInternal() } catch { playing.value = false }
      return
    }
    transitioning = false
  }

  // Promote `next` to the current track after a deck swap and arm the hop
  // after it. The active deck already carries `next`'s normalization (set on
  // the pending deck before the swap), so no re-leveling here.
  function advanceCurrentTo(next: Track) {
    alog('player', `now playing "${next.title}" #${next.id} (advanced via deck swap)`)
    currentTrack.value = next
    position.value = 0
    scrobbledTrackId.value = null
    listenedSeconds = 0
    lastTickTime = 0
    prefetchedTrackId = null
    if (next.duration && Number.isFinite(next.duration)) duration.value = next.duration
    playing.value = true
    prepareTransition()
  }

  async function play(track?: Track) {
    const e = ensureEngine()
    if (track) {
      // Never play a track whose file was removed from disk.
      if (track.available === false) return
      // Manual play invalidates any armed transition / preloaded pending deck.
      transitioning = false
      prefetchedTrackId = null
      currentTrack.value = track
      const gen = ++playGeneration
      position.value = 0
      scrobbledTrackId.value = null
      listenedSeconds = 0
      lastTickTime = 0
      if (track.duration && Number.isFinite(track.duration)) duration.value = track.duration
      const url = resolveStreamUrl(track)
      if (!url) return
      alog('player', `play "${track.title}" #${track.id}${track.isStream ? ' (stream)' : ''}`)
      // Resume the AudioContext synchronously on the user gesture, BEFORE the
      // awaited fetch below — autoplay policy needs resume() within the gesture's
      // activation window, and a local fetch sits comfortably inside it.
      void resumeContext()
      // Block on analysis so the gain is right from the FIRST sample. Without
      // this, album/auto replay gain (which can differ a lot from track gain)
      // would apply a beat late after the async fetch and audibly jump. Auto-
      // advance doesn't need it — the pending deck is fetched + leveled ahead.
      if (track.id > 0 && !track.isStream) await ensurePlaybackData(track.id)
      // A newer play() superseded us during the fetch — bail rather than load a
      // stale track onto the active deck.
      if (gen !== playGeneration) return
      applyActiveNorm(e, track)
      try {
        await e.play(url)
      } catch {
        if (gen === playGeneration) playing.value = false
      }
      if (gen !== playGeneration) return
      // Preload the next track onto the pending deck for a gap-free hand-off.
      prepareTransition()
      return
    }
    // No track passed — resume current
    if (!currentTrack.value) return
    try {
      await e.resume()
    } catch {
      playing.value = false
    }
  }

  function pause() {
    if (!engineWired.value) return
    ensureEngine().pause()
  }

  async function togglePlay() {
    if (playing.value) pause()
    else await play()
  }

  // seek takes a 0-1 fraction (legacy API the UI uses).
  function seek(pct: number) {
    const target = Math.max(0, Math.min(1, pct)) * (duration.value || 0)
    if (engineWired.value) ensureEngine().seek(target)
    position.value = target
    lastTickTime = target
  }

  function setVolume(v: number) {
    const clamped = Math.max(0, Math.min(100, v))
    volume.value = clamped
    if (clamped > 0) muted.value = false
    if (engineWired.value) ensureEngine().setVolume(muted.value ? 0 : clamped / 100)
  }

  function toggleMute() {
    muted.value = !muted.value
    if (engineWired.value) ensureEngine().setVolume(muted.value ? 0 : volume.value / 100)
  }

  // --- Shuffle (reorders the queue in place) -------------------------------
  // The played + currently-playing tracks stay fixed; only the upcoming slice
  // is shuffled / restored.
  function upcomingStart() {
    return currentIndex.value >= 0 ? currentIndex.value + 1 : 0
  }
  function shuffleUpcoming() {
    const start = upcomingStart()
    if (start >= queue.value.length) return
    const head = queue.value.slice(0, start)
    const upcoming = queue.value.slice(start)
    for (let i = upcoming.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1))
      ;[upcoming[i], upcoming[j]] = [upcoming[j]!, upcoming[i]!]
    }
    queue.value = [...head, ...upcoming]
  }
  // Restore the pre-shuffle ordering, reconciled against the *current* queue so
  // edits made while shuffled survive: tracks removed during shuffle stay gone,
  // tracks added during shuffle are kept (appended after the restored run in
  // their current order). Only the upcoming slice is reordered.
  function restoreOriginalOrder() {
    if (!originalOrder.value.length) return
    const start = upcomingStart()
    const head = queue.value.slice(0, start)
    const upcomingNow = queue.value.slice(start)
    const upcomingIds = new Set(upcomingNow.map((t) => t.id))
    // Original ordering, but only for tracks still upcoming in the live queue.
    const restored = originalOrder.value.filter((t) => upcomingIds.has(t.id))
    const restoredIds = new Set(restored.map((t) => t.id))
    // Tracks queued while shuffled weren't in the snapshot — keep them.
    const added = upcomingNow.filter((t) => !restoredIds.has(t.id))
    queue.value = [...head, ...restored, ...added]
    originalOrder.value = []
  }
  function toggleShuffle() {
    shuffled.value = !shuffled.value
    if (shuffled.value) {
      originalOrder.value = [...queue.value]
      shuffleUpcoming()
    } else {
      restoreOriginalOrder()
    }
    prepareTransition()
  }

  function cycleRepeat() {
    const modes: Array<'off' | 'all' | 'one'> = ['off', 'all', 'one']
    const idx = modes.indexOf(repeatMode.value)
    repeatMode.value = modes[(idx + 1) % modes.length]!
    prepareTransition()
  }

  // The next track in queue order for a MANUAL skip — ignores repeat-one (the
  // user wants to move on), wraps when repeat-all.
  function forwardNext(): Track | null {
    if (!queue.value.length) return null
    const idx = currentIndex.value
    const next = idx >= 0 ? queue.value[idx + 1] : queue.value[0]
    if (next) return next
    return repeatMode.value !== 'off' ? (queue.value[0] ?? null) : null
  }

  // Plain cold advance — used as a fallback by the transition handlers.
  async function skipToNextInternal() {
    const next = forwardNext()
    if (next) await play(next)
    else playing.value = false
  }

  // Manual "next": if the next track is already buffered on the pending deck,
  // swap to it instantly (no cold-load gap, no src-clobber); otherwise cold play.
  async function nextTrack() {
    const next = forwardNext()
    if (!next) { playing.value = false; return }
    if (playing.value && !transitioning && prefetchedTrackId === next.id && !next.isStream) {
      const e = ensureEngine()
      transitioning = true
      try {
        alog('player', `skip → "${next.title}" (instant, preloaded ✓)`)
        await e.transition('gapless')
        advanceCurrentTo(next)
        transitioning = false
        return
      } catch (err) {
        alog('player', 'instant skip failed — cold play', err)
        transitioning = false
      }
    }
    await play(next)
  }

  async function prevTrack() {
    if (position.value > 3) {
      if (engineWired.value) ensureEngine().seek(0)
      position.value = 0
      lastTickTime = 0
      return
    }
    if (!queue.value.length) return
    const idx = currentIndex.value
    const prev = queue.value[(idx - 1 + queue.value.length) % queue.value.length]
    if (prev) await play(prev)
  }

  // Fires on the outgoing deck's natural `ended`. This is the GAPLESS path: the
  // track has played in full (no clip), and the next track is already buffered
  // on the pending deck, so we swap to it immediately — the only gap is the
  // element's play() latency (a few ms), not a cold reload. Crossfade never
  // reaches here (it swaps early via handleTransition and clears the old deck's
  // events). repeat-one loops through the same preloaded-swap path.
  async function handleEnded() {
    if (transitioning) return
    const finished = currentTrack.value
    if (finished && finished.id > 0 && scrobbledTrackId.value !== finished.id) {
      scrobbledTrackId.value = finished.id
      void scrobbleTrack(finished, Math.floor(listenedSeconds || finished.duration), true)
    }

    // Sleep timer set to "end of track" — stop here instead of advancing.
    if (sleepAtTrackEnd.value) {
      sleepAtTrackEnd.value = false
      pause()
      alog('player', 'sleep timer: stopped at end of track')
      return
    }

    const next = peekNextTrack() // queue order; returns current for repeat-one
    if (!next) {
      alog('player', `queue ended after "${finished?.title}"`)
      playing.value = false
      return
    }

    const e = ensureEngine()
    transitioning = true
    try {
      if (prefetchedTrackId === next.id && !next.isStream) {
        // Pending deck already holds `next`, buffered and normalized — gapless
        // swap with no cold-load gap.
        alog('player', `gapless swap → "${next.title}" (preloaded ✓)`)
        await e.transition('gapless')
      } else {
        // Not preloaded (queue changed, stream, or preload failed) — cold play.
        alog('player', `advance → "${next.title}" (cold load — not preloaded)`)
        const url = resolveStreamUrl(next)
        if (!url) { transitioning = false; playing.value = false; return }
        applyActiveNorm(e, next)
        await e.play(url)
      }
      advanceCurrentTo(next)
    } catch (err) {
      alog('player', 'ended-advance failed', err)
      transitioning = false
      playing.value = false
      return
    }
    transitioning = false
  }

  function toggleLoved() {
    if (currentTrack.value) {
      currentTrack.value = { ...currentTrack.value, loved: !currentTrack.value.loved }
    }
  }

  function toggleQueue() { queueOpen.value = !queueOpen.value }
  function toggleLyrics() { lyricsOpen.value = !lyricsOpen.value }

  function formatTime(s: number) {
    if (!Number.isFinite(s)) return '0:00'
    const m = Math.floor(s / 60)
    const sec = Math.floor(s % 60)
    return `${m}:${sec.toString().padStart(2, '0')}`
  }

  // --- Queue management (Played / Now Playing / Up Next semantics) ---
  // Lifted from hibiki's player store so the right sidebar can render the
  // three sections plus drag / remove. `currentIndex` is the index of the
  // playing track in `queue`; -1 when nothing is playing. We derive it from
  // `currentTrack.id` rather than storing both — single source of truth.
  const currentIndex = computed(() => {
    const t = currentTrack.value
    if (!t) return -1
    return queue.value.findIndex((x) => x.id === t.id)
  })
  const playedTracks = computed(() => {
    const idx = currentIndex.value
    return idx > 0 ? queue.value.slice(0, idx) : []
  })
  const upcomingTracks = computed(() => {
    const idx = currentIndex.value
    return idx >= 0 ? queue.value.slice(idx + 1) : []
  })
  const upcomingCount = computed(() => upcomingTracks.value.length)

  // jumpTo plays the queue item at absolute index, treating the queue as
  // the authoritative ordering. Used by the right-sidebar rows.
  async function jumpTo(index: number) {
    const t = queue.value[index]
    if (!t) return
    await play(t)
  }

  // removeFromQueue drops one upcoming track. Guard against removing the
  // current or played items — the sidebar only exposes the up-next bucket
  // anyway, but the guard keeps callers honest.
  function removeFromQueue(index: number) {
    if (index <= currentIndex.value) return
    if (index >= queue.value.length) return
    queue.value.splice(index, 1)
    prepareTransition()
  }

  // moveInQueue reorders an upcoming track. Same guards as remove.
  function moveInQueue(from: number, to: number) {
    if (from <= currentIndex.value || to <= currentIndex.value) return
    if (from >= queue.value.length || to >= queue.value.length) return
    if (from === to) return
    const next = queue.value.slice()
    const [item] = next.splice(from, 1)
    if (item) next.splice(to, 0, item)
    queue.value = next
    prepareTransition()
  }

  // addToQueue appends one or more tracks to the end of the queue. When the
  // queue is empty the first added track becomes "now playing" so the user
  // gets immediate feedback. De-dupes by track id against the up-next slice
  // so spamming "Add to Queue" doesn't pile up duplicates.
  async function addToQueue(tracks: Track | Track[]) {
    const list = (Array.isArray(tracks) ? tracks : [tracks]).filter((t) => t.available !== false)
    if (!list.length) return
    if (!queue.value.length) {
      queue.value = list
      await play(list[0]!)
      return
    }
    const upcomingIds = new Set(upcomingTracks.value.map((t) => t.id))
    const fresh = list.filter((t) => !upcomingIds.has(t.id))
    if (!fresh.length) return
    queue.value = [...queue.value, ...fresh]
    prepareTransition()
  }

  // playNext inserts one or more tracks immediately after the currently-
  // playing one (or at the head if nothing's playing). Mirrors Spotify /
  // Apple Music "Play Next". Same de-dupe behavior as addToQueue.
  async function playNext(tracks: Track | Track[]) {
    const list = (Array.isArray(tracks) ? tracks : [tracks]).filter((t) => t.available !== false)
    if (!list.length) return
    if (!queue.value.length) {
      queue.value = list
      await play(list[0]!)
      return
    }
    const upcomingIds = new Set(upcomingTracks.value.map((t) => t.id))
    const fresh = list.filter((t) => !upcomingIds.has(t.id))
    if (!fresh.length) return
    const idx = currentIndex.value
    const insertAt = idx < 0 ? 0 : idx + 1
    const next = queue.value.slice()
    next.splice(insertAt, 0, ...fresh)
    queue.value = next
    prepareTransition()
  }

  // clearUpcoming empties everything after the current track. Used by the
  // sidebar's "Clear" button on the Up Next header.
  function clearUpcoming() {
    const idx = currentIndex.value
    if (idx < 0) {
      queue.value = []
      originalOrder.value = []
      return
    }
    queue.value = queue.value.slice(0, idx + 1)
    originalOrder.value = []
    prepareTransition()
  }

  // stop unloads the engine + clears state. Used by the playbar long-press.
  function stop() {
    if (engineWired.value) ensureEngine().stop()
    playing.value = false
    currentTrack.value = null
    queue.value = []
    originalOrder.value = []
    position.value = 0
    duration.value = 0
    transitioning = false
    prefetchedTrackId = null
    pendingNext = null
    listenedSeconds = 0
    lastTickTime = 0
  }

  return {
    playing, currentTrack, position, duration, volume, muted,
    shuffled, repeatMode, queue, queueOpen, lyricsOpen,
    currentIndex, playedTracks, upcomingTracks, upcomingCount,
    play, pause, togglePlay, seek, setVolume, toggleMute, stop,
    toggleShuffle, cycleRepeat, nextTrack, prevTrack,
    toggleLoved, toggleQueue, toggleLyrics, formatTime,
    jumpTo, removeFromQueue, moveInQueue, clearUpcoming,
    addToQueue, playNext,
  }
}

// applyAudioSettingsToEngine pushes the persisted EQ state into the engine.
// Crossfade/scheduler concerns are owned by usePlayer.prepareTransition (it
// needs per-track context for album-aware suppression + plan timing), so they
// are intentionally NOT set here. Idempotent — re-applied on every mutation.
function applyAudioSettingsToEngine(engine: ReturnType<typeof useAudioEngine>, settings: ReturnType<typeof useAudioSettings>) {
  // The SSR stub lacks the chain block accessors; bail when they're missing.
  const e = engine as ReturnType<typeof useAudioEngine> & {
    equalizer?: { enabled: boolean; setAllBands: (b: number[]) => void }
    preamp?: { enabled: boolean; setGain: (db: number) => void }
    postgain?: { enabled: boolean; setGain: (db: number) => void }
    crossfeed?: { enabled: boolean; setPreset: (p: 'subtle' | 'natural' | 'strong') => void }
    limiter?: { enabled: boolean }
    signalChain?: { rebuild: () => void; reorderBlocks: (names: string[]) => void }
  }
  const eq = settings.eq.value
  if (e.equalizer) {
    e.equalizer.setAllBands(eq.bands)
    e.equalizer.enabled = eq.enabled
  }
  if (e.preamp) {
    e.preamp.setGain(eq.preamp)
    e.preamp.enabled = eq.enabled
  }
  if (e.postgain) {
    e.postgain.setGain(eq.postgain)
    e.postgain.enabled = eq.enabled
  }
  const cfeed = settings.crossfeed.value
  if (e.crossfeed) {
    e.crossfeed.setPreset(cfeed.preset)
    e.crossfeed.enabled = cfeed.enabled
  }
  const chain = settings.dspChain.value
  if (e.limiter) e.limiter.enabled = chain.limiterEnabled
  alog('dsp', `apply: EQ ${eq.enabled ? 'on' : 'off'} · crossfeed ${cfeed.enabled ? cfeed.preset : 'off'} · limiter ${chain.limiterEnabled ? 'on' : 'off'} · order [${chain.order.join(' → ')}]`)
  // Reorder the effect blocks. The EQ's preamp/postgain travel with 'equalizer'.
  // reorderBlocks pins the normalization head + limiter tail and rebuilds, so
  // the toggles above take effect in the same pass.
  if (e.signalChain) {
    const middle: string[] = []
    for (const id of chain.order) {
      if (id === 'equalizer') middle.push('preamp', 'equalizer', 'postgain')
      else if (id === 'crossfeed') middle.push('crossfeed')
    }
    e.signalChain.reorderBlocks(middle)
  }
}
