<script setup lang="ts">
import AkariSub from 'akarisub'
import { DropdownMenuItem, DropdownMenuSeparator } from 'reka-ui'
import type { StreamAudio, StreamSubtitle, QualityOption } from '~~/shared/types'
import type { CastStateEvent } from '~/composables/useCast'
import { useQuery } from '@pinia/colada'
import { playbackPreferenceQuery } from '~/queries/playback'
import { continueWatchingQuery } from '~/queries/activity'

const props = defineProps<{
  fileId: string | number
  mediaItemId: number | null
  title?: string
  startTime?: number
  // entity_type / entity_id — tells the now-playing session which kind of
  // thing we're playing so the activity panel can format "S01E03 · Episode"
  // for TV instead of just the series title. Defaults to "movie" + the
  // mediaItemId when empty.
  entityType?: string
  entityId?: number
}>()
const emit = defineEmits<{ close: [] }>()
const entityPreferenceQuery = useQuery(() => ({
  ...playbackPreferenceQuery(props.mediaItemId ?? 0),
  enabled: !!props.mediaItemId,
}))
const continueQuery = useQuery(continueWatchingQuery())

const { token } = useAuth()
const { toast } = useToast()
const videoEl = ref<HTMLVideoElement>()
const { state: localState, controls: localControls, loadSource, destroyHLS } = useHeyaPlayer(videoEl)
const cast = useCastStore()
const musicPlayer = usePlayerStore()
const showCastMenu = ref(false)
const videoCastPending = ref(false)
const videoCastSessionID = ref<string | null>(null)
const videoCastStopping = ref(false)
const remoteEnded = ref(false)
const lastRemotePosition = ref(0)
const remoteSeekTick = ref(0)
const castClockTick = ref(0)
const deliberatelyStoppedCastSessions = new Set<string>()

const castDevices = computed(() => cast.devices.filter(d => d.capabilities?.includes('video')))
const videoCastSession = computed(() => {
  const s = cast.session
  if (!s || s.media_kind !== 'video') return null
  if (videoCastSessionID.value && s.id === videoCastSessionID.value) return s
  const sameEntity = s.entity_type === (props.entityType || 'movie')
    && s.entity_id === (props.entityId || props.mediaItemId || 0)
  return sameEntity ? s : null
})
const videoCastActive = computed(() => !!videoCastSession.value || videoCastPending.value)
const videoCastMode = computed(() => videoCastActive.value || remoteEnded.value)
const castConnecting = computed(() => videoCastPending.value || cast.connecting || videoCastSession.value?.state === 'starting')

// The local <video> stays mounted and paused during a cast. This proxy keeps
// the rest of the mature player UI (seek bar, keyboard controls, progress
// heartbeat, Up Next) reading one state surface while the active clock and
// transport come from Chromecast.
const state = new Proxy(localState, {
  get(target, key, receiver) {
    if (!videoCastMode.value) return Reflect.get(target, key, receiver)
    const session = videoCastSession.value
    switch (key) {
      case 'playing': return !!session && session.state === 'playing'
      case 'paused': return remoteEnded.value || session?.state === 'paused'
      case 'ended': return remoteEnded.value
      case 'loading': return false
      case 'buffering': return castConnecting.value
      case 'currentTime': castClockTick.value; return remoteEnded.value ? (session?.duration_sec ?? localState.duration) : (session ? cast.livePositionSec() : lastRemotePosition.value)
      case 'duration': return session?.duration_sec ?? localState.duration
      case 'buffered': return session ? cast.livePositionSec() : lastRemotePosition.value
      case 'volume': return (session?.volume ?? Math.round(localState.volume * 100)) / 100
      case 'muted': return (session?.volume ?? 1) === 0
      case 'seekTick': return remoteSeekTick.value
      default: return Reflect.get(target, key, receiver)
    }
  },
}) as typeof localState

let remoteVolumeBeforeMute = 30
const controls = {
  play() {
    if (videoCastActive.value) { void cast.resume().catch(() => {}); return }
    localControls.play()
  },
  pause() {
    if (videoCastActive.value) { void cast.pause().catch(() => {}); return }
    localControls.pause()
  },
  togglePlay() {
    if (videoCastActive.value) {
      if (videoCastSession.value?.state === 'paused') void cast.resume().catch(() => {})
      else void cast.pause().catch(() => {})
      return
    }
    localControls.togglePlay()
  },
  seek(time: number) {
    if (videoCastActive.value) {
      lastRemotePosition.value = time
      remoteSeekTick.value++
      void cast.seekTo(time).catch(() => {})
      return
    }
    localControls.seek(time)
  },
  skip(seconds: number) {
    if (videoCastActive.value) { controls.seek(state.currentTime + seconds); return }
    localControls.skip(seconds)
  },
  setVolume(value: number) {
    if (videoCastActive.value) {
      const level = Math.max(0, Math.min(100, Math.round(value * 100)))
      if (level > 0) remoteVolumeBeforeMute = level
      cast.setVolume(level)
      return
    }
    localControls.setVolume(value)
  },
  toggleMute() {
    if (videoCastActive.value) {
      const level = videoCastSession.value?.volume ?? 0
      if (level > 0) { remoteVolumeBeforeMute = level; cast.setVolume(0) }
      else cast.setVolume(remoteVolumeBeforeMute)
      return
    }
    localControls.toggleMute()
  },
  toggleFullscreen: localControls.toggleFullscreen,
}
// Touch devices: rotate to landscape → immersive fullscreen, back to portrait
// → exit. No-op on desktop / where the browser blocks it (see composable).
useOrientationFullscreen()
const fileIdRef = computed(() => props.fileId)
const mediaItemIdRef = computed(() => props.mediaItemId)
// entityType / entityId for the watch-progress payload — passing these
// means TV progress lands as ('episode', episode_id) instead of being
// mis-attributed to ('movie', series_media_item_id), which was breaking
// the CW row's episode-detail rendering and the Resume label detection.
const entityTypeRef = computed(() => props.entityType || 'movie')
const entityIdRef = computed(() => props.entityId || props.mediaItemId || 0)
const { state: streamState, loadStreamInfo, subtitleUrl, emitProgress } = useVideoPlayer(fileIdRef, mediaItemIdRef, entityTypeRef, entityIdRef)
const { settings, load: loadSettings, playbackForLibrary } = useUserSettings()
const { loaded: hasTrickplay, load: loadTrickplay, getThumbnail } = useTrickplay(fileIdRef)
const { load: loadSegments, segmentAt } = useMediaSegments(fileIdRef)

// Skip-segment button (intro/recap/credits markers). Dismissal is
// per-segment so skipping the intro doesn't suppress the credits button,
// and clearing on playback start resets it per file.
const dismissedSegments = ref(new Set<number>())
const skipSegmentLabels: Record<string, string> = {
  intro: 'Skip Intro',
  recap: 'Skip Recap',
  credits: 'Skip Credits',
  preview: 'Skip Preview',
  commercial: 'Skip Ad',
}
const activeSkipSegment = computed(() => {
  const seg = segmentAt(state.currentTime)
  if (!seg || dismissedSegments.value.has(seg.start_ms)) return null
  return seg
})
function skipSegment() {
  const seg = activeSkipSegment.value
  if (!seg) return
  dismissedSegments.value = new Set(dismissedSegments.value).add(seg.start_ms)
  controls.seek(seg.end_ms / 1000)
  // Flash the control bar so the time jump is visible feedback — a skip
  // that lands in similar-looking footage otherwise reads as a no-op.
  showCtrl()
}

const controlsVisible = ref(true)
const showInfoPanel = ref(false)
// 'compact' = essentials only (Decision + Playback + Network).
// 'detailed' = full diagnostics including transcoder telemetry.
const panelMode = ref<'compact' | 'detailed'>('compact')
const showSubMenu = ref(false)
const showAudioMenu = ref(false)
const showQualityMenu = ref(false)
const seekHover = ref<number | null>(null)
const activeSubIdx = ref(-1)
const activeAudioIdx = ref(0)
const activeQuality = ref('auto')
// True while focus sits on any control inside .ctrl — keeps the bar visible
// past the normal 3s auto-hide so a keyboard user's focus never disappears.
const controlsFocused = ref(false)
// Single source of truth for "is the control bar showing" — drives both the
// .ctrl visible class and the VTT overlay's nudge-up so subs clear the bar.
const ctrlShown = computed(() => controlsVisible.value || state.paused || state.buffering || controlsFocused.value)
const resumeCardRef = ref<HTMLElement>()
let assRenderer: AkariSub | null = null
// VTT path state — non-ASS tracks are served as WebVTT by the backend and
// rendered through a hidden <track> + custom overlay (see initVTT).
let vttTrackEl: HTMLTrackElement | null = null
let vttTextTrack: TextTrack | null = null
let vttCueChangeHandler: (() => void) | null = null
const vttCueLines = ref<string[]>([])
let hideTimer: ReturnType<typeof setTimeout> | null = null
let sessionId = Math.random().toString(36).slice(2, 10)

interface UpNextData {
  has_next: boolean
  episode_id?: number
  episode_number?: number
  episode_title?: string
  season_number?: number
  media_item_id?: number
  file_id?: number
  file_public_id?: string
  runtime?: number
}
const upNext = ref<UpNextData | null>(null)
const upNextCountdown = ref(-1)
let countdownTimer: ReturnType<typeof setInterval> | null = null

const knownDuration = computed(() => streamState.streamInfo?.duration || state.duration)
const progress = computed(() => knownDuration.value > 0 ? (state.currentTime / knownDuration.value) * 100 : 0)
const bufferProgress = computed(() => knownDuration.value > 0 ? (state.buffered / knownDuration.value) * 100 : 0)
const audioTracks = computed<StreamAudio[]>(() => streamState.streamInfo?.audio || [])
const subtitleTracks = computed<StreamSubtitle[]>(() => streamState.streamInfo?.subtitle || [])
const availableQualities = computed<QualityOption[]>(() => streamState.streamInfo?.qualities || [])
const usingHLS = ref(false)

