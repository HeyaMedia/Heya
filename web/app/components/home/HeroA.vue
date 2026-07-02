<template>
  <section class="hero-featured" v-if="items.length" :style="{ '--hero-tint': tint }">
    <div class="hero-bg">
      <NuxtImg
        v-if="bgA"
        :src="bgA"
        :width="1920"
        :quality="80"
        class="hero-bg-img"
        :class="{ visible: showA && !trailerVisible }"
        @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
      />
      <NuxtImg
        v-if="bgB"
        :src="bgB"
        :width="1920"
        :quality="80"
        class="hero-bg-img"
        :class="{ visible: !showA && !trailerVisible }"
        @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
      />
      <!-- Trailer takeover: mounts after the slide has lingered, fades in over
           the backdrop, hands the rotation back when it ends or errors. -->
      <video
        v-if="trailerSrc"
        class="hero-trailer"
        :class="{ visible: trailerVisible }"
        autoplay
        muted
        playsinline
        :src="trailerSrc"
        @playing="trailerVisible = true"
        @ended="endTrailer(true)"
        @error="endTrailer(false)"
      />
      <div class="hero-bg-gradient" />
    </div>

    <div class="hero-inner">
      <NuxtLink :to="mediaUrl(current)" class="hero-poster">
        <Poster :idx="currentIdx" :src="posterUrl" :aspect="'2/3'" />
      </NuxtLink>

      <div class="hero-info">
        <div style="display: flex; align-items: center; gap: 12px; margin-bottom: 12px">
          <Chip gold>{{ current.chip || 'Featured' }}</Chip>
          <span class="hero-counter">{{ String(currentIdx + 1).padStart(2, '0') }} / {{ String(items.length).padStart(2, '0') }}</span>
        </div>

        <NuxtLink :to="mediaUrl(current)" class="hero-title-link">
          <img
            v-if="logoOk[current.id]"
            class="hero-logo"
            :src="logoUrl(current.id)"
            :alt="current.title"
          >
          <h1 v-else class="hero-title">{{ current.title }}</h1>
        </NuxtLink>

        <div class="hero-meta-row" v-if="current.year || movie?.runtime_minutes || movie?.rating">
          <span v-if="current.year">{{ current.year }}</span>
          <span v-if="movie?.runtime_minutes" class="dot" />
          <span v-if="movie?.runtime_minutes">{{ Math.floor(movie.runtime_minutes / 60) }}h {{ movie.runtime_minutes % 60 }}m</span>
          <template v-if="movie?.rating">
            <span class="dot" />
            <Icon name="star" :size="14" style="color: var(--gold)" />
            <span style="color: var(--gold)">{{ parseFloat(String(movie.rating)).toFixed(1) }}</span>
          </template>
        </div>

        <p class="hero-synopsis" v-if="current.description">
          {{ current.description.slice(0, 180) }}{{ current.description.length > 180 ? '…' : '' }}
        </p>

        <div class="hero-actions">
          <button
            class="btn btn-primary"
            :disabled="!canPlayCurrent"
            @click="$emit('play', current)"
          >
            <Icon name="play" :size="16" />
            {{ playLabel }}
          </button>
          <NuxtLink :to="mediaUrl(current)" class="btn btn-ghost">
            <Icon name="info" :size="16" />
            Details
          </NuxtLink>
        </div>

        <div class="hero-dots" v-if="items.length > 1" @mouseenter="pauseHero" @mouseleave="resumeHero">
          <button
            v-for="(_, i) in items"
            :key="`hero-${i}-${currentIdx}`"
            class="hero-dot"
            :class="{ active: i === currentIdx, paused: (heroPaused || !!trailerSrc) && i === currentIdx }"
            @click="jumpHero(i)"
          />
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import type { MediaItem, Movie } from '~~/shared/types'

