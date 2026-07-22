<script setup lang="ts">
// MusicMixCard — square, artwork-led daily-mix tile. The host supplies a
// deterministic list of representative artist images (backdrop first, poster
// second); each one is resolved through Heya's cached, server-baked ambient
// transform before display. If every candidate is absent/broken, the same
// deterministic generated colour field as the mobile player takes over.
//
// Pure presentation, like MusicCard: the host owns the NuxtLink and context
// menu. The hover play affordance is a role=button span because nesting a real
// button inside that link would be invalid interactive markup.
const props = withDefaults(defineProps<{
  name: string
  /** Representative artist backdrop/poster URLs, ordered by preference. */
  images?: string[]
  /** Seed / representative artist names, rendered as bottom overlay chips. */
  artists?: string[]
  noPlay?: boolean
}>(), {
  images: () => [],
  artists: () => [],
  noPlay: false,
})

const emit = defineEmits<{ play: [] }>()

// Factory must be called during setup. ambientVariant() itself is safe inside
// computeds and gives us the shared 960px WebP + blur=31 cache identity.
const bgImg = useBackgroundImageTools()
const imageIndex = ref(0)
const imageSignature = computed(() => props.images.join('|'))
watch(imageSignature, () => { imageIndex.value = 0 })

const rawImage = computed(() => props.images[imageIndex.value] ?? null)
const ambientImage = computed(() => rawImage.value ? bgImg.ambientVariant(rawImage.value) : null)
function tryNextImage() {
  imageIndex.value++
}

const fallbackSeed = computed(() => [props.name, ...props.artists].join('|'))
const fallbackBackground = computed(() => musicUltrablurGradient(fallbackSeed.value))
const fallbackTitle = computed(() => {
  const hue = musicUltrablurHash(fallbackSeed.value) % 360
  return `hsl(${(hue + 180) % 360} 82% 84%)`
})

// The same complement-to-text treatment used by MusicCard and the detail
// heroes. sampleImageTone is memoized and samples a 64px derivative, so a rail
// pays once per representative artist rather than per render.
const { tintedCaptionsEnabled } = useAppearance()
const complement = ref<string | null>(null)
let toneSequence = 0
watch([rawImage, () => tintedCaptionsEnabled.value], ([src, tint]) => {
  const sequence = ++toneSequence
  complement.value = null
  if (!src || !tint || !import.meta.client) return
  sampleImageTone(src).then((tone) => {
    if (sequence !== toneSequence || !tone) return
    complement.value = `rgb(${toneTextVariant(tone.complementTriplet)})`
  })
}, { immediate: true })

const cardStyle = computed(() => ({
  background: fallbackBackground.value,
  '--mix-title-color': complement.value
    ?? (tintedCaptionsEnabled.value ? fallbackTitle.value : 'rgb(255 255 255)'),
}))
</script>

<template>
  <div class="mix-card" :style="cardStyle">
    <img
      v-if="ambientImage"
      :key="ambientImage"
      class="mix-image"
      :src="ambientImage"
      alt=""
      loading="lazy"
      decoding="async"
      draggable="false"
      @error="tryNextImage"
    >
    <div class="mix-wash" aria-hidden="true" />

    <div v-if="!noPlay" class="mix-play-wrap">
      <span
        role="button"
        tabindex="0"
        class="mix-play"
        :aria-label="`Play ${name}`"
        :title="`Play ${name}`"
        @click.stop.prevent="emit('play')"
        @keydown.enter.stop.prevent="emit('play')"
        @keydown.space.stop.prevent="emit('play')"
      >
        <Icon name="play" :size="16" />
      </span>
    </div>

    <div class="mix-title-wrap">
      <h3 class="mix-name">{{ name }}</h3>
    </div>

    <div v-if="artists.length" class="mix-artists">
      <span v-for="artist in artists" :key="artist" class="mix-artist">{{ artist }}</span>
    </div>
  </div>
</template>

<style scoped>
.mix-card {
  position: relative;
  width: 100%;
  aspect-ratio: 1 / 1;
  container-type: inline-size;
  isolation: isolate;
  border-radius: var(--r-md);
  overflow: hidden;
  border: 1px solid rgb(var(--ink) / 0.1);
  box-shadow: var(--shadow-card);
  transition: transform 0.18s ease, box-shadow 0.28s ease;
}
.mix-card:hover {
  transform: translateY(-4px);
  box-shadow: var(--shadow-card-hover);
}

/* The source bytes already contain the expensive Gaussian blur. Keep this a
   plain static image layer — no CSS filter/compositor tax while the page idles. */
.mix-image {
  position: absolute;
  inset: -4%;
  z-index: 0;
  width: 108%;
  height: 108%;
  object-fit: cover;
  transform: scale(1.03);
}
.mix-wash {
  position: absolute;
  inset: 0;
  z-index: 1;
  background:
    radial-gradient(circle at 50% 44%, rgba(0, 0, 0, 0.04), rgba(0, 0, 0, 0.28) 72%),
    linear-gradient(180deg, rgba(0, 0, 0, 0.12), rgba(0, 0, 0, 0.06) 46%, rgba(0, 0, 0, 0.68));
  pointer-events: none;
}

.mix-title-wrap {
  position: absolute;
  inset: 18px 16px 48px;
  z-index: 2;
  display: flex;
  align-items: center;
  justify-content: center;
  text-align: center;
  pointer-events: none;
}
.mix-name {
  display: -webkit-box;
  max-width: 100%;
  margin: 0;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
  font-family: var(--font-display);
  font-weight: 800;
  font-variation-settings: 'wdth' 116;
  font-size: clamp(22px, 10cqi, 32px);
  line-height: 0.98;
  letter-spacing: -0.02em;
  text-wrap: balance;
  color: var(--mix-title-color, #fff);
  text-shadow:
    0 1px 2px rgba(0, 0, 0, 0.9),
    0 0 12px rgba(0, 0, 0, 0.72),
    0 0 28px rgba(0, 0, 0, 0.58);
}

.mix-artists {
  position: absolute;
  left: 10px;
  right: 10px;
  bottom: 10px;
  z-index: 3;
  display: flex;
  min-width: 0;
  justify-content: center;
  gap: 5px;
  flex-wrap: wrap;
  overflow: hidden;
  pointer-events: none;
}
.mix-artist {
  flex: 0 1 auto;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  padding: 4px 8px;
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 999px;
  background: rgba(0, 0, 0, 0.58);
  color: rgba(255, 255, 255, 0.88);
  font: 650 clamp(8px, 3.8cqi, 10px) var(--font-mono);
  letter-spacing: 0.05em;
  text-overflow: ellipsis;
  text-transform: uppercase;
  white-space: nowrap;
}

.mix-play-wrap {
  position: absolute;
  top: 12px;
  right: 12px;
  z-index: 4;
  opacity: 0;
  transition: opacity 0.18s ease-out;
}
.mix-card:hover .mix-play-wrap,
.mix-play-wrap:has(.mix-play:focus-visible) { opacity: 1; }
.mix-play {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.62);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.38);
  transition: transform 0.15s ease-out, background 0.15s;
}
.mix-play:hover { transform: scale(1.08); background: rgba(0, 0, 0, 0.78); }

/* Touch: tapping the tile navigates; long-press opens the existing mix menu. */
@media (pointer: coarse) {
  .mix-play-wrap { display: none; }
}
</style>
