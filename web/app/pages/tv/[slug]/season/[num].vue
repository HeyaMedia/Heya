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
            <template v-if="watchedCount > 0">
              <span class="dot" />
              <span>{{ watchedCount }}/{{ episodes.length }} watched</span>
            </template>
          </div>
          <div class="hero-actions" style="margin-top: 12px">
            <button class="btn btn-secondary btn-sm" @click="toggleSeasonWatched">
              <Icon name="check" :size="14" />
              {{ allWatched ? 'Unmark season' : 'Mark season watched' }}
            </button>
            <button class="btn-icon" :style="{ color: seasonFavorited ? 'var(--bad)' : 'var(--fg-2)' }" @click="toggleFavorite">
              <Icon :name="seasonFavorited ? 'heartfill' : 'heart'" :size="18" />
            </button>
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

    <!-- Episode cards -->
    <div class="episode-grid">
      <div v-for="ep in episodes" :key="ep.id" class="ep-card" :class="{ watched: isWatched(ep.id), playable: !!episodeFileId(ep) }">
        <div class="ep-still" @click="playEpisode(ep)">
          <img :src="episodeStillUrl(ep)" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
          <div v-if="episodeFileId(ep)" class="ep-play-overlay">
            <div class="ep-play-btn"><Icon name="play" :size="16" /></div>
          </div>
          <div class="ep-num-badge">{{ ep.episode_number }}</div>
          <div v-if="episodeProgressPct(ep.id) > 0 && !isWatched(ep.id)" class="ep-progress-bar">
            <div class="ep-progress-fill" :style="{ width: episodeProgressPct(ep.id) + '%' }" />
          </div>
        </div>
        <div class="ep-body">
          <div class="ep-header">
            <div class="ep-title">{{ ep.title || `Episode ${ep.episode_number}` }}</div>
            <div class="ep-actions">
              <button class="ep-action-btn" :class="{ active: isWatched(ep.id) }" @click="toggleEpisodeWatched(ep)" title="Toggle watched">
                <Icon name="check" :size="12" />
              </button>
            </div>
          </div>
          <div class="ep-meta">
            <span v-if="ep.air_date">{{ formatDate(ep.air_date) }}</span>
            <span v-if="ep.runtime_minutes" class="dot-sep">{{ ep.runtime_minutes }}m</span>
            <span v-if="ep.rating" class="dot-sep"><Icon name="star" :size="10" style="color: var(--gold)" /> {{ parseFloat(ep.rating).toFixed(1) }}</span>
          </div>
          <p v-if="ep.overview" class="ep-overview">{{ ep.overview }}</p>
        </div>
      </div>

      <div v-if="!episodes.length" style="grid-column: 1/-1; padding: 40px 0; text-align: center; color: var(--fg-3)">
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
const watchedEpisodes = ref<Set<number>>(new Set())
const episodeProgress = ref<Map<number, { progress: number; total: number }>>(new Map())
const seasonFavorited = ref(false)

const backdropUrl = computed(() => detail.value ? useBackdropUrl(detail.value.media_item.id) : null)

const allSeasons = computed(() => {
  if (!detail.value?.seasons) return []
  return [...detail.value.seasons].sort((a: any, b: any) => a.season_number - b.season_number)
})

const season = computed(() => {
  return allSeasons.value.find((s: any) => s.season_number === currentSeasonNum.value) || null
})

const episodes = computed(() => {
  return ((season.value as any)?.episodes || []).sort((a: any, b: any) => a.episode_number - b.episode_number)
})

const seasonTitle = computed(() => {
  if (currentSeasonNum.value === 0) return 'Specials'
  return (season.value as any)?.title || (season.value as any)?.name || `Season ${currentSeasonNum.value}`
})

const watchedCount = computed(() => {
  let count = 0
  for (const ep of episodes.value) {
    if (watchedEpisodes.value.has(ep.id)) count++
  }
  return count
})

const allWatched = computed(() => episodes.value.length > 0 && watchedCount.value >= episodes.value.length)

function isWatched(epId: number) { return watchedEpisodes.value.has(epId) }

async function toggleEpisodeWatched(ep: any) {
  const watched = isWatched(ep.id)
  if (watched) {
    await apiFetch(`/api/episodes/${ep.id}/watched`, { method: 'DELETE' })
    watchedEpisodes.value.delete(ep.id)
  } else {
    await apiFetch(`/api/episodes/${ep.id}/watched`, { method: 'POST' })
    watchedEpisodes.value.add(ep.id)
  }
}

async function toggleSeasonWatched() {
  if (!season.value) return
  const s = season.value as any
  await apiFetch(`/api/seasons/${s.id}/watched`, { method: 'POST', body: JSON.stringify({ watched: !allWatched.value }) })
  await loadWatchState()
}

async function toggleFavorite() {
  if (!season.value) return
  const s = season.value as any
  const res = await apiFetch<{ favorited: boolean }>('/api/favorites/toggle', {
    method: 'POST',
    body: JSON.stringify({ entity_type: 'season', entity_id: s.id }),
  })
  seasonFavorited.value = res.favorited
}

function episodeProgressPct(epId: number): number {
  const p = episodeProgress.value.get(epId)
  if (!p || p.total === 0) return 0
  return Math.min(100, Math.round((p.progress / p.total) * 100))
}

