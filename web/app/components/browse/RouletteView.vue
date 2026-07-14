<template>
  <div class="roulette-view scroll">
    <div class="rv-stage">
      <div class="rv-lead">
        <div class="rv-eyebrow">Roulette</div>

        <Transition name="pick" mode="out-in">
          <div v-if="pick && settled" :key="pick.id" class="rv-spot">
            <NuxtLink :to="mediaUrl(pick)" class="rv-title-link">
              <h1 class="rv-title">{{ pick.title }}</h1>
            </NuxtLink>
            <div class="rv-meta">
              <span v-if="pick.year">{{ pick.year }}</span>
              <span v-if="pick.runtime_minutes" class="dot" />
              <span v-if="pick.runtime_minutes">{{ Math.floor(pick.runtime_minutes / 60) }}h {{ pick.runtime_minutes % 60 }}m</span>
              <template v-if="pick.rating">
                <span class="dot" />
                <Icon name="star" :size="13" style="color: var(--gold)" />
                <span style="color: var(--gold)">{{ pick.rating.toFixed(1) }}</span>
              </template>
              <span v-if="pick.genres?.length" class="dot" />
              <span v-if="pick.genres?.length" class="rv-genres">{{ pick.genres.slice(0, 3).join(' · ') }}</span>
            </div>
            <!-- Why the engine likes this one for you (For-you source only). -->
            <div v-if="pickReason" class="rv-reason">
              <Icon name="sparkle" :size="12" />
              {{ pickReason }}
            </div>
          </div>
          <div v-else key="idle" class="rv-spot">
            <h1 class="rv-title muted">{{ spinning ? 'Spinning…' : "Can't decide?" }}</h1>
            <p class="rv-hint" v-if="!spinning">{{ poolLine }}</p>
          </div>
        </Transition>

        <div class="rv-actions">
          <button class="btn btn-primary rv-spin" :class="{ spinning }" :style="spinStyle" :disabled="spinning || !pool.length" @click="spin">
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

        <div class="rv-filters">
          <!-- Pool source: the personalized engine (rank-weighted) vs the
               whole library. Only offered once the engine has signal. -->
          <div v-if="canForYou" class="rv-seg">
            <button :class="{ active: effectiveSource === 'foryou' }" :aria-pressed="effectiveSource === 'foryou'" @click="source = 'foryou'">
              <Icon name="sparkle" :size="11" /> For you
            </button>
            <button :class="{ active: effectiveSource === 'all' }" :aria-pressed="effectiveSource === 'all'" @click="source = 'all'">Anything</button>
          </div>
          <span v-if="canForYou" class="rv-filter-sep" />
          <button
            v-for="g in topGenres"
            :key="g"
            class="rv-chip"
            :class="{ on: genreFilter.has(g) }"
            :aria-pressed="genreFilter.has(g)"
            @click="toggleGenre(g)"
          >{{ g }}</button>
          <span class="rv-filter-sep" />
          <button
            v-for="rt in RUNTIMES"
            :key="rt.label"
            class="rv-chip"
            :class="{ on: maxRuntime === rt.max }"
            :aria-pressed="maxRuntime === rt.max"
            @click="maxRuntime = maxRuntime === rt.max ? 0 : rt.max"
          >{{ rt.label }}</button>
        </div>
      </div>

      <div class="rv-wheel">
        <div ref="wheelFrame" class="wheel-frame" :class="{ spinning, settled }">
          <!-- Slot reel: a vertical strip of posters translated with a long
               ease-out; the pick sits at the end of the strip. Motion-blurred
               while moving, snaps crisp with a gold flash on arrival. -->
          <div
            v-if="reel.length"
            class="wheel-reel"
            :class="{ moving: spinning }"
            :style="{ transform: `translateY(${reelOffset}px)`, transitionDuration: spinning ? `${SPIN_MS}ms` : '0ms' }"
            @transitionend="onReelLanded"
          >
            <div v-for="(m, i) in reel" :key="`${i}-${m.id}`" class="wheel-cell">
              <LoadingImage
                :src="usePosterUrl(m) ?? ''"
                :width="280"
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
  </div>
</template>

<script setup lang="ts">
// Roulette — the decision-paralysis killer, a view within /movies (sidebar
// stays) at /movies/roulette. Two pools: "For you" draws rank-weighted from
// the personalized recommendation engine (and shows WHY the pick fits);
// "Anything" is the whole library. Filters narrow either. A slot reel of
// your own posters decelerates onto the pick; its backdrop takes over the
// page background on settle.
import { useQuery } from '@pinia/colada'
import type { ImageTone } from '~/composables/useImageTone'

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
type RouletteEntry = EnrichedMovie & { reason?: string }

const { $heya } = useNuxtApp()

