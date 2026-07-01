// Per-page browse state for the library pages (movies / tv / books):
// view mode, sort, filters, selected sidebar library/view, and scroll offset.
//
// State lives in a module-scope store so it survives client-side navigation —
// open a movie, go back, and the page picks up exactly where it was. The
// preference half (view/sort/filters/activeLib/activeView) is also mirrored
// to localStorage so it survives reloads. Scroll position is intentionally
// memory-only: after a reload the library may have changed and a stale
// offset is worse than starting at the top.

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
      if (typeof p.activeLib === 'number') base.activeLib = p.activeLib
      if (typeof p.activeView === 'string') base.activeView = p.activeView
    }
  } catch { /* corrupt snapshot → defaults */ }
  return base
}

export function useBrowseState(page: string) {
  let store = stores.get(page)
  if (!store) {
    store = reactive(initialStore(page))
    stores.set(page, store)
  }
  const s = store

  watch(
    () => ({ view: s.view, sort: s.sort, filters: s.filters, activeLib: s.activeLib, activeView: s.activeView }),
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
