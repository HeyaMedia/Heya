<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div style="height: 200px; background: var(--bg-2)" />
  </div>

  <div v-else-if="detail" class="scroll" style="height: 100%">
    <!-- Condensed hero -->
    <div class="hero-compact">
      <div class="hero-bg">
        <NuxtImg v-if="backdropUrl" :src="backdropUrl" :width="1920" :quality="80" class="hero-bg-img visible" @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }" />
        <div class="hero-bg-fade" />
      </div>

      <div class="hero-content">
        <NuxtLink :to="`/tv/${slug}`" class="hero-poster-link">
          <Poster :idx="0" :src="usePosterUrl(detail.media_item.id)" :title="detail.media_item.title" aspect="2/3" :width="600" />
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

          <!-- Progress bar -->
          <div v-if="episodes.length" class="season-progress">
            <div class="season-progress-fill" :style="{ width: watchedPct + '%' }" />
          </div>

          <div class="hero-actions">
            <button class="btn btn-secondary btn-sm" @click="toggleSeasonWatched">
              <Icon name="check" :size="14" />
              {{ allWatched ? 'Unmark season' : 'Mark season watched' }}
            </button>
            <button class="btn-icon" :style="{ color: seasonFavorited ? 'var(--bad)' : 'var(--fg-2)' }" @click="toggleFavorite">
              <Icon :name="seasonFavorited ? 'heartfill' : 'heart'" :size="18" />
            </button>
            <button class="btn-icon" title="Edit Metadata" @click="showMetadataEditor = true">
              <Icon name="settings" :size="16" />
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
        :class="{ active: s.season_number === currentSeasonNum, watched: isSeasonWatched(s) }"
      >
        <span class="season-nav-num">{{ s.season_number === currentSeasonNum ? (s.season_number === 0 ? 'Specials' : `Season ${s.season_number}`) : (s.season_number === 0 ? 'SP' : s.season_number) }}</span>
        <span v-if="isSeasonWatched(s)" class="season-nav-check"><Icon name="check" :size="8" /></span>
      </NuxtLink>
    </div>

    <!-- Episode cards -->
    <div class="episode-grid">
      <NuxtLink v-for="ep in episodes" :key="ep.id" :to="episodeLink(ep)" class="ep-card-link">
        <EpisodeCard
          :still-url="episodeStillUrl(ep)"
          :code="epCode(ep)"
          :title="ep.preferred_title || ep.title || `Episode ${ep.episode_number}`"
          :air-date="ep.air_date"
          :runtime-minutes="ep.runtime_minutes"
          :rating="ep.rating"
          :overview="ep.preferred_overview || ep.overview"
          :watched="isWatched(ep.id)"
          :has-file="!!episodeFileId(ep)"
          :progress-pct="episodeProgressPct(ep.id)"
          @play="playEpisode(ep)"
          @toggle-watched="toggleEpisodeWatched(ep)"
        />
      </NuxtLink>

      <div v-if="!episodes.length" style="grid-column: 1/-1; padding: 40px 0; text-align: center; color: var(--fg-3)">
        No episodes found for this season.
      </div>
    </div>

    <MetadataEditorModal
      v-if="detail && season"
      :media-id="detail.media_item.id"
      :season-id="(season as any).id"
      :show="showMetadataEditor"
      @close="showMetadataEditor = false"
    />
  </div>
</template>

<script setup lang="ts">
import type { MediaDetail } from '~~/shared/types'
import { useQuery } from '@tanstack/vue-query'

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const numParam = computed(() => route.params.num as string)

const currentSeasonNum = computed(() => {
  if (numParam.value === 'specials') return 0
  return parseInt(numParam.value) || 1
})

// Same cache key as the parent /tv/:slug page — the season view shares the
// underlying MediaDetail (series) document, so opening a season from the
// series page hits the cache instantly.
const { $heya } = useNuxtApp()
const detailQuery = useQuery({
  queryKey: ['media', 'detail', slug],
  queryFn: async () => (await $heya('/api/media/{id}', { path: { id: slug.value as never } })) as MediaDetail,
  staleTime: 1000 * 60 * 5,
  retry: false,
})
const detail = computed<MediaDetail | null>(() => detailQuery.data.value ?? null)
const loading = computed(() => detailQuery.isPending.value)
watch(detailQuery.error, (err) => { if (err) navigateTo('/tv') })
const watchedEpisodes = ref<Set<number>>(new Set())
const episodeProgress = ref<Map<number, { progress: number; total: number }>>(new Map())
const seasonFavorited = ref(false)
const showMetadataEditor = ref(false)

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

const watchedPct = computed(() => {
  if (!episodes.value.length) return 0
  return Math.round((watchedCount.value / episodes.value.length) * 100)
})

const allWatched = computed(() => episodes.value.length > 0 && watchedCount.value >= episodes.value.length)

function isWatched(epId: number) { return watchedEpisodes.value.has(epId) }

function isSeasonWatched(s: any) {
  const eps = s.episodes || []
  if (!eps.length) return false
  return eps.every((ep: any) => watchedEpisodes.value.has(ep.id))
}

