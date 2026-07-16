import { effectScope, onScopeDispose, readonly, shallowRef, type EffectScope } from 'vue'
import { getClientSurface } from '~/composables/useClientSurface'
import {
  installBrowserMediaSession,
  showBrowserTrackNotification,
} from '~/composables/useMediaSession'
import { waitForSystemMediaBridge } from '~/composables/useSystemMediaBridge'
import type {
  HeyaSystemMediaBridge,
  SystemMediaArtwork,
  SystemMediaCapabilities,
  SystemMediaCommand,
  SystemMediaSnapshot,
} from '~/types/system-media'
import {
  clampSystemMediaPosition,
  systemMediaArtworkKey,
  systemMediaItemKey,
} from '~/utils/systemMedia'

type ActiveSystemMediaAdapter = 'initializing' | 'browser' | 'native' | 'unavailable'

const activeAdapter = shallowRef<ActiveSystemMediaAdapter>('initializing')
const nativeCapabilities = shallowRef<SystemMediaCapabilities | null>(null)
let started = false
let adapterScope: EffectScope | null = null

const NATIVE_ARTWORK_EDGE = 512
const NATIVE_ARTWORK_MAX_BYTES = 512 * 1024
const artworkCache = new Map<string, Promise<SystemMediaArtwork | null>>()

function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error ?? new Error('Could not read artwork'))
    reader.onload = () => {
      const result = typeof reader.result === 'string' ? reader.result : ''
      resolve(result.slice(result.indexOf(',') + 1))
    }
    reader.readAsDataURL(blob)
  })
}

function canvasBlob(canvas: HTMLCanvasElement, quality: number): Promise<Blob | null> {
  return new Promise(resolve => canvas.toBlob(resolve, 'image/jpeg', quality))
}

async function encodeNativeArtwork(source: string): Promise<SystemMediaArtwork | null> {
  const cached = artworkCache.get(source)
  if (cached) return await cached

  const pending = (async () => {
    try {
      const url = new URL(source, window.location.href)
      if (url.origin !== window.location.origin || !url.pathname.startsWith('/api/')) return null
      const response = await fetch(url, { credentials: 'same-origin' })
      if (!response.ok) return null
      const sourceBlob = await response.blob()
      if (!sourceBlob.type.startsWith('image/')) return null

      const bitmap = await createImageBitmap(sourceBlob)
      const sourceEdge = Math.min(bitmap.width, bitmap.height)
      if (sourceEdge <= 0) {
        bitmap.close()
        return null
      }
      const targetEdge = Math.min(NATIVE_ARTWORK_EDGE, sourceEdge)
      const canvas = document.createElement('canvas')
      canvas.width = targetEdge
      canvas.height = targetEdge
      const context = canvas.getContext('2d')
      if (!context) {
        bitmap.close()
        return null
      }
      const sourceX = Math.max(0, (bitmap.width - sourceEdge) / 2)
      const sourceY = Math.max(0, (bitmap.height - sourceEdge) / 2)
      context.drawImage(bitmap, sourceX, sourceY, sourceEdge, sourceEdge, 0, 0, targetEdge, targetEdge)
      bitmap.close()

      let encoded = await canvasBlob(canvas, 0.82)
      if (encoded && encoded.size > NATIVE_ARTWORK_MAX_BYTES) encoded = await canvasBlob(canvas, 0.62)
      if (!encoded || encoded.size > NATIVE_ARTWORK_MAX_BYTES) return null
      return {
        cacheKey: systemMediaArtworkKey(source),
        mimeType: 'image/jpeg' as const,
        base64Data: await blobToBase64(encoded),
      }
    } catch {
      return null
    }
  })()

  artworkCache.set(source, pending)
  while (artworkCache.size > 24) artworkCache.delete(artworkCache.keys().next().value!)
  return await pending
}

function seekTo(player: ReturnType<typeof usePlayerBindings>, seconds: number) {
  const duration = player.duration.value
  if (duration > 0) player.seek(Math.max(0, Math.min(duration, seconds)) / duration)
}

function handleNativeCommand(player: ReturnType<typeof usePlayerBindings>, command: SystemMediaCommand) {
  switch (command.type) {
    case 'play': void player.play(); break
    case 'pause': player.pause(); break
    case 'togglePlayPause': void player.togglePlay(); break
    case 'previous': void player.prevTrack(); break
    case 'next': void player.nextTrack(); break
    // Heya's stop action deliberately clears the queue, which is more
    // destructive than an OS Stop button implies. Preserve the queue here.
    case 'stop': player.pause(); break
    case 'seekTo': seekTo(player, command.positionSeconds); break
    case 'seekBy': seekTo(player, player.position.value + command.offsetSeconds); break
  }
}

