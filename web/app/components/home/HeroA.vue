<template>
  <section
    class="hero-featured"
    v-if="items.length"
    :style="toneVars"
    @touchstart.passive="onTouchStart"
    @touchend="onTouchEnd"
  >
    <!-- Sharp art layer, hard-clipped at the hero's bottom edge (the ledger
         seam). The blurred site-wide underlay is the global AmbientBackdrop —
         this hero feeds it a graded (v2) art claim when ambient is on, and
         always shows its own crisp copy in-hero (the old .ambient-extended
         hide-local behaviour is retired: the sharp hero is always visible). -->
    <div class="hero-bg">
      <LoadingImage
        v-if="bgA"
        :src="bgA"
        :width="1920"
        :quality="80"
        class="hero-bg-img"
        :class="{ visible: showA && !trailerVisible }"
        @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
      />
      <LoadingImage
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
      <!-- Readability grade (literal dark over raw artwork — the CLAUDE.md
           exception) + bottom-left tone leak. Mirrors HeroCanvas / heya2.css. -->
      <div class="hero-grade" />
      <div class="hero-tone" />
    </div>

    <div class="hero-inner">
      <div class="grow hero-ink">
        <div class="eyebrow">
          <span>Featured</span>
          <span class="sep">&middot;</span>
          <span>{{ current.chip || 'New in library' }}</span>
          <span class="sep">&middot;</span>
          <span>{{ typeLabel }}</span>
        </div>

        <NuxtLink :to="mediaUrl(current)" class="title-link">
          <h1 v-if="logoOk[current.id]" class="title title-art">
            <LoadingImage
              class="title-logo"
              :src="logoUrl(current)"
              :alt="current.title"
              :width="500"
            />
          </h1>
          <h1 v-else class="title">{{ current.title }}</h1>
        </NuxtLink>

        <p class="metaline">
          <span v-if="current.year">{{ current.year }}</span>
          <template v-if="runtimeUpper"><span class="dot">&middot;</span><span>{{ runtimeUpper }}</span></template>
          <template v-if="ratingStr"><span class="dot">&middot;</span><span>&#9733; {{ ratingStr }}</span></template>
          <template v-if="genres.length">
            <span class="dot">&middot;</span>
            <NuxtLink v-for="g in genres" :key="g" :to="`/genre/${encodeURIComponent(g)}`" class="genre">{{ g }}</NuxtLink>
          </template>
        </p>

        <div class="actions">
          <button
            class="btn-play"
            :disabled="!canPlayCurrent"
            @click="$emit('play', current)"
          >
            <span class="tri" />
            <span class="hero-play-label">{{ playLabel }}</span>
          </button>
          <NuxtLink :to="mediaUrl(current)" class="pill">
            <Icon name="info" :size="15" />
            Details
          </NuxtLink>
          <span class="grow-spacer" />
          <span v-if="items.length > 1" class="hero-count">
            {{ pad(currentIdx + 1) }}<span class="dim"> / {{ pad(items.length) }}</span>
          </span>
        </div>
      </div>
    </div>

    <!-- Slide controls: the shared prev/pause/next cluster, teleported into
         HeroDeck's top-right slot beside the mode tabs. The ring is the 30s
         rotation clock; a trailer takeover freezes it, and any manual move
         re-keys it — a fresh full window. -->
    <Teleport defer to="#hero-deck-aux">
      <CycleControls
        v-if="items.length > 1"
        v-model:paused="userPaused"
        :ring-paused="!!trailerSrc"
        :cycle-key="cycleKey"
        :duration="INTERVAL"
        item-label="slide"
        @prev="retreat"
        @next="advance"
      />
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

// One rotation window — CycleControls' ring animates at exactly this.
const INTERVAL = 30_000
const TRAILER_LINGER = 4000
const currentIdx = ref(0)
const cycleKey = ref(0)
const showA = ref(true)
const bgA = ref<string | null>(null)
const bgB = ref<string | null>(null)

