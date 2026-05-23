// Polls /api/stream/{id}/transcode-status while a panel is visible. Returns
// a reactive ref the consumer can render. Polling stops when `enabled` flips
// to false — saves backend round-trips when the diagnostics overlay is hidden.

export type TranscodeState =
  | 'idle'        // no session
  | 'running'     // ffmpeg encoding right now
  | 'throttled'   // lead cap hit — encoder paused, will resume when player catches up
  | 'completed'   // ran into already-completed territory
  | 'killed'      // manually cancelled (quality switch / shutdown)
  | 'exited'     // process exited without a more specific reason

export interface TranscodeStatus {
  active: boolean
  running: boolean
  state: TranscodeState
  head_stop_reason?: string
  session_key?: string
  total_segments: number
  ready_segments: number
  head_start_segment: number
  head_current_segment: number
  last_requested_segment: number
  lead_cap_seconds: number
  frame: number
  fps: number
  bitrate_kbps: number
  total_size_bytes: number
  out_time_seconds: number
  speed: number
  dup_frames: number
  drop_frames: number
  elapsed_seconds: number
  last_update_ago_ms: number
  started_at_unix_ms?: number
  updated_at_unix_ms?: number
}

export function useTranscodeStatus(
  fileId: Ref<number | null | undefined>,
  enabled: Ref<boolean>,
  token: Ref<string | null | undefined>,
  intervalMs = 1500,
) {
  const status = ref<TranscodeStatus | null>(null)
  const error = ref<string | null>(null)
  let timer: ReturnType<typeof setInterval> | null = null

  async function poll() {
    const id = fileId.value
    if (!id || !token.value) return
    try {
      const res = await fetch(`/api/stream/${id}/transcode-status`, {
        headers: { Authorization: `Bearer ${token.value}` },
      })
      if (!res.ok) {
        error.value = `HTTP ${res.status}`
        return
      }
      status.value = await res.json()
      error.value = null
    } catch (e: unknown) {
      error.value = e instanceof Error ? e.message : String(e)
    }
  }

  function start() {
    stop()
    poll()
    timer = setInterval(poll, intervalMs)
  }

  function stop() {
    if (timer) { clearInterval(timer); timer = null }
  }

  watch([enabled, fileId, token], ([on, id, tok]) => {
    if (on && id && tok) start()
    else stop()
  }, { immediate: true })

  onUnmounted(stop)

  return { status, error, refresh: poll }
}
