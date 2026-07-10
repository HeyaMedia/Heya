<template>
  <div
    v-if="active"
    class="ambient-backdrop"
    :style="{ '--ambient-opacity': intensity / 100 }"
    aria-hidden="true"
  >
    <NuxtImg
      v-if="srcA"
      :src="srcA"
      class="ambient-img"
      :class="{ visible: showA, drift: !reducedMotion }"
      width="1920"
      quality="70"
      alt=""
    />
    <NuxtImg
      v-if="srcB"
      :src="srcB"
      class="ambient-img"
      :class="{ visible: !showA, drift: !reducedMotion }"
      width="1920"
      quality="70"
      alt=""
    />
    <div class="ambient-scrim" />
  </div>
</template>

<script setup lang="ts">
// Full-viewport ambient background: rotates library artwork behind the app
// chrome. Route-aware — home mixes every library, /movies shows movie
// backdrops, /tv shows TV, /music artist art, /books covers.
//
// Mounted as the first child of `.app` (layouts/default.vue) with
// z-index: -1: painting order puts negative-z children above the parent's
// own background but below all in-flow sibling content, so nothing else in
// the shell needed stacking changes.
//
// The candidate pool comes from /api/media/ambient-backdrops (random,
// artwork-bearing items). Rotation preloads the next image off-DOM, then
// crossfades an A/B <NuxtImg> pair — same technique as the detail-page hero
// carousel (useBackdropCarousel), minus indicators/lightbox.
import { useQuery } from '@tanstack/vue-query'

const { $heya } = useNuxtApp()
const route = useRoute()
const { prefs, ambientEnabled } = useAppearance()
const { isAuthenticated } = useAuth()

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

// Route → media-type context. Sections with their own strong hero art
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
  enabled: active,
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
let cursor = 0
let timer: ReturnType<typeof setTimeout> | null = null

function stop() {
  if (timer) clearTimeout(timer)
  timer = null
}

function schedule() {
  stop()
  if (reducedMotion) return // static single image
  timer = setTimeout(advance, ROTATE_MS)
}

function advance() {
  const pool = poolQuery.data.value
  if (!pool?.length) return
  cursor = (cursor + 1) % pool.length
  const next = urlFor(pool[cursor]!)
  // Preload fully off-DOM so the crossfade never reveals a half-loaded image.
  const img = new Image()
  img.onload = () => {
    if (showA.value) srcB.value = next
    else srcA.value = next
    showA.value = !showA.value
    schedule()
  }
  img.onerror = () => {
    // Skip broken candidates; try the next one shortly.
    timer = setTimeout(advance, 1_000)
  }
  img.src = next
}

// (Re)start whenever the pool for the current context lands.
watch(
  () => poolQuery.data.value,
  (pool) => {
    stop()
    if (!pool?.length) return
    cursor = Math.floor(Math.random() * pool.length)
    const first = urlFor(pool[cursor]!)
    if (showA.value) srcA.value = first
    else srcB.value = first
    schedule()
  },
  { immediate: true },
)

// Don't burn bandwidth/CPU while the tab is hidden.
function onVisibility() {
  if (document.hidden) stop()
  else if (active.value) schedule()
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
  z-index: -1;
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
  opacity: var(--ambient-opacity, 0.3);
}
/* Slow push-in so the image never reads as a static wallpaper. The pair
   shares one animation; visibility does the swapping. */
.ambient-img.drift {
  animation: ambient-drift 60s ease-in-out infinite alternate;
}
@keyframes ambient-drift {
  from { transform: scale(1); }
  to { transform: scale(1.07); }
}

/* Legibility scrim: solid canvas at the top edge (topbar zone) and lower
   third (where rails/text live), lightest in the visual center. Derives
   from --bg-1 so every theme gets the right tint for free. */
.ambient-scrim {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(to bottom,
      color-mix(in srgb, var(--bg-1) 88%, transparent) 0%,
      color-mix(in srgb, var(--bg-1) 35%, transparent) 22%,
      color-mix(in srgb, var(--bg-1) 30%, transparent) 55%,
      color-mix(in srgb, var(--bg-1) 82%, transparent) 100%),
    radial-gradient(120% 90% at 50% 10%,
      transparent 40%,
      color-mix(in srgb, var(--bg-0) 55%, transparent) 100%);
}

@media (prefers-reduced-motion: reduce) {
  .ambient-img { transition: none; }
}
</style>
