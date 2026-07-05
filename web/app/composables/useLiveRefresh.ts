import { useQueryClient } from '@tanstack/vue-query'
import type { QueryKey } from '@tanstack/vue-query'
import type { MediaPayload, WsEvent } from './useEventBus'

export interface LiveRefreshGroup {
  /** WS event-type strings (e.g. 'media.added', 'media.updated') that feed this group. */
  events: string[]
  /**
   * vue-query keys to invalidate when this group fires. Uses vue-query's
   * default prefix matching, so a partial key like ['media', 'recent',
   * 'movie'] invalidates every query whose key starts with that prefix.
   */
  keys?: QueryKey[]
  /**
   * Escape hatch for pages that don't keep their data behind a vue-query
   * cache — movies/index.vue and tv/index.vue populate a plain ref from an
   * onMounted() fetch, so there's no query key to invalidate. Called
   * instead of, or alongside, invalidating `keys`.
   */
  refetch?: () => unknown
  /**
   * Gate on the event payload — most callers filter on `media_type` (see
   * `byMediaType` below) so a movie add doesn't wake the TV page's group.
   * Omit to fire on every event of the listed types.
   */
  filter?: (event: WsEvent) => boolean
}

const WINDOW_MS = 4000

/**
 * Subscribes to the WS event bus and coalesces matching events into
 * vue-query cache invalidations (and/or a raw `refetch`), so pages stay
 * live as new content is matched/enriched without hammering the API.
 *
 * WHY the coalescing exists: a library scan can match hundreds of files in
 * a few seconds, and each one fires its own `media.added` (or later
 * `media.updated`, once the debounced re-enrich lands new
 * seasons/episodes/albums). Without coalescing, a 500-album import would
 * invalidate — and refetch — the "recently added" rail 500 times. Each
 * group below runs its own leading+trailing throttle: the first event
 * fires an invalidation immediately (so the page doesn't feel laggy), any
 * further events inside the ~4s window are collapsed, and if events kept
 * arriving during the window one trailing invalidation fires right after
 * it closes (so the tail of a burst isn't lost) — then the group goes
 * idle again until the next event. Net effect: at most one invalidation
 * burst per ~4s per group, however many events land in that window.
 *
 * `refetchType: 'active'` on every invalidateQueries call is the other
 * half: only queries with a mounted observer actually refetch. A cached
 * query for a page you're not currently on is marked stale and refetches
 * lazily next time something reads it, instead of firing in the
 * background for no visible benefit.
 *
 * Cleans itself up via `onScopeDispose` — works whether called from a page
 * `<script setup>` or a nested composable, no manual onUnmounted needed.
 */
export function useLiveRefresh(groups: LiveRefreshGroup[]) {
  const { on } = useEventBus()
  const queryClient = useQueryClient()
  const cleanups: Array<() => void> = []

  for (const group of groups) {
    let timer: ReturnType<typeof setTimeout> | null = null
    let trailingPending = false

    const fire = () => {
      for (const key of group.keys ?? []) {
        queryClient.invalidateQueries({ queryKey: key, refetchType: 'active' })
      }
      group.refetch?.()
    }

    // Leading-edge fire immediately, then arm a window during which further
    // triggers just flip `trailingPending`. If the window closes with a
    // trailing trigger pending, fire once more and re-arm — this keeps a
    // sustained burst ticking over at the window cadence instead of
    // starving until the whole burst goes quiet (a plain trailing debounce
    // would never fire mid-scan).
    const invoke = () => {
      fire()
      timer = setTimeout(() => {
        timer = null
        if (trailingPending) {
          trailingPending = false
          invoke()
        }
      }, WINDOW_MS)
    }

    const trigger = (event: WsEvent) => {
      if (group.filter && !group.filter(event)) return
      if (timer) trailingPending = true
      else invoke()
    }

    for (const type of group.events) {
      cleanups.push(on(type, trigger))
    }
    cleanups.push(() => {
      if (timer) clearTimeout(timer)
    })
  }

  onScopeDispose(() => {
    cleanups.forEach(fn => fn())
  })
}

/**
 * Convenience filter for the common case: gate a group on
 * `media.added` / `media.updated`'s `media_type` field (movie / tv / music /
 * book / ...) so e.g. a new movie doesn't retrigger the TV page's group.
 */
export function byMediaType(mediaType: string) {
  return (event: WsEvent) => (event.payload as MediaPayload | undefined)?.media_type === mediaType
}
