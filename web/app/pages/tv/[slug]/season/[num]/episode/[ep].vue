<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div style="height: 200px; background: var(--bg-2)" />
  </div>

  <div v-else-if="detail && episode" class="scroll" style="height: 100%">
    <!-- Compact hero (matches season page) -->
    <div class="hero-compact">
      <div class="hero-bg">
        <img v-if="stillUrl" :src="stillUrl" class="hero-bg-img visible" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
        <div class="hero-bg-fade" />
      </div>

      <div class="hero-content">
        <!-- Left column: episode card + stream details -->
        <div class="hero-left">
          <EpisodeCard
            :still-url="stillUrl"
            :code="epCode"
            :title="episode.preferred_title || episode.title || `Episode ${episode.episode_number}`"
            :air-date="episode.air_date"
            :runtime-minutes="episode.runtime_minutes"
            :rating="episode.rating"
            :watched="watched"
            :has-file="!!fileId"
            @play="play"
            @toggle-watched="toggleWatched"
          />

          <MediaStreamInfo v-if="streamInfo" :stream="streamInfo" />
        </div>

        <!-- Right column: episode info + playback prefs -->
        <div class="hero-info">
          <NuxtLink :to="seasonLink" class="show-back">
            <Icon name="chevleft" :size="12" />
            {{ detail.media_item.title }} &middot; {{ seasonLabel }}
          </NuxtLink>

          <div class="ep-code">{{ epCode }}</div>
          <h1 class="ep-title">{{ episode.preferred_title || episode.title || `Episode ${episode.episode_number}` }}</h1>

          <div class="hero-meta-row">
            <span v-if="episode.air_date">{{ formatDate(episode.air_date) }}</span>
            <template v-if="episode.runtime_minutes">
              <span class="dot" />
              <span>{{ episode.runtime_minutes }}m</span>
            </template>
            <template v-if="episode.rating">
              <span class="dot" />
              <Icon name="star" :size="11" style="color: var(--gold)" />
              <span style="color: var(--gold)">{{ parseFloat(episode.rating).toFixed(1) }}</span>
              <span v-if="episode.vote_count" style="color: var(--fg-3); font-size: 11px">({{ episode.vote_count }})</span>
            </template>
          </div>

          <div class="hero-actions">
            <button v-if="fileId" class="btn btn-primary btn-sm" @click="play">
              <Icon name="play" :size="14" /> Play
            </button>
            <button v-else class="btn btn-primary btn-sm" disabled style="opacity: 0.35">
              <Icon name="play" :size="14" /> No File
            </button>
            <button class="btn-icon" :style="{ color: watched ? 'var(--good)' : 'var(--fg-2)' }" @click="toggleWatched">
              <Icon name="check" :size="18" />
            </button>
            <button class="btn-icon" title="Edit Metadata" @click="showMetadataEditor = true">
              <Icon name="settings" :size="16" />
            </button>
          </div>

          <p v-if="episode.preferred_overview || episode.overview" class="ep-overview">{{ episode.preferred_overview || episode.overview }}</p>

          <PlaybackPrefs v-if="fileId" :media-item-id="detail.media_item.id" always-open />
        </div>
      </div>
    </div>

    <!-- Episode navigation -->
    <div class="ep-nav">
      <NuxtLink v-if="prevEpisode" :to="episodeLink(prevEpisode)" class="ep-nav-link">
        <EpisodeCard
          :still-url="episodeStillUrl(prevEpisode)"
          :code="epCodeFor(prevEpisode)"
          :title="prevEpisode.preferred_title || prevEpisode.title || `Episode ${prevEpisode.episode_number}`"
          :air-date="prevEpisode.air_date"
          :runtime-minutes="prevEpisode.runtime_minutes"
          :rating="prevEpisode.rating"
          :overview="prevEpisode.preferred_overview || prevEpisode.overview"
          badge="Prev"
        />
      </NuxtLink>
      <div v-else class="ep-nav-spacer" />

      <NuxtLink v-if="nextEpisode" :to="episodeLink(nextEpisode)" class="ep-nav-link">
        <EpisodeCard
          :still-url="episodeStillUrl(nextEpisode)"
          :code="epCodeFor(nextEpisode)"
          :title="nextEpisode.preferred_title || nextEpisode.title || `Episode ${nextEpisode.episode_number}`"
          :air-date="nextEpisode.air_date"
          :runtime-minutes="nextEpisode.runtime_minutes"
          :rating="nextEpisode.rating"
          :overview="nextEpisode.preferred_overview || nextEpisode.overview"
          badge="Next"
        />
      </NuxtLink>
      <div v-else class="ep-nav-spacer" />
    </div>

    <MetadataEditorModal
      v-if="detail && episode"
      :media-id="detail.media_item.id"
      :episode-id="episode.id"
      :show="showMetadataEditor"
      @close="showMetadataEditor = false"
    />
  </div>
