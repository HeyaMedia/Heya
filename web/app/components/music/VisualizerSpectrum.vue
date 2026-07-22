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
  <canvas ref="canvasRef" class="viz-spec" :class="`viz-spec-${variant}`" aria-hidden="true" />
</template>

<script setup lang="ts">
const props = withDefaults(defineProps<{
  variant?: 'bars' | 'scope' | 'vu' | 'mini'
  // When false the render loop parks itself (used to pause the playbar meter
  // while audio is stopped, saving a needless rAF).
  active?: boolean
  // Freezes the render loop entirely and paints one deterministic "icon"
  // frame instead of live analyser data — the rAF never starts, so this
  // mount costs nothing while music plays in the background. Used for the
  // playbar's mini meter (it's a button glyph, not a live readout).
  static?: boolean
}>(), {
  variant: 'bars',
  active: true,
  static: false,
})

// The playbar uses this component as a frozen icon. It must not turn on the
// native PCM/FFT bridge merely by being mounted; only live visualizers create
// analyser demand.
const analyser = usePlaybackAnalyser({
  registerNativeDemand: !props.static,
  connectBrowserEngine: !props.static,
})
const canvasRef = ref<HTMLCanvasElement | null>(null)
const pageVisibility = useDocumentVisibility()
// `prefers-reduced-motion` — the global CSS reset (heya.css) doesn't reach a
// continuous rAF loop, so it's gated here explicitly. Stops the loop (one
// static frame stays drawn) rather than hiding the canvas outright.
const prefersReducedMotion = ref(false)
let motionMq: MediaQueryList | null = null
function onMotionChange(e: MediaQueryListEvent) { prefersReducedMotion.value = e.matches }
onMounted(() => {
  motionMq = window.matchMedia('(prefers-reduced-motion: reduce)')
  prefersReducedMotion.value = motionMq.matches
  motionMq.addEventListener('change', onMotionChange)
})
onUnmounted(() => motionMq?.removeEventListener('change', onMotionChange))
const shouldAnimate = computed(() => props.active && pageVisibility.value === 'visible' && !prefersReducedMotion.value)

// Theme colors resolved from CSS vars at mount; fall back to gold-ish defaults.
// Re-resolved live on the 'heya:theme' event (see useAppearance.ts) so a
// theme/accent switch doesn't leave the meter painting stale colors.
let gold = '#e6b94a'
let goldBright = '#f4cd6b'
// --ink / --shade are the theme's "glass on canvas" / "recessed on canvas"
// channels (heya.css) — space-separated RGB triplets, e.g. "255 255 255".
// Track backgrounds, LED gaps, and label text below all used to hardcode
// the dark-theme literal; resolving them means they follow light/OLED too.
let inkTriplet = '255 255 255'
let shadeTriplet = '0 0 0'
let dim = tripletRgba(inkTriplet, 0.10)
const clip = '#ff5b5b'

function tripletRgba(triplet: string, alpha: number): string {
  const parts = triplet.trim().split(/\s+/).join(', ')
  return `rgba(${parts}, ${alpha})`
}

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
  const ink = cs.getPropertyValue('--ink').trim()
  const shade = cs.getPropertyValue('--shade').trim()
  if (ink) inkTriplet = ink
  if (shade) shadeTriplet = shade
  dim = tripletRgba(inkTriplet, 0.10)
}

// Cached device-pixel canvas size, refreshed only by the ResizeObserver set up
// in onMounted below — NOT read from canvas.clientWidth/Height here, which
// would force a layout flush on every rAF tick (fitCanvas used to do exactly
// that; at 60fps that's a synchronous reflow per frame for a canvas that
// resizes maybe a handful of times per session).
let cachedW = 1
let cachedH = 1

function updateCachedSize(canvas: HTMLCanvasElement) {
  const dpr = Math.max(1, window.devicePixelRatio || 1)
  cachedW = Math.max(1, Math.floor(canvas.clientWidth * dpr))
  cachedH = Math.max(1, Math.floor(canvas.clientHeight * dpr))
}