// playInfo: per-item playback hint resolved by the parent. Movies populate
// fileId from detail.files[0]; TV populates fileId + label from /up-next.
// When fileId is null the Play button stays disabled — the hero shouldn't
// silently navigate to the detail page when the user explicitly asked to
// play.
export interface HeroPlayInfo {
  fileId: number | null
  label?: string
  // For TV hero entries, the resolved next-unwatched episode_id. Used by
  // the watch route to set entity_type=episode so the activity panel
  // shows "S01E03 · Episode title".
  episodeId?: number
}

// Hero slides are MediaItems plus an optional reason chip ("New episode",
// "Continue", …) explaining why this slide is featured.
export type HeroItem = MediaItem & { chip?: string }

const props = defineProps<{
  items: HeroItem[]
  movies?: Record<number, Movie>
  playInfo?: Record<number, HeroPlayInfo>
  // media_item_id → media_extras id of a local trailer file. Slides with an
  // entry get the trailer takeover after a short linger.
  trailers?: Record<number, number>
}>()

defineEmits<{ play: [item: MediaItem] }>()

const INTERVAL = 7000
const TRAILER_LINGER = 4000
const currentIdx = ref(0)
const heroPaused = ref(false)
const showA = ref(true)
const bgA = ref<string | null>(null)
const bgB = ref<string | null>(null)

// Template only renders when items.length > 0 (`v-if` on the root section),
// so we can safely treat this as defined inside that scope.
const current = computed(() => (props.items[currentIdx.value] ?? props.items[0])!)
const movie = computed(() => props.movies?.[current.value.id])
const posterUrl = computed(() => current.value ? usePosterUrl(current.value.id) : null)

const currentPlay = computed<HeroPlayInfo | undefined>(() => props.playInfo?.[current.value.id])
const canPlayCurrent = computed(() => !!currentPlay.value?.fileId)

// Resume detection — picks the right entity type for the hero item.
const heroEntityType = computed(() => currentPlay.value?.episodeId ? 'episode' : 'movie')
const heroEntityId = computed(() => currentPlay.value?.episodeId ?? current.value?.id ?? 0)
const { inProgress: heroInProgress } = useWatchResume(heroEntityType, heroEntityId)

const playLabel = computed(() => {
  const info = currentPlay.value
  const verb = heroInProgress.value ? 'Resume' : 'Play'
  if (!info) return verb
  if (info.label) return `${verb} ${info.label}`
  return verb
})

// --- Logo title art -------------------------------------------------------
// Probe /image/logo per slide; on 404 the h1 text stays. Probing (vs a bare
// <img @error>) avoids a broken-image flash on slides without logo art.
const logoOk = ref<Record<number, boolean>>({})
function logoUrl(id: number) {
  return `/api/media/${id}/image/logo`
}
function probeLogo(id: number) {
  if (logoOk.value[id] !== undefined) return
  const img = new Image()
  img.onload = () => { logoOk.value[id] = true }
  img.onerror = () => { logoOk.value[id] = false }
  img.src = logoUrl(id)
}

// --- Palette tint ---------------------------------------------------------
// Average the saturated pixels of the slide's backdrop (same-origin, tiny
// canvas) and let the gradient + primary button pick the color up. Falls
// back to gold when extraction fails or the image is effectively grayscale.
const tint = ref('230, 185, 74')
function extractTint(id: number) {
  const img = new Image()
  img.onload = () => {
    try {
      const c = document.createElement('canvas')
      c.width = 24
      c.height = 14
      const cx = c.getContext('2d')
      if (!cx) return
      cx.drawImage(img, 0, 0, 24, 14)
      const d = cx.getImageData(0, 0, 24, 14).data
      let r = 0, g = 0, b = 0, wsum = 0
      for (let i = 0; i < d.length; i += 4) {
        const pr = d[i]!, pg = d[i + 1]!, pb = d[i + 2]!
        const mx = Math.max(pr, pg, pb), mn = Math.min(pr, pg, pb)
        const sat = mx === 0 ? 0 : (mx - mn) / mx
        const luma = (pr * 2 + pg * 3 + pb) / 6 / 255
        // Prefer colorful mid-tones; near-black/white and gray pixels barely count.
        const w = sat * sat * (luma > 0.15 && luma < 0.85 ? 1 : 0.1) + 0.01
        r += pr * w; g += pg * w; b += pb * w; wsum += w
      }
      if (wsum < 1) return
      r /= wsum; g /= wsum; b /= wsum
      const mx = Math.max(r, g, b)
      // Lift toward a consistent brightness so dark backdrops still tint.
      if (mx > 0) { const k = 200 / mx; r = Math.min(255, r * k); g = Math.min(255, g * k); b = Math.min(255, b * k) }
      tint.value = `${Math.round(r)}, ${Math.round(g)}, ${Math.round(b)}`
    } catch { /* canvas tainted or decode issue — keep previous tint */ }
  }
  img.src = useBackdropUrl(id) ?? ''
}

