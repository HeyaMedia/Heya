// Scroll-position memory for SPA navigations. The app's scroll container
// isn't `window` — the music shell uses `<main class="music-main scroll">`
// with `overflow-y: auto`, so Vue Router's built-in `savedPosition` (which
// addresses window scroll only) always returns {top: 0}. We need to track
// the inner element's scrollTop ourselves and restore it on back/forward.
//
// How it works:
//   1. `popstate` listener flips a flag so we know the next route change is
//      back/forward (not a fresh push).
//   2. `router.beforeEach` snapshots the *outgoing* route's scrollTop into
//      a Map keyed by fullPath.
//   3. `router.afterEach` schedules a restoration: on a pop nav we restore
//      the saved scrollTop for the *incoming* route; on a fresh push we
//      reset to 0 (top of the new page, matches standard SPA expectations).
//
// The nextTick + double-rAF is intentional. Pinia Colada hands us cached data
// synchronously on remount, so the new page typically renders in one frame
// — but image loads and async children can grow the content height across
// a few more frames. Double-rAF lets the layout settle before we set
// scrollTop, avoiding the "scroll clamped to short page height" bug.
//
// Scoped to `<main class="scroll">` — the HTML semantic element + the
// project's standard scroll class together uniquely identify the main
// content scroll container in any layout. The sidebar and other panels
// also use `.scroll`, so matching on the class alone picks the wrong
// element (and never restores anything useful).

function findScrollContainer(): HTMLElement | null {
  // Prefer the main content area. Falls back to any visible
  // `<main>` element if the .scroll class is missing on a future layout.
  const candidates: NodeListOf<HTMLElement> = document.querySelectorAll('main.scroll, main')
  for (const el of candidates) {
    if (el.offsetParent !== null && el.clientHeight > 0) return el
  }
  return null
}

export default defineNuxtPlugin((nuxtApp) => {
  const router = useRouter()
  const scrollMap = new Map<string, number>()

  // popstate fires before router.beforeEach for back/forward, so flipping
  // the flag here means the next nav cycle sees it as a pop.
  let isPopNav = false
  window.addEventListener('popstate', () => {
    isPopNav = true
  })

  router.beforeEach((to, from) => {
    if (from.fullPath) {
      const el = findScrollContainer()
      if (el) scrollMap.set(from.fullPath, el.scrollTop)
    }
  })

  router.afterEach((to) => {
    const wasPop = isPopNav
    isPopNav = false

    nextTick(() => {
      // Double-rAF: lets the new page's first paint settle (content
      // height stabilizes) before we set scrollTop, otherwise the
      // browser clamps a large saved value down to the current
      // (smaller) scrollable range.
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          const el = findScrollContainer()
          if (!el) return
          if (wasPop) {
            const saved = scrollMap.get(to.fullPath)
            if (saved != null) {
              el.scrollTop = saved
              return
            }
          }
          // Fresh push or no saved position — scroll to top of the new page.
          el.scrollTop = 0
        })
      })
    })
  })

  // Garbage-collection: cap the map size so it doesn't grow unboundedly
  // across long sessions. 200 entries × ~30 bytes/key is negligible RAM,
  // but past that we trim oldest 50 to keep things tidy.
  router.afterEach(() => {
    if (scrollMap.size > 200) {
      const keys = Array.from(scrollMap.keys()).slice(0, 50)
      for (const k of keys) scrollMap.delete(k)
    }
  })

  void nuxtApp
})
