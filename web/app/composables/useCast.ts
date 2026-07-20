import { acceptHMRUpdate, defineStore } from 'pinia'
import type { MediaDetail, StreamInfoResponse } from '~~/shared/types'
import { withAuthHeaders } from '~/composables/useAuth'

// Server-side casting (docs/cast-plan.md Phase 2). The SERVER is the player:
// it pushes or exposes scoped media to the receiver and owns the session; this store is the
// client's mirror of that session plus a thin control surface over
// /api/cast/*. usePlayer routes its transport actions here while an output
// is engaged, so the playbar keeps working unchanged — same buttons, remote
// speaker.
//
// Two lifetimes, deliberately separate:
//   - `engagedDeviceId` is the user's chosen output. It survives across
//     per-track sessions (the server removes a session when its track ends;
//     the next queue advance creates a fresh one on the same device) and
//     only clears on explicit disconnect or a session failure.
//   - `session` is the live server session snapshot, fed by the cast.state
//     WS events (plugins/cast-live.client.ts) and the REST responses.

export interface CastDevice {
  id: string
  provider: string
  capabilities?: string[]
  name: string
  model?: string
  manufacturer?: string
  host?: string
  addr?: string
  port?: number
  media_origin?: string
  last_seen?: string
  kind?: string
  state?: Record<string, unknown>
}

export interface VideoQueueItem {
  fileId: string
  mediaItemId: number
  entityType: 'episode'
  entityId: number
  title: string
  episodeLabel: string
  runtimeSeconds: number
}

export interface CastSession {
  id: string
  device_id: string
  device_name: string
  user_id: number
  state: string // starting | playing | paused | stopped | failed
  media_kind?: 'audio' | 'video'
  track_id?: number
  file_id?: string
  media_item_id?: number
  entity_type?: 'movie' | 'episode'
  entity_id?: number
  title?: string
  artist?: string
  album?: string
  audio_track?: number
  subtitle_track?: number
  quality?: string
  duration_sec?: number
  position_sec: number
  volume: number
  updated_at?: string
}

// Shape of the cast.state WS payload (internal/eventhub CastStatePayload).
export interface CastStateEvent {
  session_id: string
  device_id: string
  device_name: string
  user_id: number
  state: string
  media_kind?: 'audio' | 'video'
  track_id?: number
  file_id?: string
  media_item_id?: number
  entity_type?: 'movie' | 'episode'
  entity_id?: number
  title?: string
  artist?: string
  audio_track?: number
  subtitle_track?: number
  quality?: string
  position_sec: number
  duration_sec?: number
  volume: number
  at: string
}

const VOLUME_DEBOUNCE_MS = 200

