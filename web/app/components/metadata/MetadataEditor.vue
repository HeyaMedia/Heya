<template>
  <div v-if="loading" class="me-loading">
    <Icon name="loading" :size="20" />
    Loading metadata...
  </div>
  <div v-else-if="!detail" class="me-empty">
    <Icon name="pencil" :size="28" />
    <span>Select a media item to edit its metadata.</span>
  </div>
  <div v-else class="me">
    <!-- Cinematic header with backdrop -->
    <div class="me-header">
      <div class="me-backdrop-wrap">
        <img v-if="headerBackdrop" :src="headerBackdrop" class="me-backdrop" @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }" />
        <div class="me-backdrop-fade" />
      </div>
      <div class="me-header-content">
        <img v-if="headerPoster" :src="headerPoster" class="me-poster" @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }" />
        <div class="me-title-block">
          <div class="me-badges">
            <span class="me-type-badge">{{ headerBadge }}</span>
            <span v-if="headerYear" class="me-year">{{ headerYear }}</span>
            <span v-if="libraryLanguage" class="me-lang-badge">
              <Icon name="translate" :size="10" /> {{ libraryLanguage.toUpperCase() }}
            </span>
          </div>
          <h2 class="me-title">{{ headerTitle }}</h2>
          <div v-if="headerOriginalTitle" class="me-localized">
            "{{ headerOriginalTitle }}"
          </div>
        </div>
        <div class="me-header-actions">
          <button v-if="mode === 'media'" class="btn btn-ghost-sm" :disabled="refreshing" @click="refreshMetadata">
            <Icon name="refresh" :size="14" />
            {{ refreshing ? 'Refreshing...' : 'Refresh' }}
          </button>
          <button v-if="mode === 'media'" class="btn btn-ghost-sm" @click="showIdentify = !showIdentify">
            <Icon name="search" :size="14" />
            Identify
          </button>
        </div>
      </div>
    </div>

    <MetadataIdentifyDialog :media-id="mediaId!" :detail="detail" :show="showIdentify" @applied="onIdentified" @close="showIdentify = false" />

    <!-- Sidebar + content layout -->
    <div class="me-layout">
      <TabsRoot :model-value="activeTab" @update:model-value="(v) => typeof v === 'string' && (activeTab = v)" orientation="vertical" as="nav" class="me-sidebar">
        <TabsList class="me-tabs-list" as="div">
          <TabsTrigger
            v-for="tab in visibleTabs"
            :key="tab.key"
            :value="tab.key"
            class="me-nav-item"
          >
            <Icon :name="tab.icon" :size="16" />
            <span>{{ tab.label }}</span>
          </TabsTrigger>
        </TabsList>
        <div class="me-nav-spacer" />
        <button class="me-save-btn" :disabled="!dirty" @click="save">
          <Icon name="check" :size="16" />
          Save Changes
        </button>
      </TabsRoot>

      <div class="me-content scroll">
        <template v-if="mode === 'media'">
          <MetadataEditorMusicGeneral
            v-if="activeTab === 'general' && isMusic"
            v-model:form="form"
            :detail="detail"
          />
          <MetadataEditorAlbums
            v-if="activeTab === 'albums' && isMusic"
            :albums="detail.albums || []"
            :artist-slug="detail.media_item.slug"
            @refresh="fetchDetail"
          />
          <MetadataEditorGeneral
            v-if="activeTab === 'general' && !isMusic"
            v-model:form="form"
            :media-type="detail.media_item.media_type"
            :detail="detail"
          />
          <MetadataEditorDetails
            v-if="activeTab === 'details'"
            v-model:form="form"
            :media-type="detail.media_item.media_type"
            :detail="detail"
          />
          <MetadataLocalizations
            v-if="activeTab === 'localizations'"
            :titles="detail.titles"
            :overviews="detail.overviews"
            :library-language="libraryLanguage"
            :primary-title="detail.media_item.title"
            :primary-overview="detail.media_item.description"
          />
          <MetadataEditorPeople
            v-if="activeTab === 'people'"
            :detail="detail"
          />
          <MetadataEditorImages
            v-if="activeTab === 'images'"
            :media-id="mediaId!"
            :detail="filteredDetailForImages"
            context="media"
            @refresh="fetchDetail"
          />
          <MetadataEditorMediaInfo
            v-if="activeTab === 'mediainfo'"
            :media-id="mediaId!"
          />
          <MetadataEditorSubtitles
            v-if="activeTab === 'subtitles'"
            :media-id="mediaId!"
            :detail="detail"
            @refresh="fetchDetail"
          />
        </template>

        <template v-if="mode === 'season'">
          <MetadataEditorSeason
            v-if="activeTab === 'general'"
            v-model:form="form"
            :season="activeSeason"
          />
          <MetadataLocalizations
            v-if="activeTab === 'localizations'"
            :titles="(activeSeason as any)?.titles || []"
            :overviews="(activeSeason as any)?.overviews || []"
            :library-language="libraryLanguage"
            :primary-title="activeSeason?.title"
            :primary-overview="activeSeason?.overview"
          />
          <MetadataEditorImages
            v-if="activeTab === 'images'"
            :media-id="mediaId!"
            :detail="filteredDetailForImages"
            context="season"
            @refresh="fetchDetail"
          />
        </template>

        <template v-if="mode === 'episode'">
          <MetadataEditorEpisode
            v-if="activeTab === 'general'"
            v-model:form="form"
            :episode="activeEpisode"
          />
          <MetadataLocalizations
            v-if="activeTab === 'localizations'"
            :titles="(activeEpisode?.episode as any)?.titles || []"
            :overviews="(activeEpisode?.episode as any)?.overviews || []"
            :library-language="libraryLanguage"
            :primary-title="activeEpisode?.episode?.title"
            :primary-overview="activeEpisode?.episode?.overview"
          />
          <MetadataEditorImages
            v-if="activeTab === 'images'"
            :media-id="mediaId!"
            :detail="filteredDetailForImages"
            context="episode"
            @refresh="fetchDetail"
          />
          <MetadataEditorMediaInfo
            v-if="activeTab === 'mediainfo'"
            :media-id="mediaId!"
            :file-id="episodeFileId"
          />
          <MetadataEditorSubtitles
            v-if="activeTab === 'subtitles'"
            :media-id="mediaId!"
            :file-id="episodeFileId"
            :detail="detail"
            @refresh="fetchDetail"
          />
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { TabsRoot, TabsList, TabsTrigger } from 'reka-ui'
import type { MediaDetail, UpdateMediaMetadataRequest } from '~~/shared/types'