const moviesQuery = useQuery({
  key: ['media', 'enriched', 'movie'],
  query: async () => {
    const body = await $heya('/api/media/enriched', { query: { type: 'movie', limit: 2000 } }) as { movies?: EnrichedMovie[] }
    return (body.movies ?? []).filter(m => m.available !== false)
  },
  staleTime: 1000 * 60 * 10,
})

// The personalized engine — same endpoint the For You rails use. Items come
// back rank-ordered with a human "reason"; joined against the enriched list
// so genre/runtime filters work on this pool too.
interface RecItem { id: number; reason?: string; available: boolean }
const recsQuery = useQuery({
  key: ['for-you-roulette'],
  query: async () => (await $heya('/api/me/recommendations', {
    query: { type: 'movie', limit: 60 },
  })) as { items: RecItem[]; has_signal: boolean },
  staleTime: 1000 * 60 * 5,
})

const all = computed(() => moviesQuery.data.value ?? [])
const enrichedById = computed(() => new Map(all.value.map(m => [m.id, m])))
const recPool = computed<RouletteEntry[]>(() => {
  const rows: RouletteEntry[] = []
  for (const r of recsQuery.data.value?.items ?? []) {
    const m = enrichedById.value.get(r.id)
    if (m) rows.push({ ...m, reason: r.reason })
  }
  return rows
})

// Source: default to the engine once it has signal and a workable pool.
const canForYou = computed(() => (recsQuery.data.value?.has_signal ?? false) && recPool.value.length >= 8)
const source = ref<'foryou' | 'all' | null>(null)
const effectiveSource = computed<'foryou' | 'all'>(() =>
  source.value ?? (canForYou.value ? 'foryou' : 'all'))

const genreFilter = ref(new Set<string>())
const maxRuntime = ref(0)
const RUNTIMES = [
  { label: '< 90m', max: 90 },
  { label: '< 2h', max: 120 },
  { label: '< 2h30', max: 150 },
]

const topGenres = computed(() => {
  const counts = new Map<string, number>()
  for (const m of all.value) for (const g of m.genres ?? []) counts.set(g, (counts.get(g) ?? 0) + 1)
  return [...counts.entries()].sort((a, b) => b[1] - a[1]).slice(0, 7).map(([g]) => g)
})

const pool = computed<RouletteEntry[]>(() => {
  const base: RouletteEntry[] = effectiveSource.value === 'foryou' ? recPool.value : all.value
  return base.filter((m) => {
    if (maxRuntime.value && (m.runtime_minutes || 0) > maxRuntime.value) return false
    if (genreFilter.value.size && !m.genres?.some(g => genreFilter.value.has(g))) return false
    return true
  })
})

const poolLine = computed(() => effectiveSource.value === 'foryou'
  ? `${pool.value.length} films ranked to your taste — spin for a weighted pick.`
  : `${pool.value.length} films in the pool — narrow it down or just spin.`)

function toggleGenre(g: string) {
  const next = new Set(genreFilter.value)
  if (next.has(g)) next.delete(g)
  else next.add(g)
  genreFilter.value = next
}

/** Uniform over "Anything"; rank-weighted over "For you" — the engine's
 *  top picks are likelier, the tail stays possible. */
function drawPick(p: RouletteEntry[]): RouletteEntry | null {
  if (!p.length) return null
  if (effectiveSource.value !== 'foryou') return p[Math.floor(Math.random() * p.length)] ?? null
  const total = (p.length * (p.length + 1)) / 2
  let roll = Math.random() * total
  for (let i = 0; i < p.length; i++) {
    roll -= p.length - i
    if (roll <= 0) return p[i]!
  }
  return p[p.length - 1]!
}

// --- Slot reel -------------------------------------------------------------
const SPIN_MS = 3200
const REEL_LEN = 18 // posters flown past before the pick lands
const FALLBACK_CELL_H = 420 // 280px wide frame × 3/2

const wheelFrame = ref<HTMLElement | null>(null)
const spinning = ref(false)
const settled = ref(false)
const pick = ref<RouletteEntry | null>(null)
const pickReason = computed(() =>
  (settled.value && effectiveSource.value === 'foryou' ? pick.value?.reason : '') || '')
const reel = ref<RouletteEntry[]>([])
const reelOffset = ref(0)
const pickFileId = ref<string | number | null>(null)
let reducedMotion = false
let landedGuard: ReturnType<typeof setTimeout> | null = null
// Generation token for the settle fetch: only the NEWEST request may write.
// Bumped when a spin STARTS (not just when a new fetch fires) so an
// in-flight request from the previous pick goes stale immediately — it must
// not repopulate pickFileId mid-spin, where a slow follow-up fetch would
// leave Play pointing at the previous movie. Pick-id comparison can't do
// this: consecutive spins may land on the same movie.
let settleSeq = 0

