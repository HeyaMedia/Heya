<template>
  <div
    ref="wrap"
    class="wf-wrap"
    role="slider"
    tabindex="0"
    :aria-label="ariaLabel"
    aria-valuemin="0"
    aria-valuemax="100"
    :aria-valuenow="valueNow"
    :aria-valuetext="ariaValueText"
    @pointerdown="onPointerDown"
    @pointermove="onPointerMove"
    @pointerleave="hoverPct = null"
    @keydown="onKeydown"
  >
    <canvas ref="canvas" class="wf-canvas" />
    <div v-if="hoverPct !== null" class="wf-hover" :style="{ left: hoverPct + '%' }" />
  </div>
</template>

<script setup lang="ts">
// Canvas-rendered waveform with click/drag-to-seek.
//
// Renders the entire waveform as vertical bars. The already-played
// region (left of progress) draws in the accent color; the rest
// stays in the neutral track color. A 1-pixel-wide hover ghost
// follows the cursor.
//
// Gracefully degrades: when `peaks` is empty, falls back to a flat
// neutral bar so the playbar layout doesn't jump on tracks that
// haven't been analyzed yet.
//
// Accessibility: the wrap is a keyboard-operable `role="slider"` (focusable,
// arrow/Home/End/PageUp-Down seek) with live aria-value* — pointer users get
// the canvas, keyboard/AT users get a real slider. Callers can pass a
// human-readable `valueText` (e.g. "1:23 of 3:45") for a nicer announcement
// than the bare percentage fallback.

const props = withDefaults(defineProps<{
  peaks: number[] | null
  progress: number    // 0..1
  ariaLabel?: string
  /** Overrides the aria-valuetext announcement (e.g. "1:23 of 3:45"). */
  valueText?: string | null
}>(), {
  ariaLabel: 'Seek',
  valueText: null,
})

const emit = defineEmits<{
  (e: 'seek', pct: number): void
}>()

const valueNow = computed(() => Math.round(Math.max(0, Math.min(1, props.progress)) * 100))
const ariaValueText = computed(() => props.valueText ?? `${valueNow.value}%`)

// Keyboard seek — the full WAI-ARIA slider key set. A focused slider OWNS all
// of these, so it handles both axes' arrows (↑/→ increase, ↓/← decrease),
// PageUp/Down for a larger step, and Home/End for the ends. Steps are in
// percent (the component has no duration, so % is the only unit here). Emits
// the same 0..1 fraction as a pointer seek.
//
// stopPropagation on every consumed key is load-bearing: the music shell mounts
// a WINDOW-level keydown hotkey listener (useGlobalHotkeys) that suppresses only
// for INPUT/TEXTAREA/SELECT/contenteditable — NOT for a role="slider" div. So
// without stopping propagation a focused waveform's ←/→ would ALSO fire the
// global ←/→ seek, and its ↑/↓ would ALSO fire the global ↑/↓ volume change —
// both wrong for a focused slider. When the waveform is NOT focused those
// global shortcuts still work; the slider only claims its keys while it has
// focus.
function onKeydown(e: KeyboardEvent) {
  const cur = Math.max(0, Math.min(1, props.progress))
  let next: number
  switch (e.key) {
    case 'ArrowRight':
    case 'ArrowUp': next = cur + 0.05; break
    case 'ArrowLeft':
    case 'ArrowDown': next = cur - 0.05; break
    case 'PageUp': next = cur + 0.1; break
    case 'PageDown': next = cur - 0.1; break
    case 'Home': next = 0; break
    case 'End': next = 1; break
    default: return
  }
  e.preventDefault()
  e.stopPropagation()
  emit('seek', Math.max(0, Math.min(1, next)))
}

const canvas = ref<HTMLCanvasElement | null>(null)
const wrap = ref<HTMLDivElement | null>(null)
const hoverPct = ref<number | null>(null)
const dragging = ref(false)

// The stored peaks are raw max-absolute amplitude, which sits near full-scale
// for loud/brickwalled masters — so a linear render pegs the whole strip. We
// normalize against the 95th-percentile peak (robust to a lone transient
// setting the max) and apply a gamma so the loud body gets pushed down and the
// envelope shows dynamics, then leave vertical headroom so nothing hits the
// ceiling.
const WF_GAMMA = 1.5
const WF_HEADROOM = 0.82