async function toggleEpisodeWatched(ep: any) {
  const watched = isWatched(ep.id)
  const { $heya } = useNuxtApp()
  if (watched) {
    await $heya('/api/me/watched/episode/{id}', {
      method: 'DELETE',
      path: { id: ep.id },
    })
    watchedEpisodes.value.delete(ep.id)
  } else {
    await $heya('/api/me/watched/episode/{id}', {
      method: 'POST',
      path: { id: ep.id },
    })
    watchedEpisodes.value.add(ep.id)
  }
}

async function toggleSeasonWatched() {
  if (!season.value) return
  const s = season.value as any
  const { $heya } = useNuxtApp()
  await $heya('/api/me/watched/season/{id}', {
    method: 'POST',
    path: { id: s.id },
    body: { watched: !allWatched.value } as any,
  })
  await loadWatchState()
}

async function toggleFavorite() {
  if (!season.value) return
  const s = season.value as any
  const { $heya } = useNuxtApp()
  const res = await $heya('/api/me/favorites', {
    method: 'POST',
    body: { entity_type: 'season', entity_id: s.id } as any,
  }) as { favorited: boolean }
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
  return `/api/media/${detail.value.media_item.id}/image/still?label=${label}`
}

function episodeFileId(ep: any): number | null {
  const key = `s${currentSeasonNum.value}e${ep.episode_number}`
  return detail.value?.episode_files?.[key]?.file_id ?? null
}

function epCode(ep: any) {
  return `S${String(currentSeasonNum.value).padStart(2, '0')}E${String(ep.episode_number).padStart(2, '0')}`
}

function episodeLink(ep: any) {
  const num = currentSeasonNum.value === 0 ? 'specials' : String(currentSeasonNum.value)
  return `/tv/${slug.value}/season/${num}/episode/${ep.episode_number}`
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

// Trigger watch-state load whenever detail data arrives.
watch(detail, async (d) => {
  if (d) await loadWatchState()
}, { immediate: true })

watch(numParam, async () => {
  await loadWatchState()
  if (season.value) {
    const s = season.value as any
    const { $heya } = useNuxtApp()
    const res = await $heya('/api/me/favorites/check', {
      query: { entity_type: 'season', entity_id: s.id },
    }) as { favorited: boolean }
    seasonFavorited.value = res.favorited
  }
})
</script>

<style scoped>
/* Condensed hero */
.hero-compact { position: relative; min-height: 220px; }
.hero-bg { position: absolute; inset: 0; overflow: hidden; }
.hero-bg-img { position: absolute; width: 100%; height: 100%; object-fit: cover; opacity: 0; transition: opacity 0.5s; }
.hero-bg-img.visible { opacity: 1; }
.hero-bg-fade {
  position: absolute; inset: 0;
  background:
    linear-gradient(to right, var(--bg-1) 0%, rgba(12,12,16,0.7) 40%, rgba(12,12,16,0.4) 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 50%);
}
.hero-content { position: relative; z-index: 1; display: flex; gap: 28px; padding: 36px 48px 24px; align-items: flex-end; }
.hero-poster-link { display: block; width: 130px; flex-shrink: 0; text-decoration: none; transition: opacity 0.15s; }
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
.dot { width: 3px; height: 3px; border-radius: 50%; background: var(--fg-3); }

/* Progress bar */
.season-progress {
  width: 100%; max-width: 320px; height: 3px;
  background: rgba(255,255,255,0.08); border-radius: 2px;
  margin-top: 10px; overflow: hidden;
}
.season-progress-fill {
  height: 100%; background: var(--gold); border-radius: 2px;
  transition: width 0.4s ease;
}

.hero-actions { display: flex; align-items: center; gap: 8px; margin-top: 12px; }
.btn-sm { padding: 6px 14px; font-size: 12px; }
.season-overview { font-size: 13px; color: var(--fg-2); line-height: 1.6; max-width: 600px; margin-top: 10px; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }

/* Season nav */
.season-nav {
  display: flex; gap: 4px; padding: 10px 48px 14px;
  border-bottom: 1px solid var(--border);
  overflow-x: auto; scrollbar-width: none;
}
.season-nav::-webkit-scrollbar { display: none; }
.season-nav-item {
  position: relative;
  min-width: 36px; height: 32px; padding: 0 10px;
  display: flex; align-items: center; justify-content: center; gap: 4px;
  border-radius: 6px; font-size: 12px; font-weight: 600;
  font-family: var(--font-mono); color: var(--fg-3);
  text-decoration: none; transition: all 0.15s; flex-shrink: 0;
}
.season-nav-item:hover { background: rgba(255,255,255,0.06); color: var(--fg-0); }
.season-nav-item.active { background: var(--gold-soft); color: var(--gold); }
.season-nav-item.watched .season-nav-num { color: var(--fg-2); }

.season-nav-check {
  display: flex; align-items: center;
  color: var(--good); opacity: 0.7;
}
.season-nav-item.active .season-nav-check { opacity: 1; }

/* Episode card grid */
.episode-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 20px;
  padding: 24px 48px 80px;
}
.ep-card-link { text-decoration: none; color: inherit; display: flex; flex-direction: column; }

@media (max-width: 900px) {
  .hero-content { padding: 24px 20px 16px; gap: 16px; }
  .hero-poster-link { width: 100px; }
  .season-title { font-size: 22px; }
  .episode-grid { padding: 16px 20px 60px; grid-template-columns: 1fr; }
  .season-nav { padding: 8px 20px 12px; }
}
</style>