const props = defineProps<{
  mediaId: number | null
  seasonId?: number | null
  episodeId?: number | null
}>()

const emit = defineEmits<{ close: [] }>()

const detail = ref<any>(null)
const library = ref<any>(null)
const loading = ref(false)
const refreshing = ref(false)
const showIdentify = ref(false)
const activeTab = ref('general')

const mode = computed<'media' | 'season' | 'episode'>(() => {
  if (props.episodeId) return 'episode'
  if (props.seasonId) return 'season'
  return 'media'
})

const libraryLanguage = computed<string>(() => library.value?.settings?.preferred_language || 'en')

const allTabs = [
  { key: 'general', label: 'General', icon: 'pencil', modes: ['media', 'season', 'episode'] },
  { key: 'details', label: 'Details', icon: 'info', modes: ['media'] },
  { key: 'localizations', label: 'Localizations', icon: 'translate', modes: ['media', 'season', 'episode'] },
  { key: 'people', label: 'Cast & Crew', icon: 'users', modes: ['media'] },
  { key: 'images', label: 'Images', icon: 'grid', modes: ['media', 'season', 'episode'] },
  { key: 'mediainfo', label: 'Media Info', icon: 'film', modes: ['media', 'episode'] },
  { key: 'subtitles', label: 'Subtitles', icon: 'subtitles', modes: ['media', 'episode'] },
]

// Music media items are artists — most movie/TV tabs don't apply, and albums
// get their own tab (album rows have no media_item of their own).
const musicTabs = [
  { key: 'general', label: 'General', icon: 'pencil' },
  { key: 'albums', label: 'Albums', icon: 'music' },
  { key: 'images', label: 'Images', icon: 'grid' },
]

const isMusic = computed(() => detail.value?.media_item?.media_type === 'music')

const visibleTabs = computed(() => {
  if (mode.value === 'media' && isMusic.value) return musicTabs
  return allTabs.filter(t => t.modes.includes(mode.value))
})

const activeSeason = computed(() => {
  if (!props.seasonId || !detail.value?.seasons) return null
  return detail.value.seasons.find((s: any) => s.id === props.seasonId)
})

const episodeFileId = computed<number | null>(() => {
  if (!activeEpisode.value || !detail.value?.episode_files) return null
  const e = activeEpisode.value
  const key = `s${e.season.season_number}e${e.episode.episode_number}`
  return detail.value.episode_files[key]?.file_id ?? null
})

const activeEpisode = computed(() => {
  if (!props.episodeId || !detail.value?.seasons) return null
  for (const s of detail.value.seasons) {
    for (const ep of s.episodes || []) {
      if (ep.id === props.episodeId) return { season: s, episode: ep }
    }
  }
  return null
})

