<!--
  VisualizerSpectrum — lightweight 2D-canvas analyser visuals.

  Three variants off the same engine AnalyserNode:
    bars  — log-frequency spectrum, full-bleed (fullscreen mode)
    scope — oscilloscope / time-domain line (fullscreen mode)
    mini  — compact bar meter for the playbar ("VU")

  Reads the shared analyserBridge each frame; no extra audio nodes. Colors are
  pulled from the live CSS custom properties so it tracks the theme.
-->
<template>
  <canvas ref="canvasRef" class="viz-spec" :class="`viz-spec-${variant}`" />
</template>

<script setup lang="ts">
const props = withDefaults(defineProps<{
  variant?: 'bars' | 'scope' | 'vu' | 'mini'
  // When false the render loop parks itself (used to pause the playbar meter
  // while audio is stopped, saving a needless rAF).
  active?: boolean
}>(), {
  variant: 'bars',
  active: true,
})

import type { AnalyserBridge } from '~/engine/analysis/analyserBridge'
// Real engine (client) always carries the analyser; SSR stub omits it.
const engine = useAudioEngine() as ReturnType<typeof useAudioEngine> & { analyserBridge?: AnalyserBridge }
const canvasRef = ref<HTMLCanvasElement | null>(null)

// Theme colors resolved from CSS vars at mount; fall back to gold-ish defaults.
let gold = '#e6b94a'
let goldBright = '#f4cd6b'
let dim = 'rgba(255,255,255,0.10)'
const clip = '#ff5b5b'

// VU meter smoothing (attack fast, release slow) — per stereo channel.
let vuL = 0
let vuR = 0

let rafId = 0
let cancelled = false
const barCount = computed(() => (props.variant === 'mini' ? 13 : 96))

function resolveColors(el: HTMLElement) {
  const cs = getComputedStyle(el)
  const g = cs.getPropertyValue('--gold').trim()
  const gb = cs.getPropertyValue('--gold-bright').trim()
  if (g) gold = g
  if (gb) goldBright = gb || g
}

function fitCanvas(canvas: HTMLCanvasElement) {
  const dpr = Math.max(1, window.devicePixelRatio || 1)
  const w = Math.max(1, Math.floor(canvas.clientWidth * dpr))
  const h = Math.max(1, Math.floor(canvas.clientHeight * dpr))
  if (canvas.width !== w || canvas.height !== h) {
    canvas.width = w
    canvas.height = h
  }
}

// Per-bar smoothing state (attack/release EMA), reallocated when the bar count
// changes. Kept across frames so motion is fluid rather than jittery.
let barSmooth: Float32Array | null = null

// Log-frequency band → FFT-bin mapping, cached and rebuilt only when the bar
// count / fftSize / sampleRate change. Each bar covers a Hz range; we store its
// fractional bin edges + geometric-mean center bin.
const FMIN = 40      // Hz — start above DC/rumble so the far-left isn't a leakage pedestal
const FMAX = 20000   // Hz
const MIN_DB = -80   // absolute dB floor: below this a band reads as silence (no bar)
const MAX_DB = -20   // absolute dB ceiling: at/above this a band is full height
let bandLo: Float32Array | null = null
let bandHi: Float32Array | null = null
let bandCenter: Float32Array | null = null
let bandN = 0
let bandFft = 0
let bandSr = 0

function buildBands(n: number, fftSize: number, sampleRate: number) {
  const binHz = sampleRate / fftSize
  bandLo = new Float32Array(n)
  bandHi = new Float32Array(n)
  bandCenter = new Float32Array(n)
  for (let i = 0; i < n; i++) {
    const fLo = FMIN * Math.pow(FMAX / FMIN, i / n)
    const fHi = FMIN * Math.pow(FMAX / FMIN, (i + 1) / n)
    bandLo[i] = fLo / binHz
    bandHi[i] = fHi / binHz
    bandCenter[i] = Math.sqrt(fLo * fHi) / binHz // fractional bin
  }
  bandN = n
  bandFft = fftSize
  bandSr = sampleRate
}

