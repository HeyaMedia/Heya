<template>
  <section class="hero" v-if="items.length">
    <div class="hero-bg">
      <img
        v-if="bgA"
        :src="bgA"
        class="hero-bg-img"
        :class="{ visible: showA }"
        @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'"
      />
      <img
        v-if="bgB"
        :src="bgB"
        class="hero-bg-img"
        :class="{ visible: !showA }"
        @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'"
      />
      <div class="hero-bg-gradient" />
    </div>

    <div class="hero-inner">
      <NuxtLink :to="mediaUrl(current)" class="hero-poster">
        <Poster :idx="currentIdx" :src="posterUrl" :aspect="'2/3'" />
      </NuxtLink>

      <div class="hero-info">
        <div style="display: flex; align-items: center; gap: 12px; margin-bottom: 12px">
          <Chip gold>Featured</Chip>
          <span class="hero-counter">{{ String(currentIdx + 1).padStart(2, '0') }} / {{ String(items.length).padStart(2, '0') }}</span>
        </div>

        <NuxtLink :to="mediaUrl(current)" class="hero-title-link">
          <h1 class="hero-title">{{ current.title }}</h1>
        </NuxtLink>

        <div class="hero-meta-row" v-if="current.year || movie?.runtime_minutes || movie?.rating">
          <span v-if="current.year">{{ current.year }}</span>
          <span v-if="movie?.runtime_minutes" class="dot" />
          <span v-if="movie?.runtime_minutes">{{ Math.floor(movie.runtime_minutes / 60) }}h {{ movie.runtime_minutes % 60 }}m</span>
          <template v-if="movie?.rating">
            <span class="dot" />
            <Icon name="star" :size="14" style="color: var(--gold)" />
            <span style="color: var(--gold)">{{ parseFloat(String(movie.rating)).toFixed(1) }}</span>
          </template>
        </div>

        <p class="hero-synopsis" v-if="current.description">
          {{ current.description.slice(0, 180) }}{{ current.description.length > 180 ? '…' : '' }}
        </p>

        <div class="hero-actions">
          <button
            class="btn btn-primary"
            :disabled="!canPlayCurrent"
            @click="$emit('play', current)"
          >
            <Icon name="play" :size="16" />
            {{ playLabel }}
          </button>
          <NuxtLink :to="mediaUrl(current)" class="btn btn-ghost">
            <Icon name="info" :size="16" />
            Details
          </NuxtLink>
        </div>

        <div class="hero-dots" v-if="items.length > 1" @mouseenter="pauseHero" @mouseleave="resumeHero">
          <button
            v-for="(_, i) in items"
            :key="`hero-${i}-${currentIdx}`"
            class="hero-dot"
            :class="{ active: i === currentIdx, paused: heroPaused && i === currentIdx }"
            @click="jumpHero(i)"
          />
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import type { MediaItem, Movie } from '~~/shared/types'

// playInfo: per-item playback hint resolved by the parent. Movies populate
// fileId from detail.files[0]; TV populates fileId + label from /up-next.
// When fileId is null the Play button stays disabled — the hero shouldn't
// silently navigate to the detail page when the user explicitly asked to
// play.
export interface HeroPlayInfo {
  fileId: number | null
  label?: string
}

const props = defineProps<{
  items: MediaItem[]
  movies?: Record<number, Movie>
  playInfo?: Record<number, HeroPlayInfo>
}>()

defineEmits<{ play: [item: MediaItem] }>()

const INTERVAL = 7000
const currentIdx = ref(0)
const heroPaused = ref(false)
const showA = ref(true)
const bgA = ref<string | null>(null)
const bgB = ref<string | null>(null)

// Template only renders when items.length > 0 (`v-if` on the root section),
// so we can safely treat this as defined inside that scope.
const current = computed(() => (props.items[currentIdx.value] ?? props.items[0])!)
const movie = computed(() => props.movies?.[current.value.id])
const posterUrl = computed(() => current.value ? usePosterUrl(current.value.id) : null)

const currentPlay = computed<HeroPlayInfo | undefined>(() => props.playInfo?.[current.value.id])
const canPlayCurrent = computed(() => !!currentPlay.value?.fileId)
const playLabel = computed(() => {
  const info = currentPlay.value
  if (!info) return 'Play'
  if (info.label) return `Play ${info.label}`
  return 'Play'
})

function getBackdropUrl(idx: number) {
  const item = props.items[idx]
  return item ? useBackdropUrl(item.id) : null
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

function resumeHero() {
  heroPaused.value = false
  startTime = Date.now()
  timeout = setTimeout(() => {
    advanceHero()
    startTimer()
  }, remaining)
}

function jumpHero(idx: number) {
  if (idx === currentIdx.value) return
  if (timeout) clearTimeout(timeout)
  const url = getBackdropUrl(idx)
  if (showA.value) { bgB.value = url } else { bgA.value = url }
  showA.value = !showA.value
  currentIdx.value = idx
  if (!heroPaused.value) startTimer()
}

function initBackdrops() {
  showA.value = true
  currentIdx.value = 0
  bgA.value = getBackdropUrl(0)
  bgB.value = props.items.length > 1 ? getBackdropUrl(1) : null
}

// items arrive async from the parent — bgA stays null if we only set it in
// onMounted. Watch the first item id so we (re)initialize as soon as data lands.
watch(
  () => props.items[0]?.id,
  (id) => {
    if (!id) return
    if (timeout) { clearTimeout(timeout); timeout = null }
    initBackdrops()
    if (props.items.length > 1 && !heroPaused.value) startTimer()
  },
  { immediate: true },
)

onUnmounted(() => {
  if (timeout) clearTimeout(timeout)
})

onUnmounted(() => {
  if (timeout) clearTimeout(timeout)
})
</script>

<style scoped>
.hero {
  position: relative;
  height: 480px;
  overflow: hidden;
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
.hero-bg-gradient {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(to right, var(--bg-1) 0%, rgba(12,12,16,0.6) 50%, transparent 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 40%);
}
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
  box-shadow: 0 30px 80px rgba(0,0,0,0.7), 0 0 0 1px rgba(255,255,255,0.06);
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
.hero-info {
  display: flex;
  flex-direction: column;
  justify-content: center;
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
.hero-dots {
  display: flex;
  gap: 6px;
  margin-top: 24px;
}
.hero-dot {
  width: 32px;
  height: 3px;
  border-radius: 2px;
  background: rgba(255,255,255,0.2);
  position: relative;
  overflow: hidden;
  cursor: pointer;
  transition: background 0.15s;
}
.hero-dot:hover { background: rgba(255,255,255,0.35); }
.hero-dot.active { background: rgba(255,255,255,0.15); }
.hero-dot.active::after {
  content: '';
  position: absolute;
  left: 0; top: 0; bottom: 0;
  background: var(--gold);
  border-radius: 2px;
  animation: hero-fill 7s linear forwards;
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
}
</style>
