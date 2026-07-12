import { acceptHMRUpdate, defineStore } from 'pinia'

// Server-side casting (docs/cast-plan.md Phase 2). The SERVER is the player:
// it streams PCM to the receiver and owns the session; this store is the
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
  name: string
  model?: string
  manufacturer?: string
  host?: string
  addr?: string
  port?: number
  last_seen?: string
}

export interface CastSession {
  id: string
  device_id: string
  device_name: string
  user_id: number
  state: string // starting | playing | paused | stopped | failed
  track_id?: number
  title?: string
  artist?: string
  album?: string
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
  track_id?: number
  title?: string
  artist?: string
  position_sec: number
  duration_sec?: number
  volume: number
  at: string
}

const VOLUME_DEBOUNCE_MS = 200

export const useCastStore = defineStore('cast', () => {
  const devices = ref<CastDevice[]>([])
  const devicesLoaded = ref(false)
  const session = ref<CastSession | null>(null)
  const engagedDeviceId = ref<string | null>(null)
  // True while the play POST is in flight so the UI can show a connecting
  // state before the first WS event lands.
  const connecting = ref(false)

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

  // Advance ownership: only the tab that started the current cast play
  // drives the queue when a track ends naturally, so two open tabs with
  // populated queues don't both fire the next track (WS events are global).
  // A foreign takeover is detected by a track we never requested appearing.
  let ownsPlayback = false
  let lastRequestedTrackId = 0

  // The device stream volume we last knew. The server removes a session
  // when its track ends, so the next queue advance creates a NEW session —
  // reusing this keeps a mid-queue volume tweak sticky across tracks. Null
  // until the first session reports in; then the first engage caps the
  // handoff at a modest level so a loud local slider never blasts the room.
  const lastDeviceVolume = ref<number | null>(null)

  async function refreshDevices() {
    const { $heya } = useNuxtApp()
    try {
      const res = await $heya('/api/cast/devices') as { items?: CastDevice[] | null }
      devices.value = res.items ?? []
    } catch { /* casting disabled or unreachable — keep the last list */ }
    devicesLoaded.value = true
  }

  // Adopt a session that already exists server-side. Called at boot (page
  // load while the house is casting) and again after a WS reconnect, where
  // it doubles as the re-sync: a session that ended while we were offline
  // clears the stale mirror. Adoption does NOT take queue ownership — this
  // tab didn't start the playback.
  async function adoptExisting() {
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
    const { $heya } = useNuxtApp()
    connecting.value = true
    lastRequestedTrackId = trackId
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

  async function postControl(id: string, verb: 'pause' | 'resume' | 'stop'): Promise<CastSession> {
    const { $heya } = useNuxtApp()
    const path = { id }
    if (verb === 'pause') return await $heya('/api/cast/sessions/{id}/pause', { method: 'POST', path }) as CastSession
    if (verb === 'resume') return await $heya('/api/cast/sessions/{id}/resume', { method: 'POST', path }) as CastSession
    return await $heya('/api/cast/sessions/{id}/stop', { method: 'POST', path }) as CastSession
  }

  async function pause() {
    const s = session.value
    if (!s) return
    // Optimistic: the receiver goes silent near-instantly (transport
    // teardown); the WS event confirms.
    samplePosition(livePositionSec())
    session.value = { ...s, state: 'paused' }
    const snap = await postControl(s.id, 'pause')
    session.value = snap
    samplePosition(snap.position_sec)
  }

  async function resume() {
    const s = session.value
    if (!s) return
    // Resume respawns the sender at the frozen position (~2-3s of
    // re-establishment — 'starting' keeps the play button engaged).
    session.value = { ...s, state: 'starting' }
    const snap = await postControl(s.id, 'resume')
    session.value = snap
    samplePosition(snap.position_sec)
  }

  async function seekTo(seconds: number) {
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
    const s = session.value
    session.value = null
    if (!s) return
    try {
      await postControl(s.id, 'stop')
    } catch { /* already gone server-side */ }
  }

  // Full disconnect: stop the session (if any) and release the output.
  async function disconnect() {
    engagedDeviceId.value = null
    await stopSession()
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
      session.value = null
      if (p.state === 'failed') {
        ownsPlayback = false
        return 'failed'
      }
      return wasOurs ? 'ended' : null
    }

    // A track this tab never requested = another client took over the
    // device; stop driving the queue from here.
    if (p.track_id && p.track_id !== lastRequestedTrackId) ownsPlayback = false

    session.value = {
      id: p.session_id,
      device_id: p.device_id,
      device_name: p.device_name,
      user_id: p.user_id,
      state: p.state,
      track_id: p.track_id,
      title: p.title,
      artist: p.artist,
      album: prev?.track_id === p.track_id ? prev?.album : undefined,
      duration_sec: p.duration_sec,
      position_sec: p.position_sec,
      volume: p.volume,
    }
    lastDeviceVolume.value = p.volume
    samplePosition(p.position_sec)
    return null
  }

  return {
    devices, devicesLoaded, session, engagedDeviceId, connecting, lastDeviceVolume,
    engaged, deviceName,
    refreshDevices, adoptExisting,
    playTrack, pause, resume, seekTo, setVolume, stopSession, disconnect,
    applyEvent, livePositionSec,
  }
})

if (import.meta.hot) import.meta.hot.accept(acceptHMRUpdate(useCastStore, import.meta.hot))