// --- Trailer takeover -----------------------------------------------------
const trailerSrc = ref<string | null>(null)
const trailerVisible = ref(false)
let trailerDelay: ReturnType<typeof setTimeout> | null = null
let reducedMotion = false

function armTrailer() {
  if (trailerDelay) { clearTimeout(trailerDelay); trailerDelay = null }
  const extraID = props.trailers?.[current.value.id]
  if (!extraID || reducedMotion || props.items.length === 0) return
  trailerDelay = setTimeout(() => {
    // Takeover: the rotation timer stops; the trailer owns the slide until
    // it ends (advance) or errors (resume rotation in place).
    if (timeout) { clearTimeout(timeout); timeout = null }
    // Native <video> requests can't carry the Authorization header — pass
    // the session token in the query, same as the player's stream URLs.
    const { token } = useAuth()
    trailerSrc.value = `/api/extras/${extraID}/stream?token=${token.value}`
  }, TRAILER_LINGER)
}

function killTrailer() {
  if (trailerDelay) { clearTimeout(trailerDelay); trailerDelay = null }
  trailerSrc.value = null
  trailerVisible.value = false
}

function endTrailer(advance: boolean) {
  killTrailer()
  if (advance && props.items.length > 1) advanceHero()
  if (!heroPaused.value && props.items.length > 1) startTimer()
}

function getBackdropUrl(idx: number) {
  const item = props.items[idx]
  return item ? useBackdropUrl(item.id) : null
}

function advanceHero() {
  const nextIdx = (currentIdx.value + 1) % props.items.length
  const url = getBackdropUrl(nextIdx)
  if (showA.value) { bgB.value = url } else { bgA.value = url }
  showA.value = !showA.value
  currentIdx.value = nextIdx
}

let timeout: ReturnType<typeof setTimeout> | null = null
let startTime = 0
let remaining = INTERVAL

function startTimer() {
  startTime = Date.now()
  remaining = INTERVAL
  timeout = setTimeout(() => {
    advanceHero()
    startTimer()
  }, INTERVAL)
}

function pauseHero() {
  heroPaused.value = true
  if (timeout) clearTimeout(timeout)
  remaining -= Date.now() - startTime
}

function resumeHero() {
  heroPaused.value = false
  if (trailerSrc.value) return // trailer owns the clock
  startTime = Date.now()
  timeout = setTimeout(() => {
    advanceHero()
    startTimer()
  }, remaining)
}

function jumpHero(idx: number) {
  if (idx === currentIdx.value) return
  killTrailer()
  if (timeout) clearTimeout(timeout)
  const url = getBackdropUrl(idx)
  if (showA.value) { bgB.value = url } else { bgA.value = url }
  showA.value = !showA.value
  currentIdx.value = idx
  if (!heroPaused.value) startTimer()
}

function initBackdrops() {
  showA.value = true
  currentIdx.value = 0
  bgA.value = getBackdropUrl(0)
  bgB.value = props.items.length > 1 ? getBackdropUrl(1) : null
}

// Per-slide side effects: probe the logo, retint, re-arm the trailer linger.
watch(() => current.value?.id, (id) => {
  if (!id) return
  killTrailer()
  probeLogo(id)
  extractTint(id)
  armTrailer()
})