// Memoized per peaks array — recomputed only when the data changes, not on
// every progress tick.
const normRef = computed(() => {
  const p = props.peaks
  if (!p || p.length === 0) return 1
  const sorted = [...p].sort((a, b) => a - b)
  const idx = Math.min(sorted.length - 1, Math.floor(sorted.length * 0.95))
  return Math.max(0.05, sorted[idx] ?? 1)
})

function getCssVar(name: string, fallback: string): string {
  if (typeof getComputedStyle !== 'function' || !document.documentElement) return fallback
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return v || fallback
}

function draw() {
  const c = canvas.value
  const w = wrap.value
  if (!c || !w) return
  const dpr = window.devicePixelRatio || 1
  const cssW = w.clientWidth
  const cssH = w.clientHeight
  if (cssW === 0 || cssH === 0) return
  c.width = Math.round(cssW * dpr)
  c.height = Math.round(cssH * dpr)
  c.style.width = cssW + 'px'
  c.style.height = cssH + 'px'

  const ctx = c.getContext('2d')!
  ctx.clearRect(0, 0, c.width, c.height)

  const baseColor = getCssVar('--fg-3', '#666')
  const fillColor = getCssVar('--gold', '#d4af37')

  const peaks = props.peaks ?? []
  if (peaks.length === 0) {
    // Fallback: thin neutral line through the middle.
    const mid = c.height / 2
    ctx.fillStyle = baseColor
    ctx.fillRect(0, mid - dpr, c.width, dpr * 2)
    // Played portion
    const playedW = Math.round(c.width * Math.max(0, Math.min(1, props.progress)))
    ctx.fillStyle = fillColor
    ctx.fillRect(0, mid - dpr, playedW, dpr * 2)
    return
  }

  const barWPx = Math.max(1, Math.floor((c.width / peaks.length) - dpr))
  const gapPx = Math.max(0, Math.floor(c.width / peaks.length) - barWPx)
  const mid = c.height / 2
  const playedX = c.width * Math.max(0, Math.min(1, props.progress))
  const ref = normRef.value
  const maxH = (c.height - 2 * dpr) * WF_HEADROOM

  for (let i = 0; i < peaks.length; i++) {
    const x = i * (barWPx + gapPx)
    if (x >= c.width) break
    const norm = Math.min(1, Math.max(0, peaks[i] ?? 0) / ref)
    const h = Math.max(dpr, Math.pow(norm, WF_GAMMA) * maxH)
    const y = mid - h / 2
    ctx.fillStyle = x < playedX ? fillColor : baseColor
    ctx.fillRect(x, y, barWPx, h)
  }
}

onMounted(draw)
useResizeObserver(wrap, () => draw())

watch(() => [props.peaks, props.progress], () => draw(), { flush: 'post' })

// getCssVar() above already re-reads getComputedStyle on every draw() call —
// there's no color cache to invalidate here. The missing piece is a repaint
// trigger: theme/accent can change live (useAppearance.ts) without peaks or
// progress changing, so without this the bars keep painting the old colors
// until the next seek/resize.
useEventListener(window, 'heya:theme', () => draw())

function pctFromEvent(e: PointerEvent): number {
  const el = wrap.value
  if (!el) return 0
  const rect = el.getBoundingClientRect()
  return Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
}

function onPointerDown(e: PointerEvent) {
  dragging.value = true
  const p = pctFromEvent(e)
  emit('seek', p)
  ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
}

function onPointerMove(e: PointerEvent) {
  const p = pctFromEvent(e)
  hoverPct.value = p * 100
  if (dragging.value) emit('seek', p)
}

useEventListener(window, 'pointerup', () => { dragging.value = false })
</script>

<style scoped>
.wf-wrap {
  position: relative;
  flex: 1;
  height: 32px;
  cursor: pointer;
  user-select: none;
  touch-action: none;
}
/* Keyboard focus only — :focus-visible keeps mouse/touch interaction ring-free
   so the visual layout is unchanged for pointer users. */
.wf-wrap:focus { outline: none; }
.wf-wrap:focus-visible {
  outline: 2px solid var(--gold);
  outline-offset: 2px;
  border-radius: 3px;
}
.wf-canvas {
  display: block;
  width: 100%;
  height: 100%;
}
.wf-hover {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: rgb(var(--ink) / 0.4);
  pointer-events: none;
}
</style>
