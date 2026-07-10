<template>
  <div
    v-if="active"
    class="ambient-backdrop"
    :class="{ 'override-mode': !!override }"
    :style="{ '--ambient-opacity': intensity / 100 }"
    aria-hidden="true"
  >
    <NuxtImg
      v-if="srcA"
      :src="srcA"
      class="ambient-img"
      :class="{ visible: showA, drift: !reducedMotion && !override }"
      width="1920"
      quality="70"
      alt=""
    />
    <NuxtImg
      v-if="srcB"
      :src="srcB"
      class="ambient-img"
      :class="{ visible: !showA, drift: !reducedMotion && !override }"
      width="1920"
      quality="70"
      alt=""
    />
    <div class="ambient-scrim" />
  </div>
</template>

<script setup lang="ts">
// Full-viewport ambient background: the page's real background is artwork,
// with the theme canvas as a scrim ON TOP — sticky while content scrolls.
//
// Two sources, in priority order:
//   1. Override (useAmbientOverride): a page that owns a specific image —
//      detail heroes, the home hero deck — pushes its current backdrop, so
//      the hero image extends full-page. The owner drives rotation.
//   2. Route pool: random artwork from /api/media/ambient-backdrops scoped
//      by section (home = all libraries, /movies = movies, /tv = tv,
//      /music = artists, /books = books), rotated every 25s.
//
// Mounted as the first child of `.app` with z-index:-1 — .app carries no
// background (heya.css) so the layer sits between the body canvas and all
// content. Turning ambient off restores the plain-canvas look everywhere.
import { useQuery } from '@tanstack/vue-query'

const { $heya } = useNuxtApp()
const route = useRoute()
const { prefs, ambientEnabled } = useAppearance()
const { isAuthenticated } = useAuth()
const override = useAmbientOverride()

const ROTATE_MS = 25_000
const POOL_SIZE = 30

interface Candidate {
  id: number
  public_id: string
  media_type: string
  title: string
  slug: string
  has_backdrop: boolean
}

// Route → media-type context. Sections with their own full-screen video
// (watch) opt out via the empty list.
const types = computed<string[]>(() => {
  const p = route.path
  if (p.startsWith('/watch')) return []
  if (p.startsWith('/movies') || p.startsWith('/collection')) return ['movie']
  if (p.startsWith('/tv')) return ['tv']
  if (p.startsWith('/music')) return ['music']
  if (p.startsWith('/books')) return ['book']
  return ['movie', 'tv', 'music', 'book']
})
const typesKey = computed(() => types.value.join(','))

const reducedMotion = import.meta.client
  ? window.matchMedia('(prefers-reduced-motion: reduce)').matches
  : false

const active = computed(
  () => ambientEnabled.value && isAuthenticated.value && types.value.length > 0,
)
const intensity = computed(() => prefs.value.ambientIntensity || 30)

const poolQuery = useQuery({
  queryKey: computed(() => ['ambient-backdrops', typesKey.value]),
  queryFn: async () =>
    (await $heya('/api/media/ambient-backdrops', {
      query: { types: typesKey.value, limit: POOL_SIZE },
    })) as Candidate[],
  // Pool only feeds the fallback mode; don't fetch while an owner drives.
  enabled: computed(() => active.value && !override.value),
  staleTime: 1000 * 60 * 15,
})

function urlFor(c: Candidate): string {
  const type = c.has_backdrop ? 'backdrop' : 'poster'
  return `/api/media/${c.public_id}/image/${type}`
}

// A/B crossfade state.
const srcA = ref<string | null>(null)
const srcB = ref<string | null>(null)
const showA = ref(true)
const shown = ref<string | null>(null)
let cursor = 0
let timer: ReturnType<typeof setTimeout> | null = null

function stop() {
  if (timer) clearTimeout(timer)
  timer = null
}

/** Preload off-DOM, then crossfade to the url. */
function showImage(url: string, then?: () => void) {
  if (shown.value === url) { then?.(); return }
  const img = new Image()
  img.onload = () => {
    if (showA.value) srcB.value = url
    else srcA.value = url
    showA.value = !showA.value
    shown.value = url
    then?.()
  }
  img.onerror = () => then?.()
  img.src = url
}

