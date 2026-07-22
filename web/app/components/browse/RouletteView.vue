<template>
  <div class="roulette-view scroll" :style="toneVars">
    <div class="rv-stage">
      <div class="rv-lead">
        <!-- Mono breadcrumb eyebrow — the head for this moment page. -->
        <div class="rv-eyebrow">
          <NuxtLink to="/movies" class="rv-crumb">Movies</NuxtLink>
          <span class="sep">·</span>
          <span>Roulette</span>
        </div>

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
            <div class="rv-title-wrap">
              <span v-if="spinning" class="rv-ghostmark" aria-hidden="true">?</span>
              <h1 class="rv-title muted" :class="{ spinning }">{{ spinning ? 'Spinning…' : "Can't decide?" }}</h1>
            </div>
            <p class="rv-hint" v-if="!spinning">{{ poolLine }}</p>
          </div>
        </Transition>

        <div class="rv-actions">
          <!-- Primary CTA: Play the revealed pick (tone-glow) or, when there's no
               playable file, deep-link to its details. Idle/spinning has no CTA
               here — Spin carries the glow instead. -->
          <button v-if="pick && settled && pickFileId" class="rv-cta" @click="playPick">
            <span class="tri" />
            Play
          </button>
          <NuxtLink v-else-if="pick && settled" :to="mediaUrl(pick)" class="rv-ghost">
            <Icon name="info" :size="15" />
            Details
          </NuxtLink>

          <!-- Spin — tone-glow primary while it's the only action, a tone-tinted
               pill once a pick owns the spotlight. -->
          <button
            class="rv-spin"
            :class="{ spinning, cta: !(pick && settled), pill: pick && settled }"
            :disabled="spinning || !pool.length"
            @click="spin"
          >
            <Icon name="shuffle" :size="16" />
            {{ settled ? 'Spin again' : 'Spin' }}
          </button>
        </div>

        <div class="rv-filters">
          <!-- Pool source: the personalized engine (rank-weighted) vs the
               whole library. Only offered once the engine has signal. -->
          <template v-if="canForYou">
            <span class="rv-filter-label">Pool</span>
            <div class="rv-seg">
              <button :class="{ active: effectiveSource === 'foryou' }" :aria-pressed="effectiveSource === 'foryou'" @click="source = 'foryou'">
                <Icon name="sparkle" :size="11" /> For you
              </button>
              <button :class="{ active: effectiveSource === 'all' }" :aria-pressed="effectiveSource === 'all'" @click="source = 'all'">Anything</button>
            </div>
            <span class="rv-filter-sep" />
          </template>
          <span class="rv-filter-label">Genre</span>
          <button
            v-for="g in topGenres"
            :key="g"
            class="rv-chip"
            :class="{ on: genreFilter.has(g) }"
            :aria-pressed="genreFilter.has(g)"
            @click="toggleGenre(g)"
          >{{ g }}</button>
          <span class="rv-filter-sep" />
          <span class="rv-filter-label">Runtime</span>
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
               while moving, snaps crisp with a tone flash on arrival. -->
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
//
// Heya 2.0 dress (2026-07-15): mono breadcrumb + Archivo display title, the
// revealed pick framed as a mini detail-hero (record-card reel + tone-glow
// Play + ghost Details), controls as mono/tone pills. The whole moment tone-
// follows the ambient (--tone/--tone-rgb on the root) — the pool before a
// spin, the settled pick's backdrop after.
import { useQuery } from '@pinia/colada'

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
// veil); the settled pick's backdrop takes the whole page over with the 2.0
// grade. One owner handle swaps between the two claims.
const background = useBackground()
const currentBg = computed(() => (pick.value && settled.value ? useBackdropUrl(pick.value) : null) || null)
watch(currentBg, (url) => {
  if (url) background.set(url, { presentation: 'hero' })
  else background.pool('movie')
}, { immediate: true })

// Tone-follow: read the single app-wide ambient tone (pool before a spin, the
// settled pick after) and publish --tone/--tone-rgb on the root, so the
// eyebrow, meta dots, reason pill, Spin/Play glow and the reel's landing ring
// all glide together. Falls back to the theme accent when ambient/tone-follow
// is off (:root defaults --tone to the accent).
const bgTone = useBackgroundTone()
const { toneFollowEnabled } = useAppearance()
const toneVars = computed<Record<string, string> | undefined>(() => {
  if (!toneFollowEnabled.value) return undefined
  const t = bgTone.value
  if (!t) return undefined
  const m = t.main.match(/\d+/g)
  if (!m) return undefined
  return { '--tone': t.main, '--tone-rgb': m.slice(0, 3).join(' '), '--tone-ink': t.ink }
})

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
/* Mono breadcrumb eyebrow (MOVIES · ROULETTE) — the head for this page. */
.rv-eyebrow {
  display: flex;
  align-items: center;
  gap: 9px;
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--tone);
  margin-bottom: 16px;
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1);
  transition: color 0.9s cubic-bezier(0.22, 1, 0.36, 1);
}
.rv-eyebrow .sep { color: rgb(var(--ink) / 0.3); }
.rv-crumb { color: var(--fg-2); transition: color 0.12s ease; }
.rv-crumb:hover { color: var(--fg-0); }

