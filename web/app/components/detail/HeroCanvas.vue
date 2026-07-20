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
// height); HeroCanvas measures that section and pins itself over its
// scroll-0 band (position:fixed, overflow:hidden — THE clip). Never put a
// transform on this container OR its ancestors (a transformed ancestor
// re-anchors fixed positioning and the band would scroll with the page).
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

// The claim carries the hero's rendered geometry so the blurred underlay can
// paint the SAME crop in the SAME place — behind the hero it's pixel-aligned,
// and past the hero's bottom edge the blur continues the image instead of
// re-showing a differently-cropped copy (the "same cloud twice" seam).
//
// The art layer itself is position:FIXED at the section's scroll-0 band: the
// hero art never moves — scrolling slides the whole page up OVER it. Geometry
// (band height + viewport top) is measured off the PARENT section, since this
// component is the thing being pinned.
//
// The ledger strip right below the hero is the HARD divider between sharp
// art and the blurred ambient wash. It scrolls with the content, so the
// sharp copy is clipped at exactly the ledger's current viewport position
// (clip bottom = scrollTop, since ledger top = hero bottom − scrollTop):
// above the moving line the art stays fully sharp, below it only the
// aligned blur shows, and the boundary rides the ledger as you scroll.
const rootRef = ref<HTMLElement | null>(null)

// object-position is always "center N%" in practice — extract the Y fraction
// the underlay needs; anything unparsable falls back to the 30% default.
const posY = computed(() => {
  const m = /(\d+(?:\.\d+)?)%\s*$/.exec(props.objectPosition)
  return m ? Number(m[1]) / 100 : 0.3
})

const { pinnedStyle, align } = useHeroPin(() => rootRef.value?.parentElement ?? null, () => posY.value)

const background = useBackground()
watchEffect(() => {
  if (!props.claim) return
  if (!currentSrc.value) return
  background.set(currentSrc.value, { grade: props.claimGrade, align: align.value })
})

function hideBroken(e: Event | string) {
  if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none'
}
</script>

<template>
  <div ref="rootRef" class="hero-canvas" :style="[{ '--hc-pos': objectPosition }, pinnedStyle]" aria-hidden="true">
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
  /* Floors raised (0.3→0.38 / 0.05→0.14 horizontal, 0.28→0.34 / 0.12→0.22
     vertical) when the per-text .hero-ink::before wash was removed — the
     whole hero now carries one even, seamless grade instead of a boxed
     tint behind the identity block. */
  background:
    linear-gradient(90deg, rgb(10 12 16 / 0.82), rgb(10 12 16 / 0.38) 38%, rgb(10 12 16 / 0.14) 68%),
    linear-gradient(to top, rgb(10 12 16 / 0.75) 0%, rgb(10 12 16 / 0.34) 22%, rgb(10 12 16 / 0.22) 56%, rgb(10 12 16 / 0.36) 100%);
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
