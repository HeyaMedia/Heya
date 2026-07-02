<template>
  <div class="hero-game" ref="rootEl">
    <canvas ref="canvasEl" class="hero-game-canvas" />
    <div class="hero-game-hud">
      <span class="hud-score">{{ Math.floor(score) }}</span>
      <span class="hud-best">BEST {{ Math.floor(best) }}</span>
      <span class="hud-hint">←/→ dodge · Esc quit</span>
    </div>
    <div v-if="over" class="hero-game-over">
      <div class="go-title">CAUGHT BY THE BACKLOG</div>
      <div class="go-score">{{ Math.floor(score) }}<span v-if="isBest" class="go-best"> · new best</span></div>
      <div class="go-hint">Enter to retry · Esc to quit</div>
    </div>
  </div>
</template>

<script setup lang="ts">
// Konami-code easter egg: dodge your own library's falling posters for as
// long as you can. Pure client-side — the only persistence is the local
// high score. Reached from HeroDeck via ↑↑↓↓←→←→BA.
const props = defineProps<{ posters: string[] }>()
const emit = defineEmits<{ exit: [] }>()

const rootEl = ref<HTMLElement>()
const canvasEl = ref<HTMLCanvasElement>()
const score = ref(0)
const best = ref(0)
const over = ref(false)
const isBest = ref(false)

const BEST_KEY = 'heya-poster-dodge-best'

type Faller = { x: number; y: number; w: number; h: number; v: number; spin: number; angle: number; img: HTMLImageElement | null }

let ctx: CanvasRenderingContext2D | null = null
let raf = 0
let last = 0
let W = 0
let H = 0
let playerX = 0
let vx = 0
const keys = { left: false, right: false }
let fallers: Faller[] = []
let spawnIn = 0
let elapsed = 0
const images: HTMLImageElement[] = []

const PLAYER_W = 44
const PLAYER_H = 66

function loadImages() {
  for (const src of props.posters.slice(0, 12)) {
    const img = new Image()
    img.src = src
    images.push(img)
  }
}

function reset() {
  score.value = 0
  over.value = false
  isBest.value = false
  fallers = []
  spawnIn = 0
  elapsed = 0
  playerX = W / 2
  vx = 0
  last = performance.now()
}

function spawn() {
  const w = 36 + Math.random() * 34
  fallers.push({
    x: Math.random() * (W - w),
    y: -110,
    w,
    h: w * 1.5,
    v: 120 + Math.random() * 80 + elapsed * 6, // gets meaner over time
    spin: (Math.random() - 0.5) * 2.4,
    angle: (Math.random() - 0.5) * 0.6,
    img: images.length ? images[Math.floor(Math.random() * images.length)]! : null,
  })
}

