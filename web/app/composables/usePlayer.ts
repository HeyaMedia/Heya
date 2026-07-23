import { useAudioEngine } from '~/composables/useAudioEngine'
import { nativeAnalyserDemandActive } from '~/composables/usePlaybackAnalyser'
import type { DJMode, QueueItem, QueueSourceInput } from '~/composables/useQueue'
import { resumeContext } from '~/engine/context'
import { shouldSuppressCrossfade } from '~/engine/crossfade/albumAware'
import { SmartCrossfade } from '~/engine/crossfade/smartCrossfade'
import type { BoundaryHints, TransitionPlan } from '~/engine/crossfade/strategy'
import { TimeBasedCrossfade } from '~/engine/crossfade/timeBased'
import { alog } from '~/engine/debug'
import { prefetchManager } from '~/engine/prefetch'
import { computeNormalizationGain } from '~/engine/dsp/normalization'
import type { NativeAudioPlaybackBackend } from '~/composables/useNativeAudioPlaybackBackend'
import type { AudioPlaybackClockSource } from '~/types/audio-playback'
import type {
  NativeAudioProcessingSettings,
  NativeAudioTrackAnalysisUpdate,
  NativeAudioTrackRequest,
} from '~/types/native-audio'
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
  disc_number?: number
  track_number?: number
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
  dj_generated?: boolean
  dj_mode?: DJMode
}

// Track shape consumed by the player UI. `stream_url` is what the engine
// actually hits — derived from the track row in the caller (Phase A list
// endpoints set this to `/api/music/tracks/{id}/stream`).

// Volume + mute survive reloads via localStorage. The store starts with a safe
// default, then restores synchronously before any player control renders.
const VOLUME_STORAGE_KEY = 'heya_player_volume_v1'
const SIMILAR_AUTOPLAY_STORAGE_KEY = 'heya_similar_autoplay_v1'
const SIMILAR_AUTOPLAY_REFILL_AT = 5
const SIMILAR_AUTOPLAY_BATCH_SIZE = 20
const SIMILAR_AUTOPLAY_MAX_SEEDS = 12
const SIMILAR_AUTOPLAY_MAX_EXCLUDES = 2000
let volumeRestored = false

function persistVolumePrefs(volume: number, muted: boolean) {
  try {
    localStorage.setItem(VOLUME_STORAGE_KEY, JSON.stringify({ volume, muted }))
  } catch { /* private mode / quota — non-fatal */ }
}

// --- Transition coordination (singletons) ---------------------------------
// These coordinate between prepareTransition() (which preloads the pending
// deck + arms the scheduler) and handleTransition() (fired by the scheduler
// ~100ms before a gapless cut, or `duration` seconds before a crossfade).
// They live at module scope so every usePlayerBindings() closure and the once-wired
// engine callbacks share the same values.
let transitioning = false
let prefetchedTrackId: number | null = null
let preloadingTrackId: number | null = null
let pendingNext: Track | null = null
let pendingMode: 'gapless' | 'crossfade' = 'gapless'
let pendingPlan: TransitionPlan | null = null
// Wall-clock seconds actually heard of the current track (pause/seek-aware).
let listenedSeconds = 0
let lastTickTime = 0
// Start timestamp paired with the eventual completion scrobble. Last.fm
// requires the time playback began, not the time the track finished.
let trackStartedAtUnix = 0
// Monotonic token bumped on each play(track). Captured before play()'s awaited
// analysis fetch so a stale request that resolves late can detect it's been
// superseded by a newer one and bail instead of clobbering the active deck.
let playGeneration = 0
// Non-null while an explicit play(track) is resolving queue ownership,
// analysis and the renderer load. Queue/analysis watchers must not arm a
// scheduler against the old audible deck using the new track's boundaries.
let loadingTrackGeneration: number | null = null
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

// Minimal scheduler view of the audio engine used by transition orchestration.
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
  library_file_id: number | null
  format: string | null
  bitrate_kbps: number | null
  sample_rate_hz: number | null
  bit_depth: number | null
  channels: number | null
}

type SimilarAutoplaySeed =
  | { kind: 'track', track_id: number }
  | { kind: 'artist', artist_id: number }
  | { kind: 'album', album_id: number }

interface SimilarAutoplayResponse {
  tracks?: Array<{ track_id: number }>
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
      library_file_id: toNum(f.library_file_id),
      format: typeof f.format === 'string' ? f.format : null,
      bitrate_kbps: toNum(f.bitrate_kbps),
      sample_rate_hz: toNum(f.sample_rate_hz),
      bit_depth: toNum(f.bit_depth),
      channels: toNum(f.channels),
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
  const playbackBackend = ref<'browser' | 'native' | 'cast'>('browser')
  const nativeAudioBackend = shallowRef<NativeAudioPlaybackBackend | null>(null)
  const nativeAudioState = computed(() => nativeAudioBackend.value?.state ?? null)
  const nativeAudioCapabilities = computed(() => nativeAudioBackend.value?.capabilities ?? null)
  const nativeAudioVisualizer = computed(() => nativeAudioBackend.value?.visualizer.value ?? null)
  const nativeAudioOutputDevices = computed(() => nativeAudioBackend.value?.outputDevices.value ?? [])
  const nativeAudioOutputDeviceId = computed(() => nativeAudioBackend.value?.activeOutputDeviceId.value ?? null)
  const nativeAudioFollowsSystemDefault = computed(() => nativeAudioBackend.value?.followsSystemDefault.value ?? true)
  const scrobbledTrackId = ref<number | null>(null)
  const sleepAtTrackEnd = ref(false)
  const sleepDeadline = ref<number | null>(null)
  const sleepNowTick = ref(0)
  const similarAutoplayEnabled = ref(true)
  const similarAutoplayLoading = ref(false)

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
  const djChanging = ref(false)
  const djMode = computed<DJMode>(() => localMode.value ? 'off' : qs.djMode)
  const djAvailable = computed(() => !localMode.value
    && !!currentTrack.value
    && !currentTrack.value.isStream
    && currentTrack.value.id > 0)

  // Similar autoplay is deliberately queue-session state, not a second queue.
  // Context tracks describe what the listener intentionally started/added;
  // generated tracks only enter `seen`, so each refill stays anchored instead
  // of recursively drifting toward its own previous recommendations.
  let similarAutoplayContext: SimilarAutoplaySeed[] = []
  let similarAutoplayIntentTrackIDs: number[] = []
  const similarAutoplaySeenTrackIDs = new Set<number>()
  let similarAutoplayGeneration = 0
  let similarAutoplayRetryAfter = 0
  let similarAutoplayRequests = 0
  let similarAutoplayRequest: { generation: number, promise: Promise<boolean> } | null = null

