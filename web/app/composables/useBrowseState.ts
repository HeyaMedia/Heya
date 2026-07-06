// Per-page browse state for the library pages (movies / tv / books):
// view mode, sort, filters, selected sidebar library/view, and scroll offset.
//
// State lives in a module-scope store so it survives client-side navigation —
// open a movie, go back, and the page picks up exactly where it was. The
// display preferences (view/sort/filters) are mirrored to localStorage so
// they survive reloads. Scroll position is intentionally memory-only: after
// a reload the library may have changed and a stale offset is worse than
// starting at the top.
//
// The sidebar SELECTION (activeLib / activeView) is instead driven by the URL
// PATH — `/movies/library/<id>`, `/movies/loved`, `/movies/list/<id>`,
// `/movies/collection/<id>` (and the `/tv/...` equivalents). Every pick
// becomes a real history entry, so back/forward walks the selection chain
// instead of leaving the page. A bare `/movies` always means "All"; the last
// selection is deliberately NOT restored from localStorage (that would fight
// the address bar). A view supersedes a library (single-selection semantics).
//
// Those sub-paths all render the SAME index component as `/movies` — they're
// registered in `app/router.options.ts` with a shared `meta.key` so switching
// selection doesn't remount (and refetch) the page.

import type { FilterState } from '~~/shared/types'

const BROWSE_VIEWS = new Set(['grid', 'detail', 'list'])
const BROWSE_SORTS = new Set(['title', 'added', 'year-desc', 'year-asc', 'rating'])
const DEFAULT_SORT = 'title'

interface BrowseStore {
  view: string
  sort: string
  filters: FilterState
  activeLib: number | null
  activeView: string | null
  scrollTop: number
}

const stores = new Map<string, BrowseStore>()

function storageKey(page: string) {
  return `heya:browse:${page}`
}

function initialStore(page: string): BrowseStore {
  const base: BrowseStore = {
    view: 'grid',
    sort: DEFAULT_SORT,
    filters: defaultFilters(),
    activeLib: null,
    activeView: null,
    scrollTop: 0,
  }
  try {
    const raw = localStorage.getItem(storageKey(page))
    if (raw) {
      const p = JSON.parse(raw)
      if (BROWSE_VIEWS.has(p.view)) base.view = p.view
      if (BROWSE_SORTS.has(p.sort)) base.sort = p.sort
      if (p.filters && typeof p.filters === 'object') base.filters = { ...defaultFilters(), ...p.filters }
      // activeLib / activeView are intentionally NOT restored — they come
      // from the URL query (see useBrowseState below).
    }
  } catch { /* corrupt snapshot → defaults */ }
  return base
}

// ── Sidebar selection ⇄ URL path helpers ───────────────────────────────
// A view supersedes a library (single-selection semantics that match the
// sidebar's active-highlight), so a live view always drops any library.
interface Selection {
  activeLib: number | null
  activeView: string | null
}

function normalizeSelection(activeLib: number | null, activeView: string | null): Selection {
  return activeView ? { activeLib: null, activeView } : { activeLib, activeView: null }
}