function step(now: number) {
  if (!ctx) return
  const dt = Math.min((now - last) / 1000, 0.05)
  last = now

  if (!over.value) {
    elapsed += dt
    score.value = elapsed * 10

    // Player physics: snappy accel, light friction.
    const accel = 2600
    if (keys.left) vx -= accel * dt
    if (keys.right) vx += accel * dt
    vx *= 0.86
    playerX = Math.max(PLAYER_W / 2, Math.min(W - PLAYER_W / 2, playerX + vx * dt))

    spawnIn -= dt
    if (spawnIn <= 0) {
      spawn()
      // Spawn cadence tightens as the run goes on, floor at 220ms.
      spawnIn = Math.max(0.22, 0.85 - elapsed * 0.02)
    }

    const px = playerX - PLAYER_W / 2
    const py = H - PLAYER_H - 10
    for (const f of fallers) {
      f.y += f.v * dt
      f.angle += f.spin * dt
      // AABB with a little forgiveness so deaths feel fair.
      const pad = 7
      if (
        f.y + f.h - pad > py && f.y + pad < py + PLAYER_H &&
        f.x + f.w - pad > px && f.x + pad < px + PLAYER_W
      ) {
        over.value = true
        if (score.value > best.value) {
          best.value = score.value
          isBest.value = true
          try { localStorage.setItem(BEST_KEY, String(Math.floor(best.value))) } catch { /* private mode */ }
        }
      }
    }
    fallers = fallers.filter(f => f.y < H + 130)
  }

  // --- draw ---
  ctx.clearRect(0, 0, W, H)
  for (const f of fallers) {
    ctx.save()
    ctx.translate(f.x + f.w / 2, f.y + f.h / 2)
    ctx.rotate(f.angle)
    if (f.img?.complete && f.img.naturalWidth > 0) {
      ctx.drawImage(f.img, -f.w / 2, -f.h / 2, f.w, f.h)
      ctx.strokeStyle = 'rgba(255,255,255,0.12)'
      ctx.strokeRect(-f.w / 2, -f.h / 2, f.w, f.h)
    } else {
      ctx.fillStyle = '#1a1a20'
      ctx.fillRect(-f.w / 2, -f.h / 2, f.w, f.h)
    }
    ctx.restore()
  }

  // Player: a little gold-edged poster tile.
  const px = playerX - PLAYER_W / 2
  const py = H - PLAYER_H - 10
  ctx.fillStyle = '#0c0c10'
  ctx.fillRect(px, py, PLAYER_W, PLAYER_H)
  ctx.strokeStyle = over.value ? 'rgba(230,185,74,0.35)' : '#e6b94a'
  ctx.lineWidth = 2
  ctx.strokeRect(px, py, PLAYER_W, PLAYER_H)
  ctx.fillStyle = over.value ? 'rgba(230,185,74,0.35)' : '#e6b94a'
  ctx.font = '20px JetBrains Mono, monospace'
  ctx.textAlign = 'center'
  ctx.fillText('心', playerX, py + PLAYER_H / 2 + 7)

  raf = requestAnimationFrame(step)
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') { emit('exit'); return }
  if (e.key === 'ArrowLeft' || e.key === 'a') { keys.left = e.type !== 'keyup'; e.preventDefault() }
  if (e.key === 'ArrowRight' || e.key === 'd') { keys.right = e.type !== 'keyup'; e.preventDefault() }
  if (e.key === 'Enter' && over.value) reset()
}
function onKeyUp(e: KeyboardEvent) {
  if (e.key === 'ArrowLeft' || e.key === 'a') keys.left = false
  if (e.key === 'ArrowRight' || e.key === 'd') keys.right = false
}

function fit() {
  const c = canvasEl.value
  const root = rootEl.value
  if (!c || !root) return
  const dpr = window.devicePixelRatio || 1
  W = root.clientWidth
  H = root.clientHeight
  c.width = W * dpr
  c.height = H * dpr
  c.style.width = `${W}px`
  c.style.height = `${H}px`
  ctx = c.getContext('2d')
  ctx?.scale(dpr, dpr)
}

onMounted(() => {
  try { best.value = Number(localStorage.getItem(BEST_KEY) || 0) } catch { /* private mode */ }
  loadImages()
  fit()
  reset()
  window.addEventListener('resize', fit)
  window.addEventListener('keydown', onKey)
  window.addEventListener('keyup', onKeyUp)
  raf = requestAnimationFrame((t) => { last = t; step(t) })
})
onUnmounted(() => {
  cancelAnimationFrame(raf)
  window.removeEventListener('resize', fit)
  window.removeEventListener('keydown', onKey)
  window.removeEventListener('keyup', onKeyUp)
})
</script>

<style scoped>
.hero-game {
  position: absolute;
  inset: 0;
  z-index: 5;
  outline: none;
  background: radial-gradient(ellipse at 50% 120%, rgba(230, 185, 74, 0.08), transparent 60%), var(--bg-0);
  cursor: none;
}
.hero-game-canvas { position: absolute; inset: 0; }
.hero-game-hud {
  position: absolute;
  top: 14px;
  left: 20px;
  right: 20px;
  display: flex;
  align-items: baseline;
  gap: 16px;
  font-family: var(--font-mono);
  pointer-events: none;
}
.hud-score { font-size: 26px; color: var(--gold); }
.hud-best { font-size: 12px; color: var(--fg-3); letter-spacing: 0.08em; }
.hud-hint { margin-left: auto; font-size: 11px; color: var(--fg-3); letter-spacing: 0.06em; }
.hero-game-over {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: rgba(7, 7, 10, 0.55);
  font-family: var(--font-mono);
}
.go-title { font-size: 13px; letter-spacing: 0.22em; color: var(--fg-2); }
.go-score { font-size: 44px; color: var(--gold); }
.go-best { font-size: 14px; color: var(--gold-bright); }
.go-hint { font-size: 11px; color: var(--fg-3); letter-spacing: 0.08em; }
</style>
