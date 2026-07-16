import { useQueryCache, type UseQueryOptions } from '@pinia/colada'
import { collectionDetailQuery, personDetailQuery } from '~/queries/discovery'
import { mediaDetailQuery } from '~/queries/media'
import { musicAlbumDetailQuery, musicArtistDetailQuery, musicMixesQuery, playlistDetailQuery } from '~/queries/music'
import { enrichedCatalogQuery } from '~/queries/catalog'
import { toValue } from 'vue'

// Central route → data-query registry. NuxtLink already preloads route code;
// this plugin adds the critical API payload when pointer/focus intent is
// visible. New domains can join without every poster knowing cache details.
function queryForPath(pathname: string): UseQueryOptions<unknown> | null {
  const parts = pathname.split('/').filter(Boolean)
  const movieCatalogRoutes = new Set(['all', 'loved', 'library', 'list', 'franchises'])
  const tvCatalogRoutes = new Set(['all', 'loved', 'library', 'list'])
  if (parts[0] === 'movies' && parts[1] && movieCatalogRoutes.has(parts[1])) return enrichedCatalogQuery('movie')
  if (parts[0] === 'tv' && parts[1] && tvCatalogRoutes.has(parts[1])) return enrichedCatalogQuery('tv')
  if (parts[0] === 'movies' && ['recommendations', 'roulette', 'collection'].includes(parts[1] ?? '')) return null
  if (parts[0] === 'tv' && parts[1] === 'recommendations') return null
  if (parts[0] === 'movies' && parts[1]) return mediaDetailQuery(decodeURIComponent(parts[1]))
  if (parts[0] === 'tv' && parts[1]) return mediaDetailQuery(decodeURIComponent(parts[1]))
  if (parts[0] === 'books' && parts[1]) return mediaDetailQuery(decodeURIComponent(parts[1]))
  if (parts[0] === 'person' && parts[1]) return personDetailQuery(decodeURIComponent(parts[1]))
  if (parts[0] === 'collection' && parts[1]) {
    const id = Number(parts[1])
    return Number.isFinite(id) ? collectionDetailQuery(id) : null
  }
  if (parts[0] === 'music' && parts[1] === 'artist' && parts[2]) {
    const artist = decodeURIComponent(parts[2])
    if (parts[3] && parts[3] !== 'top-tracks') return musicAlbumDetailQuery({ artistSlug: artist, albumSlug: decodeURIComponent(parts[3]) })
    return musicArtistDetailQuery(artist)
  }
  if (parts[0] === 'music' && parts[1] === 'playlist' && parts[2]) {
    const id = Number(parts[2])
    return Number.isFinite(id) ? playlistDetailQuery(id) : null
  }
  if (parts[0] === 'music' && parts[1] === 'mix' && parts[2]) return musicMixesQuery()
  return null
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
  }).connection
  const canSpeculate = !connection?.saveData && !connection?.effectiveType?.includes('2g')
  const touchFirst = matchMedia('(hover: none)').matches

  function targetFrom(event: Event): HTMLElement | null {
    const target = event.target
    return target instanceof Element ? target.closest<HTMLElement>('a[href], [data-prefetch-to]') : null
  }

  function hrefFor(target: HTMLElement) {
    if (target instanceof HTMLAnchorElement) return target.href
    return target.dataset.prefetchTo ?? ''
  }

  function warm(target: HTMLElement) {
    const href = hrefFor(target)
    if (!href) return
    const url = new URL(href, location.href)
    if (url.origin !== location.origin) return
    void preloadRouteComponents(url.pathname)
    const options = queryForPath(url.pathname)
    if (!options) return
    if (warmedPaths.has(url.pathname)) return
    const existing = queryCache.get(toValue(options.key))
    const alreadyCached = existing?.state.value.status === 'success'
    metrics.recordPrefetch(alreadyCached)
    const entry = queryCache.ensure(options)
    void queryCache.refresh(entry).catch(() => {})
    const wasteTimer = alreadyCached ? null : setTimeout(() => {
      if (!warmedPaths.has(url.pathname)) return
      warmedPaths.delete(url.pathname)
      metrics.recordPrefetchWasted()
    }, 30_000)
    warmedPaths.set(url.pathname, wasteTimer)
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
      if (url.origin === location.origin && queryForPath(url.pathname)) visibleObserver.observe(target)
    }
  }

  router.beforeEach((to) => {
    navigationStarted = performance.now()
    navigationPath = to.fullPath
    const warmed = warmedPaths.get(to.path)
    if (warmed !== undefined) {
      if (warmed) clearTimeout(warmed)
      warmedPaths.delete(to.path)
      metrics.recordPrefetchUsed()
      const options = queryForPath(to.path)
      navigationWarm = options
        ? queryCache.get(toValue(options.key))?.state.value.status === 'success'
        : null
      return
    }
    const options = queryForPath(to.path)
    navigationWarm = options
      ? queryCache.get(toValue(options.key))?.state.value.status === 'success'
      : null
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
