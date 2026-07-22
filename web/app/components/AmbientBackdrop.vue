<template>
  <div
    v-if="active"
    class="ambient-backdrop"
    :class="{ 'override-mode': !!overrideUrl, 'override-v2': !!overrideUrl && overrideGrade === 'v2', 'veil-content': !!claimedPool && !overrideUrl, reveal: ctl.reveal }"
    :style="{ '--ambient-opacity': intensity / 100 }"
    aria-hidden="true"
  >
    <!-- srcA/srcB already carry the resolved ?w=&q= variant URL (see
         showImage) — no width/quality props, so NuxtImg passes the src
         through untouched and the rendered file is byte-identical to the
         preloaded one. With modifier props here, NuxtImg's densities srcset
         could pick a w=3840 file the preloader never warmed.

         Each layer carries its OWN grade class (grade-pool/art/v2), stamped
         when its image lands. Grading from the container restyled BOTH
         layers the instant the winning claim changed: leaving a v2 detail
         page stripped brightness(0.4) off the still-visible art — a bright
         flash — before the incoming pool image darkened it back.

         The layer wrapper (not the img) carries blur/opacity/drift: blur on
         the wrapper composites the main img and its mirror strip into ONE
         raster before blurring, so the two can't bleed into each other at
         their join, and the oversized wrapper pushes the blur's edge fade
         off-screen (replacing the old per-image scale-up trick).

         When the winning claim publishes hero geometry (v2 detail pages),
         the main img is placed at EXACTLY the sharp hero's scale/offset —
         behind the hero it's pixel-aligned, and below the hero's bottom edge
         the blur shows the image's real continuation instead of a
         differently-cropped second copy. The mirror strip reflects the
         image's bottom edge to fill whatever viewport remains, which is
         seam-continuous by construction. -->
    <div
      v-if="srcA"
      class="ambient-layer"
      :class="[`grade-${gradeA}`, { visible: showA }]"
      @transitionend="onLayerTransitionEnd('a', $event)"
    >
      <LoadingImage :src="srcA" class="ambient-img" :style="mainStyleA" alt="" />
      <NuxtImg v-if="mirrorStyleA" :src="srcA" class="ambient-mirror" :style="mirrorStyleA" alt="" />
    </div>
    <div
      v-if="srcB"
      class="ambient-layer"
      :class="[`grade-${gradeB}`, { visible: !showA }]"
      @transitionend="onLayerTransitionEnd('b', $event)"
    >
      <LoadingImage :src="srcB" class="ambient-img" :style="mainStyleB" alt="" />
      <NuxtImg v-if="mirrorStyleB" :src="srcB" class="ambient-mirror" :style="mirrorStyleB" alt="" />
    </div>
    <!-- All three scrim looks stay mounted and opacity-crossfade on mode
         changes — background gradients can't transition, so a single
         swapped element snapped between looks a beat before the artwork
         faded. -->
    <div class="ambient-scrim scrim-open" />
    <div class="ambient-scrim scrim-veil" />
    <div class="ambient-scrim scrim-override" />
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
import { useQuery } from '@pinia/colada'
import type { ClaimAlign } from '~/composables/useBackground'

const { $heya } = useNuxtApp()
const route = useRoute()
const { prefs, ambientEnabled } = useAppearance()
const { isAuthenticated } = useAuth()
// Hoisted at setup — the factory touches useImage()/useNuxtApp(), which
// hangs when first called from timers/async bodies (docs/ui.md gotcha #1).
const bgImg = useBackgroundImageTools()
const claim = useBackgroundClaim()
const overrideUrl = computed(() => (claim.value?.kind === 'art' ? claim.value.url : null))
// Heya 2.0 art owners tag their claim with grade:'v2' — the layer then paints
// the redesign's soft grade instead of the legacy pool coat (see override-v2).
const overrideGrade = computed(() => (claim.value?.kind === 'art' ? claim.value.grade : undefined))
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
  if (p.startsWith('/tv')) return ['tv', 'anime']
  if (p.startsWith('/music')) return ['music']
  if (p.startsWith('/books')) return ['book']
  return ['movie', 'tv', 'anime', 'music', 'book']
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
  key: () => ['ambient-backdrops', typesKey.value],
  query: async () =>
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