// items arrive async from the parent — bgA stays null if we only set it in
// onMounted. Watch the first item id so we (re)initialize as soon as data lands.
watch(
  () => props.items[0]?.id,
  (id) => {
    if (!id) return
    if (timeout) { clearTimeout(timeout); timeout = null }
    killTrailer()
    initBackdrops()
    probeLogo(id)
    extractTint(id)
    armTrailer()
    if (props.items.length > 1 && !heroPaused.value) startTimer()
  },
  { immediate: true },
)

onMounted(() => {
  reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
})

onUnmounted(() => {
  if (timeout) clearTimeout(timeout)
  killTrailer()
})
</script>

<style scoped>
.hero-featured {
  position: relative;
  height: 100%;
}
.hero-bg {
  position: absolute;
  inset: 0;
}
.hero-bg-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  opacity: 0;
  transition: opacity 1.2s ease;
}
.hero-bg-img.visible { opacity: 1; }
.hero-trailer {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  opacity: 0;
  transition: opacity 1.4s ease;
}
.hero-trailer.visible { opacity: 1; }
.hero-bg-gradient {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(to right, var(--bg-1) 0%, rgba(12,12,16,0.6) 50%, transparent 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 40%),
    radial-gradient(ellipse at 85% 110%, rgba(var(--hero-tint), 0.16), transparent 55%);
}
.hero-inner {
  position: relative;
  z-index: 2;
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 56px;
  height: 100%;
  padding: 40px 40px 48px;
  max-width: 1200px;
}
.hero-poster {
  align-self: center;
  box-shadow: 0 30px 80px rgba(0,0,0,0.7), 0 0 0 1px rgba(255,255,255,0.06);
  border-radius: var(--r-md);
  overflow: hidden;
  display: block;
  transition: transform 0.2s ease;
}
.hero-poster:hover { transform: translateY(-2px); }
.hero-title-link {
  color: inherit;
  text-decoration: none;
  display: inline-block;
}
.hero-title-link:hover .hero-title { color: var(--gold); }
.hero-title { transition: color 0.15s ease; }
.hero-logo {
  display: block;
  max-width: 420px;
  max-height: 130px;
  object-fit: contain;
  object-position: left center;
  margin: 4px 0 12px;
  filter: drop-shadow(0 4px 24px rgba(0, 0, 0, 0.6));
}
.hero-info {
  display: flex;
  flex-direction: column;
  justify-content: center;
}
.hero-counter {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--fg-3);
  letter-spacing: 0.06em;
}
.hero-title {
  font-size: 48px;
  font-weight: 600;
  letter-spacing: -0.025em;
  line-height: 1.0;
  margin: 0 0 12px;
  text-wrap: balance;
}
.hero-synopsis {
  font-size: 15px;
  line-height: 1.65;
  color: var(--fg-1);
  margin: 12px 0 0;
  max-width: 560px;
}
.hero-actions {
  display: flex;
  gap: 10px;
  margin-top: 24px;
}
.hero-actions .btn-primary {
  box-shadow: 0 0 24px rgba(var(--hero-tint), 0.25);
}
.hero-dots {
  display: flex;
  gap: 6px;
  margin-top: 24px;
}
.hero-dot {
  width: 32px;
  height: 3px;
  border-radius: 2px;
  background: rgba(255,255,255,0.2);
  position: relative;
  overflow: hidden;
  cursor: pointer;
  transition: background 0.15s;
}
.hero-dot:hover { background: rgba(255,255,255,0.35); }
.hero-dot.active { background: rgba(255,255,255,0.15); }
.hero-dot.active::after {
  content: '';
  position: absolute;
  left: 0; top: 0; bottom: 0;
  background: var(--gold);
  border-radius: 2px;
  animation: hero-fill 7s linear forwards;
}
.hero-dot.paused::after {
  animation-play-state: paused;
}
@keyframes hero-fill {
  from { width: 0; }
  to { width: 100%; }
}
@media (max-width: 900px) {
  .hero-inner { grid-template-columns: 1fr; gap: 24px; }
  .hero-poster { display: none; }
  .hero-title { font-size: 36px; }
  .hero-logo { max-width: 300px; max-height: 96px; }
}
</style>
