// Overlay scrollbar — a custom floating thumb that rides ABOVE the content of
// a scroll region, so the content lays out edge-to-edge (no reserved gutter)
// and the bar fades away when it isn't needed.
//
// Why hand-rolled: the user's daily driver is Firefox, which never supported
// `overflow: overlay` and can't style `::-webkit-scrollbar`. The only
// cross-engine way to reclaim the gutter is to HIDE the native bar entirely
// (Firefox: `scrollbar-width: none`; Chromium + Safari<18.2:
// `::-webkit-scrollbar{display:none}` — both emitted together in heya.css) and
// paint our own thumb driven purely by scroll math. Native scrolling behavior
// (wheel, keyboard, touch momentum) is untouched — only the visual bar changes.
//
// Tri-engine notes:
//   • Chromium / Firefox / Safari(WebKit) all covered by the JS thumb.
//   • Safari rubber-band overscroll drives scrollTop < 0 and > max during the
//     elastic bounce — every read is CLAMPED into [0, max] before the thumb
//     geometry is computed so the thumb never shoots off the rail.
//   • Drag uses Pointer Events + setPointerCapture (Safari 13+), never
//     mouse events.
//   • Nothing load-bearing leans on overscroll-behavior (Safari 16+ only).
//
// Positioning: the rail is an absolutely-positioned child of the scroll element
// itself (so it inherits the element's own positioning context — works equally
// for a full-page scroller, a portaled sheet body, or a nested panel). An
// absolutely-positioned child of a scroll container scrolls WITH the content,
// so each frame we counter-translate the rail by the live scrollTop to keep it
// glued to the current viewport top. The thumb moves within that rail.

const MIN_THUMB = 32          // px — thumb never shrinks below this
const RAIL_WIDTH = 14         // px — hit area at the right edge (fine pointers)
const EDGE_ZONE = 20          // px — moving the cursor this close reveals the bar
const IDLE_MS = 900           // ms — fade out this long after the last activity

/** All live controllers, so the appearance knob can flip them en masse. */
const registry = new Set<OverlayScrollbarController>()

export interface OverlayScrollbarController {
  /** Force a geometry recompute (rarely needed by callers). */
  update(): void
  /** Enable/disable the custom bar (classic mode disables → native returns). */
  setEnabled(on: boolean): void
  /** Tear down: observers, listeners, injected DOM, mutated inline styles. */
  destroy(): void
  readonly el: HTMLElement
}

function clamp(v: number, lo: number, hi: number) {
  return v < lo ? lo : v > hi ? hi : v
}

/**
 * Attach an overlay scrollbar to a scroll element. Idempotent per element via
 * the WeakMap the plugin keeps; safe to call again after destroy().
 *
 * Interactivity (drag / rail-click / hover-reveal) is gated entirely in CSS by
 * `@media (hover: hover) and (pointer: fine)` + the `.is-visible` state, so the
 * listeners below are inert on touch (the rail/thumb stay pointer-events:none)
 * and the edge-reveal only fires for a real mouse. That keeps this correct
 * across hybrid devices without snapshotting the pointer type at attach time.
 */
