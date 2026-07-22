<!--
  TouchGestures — global touch affordances for the tablet/foldable + phone UI.

  Two gestures, both gated to coarse (touch) pointers, wired with a single set
  of document-level listeners:

  1. Near-left-edge swipe → opens the section sidebar on phones and in the
     compact band. Android owns the outermost edge for its system Back gesture
     and web content cannot request a native gesture-exclusion rect, so the
     activation band extends far enough inward to remain reachable there.

  2. Pull-to-refresh → pulling down while the page scroller is already at the top
     reveals a spinner; releasing past the threshold refetches the data behind
     the current page (Pinia Colada active observers) — fresh API data without a
     full navigation, so scroll position and app state survive.

  Overlays (visualizer, dialogs, lightbox) are Teleported outside `.app`, so
  gating pull-to-refresh on `target.closest('.app-main')` naturally excludes
  them — the gesture only ever engages over real page content.

  Client-only (`.client`): touch APIs don't exist on the server, and there's
  nothing to prerender — it renders only a transient indicator.
-->
<template>
  <template v-if="isCoarse">
    <div
      class="ptr"
      :class="{ 'ptr-on': pullY > 0 || refreshing }"
      :style="{ transform: `translate(-50%, ${Math.round(pullY)}px)`, opacity: progress }"
      aria-hidden="true"
    >
      <div class="ptr-ring">
        <span
          class="ptr-spinner"
          :class="{ spin: refreshing || pullY >= REFRESH_AT }"
          :style="refreshing ? undefined : { transform: `rotate(${progress * 360}deg)` }"
        />
      </div>
    </div>
    <!-- The visual indicator above is aria-hidden (it's a decorative ring
         that tracks the finger 1:1) — announce the refresh lifecycle to
         screen readers separately instead. -->
    <span class="sr-only" role="status" aria-live="polite">{{ liveMessage }}</span>
  </template>
</template>

<script setup lang="ts">
import { useQueryCache } from '@pinia/colada'

const { isPhone, isCoarse, isCompact } = useViewport()
const sidebar = useSectionSidebar()
const queryClient = useQueryCache()

// --- Tunables --------------------------------------------------------------
// Android's system Back gesture may consume a touch that begins on the
// physical edge before the browser can dispatch it. Accept starts through a
// wider near-edge band: 0–24px still works where the UA delivers it, while
// roughly 24–72px gives Android users an intentional just-inside-the-edge
// target without laying an element over page controls.
const EDGE_ZONE = 72        // max x-coordinate that arms the drawer swipe
const DIR_LOCK = 10         // px of travel before we commit to a gesture axis
const EDGE_OPEN = 48        // px rightward past which the sidebar opens
const PULL_RESIST = 0.5     // rubber-band factor on the pull distance
const PULL_MAX = 140        // px cap on the visible pull
const REFRESH_AT = 72       // px pull past which release triggers a refresh

// --- Indicator state (reactive) --------------------------------------------
const pullY = ref(0)
const refreshing = ref(false)
const progress = computed(() => Math.min(1, pullY.value / REFRESH_AT))
const liveMessage = ref('')

// --- Per-gesture scratch (non-reactive) ------------------------------------
let startX = 0
let startY = 0
let mode: 'idle' | 'ptr' | 'edge' | 'reject' = 'idle'
let edgeEligible = false
let ptrEligible = false
let scroller: HTMLElement | null = null
let moveAttached = false

function attachMove() {
  if (moveAttached) return
  window.addEventListener('touchmove', onMove, { passive: false })
  moveAttached = true
}

function detachMove() {
  if (!moveAttached) return
  window.removeEventListener('touchmove', onMove)
  moveAttached = false
}

// Nearest scrollable ancestor of the touch target — pull-to-refresh only
// engages when *that* element (not just the window) is scrolled to the top.
function scrollableAncestor(el: HTMLElement | null): HTMLElement | null {
  let node: HTMLElement | null = el
  while (node && node !== document.body) {
    const oy = getComputedStyle(node).overflowY
    if ((oy === 'auto' || oy === 'scroll') && node.scrollHeight > node.clientHeight) return node
    node = node.parentElement
  }
  return null
}

function atTop(): boolean {
  return scroller ? scroller.scrollTop <= 0 : window.scrollY <= 0
}

function onStart(e: TouchEvent) {
  detachMove()
  if (refreshing.value || e.touches.length !== 1) { mode = 'reject'; return }
  const t = e.touches[0]!
  startX = t.clientX
  startY = t.clientY
  mode = 'idle'

  const target = e.target as HTMLElement | null
  const inContent = !!target?.closest('.app-main')
  const inRail = !!target?.closest('.app-rail')

  edgeEligible = (isPhone.value || isCompact.value)
    && inContent
    && !!sidebar.kind.value
    && !sidebar.open.value
    && startX <= EDGE_ZONE
  scroller = inContent ? scrollableAncestor(target) : null
  // A rail drag owns its gesture. In particular, a slightly diagonal swipe
  // at page-top must not get axis-locked into pull-to-refresh.
  ptrEligible = inContent && !inRail && atTop()
  // Keep the scroll-blocking listener off the hot path entirely for ordinary
  // page/rail touches. It exists only during a gesture that may need to cancel
  // native scrolling.
  if (edgeEligible || ptrEligible) attachMove()
}