  function itemToTrack(i: QueueItem): Track {
    return {
      id: i.track_id,
      title: i.title,
      artist: i.artist_name,
      album: i.album_title,
      duration: i.duration,
      album_id: i.album_id,
      disc_number: i.disc_number,
      track_number: i.track_number,
      artist_id: i.artist_id,
      artist_slug: i.artist_slug,
      album_slug: i.album_slug,
      poster: useAlbumCoverUrl(i.artist_slug, i.album_slug) ?? undefined,
      dj_generated: i.dj_generated,
      dj_mode: i.dj_mode,
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
  const similarAutoplayAvailable = computed(() => !localMode.value && !!currentTrack.value && currentTrack.value.id > 0)
  const hasNext = computed(() => upcomingCount.value > 0
    || (repeatMode.value !== 'off' && queue.value.length > 0)
    || (similarAutoplayEnabled.value && similarAutoplayAvailable.value))

  // Restore persisted volume/mute once for the singleton player store.
  if (!volumeRestored) {
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
  try {
    const stored = localStorage.getItem(SIMILAR_AUTOPLAY_STORAGE_KEY)
    if (stored === '0') similarAutoplayEnabled.value = false
    else if (stored === '1') similarAutoplayEnabled.value = true
  } catch { /* private mode / quota — keep the default-on preference */ }
  const settings = useAudioSettingsBindings()
  let nativeAudioProbe: Promise<NativeAudioPlaybackBackend | null> | null = null
  let nativePreloadGeneration = 0
  let nativePreloadRetryTrackId: number | null = null
  let nativePreloadRetryTimer: ReturnType<typeof setTimeout> | null = null
  let nativeLastStartedTrackId: number | null = null
  let nativeEndedHandled = false
  let localClockTimer: ReturnType<typeof setInterval> | null = null
  let nativeClockReconcileAt = 0
  let nativeClockReconcileInFlight = false
  const nowPlaying = useNowPlayingSession()

  // Music sessions use the same Activity-page controls as video sessions.
  // The session id check ensures only this player reacts when a user has more
  // than one tab or device online.
  const { on, connect } = useEventBus()
  connect()
  const offSessionCommand = on('session.command', (event) => {
    const payload = event.payload as { session_id?: string, action?: string, message?: string, by?: string }
    if (!payload || payload.session_id !== nowPlaying.sessionId) return
    if (payload.action === 'stop') {
      stop()
      useToast().toast.info(payload.by ? `Playback stopped by ${payload.by}` : 'Playback stopped remotely')
    } else if (payload.action === 'message' && payload.message) {
      useToast().toast.info(payload.by ? `${payload.by}: ${payload.message}` : payload.message)
    }
  })
  onScopeDispose(offSessionCommand)

  function acceptLocalPosition(t: number) {
    position.value = t
    // Accumulate genuinely-heard time: only count small forward deltas, so
    // seeks and track-boundary resets never inflate completion telemetry.
    const dt = t - lastTickTime
    lastTickTime = t
    if (playing.value && dt > 0 && dt < 2) listenedSeconds += dt
  }

  // Engine creation touches AudioContext, which the browser refuses to
  // instantiate before a user gesture. Defer it to the first play() call so
  // the autoplay-policy warning never fires on mount.
  function ensureEngine() {
    const e = useAudioEngine()
    if (!engineWired.value) {
      engineWired.value = true
      e.setOnEnded(() => handleEnded())
      e.setOnError(() => { playing.value = false })
      // The scheduler fires this at the transition point (gapless: ~100ms
      // before end; crossfade: `duration` before end). Without it the entire
      // dual-deck gapless/crossfade machinery is inert and every track change
      // is a cold reload with an audible gap.
      e.setOnTransitionPoint(() => { void handleTransition() })
      watch(e.isPlaying, (v) => { playing.value = v })
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
    e.setVolume(muted.value ? 0 : volume.value / 100)
    // Register the settings→engine bridge. Deliberately OUTSIDE the engineWired
    // guard and idempotent per backend owner: a hot reload of useAudioSettings
    // resets its module-level bridge while engineWired (a useState)
    // survives, so gating this on engineWired would permanently strand the
    // bridge after any HMR edit — EQ/crossfeed/replay-gain toggles would then
    // silently no-op. Changing owner rewires it; re-registering the same owner
    // is a no-op so transition preparation cannot recurse through ensureEngine.
    settings.registerEngineBridge(e, () => {
      applyAudioSettingsToEngine(e, settings)
      applyActiveNorm(e, currentTrack.value)
      prepareTransition()
    })
    return e
  }

  function activeLocalClockSource(): AudioPlaybackClockSource | null {
    if (playbackBackend.value === 'native') return nativeAudioBackend.value
    if (playbackBackend.value === 'browser' && engineWired.value) return useAudioEngine()
    return null
  }

  function sampleLocalClock() {
    const source = activeLocalClockSource()
    if (!source) return
    const sample = source.readClock()
    if (Number.isFinite(sample.positionSeconds)) acceptLocalPosition(sample.positionSeconds)
    if (Number.isFinite(sample.durationSeconds) && sample.durationSeconds > 0) {
      duration.value = sample.durationSeconds
    }

    if (source.kind !== 'native') return
    // Events keep the normal path responsive. Once per second, independently
    // reconcile with Rust's callback-owned PCM frame counter so a dropped Rust
    // event or WebView injection can never strand the UI clock indefinitely.
    const now = performance.now()
    if (nativeClockReconcileInFlight || now < nativeClockReconcileAt) return
    nativeClockReconcileAt = now + 1000
    nativeClockReconcileInFlight = true
    void Promise.resolve(source.reconcileClock())
      .catch(error => alog('player', 'native audio clock reconciliation failed', error))
      .finally(() => { nativeClockReconcileInFlight = false })
  }

  function updateLocalClockPolling() {
    const nativeActive = playbackBackend.value === 'native'
      && !!nativeAudioBackend.value?.rendererSessionId.value
      && (playing.value || !!nativeAudioState.value?.loading || !!nativeAudioState.value?.buffering)
    const browserActive = playbackBackend.value === 'browser' && engineWired.value && playing.value
    const shouldPoll = !!currentTrack.value && (nativeActive || browserActive)
    if (shouldPoll && !localClockTimer) {
      sampleLocalClock()
      localClockTimer = setInterval(sampleLocalClock, 250)
    } else if (!shouldPoll && localClockTimer) {
      clearInterval(localClockTimer)
      localClockTimer = null
    }
  }

  watch(
    [playing, playbackBackend, currentTrack, () => nativeAudioState.value?.loading, () => nativeAudioState.value?.buffering],
    updateLocalClockPolling,
  )
  onScopeDispose(() => {
    if (localClockTimer) clearInterval(localClockTimer)
    localClockTimer = null
  })

  function sessionPayload() {
    const track = currentTrack.value
    return {
      fileId: '',
      mediaItemId: null,
      entityType: 'track',
      entityId: track?.id ?? 0,
      positionSeconds: position.value,
      totalSeconds: duration.value || track?.duration || 0,
      paused: !playing.value,
      playbackAction: 'direct_play',
    }
  }

  // Called only after a renderer has actually accepted the track. External
  // services get their transient now-playing notification immediately; Heya's
  // activity panel starts a live heartbeat, but no history row is written.
  function beginTrack(track: Track) {
    if (track.id <= 0 || track.isStream || playbackBackend.value === 'cast') return
    trackStartedAtUnix = Math.floor(Date.now() / 1000)
    scrobbledTrackId.value = null
    alog('scrobble', `now playing "${track.title}"`)
    void recordPlayback({
      entity_type: 'track',
      entity_id: track.id,
      position_seconds: Math.floor(position.value),
      total_seconds: track.duration || 0,
      completed: false,
      source: track.source ?? '',
    })
    nowPlaying.start(sessionPayload)
  }

  // A permanent play exists only for a natural track completion. Manual
  // next/previous/stop never call this, so album-search skipping is not
  // mistaken for taste feedback.
  function completeTrack(track: Track) {
    if (track.id <= 0) return
    if (scrobbledTrackId.value === track.id) return
    scrobbledTrackId.value = track.id
    const listenedSecs = Math.floor(listenedSeconds || track.duration)
    alog('scrobble', `completed "${track.title}" — ${listenedSecs}s heard`)
    void recordPlayback({
      entity_type: 'track',
      entity_id: track.id,
      position_seconds: listenedSecs,
      total_seconds: track.duration || 0,
      completed: true,
      started_at_unix: trackStartedAtUnix,
      source: track.source ?? '',
    })
  }

  // --- Normalization (replay gain) -----------------------------------------
  // Mode lives in audio settings:
  //   off    => native level, no gain
  //   track  => each track's own EBU R128 gain toward the configured LUFS target
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
      library_file_id: null,
      format: null,
      bitrate_kbps: null,
      sample_rate_hz: null,
      bit_depth: null,
      channels: null,
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

  function nativeProcessingSettings(): NativeAudioProcessingSettings {
    const eq = settings.eq.value
    const crossfade = settings.crossfade.value
    return {
      replayGainEnabled: settings.replayGain.value.mode !== 'off',
      eqEnabled: eq.enabled,
      eqBandsDb: eq.bands.slice(0, 10),
      preampDb: eq.preamp,
      postgainDb: eq.postgain,
      limiterEnabled: settings.dspChain.value.limiterEnabled,
      crossfeedEnabled: settings.crossfeed.value.enabled,
      crossfeedPreset: settings.crossfeed.value.preset,
      dspOrder: [...settings.dspChain.value.order],
      crossfadeMode: crossfade.mode,
      crossfadeSeconds: crossfade.durationSeconds,
      // Rust publishes bounded time-domain and FFT snapshots only while a
      // mounted visualizer is drawing them — every frame otherwise costs an
      // FFT + JSON serialize + webview eval for nobody.
      visualizerEnabled: nativeAnalyserDemandActive(),
    }
  }

  function normalizationGainDb(track: Track): number | undefined {
    const loudness = effectiveLoudness(track)
    if (!loudness) return undefined
    const gain = computeNormalizationGain(
      loudness.lufs,
      loudness.peak,
      settings.replayGain.value.targetLufs,
    )
    return Number.isFinite(gain) && gain > 0 ? 20 * Math.log10(gain) : undefined
  }

  function nativeTrackAnalysisUpdate(track: Track): NativeAudioTrackAnalysisUpdate {
    const data = playbackOf(track)
    return {
      trackId: track.id,
      gainDb: normalizationGainDb(track) ?? null,
      introEndMs: data.intro_end_ms,
      outroStartMs: data.outro_start_ms,
      fadeStartMs: data.fade_start_ms,
      silenceStartMs: data.silence_start_ms,
    }
  }

  async function syncNativeTrackAnalysis(trackId: number) {
    const backend = nativeAudioBackend.value
    if (!backend || playbackBackend.value !== 'native') return
    const track = currentTrack.value?.id === trackId
      ? currentTrack.value
      : queue.value.find(candidate => candidate.id === trackId)
    if (!track || !playbackDataCache.has(trackId)) return
    await backend.updateTrackAnalysis(nativeTrackAnalysisUpdate(track))
  }

  interface NativePlaybackGrantResponse {
    media_path: string
    playback_grant: string
    header_name: string
  }

  const NATIVE_PLAYBACK_GRANT_HEADER = 'X-Heya-Playback-Grant'

  async function buildNativeTrackRequest(
    track: Track,
    startPositionSeconds = 0,
    skipCrossfade = false,
  ): Promise<NativeAudioTrackRequest | null> {
    if (track.id <= 0 || track.isStream) return null
    await ensurePlaybackData(track.id)
    const data = playbackDataCache.get(track.id)
    if (!data?.library_file_id) return null
    const { $heya } = useNuxtApp()
    const grant = await $heya<NativePlaybackGrantResponse>('/api/playback/native/grants', {
      method: 'POST',
      body: {
        file_id: String(data.library_file_id),
        mode: 'direct',
      },
    })
    if (!grant.media_path
      || !grant.playback_grant
      || grant.header_name !== NATIVE_PLAYBACK_GRANT_HEADER) {
      throw new Error('HeyaClient and the server disagree on the native audio protocol')
    }
    const format = data.format?.toLowerCase().replace(/[^a-z0-9]/g, '').slice(0, 16) || undefined
    return {
      trackId: track.id,
      durationSeconds: track.duration || 0,
      albumKey: track.album_id ? `album:${track.album_id}` : track.album,
      formatHint: format,
      skipCrossfade,
      gainDb: normalizationGainDb(track),
      introEndMs: data.intro_end_ms ?? undefined,
      outroStartMs: data.outro_start_ms ?? undefined,
      fadeStartMs: data.fade_start_ms ?? undefined,
      silenceStartMs: data.silence_start_ms ?? undefined,
      media: {
        mediaUrl: new URL(grant.media_path, window.location.origin).href,
        playbackGrant: grant.playback_grant,
        ...(startPositionSeconds > 0 ? { startPositionSeconds } : {}),
      },
    }
  }

  async function ensureNativeAudioBackend(): Promise<NativeAudioPlaybackBackend | null> {
    if (nativeAudioBackend.value) return nativeAudioBackend.value
    if (!useClientSurface().isTauriClient.value) {
      alog('player', 'native audio skipped — client surface is browser')
      return null
    }
    if (!nativeAudioProbe) {
      nativeAudioProbe = waitForNativeAudioBridge()
        .then((handshake) => {
          if (!handshake) {
            alog('player', 'native audio unavailable — bridge handshake timed out')
            return null
          }
          if (!handshake.capabilities.available
            || handshake.capabilities.backend !== 'heya-rust-audio') {
            alog('player', 'native audio unavailable — capability handshake declined', handshake.capabilities)
            return null
          }
          const backend = useNativeAudioPlaybackBackend(handshake.bridge, handshake.capabilities)
          nativeAudioBackend.value = backend

          watch(() => backend.state.playing, (value) => {
            if (playbackBackend.value === 'native') playing.value = value
          })
          watch(() => backend.state.durationSeconds, (value) => {
            if (playbackBackend.value === 'native' && Number.isFinite(value) && value > 0) duration.value = value
          })
          // `currentTrackId` means Rust accepted the load; it does NOT mean a
          // decoder has produced PCM or that the deck is active. Only the
          // transient TrackStarted marker may advance queue identity or arm a
          // preload, otherwise the current load and its preload can both target
          // the same pending deck and cancel each other.
          watch(() => backend.state.startedTrackId, (trackId) => {
            if (playbackBackend.value !== 'native' || !trackId || trackId === nativeLastStartedTrackId) return
            nativeLastStartedTrackId = trackId
            nativeEndedHandled = false
            if (currentTrack.value?.id === trackId) {
              beginTrack(currentTrack.value)
              prepareTransition()
              return
            }
            const next = pendingNext?.id === trackId
              ? pendingNext
              : queue.value.find(track => track.id === trackId)
            if (!next) return
            const finished = currentTrack.value
            if (finished) completeTrack(finished)
            nativePreloadRetryTrackId = null
            advanceCurrentTo(next)
          })
          watch(() => backend.state.ended, (ended) => {
            if (playbackBackend.value === 'native' && ended && !nativeEndedHandled) {
              nativeEndedHandled = true
              void handleNativeEnded()
            }
          })
          watch(() => backend.state.error?.message, (message) => {
            if (!message || playbackBackend.value !== 'native') return
            playing.value = false
            useToast().toast.err(message)
          })
          watch([() => backend.state.preloadStatus, () => backend.state.preloadTrackId], ([status, trackId]) => {
            if (status === 'ready' && trackId === nativePreloadRetryTrackId) {
              nativePreloadRetryTrackId = null
              return
            }
            if (playbackBackend.value !== 'native' || status !== 'failed' || !trackId) return
            const next = pendingNext
            if (next?.id !== trackId) return
            alog('player', `native preload failed for "${next.title}" — retrying once`, backend.state.preloadError)
            if (nativePreloadRetryTrackId === trackId) return
            nativePreloadRetryTrackId = trackId
            if (nativePreloadRetryTimer) clearTimeout(nativePreloadRetryTimer)
            nativePreloadRetryTimer = setTimeout(() => {
              nativePreloadRetryTimer = null
              if (playbackBackend.value === 'native' && pendingNext?.id === trackId) {
                void prepareNativeTransition()
              }
            }, 350)
          })
          return backend
        })
        .catch(() => null)
    }
    return await nativeAudioProbe
  }

  async function playNativeTrack(track: Track, startPositionSeconds = 0): Promise<boolean> {
    const backend = await ensureNativeAudioBackend()
    if (!backend) return false
    try {
      // Resolve the physical output and its Heya-owned EQ profile before the
      // load request snapshots processing settings. This also handles app
      // launches where the Audio panel has never been opened.
      await useAudioDevices().ensureInitialized()
      const request = await buildNativeTrackRequest(track, startPositionSeconds)
      if (!request) return false
      // A native load owns audio authoritatively. Stop the browser engine
      // before asking Rust to start so both can never emit sound together.
      if (engineWired.value) ensureEngine().stop()
      await backend.load({
        processing: nativeProcessingSettings(),
        track: request,
      })
      playbackBackend.value = 'native'
      // The initial native state can arrive before `load()` resolves. Its
      // watcher deliberately ignores events until ownership flips to native,
      // so mirror the accepted snapshot once here as well.
      playing.value = backend.state.playing
      acceptLocalPosition(backend.state.positionSeconds)
      if (backend.state.durationSeconds > 0) duration.value = backend.state.durationSeconds
      // A load acknowledgement only means the command/session was accepted.
      // Wait for Rust's TrackStarted event before considering this deck active.
      nativeLastStartedTrackId = null
      nativeEndedHandled = false
      // A tiny/cached source can start before load() finishes reconciling the
      // initial snapshot. The watcher ignored that event while browser still
      // owned playback, so consume the authoritative marker once here.
      if (backend.state.startedTrackId === track.id) {
        nativeLastStartedTrackId = track.id
        beginTrack(track)
      }
      await backend.setVolume(volume.value / 100)
      await backend.setMuted(muted.value)
      settings.registerEngineBridge(backend, () => {
        if (playbackBackend.value === 'native') {
          void backend.updateProcessing(nativeProcessingSettings()).catch(() => {})
          const activeTrack = currentTrack.value
          if (activeTrack) void backend.updateTrackAnalysis(nativeTrackAnalysisUpdate(activeTrack)).catch(() => {})
          prepareTransition()
        }
      })
      return true
    } catch (error) {
      await backend.dispose()
      playbackBackend.value = 'browser'
      alog('player', 'native audio initialization failed — using browser engine', error)
      return false
    }
  }

  async function disposeNativeAudio() {
    nativePreloadGeneration++
    nativePreloadRetryTrackId = null
    if (nativePreloadRetryTimer) clearTimeout(nativePreloadRetryTimer)
    nativePreloadRetryTimer = null
    const backend = nativeAudioBackend.value
    if (backend) await backend.dispose()
    if (playbackBackend.value === 'native') playbackBackend.value = 'browser'
  }

  async function probeNativeAudio() {
    return await ensureNativeAudioBackend()
  }

  async function refreshNativeAudioOutputs(): Promise<boolean> {
    const backend = await ensureNativeAudioBackend()
    if (!backend?.capabilities.outputDeviceSelection) return false
    try {
      await backend.refreshOutputDevices()
      return true
    } catch (error) {
      alog('player', 'could not enumerate native audio outputs', error)
      return false
    }
  }

  async function setNativeAudioOutputDevice(deviceId: string | null): Promise<boolean> {
    const backend = await ensureNativeAudioBackend()
    if (!backend?.capabilities.outputDeviceSelection) return false

    const track = currentTrack.value
    const ownsPlayback = playbackBackend.value === 'native' && !!track
    const resumeAt = position.value
    const wasPlaying = playing.value
    try {
      await backend.setOutputDevice(deviceId)
      // Apply the new device's profile before a replacement renderer snapshots
      // nativeProcessingSettings(). The outer Output-tab action will observe
      // the same key and remain idempotent.
      await useAudioDevices().refresh()
      // A CPAL stream is bound to its device when opened. Replace the current
      // renderer at the same position so the persisted choice takes effect
      // immediately; idle selection needs no renderer lifecycle work.
      if (ownsPlayback && track) {
        if (!await playNativeTrack(track, resumeAt)) return false
        if (!wasPlaying) await backend.pause().catch(() => {})
        prepareTransition()
      }
      return true
    } catch (error) {
      alog('player', 'could not change native audio output', error)
      return false
    }
  }

  async function prepareNativeTransition() {
    const backend = nativeAudioBackend.value
    if (!backend || playbackBackend.value !== 'native') return
    const current = currentTrack.value
    if (!current || nativeLastStartedTrackId !== current.id) return
    const generation = ++nativePreloadGeneration
    const next = peekNextTrack()
    pendingNext = next
    if (nativePreloadRetryTrackId !== next?.id) nativePreloadRetryTrackId = null
    if (!next || next.isStream) return
    try {
      const request = await buildNativeTrackRequest(next, 0, !!current && shouldSuppressCrossfade(
        {
          trackId: current.id,
          albumId: current.album_id,
          albumName: current.album,
          discNumber: current.disc_number,
          trackNumber: current.track_number,
        },
        {
          trackId: next.id,
          albumId: next.album_id,
          albumName: next.album,
          discNumber: next.disc_number,
          trackNumber: next.track_number,
        },
      ))
      if (!request || generation !== nativePreloadGeneration || playbackBackend.value !== 'native') return
      await backend.preload(request)
    } catch (error) {
      alog('player', `native preload failed for "${next.title}" — end transition will cold-load`, error)
    }
  }

  async function handleNativeEnded() {
    const finished = currentTrack.value
    if (finished) completeTrack(finished)
    if (sleepAtTrackEnd.value) {
      sleepAtTrackEnd.value = false
      pause()
      void nowPlaying.end()
      return
    }
    let next = peekNextTrack()
    if (!next && await ensureSimilarAutoplayQueue(true)) next = peekNextTrack()
    if (!next) {
      playing.value = false
      void nowPlaying.end()
      return
    }
    // Normally the Rust scheduler starts a preloaded deck before this path.
    // Reaching EOF means preload was late or failed, so replace the renderer
    // session with an explicit cold load.
    await playQueueSuccessor(next, 'ended')
  }

  function applyActiveNorm(e: ReturnType<typeof useAudioEngine>, track: Track | null) {
    const eff = track ? effectiveLoudness(track) : null
    if (eff) e.setActiveNormalization(eff.lufs, eff.peak, settings.replayGain.value.targetLufs)
    else e.resetActiveNormalization()
  }
  function applyPendingNorm(e: ReturnType<typeof useAudioEngine>, track: Track) {
    const eff = effectiveLoudness(track)
    if (eff) e.setPendingNormalization(eff.lufs, eff.peak, settings.replayGain.value.targetLufs)
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
          if (playbackBackend.value === 'native') {
            void ensurePlaybackData(trackId)
              .then(() => syncNativeTrackAnalysis(trackId))
              .catch(() => {})
          } else {
            void ensureAnalysisAndArm()
          }
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
    // The server pointer is authoritative for queue order, but it moves over
    // HTTP/WS while the renderer changes tracks locally. Never arm a transition
    // unless both sides agree which queue row is currently audible; otherwise
    // the next row can temporarily be the current track itself and its smart
    // boundary gets scheduled onto the wrong audio deck.
    if (!localMode.value) {
      const pointerTrackId = qs.items[idx]?.track_id
      if (!currentTrack.value || pointerTrackId !== currentTrack.value.id) return null
    }
    const next = queue.value[idx + 1]
    if (next) return next
    return repeatMode.value === 'all' ? (queue.value[0] ?? null) : null
  }

  // Arm the next transition from the data we already have (synchronous): choose
  // gapless / timed-crossfade / smart, set the scheduler accordingly, and
  // preload the pending deck. Called after every play / advance / queue or
  // settings change, and re-run by ensureAnalysisAndArm once analysis lands.
  function armSync() {
    if (!engineWired.value) return
    // Remote output: no local decks to arm — transitions are API calls
    // driven by castTrackEnded(), not the scheduler.
    if (useCastStore().engaged) {
      pendingNext = null
      pendingPlan = null
      prefetchedTrackId = null
      preloadingTrackId = null
      return
    }
    if (playbackBackend.value === 'native') return
    const e = ensureEngine() as EngineWithScheduler
    pendingNext = null
    pendingMode = 'gapless'
    pendingPlan = null

    if (loadingTrackGeneration !== null) {
      e.scheduler?.setSmartTransitionPoint(null)
      e.scheduler?.setMode('gapless')
      return
    }

    const cur = currentTrack.value
    if (!cur || cur.isStream) {
      e.scheduler?.setSmartTransitionPoint(null)
      e.scheduler?.setMode('gapless')
      return
    }

    const next = peekNextTrack()
    pendingNext = next
    if (prefetchedTrackId !== next?.id) prefetchedTrackId = null
    if (preloadingTrackId !== next?.id) preloadingTrackId = null
    if (!next) {
      e.scheduler?.setSmartTransitionPoint(null)
      e.scheduler?.setMode('gapless')
      alog('xfade', `arm: no next track (end of queue, repeat ${repeatMode.value})`)
      return
    }

    const cf = settings.crossfade.value
    let mode: 'gapless' | 'crossfade' | 'smart' = cf.mode
    let suppressed = false
    if (mode === 'crossfade' || mode === 'smart') {
      const same = shouldSuppressCrossfade(
        {
          trackId: cur.id,
          albumId: cur.album_id,
          albumName: cur.album,
          discNumber: cur.disc_number,
          trackNumber: cur.track_number,
        },
        {
          trackId: next.id,
          albumId: next.album_id,
          albumName: next.album,
          discNumber: next.disc_number,
          trackNumber: next.track_number,
        },
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
      if (url && prefetchedTrackId !== next.id && preloadingTrackId !== next.id) {
        preloadingTrackId = next.id
        const targetId = next.id
        // armSync stays synchronous (called straight from watchers), so the
        // cache lookup that might turn `url` into a blob: URL happens off to
        // the side: resolvePlayable never does network I/O itself (only
        // sync() does), so this settles almost immediately either way. The
        // targetId check guards against a late resolve loading a track that's
        // no longer the armed "next" (queue changed again in the meantime).
        void prefetchManager.resolvePlayable(next)
          .then(async (playable) => {
            if (preloadingTrackId !== targetId || pendingNext?.id !== targetId) return
            await e.loadNext(playable || url)
            if (preloadingTrackId !== targetId || pendingNext?.id !== targetId) return
            prefetchedTrackId = targetId
            preloadingTrackId = null
          })
          .catch((err) => {
            alog('xfade', `preload FAILED for "${next.title}" — will cold-play`, err)
            if (prefetchedTrackId === targetId) prefetchedTrackId = null
            if (preloadingTrackId === targetId) preloadingTrackId = null
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
    if (!engineWired.value) return
    if (playbackBackend.value === 'native') return
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
    if (loadingTrackGeneration !== null) return
    // A short queue begins its recommendation request while there is still
    // plenty of playback time left, so gapless/crossfade can preload the
    // generated next track exactly like an ordinary queued track.
    void ensureSimilarAutoplayQueue()
    // Remote output: skip entirely — ensureAnalysisAndArm would otherwise
    // spin up an AudioContext (via ensureEngine) that nothing will play on.
    if (useCastStore().engaged) return
    if (playbackBackend.value === 'native') {
      void prepareNativeTransition()
      return
    }
    armSync()
    void ensureAnalysisAndArm()
  }

  // Fired by the scheduler `crossfadeDuration` before the end — CROSSFADE ONLY.
  // The outgoing deck keeps playing (and fading) through to its real end during
  // the overlap, so nothing is clipped. Gapless is NOT handled here: pausing the
  // outgoing deck early would lop off its tail; it swaps on `ended` instead (see
  // handleEnded). A late pending deck also falls back at natural EOF so the
  // outgoing tail is never clipped by a cold load at the smart boundary.
  async function handleTransition() {
    if (transitioning) return
    if (pendingMode !== 'crossfade') return // gapless handled on `ended`
    const e = ensureEngine()
    const cur = currentTrack.value
    if (!cur || cur.isStream) return
    const next = pendingNext
    if (!next) return

    transitioning = true
    const preloaded = prefetchedTrackId === next.id
    if (!preloaded) {
      // A cold load at the smart boundary would cut off the outgoing tail—the
      // exact opposite of a graceful fallback. Leave it playing to natural EOF;
      // handleEnded will use the pending deck if it becomes ready in time, or
      // perform the cold load only after the final sample.
      alog('xfade', `crossfade deferred for "${next.title}" — pending deck not ready; preserving track tail`)
      pendingMode = 'gapless'
      pendingPlan = null
      transitioning = false
      return
    }
    alog('xfade', `CROSSFADE → "${next.title}" (preloaded ✓)`)
    try {
      // The outgoing track is at ≈completion (it plays through the fade to its
      // real end). Scrobble it now — its deck `ended` won't fire post-swap.
      completeTrack(cur)

      // 'timed' routes the pending deck through the signal chain so EQ/limiter
      // apply during the overlap.
      await e.transition('timed', pendingPlan ?? undefined)
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
    preloadingTrackId = null
    if (next.duration && Number.isFinite(next.duration)) duration.value = next.duration
    playing.value = true
    beginTrack(next)
    prepareTransition()
  }

  // Tell the server the renderer crossed a track boundary so its pointer
  // follows. Fire-and-forget: playback already advanced locally; the
  // from_item_id guard makes a racing double-report a no-op, and the
  // response/WS event reconciles the mirror.
  function reportAdvance(reason: 'ended' | 'skip' | 'prev') {
    if (localMode.value) return
    const from = qs.currentItemID
    if (!from) return
    void qs.advance(from, reason).catch(() => { /* WS view reconciles */ })
  }

  // Start a known forward queue successor without letting play(track) turn
  // the transition into /queue/jump. Jumping updates the pointer but bypasses
  // the track-boundary work that refills DJs. The renderer stays responsive by
  // reporting fire-and-forget, matching the preloaded deck-swap path above.
  async function playQueueSuccessor(next: Track, reason: 'ended' | 'skip') {
    if (localMode.value) {
      await play(next)
      return
    }
    reportAdvance(reason)
    await play(next, { skipQueueSync: true })
  }

  // --- Endless similar autoplay -------------------------------------------
  function similarSeedKey(seed: SimilarAutoplaySeed): string {
    if (seed.kind === 'track') return `track:${seed.track_id}`
    if (seed.kind === 'artist') return `artist:${seed.artist_id}`
    return `album:${seed.album_id}`
  }

  function uniqueSimilarSeeds(seeds: SimilarAutoplaySeed[]): SimilarAutoplaySeed[] {
    const seen = new Set<string>()
    return seeds.filter((seed) => {
      const key = similarSeedKey(seed)
      if (seen.has(key)) return false
      seen.add(key)
      return true
    })
  }

  function spreadSimilarSeeds(seeds: SimilarAutoplaySeed[]): SimilarAutoplaySeed[] {
    if (seeds.length <= SIMILAR_AUTOPLAY_MAX_SEEDS) return seeds
    const selected: SimilarAutoplaySeed[] = []
    for (let i = 0; i < SIMILAR_AUTOPLAY_MAX_SEEDS; i++) {
      const index = Math.round(i * (seeds.length - 1) / (SIMILAR_AUTOPLAY_MAX_SEEDS - 1))
      const seed = seeds[index]
      if (seed) selected.push(seed)
    }
    return uniqueSimilarSeeds(selected)
  }

  function rememberSimilarAutoplayTracks(trackIDs: number[]) {
    for (const id of trackIDs) {
      if (id > 0) similarAutoplaySeenTrackIDs.add(id)
    }
    while (similarAutoplaySeenTrackIDs.size > SIMILAR_AUTOPLAY_MAX_EXCLUDES) {
      const oldest = similarAutoplaySeenTrackIDs.values().next().value as number | undefined
      if (oldest == null) break
      similarAutoplaySeenTrackIDs.delete(oldest)
    }
  }

  function queueSourceSeeds(source: QueueSourceInput): SimilarAutoplaySeed[] {
    if (source.kind === 'tracks') {
      return (source.track_ids ?? []).filter((id) => id > 0).map((track_id) => ({ kind: 'track', track_id }))
    }
    if (source.kind === 'artist' && (source.id ?? 0) > 0) return [{ kind: 'artist', artist_id: source.id! }]
    if (source.kind === 'album' && (source.id ?? 0) > 0) return [{ kind: 'album', album_id: source.id! }]
    // Playlist/library/genre queues use their materialized track window. That
    // keeps continuation working when the optional text-audio model is absent.
    return []
  }

  function resetSimilarAutoplayContext(source?: QueueSourceInput, viewItems: QueueItem[] = []) {
    similarAutoplayGeneration++
    similarAutoplayRetryAfter = 0
    similarAutoplayIntentTrackIDs = source?.kind === 'tracks'
      ? [...new Set((source.track_ids ?? []).filter((id) => id > 0))]
      : []
    similarAutoplaySeenTrackIDs.clear()
    const visibleIDs = viewItems.map((item) => item.track_id)
    rememberSimilarAutoplayTracks([...similarAutoplayIntentTrackIDs, ...visibleIDs])
    similarAutoplayContext = uniqueSimilarSeeds([
      ...(source ? queueSourceSeeds(source) : []),
      ...visibleIDs.map((track_id) => ({ kind: 'track' as const, track_id })),
    ])
  }

  function recalculateSimilarAutoplayContext() {
    similarAutoplayGeneration++
    similarAutoplayRetryAfter = 0
    const visibleIDs = qs.items.map((item) => item.track_id).filter((id) => id > 0)
    const nonTrackSeeds = similarAutoplayContext.filter((seed) => seed.kind !== 'track')
    similarAutoplayContext = uniqueSimilarSeeds([
      ...visibleIDs.map((track_id) => ({ kind: 'track' as const, track_id })),
      ...nonTrackSeeds,
      ...similarAutoplayIntentTrackIDs.map((track_id) => ({ kind: 'track' as const, track_id })),
    ])
    rememberSimilarAutoplayTracks(visibleIDs)
  }

  function addSimilarAutoplayIntent(trackIDs: number[]) {
    const valid = trackIDs.filter((id) => id > 0)
    if (!valid.length) return
    similarAutoplayIntentTrackIDs = [...new Set([...similarAutoplayIntentTrackIDs, ...valid])]
    recalculateSimilarAutoplayContext()
    rememberSimilarAutoplayTracks(valid)
  }

  function removeSimilarAutoplayIntent(trackID: number) {
    similarAutoplayIntentTrackIDs = similarAutoplayIntentTrackIDs.filter((id) => id !== trackID)
    similarAutoplayContext = similarAutoplayContext.filter((seed) => seed.kind !== 'track' || seed.track_id !== trackID)
    similarAutoplayGeneration++
    similarAutoplayRetryAfter = 0
    // Keep the removed track in `seen`: removing it from Up Next should never
    // cause the recommender to immediately put it back.
    rememberSimilarAutoplayTracks([trackID])
  }

  function ensureSimilarAutoplayQueue(force = false): Promise<boolean> {
    if (!similarAutoplayEnabled.value || localMode.value || qs.djMode !== 'off') return Promise.resolve(false)
    if (!currentTrack.value || currentTrack.value.id <= 0 || repeatMode.value !== 'off') return Promise.resolve(false)
    const remaining = Math.max(0, qs.total - qs.currentIndex - 1)
    if (!force && remaining > SIMILAR_AUTOPLAY_REFILL_AT) return Promise.resolve(false)
    if (!force && Date.now() < similarAutoplayRetryAfter) return Promise.resolve(false)

    if (!similarAutoplayContext.length) recalculateSimilarAutoplayContext()
    if (!similarAutoplayContext.length) {
      similarAutoplayContext = [{ kind: 'track', track_id: currentTrack.value.id }]
      rememberSimilarAutoplayTracks([currentTrack.value.id])
    }
    const seeds = spreadSimilarSeeds(similarAutoplayContext)
    if (!seeds.length) return Promise.resolve(false)

    const generation = similarAutoplayGeneration
    if (similarAutoplayRequest?.generation === generation) return similarAutoplayRequest.promise
    const excludeTrackIDs = [...similarAutoplaySeenTrackIDs]
    const { $heya } = useNuxtApp()
    similarAutoplayRequests++
    similarAutoplayLoading.value = true

    const promise = (async () => {
      try {
        const res = await $heya('/api/music/radio', {
          method: 'POST',
          body: {
            seed: seeds[0],
            seeds,
            limit: SIMILAR_AUTOPLAY_BATCH_SIZE,
            exclude_track_ids: excludeTrackIDs,
          } as never,
        }) as SimilarAutoplayResponse
        if (generation !== similarAutoplayGeneration || !similarAutoplayEnabled.value) return false

        const freshIDs = [...new Set((res.tracks ?? []).map((track) => track.track_id))]
          .filter((id) => id > 0 && !similarAutoplaySeenTrackIDs.has(id))
        if (!freshIDs.length) {
          similarAutoplayRetryAfter = Date.now() + 60_000
          return false
        }
        const added = await qs.enqueue(freshIDs, 'end')
        if (added > 0) rememberSimilarAutoplayTracks(freshIDs)
        if (generation !== similarAutoplayGeneration || !similarAutoplayEnabled.value) return added > 0
        if (added > 0) prepareTransition()
        return added > 0
      } catch (error) {
        if (generation === similarAutoplayGeneration) similarAutoplayRetryAfter = Date.now() + 60_000
        console.warn('similar autoplay refill failed:', error)
        return false
      } finally {
        similarAutoplayRequests = Math.max(0, similarAutoplayRequests - 1)
        similarAutoplayLoading.value = similarAutoplayRequests > 0
        if (similarAutoplayRequest?.generation === generation) similarAutoplayRequest = null
      }
    })()
    similarAutoplayRequest = { generation, promise }
    return promise
  }

  function setSimilarAutoplayEnabled(enabled: boolean) {
    similarAutoplayEnabled.value = enabled
    similarAutoplayGeneration++
    similarAutoplayRetryAfter = 0
    try { localStorage.setItem(SIMILAR_AUTOPLAY_STORAGE_KEY, enabled ? '1' : '0') } catch { /* non-fatal */ }
    if (enabled) {
      recalculateSimilarAutoplayContext()
      void ensureSimilarAutoplayQueue()
    }
  }

  watch(
    [similarAutoplayEnabled, localMode, () => qs.total, () => qs.currentIndex, repeatMode, () => qs.djMode],
    () => { void ensureSimilarAutoplayQueue() },
  )

  async function setDJMode(mode: DJMode) {
    if (!djAvailable.value && mode !== 'off') return
    djChanging.value = true
    similarAutoplayGeneration++
    try {
      await qs.setDJMode(mode)
      prepareTransition()
    } finally {
      djChanging.value = false
    }
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
    localMode.value = false
    originalOrder.value = []
    resetSimilarAutoplayContext()
    let view
    try {
      view = await qs.replace(source, opts?.startTrack?.id ?? opts?.startTrackId ?? 0, !!opts?.shuffle, queueOutputID())
    } catch {
      useToast().toast.err('Nothing playable in this selection')
      return
    }
    resetSimilarAutoplayContext(source, view.items)
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
    resetSimilarAutoplayContext()
    const first = start ?? tracks[0]
    if (first) await play(first, { skipQueueSync: true })
  }

  // syncQueuePointer keeps the server pointer in step with a direct
  // play(track): an in-queue track becomes a jump; anything else becomes
  // a one-track queue. jumpTo()/playContext() pass skipQueueSync — they
  // already positioned the pointer precisely.
  async function syncQueuePointer(track: Track) {
    if (localMode.value || track.isStream || track.id <= 0) return
    const item = qs.items.find((i) => i.track_id === track.id)
    try {
      if (item) {
        if (item.item_id !== qs.currentItemID) await qs.jump(item.item_id)
      } else {
        const source: QueueSourceInput = { kind: 'tracks', track_ids: [track.id] }
        resetSimilarAutoplayContext(source)
        const view = await qs.replace(source, track.id, false, queueOutputID())
        resetSimilarAutoplayContext(source, view.items)
      }
    } catch {
      // Playback may continue independently, but peekNextTrack's pointer
      // identity guard keeps transitions disabled until WS/API reconciliation.
    }
  }

  function invalidateManualTransition() {
    transitioning = false
    pendingNext = null
    pendingMode = 'gapless'
    pendingPlan = null
    prefetchedTrackId = null
    preloadingTrackId = null
    nativePreloadGeneration++
    nativePreloadRetryTrackId = null
    if (nativePreloadRetryTimer) clearTimeout(nativePreloadRetryTimer)
    nativePreloadRetryTimer = null

    if (playbackBackend.value === 'native' && nativeAudioBackend.value) {
      // The command captures the old renderer session synchronously. Let it
      // stop in parallel with analysis/grant resolution for the replacement.
      void nativeAudioBackend.value.stop().catch(() => {})
    } else if (engineWired.value) {
      ensureEngine().cancelPendingTransition()
    }
  }

  function settleTrackLoad(generation: number) {
    if (loadingTrackGeneration === generation) loadingTrackGeneration = null
  }

  async function play(track?: Track, opts?: {
    skipQueueSync?: boolean
    startPositionSeconds?: number
  }) {
    // Remote output: the queue/track state stays client-side (Phase 2),
    // but the audio path is an API call — never touch the local engine.
    if (useCastStore().engaged) {
      if (track && !opts?.skipQueueSync) await syncQueuePointer(track)
      await playViaCast(track)
      return
    }
    if (track) {
      // Never play a track whose file was removed from disk.
      if (track.available === false) return
      const gen = ++playGeneration
      loadingTrackGeneration = gen
      invalidateManualTransition()
      // Preserve browser autoplay activation before queue/API work yields.
      void resumeContext()
      if (!opts?.skipQueueSync) await syncQueuePointer(track)
      if (gen !== playGeneration) return
      // Rendering locally makes this tab the active output.
      if (!localMode.value && !qs.isActiveOutput) void qs.claim()
      currentTrack.value = track
      const startPositionSeconds = Math.max(0, Math.min(
        opts?.startPositionSeconds ?? 0,
        track.duration > 0 ? track.duration : Number.POSITIVE_INFINITY,
      ))
      position.value = startPositionSeconds
      scrobbledTrackId.value = null
      listenedSeconds = 0
      lastTickTime = startPositionSeconds
      trackStartedAtUnix = 0
      if (track.duration && Number.isFinite(track.duration)) duration.value = track.duration
      alog('player', `play "${track.title}" #${track.id}${track.isStream ? ' (stream)' : ''}`)
      // Block on analysis so the gain is right from the FIRST sample. Without
      // this, album/auto replay gain (which can differ a lot from track gain)
      // would apply a beat late after the async fetch and audibly jump. Auto-
      // advance doesn't need it — the pending deck is fetched + leveled ahead.
      if (track.id > 0 && !track.isStream) await ensurePlaybackData(track.id)
      // A newer play() superseded us during the fetch — bail rather than load a
      // stale track onto the active deck.
      if (gen !== playGeneration) return
      if (!track.isStream && await playNativeTrack(track, startPositionSeconds)) {
        if (gen !== playGeneration) return
        settleTrackLoad(gen)
        // beginTrack + transition preloading are driven by Rust's authoritative
        // TrackStarted event, not the earlier load acknowledgement.
        prepareTransition()
        return
      }
      if (gen !== playGeneration) return
      await disposeNativeAudio()
      const e = ensureEngine()
      playbackBackend.value = 'browser'
      const networkUrl = resolveStreamUrl(track)
      if (!networkUrl) { settleTrackLoad(gen); return }
      applyActiveNorm(e, track)
      // Cache lookup (resolvePlayable never does network I/O itself, only a
      // fast Cache.match) — check staleness again after it, same reasoning.
      const playUrl = track.isStream
        ? networkUrl
        : await prefetchManager.resolvePlayable(track).catch(() => undefined)
      if (gen !== playGeneration) return
      try {
        await e.play(playUrl || networkUrl, startPositionSeconds)
      } catch {
        if (gen === playGeneration) {
          settleTrackLoad(gen)
          playing.value = false
        }
        return
      }
      if (gen !== playGeneration) return
      playing.value = true
      settleTrackLoad(gen)
      beginTrack(track)
      // Preload the next track onto the pending deck for a gap-free hand-off.
      prepareTransition()
      return
    }
    // No track passed — resume current. Queue restore normally mirrors the
    // server pointer into currentTrack, but play() must also be safe when the
    // user clicks before that plugin has finished its handoff.
    if (!currentTrack.value && !localMode.value) {
      const idx = qs.currentWindowIndex
      const restored = idx >= 0 ? queue.value[idx] : undefined
      if (restored) {
        currentTrack.value = restored
        position.value = qs.positionSeconds
        if (restored.duration) duration.value = restored.duration
      }
    }
    if (!currentTrack.value) return
    if (playbackBackend.value === 'native' && nativeAudioBackend.value) {
      try {
        await nativeAudioBackend.value.play()
        playing.value = true
        void nowPlaying.heartbeat(sessionPayload())
      } catch {
        playing.value = false
      }
      return
    }
    // A page reload preserves the server queue/output identity but destroys
    // the browser/native renderer. Treat the first resume in the fresh page
    // as a cold handoff even when this tab still owns the output; resuming a
    // newly-created WebAudio engine otherwise succeeds while playing nothing.
    const needsColdLoad = !engineWired.value
    if (needsColdLoad) {
      localHandoff = {
        trackId: currentTrack.value.id,
        position: localMode.value ? position.value : qs.positionSeconds,
      }
    }
    // Mirror tab pressing play = "play here": claim the output and pick
    // up from the server's position via the same cold-load handoff the
    // cast-disconnect path uses.
    if (!localMode.value && !qs.isActiveOutput) {
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
      // The server pointer is already authoritative. Skipping queue sync is
      // also important for queues containing the same track more than once.
      await play(t, { skipQueueSync: true, startPositionSeconds: h.position })
      return
    }
    const e = ensureEngine()
    try {
      await e.resume()
      playing.value = true
      void nowPlaying.heartbeat(sessionPayload())
    } catch {
      playing.value = false
    }
  }

  // The cast-mode half of play(): same queue bookkeeping, remote transport.
  async function playViaCast(track?: Track) {
    const cast = useCastStore()
    void nowPlaying.end()
    if (playbackBackend.value === 'native') await disposeNativeAudio()
    playbackBackend.value = 'cast'
    if (track) {
      if (track.available === false) return
      if (track.isStream || track.id <= 0) {
        useToast().toast.err('Radio streams can\'t be cast yet')
        return
      }
      transitioning = false
      prefetchedTrackId = null
      preloadingTrackId = null
      pendingNext = null
      currentTrack.value = track
      position.value = 0
      scrobbledTrackId.value = null
      listenedSeconds = 0
      lastTickTime = 0
      trackStartedAtUnix = 0
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
    const cast = useCastStore()
    if (cast.engaged) {
      playing.value = false // optimistic; WS mirror confirms
      void cast.pause().catch(() => { /* session already gone */ })
      return
    }
    if (playbackBackend.value === 'native' && nativeAudioBackend.value) {
      playing.value = false
      void nativeAudioBackend.value.pause().catch(() => {})
      void nowPlaying.heartbeat(sessionPayload())
      return
    }
    if (!engineWired.value) return
    playing.value = false
    ensureEngine().pause()
    void nowPlaying.heartbeat(sessionPayload())
  }

  async function togglePlay() {
    if (playing.value) pause()
    else await play()
  }

  // seek takes a 0-1 fraction (legacy API the UI uses).
  function seek(pct: number) {
    const target = Math.max(0, Math.min(1, pct)) * (duration.value || 0)
    alog('player', `seek ${target.toFixed(2)}s via ${playbackBackend.value}`)
    if (useCastStore().engaged) {
      // While paused between tracks (no session) this still moves the
      // frozen position — the next re-cast starts from it.
      void useCastStore().seekTo(target).catch(() => { /* WS restores truth */ })
    } else if (playbackBackend.value === 'native' && nativeAudioBackend.value) {
      void nativeAudioBackend.value.seek(target).catch((error) => {
        alog('player', 'native audio seek was rejected', error)
      })
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
    if (useCastStore().engaged) {
      // The slider is the DEVICE stream volume while casting. Deliberately
      // not persisted — localStorage keeps the local listening level for
      // when the output comes back.
      useCastStore().setVolume(clamped)
      return
    }
    if (playbackBackend.value === 'native' && nativeAudioBackend.value) {
      void nativeAudioBackend.value.setVolume(clamped / 100).catch(() => {})
      void nativeAudioBackend.value.setMuted(muted.value).catch(() => {})
    } else if (engineWired.value) ensureEngine().setVolume(muted.value ? 0 : clamped / 100)
    persistVolumePrefs(volume.value, muted.value)
  }

  function toggleMute() {
    muted.value = !muted.value
    if (useCastStore().engaged) {
      // No mute verb on the receiver — drive the stream volume to 0 and
      // back. `muted` stays a local flag so unmute knows the level.
      useCastStore().setVolume(muted.value ? 0 : volume.value)
      return
    }
    if (playbackBackend.value === 'native' && nativeAudioBackend.value) {
      void nativeAudioBackend.value.setMuted(muted.value).catch(() => {})
    } else if (engineWired.value) ensureEngine().setVolume(muted.value ? 0 : volume.value / 100)
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
  async function toggleShuffle() {
    if (!localMode.value) {
      if (qs.djMode !== 'off') return
      try {
        await qs.setShuffle(!qs.shuffled)
        // The modes event's structural refetch is intentionally async. Fetch
        // here as well so the next radio centroid is rebuilt from the actual
        // shuffled queue the listener sees, including anything just added.
        await qs.refetch()
        recalculateSimilarAutoplayContext()
        prepareTransition()
      } catch { /* the WS mirror will restore the authoritative mode */ }
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
    let next = forwardNext()
    if (!next && await ensureSimilarAutoplayQueue(true)) next = forwardNext()
    if (next) await playQueueSuccessor(next, 'skip')
    else playing.value = false
  }

  // Manual "next": if the next track is already buffered on the pending deck,
  // swap to it instantly (no cold-load gap, no src-clobber); otherwise cold play.
  async function nextTrack() {
    let next = forwardNext()
    if (!next && await ensureSimilarAutoplayQueue(true)) next = forwardNext()
    if (!next) { playing.value = false; return }
    if (useCastStore().isClientDevice) {
      await playQueueSuccessor(next, 'skip')
      return
    }
    if (playbackBackend.value === 'native') {
      await playQueueSuccessor(next, 'skip')
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
    await playQueueSuccessor(next, 'skip')
  }

  async function prevTrack() {
    if (position.value > 3) {
      if (useCastStore().engaged) {
        void useCastStore().seekTo(0).catch(() => { /* WS restores truth */ })
      } else if (playbackBackend.value === 'native' && nativeAudioBackend.value) {
        void nativeAudioBackend.value.seek(0).catch(() => {})
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
    if (finished) completeTrack(finished)

    // Sleep timer set to "end of track" — stop here instead of advancing.
    if (sleepAtTrackEnd.value) {
      sleepAtTrackEnd.value = false
      pause()
      void nowPlaying.end()
      alog('player', 'sleep timer: stopped at end of track')
      return
    }

    let next = peekNextTrack() // queue order; returns current for repeat-one
    if (!next && await ensureSimilarAutoplayQueue(true)) next = peekNextTrack()
    if (!next) {
      alog('player', `queue ended after "${finished?.title}"`)
      playing.value = false
      void nowPlaying.end()
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
      try {
        await qs.jump(item.item_id)
      } catch {
        return
      }
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
  async function removeFromQueue(index: number) {
    if (index <= currentIndex.value) return
    if (index >= queue.value.length) return
    if (!localMode.value) {
      const item = qs.items[index]
      if (item) {
        removeSimilarAutoplayIntent(item.track_id)
        await qs.removeItem(item.item_id)
        recalculateSimilarAutoplayContext()
      }
    } else {
      localQueue.value.splice(index, 1)
    }
    prepareTransition()
  }

  // moveInQueue reorders an upcoming track. Same guards as remove.
  async function moveInQueue(from: number, to: number) {
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
      await qs.moveItem(item.item_id, pred ? pred.item_id : 0)
      recalculateSimilarAutoplayContext()
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
      const ids = list.map((t) => t.id)
      await qs.enqueue(ids, 'end')
      addSimilarAutoplayIntent(ids)
      prepareTransition()
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
      const ids = list.map((t) => t.id)
      await qs.enqueue(ids, 'next')
      addSimilarAutoplayIntent(ids)
      prepareTransition()
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
  async function clearUpcoming() {
    if (!localMode.value) {
      similarAutoplayGeneration++
      await qs.clearUpcoming()
      recalculateSimilarAutoplayContext()
      prepareTransition()
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
    if (useCastStore().engaged) void useCastStore().stopSession()
    if (!localMode.value) void qs.clearAll() // explicit gesture — labeled "stop & clear queue"
    localHandoff = null
    void disposeNativeAudio()
    if (engineWired.value) ensureEngine().stop()
    playbackBackend.value = 'browser'
    playing.value = false
    currentTrack.value = null
    localMode.value = false
    localQueue.value = []
    originalOrder.value = []
    resetSimilarAutoplayContext()
    position.value = 0
    duration.value = 0
    transitioning = false
    prefetchedTrackId = null
    preloadingTrackId = null
    pendingNext = null
    listenedSeconds = 0
    lastTickTime = 0
    trackStartedAtUnix = 0
    void nowPlaying.end()
  }

  // --- Cast output orchestration (docs/cast-plan.md Phase 2) ----------------
  // The queue, shuffle, repeat, and track-advance logic above stays the
  // owner of WHAT plays; these switch WHERE it plays.

  // Engage a device and hand the current playback off to it mid-track.
  async function startCastTo(deviceId: string) {
    const cast = useCastStore()
    void nowPlaying.end()
    if (deviceId.startsWith('client:')) {
      cast.engagedDeviceId = deviceId
      await qs.selectTarget(deviceId)
      const idx = qs.currentWindowIndex
      const remote = idx >= 0 ? queue.value[idx] : undefined
      currentTrack.value = remote ?? null
      position.value = qs.positionSeconds
      playing.value = qs.playing
      if (remote?.duration) duration.value = remote.duration
      await disposeNativeAudio()
      if (engineWired.value) ensureEngine().pause()
      playbackBackend.value = 'cast'
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
    preloadingTrackId = null
    pendingNext = null
    await disposeNativeAudio()
    if (engineWired.value) ensureEngine().pause()
    playbackBackend.value = 'cast'
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
    playbackBackend.value = 'browser'
    position.value = pos
    lastTickTime = pos
    if (track && track.id > 0 && !track.isStream) {
      localHandoff = { trackId: track.id, position: pos }
    }
    // The slider was mirroring the device volume — restore the local pref.
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
    let next = peekNextTrack() // queue order; returns current for repeat-one
    if (!next && await ensureSimilarAutoplayQueue(true)) next = peekNextTrack()
    if (!next) {
      alog('player', 'cast queue ended')
      playing.value = false
      return
    }
    alog('player', `cast advance → "${next.title}"`)
    await playQueueSuccessor(next, 'ended')
  }

  return {
    playing, currentTrack, position, duration, volume, muted,
    shuffled, repeatMode, queue, originalOrder, queueOpen, sideTab, localMode,
    engineWired, playbackBackend, nativeAudioState, nativeAudioCapabilities, nativeAudioVisualizer,
    nativeAudioOutputDevices, nativeAudioOutputDeviceId, nativeAudioFollowsSystemDefault,
    scrobbledTrackId, sleepAtTrackEnd, sleepDeadline, sleepNowTick,
    similarAutoplayEnabled, similarAutoplayLoading, similarAutoplayAvailable,
    djMode, djChanging, djAvailable,
    currentIndex, playedTracks, upcomingTracks, upcomingCount,
    nextUp, progress, hasPrevious, hasNext,
    play, pause, togglePlay, seek, setVolume, toggleMute, stop,
    playContext, playTracks, playLocal,
    toggleShuffle, cycleRepeat, nextTrack, prevTrack,
    toggleLoved, toggleQueue, toggleLyrics, formatTime,
    jumpTo, removeFromQueue, moveInQueue, clearUpcoming,
    addToQueue, playNext, setSimilarAutoplayEnabled, setDJMode,
    probeNativeAudio, refreshNativeAudioOutputs, setNativeAudioOutputDevice,
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
    setSimilarAutoplayEnabled: store.setSimilarAutoplayEnabled,
    setDJMode: store.setDJMode,
    probeNativeAudio: store.probeNativeAudio,
    refreshNativeAudioOutputs: store.refreshNativeAudioOutputs,
    setNativeAudioOutputDevice: store.setNativeAudioOutputDevice,
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
  // Native/browser backends expose slightly different optional DSP blocks.
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
