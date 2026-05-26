// useNowPlayingSession — heartbeat the server every 10s while the video
// player is mounted, so the activity panel can show "Karbowiak is watching
// Nobody — direct play — 14:23/1:30:00".
//
// One composable per VideoPlayer instance. Mints a stable session_id on
// first heartbeat, beats every 10s, and tears down on unmount. The 30s
// server-side purge handles ungraceful disconnects (closing tab without
// a clean unmount).
//
// Transcode info comes back to us from the FE's existing stream-info
// response — we just echo it through. The server treats it as advisory;
// resolving it server-side from the stream-info call would be more
// authoritative but adds complexity for marginal benefit.

export interface SessionHeartbeatPayload {
  fileId: number
  mediaItemId: number | null
  /** "movie" | "episode" | "track" — drives server-side display formatting. */
  entityType?: string
  /** Type-specific id: movie media_item_id, tv_episodes.id, tracks.id. */
  entityId?: number
  positionSeconds: number
  totalSeconds: number
  paused: boolean
  playbackAction?: string
  videoCodec?: string
  audioCodec?: string
  container?: string
  width?: number
  height?: number
  bitrateKbps?: number
}

// Mint a session id that's collision-safe enough for an in-memory map keyed
// by user. crypto.randomUUID() when available (modern browsers); fallback
// to a Math.random fountain otherwise.
function mintSessionId(): string {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
      return crypto.randomUUID()
    }
  } catch { /* ignore */ }
  return `s_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 10)}`
}

export function useNowPlayingSession() {
  const sessionId = mintSessionId()
  let timer: ReturnType<typeof setInterval> | null = null

  async function send(payload: SessionHeartbeatPayload) {
    if (!payload.mediaItemId) return // nothing to attribute the session to
    const { $heya } = useNuxtApp()
    try {
      await $heya('/api/me/sessions/heartbeat', {
        method: 'POST',
        body: {
          session_id: sessionId,
          file_id: payload.fileId,
          media_item_id: payload.mediaItemId,
          entity_type: payload.entityType ?? '',
          entity_id: payload.entityId ?? 0,
          position_seconds: Math.floor(payload.positionSeconds),
          total_seconds: Math.floor(payload.totalSeconds),
          paused: payload.paused,
          playback_action: payload.playbackAction ?? '',
          video_codec: payload.videoCodec ?? '',
          audio_codec: payload.audioCodec ?? '',
          container: payload.container ?? '',
          width: payload.width ?? 0,
          height: payload.height ?? 0,
          bitrate_kbps: payload.bitrateKbps ?? 0,
          client_user_agent: typeof navigator !== 'undefined' ? navigator.userAgent.slice(0, 256) : '',
          client_ip: '',
        } as never,
      })
    } catch {
      // Best-effort — a missed heartbeat will get backfilled on the next one.
    }
  }

  // start takes a *getter* (not a snapshot) so each 10s interval tick
  // reads live state from the consumer. Pass a function that returns the
  // current payload — typically `() => myComputedRef.value` from a Vue
  // component using a computed payload.
  function start(getPayload: () => SessionHeartbeatPayload) {
    stop()
    // Immediate beat — activity panel sees the session within a second.
    send(getPayload())
    timer = setInterval(() => send(getPayload()), 10_000)
  }

  function stop() {
    if (timer) { clearInterval(timer); timer = null }
  }

  async function end() {
    stop()
    const { $heya } = useNuxtApp()
    try {
      await $heya('/api/me/sessions/{session_id}', {
        method: 'DELETE',
        path: { session_id: sessionId },
      })
    } catch { /* ignore */ }
  }

  // Best-effort end on page unload — DELETE may not always land (navigator
  // is restrictive about long-lived requests during unload), but the
  // server-side purge sweep covers the gap.
  if (import.meta.client) {
    window.addEventListener('beforeunload', () => { end() })
  }

  return {
    sessionId,
    heartbeat: send,
    start,
    stop,
    end,
  }
}