// A/B crossfade state. srcA/srcB hold the RENDERED variant URLs; `shown`
// keeps the RAW url as identity (claims and pool candidates compare raw).
const srcA = ref<string | null>(null)
const srcB = ref<string | null>(null)
const showA = ref(true)
const shown = ref<string | null>(null)
let layerCleanupTimer: ReturnType<typeof setTimeout> | null = null

function clearOutgoingLayer() {
  if (showA.value) {
    srcB.value = null
    alignB.value = null
    natB.value = null
  } else {
    srcA.value = null
    alignA.value = null
    natA.value = null
  }
  if (layerCleanupTimer) clearTimeout(layerCleanupTimer)
  layerCleanupTimer = null
}

function scheduleLayerCleanup() {
  if (layerCleanupTimer) clearTimeout(layerCleanupTimer)
  // transitionend handles the normal path. The fallback covers reduced
  // motion, interrupted transitions, and engines that omit the event.
  layerCleanupTimer = setTimeout(clearOutgoingLayer, reducedMotion ? 0 : 2800)
}

function onLayerTransitionEnd(layer: 'a' | 'b', event: TransitionEvent) {
  if (event.propertyName !== 'opacity') return
  const outgoing = showA.value ? 'b' : 'a'
  if (layer === outgoing) clearOutgoingLayer()
}

// Hero-alignment state, stamped per layer like the grade: the claim's hero
// geometry plus the image's natural dimensions (captured at preload). An
// outgoing layer keeps its own placement while fading; null → the default
// full-viewport cover.
const claimAlign = computed<ClaimAlign | null>(() =>
  claim.value?.kind === 'art' && claim.value.grade === 'v2' ? claim.value.align ?? null : null)
const alignA = ref<ClaimAlign | null>(null)
const alignB = ref<ClaimAlign | null>(null)
const natA = ref<{ w: number; h: number } | null>(null)
const natB = ref<{ w: number; h: number } | null>(null)

// The wrapper's off-screen margin (see .ambient-layer). Keep in sync with
// the CSS --bleed value.
const BLEED = 80
const vh = ref(0)
function measureViewport() {
  vh.value = window.innerHeight
}

/** The sharp hero's vertical render geometry for this image: cover-scale
 *  within the hero box (its WIDTH, not the viewport's — pages with a side
 *  menu render the hero in the content column), focal-point offset applied.
 *  Vertical mapping is exact; horizontally the underlay stretches across the
 *  full wrapper (fill), which keeps every image ROW where the hero draws it
 *  while still washing the areas beside the hero column. */
function heroPlacement(align: ClaimAlign | null, nat: { w: number; h: number } | null) {
  if (!align || !nat || !nat.w || !nat.h || !align.heroW || !vh.value) return null
  const s = Math.max(align.heroW / nat.w, align.heroH / nat.h)
  const dispH = nat.h * s
  return {
    dispH,
    top: align.heroTop - align.posY * (dispH - align.heroH),
  }
}

/** Main img placement (wrapper coordinates = viewport + BLEED). */
function mainStyle(align: ClaimAlign | null, nat: { w: number; h: number } | null) {
  const p = heroPlacement(align, nat)
  if (!p) return undefined
  return {
    top: `${BLEED + p.top}px`,
    left: '0',
    width: '100%',
    height: `${p.dispH}px`,
    objectFit: 'fill' as const,
  }
}

/** Reflection strip below the image: its top edge shows the image's very
 *  bottom row (object-position bottom + scaleY(-1)), continuing the main
 *  copy seamlessly down to the wrapper's bottom edge. */