const headerTitle = computed(() => {
  if (mode.value === 'episode' && activeEpisode.value) {
    const e = activeEpisode.value
    const epTitle = e.episode.preferred_title || e.episode.title
    return `S${e.season.season_number}E${e.episode.episode_number} — ${epTitle}`
  }
  if (mode.value === 'season' && activeSeason.value) {
    return activeSeason.value.title || `Season ${activeSeason.value.season_number}`
  }
  return detail.value?.preferred_title || detail.value?.media_item?.title || ''
})

const headerOriginalTitle = computed(() => {
  if (mode.value === 'episode' && activeEpisode.value) {
    const e = activeEpisode.value
    if (e.episode.preferred_title && e.episode.preferred_title !== e.episode.title) {
      return e.episode.title
    }
    return null
  }
  if (mode.value === 'media') {
    const pt = detail.value?.preferred_title
    const raw = detail.value?.media_item?.title
    if (pt && pt !== raw) return raw
  }
  return null
})

const headerBadge = computed(() => {
  if (mode.value === 'episode') return 'Episode'
  if (mode.value === 'season') return 'Season'
  if (isMusic.value) return 'Artist'
  return detail.value?.media_item?.media_type || ''
})

const headerYear = computed(() => {
  if (mode.value === 'episode' && activeEpisode.value) {
    return formatPgDate(activeEpisode.value.episode.air_date)
  }
  if (mode.value === 'season' && activeSeason.value) {
    return formatPgDate(activeSeason.value.air_date)
  }
  return detail.value?.media_item?.year || ''
})

const headerPoster = computed(() => {
  if (!props.mediaId) return null
  if (mode.value === 'season' && activeSeason.value) {
    return `/api/media/${props.mediaId}/image/poster?label=season-${activeSeason.value.season_number}`
  }
  return `/api/media/${props.mediaId}/image/poster`
})

const headerBackdrop = computed(() => {
  if (!props.mediaId) return null
  if (mode.value === 'episode' && activeEpisode.value) {
    const ep = activeEpisode.value
    const label = `s${String(ep.season.season_number).padStart(2, '0')}e${String(ep.episode.episode_number).padStart(2, '0')}`
    return `/api/media/${props.mediaId}/image/still?label=${label}`
  }
  return `/api/media/${props.mediaId}/image/backdrop`
})

const filteredDetailForImages = computed(() => {
  if (!detail.value) return null
  const allAssets = detail.value.assets || []
  let filtered: any[]

  if (mode.value === 'season' && activeSeason.value) {
    const prefix = `season-${activeSeason.value.season_number}`
    filtered = allAssets.filter((a: any) => a.label === prefix)
  } else if (mode.value === 'episode' && activeEpisode.value) {
    const ep = activeEpisode.value
    const label = `s${String(ep.season.season_number).padStart(2, '0')}e${String(ep.episode.episode_number).padStart(2, '0')}`
    filtered = allAssets.filter((a: any) => a.label === label)
  } else {
    filtered = allAssets.filter((a: any) => {
      if (!a.label) return true
      if (a.label === 'custom') return true
      if (/^season-\d+$/.test(a.label)) return false
      // Episode-still labels are s%02de%02d — two-OR-MORE digits per number
      // (e.g. s01e100 for episode >= 100), so match \d+ not \d{2}.
      if (/^s\d+e\d+$/.test(a.label)) return false
      return true
    })
  }

  return { ...detail.value, assets: filtered }
})

const form = ref<Record<string, any>>({})
const initialForm = ref<string>('')

const dirty = computed(() => JSON.stringify(form.value) !== initialForm.value)

function buildMediaForm(d: any) {
  const mi = d.media_item
  if (mi.media_type === 'music') {
    const artist = d.artist
    return {
      title: mi.title,
      sort_name: artist?.sort_name || '',
      disambiguation: artist?.disambiguation || '',
      biography: artist?.biography || '',
    }
  }
  const movie = d.movie
  const tv = d.tv_series
  return {
    title: mi.title,
    sort_title: mi.sort_title,
    year: mi.year,
    description: mi.description,
    original_title: movie?.original_title || tv?.original_name || '',
    original_language: movie?.original_language || tv?.original_language || '',
    tagline: movie?.tagline || '',
    runtime_minutes: movie?.runtime_minutes || 0,
    genres: [...(movie?.genres || tv?.genres || [])],
    release_date: movie?.release_date ? formatPgDate(movie.release_date) : '',
    status: tv?.status || '',
    first_air_date: tv?.first_air_date ? formatPgDate(tv.first_air_date) : '',
    last_air_date: tv?.last_air_date ? formatPgDate(tv.last_air_date) : '',
    networks: [...(tv?.networks || [])],
    original_name: tv?.original_name || '',
    external_ids: mergeExternalIDs(mi.external_ids, movie, tv),
  }
}

