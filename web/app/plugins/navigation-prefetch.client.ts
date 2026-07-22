import { setInfiniteQueryData, useQueryCache, type EntryKey, type UseQueryOptions } from '@pinia/colada'
import { collectionDetailQuery, personDetailQuery } from '~/queries/discovery'
import { mediaDetailQuery } from '~/queries/media'
import { continueWatchingInfinite, upNextRailInfinite } from '~/queries/activity'
import { meSettingsQuery } from '~/queries/user'
import {
  musicAlbumDetailQuery,
  musicArtistDetailQuery,
  musicGenreShelfQuery,
  musicLabelShelfQuery,
  musicLapsedShelfQuery,
  musicMixesQuery,
  musicMoreByArtistsQuery,
  musicMostPlayedShelfQuery,
  musicOnThisDayInfinite,
  musicRecentArtistsInfinite,
  musicRecentPlaylistsInfinite,
  playlistDetailQuery,
} from '~/queries/music'
import { enrichedCatalogQuery } from '~/queries/catalog'
import {
  forYouInfinite,
  recentArtistsInfinite,
  recentAlbumsInfinite,
  recentMediaInfinite,
  recentTVInfinite,
} from '~/queries/rails'
import { toValue } from 'vue'

// Central route → data-query registry. NuxtLink already preloads route code;
// this plugin adds the critical API payload when pointer/focus intent is
// visible. New domains can join without every poster knowing cache details.
// Section roots (/, /music, /movies, /tv) map to their full landing sets so
// intent warming and the idle cross-section pump below can make switching
// sections seamless.
function queriesForPath(pathname: string): UseQueryOptions<unknown>[] {
  const parts = pathname.split('/').filter(Boolean)
  if (!parts.length) {
    // Home: hero/ledger feeders + every finite rail (infinite rails are
    // listed separately in infiniteRailsForPath).
    return [meSettingsQuery()]
  }
  if (parts[0] === 'music' && !parts[1]) {
    return [
      musicMixesQuery(),
      musicMoreByArtistsQuery(),
      musicGenreShelfQuery(),
      musicMostPlayedShelfQuery(),
      musicLapsedShelfQuery(),
      musicLabelShelfQuery(),
    ]
  }
  const movieCatalogRoutes = new Set(['all', 'loved', 'library', 'list', 'franchises'])
  const tvCatalogRoutes = new Set(['all', 'loved', 'library', 'list'])
  if (parts[0] === 'movies' && !parts[1]) return [enrichedCatalogQuery('movie')]
  if (parts[0] === 'tv' && !parts[1]) return [enrichedCatalogQuery('tv')]
  if (parts[0] === 'movies' && parts[1] && movieCatalogRoutes.has(parts[1])) return [enrichedCatalogQuery('movie')]
  if (parts[0] === 'tv' && parts[1] && tvCatalogRoutes.has(parts[1])) return [enrichedCatalogQuery('tv')]
  if (parts[0] === 'movies' && ['recommendations', 'roulette', 'collection'].includes(parts[1] ?? '')) return []
  if (parts[0] === 'tv' && parts[1] === 'recommendations') return []
  if (parts[0] === 'movies' && parts[1]) return [mediaDetailQuery(decodeURIComponent(parts[1]))]
  if (parts[0] === 'tv' && parts[1]) return [mediaDetailQuery(decodeURIComponent(parts[1]))]
  if (parts[0] === 'books' && parts[1]) return [mediaDetailQuery(decodeURIComponent(parts[1]))]
  if (parts[0] === 'person' && parts[1]) return [personDetailQuery(decodeURIComponent(parts[1]))]
  if (parts[0] === 'collection' && parts[1]) {
    const id = Number(parts[1])
    return Number.isFinite(id) ? [collectionDetailQuery(id)] : []
  }
  if (parts[0] === 'music' && parts[1] === 'artist' && parts[2]) {
    const artist = decodeURIComponent(parts[2])
    if (parts[3] && parts[3] !== 'top-tracks') return [musicAlbumDetailQuery({ artistSlug: artist, albumSlug: decodeURIComponent(parts[3]) })]
    return [musicArtistDetailQuery(artist)]
  }
  if (parts[0] === 'music' && parts[1] === 'playlist' && parts[2]) {
    const id = Number(parts[2])
    return Number.isFinite(id) ? [playlistDetailQuery(id)] : []
  }
  if (parts[0] === 'music' && parts[1] === 'mix' && parts[2]) return [musicMixesQuery()]
  return []
}

// Infinite rails can't be warmed by a plain ensure+refresh — their query fn
// needs a pageParam — so the section sets list them separately. The warmer
// fetches page 0 with the options' own query fn and seeds the cache in the
// { pages, pageParams } shape via setInfiniteQueryData.
interface WarmableInfiniteRail {
  key: EntryKey
  initialPageParam: unknown
  query: (context: { pageParam: unknown }) => Promise<unknown>
  meta?: Record<string, unknown>
}