// Background: pre-settle the movie pool rides behind the view (content
// veil); the settled pick's backdrop takes the whole page over. One owner
// handle swaps between the two claims.
const background = useBackground()
const currentBg = computed(() => (pick.value && settled.value ? useBackdropUrl(pick.value) : null) || null)
watch(currentBg, (url) => {
  if (url) background.set(url)
  else background.pool('movie')
}, { immediate: true })

// Spin button wears the settled backdrop's tone (theme accent until then).
const heroTone = ref<ImageTone | null>(null)
watch(currentBg, async (url) => {
  heroTone.value = url ? await sampleImageTone(url) : null
}, { immediate: true })
const spinStyle = computed(() =>
  heroTone.value ? { background: heroTone.value.main, color: heroTone.value.ink } : undefined)

function spin() {
  const p = pool.value
  if (!p.length || spinning.value) return
  settleSeq++ // stale-out any in-flight settle fetch before the reset below
  settled.value = false
  pickFileId.value = null
  pick.value = drawPick(p)
  if (!pick.value) return

  if (reducedMotion || p.length < 4) {
    reel.value = [pick.value]
    reelOffset.value = 0
    settle()
    return
  }

  // Strip of random posters, current pick landing at the end. Start above
  // the frame (offset 0 shows cell 0), then let one long transition carry it
  // to the final cell. Cell height is measured off the rendered frame — the
  // frame is responsive, so a JS constant would drift out of sync.
  const strip: RouletteEntry[] = []
  for (let i = 0; i < REEL_LEN; i++) strip.push(p[Math.floor(Math.random() * p.length)]!)
  strip.push(pick.value)
  reel.value = strip
  reelOffset.value = 0
  const cellH = wheelFrame.value?.offsetHeight || FALLBACK_CELL_H

  // Two frames so the reset offset paints before the transition arms.
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      spinning.value = true
      reelOffset.value = -(strip.length - 1) * cellH
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
  const target = pick.value
  if (!target) return
  const seq = ++settleSeq
  try {
    const detail = await $heya('/api/media/{id}', { path: { id: String(target.id) } }) as { files?: { id: number; public_id?: string }[] }
    if (seq !== settleSeq) return
    pickFileId.value = detail.files?.[0]?.public_id || detail.files?.[0]?.id || null
  } catch {
    if (seq === settleSeq) pickFileId.value = null
  }
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
.roulette-view { height: 100%; }
.rv-stage {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 280px;
  align-items: center;
  gap: 64px;
  min-height: 100%;
  padding: 48px clamp(24px, 5vw, 72px);
  max-width: 1280px;
  margin: 0 auto;
}
/* Blended readability wash behind the whole lead block — same recipe as the
   heroes: --bg-1-derived, long falloff, heavy blur, no locatable edge. Halos
   alone lose against very bright pool artwork. */
.rv-lead {
  position: relative;
  isolation: isolate;
}
.rv-lead::before {
  content: '';
  position: absolute;
  inset: -70px -120px -60px -110px;
  z-index: -1;
  pointer-events: none;
  background: radial-gradient(ellipse 80% 75% at 35% 42%,
    color-mix(in srgb, var(--bg-1) 62%, transparent) 0%,
    color-mix(in srgb, var(--bg-1) 44%, transparent) 42%,
    color-mix(in srgb, var(--bg-1) 20%, transparent) 70%,
    transparent 92%);
  filter: blur(28px);
}
.rv-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--gold);
  margin-bottom: 10px;
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1);
}
.rv-title-link { color: inherit; text-decoration: none; }
.rv-title-link:hover .rv-title { color: var(--gold); }
.rv-title {
  font-size: 52px;
  font-weight: 600;
  letter-spacing: -0.025em;
  line-height: 1.05;
  margin: 0 0 10px;
  text-wrap: balance;
  transition: color 0.15s;
  text-shadow:
    0 1px 2px var(--bg-1),
    0 0 10px var(--bg-1),
    0 0 24px var(--bg-1);
}
.rv-title.muted { color: var(--fg-1); }
.rv-hint {
  font-size: 14px; color: var(--fg-1); margin: 0;
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
.rv-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: var(--fg-1);
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1);
}
.rv-meta .dot {
  width: 3px;
  height: 3px;
  border-radius: 50%;
  background: var(--fg-3);
}
.rv-genres { color: var(--fg-1); }
/* The engine's "why this fits you" line. */
.rv-reason {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  margin-top: 12px;
  padding: 5px 12px;
  border-radius: 999px;
  font-size: 12px;
  color: var(--gold);
  background: color-mix(in srgb, var(--gold) 10%, var(--bg-2));
  border: 1px solid color-mix(in srgb, var(--gold) 35%, transparent);
  box-shadow: var(--shadow-el);
}
.rv-actions { display: flex; gap: 10px; margin-top: 22px; }
.rv-spin {
  box-shadow: var(--shadow-el);
  transition: filter 0.15s ease,
              background 0.9s cubic-bezier(0.22, 1, 0.36, 1),
              color 0.9s cubic-bezier(0.22, 1, 0.36, 1);
}
/* Anticipation shimmer while the reel runs. */
.rv-spin.spinning { animation: rv-spin-pulse 1s ease-in-out infinite; }
@keyframes rv-spin-pulse {
  0%, 100% { filter: brightness(1); }
  50% { filter: brightness(1.18); }
}