function mirrorStyle(align: ClaimAlign | null, nat: { w: number; h: number } | null) {
  const p = heroPlacement(align, nat)
  if (!p) return null
  const top = BLEED + p.top + p.dispH
  const height = vh.value + BLEED * 2 - top
  if (height <= 0) return null
  return {
    top: `${top}px`,
    left: '0',
    width: '100%',
    height: `${height}px`,
  }
}

const mainStyleA = computed(() => mainStyle(alignA.value, natA.value))
const mainStyleB = computed(() => mainStyle(alignB.value, natB.value))
const mirrorStyleA = computed(() => mirrorStyle(alignA.value, natA.value))
const mirrorStyleB = computed(() => mirrorStyle(alignB.value, natB.value))

// The hero republishes its claim on resize (heroH changes) without the URL
// changing — restamp the visible layer in place so placement tracks.
watch(claimAlign, (a) => {
  if (claim.value?.kind !== 'art' || claim.value.url !== shown.value) return
  ;(showA.value ? alignA : alignB).value = a
})

// Per-layer grade — the look each image was shown under. An outgoing image
// keeps its grade while fading out; only the incoming layer (or the visible
// one when the SAME image is re-claimed, e.g. list → detail sharing art)
// takes the current claim's grade.
type ImgGrade = 'pool' | 'art' | 'v2'
const gradeA = ref<ImgGrade>('pool')
const gradeB = ref<ImgGrade>('pool')
const claimGrade = computed<ImgGrade>(() =>
  overrideUrl.value ? (overrideGrade.value === 'v2' ? 'v2' : 'art') : 'pool')
let cursor = 0
let timer: ReturnType<typeof setTimeout> | null = null

// Publish the shown image's dominant tone so any page can paint
// artwork-adaptive controls (useBackgroundToneStyle). Guarded against
// out-of-order sampling: only the still-current image lands. Samples the
// w=64 thumb — a 24×24 canvas average needs kilobytes, not the multi-MB
// original (which also polluted the cache with a CORS-mode copy).
const tone = useBackgroundTone()
watch(shown, async (url) => {
  if (!url) { tone.value = null; return }
  const t = await sampleImageTone(bgImg.thumb(url))
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

/** Preload the RENDERED variant off-DOM, decode it, then crossfade. The
 *  callback reports whether the image actually landed — a failed load keeps
 *  the previous image on screen, and identity-tracking callers must not
 *  pretend otherwise.
 *
 *  Sequence-guarded: rapid navigation used to leave several loads in
 *  flight and whichever FINISHED last won, not whichever was requested
 *  last — a late stale image would land on top of the right one and the
 *  backdrop visibly jumped. Only the newest request may flip the fade;
 *  stale completions are dropped silently (no callback — their rotation
 *  window belongs to a superseded context). */
let loadSeq = 0
function showImage(url: string, then?: (ok: boolean) => void) {
  // Invalidate any in-flight load BEFORE the no-op check: "show what's
  // already shown" must still cancel a pending switch, or A → (B loading)
  // → A leaves B current and it lands late anyway.
  const seq = ++loadSeq
  if (shown.value === url) {
    // Same image, possibly a new claim grade (list → detail whose art is the
    // current pool pick): re-coat the visible layer in place — a deliberate,
    // smooth transition on one layer, not a swap. Placement restamps with it
    // (naturals for this layer are already known from its own load).
    ;(showA.value ? gradeA : gradeB).value = claimGrade.value
    ;(showA.value ? alignA : alignB).value = claimAlign.value
    then?.(true)
    return
  }
  const variant = bgImg.variant(url)
  const img = new Image()
  img.onload = async () => {
    // Decode off-screen so the crossfade's first painted frame doesn't
    // stall on a main-thread decode (the visible "stutter" at fade start).
    try { await img.decode() } catch { /* decodable enough to paint */ }
    if (seq !== loadSeq) return
    const nat = { w: img.naturalWidth, h: img.naturalHeight }
    if (showA.value) {
      srcB.value = variant
      gradeB.value = claimGrade.value
      natB.value = nat
      alignB.value = claimAlign.value
    } else {
      srcA.value = variant
      gradeA.value = claimGrade.value
      natA.value = nat
      alignA.value = claimAlign.value
    }
    showA.value = !showA.value
    shown.value = url
    scheduleLayerCleanup()
    then?.(true)
  }
  img.onerror = () => { if (seq === loadSeq) then?.(false) }
  img.src = variant
}

function scheduleRotation() {
  stop()
  if (reducedMotion || overrideUrl.value || ctl.value.paused) return
  timer = setTimeout(advance, BG_ROTATE_MS)
  // Report that an automatic switch is armed. The corner marker is static:
  // animating even a small progress ring would keep producing frames for the
  // entire otherwise-idle 30-second window.
  ctl.value.rotating = true
  warmAhead()
}

/** Warm the next couple of pool variants while this window idles, so the
 *  upcoming rotation (or a quick manual next) crossfades from a hot cache.
 *  Low-priority + idle-scheduled: never competes with page content. */
function warmAhead() {
  const pool = poolQuery.data.value
  if (!pool || pool.length < 2) return
  const kick = () => {
    for (const off of [1, 2]) {
      const c = pool[(cursor + off) % pool.length]
      if (c) bgImg.warm(urlFor(c))
    }
  }
  if ('requestIdleCallback' in window) requestIdleCallback(kick, { timeout: 4000 })
  else setTimeout(kick, 800)
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
    // (Re)enter pool mode. If what's on screen is already one of this
    // pool's images (list → detail without art → back, claim churn on
    // section navigation), KEEP it — re-anchor the cursor and just rearm
    // the clock. Random-restarting here made every back-navigation jump
    // to a new backdrop for no reason.
    const keep = shown.value ? pool.findIndex((c) => urlFor(c) === shown.value) : -1
    if (keep >= 0) {
      cursor = keep
      const c = pool[keep]!
      ctl.value.current = {
        title: c.title,
        slug: c.slug,
        mediaType: c.media_type,
        poster: `/api/media/${c.public_id}/image/poster`,
      }
      scheduleRotation()
      return
    }
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
  measureViewport()
  window.addEventListener('resize', measureViewport, { passive: true })
  // Restore the paused wish across reloads (navigation persistence comes
  // from useState itself).
  try {
    if (localStorage.getItem('heya-bg-paused') === '1') ctl.value.paused = true
  } catch { /* private mode */ }
})
onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibility)
  window.removeEventListener('resize', measureViewport)
  stop()
  if (layerCleanupTimer) clearTimeout(layerCleanupTimer)
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

