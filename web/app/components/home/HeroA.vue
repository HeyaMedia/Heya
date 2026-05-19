<template>
  <section class="hero" v-if="items.length">
    <div class="hero-bg">
      <img
        v-if="backdropUrl"
        :src="backdropUrl"
        class="hero-bg-img"
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

        <div class="hero-dots" v-if="items.length > 1">
          <button
            v-for="(_, i) in items"
            :key="i"
            class="hero-dot"
            :class="{ active: i === currentIdx }"
            @click="currentIdx = i"
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

const currentIdx = ref(0)

const current = computed(() => props.items[currentIdx.value] || props.items[0])
const movie = computed(() => props.movies?.[current.value?.id])
const posterUrl = computed(() => current.value ? usePosterUrl(current.value.id) : null)
const backdropUrl = computed(() => current.value ? useBackdropUrl(current.value.id) : null)

let interval: ReturnType<typeof setInterval> | null = null

onMounted(() => {
  if (props.items.length > 1) {
    interval = setInterval(() => {
      currentIdx.value = (currentIdx.value + 1) % props.items.length
    }, 7000)
  }
})

onUnmounted(() => {
  if (interval) clearInterval(interval)
})

watch(currentIdx, () => {
  if (interval) clearInterval(interval)
  if (props.items.length > 1) {
    interval = setInterval(() => {
      currentIdx.value = (currentIdx.value + 1) % props.items.length
    }, 7000)
  }
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
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: opacity 0.6s ease;
}
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
  gap: 8px;
  margin-top: 24px;
}
.hero-dot {
  width: 24px;
  height: 3px;
  border-radius: 2px;
  background: var(--fg-4);
  transition: width 0.3s ease, background 0.3s ease;
}
.hero-dot.active {
  width: 44px;
  background: var(--gold);
}
@media (max-width: 900px) {
  .hero-inner { grid-template-columns: 1fr; gap: 24px; }
  .hero-poster { display: none; }
  .hero-title { font-size: 36px; }
}
</style>
