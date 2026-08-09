import type HlsType from 'hls.js'
import type { VideoPlaybackDiagnostics, VideoPlaybackState, VideoPlaybackTimelineEvent } from '~/types/video-playback'
import { isBearerAuthToken } from '~/composables/useAuth'

// HTTP outcomes that mean "the server is bouncing or forgot this session",
// not "this stream is dead". hls.js refuses to retry any of these on its own:
// its built-in retryForHttpStatus() skips every 4xx *and* status 0 (an XHR
// that never got a response — exactly what a restarting API looks like while
// the browser is still online). Heya's segment endpoint rebuilds a missing
// session from the segment URL alone, so the retry is what brings the stream
// back; without it a 10-second restart permanently dead-ends playback.
const RESUMABLE_LOAD_STATUS = new Set([0, 404, 410, 500, 502, 503, 504])

// How long we keep nursing a broken load before surfacing an error. The
// backoff below tops out at 15s, so eight attempts ride out roughly two
// minutes of downtime — comfortably longer than an air rebuild or a pod roll.
const MAX_RECONNECT_ATTEMPTS = 8

function retryThroughRestart(
  retryConfig: { maxNumRetry: number } | null | undefined,
  retryCount: number,
  isTimeout: boolean,
  loaderResponse: { code?: number } | undefined,
  hlsDefault: boolean,
): boolean {
  if (hlsDefault) return true
  if (!retryConfig || retryCount >= retryConfig.maxNumRetry) return false
  return isTimeout || RESUMABLE_LOAD_STATUS.has(loaderResponse?.code ?? 0)
}

// Load policies that survive a server restart. Values mirror hls.js's own
// defaults apart from a bigger error budget and the shouldRetry override.
function resumableLoadPolicy(maxTimeToFirstByteMs: number, maxLoadTimeMs: number, maxNumRetry: number) {
  return {
    default: {
      maxTimeToFirstByteMs,
      maxLoadTimeMs,
      timeoutRetry: { maxNumRetry: 4, retryDelayMs: 0, maxRetryDelayMs: 0 },
      errorRetry: {
        maxNumRetry,
        retryDelayMs: 1000,
        maxRetryDelayMs: 15000,
        shouldRetry: retryThroughRestart,
      },
    },
  }
}

