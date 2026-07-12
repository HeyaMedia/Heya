<!--
  VisualizerStarfield — audio-reactive warp-field.

  A perspective starfield flying toward the camera. Overall loudness (RMS) and
  low-band energy, read from the engine's shared time-domain PCM, push the warp
  speed — the field surges on transients and eases back to a slow drift when
  quiet. Ported from the plexmusicclient (hibiki) fullscreen visualizer. Pure
  Canvas2D over the shared AnalyserNode; no extra audio nodes. Draws a
  translucent black wash each frame instead of clearing, so fast stars leave
  motion-blur streaks (longer the louder it gets).
-->
<template>
  <canvas ref="canvasRef" class="viz-star" aria-hidden="true" />
</template>

<script setup lang="ts">
import type { AnalyserBridge } from '~/engine/analysis/analyserBridge'

// The graph engine (client, off-iOS) carries the analyser; the SSR stub and the
// direct-element engine (iOS) omit it — guard at use, stars just drift then.
const engine = useAudioEngine() as ReturnType<typeof useAudioEngine> & { analyserBridge?: AnalyserBridge }
const canvasRef = ref<HTMLCanvasElement | null>(null)
const pageVisibility = useDocumentVisibility()
// Speed + reactivity are user settings (persisted in useVisualizer), read live
// each frame so the popover sliders take effect instantly.
const vis = useVisualizer()

// --- Fixed tunables --------------------------------------------------------
const NUM_STARS = 800   // moving perspective stars
const NUM_STATIC = 300  // fixed dim background sky
const MAX_DEPTH = 1500
const FOV = 256

// JWST-inspired stellar/nebula hues; ~80% of stars stay blue-white (hue -1).
const STAR_HUES = [5, 15, 40, 50, 175, 190, 215, 225, 310, 330]
function randomHue(): number {
  return Math.random() < 0.2 ? STAR_HUES[Math.floor(Math.random() * STAR_HUES.length)]! : -1
}

interface Star { x: number; y: number; z: number; pz: number; hue: number }
interface StaticStar { sx: number; sy: number; brightness: number; size: number; hue: number }

let stars: Star[] | null = null
let staticStars: StaticStar[] | null = null
let starSpeed = 0
let dpr = 1

let rafId = 0
let cancelled = false
// `prefers-reduced-motion` — the global CSS reset (heya.css) doesn't reach a
// continuous rAF loop, so it's gated here explicitly. Stops the loop (last
// frame stays on screen) rather than blanking the canvas.
const prefersReducedMotion = ref(false)
let motionMq: MediaQueryList | null = null
function onMotionChange(e: MediaQueryListEvent) { prefersReducedMotion.value = e.matches }
onMounted(() => {
  motionMq = window.matchMedia('(prefers-reduced-motion: reduce)')
  prefersReducedMotion.value = motionMq.matches
  motionMq.addEventListener('change', onMotionChange)
})
onUnmounted(() => motionMq?.removeEventListener('change', onMotionChange))
const shouldAnimate = computed(() => pageVisibility.value === 'visible' && !prefersReducedMotion.value)

function fitCanvas(canvas: HTMLCanvasElement) {
  dpr = Math.max(1, window.devicePixelRatio || 1)
  const w = Math.max(1, Math.floor(canvas.clientWidth * dpr))
  const h = Math.max(1, Math.floor(canvas.clientHeight * dpr))
  if (canvas.width !== w || canvas.height !== h) {
    canvas.width = w
    canvas.height = h
    stars = null // re-seed at the new size on the next frame
    staticStars = null
  }
}

// (Re)seed both layers for the current canvas pixel size. Star coords scale with
// W/H so coverage is resolution-independent; visual sizes scale by dpr so they
// stay crisp on hi-DPI without the field spreading out.
function seed(W: number, H: number) {
  stars = Array.from({ length: NUM_STARS }, () => ({
    x: (Math.random() - 0.5) * W * 2,
    y: (Math.random() - 0.5) * H * 2,
    z: Math.random() * MAX_DEPTH,
    pz: Math.random() * MAX_DEPTH,
    hue: randomHue(),
  }))
  staticStars = Array.from({ length: NUM_STATIC }, () => ({
    sx: Math.random(),
    sy: Math.random(),
    brightness: 0.15 + Math.random() * 0.35,
    size: (0.3 + Math.random() * 0.5) * dpr,
    hue: randomHue(),
  }))
}

