<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div style="height: 200px; background: var(--bg-2)" />
  </div>

  <div v-else-if="detail" class="scroll" style="height: 100%">
    <!-- Condensed hero -->
    <div class="hero-compact">
      <div class="hero-bg">
        <img v-if="backdropUrl" :src="backdropUrl" class="hero-bg-img visible" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
        <div class="hero-bg-fade" />
      </div>

      <div class="hero-content">
        <NuxtLink :to="`/tv/${slug}`" class="hero-poster-link">
          <Poster :idx="0" :src="usePosterUrl(detail.media_item.id)" :title="detail.media_item.title" aspect="2/3" />
        </NuxtLink>

        <div class="hero-info">
          <NuxtLink :to="`/tv/${slug}`" class="show-back">
            <Icon name="chevleft" :size="12" />
            {{ detail.media_item.title }}
          </NuxtLink>
          <h1 class="season-title">{{ seasonTitle }}</h1>
          <div class="hero-meta-row">
            <span v-if="season?.air_date">{{ formatYear(season.air_date) }}</span>
            <span class="dot" />
            <span>{{ episodes.length }} episode{{ episodes.length !== 1 ? 's' : '' }}</span>
          </div>
          <p v-if="season?.overview" class="season-overview">{{ season.overview }}</p>
        </div>
      </div>
    </div>

    <!-- Season navigation -->
    <div class="season-nav">
      <NuxtLink
        v-for="s in allSeasons"
        :key="s.season_number"
        :to="seasonLink(s)"
        class="season-nav-item"
        :class="{ active: s.season_number === currentSeasonNum }"
      >
        {{ s.season_number === 0 ? 'SP' : s.season_number }}
      </NuxtLink>
    </div>

    <!-- Episodes -->
    <div class="episode-body">
      <div v-for="ep in episodes" :key="ep.id" class="episode-row">
        <div class="ep-number">{{ ep.episode_number }}</div>
        <div class="ep-still">
          <img
            :src="episodeStillUrl(ep)"
            @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'"
          />
          <div class="ep-play"><Icon name="play" :size="14" /></div>
        </div>
        <div class="ep-info">
          <div class="ep-title">{{ ep.title }}</div>
          <div class="ep-meta">
            <span v-if="ep.air_date">{{ formatDate(ep.air_date) }}</span>
            <span v-if="ep.runtime_minutes" class="dot-sep">{{ ep.runtime_minutes }}m</span>
            <span v-if="ep.rating" class="dot-sep">
              <Icon name="star" :size="10" style="color: var(--gold)" /> {{ parseFloat(ep.rating).toFixed(1) }}
            </span>
          </div>
          <p v-if="ep.overview" class="ep-overview">{{ ep.overview }}</p>
        </div>
      </div>

      <div v-if="!episodes.length" style="padding: 40px 0; text-align: center; color: var(--fg-3)">
        No episodes found for this season.
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaDetail } from '~~/shared/types'

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const numParam = computed(() => route.params.num as string)

const currentSeasonNum = computed(() => {
  if (numParam.value === 'specials') return 0
  return parseInt(numParam.value) || 1
})

const detail = ref<MediaDetail | null>(null)
const loading = ref(true)

const backdropUrl = computed(() => detail.value ? useBackdropUrl(detail.value.media_item.id) : null)

const allSeasons = computed(() => {
  if (!detail.value?.seasons) return []
  return [...detail.value.seasons].sort((a: any, b: any) => a.season_number - b.season_number)
})

const season = computed(() => {
  return allSeasons.value.find((s: any) => s.season_number === currentSeasonNum.value) || null
})

const episodes = computed(() => {
  return (season.value as any)?.episodes || []
})

const seasonTitle = computed(() => {
  if (currentSeasonNum.value === 0) return 'Specials'
  return season.value?.title || `Season ${currentSeasonNum.value}`
})

function seasonLink(s: any) {
  const num = s.season_number === 0 ? 'specials' : String(s.season_number)
  return `/tv/${slug.value}/season/${num}`
}

function episodeStillUrl(ep: any) {
  if (!detail.value) return ''
  const label = `s${String(currentSeasonNum.value).padStart(2, '0')}e${String(ep.episode_number).padStart(2, '0')}`
  return `/api/media/${detail.value.media_item.id}/image/backdrop?label=${label}`
}