// Ambient extension: with the ambient background on, this hero's current
// backdrop is also published (graded v2) as the full-page blurred underlay via
// the AmbientBackdrop layer, which follows the deck's rotation through this
// watcher. The sharp local copy always stays visible in-hero.
const { ambientEnabled } = useAppearance()
const background = useBackground()
const bgImg = useBackgroundImageTools()
// Warm the NEXT slide's backdrop whenever the current one settles, so the
// ring-driven advance (and a shown deck's first rotation) crossfades from
// a hot cache instead of stuttering mid-fade.
watch(() => currentIdx.value + props.items.length, () => {
  if (props.items.length <= 1) return
  const url = getBackdropUrl((currentIdx.value + 1) % props.items.length)
  if (url) bgImg.warm(url)
}, { immediate: true })
const currentBg = computed(() => (showA.value ? bgA.value : bgB.value) || null)
watch([currentBg, ambientEnabled], ([url, on]) => {
  if (on && url) background.set(url, { grade: 'v2' })
  else background.clear()
}, { immediate: true })

// Dominant-tone sampling → --tone. The w=64 thumb is enough for a 24×24 canvas
// average; the whole hero (eyebrow accent, tone-glow Play, tone leak) follows
// the current slide precisely, in every ambient mode. Falls back to the page
// --tone (inherited) when sampling fails.
const tone = ref<ImageTone | null>(null)
watch(currentBg, async (url) => {
  tone.value = url ? await sampleImageTone(bgImg.thumb(url)) : null
}, { immediate: true })
const { toneFollowEnabled } = useAppearance()
const toneVars = computed<Record<string, string> | undefined>(() => {
  if (!toneFollowEnabled.value) return undefined
  const t = tone.value
  if (!t) return undefined
  const m = t.main.match(/\d+/g)
  if (!m) return undefined
  return toneStyleVars(t)
})

// Template only renders when items.length > 0 (`v-if` on the root section),
// so we can safely treat this as defined inside that scope.
const current = computed(() => (props.items[currentIdx.value] ?? props.items[0])!)
const movie = computed(() => props.movies?.[current.value.id])

const typeLabel = computed(() => (current.value?.media_type === 'movie' ? 'Film' : 'Series'))
const genres = computed(() => (movie.value?.genres ?? []).slice(0, 3))
const ratingStr = computed(() => {
  const r = movie.value?.rating
  return r ? parseFloat(String(r)).toFixed(1) : ''
})
const runtimeUpper = computed(() => {
  const mins = movie.value?.runtime_minutes
  if (!mins) return ''
  const h = Math.floor(mins / 60)
  const m = mins % 60
  return [h ? `${h}H` : '', m ? `${m}M` : ''].filter(Boolean).join(' ')
})
function pad(n: number) { return String(n).padStart(2, '0') }

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
// Probe /image/logo per slide; on 404 the display title stays. Probing (vs a
// bare <img @error>) avoids a broken-image flash on slides without logo art.
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
    // Takeover: setting trailerSrc freezes the cycle ring (ring-paused);
    // the trailer owns the slide until it ends (advance) or errors
    // (rotation resumes in place).
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