// Poll the transcoder status endpoint while the diagnostics panel is open.
// Polling stops automatically when the panel is hidden.
const { status: transcodeStatus } = useTranscodeStatus(
  fileIdRef,
  computed(() => showInfoPanel.value && usingHLS.value),
  token,
)

const qualityLabel = computed(() => {
  if (activeQuality.value === 'auto') return 'Auto'
  return activeQuality.value
})

const hoverThumbnail = computed(() => {
  if (seekHover.value === null || !hasTrickplay.value) return null
  return getThumbnail(seekHover.value)
})

function buildHLSUrl() {
  const caps = useClientCaps()
  const params = new URLSearchParams({ token: token.value!, sid: sessionId })
  for (const [k, v] of Object.entries(caps)) { if (v) params.set(k, '1') }
  if (activeAudioIdx.value > 0) params.set('audio', String(activeAudioIdx.value))
  if (activeQuality.value !== 'auto') params.set('quality', activeQuality.value)
  const originalAction = streamState.streamInfo?.playback?.action
  if (originalAction === 'direct_play' && activeAudioIdx.value > 0) {
    params.set('remux', '1')
  }
  return `/api/stream/${props.fileId}/hls/master.m3u8?${params}`
}

function castDeviceSub(device: { manufacturer?: string, model?: string, provider: string }) {
  const model = [device.manufacturer, device.model].filter(Boolean).join(' ')
  return model ? `${device.provider} · ${model}` : device.provider
}

async function pickVideoCastDevice(deviceID: string) {
  showCastMenu.value = false
  if (videoCastSession.value?.device_id === deviceID) {
    await stopVideoCast(true)
    return
  }
  const position = state.currentTime
  const wasPlaying = state.playing
  try {
    if (cast.session) await cast.stopSession()
    cast.engagedDeviceId = deviceID
    videoCastPending.value = true
    remoteEnded.value = false
    lastRemotePosition.value = position
    localControls.pause()
    // A music session may have owned this Cast store immediately before the
    // video handoff. Keep its global playbar from presenting stale playback.
    musicPlayer.playing = false
    const snap = await cast.playVideo({
      fileId: props.fileId,
      entityType: (props.entityType === 'episode' ? 'episode' : 'movie'),
      entityId: props.entityId || props.mediaItemId || 0,
      title: props.title,
      audioTrack: activeAudioIdx.value,
      quality: activeQuality.value,
      fallbackVolume: localState.volume * 100,
      startSeconds: position,
    })
    videoCastSessionID.value = snap.id
    lastRemotePosition.value = snap.position_sec
  } catch (error) {
    cast.engagedDeviceId = null
    videoCastSessionID.value = null
    if (wasPlaying) localControls.play()
    toast.err(error instanceof Error ? error.message : 'Could not cast this video')
  } finally {
    videoCastPending.value = false
  }
}

async function restartVideoCast() {
  const session = videoCastSession.value
  if (!session || !cast.engagedDeviceId) return
  const position = state.currentTime
  videoCastPending.value = true
  try {
    const snap = await cast.playVideo({
      fileId: props.fileId,
      entityType: (props.entityType === 'episode' ? 'episode' : 'movie'),
      entityId: props.entityId || props.mediaItemId || 0,
      title: props.title,
      audioTrack: activeAudioIdx.value,
      quality: activeQuality.value,
      fallbackVolume: session.volume,
      startSeconds: position,
    })
    videoCastSessionID.value = snap.id
    lastRemotePosition.value = snap.position_sec
  } catch (error) {
    toast.err(error instanceof Error ? error.message : 'Could not update Chromecast playback')
  } finally {
    videoCastPending.value = false
  }
}

async function stopVideoCast(resumeLocal: boolean) {
  const session = videoCastSession.value
  if (!session || videoCastStopping.value) return
  videoCastStopping.value = true
  const position = cast.livePositionSec()
  const wasPlaying = session.state === 'playing' || session.state === 'starting'
  deliberatelyStoppedCastSessions.add(session.id)
  lastRemotePosition.value = position
  try {
    await cast.disconnect()
  } finally {
    videoCastSessionID.value = null
    videoCastPending.value = false
    remoteEnded.value = false
    videoCastStopping.value = false
    const video = videoEl.value
    if (video && Number.isFinite(position)) {
      video.currentTime = Math.max(0, Math.min(knownDuration.value || position, position))
    }
    if (resumeLocal && wasPlaying) localControls.play()
  }
}

watch(showCastMenu, (open) => {
  if (open) void cast.refreshDevices()
})

function autoSelectAudio(prefs: ReturnType<typeof playbackForLibrary>) {
  if (!prefs.default_audio_language || !audioTracks.value.length) return
  const lang = prefs.default_audio_language
  const idx = audioTracks.value.findIndex(a => langMatches(a.language ?? '', lang))
  if (idx >= 0) activeAudioIdx.value = idx
}

function isSignsOrSongs(s: StreamSubtitle): boolean {
  const t = (s.title || '').toLowerCase()
  return s.is_forced || /\b(sign|song|s&s|forced|commentary)\b/i.test(t)
}

// ISO 639-1 ↔ 639-2 alias groups. Subtitle/audio tracks usually use 3-letter
// codes (e.g. "eng", "jpn") while browser locales use 2-letter ("en", "ja").
const LANG_ALIASES: string[][] = [
  ['en', 'eng'], ['ja', 'jpn', 'jap'], ['da', 'dan'], ['de', 'ger', 'deu'],
  ['fr', 'fre', 'fra'], ['es', 'spa'], ['zh', 'chi', 'zho'], ['ko', 'kor'],
  ['ru', 'rus'], ['it', 'ita'], ['pt', 'por'], ['nl', 'dut', 'nld'],
  ['pl', 'pol'], ['sv', 'swe'], ['no', 'nor'], ['fi', 'fin'],
  ['ar', 'ara'], ['he', 'heb', 'iw'], ['hi', 'hin'], ['th', 'tha'],
  ['vi', 'vie'], ['tr', 'tur'], ['cs', 'cze', 'ces'], ['hu', 'hun'],
  ['ro', 'rum', 'ron'], ['el', 'gre', 'ell'], ['uk', 'ukr'],
  ['id', 'ind'], ['ms', 'may', 'msa'],
]

function normLang(s: string | null | undefined): string {
  return (s || '').toLowerCase().split(/[-_]/)[0] ?? ''
}

function langMatches(a: string, b: string): boolean {
  if (!a || !b) return false
  const na = normLang(a)
  const nb = normLang(b)
  if (na === nb) return true
  for (const group of LANG_ALIASES) {
    if (group.includes(na) && group.includes(nb)) return true
  }
  return false
}

function browserLanguages(): string[] {
  if (typeof navigator === 'undefined') return []
  const raw = navigator.languages?.length ? navigator.languages : (navigator.language ? [navigator.language] : [])
  const out: string[] = []
  for (const l of raw) {
    const n = normLang(l)
    if (n && !out.includes(n)) out.push(n)
  }
  return out
}

function autoSelectSubtitle(prefs: ReturnType<typeof playbackForLibrary>) {
  const mode = prefs.subtitle_mode
  if (mode === 'off') { activeSubIdx.value = -1; return }

  const subs = subtitleTracks.value
  if (!subs.length) { activeSubIdx.value = -1; return }

  if (mode === 'forced_only') {
    const forced = subs.findIndex(s => s.is_forced)
    activeSubIdx.value = forced >= 0 ? forced : -1
    return
  }

  const indexed = subs.map((s, i) => ({ s, i }))
  const priority = prefs.subtitle_priority || []

  function pickBest(pool: { s: StreamSubtitle; i: number }[]): number {
    if (!pool.length) return -1
    const dialogue = pool.filter(({ s }) => !isSignsOrSongs(s))
    const candidates = dialogue.length > 0 ? dialogue : pool
    for (const codec of priority) {
      const found = candidates.find(({ s }) => s.codec?.toLowerCase() === codec.toLowerCase())
      if (found) return found.i
    }
    const defIdx = candidates.find(({ s }) => s.is_default)
    if (defIdx) return defIdx.i
    return candidates[0]!.i
  }

  if (mode === 'always') {
    activeSubIdx.value = pickBest(indexed)
    return
  }

  // mode === 'auto':
  // Build a language cascade the user understands:
  //   1. Preferred sub language (if set)
  //   2. Browser languages (in order)
  //   3. English fallback
  const cascade: string[] = []
  const push = (l: string) => { const n = normLang(l); if (n && !cascade.includes(n)) cascade.push(n) }
  if (prefs.default_subtitle_language) push(prefs.default_subtitle_language)
  for (const l of browserLanguages()) push(l)
  push('en')

  const audioLang = audioTracks.value[activeAudioIdx.value]?.language ?? ''
  // If the playing audio is in a language the user understands → no subs.
  if (audioLang && cascade.some(l => langMatches(audioLang, l))) {
    activeSubIdx.value = -1
    return
  }

  // Audio is foreign — cascade through user languages.
  for (const lang of cascade) {
    const matching = indexed.filter(({ s }) => langMatches(s.language ?? '', lang))
    if (matching.length) {
      activeSubIdx.value = pickBest(matching)
      return
    }
  }

  // No language match — show the best available subtitle anyway.
  activeSubIdx.value = pickBest(indexed)
}

