// Browser-history scroll restoration for the app's nested scroll containers.
// The document never scrolls: pages put their own overflow element below
// #main-content, and media rails add independent horizontal overflow elements.

type ScrollSnapshot = {
  top: number
  rails: Record<string, number>
}

function visible(el: HTMLElement) {
  return el.offsetParent !== null && el.clientWidth > 0 && el.clientHeight > 0
}

function pageScroller(): HTMLElement | null {
  const main = document.querySelector<HTMLElement>('#main-content')
  if (!main) return null

  // Pick the largest visible vertical overflow region inside the content
  // shell. This deliberately ignores sidebars, dialogs, and horizontal rails.
  const candidates = [main, ...main.querySelectorAll<HTMLElement>('.scroll')]
    .filter((el) => {
      if (!visible(el)) return false
      const overflow = getComputedStyle(el).overflowY
      return overflow === 'auto' || overflow === 'scroll'
    })
  return candidates.sort((a, b) =>
    b.clientWidth * b.clientHeight - a.clientWidth * a.clientHeight)[0] ?? null
}

function railElements(): Array<[string, HTMLElement]> {
  const counts = new Map<string, number>()
  return [...document.querySelectorAll<HTMLElement>('[data-scroll-memory]')]
    .filter(visible)
    .map((el) => {
      const base = el.dataset.scrollMemory || 'rail'
      const n = counts.get(base) ?? 0
      counts.set(base, n + 1)
      return [n ? `${base}:${n}` : base, el]
    })
}

function entryKey(fullPath: string): string {
  const state = history.state as { position?: number; key?: string } | null
  // Vue Router assigns a distinct position to every history entry. The
  // fallback keeps restoration working in browsers which omit that field.
  return state?.position != null
    ? `position:${state.position}`
    : state?.key
      ? `key:${state.key}`
      : `path:${fullPath}`
}

function capture(): ScrollSnapshot {
  const rails: Record<string, number> = {}
  for (const [key, el] of railElements()) rails[key] = el.scrollLeft
  return { top: pageScroller()?.scrollTop ?? 0, rails }
}

let restorationGeneration = 0

function restore(snapshot: ScrollSnapshot, generation: number) {
  let tries = 0
  const attempt = () => {
    if (generation !== restorationGeneration) return
    const vertical = pageScroller()
    if (vertical) vertical.scrollTop = snapshot.top

    const rails = railElements()
    for (const [key, el] of rails) {
      const left = snapshot.rails[key]
      if (left != null) el.scrollLeft = left
    }

    const verticalReady = snapshot.top === 0
      || (!!vertical && Math.abs(vertical.scrollTop - snapshot.top) < 2)
    const railsReady = Object.entries(snapshot.rails).every(([key, left]) => {
      if (left === 0) return true
      const el = rails.find(([candidate]) => candidate === key)?.[1]
      return !!el && Math.abs(el.scrollLeft - left) < 2
    })

    // Queries, virtualized tracks, and responsive measurements can grow the
    // scroll ranges after mount. Keep applying for roughly half a second.
    if ((!verticalReady || !railsReady) && ++tries < 40) requestAnimationFrame(attempt)
  }
  requestAnimationFrame(attempt)
}

export default defineNuxtPlugin(() => {
  const router = useRouter()
  const positions = new Map<string, ScrollSnapshot>()
  const pathFallback = new Map<string, ScrollSnapshot>()
  let isHistoryNavigation = false
  // On popstate, history.state already describes the incoming entry while the
  // DOM still belongs to the outgoing route. Retain the last settled key so
  // beforeEach never files the outgoing snapshot under the destination.
  let currentEntryKey = entryKey(router.currentRoute.value.fullPath)

  window.addEventListener('popstate', () => { isHistoryNavigation = true })

  router.beforeEach((_to, from) => {
    if (!from.fullPath) return
    const snapshot = capture()
    positions.set(currentEntryKey, snapshot)
    pathFallback.set(from.fullPath, snapshot)
  })

  router.afterEach((to) => {
    const generation = ++restorationGeneration
    const wasHistoryNavigation = isHistoryNavigation
    isHistoryNavigation = false
    const incomingEntryKey = entryKey(to.fullPath)
    currentEntryKey = incomingEntryKey

    nextTick(() => {
      if (wasHistoryNavigation) {
        const snapshot = positions.get(incomingEntryKey) ?? pathFallback.get(to.fullPath)
        if (snapshot) restore(snapshot, generation)
        return
      }
      // A new destination starts at the top. Its rails are newly mounted and
      // naturally begin at zero; shared route components need the explicit Y reset.
      requestAnimationFrame(() => {
        const vertical = pageScroller()
        if (vertical) vertical.scrollTop = 0
      })
    })

    if (positions.size > 200) {
      for (const key of [...positions.keys()].slice(0, 50)) positions.delete(key)
    }
    if (pathFallback.size > 200) {
      for (const key of [...pathFallback.keys()].slice(0, 50)) pathFallback.delete(key)
    }
  })
})
