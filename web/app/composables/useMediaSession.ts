// Browser implementation of Heya's system-media adapter. HeyaClient uses the
// native protocol instead, preventing a WebView Media Session and the native
// OS session from both claiming the same hardware-key event.
import type { usePlayerBindings } from '~/composables/usePlayer'
import { systemMediaItemKey, systemMediaNotificationBody } from '~/utils/systemMedia'

type PlayerBindings = ReturnType<typeof usePlayerBindings>

function resolveArtworkUrl(src: string | null | undefined): string | null {
  if (!src) return null
  if (src.startsWith('/')) return window.location.origin + src
  if (src.startsWith('http://') || src.startsWith('https://')) return src
  return null
}

/** Install browser Media Session metadata, timeline, and transport handlers. */
export function installBrowserMediaSession(player: PlayerBindings): () => void {
  if (!('mediaSession' in navigator)) return () => {}

  const ms = navigator.mediaSession
  const stops: Array<() => void> = []

  function seekToSeconds(seconds: number) {
    const dur = player.duration.value
    if (dur > 0) player.seek(Math.max(0, Math.min(dur, seconds)) / dur)
  }

  const actions: [MediaSessionAction, MediaSessionActionHandler][] = [
    ['play', () => { void player.play() }],
    ['pause', () => player.pause()],
    ['previoustrack', () => { void player.prevTrack() }],
    ['nexttrack', () => { void player.nextTrack() }],
    ['seekto', (details) => { if (details.seekTime != null) seekToSeconds(details.seekTime) }],
    ['seekbackward', details => seekToSeconds(player.position.value - (details.seekOffset ?? 10))],
    ['seekforward', details => seekToSeconds(player.position.value + (details.seekOffset ?? 10))],
  ]
  for (const [action, handler] of actions) {
    try { ms.setActionHandler(action, handler) } catch { /* action unsupported here */ }
  }

  let lastMetadataKey: string | null = null
  stops.push(watch(() => player.currentTrack.value, (track) => {
    if (!track) {
      ms.metadata = null
      ms.playbackState = 'none'
      lastMetadataKey = null
      return
    }
    const key = systemMediaItemKey(track)
    if (key === lastMetadataKey) return
    lastMetadataKey = key

    const base = {
      title: track.title,
      artist: track.artist || undefined,
      album: track.album || undefined,
    }
    const artwork = resolveArtworkUrl(track.poster)
    ms.metadata = new MediaMetadata(artwork
      ? {
          ...base,
          artwork: [
            { src: artwork, sizes: '256x256' },
            { src: artwork, sizes: '512x512' },
          ],
        }
      : base)
  }, { immediate: true, deep: true }))

  stops.push(watch(() => player.playing.value, (playing) => {
    ms.playbackState = player.currentTrack.value ? (playing ? 'playing' : 'paused') : 'none'
  }, { immediate: true }))

  stops.push(watchEffect(() => {
    const duration = player.duration.value
    const position = player.position.value
    if (duration <= 0 || !Number.isFinite(duration) || !Number.isFinite(position)) return
    try {
      ms.setPositionState({
        duration,
        position: Math.max(0, Math.min(position, duration)),
        playbackRate: 1,
      })
    } catch { /* browsers reject transiently inconsistent position state */ }
  }))

  return () => {
    for (const stop of stops) stop()
    for (const [action] of actions) {
      try { ms.setActionHandler(action, null) } catch { /* unsupported action */ }
    }
    ms.metadata = null
    ms.playbackState = 'none'
  }
}

export function browserTrackNotificationsSupported(): boolean {
  return import.meta.client && 'Notification' in window
}

export async function requestBrowserTrackNotificationPermission(): Promise<NotificationPermission> {
  if (!browserTrackNotificationsSupported()) return 'denied'
  if (Notification.permission !== 'default') return Notification.permission
  return await Notification.requestPermission()
}

/**
 * Show one replaceable, silent notification. The caller owns change detection;
 * this adapter owns browser permission and background eligibility.
 */
export async function showBrowserTrackNotification(track: NonNullable<PlayerBindings['currentTrack']['value']>): Promise<boolean> {
  if (!browserTrackNotificationsSupported()
    || Notification.permission !== 'granted'
    || (document.visibilityState === 'visible' && document.hasFocus())) return false

  const icon = resolveArtworkUrl(track.poster) ?? undefined
  const options: NotificationOptions = {
    body: systemMediaNotificationBody(track),
    icon,
    tag: 'heya-now-playing',
    silent: true,
    data: { url: '/music' },
  }

  // Persistent notifications are required by some mobile browsers and keep
  // working while an installed PWA is backgrounded. The imported SW click
  // handler focuses the existing Heya client rather than opening duplicates.
  if ('serviceWorker' in navigator) {
    try {
      const registration = await navigator.serviceWorker.getRegistration()
      if (registration) {
        await registration.showNotification(track.title, options)
        return true
      }
    } catch { /* fall through to the page-scoped constructor */ }
  }

  try {
    const notification = new Notification(track.title, options)
    notification.onclick = () => {
      window.focus()
      void navigateTo('/music')
      notification.close()
    }
    return true
  } catch {
    return false
  }
}