function scheduleRotation() {
  stop()
  if (reducedMotion || override.value) return
  timer = setTimeout(advance, ROTATE_MS)
}

function advance() {
  const pool = poolQuery.data.value
  if (!pool?.length || override.value) return
  cursor = (cursor + 1) % pool.length
  showImage(urlFor(pool[cursor]!), scheduleRotation)
}

// Source arbitration: override wins; otherwise ride the pool.
watch(
  [() => override.value, () => poolQuery.data.value, active],
  ([ov, pool, on]) => {
    if (!on) { stop(); return }
    if (ov) {
      stop()
      showImage(ov.url)
      return
    }
    if (!pool?.length) { stop(); return }
    // (Re)enter pool mode — pick a random start if the current image
    // isn't from this pool anyway.
    cursor = Math.floor(Math.random() * pool.length)
    showImage(urlFor(pool[cursor]!), scheduleRotation)
  },
  { immediate: true, deep: false },
)

// Don't burn bandwidth/CPU while the tab is hidden.
function onVisibility() {
  if (document.hidden) stop()
  else if (active.value && !override.value) scheduleRotation()
}
onMounted(() => document.addEventListener('visibilitychange', onVisibility))
onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibility)
  stop()
})
</script>

<style scoped>
.ambient-backdrop {
  position: absolute;
  inset: 0;
  /* z-index 0, NOT -1: negative-z children paint in the ROOT stacking
     context (nothing between here and <html> creates one), and when <html>
     has its own background the body's background paints AFTER negative-z
     layers — hiding this layer entirely in some engines. At z:0 the layer
     paints with the positioned band (above all in-flow backgrounds
     including body's) while every later sibling in .app still paints above
     it by tree order. */
  z-index: 0;
  overflow: hidden;
  pointer-events: none;
}

.ambient-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  opacity: 0;
  transition: opacity 2.5s ease;
}
.ambient-img.visible {
  /* Intensity maps to real presence: 30% setting ≈ 0.55 image opacity.
     The scrim handles legibility; without the boost the artwork reads
     as "off" — especially on the light theme's paper canvas. */
  opacity: min(calc(var(--ambient-opacity, 0.3) * 1.8), 0.92);
}
/* Owner-driven artwork (hero extended full-page) is the page's identity —
   let it carry more presence and switch faster with its hero. */
.override-mode .ambient-img { transition: opacity 1.2s ease; }
.override-mode .ambient-img.visible {
  opacity: min(calc(var(--ambient-opacity, 0.3) * 2.4), 0.95);
}

/* Slow push-in so pool artwork never reads as a static wallpaper. */
.ambient-img.drift {
  animation: ambient-drift 60s ease-in-out infinite alternate;
}
@keyframes ambient-drift {
  from { transform: scale(1); }
  to { transform: scale(1.07); }
}

/* Pool mode: solid canvas at the top edge (topbar zone) and lower third
   (where rails/text live), lightest in the visual center. Derives from
   --bg-1 so every theme tints correctly for free. */
.ambient-scrim {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(to bottom,
      color-mix(in srgb, var(--bg-1) 78%, transparent) 0%,
      color-mix(in srgb, var(--bg-1) 18%, transparent) 24%,
      color-mix(in srgb, var(--bg-1) 14%, transparent) 55%,
      color-mix(in srgb, var(--bg-1) 68%, transparent) 100%),
    radial-gradient(120% 90% at 50% 10%,
      transparent 45%,
      color-mix(in srgb, var(--bg-0) 45%, transparent) 100%);
}
/* Override mode: the hero zone (top) shows the art nearly clean — the
   owning page's own fade handles its text — and the canvas builds back
   up toward the bottom where long-form content lives. */
.override-mode .ambient-scrim {
  background:
    linear-gradient(to bottom,
      color-mix(in srgb, var(--bg-1) 22%, transparent) 0%,
      color-mix(in srgb, var(--bg-1) 16%, transparent) 30%,
      color-mix(in srgb, var(--bg-1) 55%, transparent) 68%,
      color-mix(in srgb, var(--bg-1) 78%, transparent) 100%);
}

@media (prefers-reduced-motion: reduce) {
  .ambient-img { transition: none; }
}
</style>
