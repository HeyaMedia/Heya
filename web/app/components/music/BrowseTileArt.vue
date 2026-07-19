<!--
  BrowseTileArt — lightweight artist-image cycler for a Browse tile (moods /
  genres / tempo). Fills its positioned parent, painting a slowly-rotating
  layer of the bucket's top artists beneath the tile's existing
  gradient/scrim/label. Renders nothing for zero artists — the parent's
  gradient-only look is untouched.

  A/B crossfade shape (srcA/srcB refs, opacity transition) lifted from
  MusicCollectionHero.vue; preload-decode-then-flip and the rotation clock
  (per-instance phase offset, pause/resume starts a fresh window rather than
  a remembered remainder, viewport-gated) lifted from AmbientBackdrop.vue.
-->
<template>
  <div
    v-if="urls.length"
    ref="root"
    class="bta"
    @mouseenter="hovered = true"
    @mouseleave="hovered = false"
  >
    <NuxtImg
      v-if="srcA"
      :src="srcA"
      :alt="alt"
      class="bta-img"
      :class="{ visible: showA }"
      :width="240"
      :quality="70"
      densities="1x 2x"
      loading="lazy"
    />
    <NuxtImg
      v-if="srcB"
      :src="srcB"
      :alt="alt"
      class="bta-img"
      :class="{ visible: !showA }"
      :width="240"
      :quality="70"
      densities="1x 2x"
      loading="lazy"
    />
  </div>
</template>

<script setup lang="ts">
import type { BrowseBucketArtist } from '~/queries/music'

const props = withDefaults(defineProps<{
  artists: BrowseBucketArtist[]
  alt?: string
}>(), { alt: '' })

const CYCLE_MS = 5000

// Hoisted at setup — useImage() touches useNuxtApp() internally, which
// silently hangs when first called from a timer/async body (docs/ui.md
// gotcha #1).
const $img = useImage()
function variant(url: string) {
  return $img(url, { width: 240, quality: 70 })
}

// usePosterUrl is unconditional — bucket artists carry no path field to gate
// on, just {id, public_id}.
const urls = computed(() => props.artists
  .map(a => usePosterUrl(a))
  .filter((u): u is string => !!u))

const root = ref<HTMLElement | null>(null)
const hovered = ref(false)
const visible = ref(false)

const showA = ref(true)
const srcA = ref<string | null>(null)
const srcB = ref<string | null>(null)
const idx = ref(0)

const reducedMotion = import.meta.client
  ? window.matchMedia('(prefers-reduced-motion: reduce)').matches
  : false

// URLs whose fetch/decode failed — a layer src is only ever assigned after a
// successful preload, so a 404 poster can never paint the browser's
// broken-image glyph over the tile (bucket artists are emitted
// unconditionally; a small/new library legitimately 404s some of them).
const failed = new Set<string>()
const failedCount = ref(0)
const aliveCount = computed(() => urls.value.length - failedCount.value)

/** Preload the resized variant off-DOM and decode it before it's asked to
 *  paint, so the crossfade's first frame doesn't stall on a main-thread
 *  decode. Resolves false when the image can't be fetched. */
function preload(url: string): Promise<boolean> {
  return new Promise((resolve) => {
    if (!import.meta.client) { resolve(true); return }
    const img = new Image()
    img.decoding = 'async'
    img.onload = async () => {
      try { await img.decode() } catch { /* decodable enough to paint */ }
      resolve(true)
    }
    img.onerror = () => resolve(false)
    img.src = variant(url)
  })
}

/** First loadable URL at/after `start` (wrapping), marking dead ones so later
 *  cycles skip them without re-fetching. Null once every URL is dead. */
async function firstAlive(start: number): Promise<{ url: string; i: number } | null> {
  const list = urls.value
  for (let step = 0; step < list.length; step++) {
    const i = (start + step) % list.length
    const url = list[i]!
    if (failed.has(url)) continue
    if (await preload(url)) return { url, i }
    failed.add(url)
    failedCount.value++
  }
  return null
}

// Generation token: pool changes and unmount invalidate in-flight async work.
let gen = 0

async function showIdx(i: number) {
  const myGen = gen
  const hit = await firstAlive(i)
  if (myGen !== gen) return
  if (!hit) { srcA.value = null; srcB.value = null; return }
  idx.value = hit.i
  if (showA.value) srcB.value = hit.url
  else srcA.value = hit.url
  showA.value = !showA.value
}

let timer: ReturnType<typeof setTimeout> | null = null
let hasCycled = false
function stop() {
  if (timer) clearTimeout(timer)
  timer = null
}

// Per-instance phase offset so a grid of tiles doesn't flip in lockstep.
const phaseOffset = Math.floor(Math.random() * CYCLE_MS)

/** Pause while hovered or off-screen; reduced-motion never schedules — a
 *  static first frame only. Only the very first window (per mount/pool)
 *  uses the random phase offset; resuming after a pause (hover-out,
 *  back on screen) starts a fresh CYCLE_MS window rather than a
 *  remembered remainder — same call AmbientBackdrop makes on resume. */
function arm() {
  stop()
  if (reducedMotion || aliveCount.value < 2 || hovered.value || !visible.value) return
  const delay = hasCycled ? CYCLE_MS : phaseOffset
  timer = setTimeout(() => {
    hasCycled = true
    void showIdx((idx.value + 1) % urls.value.length).then(arm)
  }, delay)
}

// (Re)seed on pool changes: the first frame paints only once a candidate has
// actually loaded — until then the parent's gradient-only tile shows.
watch(urls, (list) => {
  gen++
  stop()
  hasCycled = false
  failed.clear()
  failedCount.value = 0
  srcA.value = null
  srcB.value = null
  showA.value = true
  if (!list.length) return
  const myGen = gen
  void (async () => {
    const hit = await firstAlive(0)
    if (myGen !== gen || !hit) return
    idx.value = hit.i
    srcA.value = hit.url
    arm()
  })()
}, { immediate: true })

watch([hovered, visible], arm)

useIntersectionObserver(root, ([entry]) => {
  visible.value = !!entry?.isIntersecting
}, { threshold: 0.1 })

onBeforeUnmount(() => {
  gen++
  stop()
})
</script>

<style scoped>
.bta {
  position: absolute;
  inset: 0;
  z-index: 0;
  overflow: hidden;
}
.bta-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  opacity: 0;
  transition: opacity 1s ease;
}
.bta-img.visible { opacity: 1; }

@media (prefers-reduced-motion: reduce) {
  .bta-img { transition: none; }
}
</style>