function onMove(e: TouchEvent) {
  if (mode === 'reject' || refreshing.value) return
  const t = e.touches[0]!
  const dx = t.clientX - startX
  const dy = t.clientY - startY

  // Lock to an axis/gesture once past the dead zone.
  if (mode === 'idle') {
    if (Math.abs(dx) < DIR_LOCK && Math.abs(dy) < DIR_LOCK) return
    if (edgeEligible && dx > 0 && Math.abs(dx) > Math.abs(dy)) mode = 'edge'
    else if (ptrEligible && dy > 0 && Math.abs(dy) > Math.abs(dx)) mode = 'ptr'
    else { mode = 'reject'; detachMove(); return }
  }

  if (mode === 'edge') {
    e.preventDefault()
    if (dx >= EDGE_OPEN) { sidebar.open.value = true; mode = 'reject'; detachMove() }
    return
  }

  if (mode === 'ptr') {
    // Bailed back above the top (or scrolled up) — hand control back to native.
    if (dy <= 0 || !atTop()) { pullY.value = 0; mode = 'reject'; detachMove(); return }
    e.preventDefault() // suppress the native overscroll/bounce while pulling
    pullY.value = Math.min(PULL_MAX, dy * PULL_RESIST)
  }
}

function onEnd() {
  detachMove()
  if (mode === 'ptr') {
    if (pullY.value >= REFRESH_AT) { void doRefresh(); return }
    pullY.value = 0 // animates back via the CSS transition
  }
  mode = 'idle'
}

async function doRefresh() {
  refreshing.value = true
  pullY.value = 56 // rest the spinner in view while queries refetch
  liveMessage.value = 'Refreshing…'
  try {
    // Refetch the data behind the current page (every active Colada query
    // observer) instead of a full navigation — keeps scroll + app state, just
    // pulls fresh API data. The 450ms floor stops the spinner flashing when a
    // refetch resolves instantly from a warm connection.
    await Promise.all([
      queryClient.invalidateQueries(),
      new Promise((resolve) => setTimeout(resolve, 450)),
    ])
  } catch { /* refetch failures surface through the pages themselves */ }
  refreshing.value = false
  pullY.value = 0
  liveMessage.value = 'Refreshed'
}

function attach() {
  // touchmove is attached lazily by onStart only for a gesture that may need
  // preventDefault; ordinary scrolling keeps a fully passive listener path.
  window.addEventListener('touchstart', onStart, { passive: true })
  window.addEventListener('touchend', onEnd, { passive: true })
  window.addEventListener('touchcancel', onEnd, { passive: true })
}
function detach() {
  window.removeEventListener('touchstart', onStart)
  detachMove()
  window.removeEventListener('touchend', onEnd)
  window.removeEventListener('touchcancel', onEnd)
}

onMounted(() => {
  // Track pointer coarseness reactively rather than sampling it once: a
  // one-shot mount check leaves the gesture permanently dead when coarse
  // arrives after mount (DevTools device-mode toggled on a loaded page,
  // a convertible switching modes) — the indicator would render via v-if
  // while the listeners were never attached.
  watch(isCoarse, (coarse, _, onCleanup) => {
    if (!coarse) return
    attach()
    onCleanup(() => {
      detach()
      pullY.value = 0
      mode = 'idle'
    })
  }, { immediate: true })
})
onUnmounted(detach)
</script>

<style scoped>
.ptr {
  position: fixed;
  top: var(--topbar-h);
  left: 50%;
  z-index: 45; /* above content, below the topbar (50) and overlays */
  margin-top: -44px; /* park it hidden just above the content area at rest */
  pointer-events: none;
  will-change: transform, opacity;
}
/* Only the release/settle animates; the live pull tracks the finger 1:1. */
.ptr:not(.ptr-on) { transition: transform 0.25s ease, opacity 0.25s ease; }

.ptr-ring {
  width: 36px; height: 36px;
  border-radius: 50%;
  display: grid; place-items: center;
  background: color-mix(in srgb, var(--bg-1) 92%, transparent);
  border: 1px solid var(--border);
  box-shadow: 0 8px 22px rgb(var(--shade) / 0.45);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
}
.ptr-spinner {
  width: 18px; height: 18px;
  border-radius: 50%;
  border: 2px solid var(--gold-soft);
  border-top-color: var(--gold);
}
.ptr-spinner.spin { animation: ptr-spin 0.7s linear infinite; }
@keyframes ptr-spin { to { transform: rotate(360deg); } }
</style>