export function useBrowseState(page: string, opts: { recommendedDefault?: boolean } = {}) {
  // When recommendedDefault is set (movies / tv), the bare `/movies` path is
  // the Recommended landing (activeView='recommended') and the flat grid moves
  // to `/movies/all`. Books opts out, so its bare `/books` stays the grid.
  const recommended = opts.recommendedDefault ?? false

  let store = stores.get(page)
  if (!store) {
    store = reactive(initialStore(page))
    stores.set(page, store)
  }
  const s = store

  const route = useRoute()
  const router = useRouter()
  const base = `/${page}`

  // Parse the sidebar selection out of the current path. Returns null when the
  // route isn't one of THIS page's browse views (e.g. a media detail page
  // mid-navigation) so the sync watchers below leave the store untouched
  // rather than reading it as "All" and hijacking the navigation.
  function selectionFromRoute(): Selection | null {
    if (route.path === base) return { activeLib: null, activeView: recommended ? 'recommended' : null }
    if (recommended && route.path === `${base}/all`) return { activeLib: null, activeView: null }
    if (route.path === `${base}/loved`) return { activeLib: null, activeView: 'loved' }
    if (route.path === `${base}/franchises`) return { activeLib: null, activeView: 'franchises' }
    const rest = route.path.startsWith(`${base}/`) ? route.path.slice(base.length + 1) : ''
    const m = /^(library|list)\/(\d+)$/.exec(rest)
    if (!m) return null
    const [, kind, id] = m
    // 'library' → activeLib; 'list' → the list-<id> activeView key. (A specific
    // franchise is its own /collection/:id page, not a browse selection.)
    return kind === 'library'
      ? { activeLib: Number(id), activeView: null }
      : { activeLib: null, activeView: `${kind}-${id}` }
  }

  function pathForSelection(sel: Selection): string {
    if (sel.activeView === 'recommended') return base
    if (sel.activeView === 'loved') return `${base}/loved`
    if (sel.activeView === 'franchises') return `${base}/franchises`
    if (sel.activeView?.startsWith('list-')) return `${base}/list/${sel.activeView.slice(5)}`
    if (sel.activeLib != null) return `${base}/library/${sel.activeLib}`
    // {null, null} is the flat "All" grid — its own path when Recommended owns
    // the bare route, otherwise the bare route itself (books).
    return recommended ? `${base}/all` : base
  }

  // Reconcile the store from the URL up front — synchronous and before the
  // watchers below register, so the first render already matches the address
  // bar (no stale-selection flash) without minting a history entry.
  {
    const sel = selectionFromRoute()
    if (sel) {
      s.activeLib = sel.activeLib
      s.activeView = sel.activeView
    }
  }

  // store → URL: a sidebar pick pushes a new history entry. Skip when the path
  // already reflects the selection so the URL→store writes below don't echo a
  // redundant push (which is what breaks the two-way sync into a loop), and
  // skip entirely when we've left the browse views so we can't hijack a
  // detail-page navigation.
  watch(
    () => [s.activeLib, s.activeView] as const,
    () => {
      if (!selectionFromRoute()) return
      const target = pathForSelection(normalizeSelection(s.activeLib, s.activeView))
      if (route.path === target) return
      router.push(target).catch(() => { /* redundant/aborted nav */ })
    },
  )

  // URL → store: back/forward (or a deep link / shared URL) re-applies it.
  watch(
    () => route.path,
    () => {
      const sel = selectionFromRoute()
      if (!sel) return
      if (sel.activeLib === s.activeLib && sel.activeView === s.activeView) return
      s.activeLib = sel.activeLib
      s.activeView = sel.activeView
    },
  )

  // Display preferences only — the selection is URL-driven (above).
  watch(
    () => ({ view: s.view, sort: s.sort, filters: s.filters }),
    (snap) => {
      try { localStorage.setItem(storageKey(page), JSON.stringify(snap)) } catch { /* private mode / quota — prefs just won't stick */ }
    },
    { deep: true },
  )

  const isDirty = computed(() => s.sort !== DEFAULT_SORT || hasActiveFilters(s.filters))

  function reset() {
    s.sort = DEFAULT_SORT
    s.filters = defaultFilters()
  }

  // Re-applies the saved scroll offset after remount. The virtualized grid
  // grows scrollHeight over the first few frames (column count lands via
  // ResizeObserver), so keep re-applying until the offset sticks.
  function restoreScroll(el: HTMLElement | null | undefined) {
    const top = s.scrollTop
    if (!el || top <= 0) return
    let tries = 0
    const attempt = () => {
      el.scrollTop = top
      if (Math.abs(el.scrollTop - top) < 2 || ++tries > 30) return
      requestAnimationFrame(attempt)
    }
    requestAnimationFrame(attempt)
  }

  return { ...toRefs(s), isDirty, reset, restoreScroll }
}