function buildSeasonForm(season: any) {
  return {
    title: season.title || '',
    overview: season.overview || '',
    air_date: formatPgDate(season.air_date),
  }
}

function buildEpisodeForm(ep: any) {
  return {
    title: ep.title || '',
    overview: ep.overview || '',
    air_date: formatPgDate(ep.air_date),
    runtime_minutes: ep.runtime_minutes || 0,
  }
}

function rebuildForm() {
  if (!detail.value) return
  if (mode.value === 'episode' && activeEpisode.value) {
    form.value = buildEpisodeForm(activeEpisode.value.episode)
  } else if (mode.value === 'season' && activeSeason.value) {
    form.value = buildSeasonForm(activeSeason.value)
  } else {
    form.value = buildMediaForm(detail.value)
  }
  initialForm.value = JSON.stringify(form.value)
}

function mergeExternalIDs(raw: any, movie: any, tv: any): Record<string, string> {
  const ids = parseExternalIDs(raw)
  if (movie) {
    if (movie.tmdb_id && !ids.tmdb) ids.tmdb = String(movie.tmdb_id)
    if (movie.imdb_id && !ids.imdb) ids.imdb = movie.imdb_id
  }
  if (tv) {
    if (tv.tmdb_id && !ids.tmdb) ids.tmdb = String(tv.tmdb_id)
    if (tv.imdb_id && !ids.imdb) ids.imdb = tv.imdb_id
  }
  if (!ids.tmdb) ids.tmdb = ''
  if (!ids.imdb) ids.imdb = ''
  if (!ids.tvdb) ids.tvdb = ''
  return ids
}

function parseExternalIDs(raw: any): Record<string, string> {
  if (!raw) return {}
  if (typeof raw === 'string') {
    try { return JSON.parse(raw) } catch { return {} }
  }
  return { ...raw }
}

function formatPgDate(d: any): string {
  if (!d) return ''
  if (typeof d === 'string') return d.substring(0, 10)
  if (d.Time) return new Date(d.Time).toISOString().substring(0, 10)
  return ''
}

async function fetchDetail() {
  if (!props.mediaId) return
  loading.value = true
  try {
    const { $heya } = useNuxtApp()
    // Spec types `id` as `string` because the endpoint accepts a slug OR a numeric ID.
    detail.value = await $heya('/api/media/{id}', { path: { id: String(props.mediaId) } }) as MediaDetail
    rebuildForm()
    if (detail.value?.media_item?.library_id) {
      try {
        library.value = await $heya('/api/libraries/{id}', {
          path: { id: detail.value.media_item.library_id },
        })
      } catch { library.value = null }
    }
  } catch { detail.value = null }
  loading.value = false
}

async function save() {
  if (!props.mediaId || !dirty.value) return
  const { $heya } = useNuxtApp()

  if (mode.value === 'episode' && props.episodeId) {
    try {
      await $heya('/api/media/{id}/episode/{episode_id}', {
        method: 'PUT',
        path: { id: props.mediaId, episode_id: props.episodeId },
        body: form.value as any,
      })
      await fetchDetail()
    } catch { /* empty */ }
    return
  }

  if (mode.value === 'media' && isMusic.value) {
    const body: UpdateMediaMetadataRequest = {
      title: form.value.title,
      sort_name: form.value.sort_name,
      disambiguation: form.value.disambiguation,
      biography: form.value.biography,
    }
    try {
      await $heya('/api/media/{id}/metadata', {
        method: 'PUT',
        path: { id: props.mediaId },
        body: body as any,
      })
      await fetchDetail()
    } catch { /* empty */ }
    return
  }

  if (mode.value === 'media') {
    const body: UpdateMediaMetadataRequest = {
      title: form.value.title,
      sort_title: form.value.sort_title,
      year: form.value.year,
      description: form.value.description,
      external_ids: form.value.external_ids,
      tagline: form.value.tagline,
      genres: form.value.genres,
      release_date: form.value.release_date,
      original_title: form.value.original_title,
      original_language: form.value.original_language,
      runtime_minutes: form.value.runtime_minutes,
      status: form.value.status,
      first_air_date: form.value.first_air_date,
      last_air_date: form.value.last_air_date,
      networks: form.value.networks,
      original_name: form.value.original_name,
    }
    try {
      await $heya('/api/media/{id}/metadata', {
        method: 'PUT',
        path: { id: props.mediaId },
        body: body as any,
      })
      await fetchDetail()
    } catch { /* empty */ }
  }
}