function infiniteRailsForPath(pathname: string): WarmableInfiniteRail[] {
  const parts = pathname.split('/').filter(Boolean)
  if (!parts.length) {
    return [
      recentMediaInfinite('movie'),
      recentTVInfinite(),
      recentAlbumsInfinite(),
      recentArtistsInfinite(),
      recentMediaInfinite('book'),
      forYouInfinite({ section: 'all' }),
      continueWatchingInfinite(),
      upNextRailInfinite(),
    ] as unknown as WarmableInfiniteRail[]
  }
  if (parts[0] === 'music' && !parts[1]) {
    return [
      recentAlbumsInfinite(),
      musicRecentArtistsInfinite(),
      musicOnThisDayInfinite(),
      musicRecentPlaylistsInfinite(),
    ] as unknown as WarmableInfiniteRail[]
  }
  return []
}

export default defineNuxtPlugin((nuxtApp) => {
  const queryCache = useQueryCache()
  const metrics = useDataMetricsStore()
  const router = useRouter()
  let timer: ReturnType<typeof setTimeout> | null = null
  let pendingHref = ''
  let visibleBudget = 4
  const visiblyWarmed = new Set<string>()
  const warmedPaths = new Map<string, ReturnType<typeof setTimeout> | null>()
  let navigationStarted = 0
  let navigationPath = ''
  let navigationWarm: boolean | null = null

  const connection = (navigator as Navigator & {
    connection?: { saveData?: boolean; effectiveType?: string }
    deviceMemory?: number
  }).connection
  const canSpeculate = !connection?.saveData && !connection?.effectiveType?.includes('2g')
  const touchFirst = matchMedia('(hover: none)').matches
  const deviceMemory = (navigator as Navigator & { deviceMemory?: number }).deviceMemory
  const constrainedDevice = touchFirst || (deviceMemory !== undefined && deviceMemory <= 4)

  function targetFrom(event: Event): HTMLElement | null {
    const target = event.target
    return target instanceof Element ? target.closest<HTMLElement>('a[href], [data-prefetch-to]') : null
  }

  function hrefFor(target: HTMLElement) {
    if (target instanceof HTMLAnchorElement) return target.href
    return target.dataset.prefetchTo ?? ''
  }

  /** All-success across a path's mapped queries; null when nothing maps. */
  function pathWarmState(pathname: string): boolean | null {
    const queries = queriesForPath(pathname)
    const rails = infiniteRailsForPath(pathname)
    if (!queries.length && !rails.length) return null
    return queries.every(options => queryCache.get(toValue(options.key))?.state.value.status === 'success')
      && rails.every(rail => queryCache.get(toValue(rail.key))?.state.value.status === 'success')
  }

  async function warmInfiniteRail(rail: WarmableInfiniteRail) {
    const key = toValue(rail.key)
    if (queryCache.get(key)?.state.value.data !== undefined) return
    try {
      const pageParam = toValue(rail.initialPageParam)
      const page = await rail.query({ pageParam })
      if (queryCache.get(key)?.state.value.data !== undefined) return
      setInfiniteQueryData(queryCache, key, { pages: [page], pageParams: [pageParam] })
      // setQueryData-created entries carry no options meta, and a later
      // ensure() never backfills meta on an existing entry — stamp the
      // rail's own meta so the persistence layer treats the seeded page
      // exactly like a mounted fetch.
      const seeded = queryCache.get(key)
      if (seeded && rail.meta) Object.assign(seeded.meta, rail.meta)
    } catch { /* speculative warm — never surface */ }
  }

  function warmPath(pathname: string) {
    void preloadRouteComponents(pathname)
    const queries = queriesForPath(pathname)
    const rails = infiniteRailsForPath(pathname)
    if (!queries.length && !rails.length) return
    if (warmedPaths.has(pathname)) return
    const alreadyCached = pathWarmState(pathname) === true
    metrics.recordPrefetch(alreadyCached)
    for (const options of queries) {
      const entry = queryCache.ensure(options)
      void queryCache.refresh(entry).catch(() => {})
    }
    for (const rail of rails) void warmInfiniteRail(rail)
    const wasteTimer = alreadyCached ? null : setTimeout(() => {
      if (!warmedPaths.has(pathname)) return
      warmedPaths.delete(pathname)
      metrics.recordPrefetchWasted()
    }, 30_000)
    warmedPaths.set(pathname, wasteTimer)
  }

  function warm(target: HTMLElement) {
    const href = hrefFor(target)
    if (!href) return
    const url = new URL(href, location.href)
    if (url.origin !== location.origin) return
    warmPath(url.pathname)
  }

  // Touch devices do not have hover time. Warm only the first few detail
  // targets that enter the near viewport; this gives phones/foldables useful
  // lead time without downloading an entire shelf. Save-Data and 2G skip it.
  const visibleObserver = canSpeculate && touchFirst
    ? new IntersectionObserver((entries) => {
        for (const observed of entries) {
          if (!observed.isIntersecting || visibleBudget <= 0) continue
          const target = observed.target as HTMLElement
          const href = hrefFor(target)
          visibleObserver?.unobserve(target)
          if (!href || visiblyWarmed.has(href)) continue
          visiblyWarmed.add(href)
          visibleBudget--
          warm(target)
        }
      }, { rootMargin: '160px 0px' })
    : null

  function observeVisibleTargets() {
    if (!visibleObserver || visibleBudget <= 0) return
    for (const target of document.querySelectorAll<HTMLElement>('a[href], [data-prefetch-to]')) {
      const href = hrefFor(target)
      if (!href || visiblyWarmed.has(href)) continue
      const url = new URL(href, location.href)
      if (url.origin !== location.origin) continue
      if (queriesForPath(url.pathname).length || infiniteRailsForPath(url.pathname).length) {
        visibleObserver.observe(target)
      }
    }
  }

  // ── Idle cross-section pump ───────────────────────────────────────────────
  // After a navigation settles, warm the OTHER main sections' landing sets —
  // one section per idle period — so the first tap on Movies/Music/TV paints
  // from a warm cache even before persistence has a snapshot this session.
  // Each section warms once per app session; hover/press intent still
  // freshens on demand. Save-Data and 2G skip it entirely (canSpeculate).
  const IDLE_SECTIONS = ['/', '/music', '/movies', '/tv']
  const idleWarmed = new Set<string>()

  function sectionOf(pathname: string): string {
    const first = pathname.split('/').filter(Boolean)[0]
    return first ? `/${first}` : '/'
  }

  // Safari has no requestIdleCallback — fall back to a settle delay.
  const requestIdle: (cb: () => void) => void
    = 'requestIdleCallback' in window
      ? cb => void window.requestIdleCallback(() => cb(), { timeout: 4000 })
      : cb => void setTimeout(cb, 2500)

  function pumpIdleSections() {
    // Touch-first and low-memory devices benefit much more from a quiet radio,
    // smaller query cache, and fewer persistence writes than from warming
    // several landing pages they may never open. Intent prefetch below remains
    // active for taps/focus on those devices.
    if (!canSpeculate || constrainedDevice || document.hidden) return
    const current = sectionOf(router.currentRoute.value.path)
    const next = IDLE_SECTIONS.find(s => s !== current && !idleWarmed.has(s))
    if (!next) return
    idleWarmed.add(next)
    warmPath(next)
    requestIdle(pumpIdleSections)
  }

  router.beforeEach((to) => {
    navigationStarted = performance.now()
    navigationPath = to.fullPath
    const warmed = warmedPaths.get(to.path)
    if (warmed !== undefined) {
      if (warmed) clearTimeout(warmed)
      warmedPaths.delete(to.path)
      metrics.recordPrefetchUsed()
    }
    navigationWarm = pathWarmState(to.path)
  })

  nuxtApp.hook('page:finish', () => {
    if (navigationStarted) {
      metrics.recordNavigation(navigationPath, performance.now() - navigationStarted, navigationWarm)
      navigationStarted = 0
    }
    visibleObserver?.disconnect()
    visibleBudget = 4
    visiblyWarmed.clear()
    requestAnimationFrame(observeVisibleTargets)
    requestIdle(pumpIdleSections)
  })

  function cancel() {
    if (timer) clearTimeout(timer)
    timer = null
    pendingHref = ''
  }

  function schedule(event: Event) {
    const target = targetFrom(event)
    if (!target) return
    const href = hrefFor(target)
    if (!href || href === pendingHref) return
    cancel()
    // Respect explicit data-saving for speculative hover. A press/focus still
    // warms immediately because navigation is then highly likely.
    if (connection?.saveData) return
    pendingHref = href
    timer = setTimeout(() => {
      timer = null
      warm(target)
    }, 100)
  }

  function immediate(event: Event) {
    const target = targetFrom(event)
    if (!target) return
    cancel()
    warm(target)
  }

  function leave(event: PointerEvent) {
    const target = targetFrom(event)
    const next = event.relatedTarget
    if (!target || (next instanceof Node && target.contains(next))) return
    if (hrefFor(target) === pendingHref) cancel()
  }

  document.addEventListener('pointerover', schedule, { passive: true })
  document.addEventListener('pointerout', leave, { passive: true })
  document.addEventListener('focusin', immediate, { passive: true })
  document.addEventListener('pointerdown', immediate, { passive: true })

  nuxtApp.vueApp.onUnmount(() => {
    cancel()
    for (const wasteTimer of warmedPaths.values()) if (wasteTimer) clearTimeout(wasteTimer)
    warmedPaths.clear()
    document.removeEventListener('pointerover', schedule)
    document.removeEventListener('pointerout', leave)
    document.removeEventListener('focusin', immediate)
    document.removeEventListener('pointerdown', immediate)
  })
})