export function useHeyaPlayer(videoRef: Ref<HTMLVideoElement | undefined>) {
  let hls: HlsType | null = null
  let sourceGeneration = 0

  const state = reactive<VideoPlaybackState>({
    playing: false,
    paused: true,
    ended: false,
    loading: true,
    buffering: false,
    currentTime: 0,
    duration: 0,
    buffered: 0,
    volume: 1,
    muted: false,
    fullscreen: false,
    error: null,
    reconnecting: false,
    seekRevision: 0,
  })

  const diagnostics = reactive<VideoPlaybackDiagnostics>({
    backend: 'browser',
    transport: {
      inputBytesPerSecond: 0,
      segmentsLoaded: 0,
      activeVariantIndex: -1,
      lastSegmentBytes: 0,
      lastSegmentMilliseconds: 0,
      timeline: [],
    },
    health: {
      droppedFrames: 0,
      decodedFrames: 0,
    },
  })

  const timeline: VideoPlaybackTimelineEvent[] = diagnostics.transport!.timeline!
  let requestedSeek: number | null = null
  let lastObservedTime = 0
  function trace(kind: string, positionSeconds: number, detail?: string) {
    timeline.push({ atMilliseconds: Date.now(), kind, positionSeconds, ...(detail ? { detail } : {}) })
    if (timeline.length > 80) timeline.splice(0, timeline.length - 80)
  }

  // Sample video element quality stats. Called from the metrics interval —
  // browsers update these on a frame-by-frame basis, so polling is sufficient.
  function sampleQuality() {
    const v = videoRef.value
    if (!v || typeof v.getVideoPlaybackQuality !== 'function') return
    const q = v.getVideoPlaybackQuality()
    diagnostics.health!.droppedFrames = q.droppedVideoFrames
    diagnostics.health!.decodedFrames = q.totalVideoFrames
    diagnostics.sampledAtMilliseconds = Date.now()
  }

  let metricsInterval: ReturnType<typeof setInterval> | null = null

  function syncState() {
    const v = videoRef.value
    if (!v) return
    state.playing = !v.paused && !v.ended
    state.paused = v.paused
    state.ended = v.ended
    if (v.currentTime < lastObservedTime-1.5 && requestedSeek == null) {
      trace('unexpected-backward-time', v.currentTime, `from=${lastObservedTime.toFixed(3)}`)
    }
    state.currentTime = v.currentTime
    lastObservedTime = v.currentTime
    state.duration = isFinite(v.duration) ? v.duration : state.duration
    state.volume = v.volume
    state.muted = v.muted
    if (v.buffered.length > 0) {
      state.buffered = v.buffered.end(v.buffered.length - 1)
    }
  }

  useEventListener(videoRef, 'timeupdate', syncState)
  useEventListener(videoRef, 'durationchange', syncState)
  useEventListener(videoRef, 'volumechange', syncState)
  useEventListener(videoRef, 'play', () => { state.playing = true; state.paused = false; state.loading = false })
  useEventListener(videoRef, 'pause', () => { state.playing = false; state.paused = true })
  useEventListener(videoRef, 'ended', () => { state.ended = true; state.playing = false })
  useEventListener(videoRef, 'waiting', () => { state.buffering = true })
  useEventListener(videoRef, 'canplay', () => { state.buffering = false; state.loading = false })
  useEventListener(videoRef, 'playing', () => { state.buffering = false; state.loading = false; state.playing = true; state.paused = false })
  useEventListener(videoRef, 'progress', syncState)
  useEventListener(videoRef, 'seeking', () => trace('media-seeking', videoRef.value?.currentTime ?? state.currentTime, requestedSeek == null ? 'browser' : `requested=${requestedSeek.toFixed(3)}`))
  useEventListener(videoRef, 'seeked', () => {
    trace('media-seeked', videoRef.value?.currentTime ?? state.currentTime, requestedSeek == null ? undefined : `requested=${requestedSeek.toFixed(3)}`)
    requestedSeek = null
    state.seekRevision += 1
    syncState()
  })
  useEventListener(videoRef, 'error', () => {
    const v = videoRef.value
    const e = v?.error
    if (!e) return
    // A reconnect in flight owns the error surface. MSE routinely reports a
    // network error on the element the moment hls.js stops feeding it, and
    // letting that win would swap the spinner for a dead-end error card while
    // we are already recovering.
    if (state.reconnecting) return
    const codes: Record<number, string> = { 1: 'Aborted', 2: 'Network error', 3: 'Decode error', 4: 'Source not supported' }
    state.error = `${codes[e.code] || 'Error'}${e.message ? ` — ${e.message}` : ''}`
  })

  // Reconnect bookkeeping. hls.js gives up permanently once a load error is
  // marked fatal, so recovery has to be driven from here: startLoad() rebuilds
  // the pipeline from the current position, and the server rebuilds the
  // transcode session from the segment URL it receives.
  let reconnectAttempt = 0
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  let mediaRecoveryAttempt = 0

  function cancelReconnect() {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    reconnectAttempt = 0
    mediaRecoveryAttempt = 0
    state.reconnecting = false
  }

  function scheduleReconnect(generation: number, detail: string) {
    if (reconnectTimer) return
    if (reconnectAttempt >= MAX_RECONNECT_ATTEMPTS) {
      state.reconnecting = false
      state.error = `HLS: ${detail}`
      return
    }
    const delay = Math.min(1000 * 2 ** reconnectAttempt, 15000)
    reconnectAttempt++
    state.reconnecting = true
    state.buffering = true
    state.error = null
    trace('hls-reconnect-scheduled', videoRef.value?.currentTime ?? 0, detail)
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null
      if (generation !== sourceGeneration || !hls) return
      // Resume where the element actually sits. -1 lets hls.js pick (a fresh
      // load that never reached playback has no meaningful position yet).
      const resumeAt = videoRef.value?.currentTime ?? 0
      trace('hls-reconnect-start', resumeAt, `generation=${generation}`)
      hls.startLoad(resumeAt > 0 ? resumeAt : -1)
      videoRef.value?.play().catch(() => {})
    }, delay)
  }

  async function loadSource(src: string, token?: string) {
    const generation = ++sourceGeneration
    clearHLS()
    const v = videoRef.value
    if (!v) return

    state.error = null
    state.loading = true
    state.ended = false
    const isHLS = src.includes('.m3u8')
    // Reset diagnostics — new source, new measurements.
    diagnostics.sampledAtMilliseconds = undefined
    diagnostics.transport = {
      inputBytesPerSecond: 0,
      segmentsLoaded: 0,
      activeVariantIndex: -1,
      lastSegmentBytes: 0,
      lastSegmentMilliseconds: 0,
      timeline,
    }
    trace('source-load', v.currentTime, `generation=${generation} ${isHLS ? 'hls' : 'direct'}`)
    diagnostics.health = {
      droppedFrames: 0,
      decodedFrames: 0,
    }
    // Safari can play HLS natively and never needs the half-megabyte JS
    // engine. Other browsers import hls.js only when an HLS source is
    // actually selected, keeping normal browsing out of the initial bundle.
    if (isHLS && v.canPlayType('application/vnd.apple.mpegurl')) {
      v.src = src
      v.play().catch(() => {})
      return
    }

    if (isHLS) {
      const { default: Hls } = await import('hls.js')
      if (generation !== sourceGeneration || !videoRef.value) return
      if (!Hls.isSupported()) {
        state.loading = false
        state.error = 'HLS playback is not supported by this browser'
        return
      }
      hls = new Hls({
        maxBufferLength: 30,
        maxMaxBufferLength: 60,
        // Explicit policies rather than the deprecated *LoadingMaxRetry knobs:
        // only the policy form accepts shouldRetry, and shouldRetry is the
        // whole point — see RESUMABLE_LOAD_STATUS.
        fragLoadPolicy: resumableLoadPolicy(10000, 120000, 10),
        playlistLoadPolicy: resumableLoadPolicy(10000, 20000, 8),
        manifestLoadPolicy: resumableLoadPolicy(Infinity, 20000, 8),
        startPosition: 0,
        xhrSetup(xhr: XMLHttpRequest, url: string) {
          if (isBearerAuthToken(token)) xhr.setRequestHeader('Authorization', `Bearer ${token}`)
          withClientSurfaceHeaders(url).forEach((value, name) => xhr.setRequestHeader(name, value))
        },
      })
      hls.loadSource(src)
      hls.attachMedia(videoRef.value)
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (!data.fatal) return
        if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
          // recoverMediaError alone loops forever on a codec the decoder will
          // never accept. Escalate: plain recover, then an audio-codec swap,
          // then admit defeat.
          mediaRecoveryAttempt++
          if (mediaRecoveryAttempt === 1) {
            hls!.recoverMediaError()
          } else if (mediaRecoveryAttempt === 2) {
            hls!.swapAudioCodec()
            hls!.recoverMediaError()
          } else {
            state.reconnecting = false
            state.error = `HLS: ${data.type} - ${data.details}`
          }
          return
        }
        // Everything else that reaches "fatal" is a load that ran out of
        // retries — a restarting server, a dropped tunnel, an evicted session.
        // None of those are terminal for the stream itself, so keep nursing it
        // instead of tearing the player down and making the user hit play again.
        scheduleReconnect(generation, `${data.type} - ${data.details}`)
      })
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        videoRef.value?.play().catch(() => {})
      })

      // Bandwidth telemetry. hls.js fires FRAG_LOADED for every segment with
      // detailed timing & size info; we EWMA it to smooth over bursts.
      hls.on(Hls.Events.FRAG_LOADED, (_event, data) => {
        // A segment arrived, so whatever we were reconnecting through is over.
        // Reset the budget too: the next outage gets a full set of attempts.
        if (state.reconnecting || reconnectAttempt > 0) cancelReconnect()

        const bytes = data.frag?.stats?.loaded ?? 0
        const loading = data.frag?.stats?.loading
        const ms = loading ? Math.max(1, loading.end - loading.start) : 0
        if (!bytes || !ms) return
        const bps = (bytes / ms) * 1000
        // EWMA with alpha=0.3 — responsive without flickering.
        const transport = diagnostics.transport!
        const previous = transport.inputBytesPerSecond ?? 0
        transport.inputBytesPerSecond = previous === 0 ? bps : previous * 0.7 + bps * 0.3
        transport.lastSegmentBytes = bytes
        transport.lastSegmentMilliseconds = ms
        transport.segmentsLoaded = (transport.segmentsLoaded ?? 0) + 1
        diagnostics.sampledAtMilliseconds = Date.now()
        trace('hls-fragment', videoRef.value?.currentTime ?? 0, `sn=${String(data.frag?.sn)} start=${Number(data.frag?.start ?? 0).toFixed(3)} duration=${Number(data.frag?.duration ?? 0).toFixed(3)}`)
      })
      hls.on(Hls.Events.LEVEL_SWITCHED, (_event, data) => {
        diagnostics.transport!.activeVariantIndex = data.level
        diagnostics.sampledAtMilliseconds = Date.now()
      })
    } else {
      v.src = src
      v.play().catch(() => {})
    }
  }

  function clearHLS() {
    cancelReconnect()
    if (hls) { hls.destroy(); hls = null }
  }

  function destroyHLS() {
    sourceGeneration++
    clearHLS()
  }

  const controls = {
    play() { videoRef.value?.play() },
    pause() { videoRef.value?.pause() },
    togglePlay() { videoRef.value?.paused ? videoRef.value?.play() : videoRef.value?.pause() },
    seek(time: number) {
      if (!videoRef.value) return
      requestedSeek = Math.max(0, Math.min(state.duration, time))
      trace('user-seek', state.currentTime, `to=${requestedSeek.toFixed(3)}`)
      videoRef.value.currentTime = requestedSeek
    },
    skip(seconds: number) { if (videoRef.value) videoRef.value.currentTime = Math.max(0, Math.min(state.duration, videoRef.value.currentTime + seconds)) },
    setVolume(v: number) { if (videoRef.value) { videoRef.value.volume = Math.max(0, Math.min(1, v)); state.volume = videoRef.value.volume } },
    toggleMute() { if (videoRef.value) { videoRef.value.muted = !videoRef.value.muted; state.muted = videoRef.value.muted } },
    toggleFullscreen() {
      if (document.fullscreenElement) document.exitFullscreen()
      else document.documentElement.requestFullscreen()
    },
  }

  useEventListener(document, 'fullscreenchange', () => { state.fullscreen = !!document.fullscreenElement })

  onMounted(() => {
    // Sample dropped/decoded frame counters at 1 Hz. Cheap call; only fires
    // while the player is mounted.
    metricsInterval = setInterval(sampleQuality, 1000)
  })

  onUnmounted(() => {
    destroyHLS()
    if (metricsInterval) { clearInterval(metricsInterval); metricsInterval = null }
  })

  function isTimeBuffered(time: number) {
    const v = videoRef.value
    if (!v) return false
    for (let i = 0; i < v.buffered.length; i++) {
      if (time >= v.buffered.start(i) && time <= v.buffered.end(i)) return true
    }
    return false
  }

  return { state, diagnostics, controls, loadSource, destroyHLS, isTimeBuffered, trace }
}

export function formatTime(s: number): string {
  if (!isFinite(s) || s < 0) return '0:00'
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = Math.floor(s % 60)
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : `${m}:${String(sec).padStart(2, '0')}`
}