function fitCanvas(canvas: HTMLCanvasElement) {
  if (canvas.width !== cachedW || canvas.height !== cachedH) {
    canvas.width = cachedW
    canvas.height = cachedH
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
  const data = analyser.getFrequencyData() // Float32 dBFS
  if (data.length < 2) return
  const bins = data.length
  const n = barCount.value
  const fftSize = analyser.fftSize()
  const sampleRate = analyser.sampleRate()
  if (!bandLo || bandN !== n || bandFft !== fftSize || bandSr !== sampleRate) {
    buildBands(n, fftSize, sampleRate)
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

// Fixed bar-height profile for the static (icon) frame — a pleasant
// double-humped spectrum shape, 0..1, sized for the 13-bar mini meter.
// Resampled below for other bar counts.
const STATIC_BAR_LEVELS = [0.24, 0.46, 0.7, 0.92, 0.66, 0.4, 0.22, 0.42, 0.7, 0.95, 0.74, 0.48, 0.26]

// Draws one deterministic frame through the exact same geometry/gradient code
// as drawBars, just sourcing bar heights from STATIC_BAR_LEVELS instead of the
// analyser — so the frozen playbar meter is visually indistinguishable from a
// paused live one.
function drawBarsStatic(ctx: CanvasRenderingContext2D, w: number, h: number) {
  const n = barCount.value
  const gap = props.variant === 'mini' ? 2 : 3
  const barW = (w - gap * (n - 1)) / n
  const minBarPx = props.variant === 'mini' ? 1 : 2

  for (let i = 0; i < n; i++) {
    const v = STATIC_BAR_LEVELS[Math.floor((i / n) * STATIC_BAR_LEVELS.length)] ?? 0
    const barH = Math.max(minBarPx, v * h)
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
  const data = analyser.getTimeDomainData()
  if (data.length < 2) return
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
  const data = analyser.getTimeDomainData()
  if (data.length < 2) return
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
  ctx.fillStyle = tripletRgba(inkTriplet, 0.06)
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
  ctx.fillStyle = tripletRgba(shadeTriplet, 0.5)
  const seg = Math.max(7, h * 0.55)
  for (let x = trackX + seg; x < trackX + trackW; x += seg) ctx.fillRect(x - 1, y, 2, h)
  ctx.restore()

  // Channel label + dB readout.
  const fs = Math.min(h * 0.66, 15)
  ctx.font = `600 ${fs}px ui-monospace, monospace`
  ctx.textBaseline = 'middle'
  ctx.fillStyle = tripletRgba(inkTriplet, 0.6)
  ctx.textAlign = 'left'
  ctx.fillText(label, h * 0.15, y + h / 2)
  ctx.textAlign = 'right'
  ctx.fillStyle = db > -1 ? clip : tripletRgba(inkTriplet, 0.5)
  ctx.fillText(db <= floor ? '−∞ dB' : `${db.toFixed(1)} dB`, w - h * 0.15, y + h / 2)
  ctx.textAlign = 'left'
}

function drawFrame() {
  if (cancelled) return
  const canvas = canvasRef.value
  if (canvas) {
    fitCanvas(canvas)
    const ctx = canvas.getContext('2d')
    if (ctx) {
      const w = canvas.width
      const h = canvas.height
      ctx.clearRect(0, 0, w, h)
      // Static mount (playbar mini meter): always the fixed colored profile,
      // never the dim flat-line baseline below — the button should read as
      // "alive" whether or not audio is currently playing.
      if (props.static) {
        drawBarsStatic(ctx, w, h)
      } else if (!props.active || !analyser.available.value) {
        // The direct-element engine (iOS compatibility mode, see
        // engine/directEngine.ts) has no AnalyserNode at all — draw the same
        // flat "present but silent" baseline as the inactive state instead of
        // leaving the canvas blank while audio is actually playing.
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
}

function startLoop() {
  // Static mounts never animate, regardless of active/visibility — they draw
  // one frame (see drawFrame) and stay parked forever.
  if (cancelled || rafId || !canvasRef.value || !shouldAnimate.value || props.static) return
  rafId = requestAnimationFrame(frame)
}

function stopLoop() {
  if (!rafId) return
  cancelAnimationFrame(rafId)
  rafId = 0
}

function frame() {
  rafId = 0
  drawFrame()
  startLoop()
}

// Theme/accent can change live (useAppearance.ts dispatches this after every
// applied change). While the rAF loop is running the next frame just picks
// up the reassigned color vars for free; when it's parked (paused/inactive
// meter) nothing else would repaint, so force one frame through explicitly.
function onThemeChange() {
  const canvas = canvasRef.value
  if (canvas) resolveColors(canvas)
  if (!rafId) drawFrame()
}

// Replaces the old per-frame clientWidth/Height read in fitCanvas: only
// recompute the cached device-pixel size when the canvas actually resizes,
// and push one redraw through so a resize while parked (static/paused)
// doesn't leave a stale frame at the old dimensions.
let resizeObserver: ResizeObserver | null = null

onMounted(() => {
  const canvas = canvasRef.value
  if (!canvas) return
  resolveColors(canvas)
  updateCachedSize(canvas)
  drawFrame()
  startLoop()
  window.addEventListener('heya:theme', onThemeChange)
  resizeObserver = new ResizeObserver(() => {
    const c = canvasRef.value
    if (!c) return
    updateCachedSize(c)
    if (!rafId) drawFrame()
  })
  resizeObserver.observe(canvas)
})
watch(shouldAnimate, (run) => {
  if (run) startLoop()
  else {
    stopLoop()
    if (pageVisibility.value === 'visible') drawFrame()
  }
})
onUnmounted(() => {
  cancelled = true
  stopLoop()
  window.removeEventListener('heya:theme', onThemeChange)
  resizeObserver?.disconnect()
  resizeObserver = null
})
</script>

<style scoped>
.viz-spec { display: block; width: 100%; height: 100%; }
.viz-spec-mini { width: 100%; height: 100%; }
</style>
