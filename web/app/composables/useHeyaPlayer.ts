import Hls from 'hls.js'

export interface HeyaPlayerState {
  playing: boolean
  paused: boolean
  ended: boolean
  loading: boolean
  buffering: boolean
  currentTime: number
  duration: number
  buffered: number
  volume: number
  muted: boolean
  fullscreen: boolean
  error: string | null

  // Diagnostics — updated as HLS fragments load and as the video element
  // reports playback quality. Zero until the first sample arrives.
  downloadBps: number       // EWMA of bytes/sec across recent fragment loads
  lastFragBytes: number
  lastFragMs: number
  fragsLoaded: number
  currentLevel: number      // hls.js variant index (-1 when not HLS)
  droppedFrames: number
  decodedFrames: number
}

export function useHeyaPlayer(videoRef: Ref<HTMLVideoElement | undefined>) {
  let hls: Hls | null = null

  const state = reactive<HeyaPlayerState>({
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
    downloadBps: 0,
    lastFragBytes: 0,
    lastFragMs: 0,
    fragsLoaded: 0,
    currentLevel: -1,
    droppedFrames: 0,
    decodedFrames: 0,
  })

  // Sample video element quality stats. Called from the metrics interval —
  // browsers update these on a frame-by-frame basis, so polling is sufficient.
  function sampleQuality() {
    const v = videoRef.value
    if (!v || typeof v.getVideoPlaybackQuality !== 'function') return
    const q = v.getVideoPlaybackQuality()
    state.droppedFrames = q.droppedVideoFrames
    state.decodedFrames = q.totalVideoFrames
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

  function bindEvents() {
    const v = videoRef.value
    if (!v) return
    v.addEventListener('timeupdate', syncState)
    v.addEventListener('durationchange', syncState)
    v.addEventListener('volumechange', syncState)
    v.addEventListener('play', () => { state.playing = true; state.paused = false; state.loading = false })
    v.addEventListener('pause', () => { state.playing = false; state.paused = true })
    v.addEventListener('ended', () => { state.ended = true; state.playing = false })
    v.addEventListener('waiting', () => { state.buffering = true })
    v.addEventListener('canplay', () => { state.buffering = false; state.loading = false })
    v.addEventListener('playing', () => { state.buffering = false; state.loading = false; state.playing = true; state.paused = false })
    v.addEventListener('progress', syncState)
    v.addEventListener('error', () => {
      const e = v.error
      if (!e) return
      const codes: Record<number, string> = { 1: 'Aborted', 2: 'Network error', 3: 'Decode error', 4: 'Source not supported' }
      state.error = `${codes[e.code] || 'Error'}${e.message ? ` — ${e.message}` : ''}`
    })
  }

  let eventsBound = false

  function loadSource(src: string, token?: string) {
    destroyHLS()
    const v = videoRef.value
    if (!v) return

    if (!eventsBound) { bindEvents(); eventsBound = true }

    state.error = null
    state.loading = true
    state.ended = false
    // Reset diagnostics — new source, new measurements.
    state.downloadBps = 0
    state.lastFragBytes = 0
    state.lastFragMs = 0
    state.fragsLoaded = 0
    state.currentLevel = -1
    state.droppedFrames = 0
    state.decodedFrames = 0

    const isHLS = src.includes('.m3u8')

    if (isHLS && Hls.isSupported()) {
      hls = new Hls({
        maxBufferLength: 30,
        maxMaxBufferLength: 60,
        fragLoadingMaxRetry: 10,
        fragLoadingRetryDelay: 1500,
        fragLoadingMaxRetryTimeout: 30000,
        levelLoadingMaxRetry: 6,
        levelLoadingRetryDelay: 1000,
        startPosition: 0,
        ...(token ? {
          xhrSetup(xhr: XMLHttpRequest) {
            xhr.setRequestHeader('Authorization', `Bearer ${token}`)
          },
        } : {}),
      })
      hls.loadSource(src)
      hls.attachMedia(v)
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (!data.fatal) return
        if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
          hls!.recoverMediaError()
        } else {
          state.error = `HLS: ${data.type} - ${data.details}`
        }
      })
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        v.play().catch(() => {})
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
        state.downloadBps = state.downloadBps === 0 ? bps : state.downloadBps * 0.7 + bps * 0.3
        state.lastFragBytes = bytes
        state.lastFragMs = ms
        state.fragsLoaded += 1
      })
      hls.on(Hls.Events.LEVEL_SWITCHED, (_event, data) => {
        state.currentLevel = data.level
      })
    } else if (isHLS && v.canPlayType('application/vnd.apple.mpegurl')) {
      v.src = src
      v.play().catch(() => {})
    } else {
      v.src = src
      v.play().catch(() => {})
    }
  }

  function destroyHLS() {
    if (hls) { hls.destroy(); hls = null }
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

  onMounted(() => {
    document.addEventListener('fullscreenchange', () => { state.fullscreen = !!document.fullscreenElement })
    // Sample dropped/decoded frame counters at 1 Hz. Cheap call; only fires
    // while the player is mounted.
    metricsInterval = setInterval(sampleQuality, 1000)
  })

  onUnmounted(() => {
    destroyHLS()
    if (metricsInterval) { clearInterval(metricsInterval); metricsInterval = null }
  })

  return { state, controls, loadSource, destroyHLS }
}

export function formatTime(s: number): string {
  if (!isFinite(s) || s < 0) return '0:00'
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = Math.floor(s % 60)
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : `${m}:${String(sec).padStart(2, '0')}`
}