function installBrowserAdapter(player: ReturnType<typeof usePlayerBindings>) {
  adapterScope = effectScope()
  adapterScope.run(() => {
    onScopeDispose(installBrowserMediaSession(player))
    const { settings } = useDeviceSettings()
    let previousItemKey: string | null = null
    let pendingNotificationKey: string | null = null

    function notifyIfReady() {
      const track = player.currentTrack.value
      if (!track || !player.playing.value || !pendingNotificationKey) return
      if (systemMediaItemKey(track) !== pendingNotificationKey) return
      pendingNotificationKey = null
      if (settings.value.trackChangeNotifications) void showBrowserTrackNotification(track)
    }

    watch(() => player.currentTrack.value, (track) => {
      if (!track) {
        previousItemKey = null
        pendingNotificationKey = null
        return
      }
      const itemKey = systemMediaItemKey(track)
      if (previousItemKey && itemKey !== previousItemKey) pendingNotificationKey = itemKey
      previousItemKey = itemKey
      notifyIfReady()
    }, { immediate: true, deep: true })
    watch(() => player.playing.value, notifyIfReady)
  })
  activeAdapter.value = 'browser'
}

function installNativeAdapter(
  player: ReturnType<typeof usePlayerBindings>,
  bridge: Readonly<HeyaSystemMediaBridge>,
  capabilities: SystemMediaCapabilities,
) {
  adapterScope = effectScope()
  adapterScope.run(() => {
    let revision = 0
    let trackGeneration = 0
    let previousItemKey: string | null = null
    let pendingNotificationKey: string | null = null
    let lastPositionSent = -1
    let lastCommandSequence = 0

    const unsubscribeCommands = bridge.subscribeSystemMediaCommands((command) => {
      if (command.commandSequence <= lastCommandSequence) return
      lastCommandSequence = command.commandSequence
      handleNativeCommand(player, command)
    })
    onScopeDispose(unsubscribeCommands)

    function snapshot(artwork?: SystemMediaArtwork): SystemMediaSnapshot | null {
      const track = player.currentTrack.value
      if (!track) return null
      return {
        revision: ++revision,
        itemKey: systemMediaItemKey(track),
        title: track.title,
        artist: track.artist || undefined,
        album: track.album || undefined,
        durationSeconds: Math.max(0, Number.isFinite(player.duration.value) ? player.duration.value : 0),
        positionSeconds: clampSystemMediaPosition(player.position.value, player.duration.value),
        playbackState: player.playing.value ? 'playing' : 'paused',
        canGoPrevious: player.hasPrevious.value,
        canGoNext: player.hasNext.value,
        canSeek: player.duration.value > 0 && !track.isStream,
        artwork,
      }
    }

    async function publish(artwork?: SystemMediaArtwork): Promise<SystemMediaSnapshot | null> {
      const next = snapshot(artwork)
      if (!next) {
        await bridge.clearSystemMedia({ revision: ++revision }).catch(() => {})
        return null
      }
      lastPositionSent = next.positionSeconds
      await bridge.updateSystemMedia(next).catch(() => {})
      return next
    }

    async function notifyIfReady() {
      const track = player.currentTrack.value
      if (!track || !player.playing.value || !pendingNotificationKey) return
      const itemKey = systemMediaItemKey(track)
      if (itemKey !== pendingNotificationKey) return
      pendingNotificationKey = null
      if (!capabilities.trackNotifications) return
      const published = await publish()
      if (published?.itemKey === itemKey) {
        await bridge.notifyTrackChanged({ revision: published.revision, itemKey }).catch(() => {})
      }
    }

    watch(() => player.currentTrack.value, (track) => {
      const generation = ++trackGeneration
      if (!track) {
        previousItemKey = null
        pendingNotificationKey = null
        void publish()
        return
      }

      const itemKey = systemMediaItemKey(track)
      if (previousItemKey && itemKey !== previousItemKey) pendingNotificationKey = itemKey
      previousItemKey = itemKey
      void publish().then(() => notifyIfReady())

      if (capabilities.artwork && track.poster) {
        void encodeNativeArtwork(track.poster).then((artwork) => {
          const current = player.currentTrack.value
          if (artwork && current && generation === trackGeneration && systemMediaItemKey(track) === systemMediaItemKey(current)) {
            void publish(artwork)
          }
        })
      }
    }, { immediate: true, deep: true })

    watch(() => player.playing.value, () => {
      void publish().then(() => notifyIfReady())
    })
    watch([() => player.duration.value, () => player.hasPrevious.value, () => player.hasNext.value], () => {
      void publish()
    })
    watch(() => player.position.value, (position) => {
      // OS timelines extrapolate while playing. Refresh every ~5 seconds and
      // immediately after any larger seek without sending 4 Hz IPC traffic.
      if (Math.abs(position - lastPositionSent) >= 5) void publish()
    })
  })

  nativeCapabilities.value = capabilities
  activeAdapter.value = 'native'
}

export function useSystemMediaIntegration() {
  if (import.meta.server || started) return
  started = true
  const player = usePlayerBindings()

  if (getClientSurface() !== 'tauri') {
    installBrowserAdapter(player)
    return
  }

  void waitForSystemMediaBridge().then((handshake) => {
    if (handshake?.capabilities.available
      && (handshake.capabilities.nowPlaying || handshake.capabilities.mediaCommands)) {
      installNativeAdapter(player, handshake.bridge, handshake.capabilities)
    } else if ('mediaSession' in navigator) {
      installBrowserAdapter(player)
    } else {
      activeAdapter.value = 'unavailable'
    }
  })
}

export function useSystemMediaStatus() {
  return {
    activeAdapter: readonly(activeAdapter),
    nativeCapabilities: readonly(nativeCapabilities),
  }
}