.rv-title-wrap { position: relative; display: inline-block; }
/* Giant ghost "?" scaled behind the Spinning… word — drama within the grammar
   (heya2 .bignum stroke-outline numeral). */
.rv-ghostmark {
  position: absolute;
  left: -0.32em;
  top: 50%;
  transform: translateY(-54%);
  z-index: -1;
  font: 700 clamp(150px, 20vw, 260px)/1 var(--font-mono);
  letter-spacing: -0.06em;
  color: transparent;
  -webkit-text-stroke: 2px rgb(var(--tone-rgb) / 0.4);
  pointer-events: none;
  user-select: none;
  animation: rv-ghost-pulse 1.6s ease-in-out infinite;
}
@keyframes rv-ghost-pulse {
  0%, 100% { opacity: 0.35; }
  50% { opacity: 0.7; }
}
.rv-title-link { color: inherit; text-decoration: none; }
.rv-title-link:hover .rv-title { color: var(--tone); }
/* Archivo condensed display title (heya2 .title). */
.rv-title {
  font-family: var(--font-display);
  font-size: clamp(2.6rem, 5vw, 4rem);
  font-weight: 800;
  font-variation-settings: 'wdth' 112;
  letter-spacing: -0.022em;
  line-height: 0.99;
  margin: 0 0 12px;
  text-wrap: balance;
  max-width: 18ch;
  transition: color 0.15s;
  text-shadow:
    0 1px 2px var(--bg-1),
    0 0 10px var(--bg-1),
    0 0 24px var(--bg-1);
}
.rv-title.muted { color: rgb(var(--ink) / 0.9); }
.rv-title.spinning { color: var(--tone); transition: color 0.9s cubic-bezier(0.22, 1, 0.36, 1); }
.rv-hint {
  font: 500 13px var(--font-mono); letter-spacing: 0.03em;
  color: var(--fg-1); margin: 0;
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
/* Mono metaline (heya2 .metaline). */
.rv-meta {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px 9px;
  font: 500 12.5px var(--font-mono);
  letter-spacing: 0.04em;
  color: rgb(var(--ink) / 0.72);
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1);
}
.rv-meta .dot {
  width: 3px;
  height: 3px;
  border-radius: 50%;
  background: rgb(var(--tone-rgb) / 0.8);
}
.rv-genres { color: rgb(var(--ink) / 0.6); }
/* The engine's "why this fits you" line — tone-tinted pill. */
.rv-reason {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  margin-top: 14px;
  padding: 6px 13px;
  border-radius: 999px;
  font-size: 12px;
  color: var(--tone);
  background: rgb(var(--tone-rgb) / 0.1);
  border: 1px solid rgb(var(--tone-rgb) / 0.35);
  box-shadow: 0 0 18px rgb(var(--tone-rgb) / 0.12), var(--shadow-el);
}
.rv-actions { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 24px; }

/* Tone-glow primary (heya2 .btn-play) — Play, and Spin while it's the only CTA. */
.rv-cta,
.rv-spin.cta {
  display: inline-flex; align-items: center; gap: 10px;
  padding: 12px 22px; border: 0; border-radius: 999px; cursor: pointer;
  background: var(--tone); color: var(--tone-ink, var(--accent-ink));
  font: 650 14px var(--font-sans); letter-spacing: 0.01em;
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.45),
    0 0 24px rgb(var(--tone-rgb) / 0.4),
    6px 10px 36px -8px rgb(var(--tone-rgb) / 0.75);
  transition: transform 0.15s ease, box-shadow 0.15s ease, filter 0.15s ease,
              background 0.9s cubic-bezier(0.22, 1, 0.36, 1), color 0.9s cubic-bezier(0.22, 1, 0.36, 1);
}
.rv-cta:hover,
.rv-spin.cta:hover:not(:disabled) {
  transform: translateY(-1px);
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.6),
    0 0 40px rgb(var(--tone-rgb) / 0.6),
    8px 14px 48px -8px rgb(var(--tone-rgb) / 0.9);
}
.rv-cta .tri {
  width: 0; height: 0;
  border-left: 11px solid var(--tone-ink, var(--accent-ink));
  border-top: 7px solid transparent;
  border-bottom: 7px solid transparent;
}
.rv-spin:disabled { opacity: 0.45; cursor: default; filter: saturate(0.5); }

