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
      <div class="hero-poster">
        <Poster :idx="currentIdx" :src="posterUrl" :aspect="'2/3'" />
      </div>

      <div class="hero-info">
        <div style="display: flex; align-items: center; gap: 12px; margin-bottom: 12px">
          <Chip gold>Featured</Chip>
          <span class="hero-counter">{{ String(currentIdx + 1).padStart(2, '0') }} / {{ String(items.length).padStart(2, '0') }}</span>
        </div>

        <h1 class="hero-title">{{ current.title }}</h1>

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
          <NuxtLink :to="mediaUrl(current)" class="btn btn-primary">
            <Icon name="play" :size="16" />
            Play
          </NuxtLink>
          <button class="btn btn-ghost">
            <Icon name="plus" :size="16" />
            Add to list
          </button>
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

const props = defineProps<{
  items: MediaItem[]
  movies?: Record<number, Movie>
}>()

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

onMounted(() => {
  bgA.value = getBackdropUrl(0)
  if (props.items.length > 1) {
    bgB.value = getBackdropUrl(1)
    startTimer()
  }
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
}
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