async function init() {
  const entityPrefPromise = props.mediaItemId
    ? waitForQuery(entityPreferenceQuery).then(() => entityPreferenceQuery.data.value ?? null).catch(() => null)
    : Promise.resolve(null)

  await Promise.all([loadStreamInfo(), loadSettings(), entityPrefPromise])
  const entityPref = await entityPrefPromise

  const libId = streamState.streamInfo?.library_id
  const prefs = playbackForLibrary(libId)

  if (entityPref?.audio_language) prefs.default_audio_language = entityPref.audio_language
  if (entityPref?.subtitle_language) prefs.default_subtitle_language = entityPref.subtitle_language
  if (entityPref?.subtitle_mode) prefs.subtitle_mode = entityPref.subtitle_mode as typeof prefs.subtitle_mode

  const serverAction = streamState.streamInfo?.playback?.action
  if (prefs.default_quality && prefs.default_quality !== 'auto' && serverAction !== 'direct_play') {
    const avail = availableQualities.value
    if (avail.some(q => q.label === prefs.default_quality)) {
      activeQuality.value = prefs.default_quality
    }
  }

  autoSelectAudio(prefs)
  autoSelectSubtitle(prefs)

  if (activeSubIdx.value >= 0) {
    const sub = subtitleTracks.value[activeSubIdx.value]
    // Warm the subtitle endpoint for whatever we auto-selected — the server
    // extracts ASS / converts everything else to WebVTT on first hit, so
    // this prefetch means the renderer isn't blocked on that work later.
    if (sub) fetch(subtitleUrl(sub.index)).catch(() => {})
  }

  // Before loading the source (which auto-plays on canplay), check whether
  // we have saved progress and need to ask the user. We block here until
  // the user picks — that way no frame of video plays under the modal.
  // The user's pick decides what startTime we honor when we finally load.
  await checkResume()

  startPlayback()
}

// Kicks off the actual source load + autoplay. Called after the resume
// decision is finalized (either no resume needed, or the user picked).
function startPlayback() {
  const { $heya } = useNuxtApp()
  const action = streamState.streamInfo?.playback?.action
  const needsNonDefaultAudio = activeAudioIdx.value > 0
  if (action === 'direct_play' && !needsNonDefaultAudio) {
    usingHLS.value = false
    loadSource(`/api/stream/${props.fileId}?token=${token.value}`, token.value!)
  } else {
    usingHLS.value = true
    loadSource(buildHLSUrl(), token.value!)
  }

  // Seek to whatever startTime is set — that's either the URL ?t= override
  // OR the resume position the user picked OR 0 (start over / no resume).
  // Listener is one-shot; install AFTER loadSource so canplay is fresh.
  const target = pendingSeekTo.value
  if (target > 0) {
    const v = videoEl.value
    if (v) {
      const onReady = () => {
        v.currentTime = target
        v.removeEventListener('canplay', onReady)
      }
      v.addEventListener('canplay', onReady)
    }
  }

  if (activeSubIdx.value >= 0) awaitVideoReady().then(() => initSubtitles())

  loadTrickplay(token.value!).catch(() => {})
  dismissedSegments.value = new Set()
  loadSegments().catch(() => {})

  if (props.mediaItemId) {
    // /api/media/{id} accepts slug or numeric ID — spec types id as string.
    $heya('/api/media/{id}/up-next', { path: { id: props.mediaItemId } })
      .then(data => {
        const ud = data as UpNextData
        if (ud?.has_next && (ud.file_public_id || ud.file_id)) upNext.value = ud
      })
      .catch(() => {})
  }
}

function awaitVideoReady(): Promise<void> {
  return new Promise((resolve) => {
    const v = videoEl.value
    if (!v) { resolve(); return }
    if (v.videoWidth > 0) { resolve(); return }
    const check = () => {
      if (!v || v.videoWidth > 0) {
        v?.removeEventListener('loadedmetadata', check)
        v?.removeEventListener('canplay', check)
        resolve()
      }
    }
    v.addEventListener('loadedmetadata', check)
    v.addEventListener('canplay', check)
  })
}

function destroyASS() { if (assRenderer) { assRenderer.destroy(); assRenderer = null } }

// Tears down whichever renderer is live — AkariSub canvas AND/OR the VTT
// <track>/overlay. Safe to call when neither exists. Every subtitle switch
// (ASS ⇄ VTT ⇄ off) funnels through this so renderers never stack.
function destroySubtitles() {
  destroyASS()
  destroyVTT()
}

// Dispatches by codec: ASS/SSA → AkariSub (full styling/positioning needs
// libass), everything else → the backend has already converted it to WebVTT
// at /subtitles/{index}, so a native <track> + custom overlay renders it.
function initSubtitles() {
  destroySubtitles()
  if (import.meta.server) return
  if (activeSubIdx.value < 0 || !videoEl.value) return
  const sub = subtitleTracks.value[activeSubIdx.value]
  if (!sub) return
  if (sub.codec === 'ass' || sub.codec === 'ssa') initASS(sub)
  else initVTT(sub)
}

function initASS(sub: StreamSubtitle) {
  if (!videoEl.value) return
  try {
    assRenderer = new AkariSub({
      video: videoEl.value,
      subUrl: subtitleUrl(sub.index),
      workerUrl: '/akarisub/akarisub-worker.js',
      wasmUrl: '/akarisub/akarisub-worker.wasm',
      availableFonts: { 'liberation sans': '/akarisub/default.woff2' },
      timeOffset: 0,
    })
    assRenderer.addEventListener('error', (e: any) => {
      console.warn('AkariSub render error:', e?.error?.message || e)
      destroyASS()
    })
  } catch (e) {
    console.warn('AkariSub init failed:', e)
    assRenderer = null
  }
}

// WebVTT cue text can carry inline tags (<i>, <b>, <c.class>, <v Speaker>,
// karaoke timestamps). We render plain styled text, so strip the tags and
// decode the few entities VTT requires escaping — Vue's {{ }} would show
// them literally otherwise. Order matters: strip tags before decoding so
// an encoded &lt; can never conjure a tag.
function stripVttTags(text: string): string {
  return text
    .replace(/<[^>]*>/g, '')
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&nbsp;/g, ' ')
    .replace(/&lrm;|&rlm;/g, '')
}

function initVTT(sub: StreamSubtitle) {
  const v = videoEl.value
  if (!v) return
  const trackEl = document.createElement('track')
  trackEl.kind = 'subtitles'
  trackEl.srclang = normLang(sub.language) || 'en'
  trackEl.label = sub.title || sub.language || `Track ${sub.index}`
  trackEl.src = subtitleUrl(sub.index)
  v.appendChild(trackEl)
  vttTrackEl = trackEl

  // 'hidden' = browser parses + times the cues (activeCues stays live and
  // cuechange fires) but paints nothing — we render into our own overlay so
  // the subs match app styling and can dodge the control bar, instead of
  // the UA's ::cue box painting underneath/over the OSD.
  const tt = trackEl.track
  tt.mode = 'hidden'
  vttTextTrack = tt

  const handler = () => {
    // Guard: a queued cuechange (or late track 'load') can land after
    // teardown / after switching to a different track — ignore it.
    if (vttTextTrack !== tt) return
    const cues = tt.activeCues
    const lines: string[] = []
    if (cues) {
      for (let i = 0; i < cues.length; i++) {
        const cue = cues[i] as VTTCue | undefined
        if (!cue || typeof cue.text !== 'string') continue
        for (const line of stripVttTags(cue.text).split('\n')) {
          if (line.trim()) lines.push(line)
        }
      }
    }
    vttCueLines.value = lines
  }
  vttCueChangeHandler = handler
  tt.addEventListener('cuechange', handler)
  // Cues load async; if playback is mid-cue when the track finishes loading
  // (e.g. sub switched while paused), no cuechange fires until the next cue
  // boundary — seed from activeCues once the resource is in.
  trackEl.addEventListener('load', handler)
  handler()
}

function destroyVTT() {
  if (vttTextTrack) {
    if (vttCueChangeHandler) vttTextTrack.removeEventListener('cuechange', vttCueChangeHandler)
    vttTextTrack.mode = 'disabled'
  }
  vttTextTrack = null
  vttCueChangeHandler = null
  vttTrackEl?.remove()
  vttTrackEl = null
  vttCueLines.value = []
}

function selectSub(idx: number) {
  activeSubIdx.value = idx
  showSubMenu.value = false
  awaitVideoReady().then(() => initSubtitles())
}
function disableSubs() { activeSubIdx.value = -1; showSubMenu.value = false; destroySubtitles() }
function selectAudio(idx: number) {
  if (idx === activeAudioIdx.value) { showAudioMenu.value = false; return }
  const currentTime = state.currentTime
  activeAudioIdx.value = idx
  sessionId = Math.random().toString(36).slice(2, 10)
  showAudioMenu.value = false
  if (videoCastActive.value) {
    void restartVideoCast()
    return
  }
  const canDirectPlay = streamState.streamInfo?.playback?.action === 'direct_play' && idx === 0
  const url = canDirectPlay
    ? `/api/stream/${props.fileId}?token=${token.value}`
    : buildHLSUrl()
  usingHLS.value = !canDirectPlay
  loadSource(url, token.value!)
  const v = videoEl.value
  if (v) {
    const onReady = () => { v.currentTime = currentTime; v.removeEventListener('canplay', onReady) }
    v.addEventListener('canplay', onReady)
  }
}
function selectQuality(quality: string) {
  if (quality === activeQuality.value) { showQualityMenu.value = false; return }
  const currentTime = state.currentTime
  activeQuality.value = quality
  sessionId = Math.random().toString(36).slice(2, 10)
  showQualityMenu.value = false
  if (videoCastActive.value) {
    void restartVideoCast()
    return
  }
  usingHLS.value = true
  loadSource(buildHLSUrl(), token.value!)
  const v = videoEl.value
  if (v) {
    const onReady = () => { v.currentTime = currentTime; v.removeEventListener('canplay', onReady) }
    v.addEventListener('canplay', onReady)
  }
}

