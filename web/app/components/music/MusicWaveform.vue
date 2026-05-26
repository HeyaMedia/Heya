<template>
  <div ref="wrap" class="wf-wrap" @pointerdown="onPointerDown" @pointermove="onPointerMove" @pointerleave="hoverPct = null">
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

const props = defineProps<{
  peaks: number[] | null
  progress: number    // 0..1
}>()

const emit = defineEmits<{
  (e: 'seek', pct: number): void
}>()

const canvas = ref<HTMLCanvasElement | null>(null)
const wrap = ref<HTMLDivElement | null>(null)
const hoverPct = ref<number | null>(null)
const dragging = ref(false)

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

  for (let i = 0; i < peaks.length; i++) {
    const x = i * (barWPx + gapPx)
    if (x >= c.width) break
    const peak = Math.max(0.01, Math.min(1, peaks[i] ?? 0))
    const h = peak * (c.height - 2 * dpr)
    const y = mid - h / 2
    ctx.fillStyle = x < playedX ? fillColor : baseColor
    ctx.fillRect(x, y, barWPx, h)
  }
}

onMounted(draw)
useResizeObserver(wrap, () => draw())

watch(() => [props.peaks, props.progress], () => draw(), { flush: 'post' })

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
  background: rgba(255, 255, 255, 0.4);
  pointer-events: none;
}
</style>
