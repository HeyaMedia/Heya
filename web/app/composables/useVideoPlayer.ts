import type { StreamInfoResponse } from '~~/shared/types'

export interface PlayerState {
  fileId: number
  mediaItemId: number | null
  streamInfo: StreamInfoResponse | null
  currentTime: number
  duration: number
  paused: boolean
  buffered: number
  volume: number
  muted: boolean
  fullscreen: boolean
  quality: string
  loading: boolean
  error: string | null
  showNerdInfo: boolean
}

export function useVideoPlayer(fileId: Ref<number>, mediaItemId: Ref<number | null>) {
  const state = reactive<PlayerState>({
    fileId: fileId.value,
    mediaItemId: mediaItemId.value,
    streamInfo: null,
    currentTime: 0,
    duration: 0,
    paused: true,
    buffered: 0,
    volume: 100,
    muted: false,
    fullscreen: false,
    quality: 'auto',
    loading: true,
    error: null,
    showNerdInfo: false,
  })

  let progressInterval: ReturnType<typeof setInterval> | null = null

  async function loadStreamInfo() {
    try {
      const caps = useClientCaps()
      const capsQuery = capsToQueryString(caps)
      // /api/stream/{file_id}/info — keep raw $fetch: capsToQueryString already
      // builds the full query string and the response shape isn't pinned in the
      // OpenAPI spec yet.
      state.streamInfo = await $fetch<StreamInfoResponse>(
        `/api/stream/${fileId.value}/info${capsQuery ? `?${capsQuery}` : ''}`,
        { headers: useAuth().token.value ? { Authorization: `Bearer ${useAuth().token.value}` } : {} },
      )
    } catch {
      state.error = 'Failed to load stream info'
    }
    state.loading = false
  }

  async function reportProgress() {
    if (!mediaItemId.value || state.currentTime < 1) return
    try {
      const { $heya } = useNuxtApp()
      await $heya('/api/me/watch/{media_item_id}/progress', {
        method: 'POST',
        path: { media_item_id: mediaItemId.value },
        body: {
          // TODO: plumb episode/audiobook awareness from VideoPlayer props so
          // this isn't hardcoded. Server description says it defaults to 'movie'.
          entity_type: 'movie',
          progress_seconds: Math.floor(state.currentTime),
          total_seconds: Math.floor(state.duration),
        },
      })
    } catch {}
  }

  function startProgressReporting() {
    stopProgressReporting()
    progressInterval = setInterval(reportProgress, 10_000)
  }

  function stopProgressReporting() {
    if (progressInterval) {
      clearInterval(progressInterval)
      progressInterval = null
    }
  }

  function subtitleUrl(index: number) {
    const { token } = useAuth()
    return `/api/stream/${fileId.value}/subtitles/${index}?token=${token.value}`
  }

  async function loadResumePosition(): Promise<number> {
    if (!mediaItemId.value) return 0
    try {
      const { $heya } = useNuxtApp()
      const history = await $heya('/api/me/watch/continue') as any[]
      const entry = history?.find((h: any) => h.media_item_id === mediaItemId.value)
      if (entry && !entry.completed && entry.progress_seconds > 10) {
        return entry.progress_seconds
      }
    } catch {}
    return 0
  }

  onUnmounted(() => {
    reportProgress()
    stopProgressReporting()
  })

  return {
    state,
    loadStreamInfo,
    reportProgress,
    startProgressReporting,
    stopProgressReporting,
    subtitleUrl,
    loadResumePosition,
  }
}