/* One crossfade layer = wrapper + main img (+ optional mirror strip). The
   wrapper carries blur/opacity so its children composite into ONE
   raster before blurring (no bleed at the main/mirror join), and it hangs
   --bleed past the viewport on every side so the blur's edge fade happens
   off-screen — replacing the old per-image scale-up trick. */
.ambient-backdrop { --bleed: 80px; }
.ambient-layer {
  position: absolute;
  inset: calc(-1 * var(--bleed));
  opacity: 0;
  transition: opacity 2.5s ease, filter 0.8s ease;
  /* Soft-focus so content always wins. ONE blur and ONE presence formula for
     both modes — the list-page pools and the hero-driven pages must read as
     the same material. */
  filter: blur(9px);
}
.ambient-layer.visible {
  opacity: min(calc(var(--ambient-opacity, 0.3) * 1.9), 0.9);
}
/* Owner-driven artwork switches with its hero — snappier fade only. */
.ambient-layer.grade-art {
  transition: opacity 1.2s ease;
}

/* Heya 2.0 art owners (grade:'v2') — the redesign's soft underlay grade:
   heavier blur, dimmer, richer, and pushed to full opacity so below-hero
   content reads on a deliberate dark wash rather than the legacy ~0.5
   presence coat. Non-v2 art claims and every pool page are untouched. Kept
   ABOVE the .reveal rules so a reveal still clears the blur cleanly.

   Grades live on each layer, NOT the container: an outgoing image must fade
   out under the coat it was shown with, or leaving a v2 page brightens the
   still-visible art (brightness 0.4 → none) before the next image lands. */
