import { useQueryCache } from '@pinia/colada'
import type { EntryKey } from '@pinia/colada'
import type { MediaPayload, WsEvent } from './useEventBus'

export interface LiveRefreshGroup {
  /** WS event-type strings (e.g. 'media.added', 'media.updated') that feed this group. */
  events: string[]
  /**
   * Pinia Colada keys to invalidate when this group fires. Uses prefix
   * default prefix matching, so a partial key like ['media', 'recent',
   * 'movie'] invalidates every query whose key starts with that prefix.
   */
  keys?: EntryKey[]
  /**
   * Escape hatch for pages that don't keep their data behind a Pinia Colada
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
 * query-cache invalidations (and/or a raw `refetch`), so pages stay
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
 * Pinia Colada only refetches active entries on invalidation. A cached
 * query for a page you're not currently on is marked stale and refetches
 * lazily next time something reads it, instead of firing in the
 * background for no visible benefit.
 *
 * Cleans itself up via `onScopeDispose` — works whether called from a page
 * `<script setup>` or a nested composable, no manual onUnmounted needed.
 */
export function useLiveRefresh(groups: LiveRefreshGroup[]) {
  const { on } = useEventBus()
  const queryClient = useQueryCache()
  const cleanups: Array<() => void> = []
  // One shared visibilitychange listener drives every group's catch-up
  // fire (see below) instead of each group registering its own — a page
  // can carry a handful of groups and they'd all be waking for the same
  // event.
  const onReturnToVisible: Array<() => void> = []

  for (const group of groups) {
    let timer: ReturnType<typeof setTimeout> | null = null
    let trailingPending = false
    // Set instead of firing when a trigger lands while the tab is hidden —
    // a backgrounded tab has nobody looking at the page, so invalidating
    // (and the network refetch that follows) would just wake the radio/CPU
    // for no visible benefit. Cleared by a single catch-up fire once the
    // tab is visible again.
    let hiddenPending = false

    const fire = () => {
      for (const key of group.keys ?? []) {
        queryClient.invalidateQueries({ key })
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
          // The window can close while the tab is hidden (it was armed
          // before the tab went to the background) — treat that exactly
          // like a fresh hidden trigger instead of firing blind.
          if (document.visibilityState === 'hidden') hiddenPending = true
          else invoke()
        }
      }, WINDOW_MS)
    }

    const trigger = (event: WsEvent) => {
      if (group.filter && !group.filter(event)) return
      if (document.visibilityState === 'hidden') {
        hiddenPending = true
        return
      }
      if (timer) trailingPending = true
      else invoke()
    }

    for (const type of group.events) {
      cleanups.push(on(type, trigger))
    }
    cleanups.push(() => {
      if (timer) clearTimeout(timer)
    })

    onReturnToVisible.push(() => {
      if (!hiddenPending) return
      hiddenPending = false
      // A trailing window can still be armed from before the tab went
      // hidden (it was left running, just forbidden from invoking) —
      // route through the same leading/trailing machinery so a live
      // window collapses this into the trailing fire instead of a double
      // invalidation.
      if (timer) trailingPending = true
      else invoke()
    })
  }

  const handleVisibilityChange = () => {
    if (document.visibilityState !== 'visible') return
    for (const fn of onReturnToVisible) fn()
  }
  document.addEventListener('visibilitychange', handleVisibilityChange)
  cleanups.push(() => document.removeEventListener('visibilitychange', handleVisibilityChange))

  onScopeDispose(() => {
    cleanups.forEach(fn => fn())
  })
}

/**
 * Convenience filter for the common case: gate a group on
 * `media.added` / `media.updated`'s `media_type` field (movie / tv / anime /
 * music / book / ...), so e.g. a new movie doesn't retrigger the TV page's group.
 */
export function byMediaType(...mediaTypes: string[]) {
  const allowed = new Set(mediaTypes)
  return (event: WsEvent) => {
    const mediaType = (event.payload as MediaPayload | undefined)?.media_type
    return !!mediaType && allowed.has(mediaType)
  }
}
