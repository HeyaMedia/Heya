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
  })

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
    v.addEventListener('playing', () => { state.buffering = false; state.loading = false })
    v.addEventListener('progress', syncState)
    v.addEventListener('error', () => {
      const e = v.error
      if (!e) return
      const codes: Record<number, string> = { 1: 'Aborted', 2: 'Network error', 3: 'Decode error', 4: 'Source not supported' }
      state.error = `${codes[e.code] || 'Error'}${e.message ? ` — ${e.message}` : ''}`
    })
  }

  function loadSource(src: string, token?: string) {
    destroyHLS()
    const v = videoRef.value
    if (!v) return

    state.error = null
    state.loading = true
    state.ended = false

    const isHLS = src.includes('.m3u8')

    if (isHLS && Hls.isSupported()) {
      hls = new Hls({
        maxBufferLength: 30,
        maxMaxBufferLength: 60,
        ...(token ? {
          xhrSetup(xhr: XMLHttpRequest) {
            xhr.setRequestHeader('Authorization', `Bearer ${token}`)
          },
        } : {}),
      })
      hls.loadSource(src)
      hls.attachMedia(v)
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (data.fatal) state.error = `HLS: ${data.type} - ${data.details}`
      })
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        v.play().catch(() => {})
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
    nextTick(() => bindEvents())
    document.addEventListener('fullscreenchange', () => { state.fullscreen = !!document.fullscreenElement })
  })

  onUnmounted(() => { destroyHLS() })

  return { state: readonly(state) as HeyaPlayerState, controls, loadSource, destroyHLS }
}

export function formatTime(s: number): string {
  if (!isFinite(s) || s < 0) return '0:00'
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = Math.floor(s % 60)
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : `${m}:${String(sec).padStart(2, '0')}`
}
