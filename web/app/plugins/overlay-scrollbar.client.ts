// Wires the overlay scrollbar (useOverlayScrollbar.ts) into the app three ways:
//
//   1. Auto-attach to every `.scroll` element — the app's single vertical
//      scroll utility. The document itself never scrolls; each page and panel
//      puts its own `overflow-y:auto` region under `.scroll` (see the
//      scroll-container inventory in the port notes). Attaching here means
//      every page gets the floating thumb with ZERO per-page edits. Library
//      "All" grids scroll their `.library-main.scroll` ancestor (RecycleScroller
//      runs in page-mode, so that ancestor owns the scrollbar) — covered too.
//   2. A `v-overlay-scrollbar` directive for the rare bespoke scroller that
//      isn't tagged `.scroll` but still wants the treatment.
//   3. An observer on <html data-scrollbar> so the appearance "Classic" escape
//      hatch flips every live bar back to native (the hide-native CSS in
//      heya.css is gated on the same attribute, so natives return instantly).
//
// Native scroll bars on `.scroll` are hidden pre-paint by CSS (heya.css), so
// there's no flash of a native bar before this client plugin runs.

import {
  createOverlayScrollbar,
  setOverlayScrollbarsEnabled,
  type OverlayScrollbarController,
} from '~/composables/useOverlayScrollbar'

export default defineNuxtPlugin((nuxtApp) => {
  // el → its controller. Elements attached via the directive are tracked
  // separately so the `.scroll` rescan never adopts (or reaps) them.
  const managed = new WeakMap<HTMLElement, OverlayScrollbarController>()
  const autoAttached = new Set<HTMLElement>()
  const directiveManaged = new WeakSet<HTMLElement>()

  function attach(el: HTMLElement, viaDirective: boolean) {
    if (managed.has(el)) return
    const controller = createOverlayScrollbar(el)
    managed.set(el, controller)
    if (viaDirective) directiveManaged.add(el)
    else autoAttached.add(el)
  }

  function detach(el: HTMLElement) {
    managed.get(el)?.destroy()
    managed.delete(el)
    autoAttached.delete(el)
    directiveManaged.delete(el)
  }

  // Rescan is O(number of .scroll elements), not O(mutations): a debounced
  // sweep that adopts new `.scroll` roots and reaps auto-attached ones that
  // have left the DOM. The observer below only requests one when a changed
  // subtree can actually add/remove a `.scroll` root, so virtualized tile churn
  // never turns into repeated whole-document queries.
  let rescanScheduled = false
  function rescan() {
    rescanScheduled = false
    for (const node of document.querySelectorAll<HTMLElement>('.scroll')) {
      if (!managed.has(node)) attach(node, false)
    }
    for (const el of [...autoAttached]) {
      if (!el.isConnected) detach(el)
    }
  }
  function scheduleRescan() {
    if (rescanScheduled) return
    rescanScheduled = true
    requestAnimationFrame(rescan)
  }

  function containsScrollRoot(node: Node) {
    return node instanceof Element
      && (node.matches('.scroll') || node.querySelector('.scroll') !== null)
  }

  // One subtree observer for the whole app. Ignore mutations that cannot
  // affect the set of vertical scroll roots (rail cards, badges, images, etc.).
  const mo = new MutationObserver((mutations) => {
    for (const mutation of mutations) {
      if ([...mutation.addedNodes, ...mutation.removedNodes].some(containsScrollRoot)) {
        scheduleRescan()
        return
      }
    }
  })
  mo.observe(document.body, { childList: true, subtree: true })

  // Initial + per-navigation sweeps (belt-and-braces alongside the observer).
  nuxtApp.hook('app:mounted', scheduleRescan)
  nuxtApp.hook('page:finish', scheduleRescan)

  // ── Directive: explicit opt-in for non-`.scroll` scrollers ────────────────
  nuxtApp.vueApp.directive('overlay-scrollbar', {
    mounted(el: HTMLElement) {
      if (!managed.has(el)) attach(el, true)
    },
    unmounted(el: HTMLElement) {
      if (directiveManaged.has(el)) detach(el)
    },
  })

  // ── Classic escape hatch: <html data-scrollbar="classic"> ────────────────
  const root = document.documentElement
  const syncMode = () => setOverlayScrollbarsEnabled(root.dataset.scrollbar !== 'classic')
  syncMode()
  const attrObserver = new MutationObserver(syncMode)
  attrObserver.observe(root, { attributes: true, attributeFilter: ['data-scrollbar'] })
})