async function refreshMetadata() {
  if (!props.mediaId) return
  refreshing.value = true
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/media/{id}/refresh', { method: 'POST', path: { id: props.mediaId } })
    await new Promise(r => setTimeout(r, 2000))
    await fetchDetail()
  } catch { /* empty */ }
  refreshing.value = false
}

function onIdentified() {
  showIdentify.value = false
  fetchDetail()
}

watch(() => props.mediaId, () => {
  showIdentify.value = false
  activeTab.value = 'general'
  if (props.mediaId) fetchDetail()
  else detail.value = null
}, { immediate: true })

watch([() => props.seasonId, () => props.episodeId], () => {
  activeTab.value = 'general'
  rebuildForm()
})
</script>

<style scoped>
.me {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  flex: 1;
  min-width: 0;
}

.me-loading,
.me-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  height: 100%;
  flex: 1;
  color: var(--fg-3);
  font-size: 14px;
}

/* ── Header ── */
.me-header {
  position: relative;
  flex-shrink: 0;
  height: 140px;
  overflow: hidden;
}

.me-backdrop-wrap {
  position: absolute;
  inset: 0;
}

.me-backdrop {
  width: 100%;
  height: 100%;
  object-fit: cover;
  filter: brightness(0.4) saturate(0.7);
}

.me-backdrop-fade {
  position: absolute;
  inset: 0;
  background: linear-gradient(
    to bottom,
    rgba(19, 19, 24, 0.3) 0%,
    rgba(19, 19, 24, 0.6) 50%,
    var(--bg-2) 100%
  );
}

.me-header-content {
  position: relative;
  z-index: 1;
  display: flex;
  align-items: flex-end;
  gap: 20px;
  height: 100%;
  padding: 0 28px 20px;
}

.me-poster {
  width: 68px;
  height: 100px;
  border-radius: var(--r-sm);
  object-fit: cover;
  flex-shrink: 0;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  border: 1px solid rgba(255, 255, 255, 0.08);
}

.me-title-block {
  flex: 1;
  min-width: 0;
}

.me-badges {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.me-type-badge {
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--gold-soft);
  color: var(--gold-bright);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.me-year {
  font-size: 12px;
  color: var(--fg-2);
  font-family: var(--font-mono);
}

.me-lang-badge {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  padding: 2px 8px;
  border-radius: 4px;
  background: rgba(96, 165, 250, 0.12);
  color: rgb(96, 165, 250);
  font-size: 10px;
  font-weight: 700;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
}

.me-title {
  font-size: 22px;
  font-weight: 600;
  color: var(--fg-0);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  margin: 0;
  letter-spacing: -0.01em;
}

.me-localized {
  font-size: 13px;
  font-style: italic;
  color: var(--fg-2);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.me-header-actions {
  display: flex;
  gap: 8px;
  flex-shrink: 0;
  align-self: flex-end;
  padding-bottom: 2px;
}

/* ── Sidebar + Content ── */
.me-layout {
  display: flex;
  flex: 1;
  min-height: 0;
}

.me-sidebar {
  width: 180px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 16px 12px;
  border-right: 1px solid var(--border);
  background: var(--bg-1);
}

.me-nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  border-radius: var(--r-sm);
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-2);
  cursor: pointer;
  transition: all 0.12s;
  border: none;
  background: none;
  text-align: left;
  width: 100%;
  position: relative;
}

.me-nav-item:hover {
  background: rgba(255, 255, 255, 0.04);
  color: var(--fg-1);
}

.me-nav-item[data-state="active"] {
  background: var(--gold-soft);
  color: var(--gold-bright);
}

.me-nav-item[data-state="active"]::before {
  content: '';
  position: absolute;
  left: 0;
  top: 8px;
  bottom: 8px;
  width: 3px;
  border-radius: 2px;
  background: var(--gold);
}

.me-tabs-list { display: contents; }

.me-nav-spacer {
  flex: 1;
}

.me-save-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 10px 14px;
  border-radius: var(--r-md);
  font-size: 13px;
  font-weight: 600;
  background: var(--gold);
  color: #1a1408;
  border: none;
  cursor: pointer;
  transition: all 0.15s;
}

.me-save-btn:hover:not(:disabled) {
  background: var(--gold-bright);
}

.me-save-btn:disabled {
  opacity: 0.35;
  cursor: default;
}

/* ── Content area ── */
.me-content {
  flex: 1;
  min-width: 0;
  padding: 24px 28px;
  overflow-y: auto;
}
</style>
