// Pins a hero's art layer to the viewport band its section occupies at
// scroll 0, and clips its bottom edge by the scrolled distance — so the
// sharp art never moves while the page slides up over it, and the ledger
// strip right below the hero acts as a hard moving divider: fully sharp
// above the line, only the blurred ambient wash below it.
//
// Used by HeroCanvas (detail pages) and the home hero deck. The returned
// `align` is the geometry a v2 art claim publishes so AmbientBackdrop can
// draw the blur at exactly the hero's scale and offset (see ClaimAlign).
//
// Contract: `section` is the in-flow hero <section> that owns the band's
// geometry. Neither it nor its ancestors may carry a transform — that would
// re-anchor position:fixed and the band would scroll with the page.

import type { ClaimAlign } from './useBackground'

export function useHeroPin(
  section: () => HTMLElement | null,
  posY: () => number,
) {
  const heroH = ref(0)
  const heroTop = ref(0)
  const heroLeft = ref(0)
  const heroW = ref(0)
  const scrollClip = ref(0)
  let observer: ResizeObserver | null = null
  let scroller: HTMLElement | null = null

  function measure() {
    const el = section()
    if (!el) return
    const rect = el.getBoundingClientRect()
    heroH.value = el.clientHeight
    // Viewport Y at scroll 0 — rect.top drifts with scroll, so add the
    // scroller's current offset back to get the scroll-independent band.
    heroTop.value = rect.top + (scroller?.scrollTop ?? 0)
    // Horizontal band = the section's own box, NOT the viewport: pages with
    // a side column (music) must not have pinned art bleed under the menu.
    heroLeft.value = rect.left
    heroW.value = rect.width
  }

  function onScroll() {
    if (!scroller) return
    const next = Math.max(0, Math.min(heroH.value, Math.round(scroller.scrollTop)))
    if (next !== scrollClip.value) scrollClip.value = next
  }

  function teardown() {
    observer?.disconnect()
    observer = null
    scroller?.removeEventListener('scroll', onScroll)
    scroller = null
  }

  function setup(el: HTMLElement) {
    teardown()
    // The document never scrolls — find the page's own overflow container.
    let cur: HTMLElement | null = el.parentElement
    while (cur && cur !== document.body) {
      const overflow = getComputedStyle(cur).overflowY
      if (overflow === 'auto' || overflow === 'scroll') break
      cur = cur.parentElement
    }
    scroller = cur && cur !== document.body ? cur : null
    scroller?.addEventListener('scroll', onScroll, { passive: true })
    if (typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(measure)
      observer.observe(el)
    }
    measure()
    onScroll()
  }

  // Sections behind async data (`v-if` on the hero root) appear after mount —
  // track the element reactively rather than probing once.
  watch(() => section(), (el) => {
    if (el) setup(el)
    else {
      teardown()
      heroH.value = 0
    }
  })
  onMounted(() => {
    const el = section()
    if (el) setup(el)
  })
  onScopeDispose(teardown)

  // Fixed band once measured; before the first measure (and during SSR) the
  // caller's own static CSS (absolute inset-0) applies, which is exactly the
  // scroll-0 rendering anyway.
  const pinnedStyle = computed(() => {
    if (heroH.value <= 0 || heroW.value <= 0) return undefined
    const fullyClipped = scrollClip.value >= heroH.value
    return {
      position: 'fixed' as const,
      top: `${heroTop.value}px`,
      left: `${heroLeft.value}px`,
      width: `${heroW.value}px`,
      right: 'auto',
      bottom: 'auto',
      height: `${heroH.value}px`,
      // The moving sharp/blur divider: clip the band's bottom by exactly the
      // scrolled distance so the edge rides the ledger strip.
      clipPath: scrollClip.value > 0 ? `inset(0 0 ${scrollClip.value}px 0)` : undefined,
      visibility: fullyClipped ? 'hidden' as const : undefined,
    }
  })

  const align = computed<ClaimAlign | undefined>(() =>
    heroH.value > 0 && heroW.value > 0
      ? { heroH: heroH.value, heroTop: heroTop.value, heroW: heroW.value, posY: posY() }
      : undefined)

  return { pinnedStyle, align }
}
