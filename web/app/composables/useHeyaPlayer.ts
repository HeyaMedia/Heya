import type HlsType from 'hls.js'
import type { VideoPlaybackDiagnostics, VideoPlaybackState } from '~/types/video-playback'
import { isBearerAuthToken } from '~/composables/useAuth'

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
    },
    health: {
      droppedFrames: 0,
      decodedFrames: 0,
    },
  })

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
    state.currentTime = v.currentTime
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
  useEventListener(videoRef, 'seeked', () => { state.seekRevision += 1; syncState() })
  useEventListener(videoRef, 'error', () => {
    const v = videoRef.value
    const e = v?.error
    if (!e) return
    const codes: Record<number, string> = { 1: 'Aborted', 2: 'Network error', 3: 'Decode error', 4: 'Source not supported' }
    state.error = `${codes[e.code] || 'Error'}${e.message ? ` — ${e.message}` : ''}`
  })

  async function loadSource(src: string, token?: string) {
    const generation = ++sourceGeneration
    clearHLS()
    const v = videoRef.value
    if (!v) return

    state.error = null
    state.loading = true
    state.ended = false
    // Reset diagnostics — new source, new measurements.
    diagnostics.sampledAtMilliseconds = undefined
    diagnostics.transport = {
      inputBytesPerSecond: 0,
      segmentsLoaded: 0,
      activeVariantIndex: -1,
      lastSegmentBytes: 0,
      lastSegmentMilliseconds: 0,
    }
    diagnostics.health = {
      droppedFrames: 0,
      decodedFrames: 0,
    }

    const isHLS = src.includes('.m3u8')

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
        fragLoadingMaxRetry: 10,
        fragLoadingRetryDelay: 1500,
        fragLoadingMaxRetryTimeout: 30000,
        levelLoadingMaxRetry: 6,
        levelLoadingRetryDelay: 1000,
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
          hls!.recoverMediaError()
        } else {
          state.error = `HLS: ${data.type} - ${data.details}`
        }
      })
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        videoRef.value?.play().catch(() => {})
      })

      // Bandwidth telemetry. hls.js fires FRAG_LOADED for every segment with
      // detailed timing & size info; we EWMA it to smooth over bursts.
      hls.on(Hls.Events.FRAG_LOADED, (_event, data) => {
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
    seek(time: number) { if (videoRef.value) videoRef.value.currentTime = Math.max(0, Math.min(state.duration, time)) },
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

  return { state, diagnostics, controls, loadSource, destroyHLS }
}

export function formatTime(s: number): string {
  if (!isFinite(s) || s < 0) return '0:00'
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = Math.floor(s % 60)
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : `${m}:${String(sec).padStart(2, '0')}`
}