async function loadWatchState() {
  if (!detail.value) return
  try {
    const st = await fetchUserState('episodes', detail.value.media_item.id)
    watchedEpisodes.value = new Set(st.watched_episode_ids || [])
    const pm = new Map<number, { progress: number; total: number }>()
    for (const ep of (st.episode_progress || [])) {
      if (!ep.completed && ep.progress_seconds > 0) {
        pm.set(ep.episode_id, { progress: ep.progress_seconds, total: ep.total_seconds })
      }
    }
    episodeProgress.value = pm
    if (season.value) {
      const s = season.value as any
      seasonFavorited.value = (st.favorited_seasons || []).includes(s.id)
    }
  } catch { /* empty */ }
}

function seasonLink(s: any) {
  const num = s.season_number === 0 ? 'specials' : String(s.season_number)
  return `/tv/${slug.value}/season/${num}`
}

function episodeStillUrl(ep: any) {
  if (!detail.value) return ''
  const label = `s${String(currentSeasonNum.value).padStart(2, '0')}e${String(ep.episode_number).padStart(2, '0')}`
  return `/api/media/${detail.value.media_item.id}/image/backdrop?label=${label}`
}

function episodeFileId(ep: any): number | null {
  const key = `s${currentSeasonNum.value}e${ep.episode_number}`
  return detail.value?.episode_files?.[key]?.file_id ?? null
}

function playEpisode(ep: any) {
  const fileId = episodeFileId(ep)
  if (!fileId || !detail.value) return
  const params = new URLSearchParams({
    media_item_id: String(detail.value.media_item.id),
    title: `${detail.value.media_item.title} - S${String(currentSeasonNum.value).padStart(2, '0')}E${String(ep.episode_number).padStart(2, '0')} - ${ep.title}`,
  })
  navigateTo(`/watch/${fileId}?${params}`)
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
    await loadWatchState()
  } catch { navigateTo('/tv') }
  loading.value = false
})

watch(numParam, async () => {
  await loadWatchState()
  if (season.value) {
    const s = season.value as any
    const res = await apiFetch<{ favorited: boolean }>(`/api/favorites/check?entity_type=season&entity_id=${s.id}`)
    seasonFavorited.value = res.favorited
  }
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
  font-family: var(--font-mono); margin-bottom: 4px; transition: color 0.15s;
}
.show-back:hover { color: var(--gold); }
.season-title { font-size: 28px; font-weight: 700; letter-spacing: -0.02em; margin: 0; }
.hero-meta-row { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg-2); margin-top: 6px; }
.hero-actions { display: flex; align-items: center; gap: 8px; }
.btn-sm { padding: 6px 14px; font-size: 12px; }
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
  text-decoration: none; transition: all 0.15s; flex-shrink: 0;
}
.season-nav-item:hover { background: rgba(255,255,255,0.06); color: var(--fg-0); }
.season-nav-item.active { background: var(--gold-soft); color: var(--gold); }

/* Episode card grid */
.episode-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 16px;
  padding: 20px 48px 80px;
}

.ep-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
  transition: border-color 0.15s, transform 0.15s;
}
.ep-card:hover { border-color: var(--border-strong); }
.ep-card.watched { opacity: 0.65; }
.ep-card.watched:hover { opacity: 1; }

.ep-still {
  position: relative;
  aspect-ratio: 16/9;
  background: var(--bg-3);
  cursor: pointer;
}
.ep-still img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ep-play-overlay {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.35);
  opacity: 0; transition: opacity 0.15s;
}
.ep-card:hover .ep-play-overlay { opacity: 1; }
.ep-play-btn {
  width: 40px; height: 40px; border-radius: 50%;
  background: rgba(255,255,255,0.15); backdrop-filter: blur(4px);
  display: flex; align-items: center; justify-content: center; color: #fff;
}
.ep-num-badge {
  position: absolute; top: 8px; left: 8px;
  min-width: 24px; height: 24px; padding: 0 6px;
  border-radius: var(--r-sm); font-size: 11px; font-weight: 700;
  font-family: var(--font-mono); background: rgba(0,0,0,0.6); color: var(--fg-0);
  display: flex; align-items: center; justify-content: center;
}
.ep-progress-bar {
  position: absolute; bottom: 0; left: 0; right: 0; height: 3px;
  background: rgba(255,255,255,0.15);
}
.ep-progress-fill {
  height: 100%; background: var(--gold); border-radius: 0 2px 2px 0;
  transition: width 0.3s ease;
}

.ep-body { padding: 12px 14px 14px; }
.ep-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 8px; }
.ep-title { font-size: 14px; font-weight: 500; line-height: 1.3; }
.ep-actions { display: flex; gap: 4px; flex-shrink: 0; }
.ep-action-btn {
  width: 24px; height: 24px; border-radius: var(--r-sm);
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-3); transition: all 0.15s;
}
.ep-action-btn:hover { background: rgba(255,255,255,0.06); color: var(--fg-0); }
.ep-action-btn.active { color: var(--good); }
.ep-meta { font-size: 11px; color: var(--fg-3); margin-top: 4px; display: flex; align-items: center; gap: 4px; }
.dot-sep::before { content: '·'; margin-right: 4px; }
.ep-overview {
  font-size: 12px; color: var(--fg-2); line-height: 1.5; margin-top: 8px;
  display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
}

@media (max-width: 900px) {
  .hero-content { padding: 24px 20px 16px; gap: 16px; }
  .hero-poster-link { width: 80px; }
  .season-title { font-size: 22px; }
  .episode-grid { padding: 12px 20px 60px; grid-template-columns: 1fr; }
  .season-nav { padding: 8px 20px 12px; }
}
</style>