.ambient-layer.grade-v2 {
  filter: blur(72px) brightness(0.4) saturate(1.2);
  /* Match HeroCanvas's fade so the blur underlay and the sharp hero art
     arrive together instead of the blur trailing a beat behind. */
  transition: opacity 0.6s ease;
}
.ambient-layer.grade-v2.visible {
  opacity: 1;
}

/* Default (no hero alignment): fill the whole oversized wrapper with a cover
   crop — the margin overpaint is what keeps the blur from vignetting at the
   screen edges. Hero-aligned layers replace this with exact inline geometry
   (see mainStyle/mirrorStyle). */
.ambient-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.ambient-mirror {
  position: absolute;
  object-fit: cover;
  /* Bottom strip of the image, flipped: the strip's top edge shows the
     image's last row — seam-continuous with the main copy above it. */
  object-position: center bottom;
  transform: scaleY(-1);
}

/* Three scrim looks, all mounted, opacity-crossfaded on mode change —
   backgrounds can't transition, so swapping one element's gradient
   snapped the veil a beat before the artwork faded. Derive from --bg-1
   so every theme tints correctly for free. */
.ambient-scrim {
  position: absolute;
  inset: 0;
  transition: opacity 0.8s ease;
  opacity: 0;
}

/* Open pool (no explicit claim — home): solid canvas at the top edge
   (topbar zone) and lower third (rails/text), lightest in the center.
   Visible by default (source order beats the base's opacity: 0); the two
   claimed modes fade it out below. Keep all these opacity rules at ≤
   (0,2,0) specificity so the trailing `.reveal .ambient-scrim` rule wins
   every tie by coming last. */
.scrim-open {
  opacity: 1;
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
.override-mode .scrim-open,
.veil-content .scrim-open { opacity: 0; }

/* Content veil (explicit pool claims — the list pages): mirrors the
   override scrim so /movies//tv//music read exactly like home, with a
   touch more base at the very top where FilterBar/headers sit. */
.scrim-veil {
  background:
    linear-gradient(to bottom,
      color-mix(in srgb, var(--bg-1) 34%, transparent) 0%,
      color-mix(in srgb, var(--bg-1) 16%, transparent) 30%,
      color-mix(in srgb, var(--bg-1) 55%, transparent) 68%,
      color-mix(in srgb, var(--bg-1) 78%, transparent) 100%);
}
.veil-content .scrim-veil { opacity: 1; }

/* Override mode: the hero zone (top) shows the art nearly clean — the
   owning page's own fade handles its text — and the canvas builds back
   up toward the bottom where long-form content lives. */
.scrim-override {
  background:
    linear-gradient(to bottom,
      color-mix(in srgb, var(--bg-1) 22%, transparent) 0%,
      color-mix(in srgb, var(--bg-1) 16%, transparent) 30%,
      color-mix(in srgb, var(--bg-1) 55%, transparent) 68%,
      color-mix(in srgb, var(--bg-1) 78%, transparent) 100%);
}
.override-mode .scrim-override { opacity: 1; }

/* Reveal (corner eye button): the artwork clean — no blur, full presence,
   no scrim. The app content fades away via .app.bg-reveal (heya.css).
   Hero-aligned placement is inline style, so the reveal geometry override
   needs !important to win — reveal shows a plain full-viewport cover crop
   and hides the mirror strip. */
.reveal .ambient-layer { transition: opacity 0.8s ease, filter 0.8s ease; filter: none; }
.reveal .ambient-layer.visible { opacity: 1; }
.reveal .ambient-img {
  top: var(--bleed) !important;
  left: var(--bleed) !important;
  width: calc(100% - var(--bleed) * 2) !important;
  height: calc(100% - var(--bleed) * 2) !important;
  object-fit: cover !important;
}
.reveal .ambient-mirror { display: none; }
.reveal .ambient-scrim { opacity: 0; }

@media (prefers-reduced-motion: reduce) {
  .ambient-layer { transition: none; }
}
</style>