export const useCastStore = defineStore('cast', () => {
  const { token } = useAuth()
  const devices = ref<CastDevice[]>([])
  const devicesLoaded = ref(false)
  const session = ref<CastSession | null>(null)
  const engagedDeviceId = ref<string | null>(null)
  // True while the play POST is in flight so the UI can show a connecting
  // state before the first WS event lands.
  const connecting = ref(false)
  const videoStreamInfo = shallowRef<StreamInfoResponse | null>(null)
  const videoStreamInfoFileID = ref('')
  const videoStreamInfoLoading = ref(false)
  const videoStreamInfoError = ref('')
  const videoRemoteOpen = ref(false)
  const videoQueue = ref<VideoQueueItem[]>([])
  const videoQueueLoading = ref(false)
  let videoInfoRequest = 0
  let videoQueueRequest = 0
  let videoQueueKey = ''
  let lastVideoSession: CastSession | null = null

  const engaged = computed(() => engagedDeviceId.value !== null)
  const deviceName = computed(() => {
    if (session.value) return session.value.device_name
    const d = devices.value.find((d) => d.id === engagedDeviceId.value)
    return d?.name ?? ''
  })

  // Position interpolation base: the server only emits on state edges (no
  // 1 Hz ticks), so the FE advances the clock itself from the last sample.
  // Client receive time, not the payload's `at` — no cross-clock skew.
  let positionBaseSec = 0
  let positionSampledAt = 0
  function samplePosition(sec: number) {
    positionBaseSec = sec
    positionSampledAt = Date.now()
  }
  function livePositionSec(): number {
    if (!session.value) return 0
    let pos = positionBaseSec
    if (session.value.state === 'playing') {
      pos += (Date.now() - positionSampledAt) / 1000
    }
    const dur = session.value.duration_sec ?? 0
    if (dur > 0 && pos > dur) pos = dur
    return pos
  }

  function resetVideoStreamInfo(fileID = '') {
    if (videoStreamInfoFileID.value === fileID && videoStreamInfo.value) return
    videoInfoRequest++
    videoStreamInfoFileID.value = fileID
    videoStreamInfo.value = null
    videoStreamInfoLoading.value = false
    videoStreamInfoError.value = ''
  }

  async function loadVideoStreamInfo(fileID = session.value?.file_id ?? '') {
    if (!fileID) return null
    if (videoStreamInfoFileID.value === fileID && videoStreamInfo.value) return videoStreamInfo.value
    if (videoStreamInfoFileID.value === fileID && videoStreamInfoLoading.value) return null
    resetVideoStreamInfo(fileID)
    const request = ++videoInfoRequest
    videoStreamInfoLoading.value = true
    try {
      const url = `/api/stream/${encodeURIComponent(fileID)}/info`
      const info = await $fetch<StreamInfoResponse>(url, {
        headers: withAuthHeaders(url),
      })
      if (request !== videoInfoRequest) return null
      videoStreamInfo.value = info
      return info
    } catch (error) {
      if (request === videoInfoRequest) {
        videoStreamInfoError.value = error instanceof Error ? error.message : 'Could not load video controls'
      }
      return null
    } finally {
      if (request === videoInfoRequest) videoStreamInfoLoading.value = false
    }
  }

  // Advance ownership: only the tab that started the current cast play
  // drives the queue when a track ends naturally, so two tabs belonging to
  // the same user don't both fire the next track (WS events are per-user).
  // A foreign takeover is detected by a track we never requested appearing.
  let ownsPlayback = false
  let lastRequestedMediaKey = ''

  // The device stream volume we last knew. The server removes a session
  // when its track ends, so the next queue advance creates a NEW session —
  // reusing this keeps a mid-queue volume tweak sticky across tracks. Null
  // until the first session reports in; then the first engage caps the
  // handoff at a modest level so a loud local slider never blasts the room.
  const lastDeviceVolume = ref<number | null>(null)

  async function refreshDevices() {
    const { $heya } = useNuxtApp()
    // HeyaConnect devices are user-private and independent of server-side
    // casting permission. Fetch the two sources separately so a 403 from the
    // cast allowlist never hides the user's own Heya clients.
    const [castResult, clientResult] = await Promise.allSettled([
      $heya('/api/cast/devices') as Promise<{ items?: CastDevice[] | null }>,
      ($heya as any)('/api/me/devices') as Promise<{ items?: Array<{
        id: string
        name: string
        kind: string
        capabilities?: string[]
        state?: Record<string, unknown>
        last_seen: string
      }> }>,
    ])
    const castDevices = castResult.status === 'fulfilled' ? (castResult.value.items ?? []) : []
    const clients = clientResult.status === 'fulfilled' ? (clientResult.value.items ?? []) : []
    devices.value = [
      ...clients.filter(d => d.id !== clientDeviceID()).map(d => normalizeClientDevice({ ...d, provider: 'client' })),
      ...castDevices,
    ]
    devicesLoaded.value = true
    const selectedClient = devices.value.find(d => d.id === engagedDeviceId.value && d.provider === 'client')
    if (selectedClient) applyClientDeviceState(selectedClient)
  }

  const isClientDevice = computed(() => engagedDeviceId.value?.startsWith('client:') ?? false)

  function normalizeClientDevice(device: CastDevice): CastDevice {
    const declared = device.capabilities ?? []
    return {
      ...device,
      provider: 'client',
      capabilities: [
        ...(declared.includes('audio') || declared.includes('playback.local.audio') || declared.includes('play') ? ['audio'] : []),
        ...(declared.includes('video') || declared.includes('playback.local.video') ? ['video'] : []),
      ],
    }
  }
  async function clientCommand(action: string, args?: Record<string, unknown>) {
    const id = engagedDeviceId.value
    if (!id) return
    const { $heya } = useNuxtApp()
    await ($heya as any)('/api/me/devices/{id}/command', { method: 'POST', path: { id }, body: { action, args } })
  }

  function sessionFromClientDevice(device: CastDevice): CastSession | null {
    const state = device.state
    if (!state || state.media_kind !== 'video' || state.state === 'stopped') return null
    const fileID = String(state.file_id ?? '')
    if (!fileID) return null
    return {
      id: String(state.session_id ?? `${device.id}:${fileID}`),
      device_id: device.id,
      device_name: device.name,
      user_id: 0,
      state: String(state.state ?? 'paused'),
      media_kind: 'video',
      file_id: fileID,
      media_item_id: Number(state.media_item_id ?? 0) || undefined,
      entity_type: state.entity_type === 'episode' ? 'episode' : 'movie',
      entity_id: Number(state.entity_id ?? state.media_item_id ?? 0),
      title: String(state.title ?? 'Video'),
      audio_track: Number(state.audio_track ?? 0),
      subtitle_track: state.subtitle_track == null ? undefined : Number(state.subtitle_track),
      quality: String(state.quality ?? 'auto'),
      duration_sec: Number(state.duration_sec ?? 0),
      position_sec: Number(state.position_sec ?? 0),
      volume: Number(state.volume ?? 100),
      updated_at: new Date().toISOString(),
    }
  }

  function applyClientDeviceState(device: CastDevice) {
    const normalized = normalizeClientDevice(device)
    const index = devices.value.findIndex(d => d.id === normalized.id)
    if (index >= 0) devices.value[index] = { ...devices.value[index], ...normalized }
    else devices.value.push(normalized)
    if (engagedDeviceId.value !== normalized.id) return
    const next = sessionFromClientDevice(normalized)
    if (!next) {
      if (session.value?.device_id === normalized.id) session.value = null
      return
    }
    session.value = next
    lastVideoSession = next
    lastDeviceVolume.value = next.volume
    samplePosition(next.position_sec)
    if (next.file_id && videoStreamInfoFileID.value !== next.file_id) resetVideoStreamInfo(next.file_id)
    if (next.entity_type === 'episode' && next.media_item_id) {
      void loadVideoQueue(next.media_item_id, next.entity_id ?? 0)
    }
  }

  function engageClientDevice(deviceID: string) {
    engagedDeviceId.value = deviceID
    const device = devices.value.find(d => d.id === deviceID)
    if (device) applyClientDeviceState(device)
  }

  function releaseDevice() {
    session.value = null
    engagedDeviceId.value = null
    videoRemoteOpen.value = false
  }

  // Adopt a session that already exists server-side. Called at boot (page
  // load while the house is casting) and again after a WS reconnect, where
  // it doubles as the re-sync: a session that ended while we were offline
  // clears the stale mirror. Adoption does NOT take queue ownership — this
  // tab didn't start the playback.
  async function adoptExisting() {
    if (isClientDevice.value) {
      const device = devices.value.find(d => d.id === engagedDeviceId.value)
      if (device) applyClientDeviceState(device)
      return
    }
    const { $heya } = useNuxtApp()
    try {
      const res = await $heya('/api/cast/sessions') as { items?: CastSession[] | null }
      const live = (res.items ?? []).filter((s) => s.state !== 'stopped' && s.state !== 'failed')
      // Engaged: track our own device only. Fresh boot: adopt whatever runs.
      const s = engagedDeviceId.value
        ? live.find((s) => s.device_id === engagedDeviceId.value) ?? null
        : live[0] ?? null
      if (s) {
        session.value = s
        engagedDeviceId.value = s.device_id
        lastDeviceVolume.value = s.volume
        samplePosition(s.position_sec)
        if (s.media_kind === 'video' && s.file_id) resetVideoStreamInfo(s.file_id)
        if (s.media_kind === 'video' && s.entity_type === 'episode' && s.media_item_id) {
          void loadVideoQueue(s.media_item_id, s.entity_id ?? 0)
        }
      } else if (engagedDeviceId.value && !connecting.value) {
        session.value = null
      }
    } catch { /* not fatal — WS events will catch us up */ }
  }

  // Start (or retarget) playback on the engaged device. `fallbackVolume`
  // (the local slider) only matters before the device ever reported a
  // level, and is capped so the handoff starts polite.
  async function playTrack(trackId: number, fallbackVolume: number, startSeconds = 0) {
    const deviceId = engagedDeviceId.value
    if (!deviceId) throw new Error('no cast device engaged')
    if (deviceId.startsWith('client:')) {
      await clientCommand('play', { track_id: trackId, position_seconds: startSeconds })
      return
    }
    const { $heya } = useNuxtApp()
    connecting.value = true
    lastRequestedMediaKey = `audio:${trackId}`
    try {
      const snap = await $heya('/api/cast/sessions', {
        method: 'POST',
        body: {
          device_id: deviceId,
          track_id: trackId,
          volume: lastDeviceVolume.value ?? Math.min(Math.max(Math.round(fallbackVolume), 0), 30),
          start_seconds: Math.max(0, Math.floor(startSeconds)),
        },
      }) as CastSession
      session.value = snap
      lastDeviceVolume.value = snap.volume
      samplePosition(snap.position_sec)
      ownsPlayback = true
    } finally {
      connecting.value = false
    }
  }

  // Video shares one controller shape across server-owned Cast sessions and
  // HeyaConnect renderers. Chromecast gets a scoped URL-pull session; a Heya
  // device gets a typed play_video command and reports the same fields back
  // through device.state.
  async function playVideo(input: {
    fileId: string | number
    mediaItemId?: number
    entityType: 'movie' | 'episode'
    entityId: number
    title?: string
    audioTrack?: number
    subtitleTrack?: number
    quality?: string
    fallbackVolume: number
    startSeconds?: number
    startPaused?: boolean
  }) {
    const deviceId = engagedDeviceId.value
    if (!deviceId) throw new Error('no cast device engaged')
    if (deviceId.startsWith('client:')) {
      const device = devices.value.find(d => d.id === deviceId)
      connecting.value = true
      lastRequestedMediaKey = `video:${String(input.fileId)}:${input.entityType}:${input.entityId}`
      try {
        await clientCommand('play_video', {
          file_id: String(input.fileId),
          media_item_id: input.mediaItemId ?? 0,
          entity_type: input.entityType,
          entity_id: input.entityId,
          title: input.title ?? '',
          position_seconds: Math.max(0, Math.floor(input.startSeconds ?? 0)),
        })
        const snap: CastSession = {
          id: `${deviceId}:${String(input.fileId)}`,
          device_id: deviceId,
          device_name: device?.name ?? 'Heya device',
          user_id: 0,
          state: input.startPaused ? 'paused' : 'starting',
          media_kind: 'video',
          file_id: String(input.fileId),
          media_item_id: input.mediaItemId,
          entity_type: input.entityType,
          entity_id: input.entityId,
          title: input.title,
          audio_track: input.audioTrack ?? 0,
          subtitle_track: input.subtitleTrack,
          quality: input.quality ?? 'auto',
          position_sec: Math.max(0, input.startSeconds ?? 0),
          volume: lastDeviceVolume.value ?? Math.min(Math.max(Math.round(input.fallbackVolume), 0), 30),
        }
        session.value = snap
        lastVideoSession = snap
        samplePosition(snap.position_sec)
        ownsPlayback = true
        if (snap.entity_type === 'episode' && snap.media_item_id) {
          void loadVideoQueue(snap.media_item_id, snap.entity_id ?? 0)
        } else {
          videoQueue.value = []
          videoQueueKey = ''
        }
        return snap
      } finally {
        connecting.value = false
      }
    }
    const { $heya } = useNuxtApp()
    connecting.value = true
    lastRequestedMediaKey = `video:${String(input.fileId)}:${input.entityType}:${input.entityId}`
    try {
      const body: Record<string, unknown> = {
        device_id: deviceId,
        file_id: String(input.fileId),
        entity_type: input.entityType,
        title: input.title ?? '',
        audio_track: Math.max(0, Math.floor(input.audioTrack ?? 0)),
        quality: input.quality ?? 'auto',
        volume: lastDeviceVolume.value ?? Math.min(Math.max(Math.round(input.fallbackVolume), 0), 30),
        start_seconds: Math.max(0, Math.floor(input.startSeconds ?? 0)),
        start_paused: input.startPaused ?? false,
      }
      if (input.entityId > 0) body.entity_id = input.entityId
      if (input.subtitleTrack != null && input.subtitleTrack >= 0) body.subtitle_track = Math.floor(input.subtitleTrack)
      const snap = await ($heya as any)('/api/cast/sessions', {
        method: 'POST',
        body,
      }) as CastSession
      session.value = snap
      lastVideoSession = snap
      lastRequestedMediaKey = `video:${snap.file_id ?? String(input.fileId)}:${snap.entity_type ?? input.entityType}:${snap.entity_id ?? input.entityId}`
      lastDeviceVolume.value = snap.volume
      samplePosition(snap.position_sec)
      ownsPlayback = true
      if (snap.entity_type === 'episode' && snap.media_item_id) {
        void loadVideoQueue(snap.media_item_id, snap.entity_id ?? 0)
      } else {
        videoQueue.value = []
        videoQueueKey = ''
      }
      return snap
    } finally {
      connecting.value = false
    }
  }

  // Reconfigure the active server-owned video session from any of the user's
  // clients. The receiver is reloaded at its live position because Google's
  // Default Media Receiver cannot switch embedded audio tracks in place.
  async function updateVideo(input: { audioTrack?: number, subtitleTrack?: number | null, quality?: string }) {
    const s = session.value
    if (!s || s.media_kind !== 'video' || !s.file_id || !s.entity_type || !s.entity_id) {
      throw new Error('no remote video session is active')
    }
    if (isClientDevice.value) {
      if (input.audioTrack !== undefined) await clientCommand('audio', { track: input.audioTrack })
      if (input.subtitleTrack !== undefined) await clientCommand('subtitle', { track: input.subtitleTrack })
      if (input.quality !== undefined) await clientCommand('quality', { quality: input.quality })
      session.value = {
        ...s,
        audio_track: input.audioTrack ?? s.audio_track,
        subtitle_track: input.subtitleTrack === undefined ? s.subtitle_track : (input.subtitleTrack ?? undefined),
        quality: input.quality ?? s.quality,
      }
      return session.value
    }
    return await playVideo({
      fileId: s.file_id,
      mediaItemId: s.media_item_id,
      entityType: s.entity_type,
      entityId: s.entity_id,
      title: s.title,
      audioTrack: input.audioTrack ?? s.audio_track ?? 0,
      subtitleTrack: input.subtitleTrack === undefined ? s.subtitle_track : (input.subtitleTrack ?? undefined),
      quality: input.quality ?? s.quality ?? 'auto',
      fallbackVolume: s.volume,
      startSeconds: livePositionSec(),
      startPaused: s.state === 'paused',
    })
  }

  async function postControl(id: string, verb: 'pause' | 'resume' | 'stop'): Promise<CastSession> {
    const { $heya } = useNuxtApp()
    const path = { id }
    if (verb === 'pause') return await $heya('/api/cast/sessions/{id}/pause', { method: 'POST', path }) as CastSession
    if (verb === 'resume') return await $heya('/api/cast/sessions/{id}/resume', { method: 'POST', path }) as CastSession
    return await $heya('/api/cast/sessions/{id}/stop', { method: 'POST', path }) as CastSession
  }

  async function pause() {
    if (isClientDevice.value) {
      if (session.value) {
        samplePosition(livePositionSec())
        session.value = { ...session.value, state: 'paused' }
      }
      await clientCommand('pause')
      return
    }
    const s = session.value
    if (!s) return
    // Optimistic: the WS event confirms the provider-specific pause path.
    samplePosition(livePositionSec())
    session.value = { ...s, state: 'paused' }
    const snap = await postControl(s.id, 'pause')
    session.value = snap
    samplePosition(snap.position_sec)
  }

  async function resume() {
    if (isClientDevice.value) {
      if (session.value) session.value = { ...session.value, state: 'playing' }
      samplePosition(session.value?.position_sec ?? 0)
      await clientCommand('resume')
      return
    }
    const s = session.value
    if (!s) return
    // AirPlay respawns at the frozen position; URL-pull providers resume the
    // existing receiver session. The REST/WS snapshots restore the truth.
    session.value = { ...s, state: 'starting' }
    const snap = await postControl(s.id, 'resume')
    session.value = snap
    samplePosition(snap.position_sec)
  }

  async function seekTo(seconds: number) {
    if (isClientDevice.value) {
      samplePosition(seconds)
      if (session.value) session.value = { ...session.value, position_sec: seconds }
      await clientCommand('seek', { seconds })
      return
    }
    const s = session.value
    if (!s) return
    samplePosition(seconds)
    const { $heya } = useNuxtApp()
    const snap = await $heya('/api/cast/sessions/{id}/seek', {
      method: 'POST',
      path: { id: s.id },
      body: { seconds: Math.max(0, Math.floor(seconds)) },
    }) as CastSession
    session.value = snap
    samplePosition(snap.position_sec)
  }

  // Volume drags fire per pixel — debounce the POST, apply optimistically.
  let volumeTimer: ReturnType<typeof setTimeout> | null = null
  function setVolume(level: number) {
    if (isClientDevice.value) {
      const clamped = Math.max(0, Math.min(100, Math.round(level)))
      if (session.value) session.value = { ...session.value, volume: clamped }
      lastDeviceVolume.value = clamped
      void clientCommand('volume', { level: clamped })
      return
    }
    const s = session.value
    if (!s) return
    const clamped = Math.max(0, Math.min(100, Math.round(level)))
    session.value = { ...s, volume: clamped }
    lastDeviceVolume.value = clamped
    if (volumeTimer) clearTimeout(volumeTimer)
    volumeTimer = setTimeout(() => {
      volumeTimer = null
      const cur = session.value
      if (!cur) return
      const { $heya } = useNuxtApp()
      void $heya('/api/cast/sessions/{id}/volume', {
        method: 'POST',
        path: { id: cur.id },
        body: { level: cur.volume },
      }).catch(() => { /* next WS event restores the true level */ })
    }, VOLUME_DEBOUNCE_MS)
  }

  // Stop the live session but keep the device engaged (playbar stop / end
  // of queue). Clearing ownership FIRST means the resulting 'stopped' WS
  // event reads as deliberate, not as a natural end to advance past.
  async function stopSession() {
    ownsPlayback = false
    if (isClientDevice.value) {
      session.value = null
      await clientCommand('stop')
      return
    }
    const s = session.value
    session.value = null
    if (!s) return
    try {
      await postControl(s.id, 'stop')
    } catch { /* already gone server-side */ }
  }

  // Full disconnect: stop the session (if any) and release the output.
  async function disconnect() {
    await stopSession()
    engagedDeviceId.value = null
    videoRemoteOpen.value = false
  }

  async function loadVideoQueue(mediaItemID: number, currentEpisodeID: number) {
    if (!mediaItemID) {
      videoQueue.value = []
      videoQueueKey = ''
      return
    }
    const key = `${mediaItemID}:${currentEpisodeID}`
    if (videoQueueKey === key) return
    videoQueueKey = key
    const request = ++videoQueueRequest
    videoQueueLoading.value = true
    try {
      const { $heya } = useNuxtApp()
      const detail = await $heya('/api/media/{id}', {
        path: { id: String(mediaItemID) as never },
      }) as MediaDetail
      if (request !== videoQueueRequest) return
      const files = detail.episode_files ?? {}
      const all: VideoQueueItem[] = []
      const seasons = [...(detail.seasons ?? [])].sort((a, b) => a.season_number - b.season_number)
      for (const season of seasons) {
        const episodes = [...(season.episodes ?? [])].sort((a, b) => a.episode_number - b.episode_number)
        for (const episode of episodes) {
          const key = `s${season.season_number}e${episode.episode_number}`
          const file = files[key]
          const fileID = file?.file_public_id || file?.file_id
          if (!fileID) continue
          const episodeLabel = `S${String(season.season_number).padStart(2, '0')}E${String(episode.episode_number).padStart(2, '0')}`
          const name = episode.preferred_title || episode.title
          all.push({
            fileId: String(fileID),
            mediaItemId: mediaItemID,
            entityType: 'episode',
            entityId: episode.id,
            title: name ? `${episodeLabel} · ${name}` : episodeLabel,
            episodeLabel,
            runtimeSeconds: Math.max(0, episode.runtime_minutes ?? 0) * 60,
          })
        }
      }
      const current = all.findIndex(item => item.entityId === currentEpisodeID)
      videoQueue.value = current >= 0 ? all.slice(current + 1) : all
    } catch {
      if (request === videoQueueRequest) {
        videoQueue.value = []
        videoQueueKey = ''
      }
    } finally {
      if (request === videoQueueRequest) videoQueueLoading.value = false
    }
  }

  async function playVideoQueueItem(item: VideoQueueItem) {
    const current = session.value?.media_kind === 'video' ? session.value : lastVideoSession
    if (!current) return null
    const snap = await playVideo({
      fileId: item.fileId,
      mediaItemId: item.mediaItemId,
      entityType: item.entityType,
      entityId: item.entityId,
      title: item.title,
      audioTrack: current.audio_track ?? 0,
      subtitleTrack: current.subtitle_track,
      quality: current.quality ?? 'auto',
      fallbackVolume: current.volume,
    })
    videoRemoteOpen.value = true
    return snap
  }

  async function playNextVideo() {
    const next = videoQueue.value[0]
    if (!next || !engagedDeviceId.value) return null
    return await playVideoQueueItem(next)
  }

  // WS mirror entry point (plugins/cast-live.client.ts). Returns 'ended'
  // when a session this tab owns finished naturally (caller advances the
  // queue), 'failed' on a device failure, null otherwise.
  function applyEvent(p: CastStateEvent): 'ended' | 'failed' | null {
    const prev = session.value

    // Engaged tabs mirror only their own device — a second receiver's
    // session (someone casting to another room) must not hijack the state.
    // Un-engaged tabs mirror whatever is newest, purely for display.
    if (engagedDeviceId.value && p.device_id !== engagedDeviceId.value) return null

    if (p.state === 'stopped' || p.state === 'failed') {
      // Only react to the session we're actually mirroring — a stale event
      // from a session we already replaced must not clear the new one.
      if (!prev || prev.id !== p.session_id) return null
      const wasOurs = ownsPlayback
      if (prev.media_kind === 'video') lastVideoSession = prev
      if (p.state === 'failed') {
        session.value = null
        ownsPlayback = false
        return 'failed'
      }
      if (wasOurs && prev.media_kind === 'video' && videoQueue.value.length) {
        session.value = { ...prev, state: 'starting', position_sec: 0 }
        samplePosition(0)
        return 'ended'
      }
      session.value = null
      return wasOurs ? 'ended' : null
    }

    // A track this tab never requested = another client took over the
    // device; stop driving the queue from here.
    const incomingMediaKey = p.media_kind === 'video'
      ? `video:${p.file_id ?? ''}:${p.entity_type ?? ''}:${p.entity_id ?? 0}`
      : (p.track_id ? `audio:${p.track_id}` : '')
    if (incomingMediaKey && incomingMediaKey !== lastRequestedMediaKey) ownsPlayback = false

    session.value = {
      id: p.session_id,
      device_id: p.device_id,
      device_name: p.device_name,
      user_id: p.user_id,
      state: p.state,
      media_kind: p.media_kind,
      track_id: p.track_id,
      file_id: p.file_id,
      media_item_id: p.media_item_id,
      entity_type: p.entity_type,
      entity_id: p.entity_id,
      title: p.title,
      artist: p.artist,
      audio_track: p.audio_track,
      subtitle_track: p.subtitle_track,
      quality: p.quality,
      album: prev?.track_id === p.track_id ? prev?.album : undefined,
      duration_sec: p.duration_sec,
      position_sec: p.position_sec,
      volume: p.volume,
    }
    if (session.value.media_kind === 'video') lastVideoSession = session.value
    lastDeviceVolume.value = p.volume
    samplePosition(p.position_sec)
    if (p.media_kind === 'video' && p.file_id && videoStreamInfoFileID.value !== p.file_id) {
      resetVideoStreamInfo(p.file_id)
    }
    if (p.media_kind === 'video' && p.entity_type === 'episode' && p.media_item_id) {
      void loadVideoQueue(p.media_item_id, p.entity_id ?? 0)
    }
    return null
  }

  return {
    devices, devicesLoaded, session, engagedDeviceId, connecting, lastDeviceVolume,
    videoStreamInfo, videoStreamInfoLoading, videoStreamInfoError,
    videoRemoteOpen, videoQueue, videoQueueLoading,
    engaged, deviceName, isClientDevice,
    refreshDevices, adoptExisting,
    playTrack, playVideo, updateVideo, pause, resume, seekTo, setVolume, stopSession, disconnect,
    loadVideoStreamInfo, loadVideoQueue, playVideoQueueItem, playNextVideo,
    applyClientDeviceState, engageClientDevice, releaseDevice,
    applyEvent, livePositionSec,
  }
})

if (import.meta.hot) import.meta.hot.accept(acceptHMRUpdate(useCastStore, import.meta.hot))
