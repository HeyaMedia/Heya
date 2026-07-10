<template>
  <div
    v-if="active"
    class="ambient-backdrop"
    :class="{ 'override-mode': !!overrideUrl, 'veil-content': !!claimedPool && !overrideUrl, reveal: ctl.reveal }"
    :style="{ '--ambient-opacity': intensity / 100 }"
    aria-hidden="true"
  >
    <NuxtImg
      v-if="srcA"
      :src="srcA"
      class="ambient-img"
      :class="{ visible: showA, drift: !reducedMotion && !overrideUrl }"
      width="1920"
      quality="70"
      alt=""
    />
    <NuxtImg
      v-if="srcB"
      :src="srcB"
      class="ambient-img"
      :class="{ visible: !showA, drift: !reducedMotion && !overrideUrl }"
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
// What it paints is decided by the useBackground claim stack (top claim
// wins — see useBackground.ts):
//   1. 'art' claim: a page that owns a specific image — detail heroes, the
//      home hero deck — pushes its current backdrop, so the hero image
//      extends full-page. The owner drives rotation.
//   2. 'pool' claim: list pages (/movies, /tv, /music, /books) claim a
//      cycling pool of their own section's artwork, drawn from
//      /api/media/ambient-backdrops and rotated every 25s. Pool pages get
//      the heavier "content veil" scrim — their text starts at the very
//      top, where home's airy scrim would leave the art nearly raw.
//   3. No claim: same pool mechanics with route-derived types (home = all
//      libraries) and the open scrim.
//
// Mounted as the first child of `.app` with z-index:-1 — .app carries no
// background (heya.css) so the layer sits between the body canvas and all
// content. Turning ambient off restores the plain-canvas look everywhere.
import { useQuery } from '@tanstack/vue-query'

const { $heya } = useNuxtApp()
const route = useRoute()
const { prefs, ambientEnabled } = useAppearance()
const { isAuthenticated } = useAuth()
const claim = useBackgroundClaim()
const overrideUrl = computed(() => (claim.value?.kind === 'art' ? claim.value.url : null))
const claimedPool = computed(() => (claim.value?.kind === 'pool' ? claim.value.types : null))
// Corner-cluster channel: the layer reports mode/rotating/cycle, the
// AmbientControls buttons request pause/shuffle/reveal.
const ctl = useBackgroundControls()

const POOL_SIZE = 30

interface Candidate {
  id: number
  public_id: string
  media_type: string
  title: string
  slug: string
  has_backdrop: boolean
}

