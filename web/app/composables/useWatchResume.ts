// useWatchResume — reactive "is this item in progress?" lookup. Reads from
// the shared `me-watch-continue` vue-query cache so every consumer
// (hero, movie detail page, TV detail page) hits the same data and only
// one network round-trip happens per session.
//
// The CW endpoint returns up to 20 incomplete items. For the activity
// patterns we care about — Hero / detail page Play buttons — the user is
// almost always looking at something they've recently played, so it's in
// the list. If they have >20 incomplete items, lookup misses but the
// button just defaults to "Play" (the dialog inside the player will
// catch the saved progress via its own API call regardless).

import { useQuery, useQueryClient } from '@tanstack/vue-query'

interface ContinueWatchingRow {
  entity_type: string
  entity_id: number
  media_item_id: number
  progress_seconds: number
  total_seconds: number
  file_id?: number
}

const CW_QUERY_KEY = ['me', 'watch', 'continue'] as const

// useWatchResumeList returns the shared CW query — pages/index.vue uses
// the same key. Reading it from a second place doesn't trigger a refetch
// thanks to vue-query's dedup.
export function useWatchResumeList() {
  const { $heya } = useNuxtApp()
  return useQuery({
    queryKey: CW_QUERY_KEY,
    queryFn: async () => (await $heya('/api/me/watch/continue')) as ContinueWatchingRow[],
    staleTime: 1000 * 30,
  })
}

// Drop the shared CW cache so the home rail + Play/Resume buttons re-fetch.
// Call after anything that changes watched state (mark watched/unwatched)
// — the backend excludes completed items and deletes rows on unwatch, but
// the FE would otherwise keep showing the stale tile until the 30s staleTime
// (or a hard reload). Returns the composable so callers can grab it at
// setup time and invoke on demand.
export function useInvalidateContinueWatching() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: CW_QUERY_KEY })
}

// useWatchResume returns a reactive object describing whether the given
// (entity_type, entity_id) has saved progress. Use this for "Play" /
// "Resume" button label switching.
//
// inProgress: true when there's ≥30s of unfinished playback
// progressSeconds: how far in (0 when not in progress)
// percent: 0-100, useful for progress bars on the detail page
export function useWatchResume(entityType: Ref<string> | string, entityId: Ref<number> | number) {
  const list = useWatchResumeList()

  const epType = computed(() => typeof entityType === 'string' ? entityType : entityType.value)
  const epId = computed(() => typeof entityId === 'number' ? entityId : entityId.value)

  const entry = computed(() => {
    const items = list.data.value
    if (!items || !epId.value) return undefined
    return items.find(it => it.entity_type === epType.value && it.entity_id === epId.value)
  })

  const inProgress = computed(() => (entry.value?.progress_seconds ?? 0) > 30)
  const progressSeconds = computed(() => entry.value?.progress_seconds ?? 0)
  const totalSeconds = computed(() => entry.value?.total_seconds ?? 0)
  const percent = computed(() => {
    const total = totalSeconds.value
    if (total <= 0) return 0
    return Math.min(100, Math.round((progressSeconds.value / total) * 100))
  })

  return { inProgress, progressSeconds, totalSeconds, percent }
}