</template>

<script setup lang="ts">
import type { MediaDetail, StreamInfoResponse } from '~~/shared/types'

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const numParam = computed(() => route.params.num as string)
const epParam = computed(() => route.params.ep as string)

const currentSeasonNum = computed(() => {
  if (numParam.value === 'specials') return 0
  return parseInt(numParam.value) || 1
})

const currentEpNum = computed(() => parseInt(epParam.value) || 1)

const detail = ref<MediaDetail | null>(null)
const loading = ref(true)
const watchedEpisodes = ref<Set<number>>(new Set())
const streamInfo = ref<StreamInfoResponse | null>(null)
const showMetadataEditor = ref(false)

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

const episode = computed(() => {
  return episodes.value.find((e: any) => e.episode_number === currentEpNum.value) || null
})

const prevEpisode = computed(() => {
  const idx = episodes.value.findIndex((e: any) => e.episode_number === currentEpNum.value)
  return idx > 0 ? episodes.value[idx - 1] : null
})

const nextEpisode = computed(() => {
  const idx = episodes.value.findIndex((e: any) => e.episode_number === currentEpNum.value)
  return idx >= 0 && idx < episodes.value.length - 1 ? episodes.value[idx + 1] : null
})

const seasonLabel = computed(() => {
  if (currentSeasonNum.value === 0) return 'Specials'
  return (season.value as any)?.title || (season.value as any)?.name || `Season ${currentSeasonNum.value}`
})

const seasonLink = computed(() => {
  const num = currentSeasonNum.value === 0 ? 'specials' : String(currentSeasonNum.value)
  return `/tv/${slug.value}/season/${num}`
})

const fileId = computed(() => {
  const key = `s${currentSeasonNum.value}e${currentEpNum.value}`
  return detail.value?.episode_files?.[key]?.file_id ?? null
})

const stillUrl = computed(() => {
  if (!detail.value) return ''
  const label = `s${String(currentSeasonNum.value).padStart(2, '0')}e${String(currentEpNum.value).padStart(2, '0')}`
  return `/api/media/${detail.value.media_item.id}/image/backdrop?label=${label}`
})

const epCode = computed(() => {
  return `S${String(currentSeasonNum.value).padStart(2, '0')}E${String(currentEpNum.value).padStart(2, '0')}`
})

const watched = computed(() => episode.value ? watchedEpisodes.value.has(episode.value.id) : false)

function epCodeFor(ep: any) {
  return `S${String(currentSeasonNum.value).padStart(2, '0')}E${String(ep.episode_number).padStart(2, '0')}`
}

function episodeStillUrl(ep: any) {
  if (!detail.value) return ''
  const label = `s${String(currentSeasonNum.value).padStart(2, '0')}e${String(ep.episode_number).padStart(2, '0')}`
  return `/api/media/${detail.value.media_item.id}/image/backdrop?label=${label}`
}

async function toggleWatched() {
  if (!episode.value) return
  if (watched.value) {
    await apiFetch(`/api/episodes/${episode.value.id}/watched`, { method: 'DELETE' })
    watchedEpisodes.value.delete(episode.value.id)
  } else {
    await apiFetch(`/api/episodes/${episode.value.id}/watched`, { method: 'POST' })
    watchedEpisodes.value.add(episode.value.id)
  }
}

