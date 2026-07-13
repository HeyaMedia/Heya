// App-lifetime bridge between the full-screen VideoPlayer and HeyaConnect.
// The player registers its live snapshot + controls while mounted; the
// device-remote plugin publishes that snapshot and routes commands from other
// Heya clients back into the player. Keeping this transport-neutral avoids
// teaching VideoPlayer about WebSocket command envelopes.

export interface VideoRendererSnapshot {
  session_id: string
  media_kind: 'video'
  state: 'starting' | 'playing' | 'paused' | 'stopped'
  file_id: string
  media_item_id?: number
  entity_type: 'movie' | 'episode'
  entity_id: number
  title: string
  audio_track: number
  subtitle_track?: number
  quality: string
  position_sec: number
  duration_sec: number
  volume: number
}

export interface VideoRendererController {
  snapshot: () => VideoRendererSnapshot | null
  pause: () => void | Promise<void>
  resume: () => void | Promise<void>
  seek: (seconds: number) => void | Promise<void>
  volume: (level: number) => void | Promise<void>
  audio: (track: number) => void | Promise<void>
  subtitle: (track: number | null) => void | Promise<void>
  quality: (quality: string) => void | Promise<void>
  next: () => void | Promise<void>
  stop: () => void | Promise<void>
}

const activeController = shallowRef<VideoRendererController | null>(null)
const revision = ref(0)
let activeRegistration = 0

export function useVideoRenderer() {
  const snapshot = computed(() => {
    revision.value
    return activeController.value?.snapshot() ?? null
  })

  function attach(controller: VideoRendererController) {
    const id = ++activeRegistration
    activeController.value = controller
    revision.value++
    return () => {
      if (activeRegistration !== id) return
      activeController.value = null
      activeRegistration++
      revision.value++
    }
  }

  async function execute(action: string, args: Record<string, unknown> = {}) {
    const controller = activeController.value
    if (!controller) return false
    switch (action) {
      case 'pause': await controller.pause(); break
      case 'resume': await controller.resume(); break
      case 'seek': await controller.seek(Number(args.seconds ?? 0)); break
      case 'volume': await controller.volume(Number(args.level ?? 0)); break
      case 'audio': await controller.audio(Number(args.track ?? 0)); break
      case 'subtitle': {
        const track = args.track == null ? null : Number(args.track)
        await controller.subtitle(track != null && track >= 0 ? track : null)
        break
      }
      case 'quality': await controller.quality(String(args.quality ?? 'auto')); break
      case 'next': await controller.next(); break
      case 'stop': await controller.stop(); break
      default: return false
    }
    revision.value++
    return true
  }

  return { snapshot, attach, execute }
}