export function createOverlayScrollbar(el: HTMLElement): OverlayScrollbarController {
  const doc = el.ownerDocument

  // Rail + thumb (decorative; kept out of the a11y tree and tab order).
  const rail = doc.createElement('div')
  rail.className = 'hos-rail'
  rail.setAttribute('aria-hidden', 'true')
  const thumb = doc.createElement('div')
  thumb.className = 'hos-thumb'
  rail.appendChild(thumb)

  // The rail anchors to the scroll element, which must establish a containing
  // block. Only touch `position` when it's static, and restore on destroy.
  let restorePosition = false
  if (getComputedStyle(el).position === 'static') {
    el.style.position = 'relative'
    restorePosition = true
  }
  el.classList.add('hos-managed')
  el.appendChild(rail)

  let enabled = true
  let overflowNow = false
  let dragging = false
  let pointerOverRail = false
  let rafId = 0
  let idleTimer: ReturnType<typeof setTimeout> | undefined
  // Cached viewport rect of the scroll element (right/top for edge detection).
  // Refreshed on every measure — cheap because it only reads on layout ticks.
  let elRight = 0
  let elTop = 0
  let elBottom = 0
  let thumbH = MIN_THUMB
  let railH = 0

  const prefersReducedMotion = () =>
    doc.documentElement.dataset.motion === 'reduced' ||
    window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true

  function measure() {
    rafId = 0
    const sh = el.scrollHeight
    const ch = el.clientHeight
    const rect = el.getBoundingClientRect()
    elRight = rect.right
    elTop = rect.top
    elBottom = rect.bottom

    overflowNow = enabled && sh - ch > 1
    if (!overflowNow) {
      rail.style.display = 'none'
      rail.classList.remove('is-visible')
      return
    }
    rail.style.display = ''

    railH = ch
    rail.style.height = `${ch}px`
    // Glue the rail to the live viewport top (actual scrollTop, so it stays
    // pinned even through Safari's elastic overscroll).
    rail.style.transform = `translateY(${el.scrollTop}px)`

    // Clamp scrollTop into range for the thumb math (Safari rubber-band).
    const max = sh - ch
    const st = clamp(el.scrollTop, 0, max)
    thumbH = clamp((ch / sh) * ch, MIN_THUMB, ch)
    const top = max > 0 ? (st / max) * (ch - thumbH) : 0
    thumb.style.height = `${thumbH}px`
    thumb.style.transform = `translateY(${top}px)`
  }

  function scheduleMeasure() {
    if (!rafId) rafId = requestAnimationFrame(measure)
  }

  function reveal() {
    if (!overflowNow) return
    rail.classList.add('is-visible')
    if (idleTimer) clearTimeout(idleTimer)
    if (!dragging && !pointerOverRail) idleTimer = setTimeout(hide, IDLE_MS)
  }
  function hide() {
    if (!dragging && !pointerOverRail) rail.classList.remove('is-visible')
  }

  // ── Listeners ────────────────────────────────────────────────────────────
  function onScroll() {
    scheduleMeasure()
    reveal()
  }

  // Edge proximity: summon the bar as a MOUSE cursor nears the right edge
  // (touch is excluded — it reveals via scroll activity instead). Uses the
  // cached rect, so no per-move layout read.
  function onPointerMove(e: PointerEvent) {
    if (!overflowNow || dragging || e.pointerType !== 'mouse') return
    if (
      e.clientX >= elRight - EDGE_ZONE && e.clientX <= elRight + 2 &&
      e.clientY >= elTop && e.clientY <= elBottom
    ) reveal()
  }

  function onRailEnter() { pointerOverRail = true; reveal() }
  function onRailLeave() {
    pointerOverRail = false
    if (idleTimer) clearTimeout(idleTimer)
    idleTimer = setTimeout(hide, IDLE_MS)
  }

  // Rail click (not on the thumb) → page-jump toward the click, native-like.
  function onRailPointerDown(e: PointerEvent) {
    if (e.target === thumb || !overflowNow) return
    const railRect = rail.getBoundingClientRect()
    const clickY = e.clientY - railRect.top
    const thumbTop = thumb.getBoundingClientRect().top - railRect.top
    const dir = clickY < thumbTop ? -1 : 1
    el.scrollBy({
      top: dir * el.clientHeight * 0.9,
      behavior: prefersReducedMotion() ? 'auto' : 'smooth',
    })
    e.preventDefault()
  }

  // Thumb drag (Pointer Events + capture; Safari 13+).
  let dragStartY = 0
  let dragStartScroll = 0
  function onThumbPointerDown(e: PointerEvent) {
    e.preventDefault()
    e.stopPropagation()
    dragging = true
    rail.classList.add('is-dragging', 'is-visible')
    dragStartY = e.clientY
    dragStartScroll = el.scrollTop
    try { thumb.setPointerCapture(e.pointerId) } catch { /* older engines */ }
    thumb.addEventListener('pointermove', onThumbPointerMove)
    thumb.addEventListener('pointerup', onThumbPointerUp)
    thumb.addEventListener('pointercancel', onThumbPointerUp)
  }
  function onThumbPointerMove(e: PointerEvent) {
    if (!dragging) return
    const range = el.clientHeight - thumbH
    if (range <= 0) return
    const scrollable = el.scrollHeight - el.clientHeight
    el.scrollTop = dragStartScroll + (e.clientY - dragStartY) * (scrollable / range)
  }
  function onThumbPointerUp(e: PointerEvent) {
    dragging = false
    rail.classList.remove('is-dragging')
    try { thumb.releasePointerCapture(e.pointerId) } catch { /* ignore */ }
    thumb.removeEventListener('pointermove', onThumbPointerMove)
    thumb.removeEventListener('pointerup', onThumbPointerUp)
    thumb.removeEventListener('pointercancel', onThumbPointerUp)
    if (idleTimer) clearTimeout(idleTimer)
    idleTimer = setTimeout(hide, IDLE_MS)
  }

  el.addEventListener('scroll', onScroll, { passive: true })
  // These are always attached; CSS keeps the rail/thumb non-hittable on touch
  // and while faded, so they simply never fire in those states.
  el.addEventListener('pointermove', onPointerMove, { passive: true })
  rail.addEventListener('pointerenter', onRailEnter)
  rail.addEventListener('pointerleave', onRailLeave)
  rail.addEventListener('pointerdown', onRailPointerDown)
  thumb.addEventListener('pointerdown', onThumbPointerDown)

  // Content grows/shrinks without a resize event (virtualized grids, late data,
  // view swaps): observe the element AND its children, plus child list changes.
  const ro = new ResizeObserver(scheduleMeasure)
  function observeTargets() {
    ro.disconnect()
    ro.observe(el)
    for (const child of Array.from(el.children)) {
      if (child !== rail) ro.observe(child)
    }
  }
  observeTargets()
  const mo = new MutationObserver(() => { observeTargets(); scheduleMeasure() })
  mo.observe(el, { childList: true })

  const onWinResize = () => scheduleMeasure()
  window.addEventListener('resize', onWinResize)

  scheduleMeasure()

  const controller: OverlayScrollbarController = {
    el,
    update: scheduleMeasure,
    setEnabled(on: boolean) {
      enabled = on
      if (!on) {
        rail.style.display = 'none'
        rail.classList.remove('is-visible', 'is-dragging')
      } else {
        scheduleMeasure()
      }
    },
    destroy() {
      registry.delete(controller)
      if (rafId) cancelAnimationFrame(rafId)
      if (idleTimer) clearTimeout(idleTimer)
      ro.disconnect()
      mo.disconnect()
      el.removeEventListener('scroll', onScroll)
      el.removeEventListener('pointermove', onPointerMove)
      window.removeEventListener('resize', onWinResize)
      rail.remove()
      el.classList.remove('hos-managed')
      if (restorePosition) el.style.position = ''
    },
  }
  registry.add(controller)
  return controller
}

/** Flip every live overlay scrollbar on/off (appearance knob → classic mode). */
export function setOverlayScrollbarsEnabled(on: boolean) {
  for (const c of registry) c.setEnabled(on)
}

/**
 * Component-facing composable: attach an overlay scrollbar to a template ref
 * for its lifetime. (Most coverage comes from the global `.scroll` auto-attach
 * in the plugin; this is for the odd bespoke scroller that opts in directly.)
 */
export function useOverlayScrollbar(target: Ref<HTMLElement | null | undefined>) {
  let controller: OverlayScrollbarController | null = null
  onMounted(() => {
    if (target.value) controller = createOverlayScrollbar(target.value)
  })
  onBeforeUnmount(() => { controller?.destroy(); controller = null })
}
