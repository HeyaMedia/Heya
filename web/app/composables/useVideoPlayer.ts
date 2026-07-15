import type { StreamInfoResponse } from '~~/shared/types'
import type { PlaybackEntityType } from '~/composables/usePlaybackEvents'

export interface PlayerState {
  fileId: string | number
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

export function useVideoPlayer(
  fileId: Ref<string | number>,
  mediaItemId: Ref<number | null>,
  // entityType + entityId tell recordPlayback which row to update in
  // user_watch_progress: ("movie", media_item_id) for movies, ("episode",
  // episode_id) for episodes. Optional — when missing we default to
  // ("movie", media_item_id) for backwards compatibility, but that path
  // mis-stores TV progress against the series media_item and breaks the
  // CW endpoint's episode-detail join. Always pass these when known.
  entityType?: Ref<string>,
  entityId?: Ref<number>,
) {
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

  async function loadStreamInfo() {
    try {
      const caps = useClientCaps()
      const capsQuery = capsToQueryString(caps)
      // /api/stream/{file_id}/info — keep raw $fetch: capsToQueryString already
      // builds the full query string and the response shape isn't pinned in the
      // OpenAPI spec yet.
      const url = `/api/stream/${fileId.value}/info${capsQuery ? `?${capsQuery}` : ''}`
      const token = useAuth().token.value
      state.streamInfo = await $fetch<StreamInfoResponse>(url, {
        headers: withClientSurfaceHeaders(url, token ? { Authorization: `Bearer ${token}` } : undefined),
      })
    } catch {
      state.error = 'Failed to load stream info'
    }
    state.loading = false
  }

  // emitProgress is the single point of emission for video watch progress.
  // Caller provides the *live* position+duration values (we don't trust the
  // local PlayerState here — it's a stale shadow of the HeyaPlayer state,
  // which is the real source of truth for currentTime). `completed` defaults
  // to "within 30s of the end" — mirrors the server-side rule so the cache
  // doesn't have to recompute it.
  //
  // entityType/entityId default to ('movie', media_item_id) when not
  // supplied — works for movies but wrong for TV (it'd store progress
  // against the series media_item, hiding episode info in the CW row).
  // VideoPlayer.vue passes both for TV so progress lands as
  // ('episode', episode_id) and the CW JOIN picks up season + ep title.
  async function emitProgress(positionSeconds: number, durationSeconds: number, completed?: boolean) {
    // Narrow the type — recordPlayback's PlaybackEntityType is a tagged
    // union, but our entityType comes from a free-form ref. Anything
    // outside the known set falls back to 'movie' for back-compat.
    const raw = (entityType?.value || 'movie') as PlaybackEntityType
    const effectiveType: PlaybackEntityType =
      raw === 'episode' || raw === 'track' ? raw : 'movie'
    const effectiveId = entityId?.value || mediaItemId.value || 0
    if (!effectiveId) return
    const position = Math.floor(positionSeconds)
    const total = Math.floor(durationSeconds)
    if (position < 1) return // ignore the very start; nothing meaningful to record yet
    const isCompleted = completed ?? (total > 0 && position >= total - 30)
    await recordPlayback({
      entity_type: effectiveType,
      entity_id: effectiveId,
      position_seconds: position,
      total_seconds: total,
      completed: isCompleted,
    })
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

  return {
    state,
    loadStreamInfo,
    emitProgress,
    subtitleUrl,
    loadResumePosition,
  }
}