function closeMenus() { showSubMenu.value = false; showAudioMenu.value = false; showQualityMenu.value = false; showCastMenu.value = false }

// Mutually-exclusive menu opens — opening any one closes the other two.
// Reka's own dismissable-layer already handles click-outside cleanup in a
// real browser, but explicit watchers are safer (and let keyboard-driven
// opens via Enter close the previous menu too).
watch(showAudioMenu, (v) => { if (v) { showSubMenu.value = false; showQualityMenu.value = false; showCastMenu.value = false } })
watch(showSubMenu, (v) => { if (v) { showAudioMenu.value = false; showQualityMenu.value = false; showCastMenu.value = false } })
watch(showQualityMenu, (v) => { if (v) { showAudioMenu.value = false; showSubMenu.value = false; showCastMenu.value = false } })
watch(showCastMenu, (v) => { if (v) { showAudioMenu.value = false; showSubMenu.value = false; showQualityMenu.value = false } })
function audioLabel(a: StreamAudio) {
  const p: string[] = []
  if (a.language) p.push(a.language.toUpperCase())
  if (a.title) p.push(a.title)
  if (!a.language && !a.title) p.push(`Track ${a.index}`)
  p.push(a.codec.toUpperCase())
  if (a.channel_layout) p.push(a.channel_layout)
  else if (a.channels === 6) p.push('5.1'); else if (a.channels === 8) p.push('7.1'); else if (a.channels === 2) p.push('Stereo')
  return p.join(' · ')
}

function seek(e: MouseEvent) {
  if (!knownDuration.value) return
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  controls.seek(Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)) * knownDuration.value)
}
function onSeekHover(e: MouseEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  seekHover.value = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)) * knownDuration.value
}
function setVolume(e: MouseEvent) {
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  controls.setVolume(Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width)))
}

// Keyboard equivalents for the seek/volume sliders, mirroring the pointer
// math above. stopPropagation() keeps the window-level shortcut handler
// (ArrowLeft/Right = skip ±10s) from double-acting on the same keystroke.
function onSeekKeydown(e: KeyboardEvent) {
  if (!knownDuration.value) return
  switch (e.key) {
    case 'ArrowLeft': controls.seek(Math.max(0, state.currentTime - 5)); break
    case 'ArrowRight': controls.seek(Math.min(knownDuration.value, state.currentTime + 5)); break
    case 'Home': controls.seek(0); break
    case 'End': controls.seek(knownDuration.value); break
    default: return
  }
  e.preventDefault(); e.stopPropagation(); showCtrl()
}

function onVolumeKeydown(e: KeyboardEvent) {
  switch (e.key) {
    case 'ArrowLeft': controls.setVolume(Math.max(0, state.volume - 0.05)); break
    case 'ArrowRight': controls.setVolume(Math.min(1, state.volume + 0.05)); break
    case 'Home': controls.setVolume(0); break
    case 'End': controls.setVolume(1); break
    default: return
  }
  e.preventDefault(); e.stopPropagation(); showCtrl()
}

function playNextEpisode() {
  const fileRef = upNext.value?.file_public_id || upNext.value?.file_id
  if (!fileRef || !upNext.value?.media_item_id) return
  cancelUpNext()
  destroyHLS(); destroySubtitles()
  const label = `S${String(upNext.value.season_number).padStart(2, '0')}E${String(upNext.value.episode_number).padStart(2, '0')}`
  const params = new URLSearchParams({
    media_item_id: String(upNext.value.media_item_id),
    title: upNext.value.episode_title ? `${label} - ${upNext.value.episode_title}` : label,
  })
  // Key the next episode's progress on the episode, not the series, so it
  // tracks + clears from Continue Watching independently.
  if (upNext.value.episode_id) {
    params.set('entity_type', 'episode')
    params.set('entity_id', String(upNext.value.episode_id))
  }
  navigateTo(`/watch/${fileRef}?${params}`)
}

function startUpNextCountdown() {
  upNextCountdown.value = 10
  countdownTimer = setInterval(() => {
    upNextCountdown.value--
    if (upNextCountdown.value <= 0) {
      playNextEpisode()
    }
  }, 1000)
}

function cancelUpNext() {
  if (countdownTimer) { clearInterval(countdownTimer); countdownTimer = null }
  upNextCountdown.value = -1
}

watch(() => state.ended, (ended) => {
  if (ended) {
    // Emit one last progress with completed=true. After this the player
    // either rolls into Up Next (TV) or sits at the end (movies).
    emitProgress(state.currentTime, knownDuration.value, true)
    if (upNext.value?.has_next && (upNext.value.file_public_id || upNext.value.file_id)) {
      startUpNextCountdown()
    }
  }
})

// --- Watch progress reporting ---
// Runs only while the video is actively playing (5s cadence). On pause the
// interval clears AND we emit once to capture the pause position. On seek
// (state.seekTick increments) we emit immediately so the resume point is
// accurate even mid-seek-spree.
const PROGRESS_INTERVAL_MS = 5000
let progressTimer: ReturnType<typeof setInterval> | null = null

function fireProgress(completed = false) {
  emitProgress(state.currentTime, knownDuration.value, completed || undefined)
}

watch(() => state.playing, (playing) => {
  if (progressTimer) { clearInterval(progressTimer); progressTimer = null }
  if (playing) {
    progressTimer = setInterval(() => fireProgress(), PROGRESS_INTERVAL_MS)
  } else {
    // Pausing — capture position once. Skipped during the initial render
    // pass since paused=true is the default and emitProgress bails when
    // currentTime < 1.
    fireProgress()
  }
})

// Seek emits an immediate update — the user's saved resume position should
// reflect where they actually are, not where the next 5s tick lands.
watch(() => state.seekTick, () => { fireProgress() })

// --- Now-Playing presence ---
// Heartbeats the session every 10s so the activity panel can show what
// everyone's watching. Server prunes after 30s of silence — handles
// closed-tab cases the beforeunload hook can't catch (navigator throttles
// it). Each beat carries the live position + transcode info the FE got
// from the stream-info response.
const nowPlaying = useNowPlayingSession()

const currentSessionPayload = computed(() => {
  // Pull transcode info from the streamInfo response — playback.action +
  // first video/audio track + container/dimensions. Server-supplied so the
  // FE can't lie about it (the server told us in the first place).
  const info = streamState.streamInfo
  const playback = info?.playback
  const firstVid = info?.video?.[0]
  const firstAud = info?.audio?.[0]
  return {
    fileId: props.fileId,
    mediaItemId: props.mediaItemId,
    entityType: props.entityType || 'movie',
    entityId: props.entityId || props.mediaItemId || 0,
    positionSeconds: state.currentTime,
    totalSeconds: knownDuration.value,
    paused: state.paused,
    playbackAction: playback?.action ?? '',
    videoCodec: firstVid?.codec ?? '',
    audioCodec: firstAud?.codec ?? '',
    container: info?.container ?? '',
    width: firstVid?.width ?? 0,
    height: firstVid?.height ?? 0,
    // info.bit_rate is bits/sec from the container probe; convert to kbps.
    bitrateKbps: info?.bit_rate ? Math.round(info.bit_rate / 1000) : 0,
  }
})

// Start the heartbeat loop on mount, end on unmount. The getter returns
// live payload on each 10s tick — position/paused stay fresh.
onMounted(() => {
  nowPlaying.start(() => currentSessionPayload.value)
})

// --- Remote control (Activity page) ---
// An admin, or the owner from another device, can stop this playback or push a
// short message to it. Commands ride the shared WS broadcast, so every client
// receives the frame; we act only on the one addressed to *this* player's
// session id. connect() is idempotent — it guarantees a live socket even on a
// client that only ever plays (never opens a page that subscribes).
const { on: onWsEvent, connect: connectWs } = useEventBus()
let offSessionCmd: (() => void) | null = null
let offCastState: (() => void) | null = null
onMounted(() => {
  connectWs()
  offSessionCmd = onWsEvent('session.command', (event) => {
    const p = event.payload as { session_id?: string, action?: string, message?: string, by?: string }
    if (!p || p.session_id !== nowPlaying.sessionId) return
    if (p.action === 'stop') {
      toast.info(p.by ? `Playback stopped by ${p.by}` : 'Playback stopped remotely')
      handleClose()
    } else if (p.action === 'message' && p.message) {
      toast({ message: p.by ? `${p.by}: ${p.message}` : p.message, tone: 'info', icon: 'bell', duration: 7000 })
    }
  })
  offCastState = onWsEvent('cast.state', (event) => {
    const p = event.payload as CastStateEvent
    if (!p || p.media_kind !== 'video') return
    const sameSession = !!videoCastSessionID.value && p.session_id === videoCastSessionID.value
    const sameEntity = p.entity_type === (props.entityType || 'movie')
      && p.entity_id === (props.entityId || props.mediaItemId || 0)
    if (!sameSession && !sameEntity) return
    lastRemotePosition.value = p.position_sec
    if (p.state === 'stopped') {
      if (deliberatelyStoppedCastSessions.delete(p.session_id)) return
      videoCastPending.value = false
      remoteEnded.value = true
      cast.engagedDeviceId = null
    } else if (p.state === 'failed') {
      videoCastPending.value = false
      remoteEnded.value = false
      videoCastSessionID.value = null
      cast.engagedDeviceId = null
      if (videoEl.value) videoEl.value.currentTime = p.position_sec
    }
  })
})
onUnmounted(() => { offSessionCmd?.(); offCastState?.() })