/* Tone-tinted secondary pill (heya2 .pill) — Spin once a pick is revealed. */
.rv-spin.pill {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 11px 18px; border-radius: 999px; cursor: pointer;
  border: 1px solid rgb(var(--tone-rgb) / 0.3);
  background: rgb(var(--tone-rgb) / 0.08);
  color: rgb(var(--ink) / 0.9); font: 550 13px var(--font-sans);
  backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px);
  box-shadow: 0 0 16px rgb(var(--tone-rgb) / 0.14), var(--shadow-el);
  transition: border-color 0.15s, background 0.15s, box-shadow 0.15s, transform 0.15s;
}
.rv-spin.pill:hover:not(:disabled) {
  transform: translateY(-1px);
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.15);
  box-shadow: 0 0 24px rgb(var(--tone-rgb) / 0.26), var(--shadow-el);
}

/* Ghost pill (Details) — hairline, transparent. */
.rv-ghost {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 11px 18px; border-radius: 999px; cursor: pointer;
  border: 1px solid var(--border-strong);
  background: color-mix(in oklab, var(--bg-2) 55%, transparent);
  color: var(--fg-1); font: 550 13px var(--font-sans);
  backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px);
  transition: border-color 0.15s, color 0.15s, background 0.15s;
}
.rv-ghost:hover { color: var(--fg-0); border-color: rgb(var(--ink) / 0.35); }

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
  gap: 7px;
  margin-top: 26px;
  padding-top: 18px;
  border-top: 1px solid var(--hair);
}
.rv-filter-label {
  font: 600 9.5px var(--font-mono); letter-spacing: 0.2em; text-transform: uppercase;
  color: rgb(var(--ink) / 0.42);
  text-shadow: 0 0 10px var(--bg-1), 0 1px 2px var(--bg-1);
  margin-right: 2px;
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
  padding: 5px 13px;
  border-radius: 999px;
  font: 600 11px var(--font-mono);
  letter-spacing: 0.04em;
  color: var(--fg-2);
  cursor: pointer;
  transition: background 0.12s ease, color 0.12s ease;
}
.rv-seg button:hover { color: var(--fg-0); }
.rv-seg button.active {
  background: rgb(var(--tone-rgb) / 0.14);
  color: var(--tone);
  box-shadow: 0 0 14px rgb(var(--tone-rgb) / 0.14);
}
.rv-chip {
  font-family: var(--font-mono);
  font-size: 10.5px;
  letter-spacing: 0.05em;
  color: var(--fg-1);
  padding: 5px 11px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: color-mix(in oklab, var(--bg-2) 72%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  box-shadow: var(--shadow-el);
  transition: color 0.15s, border-color 0.15s, background 0.15s, box-shadow 0.15s;
}
.rv-chip:hover { color: var(--fg-0); border-color: var(--border-strong); }
.rv-chip.on {
  color: var(--tone);
  border-color: rgb(var(--tone-rgb) / 0.5);
  background: rgb(var(--tone-rgb) / 0.12);
  box-shadow: 0 0 16px rgb(var(--tone-rgb) / 0.16);
}
.rv-filter-sep {
  width: 1px;
  height: 14px;
  background: var(--border-strong);
  margin: 0 5px;
}
.rv-wheel { justify-self: end; }
/* Poster record-card frame (heya2 .postercard) — directional key-light shadow,
   a --tone ring bloom on settle. */
.wheel-frame {
  position: relative;
  width: 280px;
  aspect-ratio: 2/3;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--bg-2);
  box-shadow:
    0 0 0 1px rgb(var(--ink) / 0.16),
    10px 18px 34px -12px rgb(var(--shade) / 0.8),
    24px 44px 90px -20px rgb(var(--shade) / 0.95);
  transition: box-shadow 0.3s;
}
.wheel-frame.spinning {
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.4),
    10px 18px 34px -12px rgb(var(--shade) / 0.8),
    24px 44px 90px -20px rgb(var(--shade) / 0.95),
    0 0 48px rgb(var(--tone-rgb) / 0.35);
}
.wheel-frame.settled { animation: wheel-pop 0.5s cubic-bezier(0.2, 1.6, 0.4, 1); }
/* Landing flash: a tone ring that blooms and fades as the reel snaps. */
.wheel-frame.settled::after {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: inherit;
  pointer-events: none;
  box-shadow: inset 0 0 0 2px var(--tone), 0 0 42px rgb(var(--tone-rgb) / 0.55);
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
/* Ghost-numeral empty state (heya2 .bignum stroke). */
.wheel-empty {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  font: 700 96px var(--font-mono);
  letter-spacing: -0.05em;
  color: transparent;
  -webkit-text-stroke: 1.5px rgb(var(--ink) / 0.16);
}

/* Reduced motion: the reel skip is handled in JS (spin() short-circuits), but
   the decorative flourishes must also stand down. */
@media (prefers-reduced-motion: reduce) {
  .rv-spin.spinning,
  .wheel-frame.settled,
  .wheel-frame.settled::after,
  .rv-ghostmark { animation: none; }
  .wheel-reel { transition: none; }
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
  .rv-title { max-width: none; }
  .rv-eyebrow, .rv-meta, .rv-actions, .rv-filters { justify-content: center; }
  .rv-title-wrap { display: block; }
}
</style>