function play() {
  if (!fileId.value || !detail.value) return
  const title = `${detail.value.media_item.title} - ${epCode.value} - ${episode.value?.title || ''}`
  const params = new URLSearchParams({
    media_item_id: String(detail.value.media_item.id),
    title,
  })
  navigateTo(`/watch/${fileId.value}?${params}`)
}

function episodeLink(ep: any) {
  const num = currentSeasonNum.value === 0 ? 'specials' : String(currentSeasonNum.value)
  return `/tv/${slug.value}/season/${num}/episode/${ep.episode_number}`
}

function formatDate(d: string) {
  if (!d) return ''
  try { return new Date(d + 'T00:00:00').toLocaleDateString('en-US', { month: 'long', day: 'numeric', year: 'numeric' }) }
  catch { return d }
}

async function loadWatchState() {
  if (!detail.value) return
  try {
    const st = await fetchUserState('episodes', detail.value.media_item.id)
    watchedEpisodes.value = new Set(st.watched_episode_ids || [])
  } catch {}
}

async function loadStreamInfo() {
  if (!fileId.value) return
  try {
    const caps = useClientCaps()
    const capsQuery = capsToQueryString(caps)
    const url = `/api/stream/${fileId.value}/info${capsQuery ? `?${capsQuery}` : ''}`
    streamInfo.value = await apiFetch<StreamInfoResponse>(url)
  } catch {}
}

onMounted(async () => {
  try {
    detail.value = await apiFetch<MediaDetail>(`/api/media/${slug.value}`)
    await Promise.all([loadWatchState(), loadStreamInfo()])
  } catch { navigateTo(`/tv/${slug.value}`) }
  loading.value = false
})

watch([numParam, epParam], async () => {
  streamInfo.value = null
  await Promise.all([loadWatchState(), loadStreamInfo()])
})
</script>

<style scoped>
/* ── Compact hero (mirrors season page) ── */
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

.hero-content {
  position: relative; z-index: 1;
  display: flex; gap: 28px; padding: 32px 48px 24px;
  align-items: flex-start;
}

/* ── Left column ── */
.hero-left { width: 380px; flex-shrink: 0; }

/* ── Right column ── */
.hero-info { flex: 1; min-width: 0; padding-top: 4px; }

.show-back {
  display: inline-flex; align-items: center; gap: 4px;
  font-size: 12px; color: var(--fg-2); text-decoration: none;
  font-family: var(--font-mono); margin-bottom: 4px; transition: color 0.15s;
}
.show-back:hover { color: var(--gold); }

.ep-code {
  font-size: 11px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em; color: var(--gold);
  margin-bottom: 2px;
}
.ep-title { font-size: 28px; font-weight: 700; letter-spacing: -0.02em; margin: 0; line-height: 1.15; }

.hero-meta-row { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg-2); margin-top: 8px; }
.dot { width: 3px; height: 3px; border-radius: 50%; background: var(--fg-3); }

.hero-actions { display: flex; align-items: center; gap: 8px; margin-top: 14px; }
.btn-sm { padding: 6px 14px; font-size: 12px; }

.ep-overview {
  font-size: 14px; line-height: 1.65; color: var(--fg-1);
  margin-top: 16px;
  max-width: 600px;
}

/* ── Episode navigation ── */
.ep-nav {
  display: flex; gap: 16px;
  padding: 16px 48px 80px;
}
.ep-nav-spacer { flex: 1; }
.ep-nav-link {
  flex: 1; max-width: 280px;
  text-decoration: none; color: inherit;
}
.ep-nav-link:last-child { margin-left: auto; }

@media (max-width: 900px) {
  .hero-content { flex-direction: column; padding: 24px 20px 16px; gap: 16px; }
  .hero-left { width: 100%; }
  .ep-title { font-size: 22px; }
  .ep-nav { padding: 12px 20px 60px; flex-direction: column; }
  .ep-nav-link { max-width: 100%; }
}
</style>
