<!--
  VisualizerMilkdrop — the butterchurn (Milkdrop) WebGL renderer.

  Taps the engine's shared AnalyserNode (already sitting at the tail of the
  signal chain, so it sees the fully-processed post-EQ/limiter mix) and drives
  a full-bleed canvas. Preset navigation + auto-cycle are exposed so a host
  (VisualizerFullscreen) can wire buttons/hotkeys to them.

  Client-only in practice: butterchurn is dynamically imported in onMounted,
  which never runs during SSR.
-->
<template>
  <div class="viz-mk">
    <div v-if="error" class="viz-mk-error">{{ error }}</div>
    <canvas v-show="!error" ref="canvasRef" class="viz-mk-canvas" aria-hidden="true" />
  </div>
</template>

<script setup lang="ts">
import type { Visualizer } from 'butterchurn'
import type { AnalyserBridge } from '~/engine/analysis/analyserBridge'
import { getAudioContext } from '~/engine/context'

const vis = useVisualizer()
const player = usePlayerBindings()
// useAudioEngine() returns a union with an SSR stub that omits the analyser;
// the real engine (client) always has it. Narrow via cast, guard at use.
const engine = useAudioEngine() as ReturnType<typeof useAudioEngine> & { analyserBridge?: AnalyserBridge }

const canvasRef = ref<HTMLCanvasElement | null>(null)
const error = ref('')
const ready = ref(false)
const pageVisibility = useDocumentVisibility()
// `prefers-reduced-motion` — the global CSS reset (heya.css) doesn't reach a
// continuous rAF loop, so it's gated here explicitly. Stops the render loop
// (last WebGL frame stays on screen) rather than tearing the canvas down.
const prefersReducedMotion = ref(false)
let motionMq: MediaQueryList | null = null
function onMotionChange(e: MediaQueryListEvent) { prefersReducedMotion.value = e.matches }
onMounted(() => {
  motionMq = window.matchMedia('(prefers-reduced-motion: reduce)')
  prefersReducedMotion.value = motionMq.matches
  motionMq.addEventListener('change', onMotionChange)
})
onUnmounted(() => motionMq?.removeEventListener('change', onMotionChange))
const shouldAnimate = computed(() => ready.value && pageVisibility.value === 'visible' && !prefersReducedMotion.value)

let visualizer: Visualizer | null = null
let presets: Record<string, object> = {}
let presetKeys: string[] = []
let presetIndex = 0

function applyPresetByKey(key: string, blend = 2.0) {
  const preset = presets[key]
  if (!visualizer || !preset) return
  presetIndex = presetKeys.indexOf(key)
  visualizer.loadPreset(preset, blend)
  vis.setCurrentPreset(key)
}
function loadPreset(name: string) { applyPresetByKey(name) }

// The pool nav walks: favorites (∩ available) when "liked only" is on and any
// exist, otherwise the full preset set. Read fresh each call so toggling the
// mode or (un)favoriting takes effect immediately, including for auto-cycle.
function currentPool(): string[] {
  if (vis.likedOnly.value) {
    const favs = presetKeys.filter((k) => vis.favoritePresets.value.includes(k))
    if (favs.length) return favs
  }
  return presetKeys
}
function nextPreset() {
  const pool = currentPool()
  if (!pool.length) return
  const i = pool.indexOf(presetKeys[presetIndex] ?? '')
  applyPresetByKey(pool[(i + 1) % pool.length]!)
}
function prevPreset() {
  const pool = currentPool()
  if (!pool.length) return
  const i = pool.indexOf(presetKeys[presetIndex] ?? '')
  // i===-1 (current not in pool) → wrap to the last entry.
  applyPresetByKey(pool[(i - 1 + pool.length) % pool.length]!)
}
function randomPreset() {
  const pool = currentPool()
  if (!pool.length) return
  if (pool.length === 1) { applyPresetByKey(pool[0]!); return }
  const cur = presetKeys[presetIndex]
  let pick = cur
  let guard = 0
  while ((pick === cur || pick === undefined) && guard++ < 12) {
    pick = pool[Math.floor(Math.random() * pool.length)]
  }
  if (pick) applyPresetByKey(pick)
}
function presetNames() { return presetKeys }

defineExpose({ nextPreset, prevPreset, randomPreset, loadPreset, presetNames })

// --- Auto-cycle ------------------------------------------------------------
let autoTimer: ReturnType<typeof setInterval> | null = null
watch(
  [vis.autoCycleEnabled, vis.autoCycleIntervalSec, vis.autoCycleMode, shouldAnimate] as const,
  ([enabled, intervalSec, mode, canAnimate]) => {
    if (autoTimer) { clearInterval(autoTimer); autoTimer = null }
    if (!enabled || !canAnimate) return
    autoTimer = setInterval(() => {
      if (mode === 'sequential') nextPreset()
      else randomPreset()
    }, intervalSec * 1000)
  },
  { immediate: true },
)