// --- In-player Resume prompt ---
// Before the source loads, check whether the user has saved progress for
// this item. If so AND no `?t=` query forces a specific seek, show an
// in-player dialog asking Resume vs Start over. checkResume() returns a
// Promise that resolves once the user has picked — init() awaits it
// before kicking off loadSource so no frame plays under the modal.
const resumeOpen = ref(false)
const resumePosition = ref(0)
// pendingSeekTo carries the target offset into startPlayback. Set to
// props.startTime as a baseline (URL `?t=`) and overridden by the user's
// resume choice when the modal flow runs.
const pendingSeekTo = ref(props.startTime ?? 0)
let resumePickResolver: (() => void) | null = null

async function checkResume(): Promise<void> {
  // Explicit ?t= override (deep links) bypasses the modal entirely.
  if (props.startTime && props.startTime > 0) {
    pendingSeekTo.value = props.startTime
    return
  }
  if (!props.mediaItemId) return

  let entry: { progress_seconds: number } | undefined
  try {
    await waitForQuery(continueQuery)
    const items = continueQuery.data.value ?? []
    const wantType = props.entityType || 'movie'
    const wantId = props.entityId || (wantType === 'movie' ? props.mediaItemId : 0)
    entry = items.find(it => it.entity_type === wantType && it.entity_id === wantId)
  } catch {
    // Can't reach the API — fall through to default-play.
  }

  if (!entry || entry.progress_seconds <= 30) {
    return
  }

  resumePosition.value = entry.progress_seconds
  resumeOpen.value = true
  // Block init until the user picks. resumePickResolver fires from
  // onResumePick which also sets pendingSeekTo to the chosen target.
  await new Promise<void>(resolve => { resumePickResolver = resolve })
}

function onResumePick(seek: boolean) {
  pendingSeekTo.value = seek ? resumePosition.value : 0
  resumeOpen.value = false
  if (resumePickResolver) { resumePickResolver(); resumePickResolver = null }
}

// Move focus into the resume dialog the moment it mounts — otherwise focus
// stays wherever it was (often <body>), and a keyboard/screen-reader user
// gets no indication a modal just appeared. The card itself is the target
// (tabindex="-1", not in tab order) so the dialog's role+label announce
// first; Tab from there reaches "Start over" / "Resume at …".
watch(resumeOpen, (open) => {
  if (open) nextTick(() => resumeCardRef.value?.focus())
})

// Immediate heartbeat on pause-state change so the activity panel reacts
// without waiting for the next 10s tick. (The 5s progress emit already
// handles position; this catches pause/resume specifically.)
watch(() => state.paused, () => {
  if (props.mediaItemId) nowPlaying.heartbeat(currentSessionPayload.value)
})

onUnmounted(() => {
  if (progressTimer) { clearInterval(progressTimer); progressTimer = null }
  fireProgress()
  nowPlaying.end()
})

function handleClose() {
  if (videoCastSession.value) void stopVideoCast(false)
  cancelUpNext()
  destroyHLS()
  destroySubtitles()
  if (document.fullscreenElement) document.exitFullscreen()
  emit('close')
}

function showCtrl() {
  controlsVisible.value = true
  if (hideTimer) clearTimeout(hideTimer)
  // Never auto-hide while a control inside .ctrl holds keyboard focus — the
  // focusin/focusout handlers below keep controlsFocused in sync.
  hideTimer = setTimeout(() => { if (state.playing && !controlsFocused.value) controlsVisible.value = false }, 3000)
}

function onControlsFocusIn() { controlsFocused.value = true; showCtrl() }
function onControlsFocusOut() { controlsFocused.value = false; showCtrl() }

let lastTap = 0, lastTapX = 0
function onVideoClick(e: MouseEvent) {
  const now = Date.now(), x = e.clientX
  if (now - lastTap < 350 && Math.abs(x - lastTapX) < 100) {
    const w = window.innerWidth
    if (x < w * 0.3) controls.skip(-10)
    else if (x > w * 0.7) controls.skip(10)
    else controls.toggleFullscreen()
    lastTap = 0; return
  }
  lastTap = now; lastTapX = x
  setTimeout(() => { if (Date.now() - lastTap >= 300) { controls.togglePlay(); showCtrl() } }, 320)
}

function handleKeydown(e: KeyboardEvent) {
  // Single-char shortcuts (k/f/m/j/l/i…) must not fire while the user is
  // typing elsewhere, and arrow keys must not fight an open track menu's
  // own keyboard navigation.
  const target = e.target as HTMLElement | null
  if (target?.matches?.('input,textarea,[contenteditable]')) return
  if (showAudioMenu.value || showSubMenu.value || showQualityMenu.value || showCastMenu.value) return
  if (resumeOpen.value && e.key === 'Escape') { onResumePick(false); e.preventDefault(); return }
  if (upNextCountdown.value > 0 && e.key === 'Escape') { cancelUpNext(); e.preventDefault(); return }
  if (upNextCountdown.value > 0 && (e.key === 'Enter' || e.key === 'n')) { playNextEpisode(); e.preventDefault(); return }
  if (showInfoPanel.value && e.key === 'Escape') { showInfoPanel.value = false; e.preventDefault(); return }
  switch (e.key) {
    case 'Escape':
      // Back out of fullscreen first — closing the whole player on the same
      // keystroke that a fullscreen user expects to just un-immerse them is
      // surprising (and doubles up with the browser's own Escape-exits-
      // fullscreen behavior).
      if (document.fullscreenElement) { document.exitFullscreen() } else { handleClose() }
      break
    case ' ': case 'k': controls.togglePlay(); break
    case 'f': controls.toggleFullscreen(); break
    case 'm': controls.toggleMute(); break
    case 'ArrowLeft': case 'j': controls.skip(-10); break
    case 'ArrowRight': case 'l': controls.skip(10); break
    case 'ArrowUp': controls.setVolume(state.volume + 0.1); break
    case 'ArrowDown': controls.setVolume(state.volume - 0.1); break
    case 'i': if (!e.ctrlKey && !e.metaKey) showInfoPanel.value = !showInfoPanel.value; break
    default: return
  }
  e.preventDefault(); showCtrl()
}

function volIcon() {
  if (state.muted || state.volume === 0) return 'speakerx'
  if (state.volume < 0.3) return 'speakernone'
  if (state.volume < 0.7) return 'speakerlow'
  return 'speakerhigh'
}

useEventListener(window, 'keydown', handleKeydown)
onMounted(init)
let castClockTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  castClockTimer = setInterval(() => {
    if (!videoCastActive.value) return
    castClockTick.value++
    lastRemotePosition.value = cast.livePositionSec()
  }, 500)
  void cast.refreshDevices()
})
onUnmounted(() => {
  if (videoCastSession.value && !videoCastStopping.value) void stopVideoCast(false)
  if (castClockTimer) clearInterval(castClockTimer)
  destroySubtitles()
  cancelUpNext()
  if (hideTimer) clearTimeout(hideTimer)
})
</script>