function endTrailer(advanceSlide: boolean) {
  killTrailer()
  // Fresh full window either way; only move on if the user hasn't paused.
  if (advanceSlide && !userPaused.value && props.items.length > 1) advanceHero()
  cycleKey.value++
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

// ── Ring clock (CycleControls) ──
// The cluster's progress ring IS the 30s timer: its animationend calls
// advance(); every slide change re-keys it via cycleKey for a fresh full
// window. The trailer takeover freezes the ring through the ring-paused prop;
// the sticky pause is the v-model.
const userPaused = ref(false)

function advance() {
  if (props.items.length <= 1) return
  killTrailer()
  advanceHero()
  cycleKey.value++
}

function retreat() {
  if (props.items.length <= 1) return
  jumpHero((currentIdx.value - 1 + props.items.length) % props.items.length)
}

function jumpHero(idx: number) {
  if (idx === currentIdx.value) return
  killTrailer()
  const url = getBackdropUrl(idx)
  if (showA.value) { bgB.value = url } else { bgA.value = url }
  showA.value = !showA.value
  currentIdx.value = idx
  cycleKey.value++
}

// --- Touch swipe between slides (phone) ------------------------------------
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

// Per-slide side effects: probe the logo, re-arm the trailer linger.
watch(() => current.value, (item) => {
  if (!item) return
  killTrailer()
  probeLogo(item)
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
    killTrailer()
    initBackdrops()
    probeLogo(item)
    armTrailer()
    cycleKey.value++ // fresh rotation window for the fresh deck
  },
  { immediate: true },
)

onMounted(() => {
  reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
})

onUnmounted(() => {
  killTrailer()
})
</script>

<style scoped>
/* Heya 2.0 featured hero. Text rides the literal-dark art grade, so --oink
   keeps it light in every theme (dark/oled/light) — a themed --ink would flip
   near-black in light mode and vanish over the artwork. --tone / --tone-rgb /
   --tone-ink are published on the root per slide (see toneVars); when sampling
   hasn't landed they inherit the page-root tone. */
.hero-featured {
  position: relative;
  height: 100%;
  --oink: 233 236 242;
}

.hero-bg {
  position: absolute;
  inset: 0;
  overflow: hidden; /* THE hard clip at the hero's bottom edge (the ledger seam) */
}
.hero-bg-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: center 22%;
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

/* Readability grade — literal dark, painted directly over raw artwork
   (CLAUDE.md exception). Matches HeroCanvas .hc-grade / heya2.css
   .hero-art::after. */
.hero-grade {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background:
    linear-gradient(90deg, rgb(10 12 16 / 0.82), rgb(10 12 16 / 0.3) 38%, rgb(10 12 16 / 0.05) 68%),
    linear-gradient(to top, rgb(10 12 16 / 0.78) 0%, rgb(10 12 16 / 0.3) 24%, rgb(10 12 16 / 0.12) 58%, rgb(10 12 16 / 0.34) 100%);
}
.hero-tone {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: radial-gradient(90% 70% at 8% 100%, rgb(var(--tone-rgb) / 0.18), transparent 60%);
}

.hero-inner {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: flex-end;
  height: 100%;
  /* Top padding clears the glass topbar + the deck's top-right tab/nav
     cluster; content is bottom-anchored at the seam. */
  padding: 110px var(--pad-fluid) 44px;
}
.hero-inner > .grow { flex: 1; min-width: 0; }

/* mono content eyebrow (heya2.css .eyebrow) */
.eyebrow {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 16px;
  font: 600 11.5px var(--font-mono);
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--tone);
  text-shadow: 0 0 12px rgb(0 0 0 / 0.5);
}
.eyebrow .sep { color: rgb(var(--oink) / 0.3); }

/* Archivo display title (+ logo-as-title art) */
.title-link { color: inherit; text-decoration: none; display: inline-block; }
.title-link:hover .title:not(.title-art) { color: var(--tone); }
.title {
  font-family: var(--font-display);
  font-size: clamp(2.3rem, 4.6vw, 3.9rem);
  font-weight: 800;
  font-variation-settings: "wdth" 115;
  letter-spacing: -0.022em;
  line-height: 0.99;
  text-wrap: balance;
  max-width: 18ch;
  color: rgb(var(--oink) / 0.98);
  text-shadow: 0 2px 30px rgb(0 0 0 / 0.45);
  margin: 0;
  transition: color 0.15s ease;
}
.title-art { line-height: 0; }
.title-logo {
  display: block;
  width: auto;
  height: auto;
  max-width: min(440px, 100%);
  max-height: 128px;
  object-fit: contain;
  object-position: left center;
  filter: drop-shadow(0 6px 24px rgb(0 0 0 / 0.55)); /* on artwork — literal */
}