// Level-accurate log-frequency spectrum (audioMotion-analyzer approach). Each
// bar aggregates the FFT bins inside its Hz band — MAX across integer bins where
// the band spans ≥1 bin (upper freqs), interpolated at the fractional center
// where it's sub-bin (low freqs, no bins to collapse onto → no flat bass shelf).
// The magnitude is mapped through a FIXED absolute dB window (−80…−20), so a
// silent band clamps to exactly 0 (no phantom bass) — never per-frame
// normalized. Same absolute path for the fullscreen bars and the mini meter, so
// the mini stays level-accurate too.
function drawBars(ctx: CanvasRenderingContext2D, w: number, h: number) {
  const bridge = engine.analyserBridge
  if (!bridge) return
  const data = bridge.getFrequencyData() // Float32 dBFS, length = fftSize/2
  const bins = data.length
  const n = barCount.value
  const node = bridge.analyserNode
  if (!bandLo || bandN !== n || bandFft !== node.fftSize || bandSr !== node.context.sampleRate) {
    buildBands(n, node.fftSize, node.context.sampleRate)
  }
  if (!barSmooth || barSmooth.length !== n) barSmooth = new Float32Array(n)
  const s = barSmooth
  const loArr = bandLo!, hiArr = bandHi!, cArr = bandCenter!
  const gap = props.variant === 'mini' ? 2 : 3
  const barW = (w - gap * (n - 1)) / n
  const minBarPx = props.variant === 'mini' ? 1 : 2

  for (let i = 0; i < n; i++) {
    const bLo = loArr[i]!, bHi = hiArr[i]!
    let db: number
    if (bHi - bLo >= 1) {
      // Wide band (upper freqs): MAX over the integer bins it spans (skip DC).
      const lo = Math.max(1, Math.ceil(bLo))
      const hi = Math.min(bins - 1, Math.floor(bHi))
      db = -Infinity
      for (let k = lo; k <= hi; k++) { const d = data[k]!; if (d > db) db = d }
      if (!Number.isFinite(db)) {
        const x = Math.max(1, Math.min(bins - 2, cArr[i]!))
        const f = Math.floor(x)
        db = data[f]! + (data[f + 1]! - data[f]!) * (x - f)
      }
    } else {
      // Sub-bin band (low freqs): interpolate at the fractional center bin.
      const x = Math.max(1, Math.min(bins - 2, cArr[i]!))
      const f = Math.floor(x)
      const dLo = data[f]!, dHi = data[f + 1]!
      db = dLo + (dHi - dLo) * (x - f)
    }
    if (!Number.isFinite(db)) db = MIN_DB

    // Absolute dB window → 0..1, then a perceptual sqrt (applied AFTER the window
    // so it can't lift the noise floor). Attack-fast / release-slow smoothing.
    let v = (db - MIN_DB) / (MAX_DB - MIN_DB)
    v = v < 0 ? 0 : v > 1 ? 1 : v
    v = Math.sqrt(v)
    s[i]! += (v > s[i]! ? 0.5 : 0.16) * (v - s[i]!)

    const barH = Math.max(minBarPx, s[i]! * h)
    const x = i * (barW + gap)
    const grad = ctx.createLinearGradient(0, h, 0, h - barH)
    grad.addColorStop(0, gold)
    grad.addColorStop(1, goldBright)
    ctx.fillStyle = grad
    roundRectBottom(ctx, x, h - barH, barW, barH, Math.min(barW / 2, 2))
    ctx.fill()
  }
}

function drawScope(ctx: CanvasRenderingContext2D, w: number, h: number) {
  const bridge = engine.analyserBridge
  if (!bridge) return
  const data = bridge.getTimeDomainData()
  const n = data.length
  ctx.lineWidth = Math.max(1.5, h / 240)
  ctx.strokeStyle = gold
  ctx.shadowColor = gold
  ctx.shadowBlur = 12
  ctx.beginPath()
  const step = w / (n - 1)
  for (let i = 0; i < n; i++) {
    const y = h / 2 + (data[i]! * h) / 2
    const x = i * step
    if (i === 0) ctx.moveTo(x, y)
    else ctx.lineTo(x, y)
  }
  ctx.stroke()
  ctx.shadowBlur = 0
}

function roundRectBottom(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) {
  ctx.beginPath()
  ctx.moveTo(x, y + r)
  ctx.arcTo(x, y, x + r, y, r)
  ctx.arcTo(x + w, y, x + w, y + r, r)
  ctx.lineTo(x + w, y + h)
  ctx.lineTo(x, y + h)
  ctx.closePath()
}

function roundRect(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) {
  const rr = Math.min(r, h / 2, w / 2)
  ctx.beginPath()
  ctx.moveTo(x + rr, y)
  ctx.arcTo(x + w, y, x + w, y + h, rr)
  ctx.arcTo(x + w, y + h, x, y + h, rr)
  ctx.arcTo(x, y + h, x, y, rr)
  ctx.arcTo(x, y, x + w, y, rr)
  ctx.closePath()
}