<template>
  <div class="p" @mousemove="showCtrl" @click="closeMenus">
    <!-- Loading / Error -->
    <div v-if="streamState.loading" class="p-center">
      <div class="spinner" aria-hidden="true" />
      <span class="sr-only" aria-live="polite">Loading video…</span>
    </div>
    <div v-else-if="state.error || streamState.error" class="p-center" role="alert">
      <Icon name="warning" :size="28" />
      <div style="margin-top: 12px">{{ state.error || streamState.error }}</div>
      <button class="btn btn-secondary" style="margin-top: 16px" @click="handleClose">Go Back</button>
    </div>

    <template v-else>
      <video ref="videoEl" :inert="resumeOpen" @click="onVideoClick" />

      <div v-if="videoCastMode" class="cast-remote-overlay" aria-live="polite">
        <Icon :name="castConnecting ? 'loading' : 'cast'" :size="34" :class="{ 'cast-remote-spin': castConnecting }" />
        <div class="cast-remote-title">{{ remoteEnded ? 'Playback finished' : `Playing on ${cast.deviceName || 'Chromecast'}` }}</div>
        <div class="cast-remote-sub">{{ props.title || 'Video' }}</div>
      </div>

      <!-- VTT subtitle overlay — custom rendering of the hidden TextTrack's
           active cues (see initVTT). Nudges up while the control bar is
           shown so cues never hide behind it. ASS/SSA tracks paint on the
           AkariSub canvas instead and never populate vttCueLines. -->
      <div v-if="vttCueLines.length" class="vtt-layer" :class="{ 'ctrl-up': ctrlShown }">
        <div class="vtt-cue">
          <span v-for="(line, i) in vttCueLines" :key="i" class="vtt-line">{{ line }}</span>
        </div>
      </div>

      <!-- In-player resume prompt — shown on mount when saved progress
           exists for this item and no ?t= override is set. Modal: dialog
           role + focus moved into the card on open (see the resumeOpen
           watcher) + the rest of the player made inert so Tab/AT can't
           reach controls hidden behind the overlay. -->
      <div v-if="resumeOpen" class="resume-overlay" role="dialog" aria-modal="true" aria-label="Resume playback">
        <div ref="resumeCardRef" class="resume-card surface" tabindex="-1">
          <div class="resume-kind">Pick up where you left off</div>
          <div class="resume-title">{{ props.title || 'Continue watching' }}</div>
          <div class="resume-progress">
            <div
              class="resume-progress-bar"
              role="progressbar"
              :aria-valuemin="0"
              :aria-valuemax="knownDuration || 0"
              :aria-valuenow="resumePosition"
              :aria-valuetext="`${formatTime(resumePosition)} of ${formatTime(knownDuration)}`"
            ><div class="resume-progress-fill" :style="{ width: knownDuration > 0 ? Math.min(100, Math.round((resumePosition / knownDuration) * 100)) + '%' : '0%' }" /></div>
            <div class="resume-progress-label mono">{{ formatTime(resumePosition) }} / {{ formatTime(knownDuration) }}</div>
          </div>
          <div class="resume-actions">
            <button class="btn btn-secondary" @click="onResumePick(false)">
              <Icon name="rewind" :size="14" /> Start over
            </button>
            <button class="btn btn-primary" @click="onResumePick(true)">
              <Icon name="play" :size="14" /> Resume at {{ formatTime(resumePosition) }}
            </button>
          </div>
        </div>
      </div>

      <!-- Controls -->
      <div
        class="ctrl"
        :class="{ visible: ctrlShown }"
        :inert="resumeOpen"
        @focusin="onControlsFocusIn"
        @focusout="onControlsFocusOut"
      >
        <!-- Top -->
        <div class="ctrl-top">
          <button class="c-btn" aria-label="Close player" @click="handleClose"><Icon name="chevleft" :size="20" /></button>
          <div class="ctrl-title">{{ title }}</div>
          <button class="c-btn" :class="{ active: showInfoPanel }" aria-label="Stream info" :aria-expanded="showInfoPanel" @click="showInfoPanel = !showInfoPanel"><Icon name="info" :size="18" /></button>
        </div>

        <!-- Center play. The buffering ring is concentric with the button
             (same flex center) so it wraps the glyph cleanly instead of
             peeking out from a separately-centered spinner. -->
        <div class="ctrl-center" @click.stop="controls.togglePlay()">
          <div class="center-play">
            <div v-if="state.buffering" class="center-ring" aria-hidden="true" />
            <button class="center-btn" :class="{ 'is-play': state.paused }" :aria-label="state.paused ? 'Play' : 'Pause'" :aria-pressed="!state.paused">
              <Icon :name="state.paused ? 'play' : 'pause'" :size="40" />
            </button>
          </div>
          <span class="sr-only" aria-live="polite">{{ state.buffering ? 'Buffering…' : '' }}</span>
        </div>

        <!-- Bottom -->
        <div class="ctrl-bottom" @click.stop>
          <!-- Seek -->
          <div
            class="seekbar"
            role="slider"
            tabindex="0"
            aria-label="Seek"
            :aria-valuemin="0"
            :aria-valuemax="knownDuration || 0"
            :aria-valuenow="state.currentTime"
            :aria-valuetext="`${formatTime(state.currentTime)} of ${formatTime(knownDuration)}`"
            @click="seek"
            @mousemove="onSeekHover"
            @mouseleave="seekHover = null"
            @keydown="onSeekKeydown"
          >
            <div class="seekbar-bg" />
            <div class="seekbar-buf" :style="{ width: bufferProgress + '%' }" />
            <div class="seekbar-fill" :style="{ width: progress + '%' }" />
            <div class="seekbar-thumb" :style="{ left: progress + '%' }" />
            <div v-if="seekHover !== null" class="seekbar-tip" :class="{ 'has-thumb': !!hoverThumbnail }" :style="{ left: ((seekHover / knownDuration) * 100) + '%' }">
              <div v-if="hoverThumbnail" class="seekbar-thumb-preview" :style="{
                backgroundImage: `url(${hoverThumbnail.spriteUrl})`,
                backgroundPosition: `-${hoverThumbnail.x / 2}px -${hoverThumbnail.y / 2}px`,
                backgroundSize: `${(hoverThumbnail.w * 10) / 2}px auto`,
                width: `${hoverThumbnail.w / 2}px`,
                height: `${hoverThumbnail.h / 2}px`,
              }" />
              <span class="seekbar-tip-time">{{ formatTime(seekHover) }}</span>
            </div>
          </div>

          <div class="ctrl-row">
            <button class="c-btn" :aria-label="state.paused ? 'Play' : 'Pause'" :aria-pressed="!state.paused" @click="controls.togglePlay()"><Icon :name="state.paused ? 'play' : 'pause'" :size="22" /></button>
            <button class="c-btn" aria-label="Rewind 10 seconds" @click="controls.skip(-10)"><Icon name="skipback" :size="18" /></button>
            <button class="c-btn" aria-label="Forward 10 seconds" @click="controls.skip(10)"><Icon name="skipforward" :size="18" /></button>

            <div class="vol-group">
              <button class="c-btn" :aria-label="state.muted ? 'Unmute' : 'Mute'" :aria-pressed="state.muted" @click="controls.toggleMute()"><Icon :name="volIcon()" :size="18" /></button>
              <div
                class="vol-bar"
                role="slider"
                tabindex="0"
                aria-label="Volume"
                :aria-valuemin="0"
                :aria-valuemax="100"
                :aria-valuenow="Math.round((state.muted ? 0 : state.volume) * 100)"
                :aria-valuetext="`${Math.round((state.muted ? 0 : state.volume) * 100)}%`"
                @click="setVolume"
                @keydown="onVolumeKeydown"
              ><div class="vol-fill" :style="{ width: (state.muted ? 0 : state.volume * 100) + '%' }" /></div>
            </div>

            <div class="time">{{ formatTime(state.currentTime) }} <span class="time-sep">/</span> {{ formatTime(knownDuration) }}</div>
            <div style="flex: 1" />

            <!-- Audio -->
            <AppMenu
              v-if="audioTracks.length >= 1"
              v-model="showAudioMenu"
              :width="240"
              align="end"
              :side-offset="10"
              trigger-class="vp-trigger"
              content-class="vp-menu-surface"
              trigger-title="Audio track"
            >
              <template #trigger>
                <Icon name="translate" :size="18" />
              </template>
              <div class="surface-section-label vp-menu-title">Audio</div>
              <DropdownMenuItem
                v-for="(a, i) in audioTracks"
                :key="a.index"
                class="surface-item vp-item"
                :class="{ active: i === activeAudioIdx }"
                @select="selectAudio(i)"
              >
                <Icon v-if="i === activeAudioIdx" name="check" :size="14" class="vp-item-check" />
                <span>{{ audioLabel(a) }}</span>
              </DropdownMenuItem>
            </AppMenu>

            <!-- Subs -->
            <AppMenu
              v-if="subtitleTracks.length && !videoCastActive"
              v-model="showSubMenu"
              :width="260"
              align="end"
              :side-offset="10"
              :trigger-class="{ 'vp-trigger': true, active: activeSubIdx >= 0 }"
              content-class="vp-menu-surface"
              trigger-title="Subtitles"
            >
              <template #trigger>
                <Icon name="subtitles" :size="18" />
              </template>
              <div class="surface-section-label vp-menu-title">Subtitles</div>
              <DropdownMenuItem
                class="surface-item vp-item"
                :class="{ active: activeSubIdx === -1 }"
                @select="disableSubs()"
              >
                <Icon v-if="activeSubIdx === -1" name="check" :size="14" class="vp-item-check" />
                <span>Off</span>
              </DropdownMenuItem>
              <DropdownMenuItem
                v-for="(s, i) in subtitleTracks"
                :key="s.index"
                class="surface-item vp-item"
                :class="{ active: i === activeSubIdx }"
                @select="selectSub(i)"
              >
                <Icon v-if="i === activeSubIdx" name="check" :size="14" class="vp-item-check" />
                <span>{{ s.title || s.language?.toUpperCase() || `Track ${s.index}` }}</span>
                <span v-if="s.codec === 'ass' || s.codec === 'ssa'" class="sub-tag">ASS</span>
              </DropdownMenuItem>
            </AppMenu>

            <!-- Quality -->
            <AppMenu
              v-if="usingHLS && availableQualities.length > 0"
              v-model="showQualityMenu"
              :width="240"
              align="end"
              :side-offset="10"
              trigger-class="vp-trigger vp-trigger-quality"
              content-class="vp-menu-surface"
              trigger-title="Quality"
            >
              <template #trigger>
                <Icon name="eq" :size="18" />
                <span class="quality-badge">{{ qualityLabel }}</span>
              </template>
              <div class="surface-section-label vp-menu-title">Quality</div>
              <DropdownMenuItem
                class="surface-item vp-item"
                :class="{ active: activeQuality === 'auto' }"
                @select="selectQuality('auto')"
              >
                <Icon v-if="activeQuality === 'auto'" name="check" :size="14" class="vp-item-check" />
                <span>Auto (Original)</span>
              </DropdownMenuItem>
              <DropdownMenuItem
                v-for="q in availableQualities"
                :key="q.height"
                class="surface-item vp-item"
                :class="{ active: activeQuality === q.label }"
                @select="selectQuality(q.label)"
              >
                <Icon v-if="activeQuality === q.label" name="check" :size="14" class="vp-item-check" />
                <span>{{ q.label }}</span>
                <span class="quality-bitrate">{{ q.height }}p</span>
              </DropdownMenuItem>
            </AppMenu>

            <!-- Video targets only. Cast speakers are intentionally absent:
                 receiver capability comes from the Cast `ca` advertisement,
                 not from provider/model-name guesses. -->
            <AppMenu
              v-if="castDevices.length || videoCastActive"
              v-model="showCastMenu"
              :width="280"
              align="end"
              :side-offset="10"
              :trigger-class="{ 'vp-trigger': true, active: videoCastActive }"
              content-class="vp-menu-surface"
              :trigger-title="videoCastActive ? `Playing on ${cast.deviceName}` : 'Cast video'"
            >
              <template #trigger>
                <Icon :name="castConnecting ? 'loading' : 'cast'" :size="18" :class="{ 'cast-remote-spin': castConnecting }" />
              </template>
              <div class="surface-section-label vp-menu-title">Video capable</div>
              <DropdownMenuItem
                v-for="device in castDevices"
                :key="device.id"
                class="surface-item vp-item"
                @select="pickVideoCastDevice(device.id)"
              >
                <Icon name="television-simple" :size="15" class="surface-item-icon" />
                <span class="cast-video-device-text">
                  <span>{{ device.name }}</span>
                  <span class="cast-video-device-sub">{{ castDeviceSub(device) }}</span>
                </span>
                <Icon v-if="videoCastSession?.device_id === device.id" name="check" :size="13" class="vp-item-check" />
              </DropdownMenuItem>
              <template v-if="videoCastActive">
                <DropdownMenuSeparator class="surface-divider" />
                <DropdownMenuItem class="surface-item vp-item cast-video-disconnect" @select="stopVideoCast(true)">
                  <Icon name="close" :size="14" class="surface-item-icon" />
                  <span>Disconnect and continue here</span>
                </DropdownMenuItem>
              </template>
            </AppMenu>

            <button class="c-btn" :aria-label="state.fullscreen ? 'Exit fullscreen' : 'Enter fullscreen'" :aria-pressed="state.fullscreen" @click="controls.toggleFullscreen()">
              <Icon :name="state.fullscreen ? 'shrink' : 'expand'" :size="18" />
            </button>
          </div>
        </div>
      </div>

      <!-- Stream info panel -->
      <Transition name="slide">
        <div v-if="showInfoPanel" class="info-panel-wrap">
          <div class="info-panel">
            <StreamInfoPanel
            :streamInfo="streamState.streamInfo"
            :fileId="fileId"
            :activeQuality="activeQuality"
            :usingHLS="usingHLS"
            :playerState="state"
            :transcodeStatus="transcodeStatus"
            v-model:mode="panelMode"
          />
          </div>
        </div>
      </Transition>

      <!-- Skip segment button (intro/recap/credits). Up Next owns the
           corner when its countdown is running — during credits both
           would otherwise stack. -->
      <Transition name="upnext">
        <button
          v-if="activeSkipSegment && !(upNextCountdown > 0 && upNext)"
          class="skip-seg-btn"
          @click.stop="skipSegment"
        >
          <span>{{ skipSegmentLabels[activeSkipSegment.type] ?? 'Skip' }}</span>
          <Icon name="skipforward" :size="15" />
        </button>
      </Transition>

      <!-- Up Next overlay -->
      <Transition name="upnext">
        <div v-if="upNextCountdown > 0 && upNext" class="upnext-overlay" @click.stop>
          <div class="upnext-card">
            <div class="upnext-label">Up Next</div>
            <div class="upnext-title">S{{ String(upNext.season_number).padStart(2, '0') }}E{{ String(upNext.episode_number).padStart(2, '0') }}</div>
            <div v-if="upNext.episode_title" class="upnext-ep-title">{{ upNext.episode_title }}</div>
            <div class="upnext-countdown-ring">
              <svg viewBox="0 0 48 48">
                <circle cx="24" cy="24" r="20" class="ring-bg" />
                <circle cx="24" cy="24" r="20" class="ring-fill" :style="{ strokeDashoffset: (1 - upNextCountdown / 10) * 125.6 }" />
              </svg>
              <span class="ring-num">{{ upNextCountdown }}</span>
            </div>
            <div class="upnext-actions">
              <button class="upnext-btn play" @click="playNextEpisode">
                <Icon name="play" :size="14" /> Play Now
              </button>
              <button class="upnext-btn cancel" @click="cancelUpNext">Cancel</button>
            </div>
          </div>
        </div>
      </Transition>
    </template>
  </div>