/* Pick entrance: rise + fade as the reel lands. */
.pick-enter-active { transition: opacity 0.4s ease, transform 0.4s cubic-bezier(0.22, 1, 0.36, 1); }
.pick-leave-active { transition: opacity 0.15s ease; }
.pick-enter-from { opacity: 0; transform: translateY(10px); }
.pick-leave-to { opacity: 0; }

.rv-filters {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin-top: 22px;
}
/* Source segment — glass, like every steer control over ambient art. */
.rv-seg {
  display: inline-flex;
  gap: 2px;
  padding: 2px;
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  border-radius: 999px;
  box-shadow: var(--shadow-el);
}
.rv-seg button {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 12px;
  border-radius: 999px;
  font-size: 11.5px;
  font-weight: 500;
  color: var(--fg-2);
  cursor: pointer;
  transition: background 0.12s ease, color 0.12s ease;
}
.rv-seg button:hover { color: var(--fg-0); }
.rv-seg button.active { background: var(--gold-soft); color: var(--gold-bright); }
.rv-chip {
  font-family: var(--font-mono);
  font-size: 10.5px;
  letter-spacing: 0.05em;
  color: var(--fg-1);
  padding: 4px 10px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: color-mix(in oklab, var(--bg-2) 72%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  box-shadow: var(--shadow-el);
  transition: color 0.15s, border-color 0.15s, background 0.15s;
}
.rv-chip:hover { color: var(--fg-0); border-color: var(--border-strong); }
.rv-chip.on {
  color: var(--gold);
  border-color: color-mix(in srgb, var(--gold) 45%, transparent);
  background: color-mix(in srgb, var(--gold) 10%, var(--bg-2));
}
.rv-filter-sep {
  width: 1px;
  height: 14px;
  background: var(--border-strong);
  margin: 0 4px;
}
.rv-wheel { justify-self: end; }
.wheel-frame {
  position: relative;
  width: 280px;
  aspect-ratio: 2/3;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--bg-2);
  box-shadow: 0 30px 80px rgb(var(--shade) / 0.55), 0 0 0 1px rgb(var(--ink) / 0.06);
  transition: box-shadow 0.3s;
}
.wheel-frame.spinning {
  box-shadow: 0 30px 80px rgb(var(--shade) / 0.55), 0 0 48px color-mix(in srgb, var(--gold) 35%, transparent), 0 0 0 1px color-mix(in srgb, var(--gold) 40%, transparent);
}
.wheel-frame.settled { animation: wheel-pop 0.5s cubic-bezier(0.2, 1.6, 0.4, 1); }
/* Landing flash: a gold ring that blooms and fades as the reel snaps. */
.wheel-frame.settled::after {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: inherit;
  pointer-events: none;
  box-shadow: inset 0 0 0 2px var(--gold), 0 0 42px color-mix(in srgb, var(--gold) 55%, transparent);
  opacity: 0;
  animation: rv-flash 0.9s ease-out;
}
@keyframes rv-flash {
  0% { opacity: 1; }
  100% { opacity: 0; }
}
@keyframes wheel-pop {
  0% { transform: scale(0.985); }
  55% { transform: scale(1.025); }
  100% { transform: scale(1); }
}
.wheel-reel {
  will-change: transform;
  transition-property: transform;
  transition-timing-function: cubic-bezier(0.1, 0.8, 0.14, 1);
}
.wheel-reel.moving { filter: blur(2px) brightness(1.05); }
.wheel-cell {
  width: 100%;
  aspect-ratio: 2/3;
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
  background: linear-gradient(to bottom, rgba(7,7,10,0.55), transparent 30%, transparent 70%, rgba(7,7,10,0.55)); /* on artwork — stays literal */
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
/* Narrow: the wheel stacks ABOVE the lead and stays visible. */
@media (max-width: 900px) {
  .rv-stage {
    grid-template-columns: 1fr;
    gap: 24px;
    padding: 24px 20px;
    align-content: center;
    justify-items: center;
  }
  .rv-wheel { order: -1; justify-self: center; }
  .wheel-frame { width: min(200px, 48vw); }
  .rv-lead { text-align: center; width: 100%; }
  .rv-title { font-size: 32px; }
  .rv-meta, .rv-actions, .rv-filters { justify-content: center; }
}
</style>
