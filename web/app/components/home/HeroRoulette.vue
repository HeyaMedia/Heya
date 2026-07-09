<template>
  <section class="hero-roulette">
    <div class="roulette-bg">
      <NuxtImg
        v-if="pick && settled"
        :src="useBackdropUrl(pick) ?? undefined"
        :width="1920"
        :quality="75"
        class="roulette-bg-img"
        @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
      />
      <div class="roulette-bg-gradient" />
    </div>

    <div class="roulette-inner">
      <div class="roulette-lead">
        <div class="roulette-eyebrow">Roulette</div>

        <template v-if="pick && settled">
          <NuxtLink :to="mediaUrl(pick)" class="roulette-title-link">
            <h1 class="roulette-title">{{ pick.title }}</h1>
          </NuxtLink>
          <div class="roulette-meta">
            <span v-if="pick.year">{{ pick.year }}</span>
            <span v-if="pick.runtime_minutes" class="dot" />
            <span v-if="pick.runtime_minutes">{{ Math.floor(pick.runtime_minutes / 60) }}h {{ pick.runtime_minutes % 60 }}m</span>
            <template v-if="pick.rating">
              <span class="dot" />
              <Icon name="star" :size="13" style="color: var(--gold)" />
              <span style="color: var(--gold)">{{ pick.rating.toFixed(1) }}</span>
            </template>
            <span v-if="pick.genres?.length" class="dot" />
            <span v-if="pick.genres?.length" class="roulette-genres">{{ pick.genres.slice(0, 3).join(' · ') }}</span>
          </div>
        </template>
        <template v-else>
          <h1 class="roulette-title muted">{{ spinning ? 'Spinning…' : "Can't decide?" }}</h1>
          <p class="roulette-hint" v-if="!spinning">{{ poolLine }}</p>
        </template>

        <div class="roulette-actions">
          <button class="btn btn-primary" :disabled="spinning || !pool.length" @click="spin">
            <Icon name="shuffle" :size="16" />
            {{ settled ? 'Spin again' : 'Spin' }}
          </button>
          <button v-if="pick && settled && pickFileId" class="btn btn-ghost" @click="playPick">
            <Icon name="play" :size="16" />
            Play
          </button>
          <NuxtLink v-else-if="pick && settled" :to="mediaUrl(pick)" class="btn btn-ghost">
            <Icon name="info" :size="16" />
            Details
          </NuxtLink>
        </div>

        <div class="roulette-filters">
          <button
            v-for="g in topGenres"
            :key="g"
            class="filter-chip"
            :class="{ on: genreFilter.has(g) }"
            @click="toggleGenre(g)"
          >{{ g }}</button>
          <span class="filter-sep" />
          <button
            v-for="rt in RUNTIMES"
            :key="rt.label"
            class="filter-chip"
            :class="{ on: maxRuntime === rt.max }"
            @click="maxRuntime = maxRuntime === rt.max ? 0 : rt.max"
          >{{ rt.label }}</button>
        </div>
      </div>

      <div class="roulette-wheel">
        <div class="wheel-frame" :class="{ spinning, settled }">
          <!-- Slot reel: a vertical strip of posters translated with a long
               ease-out; the pick sits at the end of the strip. Motion-blurred
               while moving, snaps crisp on arrival. -->
          <div
            v-if="reel.length"
            class="wheel-reel"
            :class="{ moving: spinning }"
            :style="{ transform: `translateY(${reelOffset}px)`, transitionDuration: spinning ? `${SPIN_MS}ms` : '0ms' }"
            @transitionend="onReelLanded"
          >
            <div v-for="(m, i) in reel" :key="`${i}-${m.id}`" class="wheel-cell">
              <NuxtImg
                :src="usePosterUrl(m) ?? ''"
                :width="240"
                :quality="80"
                densities="1x 2x"
                alt=""
                @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.visibility = 'hidden' }"
              />
            </div>
          </div>
          <div v-else class="wheel-empty">?</div>
          <div class="wheel-sheen" v-if="spinning" />
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
// "Roulette" — decision-paralysis killer. Filters narrow the pool, a slot
// reel of your own posters decelerates onto the pick. The pick's detail is
// fetched on settle so Play can start the actual file.
import { useQuery } from '@tanstack/vue-query'