function formatDate(d: string) {
  if (!d) return ''
  try { return new Date(d + 'T00:00:00').toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' }) }
  catch { return d }
}

function formatYear(d: string) { return d?.slice(0, 4) || '' }

onMounted(async () => {
  try {
    detail.value = await apiFetch<MediaDetail>(`/api/media/${slug.value}`)
  } catch { navigateTo('/tv') }
  loading.value = false
})
</script>

<style scoped>
/* Condensed hero */
.hero-compact { position: relative; min-height: 200px; }
.hero-bg { position: absolute; inset: 0; overflow: hidden; }
.hero-bg-img { position: absolute; width: 100%; height: 100%; object-fit: cover; opacity: 0; transition: opacity 0.5s; }
.hero-bg-img.visible { opacity: 1; }
.hero-bg-fade {
  position: absolute; inset: 0;
  background:
    linear-gradient(to right, var(--bg-1) 0%, rgba(12,12,16,0.7) 40%, rgba(12,12,16,0.4) 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 50%);
}
.hero-content { position: relative; z-index: 1; display: flex; gap: 24px; padding: 32px 48px 20px; align-items: flex-end; }
.hero-poster-link { display: block; width: 100px; flex-shrink: 0; text-decoration: none; transition: opacity 0.15s; }
.hero-poster-link:hover { opacity: 0.8; }
.hero-info { flex: 1; min-width: 0; padding-bottom: 4px; }
.show-back {
  display: inline-flex; align-items: center; gap: 4px;
  font-size: 12px; color: var(--fg-2); text-decoration: none;
  font-family: var(--font-mono); margin-bottom: 4px;
  transition: color 0.15s;
}
.show-back:hover { color: var(--gold); }
.season-title { font-size: 28px; font-weight: 700; letter-spacing: -0.02em; margin: 0; }
.hero-meta-row { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg-2); margin-top: 6px; }
.dot { width: 3px; height: 3px; border-radius: 50%; background: var(--fg-3); }
.season-overview { font-size: 13px; color: var(--fg-2); line-height: 1.6; max-width: 600px; margin-top: 8px; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }

/* Season nav */
.season-nav {
  display: flex; gap: 2px; padding: 8px 48px 12px;
  border-bottom: 1px solid var(--border);
  overflow-x: auto; scrollbar-width: none;
}
.season-nav::-webkit-scrollbar { display: none; }
.season-nav-item {
  width: 36px; height: 36px;
  display: flex; align-items: center; justify-content: center;
  border-radius: 50%; font-size: 12px; font-weight: 600;
  font-family: var(--font-mono); color: var(--fg-2);
  text-decoration: none; transition: all 0.15s;
  flex-shrink: 0;
}
.season-nav-item:hover { background: rgba(255,255,255,0.06); color: var(--fg-0); }
.season-nav-item.active { background: var(--gold-soft); color: var(--gold); }

/* Episodes */
.episode-body { padding: 16px 48px 80px; }
.episode-row {
  display: flex; align-items: center; gap: 16px;
  padding: 14px 16px; border-radius: var(--r-md);
  transition: background 0.15s; cursor: pointer;
}
.episode-row:hover { background: rgba(255,255,255,0.03); }
.ep-number {
  width: 32px; text-align: center;
  font-size: 15px; font-weight: 700; font-family: var(--font-mono);
  color: var(--fg-3); flex-shrink: 0;
}
.ep-still {
  width: 180px; aspect-ratio: 16/9; border-radius: var(--r-sm);
  overflow: hidden; flex-shrink: 0; background: var(--bg-3);
  position: relative;
}
.ep-still img { width: 100%; height: 100%; object-fit: cover; }
.ep-still-empty { width: 100%; height: 100%; display: flex; align-items: center; justify-content: center; color: var(--fg-4); }
.ep-play {
  position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.4); opacity: 0; transition: opacity 0.15s; color: #fff;
}
.episode-row:hover .ep-play { opacity: 1; }
.ep-info { flex: 1; min-width: 0; }
.ep-title { font-size: 14px; font-weight: 500; }
.ep-meta { font-size: 11px; color: var(--fg-3); margin-top: 2px; display: flex; align-items: center; gap: 4px; }
.dot-sep::before { content: '·'; margin-right: 4px; }
.ep-overview {
  font-size: 12px; color: var(--fg-2); line-height: 1.5; margin-top: 6px;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}

@media (max-width: 900px) {
  .hero-content { padding: 24px 20px 16px; gap: 16px; }
  .hero-poster-link { width: 80px; }
  .season-title { font-size: 22px; }
  .episode-body { padding: 12px 20px 60px; }
  .season-nav { padding: 8px 20px 12px; }
  .ep-still { width: 140px; }
}
</style>