// --- Render-scale live re-size ---------------------------------------------
function sizeCanvas() {
  const canvas = canvasRef.value
  if (!canvas || !visualizer) return
  const dpr = Math.max(1, (window.devicePixelRatio || 1) * vis.renderScale.value)
  const w = Math.floor(canvas.clientWidth * dpr)
  const h = Math.floor(canvas.clientHeight * dpr)
  if (w === 0 || h === 0) return
  canvas.width = w
  canvas.height = h
  visualizer.setRendererSize(w, h)
}
watch(vis.renderScale, () => sizeCanvas())

// --- Lifecycle -------------------------------------------------------------
let rafId = 0
let resizeObserver: ResizeObserver | null = null
let cancelled = false

function stopRenderLoop() {
  if (!rafId) return
  cancelAnimationFrame(rafId)
  rafId = 0
}

function startRenderLoop() {
  if (cancelled || rafId || !visualizer || !shouldAnimate.value) return
  rafId = requestAnimationFrame(render)
}

function render() {
  rafId = 0
  if (cancelled || !shouldAnimate.value) return
  try { visualizer?.render() } catch { return }
  startRenderLoop()
}

onMounted(async () => {
  const canvas = canvasRef.value
  if (!canvas) return

  if (player.playbackBackend.value === 'native') {
    error.value = 'Milkdrop requires WebAudio. Spectrum, scope, VU, and starfield support native playback.'
    return
  }

  const audioCtx = getAudioContext()
  const analyser = engine.analyserBridge?.analyserNode
  if (!audioCtx || !analyser) { error.value = 'No audio context available'; return }

  try {
    // The bare `butterchurn-presets` specifier resolves to the package `main`,
    // which is only the 100-preset base pack. Merge in the MD1 + Extra + Extra2
    // packs (all shipped in the same package) for the full ~395-preset library.
    // Each pack is a separate lazy chunk, only pulled when the visualizer opens.
    const [bcMod, baseMod, md1Mod, extraMod, extra2Mod] = await Promise.all([
      import('butterchurn'),
      import('butterchurn-presets'),
      import('butterchurn-presets/lib/butterchurnPresetsMD1.min.js'),
      import('butterchurn-presets/lib/butterchurnPresetsExtra.min.js'),
      import('butterchurn-presets/lib/butterchurnPresetsExtra2.min.js'),
    ])
    if (cancelled) return

    const butterchurn = bcMod.default ?? bcMod
    // Each pack ships as either a flat name→preset Record or an object exposing
    // getPresets(); normalize both, then spread-merge (later packs win on any
    // duplicate name — harmless, same preset).
    const packToMap = (mod: { default?: unknown }): Record<string, object> => {
      const raw = (mod.default ?? mod) as Record<string, object> & { getPresets?: () => Record<string, object> }
      return typeof raw.getPresets === 'function' ? raw.getPresets() : raw
    }
    presets = { ...packToMap(baseMod), ...packToMap(md1Mod), ...packToMap(extraMod), ...packToMap(extra2Mod) }
    presetKeys = Object.keys(presets).sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }))
    if (!presetKeys.length) { error.value = 'No visualizer presets available'; return }

    const dpr = Math.max(1, (window.devicePixelRatio || 1) * vis.renderScale.value)
    const W = Math.max(1, Math.floor(canvas.clientWidth * dpr))
    const H = Math.max(1, Math.floor(canvas.clientHeight * dpr))
    canvas.width = W
    canvas.height = H

    try {
      visualizer = butterchurn.createVisualizer(audioCtx, canvas, { width: W, height: H })
    } catch (err) {
      error.value = `WebGL unavailable: ${String(err)}`
      return
    }
    visualizer.connectAudio(analyser)

    // Restore the last preset if it still exists; otherwise start random.
    const stored = vis.currentPresetName.value
    const storedPreset = stored ? presets[stored] : undefined
    if (stored && storedPreset) {
      presetIndex = presetKeys.indexOf(stored)
      visualizer.loadPreset(storedPreset, 0)
    } else {
      presetIndex = Math.floor(Math.random() * presetKeys.length)
      const key = presetKeys[presetIndex]!
      const p = presets[key]
      if (p) { visualizer.loadPreset(p, 0); vis.setCurrentPreset(key) }
    }

    ready.value = true

    startRenderLoop()

    resizeObserver = new ResizeObserver(() => sizeCanvas())
    resizeObserver.observe(canvas)
  } catch (err) {
    if (!cancelled) error.value = `Failed to load visualizer: ${String(err)}`
  }
})

watch(shouldAnimate, (run) => {
  if (run) startRenderLoop()
  else stopRenderLoop()
})

onUnmounted(() => {
  cancelled = true
  ready.value = false
  stopRenderLoop()
  resizeObserver?.disconnect()
  if (autoTimer) clearInterval(autoTimer)
  const analyser = engine.analyserBridge?.analyserNode
  if (visualizer && analyser) {
    try { visualizer.disconnectAudio(analyser) } catch { /* already gone */ }
  }
  visualizer = null
})
</script>

<style scoped>
.viz-mk { position: absolute; inset: 0; overflow: hidden; }
.viz-mk-canvas { position: absolute; inset: 0; width: 100%; height: 100%; display: block; }
.viz-mk-error {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  font-size: 14px;
  text-align: center;
  padding: 24px;
}
</style>
