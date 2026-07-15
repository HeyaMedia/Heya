<script setup lang="ts">
// Heya 2.0 hero art layer — the SHARP copy only.
//
// The blurred site-wide underlay is NOT rendered here: it's the global
// AmbientBackdrop, which this component feeds via a graded art claim
// (useBackground().set(url, { grade })). HeroCanvas paints just the crisp
// artwork inside the hero, hard-clipped at the hero's bottom edge — so the
// sharp art ends exactly at the ledger seam and everything below sits on the
// blurred ambient. Zero JS scroll math; works in every browser.
//
// Layout contract: the PAGE owns the relative hero <section> (and its
// height); HeroCanvas is the absolute inset-0 art layer with overflow:hidden
// (THE clip). Never put a transform on this container.
//
// Props are shaped for the whole redesign, not just the episode page:
// series/movie/artist heroes drive srcB + showA from useBackdropCarousel for
// an A/B crossfade; the episode/season heroes pass a single src.
const props = withDefaults(defineProps<{
  /** Current (A) image URL — raw same-origin /api URL, not a rendered variant. */
  src: string
  /** Optional B image for an A/B crossfade (carousel-driven pages). */
  srcB?: string | null
  /** Which layer is visible. false ⇒ B is on screen. */
  showA?: boolean
  /** object-position of the sharp art. */
  objectPosition?: string
  /** Publish the shown image to the global ambient layer as an art claim. */
  claim?: boolean
  /** Ambient grade to request when claiming (Heya 2.0 soft underlay). */
  claimGrade?: 'v2'
}>(), {
  srcB: null,
  showA: true,
  objectPosition: 'center 30%',
  claim: true,
  claimGrade: 'v2',
})

// The image actually on screen right now (RAW url) — that's what the blurred
// ambient underlay mirrors via the claim.
const currentSrc = computed(() => (props.showA === false ? (props.srcB || props.src) : props.src))

// Render the EXACT rendered variant the AmbientBackdrop will load (w=1920 q=70,
// resolved through the same nuxt-image provider), passed through NuxtImg
// untouched (no width/quality props → no densities srcset). Because the sharp
// hero and the blurred underlay then fetch a byte-identical URL, they share one
// browser-cache entry and paint together instead of the blur trailing behind.
// Hoisted at setup — the factory touches useImage()/useNuxtApp() (gotcha #1).
const bgImg = useBackgroundImageTools()
const displayA = computed(() => (props.src ? bgImg.variant(props.src) : ''))
const displayB = computed(() => (props.srcB ? bgImg.variant(props.srcB) : ''))

const background = useBackground()
watchEffect(() => {
  if (!props.claim) return
  if (currentSrc.value) background.set(currentSrc.value, { grade: props.claimGrade })
})

function hideBroken(e: Event | string) {
  if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none'
}
</script>

<template>
  <div class="hero-canvas" :style="{ '--hc-pos': objectPosition }" aria-hidden="true">
    <LoadingImage
      v-if="displayA"
      :src="displayA"
      class="hc-img"
      :class="{ visible: showA !== false }"
      alt=""
      @error="hideBroken"
    />
    <LoadingImage
      v-if="displayB"
      :src="displayB"
      class="hc-img"
      :class="{ visible: showA === false }"
      alt=""
      @error="hideBroken"
    />
    <!-- Readability grade — literal dark is allowed: this scrim paints
         directly over raw artwork (CLAUDE.md exception). Matches heya2.css
         .hero-art::after. -->
    <div class="hc-grade" />
    <!-- Tone leak, bottom-left — heya2.css .hero-scrim::after. -->
    <div class="hc-tone" />
  </div>
</template>

<style scoped>
.hero-canvas {
  position: absolute;
  inset: 0;
  z-index: 0;
  overflow: hidden; /* THE hard clip at the hero's bottom edge */
  pointer-events: none;
}

.hc-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: var(--hc-pos, center 30%);
  opacity: 0;
  transition: opacity 0.6s ease;
}
.hc-img.visible { opacity: 1; }

.hc-grade {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background:
    linear-gradient(90deg, rgb(10 12 16 / 0.82), rgb(10 12 16 / 0.3) 38%, rgb(10 12 16 / 0.05) 68%),
    linear-gradient(to top, rgb(10 12 16 / 0.75) 0%, rgb(10 12 16 / 0.28) 22%, rgb(10 12 16 / 0.12) 56%, rgb(10 12 16 / 0.32) 100%);
}

.hc-tone {
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: radial-gradient(90% 70% at 8% 100%, rgb(var(--tone-rgb) / 0.16), transparent 60%);
}

@media (prefers-reduced-motion: reduce) {
  .hc-img { transition: none; }
}
</style>
