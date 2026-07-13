import { useAudioEngine } from '~/composables/useAudioEngine'
import type { QueueItem, QueueSourceInput } from '~/composables/useQueue'
import { resumeContext } from '~/engine/context'
import { shouldSuppressCrossfade } from '~/engine/crossfade/albumAware'
import { SmartCrossfade } from '~/engine/crossfade/smartCrossfade'
import type { BoundaryHints, TransitionPlan } from '~/engine/crossfade/strategy'
import { TimeBasedCrossfade } from '~/engine/crossfade/timeBased'
import { alog } from '~/engine/debug'
import { prefetchManager } from '~/engine/prefetch'
import { acceptHMRUpdate, defineStore, storeToRefs } from 'pinia'

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
  source?: string
  isStream?: boolean
  integrated_lufs?: number | null
  true_peak_db?: number | null
  available?: boolean
}

// Track shape consumed by the player UI. `stream_url` is what the engine
// actually hits — derived from the track row in the caller (Phase A list
// endpoints set this to `/api/music/tracks/{id}/stream`).
// Last.fm-style scrobble threshold: a track counts as "played" once the user
// has *heard* at least this many seconds, OR the track has ended (whichever
// comes first). We accumulate wall-clock listened time, not raw position, so
// seeking forward past 30s never fakes a play.
const SCROBBLE_MIN_SECONDS = 30

// Volume + mute survive reloads via localStorage. useState seeds a default so
// the slider has a value before the client restore runs; setVolume/toggleMute
// are the only mutators, so persistence hangs off them (no watcher needed).
// SPA-only app (ssr: false), so the synchronous client restore below can't
// cause a hydration mismatch.
const VOLUME_STORAGE_KEY = 'heya_player_volume_v1'
let volumeRestored = false

function persistVolumePrefs(volume: number, muted: boolean) {
  if (import.meta.server) return
  try {
    localStorage.setItem(VOLUME_STORAGE_KEY, JSON.stringify({ volume, muted }))
  } catch { /* private mode / quota — non-fatal */ }
}

// --- Client-only transition coordination (singletons) ----------------------
// These coordinate between prepareTransition() (which preloads the pending
// deck + arms the scheduler) and handleTransition() (fired by the scheduler
// ~100ms before a gapless cut, or `duration` seconds before a crossfade).
// They live at module scope so every usePlayerBindings() closure and the once-wired
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
// Set by stopCasting(): the local engine has nothing loaded (or something
// stale), so the next resume must cold-load the track and seek to where the
// cast session left off instead of resuming a dead deck.
let localHandoff: { trackId: number, position: number } | null = null

// Debounces prefetchManager.sync() calls triggered by a `watch(upcomingTracks)`
// (registered once in ensureEngine, below) — covers track advance, add/
// remove, AND drag-reorder with one coalescing timer, since all of them
// reshape the up-next slice. See docs/music-audio-engine-plan.md for the
// prefetch backlog item this replaces. 2s is imperceptible against tracks
// that play for minutes, and far shorter than the ~10-30s a cold
// transcode-tier fetch can take, which is exactly the latency this hides.
const PREFETCH_SYNC_DEBOUNCE_MS = 2000
let prefetchSyncTimer: ReturnType<typeof setTimeout> | null = null
// `current` is threaded straight to prefetchManager.sync so it can retain the
// actively-playing track even though it's never part of `upcoming` (the
// upcoming list excludes the current track by definition).
function schedulePrefetchSync(upcoming: Track[], current: Track | null) {
  if (!import.meta.client) return
  if (prefetchSyncTimer) clearTimeout(prefetchSyncTimer)
  prefetchSyncTimer = setTimeout(() => {
    prefetchSyncTimer = null
    // Remote output: the server feeds the receiver directly — warming the
    // browser cache would just download tracks nobody here will play.
    if (useCastStore().engaged) return
    const { settings: deviceSettings } = useDeviceSettings()
    void prefetchManager.sync(upcoming.slice(0, deviceSettings.value.prefetchCount), current)
  }, PREFETCH_SYNC_DEBOUNCE_MS)
}

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
  boundaries_ready: boolean
}
const playbackDataCache = new Map<number, PlaybackData | null>()
const playbackAnalysisRetries = new Map<number, number>()

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
      boundaries_ready: f.boundaries_analyzed_at != null,
    }
  } catch {
    return null
  }
}