</template>

<style scoped>
.p { position: fixed; inset: 0; z-index: 9999; background: #000; }
video { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: contain; cursor: pointer; }
.cast-remote-overlay {
  position: absolute;
  inset: 0;
  z-index: 4;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--fg-1);
  background: color-mix(in srgb, var(--bg-0) 92%, transparent);
  pointer-events: none;
}
.cast-remote-overlay :deep(svg) { color: var(--accent); }
.cast-remote-title { margin-top: 8px; font-size: 18px; font-weight: 700; }
.cast-remote-sub { max-width: min(70vw, 640px); color: var(--fg-3); font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.p-center { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; color: rgba(255,255,255,0.5); font-size: 14px; gap: 8px; z-index: 20; }
.spinner { width: 28px; height: 28px; border: 2px solid rgba(255,255,255,0.1); border-top-color: var(--accent); border-radius: 50%; animation: spin 0.7s linear infinite; }

/* Resume overlay — full-surface dimmer with a centered card. Mounts only
   while resumeOpen is true; the video element is paused while it's up. */
.resume-overlay {
  position: absolute;
  inset: 0;
  z-index: 30;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(8px);
}
.resume-card {
  min-width: 420px;
  max-width: 90vw;
  padding: 24px;
  border-radius: var(--r-lg);
  background: var(--bg-1);
  border: 1px solid var(--border);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.5);
}
.resume-kind {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--accent);
  margin-bottom: 6px;
}
.resume-title {
  font-size: 22px;
  font-weight: 700;
  color: var(--fg-0);
  margin-bottom: 18px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.resume-progress {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 18px;
}
.resume-progress-bar {
  height: 6px;
  background: rgba(255, 255, 255, 0.06);
  border-radius: 999px;
  overflow: hidden;
}
.resume-progress-fill {
  height: 100%;
  background: var(--accent);
  border-radius: 999px;
}
.resume-progress-label {
  font-size: 11px;
  color: var(--fg-3);
  align-self: flex-end;
}
.resume-actions {
  display: flex;
  gap: 10px;
  justify-content: flex-end;
}
@keyframes spin { to { transform: rotate(360deg) } }

:deep(.AkariSub) { z-index: 2 !important; }

/* VTT subtitle overlay. Subtitle text paints on video, so literal white +
   black scrim/shadow is correct here (the documented on-artwork exception)
   — not theme tokens. z-index sits above the video and the AkariSub canvas
   (2) but below .ctrl (10) so the OSD always wins. */
.vtt-layer {
  position: absolute;
  left: 0;
  right: 0;
  bottom: calc(4% + env(safe-area-inset-bottom, 0px));
  z-index: 5;
  display: flex;
  justify-content: center;
  padding: 0 24px;
  pointer-events: none;
  transition: bottom 0.25s ease;
}
/* Control bar visible → lift cues clear of it (gradient + row ≈ 90px). */
.vtt-layer.ctrl-up { bottom: calc(96px + env(safe-area-inset-bottom, 0px)); }
.vtt-cue {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 2px;
  max-width: min(88%, 920px);
  text-align: center;
}
.vtt-line {
  color: #fff;
  font-size: clamp(16px, 2.6vmin, 28px);
  line-height: 1.35;
  font-weight: 500;
  background: rgba(0, 0, 0, 0.55);
  padding: 2px 10px;
  border-radius: 4px;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.9), 0 0 8px rgba(0, 0, 0, 0.4);
  white-space: pre-wrap;
}

/* Controls */
.ctrl { position: absolute; inset: 0; z-index: 10; display: flex; flex-direction: column; opacity: 0; transition: opacity 0.3s; pointer-events: none; }
.ctrl.visible { opacity: 1; pointer-events: auto; }

/* Safe-area insets fall back to 0px on non-notch devices/desktop, so the
   base 16/20px padding is unchanged there — this only pads out further to
   clear a notch/Dynamic Island in landscape fullscreen. */