interface EnrichedMovie {
  id: number
  public_id?: string
  title: string
  slug: string
  year: string
  media_type: string
  available: boolean
  genres: string[]
  rating: number
  runtime_minutes: number
}

const { $heya } = useNuxtApp()

const moviesQuery = useQuery({
  queryKey: ['media', 'enriched', 'movie'],
  queryFn: async () => {
    const body = await $heya('/api/media/enriched', { query: { type: 'movie', limit: 2000 } }) as { movies?: EnrichedMovie[] }
    return (body.movies ?? []).filter(m => m.available !== false)
  },
  staleTime: 1000 * 60 * 10,
})

const genreFilter = ref(new Set<string>())
const maxRuntime = ref(0)
const RUNTIMES = [
  { label: '< 90m', max: 90 },
  { label: '< 2h', max: 120 },
  { label: '< 2h30', max: 150 },
]

const all = computed(() => moviesQuery.data.value ?? [])

const topGenres = computed(() => {
  const counts = new Map<string, number>()
  for (const m of all.value) for (const g of m.genres ?? []) counts.set(g, (counts.get(g) ?? 0) + 1)
  return [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 7).map(([g]) => g)
})

const pool = computed(() => all.value.filter((m) => {
  if (maxRuntime.value && (m.runtime_minutes || 0) > maxRuntime.value) return false
  if (genreFilter.value.size && !m.genres?.some(g => genreFilter.value.has(g))) return false
  return true
}))

const poolLine = computed(() => `${pool.value.length} films in the pool — narrow it down or just spin.`)

function toggleGenre(g: string) {
  const next = new Set(genreFilter.value)
  if (next.has(g)) next.delete(g)
  else next.add(g)
  genreFilter.value = next
}

// --- Slot reel -------------------------------------------------------------
const SPIN_MS = 2600
const REEL_LEN = 14 // posters flown past before the pick lands
const CELL_H = 372 // 248px wide frame × 3/2

const spinning = ref(false)
const settled = ref(false)
const pick = ref<EnrichedMovie | null>(null)
const reel = ref<EnrichedMovie[]>([])
const reelOffset = ref(0)
const pickFileId = ref<string | number | null>(null)
let reducedMotion = false
let landedGuard: ReturnType<typeof setTimeout> | null = null

function spin() {
  const p = pool.value
  if (!p.length || spinning.value) return
  settled.value = false
  pickFileId.value = null
  pick.value = p[Math.floor(Math.random() * p.length)] ?? null
  if (!pick.value) return

  if (reducedMotion || p.length < 4) {
    reel.value = [pick.value]
    reelOffset.value = 0
    settle()
    return
  }

  // Strip of random posters, current pick landing at the end. Start above
  // the frame (offset 0 shows cell 0), then let one long transition carry it
  // to the final cell.
  const strip: EnrichedMovie[] = []
  for (let i = 0; i < REEL_LEN; i++) strip.push(p[Math.floor(Math.random() * p.length)]!)
  strip.push(pick.value)
  reel.value = strip
  reelOffset.value = 0

  // Two frames so the reset offset paints before the transition arms.
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      spinning.value = true
      reelOffset.value = -(strip.length - 1) * CELL_H
      // transitionend can be swallowed if the tab loses focus mid-spin —
      // a guard timeout makes sure the wheel always lands.
      if (landedGuard) clearTimeout(landedGuard)
      landedGuard = setTimeout(() => onReelLanded(), SPIN_MS + 400)
    })
  })
}

function onReelLanded() {
  if (!spinning.value) return
  if (landedGuard) { clearTimeout(landedGuard); landedGuard = null }
  settle()
}

async function settle() {
  spinning.value = false
  settled.value = true
  if (!pick.value) return
  try {
    const detail = await $heya('/api/media/{id}', { path: { id: String(pick.value.id) } }) as { files?: { id: number; public_id?: string }[] }
    pickFileId.value = detail.files?.[0]?.public_id || detail.files?.[0]?.id || null
  } catch { pickFileId.value = null }
}

function playPick() {
  if (!pick.value || !pickFileId.value) return
  const params = new URLSearchParams({
    media_item_id: String(pick.value.id),
    title: pick.value.title,
    entity_type: 'movie',
    entity_id: String(pick.value.id),
  })
  navigateTo(`/watch/${pickFileId.value}?${params}`)
}