export const usePlayerStore = defineStore('player', () => {
  const playing = ref(false)
  const currentTrack = ref<Track | null>(null)
  const position = ref(0)
  const duration = ref(0)
  const volume = ref(80)
  const muted = ref(false)
  const queueOpen = ref(false)
  const sideTab = ref<'queue' | 'lyrics'>('queue')
  const engineWired = ref(false)
  const scrobbledTrackId = ref<number | null>(null)
  const sleepAtTrackEnd = ref(false)
  const sleepDeadline = ref<number | null>(null)
  const sleepNowTick = ref(0)

  // --- Server-owned queue facade (docs/queue-plan.md Phase B) --------------
  // The queue lives server-side (useQueue windowed mirror); `queue` here is
  // a compatibility computed so the 40+ existing call sites that do
  // `queue.value = tracks; play(track)` keep working — the setter stages a
  // server replace that the following play() call finalizes with the right
  // start track. Radio streams / podcast episodes aren't music-track rows,
  // so any list containing them flips to a LOCAL queue (the pre-Phase-B
  // array behavior) — the server never hears about those.
  const qs = useQueueStore()
  const localMode = ref(false)
  const localQueue = ref<Track[]>([])
  const originalOrder = ref<Track[]>([]) // local-mode shuffle restore
  const localShuffled = ref(false)
  const localRepeat = ref<'off' | 'all' | 'one'>('off')

  function itemToTrack(i: QueueItem): Track {
    return {
      id: i.track_id,
      title: i.title,
      artist: i.artist_name,
      album: i.album_title,
      duration: i.duration,
      album_id: i.album_id,
      artist_id: i.artist_id,
      artist_slug: i.artist_slug,
      album_slug: i.album_slug,
      poster: useAlbumCoverUrl(i.artist_slug, i.album_slug) ?? undefined,
    }
  }
  const serverQueueTracks = computed(() => qs.items.map(itemToTrack))

  // The output the server should attribute playback to: the cast device
  // while casting, this tab otherwise (Phase C moves cast fully onto this).
  function queueOutputID(): string {
    const cast = useCastStore()
    return cast.engaged && cast.engagedDeviceId ? `cast:${cast.engagedDeviceId}` : qs.outputID
  }

  // Read-only: the queue is server state (or the local stream list). It
  // changes through playContext/playTracks/playLocal and the queue ops —
  // never by assignment.
  const queue = computed<Track[]>(() =>
    localMode.value ? localQueue.value : serverQueueTracks.value)

  const shuffled = computed<boolean>({
    get: () => (localMode.value ? localShuffled.value : qs.shuffled),
    set: (v) => {
      if (localMode.value) localShuffled.value = v
      else void qs.setShuffle(v)
    },
  })
  const repeatMode = computed<'off' | 'all' | 'one'>({
    get: () => (localMode.value ? localRepeat.value : qs.repeatMode),
    set: (v) => {
      if (localMode.value) localRepeat.value = v
      else void qs.setRepeat(v)
    },
  })

  // Index of the current track within the exposed `queue` array. Server
  // mode uses the pointer (duplicate tracks in a queue stay unambiguous);
  // local mode keeps the old find-by-id.
  const currentIndex = computed(() => {
    if (!localMode.value) return qs.currentWindowIndex
    const track = currentTrack.value
    return track ? localQueue.value.findIndex((item) => item.id === track.id) : -1
  })
  const playedTracks = computed(() => currentIndex.value > 0 ? queue.value.slice(0, currentIndex.value) : [])
  const upcomingTracks = computed(() => currentIndex.value >= 0 ? queue.value.slice(currentIndex.value + 1) : [])
  const upcomingCount = computed(() => upcomingTracks.value.length)
  const nextUp = computed(() => upcomingTracks.value[0] ?? null)
  const progress = computed(() => duration.value > 0 ? Math.max(0, Math.min(1, position.value / duration.value)) : 0)
  const hasPrevious = computed(() => currentIndex.value > 0 || position.value > 3)
  const hasNext = computed(() => upcomingCount.value > 0 || (repeatMode.value !== 'off' && queue.value.length > 0))

  // Restore persisted volume/mute once on the client. useState is a singleton,
  // so a single assignment propagates to every usePlayerBindings() consumer; the flag
  // keeps repeat calls (one per mounting component) from re-reading storage.
  if (import.meta.client && !volumeRestored) {
    volumeRestored = true
    try {
      const raw = localStorage.getItem(VOLUME_STORAGE_KEY)
      if (raw) {
        const p = JSON.parse(raw) as { volume?: number, muted?: boolean }
        if (typeof p.volume === 'number' && Number.isFinite(p.volume)) {
          volume.value = Math.max(0, Math.min(100, p.volume))
        }
        if (typeof p.muted === 'boolean') muted.value = p.muted
      }
    } catch { /* corrupt/absent — keep defaults */ }
  }
  const settings = useAudioSettingsBindings()

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
      // Keep the Cache API warmed for the upcoming window. Fires on every
      // shape change of the up-next slice — track advance, add/remove, drag
      // reorder — debounced so a rapid string of reorders coalesces into one
      // sync() instead of refetching on every intermediate drop position.
      watch(upcomingTracks, (list) => schedulePrefetchSync(list, currentTrack.value))
      // Server queue mutations (any tab, any client) re-arm the pending
      // deck — a reorder or reshuffle changes what plays next.
      watch(() => qs.version, () => prepareTransition())
    }
    // Seed the engine's volume OUTSIDE the wiring guard so it's idempotent: a
    // hot reload of useAudioEngine resets its module singleton (back to a 1.0
    // default) while engineWired (a useState) survives — gating the seed on
    // !engineWired would leave the rebuilt engine at full blast on the next
    // play. Re-applying every ensureEngine call is cheap and keeps them in sync.
    if (import.meta.client) e.setVolume(muted.value ? 0 : volume.value / 100)
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
      boundaries_ready: false,
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
    // Loudness was blocking, but boundaries are intentionally filled behind
    // playback. Re-fetch briefly until they land, then re-arm smart crossfade
    // with the hot-added transition points.
    if (data && !data.boundaries_ready) {
      const attempt = playbackAnalysisRetries.get(trackId) ?? 0
      if (attempt < 5) {
        playbackAnalysisRetries.set(trackId, attempt + 1)
        setTimeout(() => {
          playbackDataCache.delete(trackId)
          void ensureAnalysisAndArm()
        }, 1000)
      }
    } else if (data?.boundaries_ready) {
      playbackAnalysisRetries.delete(trackId)
    }
    return true
  }

  // The network URL for `t` (token + caps + quality) — see
  // ~/composables/useStreamUrl.ts for the full contract. Hoisted out of this
  // file so engine/prefetch.ts can build the exact same URL for its cache
  // keys; this stays a thin wrapper so every existing call site's
  // `if (!url) return` guard keeps working unchanged (buildStreamUrl returns
  // '' rather than undefined on failure — both are falsy).
  function resolveStreamUrl(t: Track): string | undefined {
    return buildStreamUrl(t) || undefined
  }

  // The URL a deck should actually load for `t`: a cached blob: URL when the
  // prefetch manager has already warmed it, otherwise the plain network URL.
  // Streams (internet radio) skip the cache entirely — they're unbounded
  // responses, not a file to warm ahead of time.
  async function resolveDeckUrl(t: Track): Promise<string | undefined> {
    const network = resolveStreamUrl(t)
    if (!network) return undefined
    if (t.isStream) return network
    const playable = await prefetchManager.resolvePlayable(t)
    return playable || network
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
    // Remote output: no local decks to arm — transitions are API calls
    // driven by castTrackEnded(), not the scheduler.
    if (useCastStore().engaged) {
      pendingNext = null
      pendingPlan = null
      prefetchedTrackId = null
      return
    }
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
        const targetId = next.id
        // armSync stays synchronous (called straight from watchers), so the
        // cache lookup that might turn `url` into a blob: URL happens off to
        // the side: resolvePlayable never does network I/O itself (only
        // sync() does), so this settles almost immediately either way. The
        // targetId check guards against a late resolve loading a track that's
        // no longer the armed "next" (queue changed again in the meantime).
        void prefetchManager.resolvePlayable(next)
          .then((playable) => {
            if (prefetchedTrackId !== targetId) return
            return e.loadNext(playable || url)
          })
          .catch((err) => {
            alog('xfade', `preload FAILED for "${next.title}" — will cold-play`, err)
            if (prefetchedTrackId === targetId) prefetchedTrackId = null
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
    // Remote output: skip entirely — ensureAnalysisAndArm would otherwise
    // spin up an AudioContext (via ensureEngine) that nothing will play on.
    if (import.meta.client && useCastStore().engaged) return
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
        const playUrl = await resolveDeckUrl(next)
        if (!playUrl) { transitioning = false; return }
        // A manual play() (or another transition) superseded us while the
        // cache lookup was in flight — bail rather than clobber it.
        if (currentTrack.value !== cur) { transitioning = false; return }
        applyActiveNorm(e, next)
        await e.play(playUrl)
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
  function advanceCurrentTo(next: Track, reason: 'ended' | 'skip' = 'ended') {
    alog('player', `now playing "${next.title}" #${next.id} (advanced via deck swap)`)
    reportAdvance(reason)
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

  // Tell the server the renderer crossed a track boundary so its pointer
  // follows. Fire-and-forget: playback already advanced locally; the
  // from_item_id guard makes a racing double-report a no-op, and the
  // response/WS event reconciles the mirror.
  function reportAdvance(reason: 'ended' | 'skip' | 'prev') {
    if (localMode.value || !import.meta.client) return
    const from = qs.currentItemID
    if (!from) return
    void qs.advance(from, reason).catch(() => { /* WS view reconciles */ })
  }

  // --- Context playback: THE way to start playing something ---------------
  // The server materializes the full source (an artist's whole
  // discography, a 10k-track genre) — clients only ever see the window,
  // which is exactly what makes server-side shuffle truly random.
  async function playContext(source: QueueSourceInput, opts?: {
    startTrackId?: number
    // Play THIS exact object (its stream_url/loudness overrides intact —
    // the quality picker's play-this-file path) instead of rebuilding the
    // track from the window row. Implies startTrackId.
    startTrack?: Track
    shuffle?: boolean
  }) {
    if (!import.meta.client) return
    localMode.value = false
    originalOrder.value = []
    let view
    try {
      view = await qs.replace(source, opts?.startTrack?.id ?? opts?.startTrackId ?? 0, !!opts?.shuffle, queueOutputID())
    } catch {
      useToast().toast.err('Nothing playable in this selection')
      return
    }
    if (opts?.startTrack) {
      await play(opts.startTrack, { skipQueueSync: true })
      return
    }
    const current = view.items.find((i) => i.item_id === (view.current_item_id ?? 0)) ?? view.items[0]
    if (!current) return
    await play(itemToTrack(current), { skipQueueSync: true })
  }

  // Explicit track lists (mixes, top-tracks, multi-selects). Lists that
  // contain non-library entries (radio streams, podcast episodes) can't
  // live server-side and drop to the local queue.
  async function playTracks(tracks: Track[], start?: Track, opts?: { shuffle?: boolean }) {
    const list = tracks.filter((t) => t.available !== false)
    if (!list.length) return
    if (list.some((t) => t.isStream || t.id <= 0)) {
      await playLocal(list, start)
      return
    }
    await playContext(
      { kind: 'tracks', track_ids: list.map((t) => t.id) },
      { startTrack: start, shuffle: opts?.shuffle },
    )
  }

  // Local-only playback for things that aren't music-track rows: internet
  // radio streams and podcast episodes. The server queue is left alone.
  async function playLocal(tracks: Track[], start?: Track) {
    localMode.value = true
    localQueue.value = tracks
    originalOrder.value = []
    const first = start ?? tracks[0]
    if (first) await play(first, { skipQueueSync: true })
  }

  // syncQueuePointer keeps the server pointer in step with a direct
  // play(track): an in-queue track becomes a jump; anything else becomes
  // a one-track queue. jumpTo()/playContext() pass skipQueueSync — they
  // already positioned the pointer precisely.
  function syncQueuePointer(track: Track) {
    if (!import.meta.client || localMode.value || track.isStream || track.id <= 0) return
    const item = qs.items.find((i) => i.track_id === track.id)
    if (item) {
      if (item.item_id !== qs.currentItemID) void qs.jump(item.item_id).catch(() => {})
    } else {
      void qs.replace({ kind: 'tracks', track_ids: [track.id] }, track.id, false, queueOutputID())
        .catch(() => { /* view reconciles via WS */ })
    }
  }

  async function play(track?: Track, opts?: { skipQueueSync?: boolean }) {
    // Remote output: the queue/track state stays client-side (Phase 2),
    // but the audio path is an API call — never touch the local engine.
    if (import.meta.client && useCastStore().engaged) {
      if (track && !opts?.skipQueueSync) syncQueuePointer(track)
      await playViaCast(track)
      return
    }
    const e = ensureEngine()
    if (track) {
      if (!opts?.skipQueueSync) syncQueuePointer(track)
      // Rendering locally makes this tab the active output.
      if (import.meta.client && !localMode.value && !qs.isActiveOutput) void qs.claim()
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
      const networkUrl = resolveStreamUrl(track)
      if (!networkUrl) return
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
      // Cache lookup (resolvePlayable never does network I/O itself, only a
      // fast Cache.match) — check staleness again after it, same reasoning.
      const playUrl = track.isStream ? networkUrl : await prefetchManager.resolvePlayable(track)
      if (gen !== playGeneration) return
      try {
        await e.play(playUrl || networkUrl)
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
    // Mirror tab pressing play = "play here": claim the output and pick
    // up from the server's position via the same cold-load handoff the
    // cast-disconnect path uses.
    if (import.meta.client && !localMode.value && !qs.isActiveOutput) {
      localHandoff = { trackId: currentTrack.value.id, position: qs.positionSeconds }
      void qs.claim()
    }
    // Coming back from a cast session: the deck holds nothing (or a stale
    // buffer from before the handoff) — cold-load and jump to where the
    // receiver left off.
    if (localHandoff && localHandoff.trackId === currentTrack.value.id) {
      const h = localHandoff
      localHandoff = null
      const t = currentTrack.value
      await play(t)
      if (engineWired.value && h.position > 0) {
        ensureEngine().seek(h.position)
        position.value = h.position
        lastTickTime = h.position
      }
      return
    }
    try {
      await e.resume()
    } catch {
      playing.value = false
    }
  }

  // The cast-mode half of play(): same queue bookkeeping, remote transport.
  async function playViaCast(track?: Track) {
    const cast = useCastStore()
    if (track) {
      if (track.available === false) return
      if (track.isStream || track.id <= 0) {
        useToast().toast.err('Radio streams can\'t be cast yet')
        return
      }
      transitioning = false
      prefetchedTrackId = null
      pendingNext = null
      currentTrack.value = track
      position.value = 0
      scrobbledTrackId.value = null
      listenedSeconds = 0
      lastTickTime = 0
      if (track.duration && Number.isFinite(track.duration)) duration.value = track.duration
      playing.value = true // optimistic; the WS mirror confirms
      alog('player', `cast play "${track.title}" #${track.id} → ${cast.deviceName}`)
      try {
        await cast.playTrack(track.id, volume.value)
      } catch {
        playing.value = false
        useToast().toast.err(`Couldn't cast to ${cast.deviceName || 'device'}`)
      }
      return
    }
    // Resume. A live session resumes in place; without one (the server
    // drops sessions between tracks) re-cast the current track from the
    // frozen position.
    const cur = currentTrack.value
    if (!cur) return
    playing.value = true
    try {
      if (cast.session) await cast.resume()
      else if (cur.id > 0 && !cur.isStream) await cast.playTrack(cur.id, volume.value, position.value)
      else playing.value = false
    } catch {
      playing.value = false
    }
  }

  function pause() {
    if (import.meta.client) {
      const cast = useCastStore()
      if (cast.engaged) {
        playing.value = false // optimistic; WS mirror confirms
        void cast.pause().catch(() => { /* session already gone */ })
        return
      }
    }
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
    if (import.meta.client && useCastStore().engaged) {
      // While paused between tracks (no session) this still moves the
      // frozen position — the next re-cast starts from it.
      void useCastStore().seekTo(target).catch(() => { /* WS restores truth */ })
    } else if (engineWired.value) {
      ensureEngine().seek(target)
    }
    position.value = target
    lastTickTime = target
  }

  function setVolume(v: number) {
    const clamped = Math.max(0, Math.min(100, v))
    // While muted the control's baseline reads 0, so a stray down-scroll or a
    // click on the zero-thumb would call setVolume(0) and wipe the remembered
    // pre-mute level (both in state and localStorage). Ignore a muted set-to-0
    // so unmuting restores the real level; explicit set-to-0 while audible
    // still works.
    if (muted.value && clamped === 0) return
    volume.value = clamped
    if (clamped > 0) muted.value = false
    if (import.meta.client && useCastStore().engaged) {
      // The slider is the DEVICE stream volume while casting. Deliberately
      // not persisted — localStorage keeps the local listening level for
      // when the output comes back.
      useCastStore().setVolume(clamped)
      return
    }
    if (engineWired.value) ensureEngine().setVolume(muted.value ? 0 : clamped / 100)
    persistVolumePrefs(volume.value, muted.value)
  }

  function toggleMute() {
    muted.value = !muted.value
    if (import.meta.client && useCastStore().engaged) {
      // No mute verb on the receiver — drive the stream volume to 0 and
      // back. `muted` stays a local flag so unmute knows the level.
      useCastStore().setVolume(muted.value ? 0 : volume.value)
      return
    }
    if (engineWired.value) ensureEngine().setVolume(muted.value ? 0 : volume.value / 100)
    persistVolumePrefs(volume.value, muted.value)
  }

  // --- Shuffle --------------------------------------------------------------
  // Server mode: one POST — the server reshuffles the upcoming slice (or
  // restores the source's natural order) over the FULL queue, which is the
  // whole point (client shuffle only ever saw the loaded window). Local
  // mode (radio/podcasts) keeps the in-place array shuffle.
  function upcomingStart() {
    return currentIndex.value >= 0 ? currentIndex.value + 1 : 0
  }
  function shuffleUpcoming() {
    const start = upcomingStart()
    if (start >= localQueue.value.length) return
    const head = localQueue.value.slice(0, start)
    const upcoming = localQueue.value.slice(start)
    for (let i = upcoming.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1))
      ;[upcoming[i], upcoming[j]] = [upcoming[j]!, upcoming[i]!]
    }
    localQueue.value = [...head, ...upcoming]
  }
  // Restore the pre-shuffle ordering, reconciled against the *current* queue so
  // edits made while shuffled survive: tracks removed during shuffle stay gone,
  // tracks added during shuffle are kept (appended after the restored run in
  // their current order). Only the upcoming slice is reordered.
  function restoreOriginalOrder() {
    if (!originalOrder.value.length) return
    const start = upcomingStart()
    const head = localQueue.value.slice(0, start)
    const upcomingNow = localQueue.value.slice(start)
    const upcomingIds = new Set(upcomingNow.map((t) => t.id))
    // Original ordering, but only for tracks still upcoming in the live queue.
    const restored = originalOrder.value.filter((t) => upcomingIds.has(t.id))
    const restoredIds = new Set(restored.map((t) => t.id))
    // Tracks queued while shuffled weren't in the snapshot — keep them.
    const added = upcomingNow.filter((t) => !restoredIds.has(t.id))
    localQueue.value = [...head, ...restored, ...added]
    originalOrder.value = []
  }
  function toggleShuffle() {
    if (!localMode.value) {
      void qs.setShuffle(!qs.shuffled)
      return // the items event re-arms the transition via the version watcher
    }
    localShuffled.value = !localShuffled.value
    if (localShuffled.value) {
      originalOrder.value = [...localQueue.value]
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
    if (import.meta.client && useCastStore().isClientDevice) {
      await play(next)
      return
    }
    if (playing.value && !transitioning && prefetchedTrackId === next.id && !next.isStream) {
      const e = ensureEngine()
      transitioning = true
      try {
        alog('player', `skip → "${next.title}" (instant, preloaded ✓)`)
        await e.transition('gapless')
        advanceCurrentTo(next, 'skip')
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
      if (import.meta.client && useCastStore().engaged) {
        void useCastStore().seekTo(0).catch(() => { /* WS restores truth */ })
      } else if (engineWired.value) {
        ensureEngine().seek(0)
      }
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
        const playUrl = await resolveDeckUrl(next)
        if (!playUrl) { transitioning = false; playing.value = false; return }
        // Superseded by a manual play() while the cache lookup was in flight.
        if (currentTrack.value !== finished) { transitioning = false; return }
        applyActiveNorm(e, next)
        await e.play(playUrl)
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

  // The Queue and Lyrics buttons both drive the one side panel: clicking a
  // button opens the panel on that tab, or closes it if it's already showing
  // that tab (so each button toggles its own view).
  function toggleQueue() {
    if (queueOpen.value && sideTab.value === 'queue') { queueOpen.value = false; return }
    sideTab.value = 'queue'
    queueOpen.value = true
  }
  function toggleLyrics() {
    if (queueOpen.value && sideTab.value === 'lyrics') { queueOpen.value = false; return }
    sideTab.value = 'lyrics'
    queueOpen.value = true
  }

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
  // jumpTo plays the queue item at absolute window index. Server mode
  // jumps by ITEM id (a queue can hold the same track twice; index-based
  // find-by-track-id would hit the wrong copy).
  async function jumpTo(index: number) {
    if (!localMode.value) {
      const item = qs.items[index]
      if (!item) return
      void qs.jump(item.item_id).catch(() => {})
      await play(itemToTrack(item), { skipQueueSync: true })
      return
    }
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
    if (!localMode.value) {
      const item = qs.items[index]
      if (item) void qs.removeItem(item.item_id)
    } else {
      localQueue.value.splice(index, 1)
    }
    prepareTransition()
  }

  // moveInQueue reorders an upcoming track. Same guards as remove.
  function moveInQueue(from: number, to: number) {
    if (from <= currentIndex.value || to <= currentIndex.value) return
    if (from >= queue.value.length || to >= queue.value.length) return
    if (from === to) return
    if (!localMode.value) {
      const item = qs.items[from]
      if (!item) return
      // The predecessor at the target slot once `from` is extracted; the
      // current item as predecessor means "head of upcoming" (0 works too).
      const without = qs.items.toSpliced(from, 1)
      const pred = without[to - 1]
      void qs.moveItem(item.item_id, pred ? pred.item_id : 0)
    } else {
      const next = localQueue.value.slice()
      const [item] = next.splice(from, 1)
      if (item) next.splice(to, 0, item)
      localQueue.value = next
    }
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
      await playTracks(list)
      return
    }
    if (!localMode.value) {
      // Server-side dedupe against the FULL upcoming slice (the local
      // window only sees part of a big queue).
      void qs.enqueue(list.map((t) => t.id), 'end').then(() => prepareTransition())
      return
    }
    const upcomingIds = new Set(upcomingTracks.value.map((t) => t.id))
    const fresh = list.filter((t) => !upcomingIds.has(t.id))
    if (!fresh.length) return
    localQueue.value = [...localQueue.value, ...fresh]
    prepareTransition()
  }

  // playNext inserts one or more tracks immediately after the currently-
  // playing one (or at the head if nothing's playing). Mirrors Spotify /
  // Apple Music "Play Next". Same de-dupe behavior as addToQueue.
  async function playNext(tracks: Track | Track[]) {
    const list = (Array.isArray(tracks) ? tracks : [tracks]).filter((t) => t.available !== false)
    if (!list.length) return
    if (!queue.value.length) {
      await playTracks(list)
      return
    }
    if (!localMode.value) {
      void qs.enqueue(list.map((t) => t.id), 'next').then(() => prepareTransition())
      return
    }
    const upcomingIds = new Set(upcomingTracks.value.map((t) => t.id))
    const fresh = list.filter((t) => !upcomingIds.has(t.id))
    if (!fresh.length) return
    const idx = currentIndex.value
    const insertAt = idx < 0 ? 0 : idx + 1
    const next = localQueue.value.slice()
    next.splice(insertAt, 0, ...fresh)
    localQueue.value = next
    prepareTransition()
  }

  // clearUpcoming empties everything after the current track. Used by the
  // sidebar's "Clear" button on the Up Next header.
  function clearUpcoming() {
    if (!localMode.value) {
      void qs.clearUpcoming().then(() => prepareTransition())
      return
    }
    const idx = currentIndex.value
    if (idx < 0) {
      localQueue.value = []
      originalOrder.value = []
      return
    }
    localQueue.value = localQueue.value.slice(0, idx + 1)
    originalOrder.value = []
    prepareTransition()
  }

  // stop unloads the engine + clears state. Used by the playbar long-press.
  // While casting it also ends the remote session (the device stays engaged
  // — the next play targets it again). Engaged-only: a tab merely watching
  // someone else's cast must not kill it by clearing its own local queue.
  function stop() {
    if (import.meta.client && useCastStore().engaged) void useCastStore().stopSession()
    if (import.meta.client && !localMode.value) void qs.clearAll() // explicit gesture — labeled "stop & clear queue"
    localHandoff = null
    if (engineWired.value) ensureEngine().stop()
    playing.value = false
    currentTrack.value = null
    localMode.value = false
    localQueue.value = []
    originalOrder.value = []
    position.value = 0
    duration.value = 0
    transitioning = false
    prefetchedTrackId = null
    pendingNext = null
    listenedSeconds = 0
    lastTickTime = 0
  }

  // --- Cast output orchestration (docs/cast-plan.md Phase 2) ----------------
  // The queue, shuffle, repeat, and track-advance logic above stays the
  // owner of WHAT plays; these switch WHERE it plays.

  // Engage a device and hand the current playback off to it mid-track.
  async function startCastTo(deviceId: string) {
    const cast = useCastStore()
    if (deviceId.startsWith('client:')) {
      cast.engagedDeviceId = deviceId
      await qs.selectTarget(deviceId)
      const idx = qs.currentWindowIndex
      const remote = idx >= 0 ? queue.value[idx] : undefined
      currentTrack.value = remote ?? null
      position.value = qs.positionSeconds
      playing.value = qs.playing
      if (remote?.duration) duration.value = remote.duration
      if (engineWired.value) ensureEngine().pause()
      return
    }
    const track = currentTrack.value
    const pos = position.value
    const wasPlaying = playing.value
    // One session per device server-side — switching receivers must stop
    // the old one first or both keep playing.
    if (cast.session && cast.session.device_id !== deviceId) await cast.stopSession()
    cast.engagedDeviceId = deviceId
    localHandoff = null
    // Silence the local engine; queue + position state stay put. Also
    // drops any armed pending deck so a later "next" can't cold-swap to it.
    transitioning = false
    prefetchedTrackId = null
    pendingNext = null
    if (engineWired.value) ensureEngine().pause()
    if (wasPlaying && track && track.id > 0 && !track.isStream) {
      playing.value = true // optimistic through the ~2s establishment
      try {
        await cast.playTrack(track.id, volume.value, pos)
      } catch {
        playing.value = false
        cast.engagedDeviceId = null
        useToast().toast.err(`Couldn't cast to ${cast.deviceName || 'device'}`)
      }
    }
  }

  // Disconnect: release the device, freeze position where the receiver
  // was, and set up the local resume handoff. Playback does NOT auto-blast
  // out of the laptop speakers — the user presses play to bring it back.
  async function stopCasting() {
    const cast = useCastStore()
    const track = currentTrack.value
    const pos = cast.session ? cast.livePositionSec() : position.value
    await cast.disconnect()
    await qs.selectTarget()
    playing.value = false
    position.value = pos
    lastTickTime = pos
    if (track && track.id > 0 && !track.isStream) {
      localHandoff = { trackId: track.id, position: pos }
    }
    // The slider was mirroring the device volume — restore the local pref.
    if (import.meta.client) {
      try {
        const raw = localStorage.getItem(VOLUME_STORAGE_KEY)
        if (raw) {
          const p = JSON.parse(raw) as { volume?: number, muted?: boolean }
          if (typeof p.volume === 'number' && Number.isFinite(p.volume)) {
            volume.value = Math.max(0, Math.min(100, p.volume))
          }
        }
      } catch { /* keep whatever's shown */ }
    }
  }

  // Fired by the cast WS mirror when a track this tab started finishes on
  // the receiver. The server already scrobbled it (source "cast") — this
  // only advances the queue. Client-driven advance is the accepted Phase 2
  // limitation: the tab must stay open (Phase 3 moves the queue server-side).
  async function castTrackEnded() {
    if (sleepAtTrackEnd.value) {
      sleepAtTrackEnd.value = false
      playing.value = false
      alog('player', 'sleep timer: stopped at end of cast track')
      return
    }
    const next = peekNextTrack() // queue order; returns current for repeat-one
    if (!next) {
      alog('player', 'cast queue ended')
      playing.value = false
      return
    }
    alog('player', `cast advance → "${next.title}"`)
    await play(next)
  }

  return {
    playing, currentTrack, position, duration, volume, muted,
    shuffled, repeatMode, queue, originalOrder, queueOpen, sideTab, localMode,
    engineWired, scrobbledTrackId, sleepAtTrackEnd, sleepDeadline, sleepNowTick,
    currentIndex, playedTracks, upcomingTracks, upcomingCount,
    nextUp, progress, hasPrevious, hasNext,
    play, pause, togglePlay, seek, setVolume, toggleMute, stop,
    playContext, playTracks, playLocal,
    toggleShuffle, cycleRepeat, nextTrack, prevTrack,
    toggleLoved, toggleQueue, toggleLyrics, formatTime,
    jumpTo, removeFromQueue, moveInQueue, clearUpcoming,
    addToQueue, playNext,
    startCastTo, stopCasting, castTrackEnded,
  }
})

if (import.meta.hot) import.meta.hot.accept(acceptHMRUpdate(usePlayerStore, import.meta.hot))

/** Template-friendly Pinia bindings. State remains refs when destructured;
 * actions remain the store's bound actions. New integrations that prefer
 * property access can consume usePlayerStore() directly. */
export function usePlayerBindings() {
  const store = usePlayerStore()
  return {
    ...storeToRefs(store),
    play: store.play,
    playContext: store.playContext,
    playTracks: store.playTracks,
    playLocal: store.playLocal,
    pause: store.pause,
    togglePlay: store.togglePlay,
    seek: store.seek,
    setVolume: store.setVolume,
    toggleMute: store.toggleMute,
    stop: store.stop,
    toggleShuffle: store.toggleShuffle,
    cycleRepeat: store.cycleRepeat,
    nextTrack: store.nextTrack,
    prevTrack: store.prevTrack,
    toggleLoved: store.toggleLoved,
    toggleQueue: store.toggleQueue,
    toggleLyrics: store.toggleLyrics,
    formatTime: store.formatTime,
    jumpTo: store.jumpTo,
    removeFromQueue: store.removeFromQueue,
    moveInQueue: store.moveInQueue,
    clearUpcoming: store.clearUpcoming,
    addToQueue: store.addToQueue,
    playNext: store.playNext,
    startCastTo: store.startCastTo,
    stopCasting: store.stopCasting,
    castTrackEnded: store.castTrackEnded,
  }
}

// applyAudioSettingsToEngine pushes the persisted EQ state into the engine.
// Crossfade/scheduler concerns are owned by usePlayer.prepareTransition (it
// needs per-track context for album-aware suppression + plan timing), so they
// are intentionally NOT set here. Idempotent — re-applied on every mutation.
function applyAudioSettingsToEngine(engine: ReturnType<typeof useAudioEngine>, settings: ReturnType<typeof useAudioSettingsBindings>) {
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