// Pool context: an explicit pool claim wins; otherwise derive from the
// route. Sections with their own full-screen video (watch) opt out via the
// empty list.
const types = computed<string[]>(() => {
  if (claimedPool.value) return claimedPool.value
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
  // Pool only feeds pool mode; don't fetch while an art owner drives.
  enabled: computed(() => active.value && !overrideUrl.value),
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

// Publish the shown image's dominant tone so any page can paint
// artwork-adaptive controls (useBackgroundToneStyle). Guarded against
// out-of-order sampling: only the still-current image lands.
const tone = useBackgroundTone()
watch(shown, async (url) => {
  if (!url) { tone.value = null; return }
  const t = await sampleImageTone(url)
  if (shown.value === url) tone.value = t
})
watch(active, (on) => {
  if (!on) tone.value = null
})

function stop() {
  if (timer) clearTimeout(timer)
  timer = null
  ctl.value.rotating = false
}

/** Preload off-DOM, then crossfade to the url. The callback reports whether
 *  the image actually landed — a failed load keeps the previous image on
 *  screen, and identity-tracking callers must not pretend otherwise. */
function showImage(url: string, then?: (ok: boolean) => void) {
  if (shown.value === url) { then?.(true); return }
  const img = new Image()
  img.onload = () => {
    if (showA.value) srcB.value = url
    else srcA.value = url
    showA.value = !showA.value
    shown.value = url
    then?.(true)
  }
  img.onerror = () => then?.(false)
  img.src = url
}

function scheduleRotation() {
  stop()
  if (reducedMotion || overrideUrl.value || ctl.value.paused) return
  timer = setTimeout(advance, BG_ROTATE_MS)
  // A new rotation window: the corner ring re-keys off `cycle` and runs a
  // BG_ROTATE_MS animation, so ring and timer stay in lockstep.
  ctl.value.rotating = true
  ctl.value.cycle++
}

/** Show a pool candidate and publish its identity for the corner poster
 *  button — but ONLY if the image actually landed. On a failed load the
 *  previous artwork stays on screen, so the previous identity must too;
 *  the rotation callback still runs so one bad image can't stall the loop. */
function showPool(c: Candidate, then?: () => void) {
  showImage(urlFor(c), (ok) => {
    if (ok) {
      ctl.value.current = {
        title: c.title,
        slug: c.slug,
        mediaType: c.media_type,
        poster: `/api/media/${c.public_id}/image/poster`,
      }
    }
    then?.()
  })
}

function advance() {
  const pool = poolQuery.data.value
  if (!pool?.length || overrideUrl.value) return
  cursor = (cursor + 1) % pool.length
  showPool(pool[cursor]!, scheduleRotation)
}

// Source arbitration: an art claim wins; otherwise ride the pool. Reveal
// never survives a mode change — navigating from a revealed list page onto
// a detail page (which hides the corner controls) must bring the UI back.
watch(
  [overrideUrl, () => poolQuery.data.value, active],
  ([ov, pool, on]) => {
    if (!on) {
      stop()
      ctl.value.mode = 'off'
      ctl.value.reveal = false
      ctl.value.current = null
      return
    }
    if (ov) {
      stop()
      ctl.value.mode = 'art'
      // Reveal survives art mode — the eye renders there too (home hero,
      // detail pages), so the user can always find their way back.
      ctl.value.current = null
      showImage(ov)
      return
    }
    ctl.value.mode = 'pool'
    if (!pool?.length) { stop(); return }
    // (Re)enter pool mode — pick a random start if the current image
    // isn't from this pool anyway.
    cursor = Math.floor(Math.random() * pool.length)
    showPool(pool[cursor]!, scheduleRotation)
  },
  { immediate: true, deep: false },
)

// Corner-cluster requests. Pause stops the clock but keeps the image;
// resume (and shuffle) start a fresh full window — the ring restarts from
// empty rather than pretending it knows the leftover time.
watch(() => ctl.value.paused, (p) => {
  try { localStorage.setItem('heya-bg-paused', p ? '1' : '0') } catch { /* private mode */ }
  if (p) stop()
  else if (active.value && !overrideUrl.value) scheduleRotation()
})

watch(() => ctl.value.shuffleReq, () => {
  const pool = poolQuery.data.value
  if (!pool?.length || overrideUrl.value || !active.value) return
  stop()
  if (pool.length > 1) {
    let next = cursor
    while (next === cursor) next = Math.floor(Math.random() * pool.length)
    cursor = next
  }
  showPool(pool[cursor]!, scheduleRotation)
})

// Don't burn bandwidth/CPU while the tab is hidden.
function onVisibility() {
  if (document.hidden) stop()
  else if (active.value && !overrideUrl.value) scheduleRotation()
}
onMounted(() => {
  document.addEventListener('visibilitychange', onVisibility)
  // Restore the paused wish across reloads (navigation persistence comes
  // from useState itself).
  try {
    if (localStorage.getItem('heya-bg-paused') === '1') ctl.value.paused = true
  } catch { /* private mode */ }
})
onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibility)
  stop()
  // Layout swap (e.g. into settings): the corner controls unmount with us,
  // so a lingering reveal would strand a faded page with no way back.
  ctl.value.mode = 'off'
  ctl.value.reveal = false
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
  transition: opacity 2.5s ease, filter 0.8s ease;
  /* Soft-focus so content always wins; scale hides the blurred edge bleed.
     ONE blur and ONE presence formula for both modes — the list-page pools
     and the hero-driven pages must read as the same material. */
  filter: blur(9px);
  transform: scale(1.05);
}
.ambient-img.visible {
  opacity: min(calc(var(--ambient-opacity, 0.3) * 1.9), 0.9);
}
/* Owner-driven artwork switches with its hero — snappier fade only. */
.override-mode .ambient-img {
  transition: opacity 1.2s ease;
}

/* Slow push-in so pool artwork never reads as a static wallpaper. */
.ambient-img.drift {
  animation: ambient-drift 60s ease-in-out infinite alternate;
}
@keyframes ambient-drift {
  /* Stays ≥ the base 1.05 so the blur's edge bleed never shows. */
  from { transform: scale(1.05); }
  to { transform: scale(1.12); }
}

/* Pool mode: solid canvas at the top edge (topbar zone) and lower third
   (where rails/text live), lightest in the visual center. Derives from
   --bg-1 so every theme tints correctly for free. */
.ambient-scrim {
  position: absolute;
  inset: 0;
  transition: opacity 0.8s ease;
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
/* Content veil (explicit pool claims — the list pages): mirrors the
   override-mode scrim below so /movies//tv//music read exactly like home,
   with just a touch more base at the very top where FilterBar/headers sit
   (hero pages earn their clean top from the heroes' own text washes). */
.veil-content .ambient-scrim {
  background:
    linear-gradient(to bottom,
      color-mix(in srgb, var(--bg-1) 34%, transparent) 0%,
      color-mix(in srgb, var(--bg-1) 16%, transparent) 30%,
      color-mix(in srgb, var(--bg-1) 55%, transparent) 68%,
      color-mix(in srgb, var(--bg-1) 78%, transparent) 100%);
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

/* Reveal (corner eye button): the artwork clean — no blur, full presence,
   no scrim. The app content fades away via .app.bg-reveal (heya.css). */
.reveal .ambient-img { transition: opacity 0.8s ease, filter 0.8s ease; filter: none; }
.reveal .ambient-img.visible { opacity: 1; }
.reveal .ambient-scrim { opacity: 0; }

@media (prefers-reduced-motion: reduce) {
  .ambient-img { transition: none; }
}
</style>