.ctrl-top { display: flex; align-items: center; gap: 10px; padding: calc(16px + env(safe-area-inset-top, 0px)) calc(20px + env(safe-area-inset-right, 0px)) 40px calc(20px + env(safe-area-inset-left, 0px)); background: linear-gradient(to bottom, rgba(0,0,0,0.6), transparent); }
.ctrl-title { flex: 1; font-size: 15px; font-weight: 600; color: #fff; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.ctrl-center { flex: 1; display: flex; align-items: center; justify-content: center; }
.center-play { position: relative; width: 72px; height: 72px; display: flex; align-items: center; justify-content: center; }
.center-btn { width: 72px; height: 72px; border-radius: 50%; background: rgba(0,0,0,0.4); backdrop-filter: blur(12px); border: 1px solid rgba(255,255,255,0.1); color: #fff; display: flex; align-items: center; justify-content: center; transition: background 0.2s, transform 0.2s; }
.center-btn:hover { background: rgba(0,0,0,0.6); transform: scale(1.08); }
/* Optical centering: the phosphor play triangle sits right-of-centre in its
   box, so nudge it left a hair. The pause glyph is symmetric — left alone. */
.center-btn.is-play :deep(svg) { transform: translateX(-2px); }
/* Buffering ring — concentric with the button, wrapping it. */
.center-ring { position: absolute; inset: -7px; border-radius: 50%; border: 3px solid rgba(255,255,255,0.12); border-top-color: var(--accent); animation: spin 0.7s linear infinite; pointer-events: none; }

.ctrl-bottom { padding: 40px calc(20px + env(safe-area-inset-right, 0px)) calc(16px + env(safe-area-inset-bottom, 0px)) calc(20px + env(safe-area-inset-left, 0px)); background: linear-gradient(to top, rgba(0,0,0,0.6), transparent); }

/* Seek bar */
.seekbar { position: relative; height: 28px; display: flex; align-items: center; cursor: pointer; margin-bottom: 4px; }
.seekbar-bg { position: absolute; left: 0; right: 0; height: 3px; background: rgba(255,255,255,0.12); border-radius: 2px; transition: height 0.12s; }
.seekbar:hover .seekbar-bg { height: 6px; }
.seekbar-buf { position: absolute; left: 0; height: 3px; background: rgba(255,255,255,0.18); border-radius: 2px; pointer-events: none; transition: height 0.12s; }
.seekbar:hover .seekbar-buf { height: 6px; }
.seekbar-fill { position: absolute; left: 0; height: 3px; background: var(--accent); border-radius: 2px; pointer-events: none; transition: height 0.12s; }
.seekbar:hover .seekbar-fill { height: 6px; }
.seekbar-thumb { position: absolute; width: 14px; height: 14px; background: var(--accent); border-radius: 50%; transform: translate(-50%, 0); opacity: 0; pointer-events: none; transition: opacity 0.15s; box-shadow: 0 0 6px color-mix(in srgb, var(--accent) 40%, transparent); }
.seekbar:hover .seekbar-thumb { opacity: 1; }
.seekbar-tip { position: absolute; bottom: 24px; transform: translateX(-50%); background: rgba(0,0,0,0.85); color: #fff; font-size: 11px; font-family: var(--font-mono, monospace); padding: 3px 8px; border-radius: 4px; pointer-events: none; white-space: nowrap; }
.seekbar-tip.has-thumb { padding: 4px; display: flex; flex-direction: column; align-items: center; gap: 4px; bottom: 28px; border-radius: 6px; }
.seekbar-thumb-preview { border-radius: 3px; flex-shrink: 0; }
.seekbar-tip-time { font-size: 10px; line-height: 1; }

/* Controls row */
.ctrl-row { display: flex; align-items: center; gap: 4px; }
.c-btn { width: 38px; height: 38px; border-radius: 8px; display: flex; align-items: center; justify-content: center; color: rgba(255,255,255,0.8); background: transparent; transition: all 0.12s; flex-shrink: 0; }
.c-btn:hover { color: #fff; background: rgba(255,255,255,0.08); }
.c-btn.active { color: var(--accent); }

/* Volume */
.vol-group { display: flex; align-items: center; gap: 4px; }
.vol-bar { width: 80px; height: 22px; display: flex; align-items: center; cursor: pointer; position: relative; }
.vol-bar::before { content: ''; position: absolute; left: 0; right: 0; height: 3px; background: rgba(255,255,255,0.15); border-radius: 2px; }
.vol-fill { position: absolute; left: 0; height: 3px; background: #fff; border-radius: 2px; pointer-events: none; }

/* Time */
.time { font-size: 12px; font-family: var(--font-mono, monospace); color: rgba(255,255,255,0.7); margin-left: 10px; white-space: nowrap; }
.time-sep { color: rgba(255,255,255,0.3); margin: 0 2px; }

/* Coarse pointers (touch) get ≥44px hit targets — mouse/trackpad keeps the
   tighter 38px chrome unchanged. Only the hit area grows for the sliders;
   the visual track/thumb sizing (absolutely positioned, centered) is
   untouched. */
@media (pointer: coarse) {
  .c-btn { width: 44px; height: 44px; }
  .seekbar { height: 44px; }
  .vol-bar { height: 44px; }
}

/* Info panel — no dimming, positioned top-right, doesn't block video */
.info-panel-wrap { position: absolute; top: 56px; right: 16px; z-index: 50; pointer-events: none; }
.info-panel { background: rgba(10,10,16,0.92); backdrop-filter: blur(20px) saturate(1.3); border: 1px solid rgba(255,255,255,0.06); border-radius: 12px; padding: 16px 18px; box-shadow: 0 8px 40px rgba(0,0,0,0.5); max-height: calc(100vh - 160px); overflow-y: auto; scrollbar-width: thin; scrollbar-color: rgba(255,255,255,0.1) transparent; pointer-events: auto; }

.slide-enter-active { transition: all 0.2s cubic-bezier(0.2, 0, 0, 1); }
.slide-leave-active { transition: all 0.12s ease-in; }
.slide-enter-from { opacity: 0; transform: translateX(12px); }
.slide-leave-to { opacity: 0; transform: translateX(8px); }

/* Up Next overlay */
.upnext-overlay {
  position: absolute; bottom: 100px; right: 24px; z-index: 60;
}
.skip-seg-btn {
  position: absolute; bottom: 100px; right: 24px; z-index: 60;
  display: flex; align-items: center; gap: 8px;
  background: rgba(10,10,16,0.92); backdrop-filter: blur(20px) saturate(1.3);
  border: 1px solid rgba(255,255,255,0.18); border-radius: 10px;
  padding: 11px 20px; cursor: pointer;
  font-size: 14px; font-weight: 700; color: #fff; letter-spacing: 0.02em;
  box-shadow: 0 12px 40px rgba(0,0,0,0.55);
  transition: background 0.15s, border-color 0.15s, transform 0.15s;
}
.skip-seg-btn :deep(svg) { color: var(--accent); }
.skip-seg-btn:hover {
  background: color-mix(in srgb, var(--accent) 16%, transparent); border-color: var(--accent);
  transform: translateY(-1px);
}
.upnext-card {
  background: rgba(10,10,16,0.92); backdrop-filter: blur(20px) saturate(1.3);
  border: 1px solid rgba(255,255,255,0.08); border-radius: 14px;
  padding: 20px 24px; min-width: 220px;
  box-shadow: 0 12px 40px rgba(0,0,0,0.6);
  display: flex; flex-direction: column; align-items: center; gap: 8px;
}
.upnext-label { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: 0.12em; color: var(--accent); }
.upnext-title { font-size: 18px; font-weight: 700; color: #fff; }
.upnext-ep-title { font-size: 13px; color: rgba(255,255,255,0.6); max-width: 200px; text-align: center; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.upnext-countdown-ring { position: relative; width: 48px; height: 48px; margin: 6px 0; }
.upnext-countdown-ring svg { width: 100%; height: 100%; transform: rotate(-90deg); }
.ring-bg { fill: none; stroke: rgba(255,255,255,0.08); stroke-width: 3; }
.ring-fill { fill: none; stroke: var(--accent); stroke-width: 3; stroke-linecap: round; stroke-dasharray: 125.6; transition: stroke-dashoffset 1s linear; }
.ring-num { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; font-size: 16px; font-weight: 700; color: #fff; font-family: var(--font-mono, monospace); }
.upnext-actions { display: flex; gap: 8px; margin-top: 4px; }
.upnext-btn {
  padding: 6px 14px; border-radius: 8px; font-size: 12px; font-weight: 600;
  display: flex; align-items: center; gap: 6px; transition: all 0.15s;
}
.upnext-btn.play { background: var(--accent); color: var(--accent-ink); }
.upnext-btn.play:hover { filter: brightness(1.1); }
.upnext-btn.cancel { background: rgba(255,255,255,0.08); color: rgba(255,255,255,0.7); }
.upnext-btn.cancel:hover { background: rgba(255,255,255,0.14); color: #fff; }

.upnext-enter-active { transition: all 0.3s cubic-bezier(0.2, 0, 0, 1); }
.upnext-leave-active { transition: all 0.15s ease-in; }
.upnext-enter-from { opacity: 0; transform: translateY(16px) scale(0.95); }
.upnext-leave-to { opacity: 0; transform: translateY(8px); }

@media (max-width: 768px) { .vol-group { display: none; } .upnext-overlay { bottom: 80px; right: 12px; } .skip-seg-btn { bottom: 80px; right: 12px; } }
</style>

<!--
  AppMenu portals the audio/subs/quality menus out of this component's
  scoped DOM, so the trigger chrome AND the per-item styles have to live
  unscoped to reach the rendered elements. Scoped `.c-btn` never lands on
  the AppMenu-rendered <button> (it carries AppMenu's scope id, not ours),
  which is why these triggers used to collapse to bare 18px icons.
-->
<style>
/* Bottom-bar menu triggers (audio / subtitles / quality). Mirrors the
   scoped `.c-btn` box so the right-hand cluster matches the plain control
   buttons on the left. */
.app-menu-trigger.vp-trigger {
  min-width: 38px;
  height: 38px;
  padding: 0 8px;
  border-radius: 8px;
  gap: 4px;
  color: rgba(255, 255, 255, 0.8);
  transition: color 0.12s, background 0.12s;
}
.app-menu-trigger.vp-trigger:hover {
  color: #fff;
  background: rgba(255, 255, 255, 0.08);
}
.app-menu-trigger.vp-trigger[data-state="open"] {
  color: var(--accent);
  background: rgba(255, 255, 255, 0.08);
}
.app-menu-trigger.vp-trigger.active { color: var(--accent); }

/* Match the scoped .c-btn touch-target bump (coarse pointers only). */
@media (pointer: coarse) {
  .app-menu-trigger.vp-trigger { min-width: 44px; height: 44px; }
}

/* Current-quality badge sitting next to the sliders icon. */
.vp-trigger .quality-badge {
  font-size: 9px;
  font-weight: 700;
  font-family: var(--font-mono, monospace);
  color: rgba(255, 255, 255, 0.6);
}
.vp-trigger[data-state="open"] .quality-badge { color: var(--accent); }

/* The player root is a z-index:9999 fixed overlay, so a menu portalled to
   <body> (reka wraps it at z-index:200) would paint *behind* the video.
   Lift only the player's own menus back above it — app-wide menus keep
   their normal stacking. */
[data-reka-popper-content-wrapper]:has(.vp-menu-surface) { z-index: 10000 !important; }

.vp-menu-title { padding: 8px 14px 6px; }

.vp-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  font-size: 13px;
  color: rgba(255, 255, 255, 0.7);
}
.vp-item.active { color: var(--accent); }
.vp-item-check { color: var(--accent); flex-shrink: 0; }
.cast-video-device-text { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
.cast-video-device-text > span:first-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cast-video-device-sub { color: var(--fg-3); font-size: 10px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cast-video-disconnect { color: var(--bad); }
.cast-video-disconnect[data-highlighted] { color: var(--bad); background: color-mix(in srgb, var(--bad) 8%, transparent); }
.cast-remote-spin { animation: video-cast-spin 0.9s linear infinite; }
@keyframes video-cast-spin { to { transform: rotate(360deg); } }

.vp-item .sub-tag {
  font-size: 9px;
  font-weight: 700;
  padding: 1px 5px;
  border-radius: 3px;
  background: rgba(200, 130, 255, 0.12);
  color: rgb(200, 130, 255);
  margin-left: auto;
}
.vp-item .quality-bitrate {
  font-size: 10px;
  color: rgba(255, 255, 255, 0.3);
  margin-left: auto;
  font-family: var(--font-mono, monospace);
}
</style>
