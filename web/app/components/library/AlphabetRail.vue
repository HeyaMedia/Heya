<template>
  <div
    ref="railEl"
    class="alpha-rail"
    :class="{ scrubbing }"
    :style="railStyle"
    @pointerdown="onDown"
    @pointermove="onMove"
    @pointerup="onUp"
    @pointercancel="onUp"
    @pointerleave="onLeave"
  >
    <!-- Shared glass shelf behind the magnified cluster: ONE blob riding
         the cursor instead of per-letter tiles (those collided into an
         overlapping mess at full magnification). -->
    <div v-if="magnet" class="alpha-blob" :style="{ top: `${magnet.y - magnet.railTop}px` }" />
    <!-- Floating letter bubble while scrubbing — the finger/cursor covers
         the rail itself. -->
    <div v-if="scrubbing && active" class="alpha-bubble" :style="{ top: `${bubbleY}px` }">{{ active }}</div>
    <span
      v-for="(l, i) in LETTERS"
      :key="l"
      class="alpha-l"
      :class="{ has: availableSet.has(l), on: l === active }"
      :style="letterStyle(i)"
    >{{ l }}</span>
  </div>
</template>

<script setup lang="ts">
// A–Z (+ # for digits/symbols) rail along the library grid's right edge —
// full page height, letters evenly distributed. Click a letter to jump;
// grab and drag to scrub. Hovering magnifies the letters near the cursor,
// macOS-Dock style, so the small glyphs are readable right where you're
// aiming. Letters with no titles render dimmed and don't jump. The PARENT
// owns the actual scrolling (it knows its virtualizer's row math) — this
// component just turns pointer geometry into `jump` events.
const LETTERS = ['#', ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ']

const props = defineProps<{
  /** Letters that actually exist in the current list. */
  available: string[]
}>()

const emit = defineEmits<{ jump: [letter: string] }>()

const availableSet = computed(() => new Set(props.available))
const railEl = ref<HTMLElement | null>(null)
const scrubbing = ref(false)
const active = ref('')
const bubbleY = ref(0)

// ── Below-the-FilterBar geometry ──
// The dock anchors at the scroll container's very top; the rail hangs
// BELOW the sticky FilterBar. The bar's height varies (the active-pills
// row comes and goes), so measure it live.
const barH = ref(64)
let barRO: ResizeObserver | null = null
onMounted(() => {
  const bar = railEl.value?.closest('.library-main')?.querySelector<HTMLElement>('.filter-bar')
  if (bar) {
    barRO = new ResizeObserver(() => { barH.value = bar.offsetHeight })
    barRO.observe(bar)
    barH.value = bar.offsetHeight
  }
})
onUnmounted(() => { barRO?.disconnect() })
const railStyle = computed(() => ({
  top: `${barH.value + 10}px`,
  height: `calc(100dvh - var(--topbar-h) - ${barH.value + 20}px)`,
}))

// ── Dock magnification ──
// One reactive snapshot per pointer move (rects measured once, not per
// letter): letters near the cursor scale up, bulge left, and push their
// neighbors apart along the rail so the grown glyphs never collide.
const magnet = ref<{ y: number; top: number; height: number; railTop: number } | null>(null)
const MAG_RADIUS = 72
const MAG_SCALE = 1.0 // added at the cursor → 2× peak
const MAG_SHIFT = 14 // px of leftward bulge at the cursor
const MAG_SPREAD = 9 // px of push-apart on the neighbors

function letterStyle(i: number) {
  const m = magnet.value
  if (!m) return undefined
  const step = m.height / LETTERS.length
  const center = m.top + step * (i + 0.5)
  const d = Math.abs(m.y - center)
  if (d > MAG_RADIUS) return undefined
  const t = Math.cos((d / MAG_RADIUS) * (Math.PI / 2))
  const dir = center === m.y ? 0 : center > m.y ? 1 : -1
  return {
    transform: `translate(${(-MAG_SHIFT * t).toFixed(1)}px, ${(dir * MAG_SPREAD * t).toFixed(1)}px) scale(${(1 + MAG_SCALE * t).toFixed(3)})`,
  }
}

// The band the letters actually occupy — NOT the rail's border box: the
// pill's vertical padding would skew the fraction math near either end,
// selecting a neighbor. offsetTop/offsetHeight are layout-truthful and
// ignore the magnification transforms (getBoundingClientRect on a scaled
// end letter would not).
function contentBox(): { top: number; height: number } | null {
  const el = railEl.value
  if (!el) return null
  const spans = el.getElementsByClassName('alpha-l') as HTMLCollectionOf<HTMLElement>
  const first = spans[0]
  const last = spans[spans.length - 1]
  if (!first || !last) return null
  const top = el.getBoundingClientRect().top + first.offsetTop
  const height = last.offsetTop + last.offsetHeight - first.offsetTop
  return height > 0 ? { top, height } : null
}

function trackMagnet(clientY: number) {
  const el = railEl.value
  const box = contentBox()
  if (!el || !box) return
  magnet.value = { y: clientY, top: box.top, height: box.height, railTop: el.getBoundingClientRect().top }
}

function letterAt(clientY: number): string | null {
  const box = contentBox()
  if (!box) return null
  const frac = Math.min(0.999, Math.max(0, (clientY - box.top) / box.height))
  return LETTERS[Math.floor(frac * LETTERS.length)] ?? null
}

function apply(clientY: number) {
  const el = railEl.value
  const l = letterAt(clientY)
  if (!el || !l || !availableSet.value.has(l)) return
  bubbleY.value = clientY - el.getBoundingClientRect().top
  if (l === active.value) return
  active.value = l
  emit('jump', l)
}

function onDown(e: PointerEvent) {
  scrubbing.value = true
  railEl.value?.setPointerCapture(e.pointerId)
  apply(e.clientY)
}
function onMove(e: PointerEvent) {
  trackMagnet(e.clientY)
  if (scrubbing.value) apply(e.clientY)
}
function onUp() {
  scrubbing.value = false
  // Let the landing letter glow a beat, then rest.
  setTimeout(() => { if (!scrubbing.value) active.value = '' }, 600)
}
function onLeave() {
  magnet.value = null
}
</script>

<style scoped>
/* Positioned by the pages' `.alpha-dock` (a zero-height sticky anchor at
   the top of the scroll container): the rail hangs in the padding band the
   container reserves via `.has-alpha-rail` — in flow between the content
   and the scrollbar, not floating over posters. top/height are inline
   (railStyle): they follow the measured FilterBar so the rail always sits
   BELOW the bar, in every scroll state. */
.alpha-rail {
  position: absolute;
  right: -32px;
  z-index: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 12px 4px;
  border-radius: 999px;
  background: color-mix(in oklab, var(--bg-2) 72%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  cursor: grab;
  user-select: none;
  touch-action: none;
  opacity: 0.7;
  transition: opacity 0.2s ease;
}
.alpha-rail:hover,
.alpha-rail.scrubbing { opacity: 1; }
.alpha-rail.scrubbing { cursor: grabbing; }

.alpha-l {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 18px;
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-4);
  /* Magnified letters bulge LEFT out of the pill over the shared blob.
     Short transition keeps the dock effect springy rather than twitchy. */
  position: relative;
  z-index: 1;
  transform-origin: right center;
  transition: transform 0.1s ease-out, color 0.12s ease;
  will-change: transform;
  /* The rail computes letters from pointer Y — spans stay inert. */
  pointer-events: none;
}
.alpha-l.has { color: var(--fg-2); }
.alpha-l.on { color: var(--gold); }

/* The shared glass shelf under the magnified cluster — one blob following
   the cursor, wide enough to back the leftward bulge. */
.alpha-blob {
  position: absolute;
  left: -22px;
  right: -2px;
  height: 150px;
  transform: translateY(-50%);
  border-radius: 24px;
  background: color-mix(in oklab, var(--bg-2) 90%, transparent);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  pointer-events: none;
}

.alpha-bubble {
  position: absolute;
  right: calc(100% + 14px);
  transform: translateY(-50%);
  width: 44px;
  height: 44px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 12px;
  font-size: 20px;
  font-weight: 700;
  color: var(--gold);
  background: color-mix(in oklab, var(--bg-2) 92%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-card);
  pointer-events: none;
}

/* Tablets/phones: no hover, and the strip fights edge-swipe gestures. */
@media (max-width: 1200px) {
  .alpha-rail { display: none; }
}
</style>