// Stereo LED-segment VU meter. AnalyserNode is a single (down-mixed) stream,
// so we split even/odd samples for a lively pseudo-stereo read rather than a
// true L/R measurement — it's a visual, not a calibration tool.
function drawVU(ctx: CanvasRenderingContext2D, w: number, h: number) {
  const bridge = engine.analyserBridge
  if (!bridge) return
  const data = bridge.getTimeDomainData()
  let sumL = 0, sumR = 0, n = 0
  for (let i = 0; i + 1 < data.length; i += 2) {
    sumL += data[i]! * data[i]!
    sumR += data[i + 1]! * data[i + 1]!
    n++
  }
  const rmsL = n ? Math.sqrt(sumL / n) : 0
  const rmsR = n ? Math.sqrt(sumR / n) : 0
  vuL += (rmsL > vuL ? 0.5 : 0.12) * (rmsL - vuL)
  vuR += (rmsR > vuR ? 0.5 : 0.12) * (rmsR - vuR)

  const floor = -48
  const barH = h * 0.15
  const gap = h * 0.06
  const top = (h - (barH * 2 + gap)) / 2
  drawMeter(ctx, w, top, barH, vuL, 'L', floor)
  drawMeter(ctx, w, top + barH + gap, barH, vuR, 'R', floor)
}

function drawMeter(ctx: CanvasRenderingContext2D, w: number, y: number, h: number, rms: number, label: string, floor: number) {
  const db = rms > 0 ? 20 * Math.log10(rms) : floor
  const frac = Math.max(0, Math.min(1, (db - floor) / -floor))
  const padL = Math.max(28, h * 0.9)
  const padR = Math.max(84, w * 0.09)
  const trackX = padL
  const trackW = Math.max(1, w - padL - padR)
  const radius = h * 0.22

  // Track background.
  ctx.fillStyle = 'rgba(255,255,255,0.06)'
  roundRect(ctx, trackX, y, trackW, h, radius)
  ctx.fill()

  // Clipped gradient fill + LED segment gaps.
  ctx.save()
  roundRect(ctx, trackX, y, trackW, h, radius)
  ctx.clip()
  const grad = ctx.createLinearGradient(trackX, 0, trackX + trackW, 0)
  grad.addColorStop(0, gold)
  grad.addColorStop(0.72, goldBright)
  grad.addColorStop(0.9, '#ffcf5a')
  grad.addColorStop(1, clip)
  ctx.fillStyle = grad
  ctx.fillRect(trackX, y, trackW * frac, h)
  ctx.fillStyle = 'rgba(0,0,0,0.5)'
  const seg = Math.max(7, h * 0.55)
  for (let x = trackX + seg; x < trackX + trackW; x += seg) ctx.fillRect(x - 1, y, 2, h)
  ctx.restore()

  // Channel label + dB readout.
  const fs = Math.min(h * 0.66, 15)
  ctx.font = `600 ${fs}px ui-monospace, monospace`
  ctx.textBaseline = 'middle'
  ctx.fillStyle = 'rgba(255,255,255,0.6)'
  ctx.textAlign = 'left'
  ctx.fillText(label, h * 0.15, y + h / 2)
  ctx.textAlign = 'right'
  ctx.fillStyle = db > -1 ? clip : 'rgba(255,255,255,0.5)'
  ctx.fillText(db <= floor ? '−∞ dB' : `${db.toFixed(1)} dB`, w - h * 0.15, y + h / 2)
  ctx.textAlign = 'left'
}

function frame() {
  if (cancelled) return
  const canvas = canvasRef.value
  if (canvas) {
    fitCanvas(canvas)
    const ctx = canvas.getContext('2d')
    if (ctx) {
      const w = canvas.width
      const h = canvas.height
      ctx.clearRect(0, 0, w, h)
      if (!props.active) {
        // Idle: draw a flat baseline so the meter reads "present but silent".
        ctx.fillStyle = dim
        ctx.fillRect(0, h - Math.max(1, h * 0.02), w, Math.max(1, h * 0.02))
      } else if (props.variant === 'scope') {
        drawScope(ctx, w, h)
      } else if (props.variant === 'vu') {
        drawVU(ctx, w, h)
      } else {
        drawBars(ctx, w, h)
      }
    }
  }
  rafId = requestAnimationFrame(frame)
}

onMounted(() => {
  const canvas = canvasRef.value
  if (!canvas) return
  resolveColors(canvas)
  rafId = requestAnimationFrame(frame)
})
onUnmounted(() => {
  cancelled = true
  cancelAnimationFrame(rafId)
})
</script>

<style scoped>
.viz-spec { display: block; width: 100%; height: 100%; }
.viz-spec-mini { width: 100%; height: 100%; }
</style>