onMounted(() => {
  reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
})
onUnmounted(() => {
  if (landedGuard) clearTimeout(landedGuard)
})
</script>

<style scoped>
.hero-roulette { position: relative; height: 100%; }
.roulette-bg { position: absolute; inset: 0; background: var(--bg-0); }
.roulette-bg-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  animation: roulette-reveal 0.8s ease;
}
@keyframes roulette-reveal {
  from { opacity: 0; }
  to { opacity: 1; }
}
.roulette-bg-gradient {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(to right, var(--bg-1) 0%, rgba(12,12,16,0.65) 50%, rgba(12,12,16,0.2) 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 40%);
}
.roulette-inner {
  position: relative;
  z-index: 2;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 248px;
  align-items: center;
  gap: 56px;
  height: 100%;
  padding: 48px 40px;
  max-width: 1200px;
}
.roulette-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--gold);
  margin-bottom: 10px;
}
.roulette-title-link { color: inherit; text-decoration: none; }
.roulette-title-link:hover .roulette-title { color: var(--gold); }
.roulette-title {
  font-size: 44px;
  font-weight: 600;
  letter-spacing: -0.025em;
  line-height: 1.05;
  margin: 0 0 10px;
  text-wrap: balance;
  transition: color 0.15s;
}
.roulette-title.muted { color: var(--fg-1); }
.roulette-hint { font-size: 14px; color: var(--fg-2); margin: 0; }
.roulette-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: var(--fg-1);
}
.roulette-meta .dot {
  width: 3px;
  height: 3px;
  border-radius: 50%;
  background: var(--fg-3);
}
.roulette-genres { color: var(--fg-2); }
.roulette-actions { display: flex; gap: 10px; margin-top: 22px; }
.roulette-filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-top: 22px;
}
.filter-chip {
  font-family: var(--font-mono);
  font-size: 10.5px;
  letter-spacing: 0.05em;
  color: var(--fg-2);
  padding: 4px 10px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: rgba(7, 7, 10, 0.4);
  transition: color 0.15s, border-color 0.15s, background 0.15s;
}
.filter-chip:hover { color: var(--fg-0); border-color: var(--border-strong); }
.filter-chip.on {
  color: var(--gold);
  border-color: rgba(230, 185, 74, 0.45);
  background: rgba(230, 185, 74, 0.08);
}
.filter-sep {
  width: 1px;
  height: 14px;
  background: var(--border-strong);
  margin: 0 4px;
}
.roulette-wheel { justify-self: end; }
.wheel-frame {
  position: relative;
  width: 248px;
  height: 372px;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--bg-2);
  box-shadow: 0 30px 80px rgba(0,0,0,0.7), 0 0 0 1px rgba(255,255,255,0.06);
  transition: box-shadow 0.3s;
}
.wheel-frame.spinning {
  box-shadow: 0 30px 80px rgba(0,0,0,0.7), 0 0 48px rgba(230, 185, 74, 0.35), 0 0 0 1px rgba(230, 185, 74, 0.4);
}
.wheel-frame.settled { animation: wheel-pop 0.4s cubic-bezier(0.2, 1.6, 0.4, 1); }
@keyframes wheel-pop {
  0% { transform: scale(0.985); }
  60% { transform: scale(1.02); }
  100% { transform: scale(1); }
}
.wheel-reel {
  will-change: transform;
  transition-property: transform;
  transition-timing-function: cubic-bezier(0.12, 0.75, 0.16, 1);
}
.wheel-reel.moving { filter: blur(2px) brightness(1.05); }
.wheel-cell {
  width: 248px;
  height: 372px;
}
.wheel-cell img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}
.wheel-sheen {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: linear-gradient(to bottom, rgba(7,7,10,0.55), transparent 30%, transparent 70%, rgba(7,7,10,0.55));
}
.wheel-empty {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: var(--font-mono);
  font-size: 64px;
  color: var(--fg-4);
}
@media (max-width: 900px) {
  .roulette-inner { grid-template-columns: 1fr; gap: 20px; padding: 24px 20px; align-content: center; }
  .roulette-title { font-size: 32px; }
  .roulette-wheel { display: none; }
}
</style>
