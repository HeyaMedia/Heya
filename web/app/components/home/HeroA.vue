<template>
  <section
    class="hero-featured"
    v-if="items.length"
    :style="{ '--hero-tint': tint }"
    @touchstart.passive="onTouchStart"
    @touchend="onTouchEnd"
  >
    <div class="hero-bg" :class="{ 'ambient-extended': ambientEnabled }">
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
          <span v-if="movie?.rating" class="chip gold hero-rating">
            <Icon name="star" :size="12" />
            {{ parseFloat(String(movie.rating)).toFixed(1) }}
          </span>
        </div>

        <NuxtLink :to="mediaUrl(current)" class="hero-title-link">
          <NuxtImg
            v-if="logoOk[current.id]"
            class="hero-logo"
            :src="logoUrl(current)"
            :alt="current.title"
            :width="500"
          />
          <h1 v-else class="hero-title">{{ current.title }}</h1>
        </NuxtLink>

        <div class="hero-meta-row" v-if="current.year || movie?.runtime_minutes">
          <span v-if="current.year">{{ current.year }}</span>
          <span v-if="movie?.runtime_minutes" class="dot" />
          <span v-if="movie?.runtime_minutes">{{ Math.floor(movie.runtime_minutes / 60) }}h {{ movie.runtime_minutes % 60 }}m</span>
        </div>

        <p class="hero-synopsis" v-if="current.description">
          {{ current.description.slice(0, 180) }}{{ current.description.length > 180 ? '…' : '' }}
        </p>

        <div class="hero-actions">
          <button
            class="btn btn-primary"
            :style="playStyle"
            :disabled="!canPlayCurrent"
            @click="$emit('play', current)"
          >
            <Icon name="play" :size="16" />
            <span class="hero-play-label">{{ playLabel }}</span>
          </button>
          <NuxtLink :to="mediaUrl(current)" class="btn btn-ghost" :style="detailsStyle">
            <Icon name="info" :size="16" />
            Details
          </NuxtLink>
        </div>

      </div>
    </div>

    <!-- Slide navigator: play/pause + counter + bars in one glass pill,
         teleported into HeroDeck's top-right cluster beside the mode tabs
         (defer: the #hero-deck-aux target renders in the same tick).
         Clicking a bar pins that slide for 30s before rotation resumes. -->
    <Teleport defer to="#hero-deck-aux">
    <div class="hero-nav" v-if="items.length > 1" @mouseenter="pauseHero" @mouseleave="resumeHero">
      <button
        class="hero-nav-toggle"
        :aria-label="userPaused ? 'Resume slide rotation' : 'Pause slide rotation'"
        @click="toggleUserPause"
      >
        <Icon :name="userPaused ? 'play' : 'pause'" :size="12" />
      </button>
      <span class="hero-counter">{{ String(currentIdx + 1).padStart(2, '0') }} / {{ String(items.length).padStart(2, '0') }}</span>
      <div class="hero-dots">
        <button
          v-for="(_, i) in items"
          :key="`hero-${i}-${currentIdx}`"
          class="hero-dot"
          :class="{ active: i === currentIdx, paused: (heroPaused || !!trailerSrc) && i === currentIdx }"
          @click="pinHero(i)"
        />
      </div>
    </div>
    </Teleport>
  </section>
</template>

<script setup lang="ts">
import type { MediaItem, Movie } from '~~/shared/types'
import type { ImageTone } from '~/composables/useImageTone'

// playInfo: per-item playback hint resolved by the parent. Movies populate
// fileId from detail.files[0]; TV populates fileId + label from /up-next.
// When fileId is null the Play button stays disabled — the hero shouldn't
// silently navigate to the detail page when the user explicitly asked to
// play.
export interface HeroPlayInfo {
  fileId: string | number | null
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

// Keep in sync with the .hero-dot::after `hero-fill 30s` animation below.
const INTERVAL = 30_000
const TRAILER_LINGER = 4000
const currentIdx = ref(0)
const heroPaused = ref(false)
const showA = ref(true)
const bgA = ref<string | null>(null)
const bgB = ref<string | null>(null)

// Ambient extension: with the ambient background on, this hero's current
// backdrop IS the page background (full-page, sticky) — the local hero
// image hides via .ambient-extended and the AmbientBackdrop layer follows
// the deck's rotation through this watcher.
const { ambientEnabled } = useAppearance()
const background = useBackground()
const currentBg = computed(() => (showA.value ? bgA.value : bgB.value) || null)
watch([currentBg, ambientEnabled], ([url, on]) => {
  if (on && url) background.set(url)
  else background.clear()
}, { immediate: true })

// Artwork-adaptive buttons: Play carries the backdrop's dominant tone,
// Details a soft tint of its complement. Falls back to the theme accent
// when sampling fails (no backdrop / decode error).
const tone = ref<ImageTone | null>(null)
watch(currentBg, async (url) => {
  tone.value = url ? await sampleImageTone(url) : null
}, { immediate: true })
const playStyle = computed(() =>
  tone.value ? { background: tone.value.main, color: tone.value.ink } : undefined)
const detailsStyle = computed(() =>
  tone.value
    ? {
        background: `rgb(${tone.value.complementTriplet} / 0.16)`,
        boxShadow: `inset 0 0 0 1px rgb(${tone.value.complementTriplet} / 0.35)`,
      }
    : undefined)

// Template only renders when items.length > 0 (`v-if` on the root section),
// so we can safely treat this as defined inside that scope.
const current = computed(() => (props.items[currentIdx.value] ?? props.items[0])!)
const movie = computed(() => props.movies?.[current.value.id])
const posterUrl = computed(() => current.value ? usePosterUrl(current.value) : null)

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
function logoUrl(item: HeroItem) {
  return `/api/media/${useMediaImageKey(item)}/image/logo`
}
function probeLogo(item: HeroItem) {
  if (logoOk.value[item.id] !== undefined) return
  const img = new Image()
  img.onload = () => { logoOk.value[item.id] = true }
  img.onerror = () => { logoOk.value[item.id] = false }
  img.src = logoUrl(item)
}

// --- Palette tint ---------------------------------------------------------
// Average the saturated pixels of the slide's backdrop (same-origin, tiny
// canvas) and let the gradient + primary button pick the color up. Falls
// back to gold when extraction fails or the image is effectively grayscale.
const tint = ref('230, 185, 74')
function extractTint(item: HeroItem) {
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
  img.src = useBackdropUrl(item) ?? ''
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
  // A pinned slide holds regardless of the trailer's outcome — advancing
  // here would break the 30s pin promise. The pin callback resumes rotation.
  if (pinTimer) return
  if (advance && props.items.length > 1) advanceHero()
  if (!heroPaused.value && props.items.length > 1) startTimer()
}

function getBackdropUrl(idx: number) {
  const item = props.items[idx]
  return item ? useBackdropUrl(item) : null
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
  // Idempotent: overlapping resume paths (pause toggle clicked while
  // hovering the pill, then mouseleave firing resumeHero again) must
  // replace the pending chain, never stack a second one.
  if (timeout) clearTimeout(timeout)
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

// Sticky user pause (the ⏸ in the nav pill) — outranks hover resume and
// pins alike; only the button itself un-pauses.
const userPaused = ref(false)
function toggleUserPause() {
  userPaused.value = !userPaused.value
  if (userPaused.value) {
    if (pinTimer) { clearTimeout(pinTimer); pinTimer = null }
    pauseHero()
  } else {
    remaining = INTERVAL
    resumeHero()
  }
}

function resumeHero() {
  if (userPaused.value) return // sticky pause: only the toggle resumes
  if (pinTimer) return // a clicked slide holds until its 30s elapse
  heroPaused.value = false
  if (trailerSrc.value) return // trailer owns the clock
  if (timeout) clearTimeout(timeout) // never stack a second chain
  startTime = Date.now()
  timeout = setTimeout(() => {
    advanceHero()
    startTimer()
  }, remaining)
}

// Click-to-pin: jump to the slide and hold it for 30s before rotation
// resumes. Clicking another bar re-pins; hover pause/resume is bypassed
// while pinned (see the guard in resumeHero).
const PIN_MS = 30_000
let pinTimer: ReturnType<typeof setTimeout> | null = null
function pinHero(idx: number) {
  jumpHero(idx)
  heroPaused.value = true
  if (timeout) clearTimeout(timeout)
  if (pinTimer) clearTimeout(pinTimer)
  pinTimer = setTimeout(() => {
    pinTimer = null
    remaining = INTERVAL
    resumeHero()
  }, PIN_MS)
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

// --- Touch swipe between slides (phone) ------------------------------------
// The dots are already tappable and auto-rotate keeps running either way, but
// a horizontal drag is the gesture phone users reach for first. Only commits
// to a slide change past a clear horizontal threshold so it never fights the
// page's own vertical scroll or a plain tap on a link/button underneath.
let touchStartX: number | null = null
let touchStartY: number | null = null

function onTouchStart(e: TouchEvent) {
  if (props.items.length <= 1) return
  const t = e.touches[0]
  if (!t) return
  touchStartX = t.clientX
  touchStartY = t.clientY
}

function onTouchEnd(e: TouchEvent) {
  if (touchStartX === null || props.items.length <= 1) return
  const startX = touchStartX
  const startY = touchStartY ?? 0
  touchStartX = null
  touchStartY = null
  const t = e.changedTouches[0]
  if (!t) return
  const dx = t.clientX - startX
  const dy = t.clientY - startY
  if (Math.abs(dx) < 40 || Math.abs(dx) < Math.abs(dy) * 1.2) return
  const dir = dx < 0 ? 1 : -1
  jumpHero((currentIdx.value + dir + props.items.length) % props.items.length)
}

function initBackdrops() {
  showA.value = true
  currentIdx.value = 0
  bgA.value = getBackdropUrl(0)
  bgB.value = props.items.length > 1 ? getBackdropUrl(1) : null
}

// Per-slide side effects: probe the logo, retint, re-arm the trailer linger.
watch(() => current.value, (item) => {
  if (!item) return
  killTrailer()
  probeLogo(item)
  extractTint(item)
  armTrailer()
})

// items arrive async from the parent — bgA stays null if we only set it in
// onMounted. Watch the first item id so we (re)initialize as soon as data lands.
watch(
  () => props.items[0]?.id,
  (id) => {
    if (!id) return
    const item = props.items[0]
    if (!item) return
    if (timeout) { clearTimeout(timeout); timeout = null }
    killTrailer()
    initBackdrops()
    probeLogo(item)
    extractTint(item)
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
  // A live pin outliving the component would fire resumeHero() detached
  // and restart the rotation chain against a dead instance.
  if (pinTimer) { clearTimeout(pinTimer); pinTimer = null }
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
    linear-gradient(to right, var(--bg-1) 0%, color-mix(in srgb, var(--bg-1) 60%, transparent) 50%, transparent 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 40%),
    radial-gradient(ellipse at 85% 110%, rgba(var(--hero-tint), 0.16), transparent 55%);
}
/* Ambient extension: the AmbientBackdrop layer shows this hero's current
   image full-page (see the background watcher), so the local copy hides —
   its different crop would seam at the hero edges — and the fade softens
   so the artwork continues past the hero bottom instead of ending at
   solid canvas. The trailer video still plays locally on top. */
.hero-bg.ambient-extended .hero-bg-img { display: none; }
/* No bottom fade in extended mode — the hero's bottom edge must match the
   ambient scrim exactly or a hard cutoff line appears against the content
   below. Left gradient covers the text column; tint stays. */
.hero-bg.ambient-extended .hero-bg-gradient { display: none; }
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
  box-shadow: 0 30px 80px rgba(0,0,0,0.7), 0 0 0 1px rgb(var(--ink) / 0.06);
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
  filter: drop-shadow(0 4px 24px rgba(0, 0, 0, 0.6)); /* on artwork — stays literal */
}
.hero-info {
  display: flex;
  flex-direction: column;
  justify-content: center;
  position: relative;
}
/* Readability patch: an oversized soft blob behind the whole text cluster.
   Derives from --bg-1, so it's a paper wash behind dark text in light mode
   and a dark wash behind light text in dark — halos alone can't save
   light-mode ink over busy art. Sits behind the content but above the
   ambient artwork (hero-inner is its own stacking context). */
.hero-info::before {
  content: '';
  position: absolute;
  inset: -110px -160px -110px -140px;
  z-index: -1;
  pointer-events: none;
  /* Long, gentle falloff + heavy blur: the wash should be felt, not seen —
     no locatable edge against the surrounding artwork. */
  background: radial-gradient(ellipse 75% 70% at 40% 50%,
    color-mix(in srgb, var(--bg-1) 58%, transparent) 0%,
    color-mix(in srgb, var(--bg-1) 40%, transparent) 40%,
    color-mix(in srgb, var(--bg-1) 18%, transparent) 68%,
    transparent 92%);
  filter: blur(28px);
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
  height: 7px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.2);
  position: relative;
  overflow: hidden;
  cursor: pointer;
  transition: background 0.15s;
}
/* Invisible hit-area extension — mouse users deserve fat targets too. */
.hero-dot::before {
  content: '';
  position: absolute;
  inset: -7px -3px;
}
.hero-dot:hover { background: rgb(var(--ink) / 0.35); }
.hero-dot.active { background: rgb(var(--ink) / 0.15); }
.hero-dot.active::after {
  content: '';
  position: absolute;
  left: 0; top: 0; bottom: 0;
  background: var(--gold);
  border-radius: 2px;
  animation: hero-fill 30s linear forwards;
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
/* Phone (W3a): bottom-anchor the content island instead of the desktop's
   vertical centering (that centering was the main source of the "very tall,
   mostly empty" hero on a narrow screen — see docs/responsive-plan.md W3a).
   Synopsis drops (title + rating + actions is the mobile-hero convention);
   Play/Details go side-by-side and both stay fully on screen. */
@media (max-width: 720px) {
  .hero-inner { padding: 16px 16px 20px; }
  .hero-info { justify-content: flex-end; }
  .hero-bg-gradient {
    background:
      linear-gradient(to top, var(--bg-1) 0%, color-mix(in srgb, var(--bg-1) 92%, transparent) 24%, color-mix(in srgb, var(--bg-1) 50%, transparent) 50%, transparent 78%),
      radial-gradient(ellipse at 50% 100%, rgba(var(--hero-tint), 0.18), transparent 60%);
  }
  .hero-synopsis { display: none; }
  .hero-title { font-size: 26px; line-height: 1.1; }
  .hero-logo { max-width: 220px; max-height: 64px; margin: 2px 0 8px; }
  .hero-meta-row { font-size: 12px; }
  .hero-actions { margin-top: 16px; gap: 8px; }
  /* Play grows to fill the row, Details keeps its natural width — so a long
     "Play S03E12 - Episode Title" label truncates instead of shoving Details
     off the right edge (the bug this package was written to fix). */
  .hero-actions .btn-primary { flex: 1 1 auto; min-width: 0; }
  .hero-actions .btn-ghost { flex: 0 0 auto; }
  .hero-play-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hero-dots { margin-top: 14px; gap: 10px; }
  /* Dots stay visually 32x3 but grow their hit area to ~44px tall via an
     invisible ::before — overflow must switch to visible for it to render
     (the ::after progress-fill is still clipped to the dot's own box because
     it's positioned by inset:0, not by this rule). */
  .hero-dot { overflow: visible; }
  .hero-dot::before {
    content: '';
    position: absolute;
    inset: -21px -6px;
  }
}

/* Dominant-tone shifts blend instead of snapping as the deck cycles. */
.hero-actions .btn {
  transition: background 0.9s cubic-bezier(0.22, 1, 0.36, 1),
              color 0.9s cubic-bezier(0.22, 1, 0.36, 1),
              box-shadow 0.9s cubic-bezier(0.22, 1, 0.36, 1);
}

/* Slide navigator pill — lives in HeroDeck's top-right cluster (teleport),
   over artwork in any theme. */
.hero-nav {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 3px 12px 3px 5px;
  border-radius: 999px;
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
}
.hero-nav .hero-counter { text-shadow: none; }
.hero-nav .hero-dots { margin: 0; }
.hero-nav-toggle {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-1);
  transition: background 0.12s, color 0.12s;
}
.hero-nav-toggle:hover { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }

/* ── Art-proof readability (the hero sits on raw artwork in ambient mode) ──
   A --bg-1 halo adapts per theme: paper glow behind dark text in light,
   dark glow behind light text in dark. */
.hero-counter,
.hero-meta-row,
.hero-synopsis {
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
.hero-title { text-shadow: 0 2px 20px rgb(var(--shade) / 0.30), 0 0 14px var(--bg-1); }
/* The reason chip gets a real glass backing instead of an 18% tint. */
.hero-info :deep(.chip) {
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
}
/* Carousel bars: stronger base + a shade outline so they survive bright art. */
.hero-dot {
  background: rgb(var(--ink) / 0.38) !important;
  box-shadow: 0 0 0 1px rgb(var(--shade) / 0.22), 0 1px 4px rgb(var(--shade) / 0.30);
}
</style>
