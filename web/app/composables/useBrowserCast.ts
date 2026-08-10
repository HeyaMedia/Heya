// Google Cast Web Sender bridge. Chrome owns mDNS, the receiver picker, and
// the control channel; Heya only supplies narrowly scoped media URLs.
const BROWSER_CAST_DEVICE_ID = 'browser:chromecast'
const CAST_SDK = 'https://www.gstatic.com/cv/js/sender/v1/cast_sender.js?loadCastFramework=1'

type BrowserCastListener = (event: 'state' | 'ended' | 'failed') => void
let sdkPromise: Promise<boolean> | null = null
let listener: BrowserCastListener | null = null
let mediaSession: any = null
let stopping = false

function castWindow(): any { return window as any }

export function browserCastSupported() {
  return import.meta.client && /Chrome|Chromium|Edg\//.test(navigator.userAgent) && !/CriOS/.test(navigator.userAgent)
}

export function useBrowserCast() {
  async function initialize(): Promise<boolean> {
    if (!browserCastSupported()) return false
    if (sdkPromise) return sdkPromise
    sdkPromise = new Promise<boolean>((resolve) => {
      const w = castWindow()
      const previous = w.__onGCastApiAvailable
      w.__onGCastApiAvailable = (available: boolean) => {
        previous?.(available)
        if (!available) { resolve(false); return }
        try {
          w.cast.framework.CastContext.getInstance().setOptions({
            receiverApplicationId: w.chrome.cast.media.DEFAULT_MEDIA_RECEIVER_APP_ID,
            autoJoinPolicy: w.chrome.cast.AutoJoinPolicy.ORIGIN_SCOPED,
          })
          resolve(true)
        } catch { resolve(false) }
      }
      if (w.cast?.framework && w.chrome?.cast) { w.__onGCastApiAvailable(true); return }
      const existing = document.querySelector(`script[src="${CAST_SDK}"]`)
      if (!existing) {
        const script = document.createElement('script')
        script.src = CAST_SDK
        script.async = true
        script.onerror = () => resolve(false)
        document.head.appendChild(script)
      }
    })
    return sdkPromise
  }

  async function requestSession(): Promise<string> {
    if (!await initialize()) throw new Error('Google Cast is not available in this browser')
    const w = castWindow()
    await w.cast.framework.CastContext.getInstance().requestSession()
    return deviceName()
  }

  function session(): any {
    return castWindow().cast?.framework?.CastContext.getInstance().getCurrentSession()
  }

  function deviceName(): string {
    return session()?.getCastDevice?.().friendlyName || 'Nearby Chromecast'
  }

  async function load(grant: any, startSeconds = 0, autoplay = true) {
    stopping = false
    const w = castWindow()
    const castSession = session()
    if (!castSession) throw new Error('Choose a Chromecast first')
    const info = new w.chrome.cast.media.MediaInfo(grant.url, grant.content_type)
    info.metadata = new w.chrome.cast.media.GenericMediaMetadata()
    info.metadata.title = grant.title || 'Heya'
    info.metadata.subtitle = [grant.artist, grant.album].filter(Boolean).join(' · ')
    info.streamType = w.chrome.cast.media.StreamType.BUFFERED
    if (grant.duration_sec > 0) info.duration = grant.duration_sec
    if (grant.text_track?.url) {
      const track = new w.chrome.cast.media.Track(1, w.chrome.cast.media.TrackType.TEXT)
      track.trackContentId = grant.text_track.url
      track.trackContentType = 'text/vtt'
      track.name = grant.text_track.name || 'Subtitles'
      track.language = grant.text_track.language || ''
      track.subtype = w.chrome.cast.media.TextTrackType.SUBTITLES
      info.tracks = [track]
    }
    const request = new w.chrome.cast.media.LoadRequest(info)
    request.currentTime = Math.max(0, startSeconds)
    request.autoplay = autoplay
    if (grant.text_track?.url) request.activeTrackIds = [1]
    mediaSession = await castSession.loadMedia(request)
    mediaSession.addUpdateListener((alive: boolean) => {
      if (!alive) {
        if (stopping) { stopping = false; mediaSession = null; return }
        const idle = mediaSession?.idleReason
        listener?.(idle === w.chrome.cast.media.IdleReason.FINISHED ? 'ended' : 'failed')
        mediaSession = null
      } else listener?.('state')
    })
    listener?.('state')
    return snapshot()
  }

  function snapshot() {
    const state = String(mediaSession?.playerState || '').toLowerCase()
    return {
      name: deviceName(),
      state: state === 'buffering' ? 'starting' : state,
      position: Number(mediaSession?.currentTime || 0),
      volume: Math.round(Number(session()?.getVolume?.() ?? 0.3) * 100),
    }
  }

  async function pause() { mediaSession?.pause?.(null, () => {}, () => {}); listener?.('state') }
  async function resume() { mediaSession?.play?.(null, () => {}, () => {}); listener?.('state') }
  async function seek(seconds: number) {
    const w = castWindow(); if (!mediaSession) return
    const request = new w.chrome.cast.media.SeekRequest(); request.currentTime = Math.max(0, seconds)
    mediaSession.seek(request, () => listener?.('state'), () => listener?.('failed'))
  }
  async function setVolume(level: number) { await session()?.setVolume?.(Math.max(0, Math.min(1, level / 100))); listener?.('state') }
  async function stop() { stopping = true; session()?.endSession?.(true); mediaSession = null }
  function onEvent(fn: BrowserCastListener | null) { listener = fn }

  return { initialize, requestSession, load, snapshot, pause, resume, seek, setVolume, stop, onEvent, deviceName }
}

export { BROWSER_CAST_DEVICE_ID }