.metaline {
  margin-top: 14px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 12px;
  font: 500 12.5px var(--font-mono);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: rgb(var(--oink) / 0.72);
  text-shadow: 0 0 12px rgb(0 0 0 / 0.55);
}
.metaline .dot { color: rgb(var(--tone-rgb) / 0.85); }
.metaline .genre {
  border-bottom: 1px solid rgb(var(--oink) / 0.25);
  padding-bottom: 1px;
  transition: color 0.15s, border-color 0.15s;
}
.metaline .genre:hover { color: rgb(var(--oink) / 0.95); border-color: rgb(var(--tone-rgb) / 0.6); }

/* actions */
.actions {
  margin-top: 26px;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.grow-spacer { flex: 1 1 auto; }
.hero-count {
  font: 600 11px var(--font-mono);
  letter-spacing: 0.2em;
  color: rgb(var(--oink) / 0.5);
}
.hero-count .dim { color: rgb(var(--oink) / 0.25); }

/* tone-glowing primary Play + tone-tinted secondary pill (heya2.css). The
   tone shifts blend rather than snap as the deck cycles. */
.btn-play {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 13px 26px 13px 20px;
  border: 0;
  border-radius: 999px;
  cursor: pointer;
  background: var(--tone);
  color: var(--tone-ink, #0a0c10);
  font: 650 14px var(--font-sans);
  letter-spacing: 0.01em;
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.45),
    0 0 24px rgb(var(--tone-rgb) / 0.4),
    6px 10px 36px -8px rgb(var(--tone-rgb) / 0.75);
  transition:
    transform 0.15s ease,
    background 0.9s cubic-bezier(0.22, 1, 0.36, 1),
    color 0.9s cubic-bezier(0.22, 1, 0.36, 1),
    box-shadow 0.5s ease;
}
.btn-play:hover {
  transform: translateY(-1px);
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.6),
    0 0 40px rgb(var(--tone-rgb) / 0.6),
    8px 14px 48px -8px rgb(var(--tone-rgb) / 0.9);
}
.btn-play[disabled] {
  cursor: not-allowed;
  opacity: 0.4;
  box-shadow: 0 0 0 1px rgb(var(--oink) / 0.14);
  transform: none;
}
.btn-play .tri {
  width: 0; height: 0;
  border-left: 11px solid var(--tone-ink, #0a0c10);
  border-top: 7px solid transparent;
  border-bottom: 7px solid transparent;
}

.pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 12px 18px;
  border-radius: 999px;
  cursor: pointer;
  text-decoration: none;
  border: 1px solid rgb(var(--tone-rgb) / 0.3);
  background: rgb(var(--tone-rgb) / 0.08);
  color: rgb(var(--oink) / 0.9);
  font: 550 13px var(--font-sans);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  box-shadow: 0 0 16px rgb(var(--tone-rgb) / 0.14), 5px 8px 22px -10px rgb(0 0 0 / 0.7);
  transition: border-color 0.15s, background 0.15s, box-shadow 0.15s, transform 0.15s, color 0.15s;
}
.pill:hover {
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.15);
  color: rgb(var(--oink));
  box-shadow: 0 0 24px rgb(var(--tone-rgb) / 0.28), 6px 10px 26px -10px rgb(0 0 0 / 0.75);
  transform: translateY(-1px);
}

@media (max-width: 900px) {
  .hero-inner { padding: 96px var(--pad-fluid) 32px; }
  .title { font-size: clamp(2rem, 7vw, 2.9rem); }
  .title-logo { max-width: 300px; max-height: 96px; }
}

/* Phone: tighter hero, the primary CTA fills its row so a long
   "Resume S03E12 - Title" label truncates instead of pushing Details off. */
@media (max-width: 720px) {
  .hero-inner { padding: 84px var(--pad-fluid) 22px; }
  .eyebrow { margin-bottom: 12px; gap: 8px; }
  .title { font-size: clamp(1.8rem, 8vw, 2.5rem); }
  .title-logo { max-width: 220px; max-height: 64px; }
  .metaline { font-size: 11.5px; }
  .actions { margin-top: 18px; gap: 8px; }
  .btn-play { flex: 1 1 auto; min-width: 0; justify-content: center; height: 48px; }
  .pill { flex: 0 0 auto; height: 48px; }
  .hero-count { display: none; }
  .hero-play-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}
</style>