function draw(ctx: CanvasRenderingContext2D, W: number, H: number) {
  if (!stars || !staticStars) seed(W, H)
  const moving = stars!
  const bg = staticStars!

  // Loudness + low-band energy straight from the raw time-domain PCM — no FFT.
  // bass is a cheap proxy: mean-abs of the first 64 samples.
  let rms = 0
  let bass = 0
  const bridge = engine.analyserBridge
  if (bridge) {
    const pcm = bridge.getTimeDomainData()
    let sum = 0
    for (let i = 0; i < pcm.length; i++) sum += pcm[i]! * pcm[i]!
    rms = Math.sqrt(sum / pcm.length)
    const bn = Math.min(64, pcm.length)
    let bsum = 0
    for (let i = 0; i < bn; i++) bsum += Math.abs(pcm[i]!)
    bass = bsum / bn
  }

  // Smooth toward the target speed (EMA low-pass) so surges aren't jumpy.
  const target = vis.starfieldSpeed.value + (bass * 60 + rms * 30) * (vis.starfieldReactivity.value / 100)
  starSpeed += 0.15 * (target - starSpeed)
  const speed = starSpeed
  const cx = W / 2
  const cy = H / 2

  // Motion-trail wash: louder → lower alpha → longer streaks.
  ctx.fillStyle = `rgba(0,0,0,${Math.max(0.15, 0.4 - rms * 0.5)})`
  ctx.fillRect(0, 0, W, H)

  // Static background sky (never moves — just depth).
  for (let i = 0; i < bg.length; i++) {
    const st = bg[i]!
    ctx.fillStyle = st.hue >= 0
      ? `hsla(${st.hue}, 50%, ${50 + st.brightness * 30}%, ${st.brightness})`
      : `rgba(200,210,255,${st.brightness})`
    ctx.beginPath()
    ctx.arc(st.sx * W, st.sy * H, st.size, 0, Math.PI * 2)
    ctx.fill()
  }

  // Flying stars.
  for (let i = 0; i < moving.length; i++) {
    const s = moving[i]!
    s.pz = s.z
    s.z -= speed
    if (s.z <= 0) {
      // Recycle past the camera rather than reallocate.
      s.x = (Math.random() - 0.5) * W * 2
      s.y = (Math.random() - 0.5) * H * 2
      s.z = MAX_DEPTH
      s.pz = MAX_DEPTH
      s.hue = randomHue()
      continue
    }

    const sx = cx + (s.x / s.z) * FOV
    const sy = cy + (s.y / s.z) * FOV
    if (sx < -10 || sx > W + 10 || sy < -10 || sy > H + 10) continue

    const depthNorm = 1 - s.z / MAX_DEPTH
    const brightness = Math.min(1, depthNorm * depthNorm * 1.5)
    const size = Math.max(0.5, depthNorm * 3) * dpr

    // Warp streak from previous → current position when moving fast.
    if (speed > 2) {
      const px = cx + (s.x / s.pz) * FOV
      const py = cy + (s.y / s.pz) * FOV
      ctx.strokeStyle = s.hue >= 0
        ? `hsla(${s.hue}, 40%, 70%, ${brightness * 0.6})`
        : `rgba(200,210,255,${brightness * 0.6})`
      ctx.lineWidth = size * 0.6
      ctx.beginPath()
      ctx.moveTo(px, py)
      ctx.lineTo(sx, sy)
      ctx.stroke()
    }

    ctx.fillStyle = s.hue >= 0
      ? `hsla(${s.hue}, 70%, ${50 + brightness * 30}%, ${brightness})`
      : `rgba(220,230,255,${brightness})`
    ctx.beginPath()
    ctx.arc(sx, sy, size, 0, Math.PI * 2)
    ctx.fill()
  }
}

function drawFrame() {
  if (cancelled) return
  const canvas = canvasRef.value
  if (canvas) {
    fitCanvas(canvas)
    const ctx = canvas.getContext('2d')
    if (ctx) draw(ctx, canvas.width, canvas.height)
  }
}

function startLoop() {
  if (cancelled || rafId || !canvasRef.value || !shouldAnimate.value) return
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

onMounted(() => {
  if (!canvasRef.value) return
  drawFrame()
  startLoop()
})
watch(shouldAnimate, (run) => {
  if (run) startLoop()
  else stopLoop()
})
onUnmounted(() => {
  cancelled = true
  stopLoop()
})
</script>

<style scoped>
.viz-star { position: absolute; inset: 0; display: block; width: 100%; height: 100%; }
</style>
