<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import {
  managerMediaDetailQuery,
  managerQualityProfilesQuery,
  type ManagerAlbumView,
  type ManagerEpisodeView,
  type ManagerSeasonView,
} from '~/queries/manager'

const route = useRoute()
const { $heya } = useNuxtApp()

const libraryId = computed(() => Number(route.params.id))
const itemId = computed(() => Number(route.params.item))

// The list page stores its filtered URL here — the breadcrumb returns to the
// exact view the user left (filters, sort) rather than a reset list.
const backLink = ref(`/manager/library/${Number(route.params.id)}`)
onMounted(() => {
  const stored = sessionStorage.getItem(`heya:mgr-lib-return:${libraryId.value}`)
  if (stored) backLink.value = stored
})

const detailQuery = useQuery(() => managerMediaDetailQuery(itemId.value))
const detail = computed(() => detailQuery.data.value)
const item = computed(() => detail.value?.item)
const loading = computed(() => detailQuery.isLoading.value && !detail.value)

useLiveRefresh([
  {
    events: ['manager.changed'],
    keys: [['manager', 'media'], ['manager', 'library-items']],
    filter: event => (event.payload as { area?: string } | undefined)?.area === 'library',
  },
  {
    events: ['media.updated'],
    keys: [['manager', 'media']],
  },
])

const queryCache = useQueryCache()
function invalidate() {
  queryCache.invalidateQueries({ key: ['manager', 'media', itemId.value] })
  queryCache.invalidateQueries({ key: ['manager', 'library-items'] })
}

// ── Vocabulary ───────────────────────────────────────────────────────────

const STATUS_LABELS: Record<string, string> = {
  returning_series: 'Continuing',
  ended: 'Ended',
  canceled: 'Canceled',
  released: 'Released',
  in_production: 'In production',
  planned: 'Planned',
}
function statusLabel(value: string): string {
  if (!value) return ''
  return STATUS_LABELS[value] ?? value.replaceAll('_', ' ')
}

const isMusic = computed(() => detail.value?.library.media_type === 'music')
const posterAspect = computed(() => isMusic.value ? '1/1' : '2/3')

const publicLink = computed(() => {
  const it = item.value
  if (!it) return '#'
  switch (it.media_type) {
    case 'movie': return `/movies/${it.slug}`
    case 'tv':
    case 'anime': return `/tv/${it.slug}`
    case 'music': return `/music/artist/${it.slug}`
    case 'book': return `/books/${it.slug}`
    default: return '#'
  }
})

const yearsLine = computed(() => {
  const d = detail.value
  if (!d) return ''
  const from = (d.release_from ?? '').slice(0, 4)
  const to = (d.release_to ?? '').slice(0, 4)
  if (from && to && to !== from) return `${from}–${to}`
  if (from && (d.item.status === 'ended' || d.item.status === 'canceled')) return `${from}–${to || from}`
  if (from && d.item.status === 'returning_series') return `${from}–`
  return from || d.item.year || ''
})

const EXTERNAL_LINKS: Record<string, { label: string, url: (id: string, mediaType: string) => string }> = {
  imdb: { label: 'IMDb', url: id => `https://www.imdb.com/title/${id}/` },
  tmdb: { label: 'TMDB', url: (id, mt) => `https://www.themoviedb.org/${mt === 'movie' ? 'movie' : 'tv'}/${id}` },
  tvdb: { label: 'TVDB', url: id => `https://thetvdb.com/dereferrer/series/${id}` },
  musicbrainz: { label: 'MusicBrainz', url: id => `https://musicbrainz.org/artist/${id}` },
  anidb: { label: 'AniDB', url: id => `https://anidb.net/anime/${id}` },
  anilist: { label: 'AniList', url: id => `https://anilist.co/anime/${id}` },
  openlibrary: { label: 'Open Library', url: id => `https://openlibrary.org/works/${id}` },
}
const externalLinks = computed(() => {
  const d = detail.value
  if (!d?.external_ids) return []
  return Object.entries(d.external_ids)
    .filter(([provider]) => EXTERNAL_LINKS[provider])
    .map(([provider, id]) => ({
      label: EXTERNAL_LINKS[provider]!.label,
      href: EXTERNAL_LINKS[provider]!.url(id, d.item.media_type),
    }))
})

// ── Actions ──────────────────────────────────────────────────────────────

const busy = ref(false)
const flash = ref('')
const flashError = ref(false)
let flashTimer: ReturnType<typeof setTimeout> | undefined
function showFlash(message: string, isError = false) {
  flash.value = message
  flashError.value = isError
  clearTimeout(flashTimer)
  flashTimer = setTimeout(() => { flash.value = '' }, 3500)
}

async function toggleMonitor() {
  const it = item.value
  if (!it || busy.value) return
  busy.value = true
  const next = !it.monitored
  try {
    await $heya('/api/manager/media', { method: 'PUT', body: { ids: [it.id], monitored: next } })
    invalidate()
    showFlash(next ? 'Monitoring enabled' : 'Monitoring disabled')
  } catch (e: any) {
    showFlash(e?.data?.detail ?? 'Update failed.', true)
  } finally {
    busy.value = false
  }
}

const profilesData = useQuery(managerQualityProfilesQuery())
const domainProfiles = computed(() =>
  (profilesData.data.value ?? []).filter(p => p.domain === detail.value?.library.domain))

async function setProfile(event: Event) {
  const raw = (event.target as HTMLSelectElement).value
  if (!item.value || busy.value) return
  busy.value = true
  try {
    await $heya('/api/manager/media', {
      method: 'PUT',
      body: { ids: [item.value.id], quality_profile_id: Number(raw || 0) },
    })
    invalidate()
    showFlash(raw ? 'Profile updated' : 'Profile cleared')
  } catch (e: any) {
    showFlash(e?.data?.detail ?? 'Update failed.', true)
  } finally {
    busy.value = false
  }
}

// ── TV: seasons ──────────────────────────────────────────────────────────

const expandedSeasons = ref(new Set<number>())
watch(detail, d => {
  // Sonarr-style: latest season open, the rest collapsed. Only on first load.
  if (d?.seasons?.length && !expandedSeasons.value.size) {
    expandedSeasons.value = new Set([d.seasons[0]!.number])
  }
}, { immediate: true })
function toggleSeason(number: number) {
  const next = new Set(expandedSeasons.value)
  if (next.has(number)) next.delete(number)
  else next.add(number)
  expandedSeasons.value = next
}
function seasonTone(s: ManagerSeasonView): string {
  if (s.aired <= 0) return 'none'
  if (s.have <= 0) return 'bad'
  if (s.have < s.aired) return 'warn'
  return 'good'
}
const today = new Date().toISOString().slice(0, 10)
function episodeState(e: ManagerEpisodeView): { label: string, tone: string } {
  if (e.has_file) return { label: e.quality || 'On disk', tone: 'good' }
  if (e.special) return { label: 'Special', tone: 'none' }
  if (!e.air_date || e.air_date > today) return { label: 'Unaired', tone: 'none' }
  return { label: 'Missing', tone: 'bad' }
}

// ── Music: albums grouped by type ────────────────────────────────────────

const ALBUM_TYPE_ORDER = ['album', 'ep', 'single', 'remix', 'compilation', 'soundtrack', 'live', 'demo', 'broadcast', 'other']
const ALBUM_TYPE_LABELS: Record<string, string> = {
  album: 'Albums', ep: 'EPs', single: 'Singles', remix: 'Remixes',
  compilation: 'Compilations', soundtrack: 'Soundtracks', live: 'Live',
  demo: 'Demos', broadcast: 'Broadcasts', other: 'Other',
}
const albumGroups = computed(() => {
  const groups = new Map<string, ManagerAlbumView[]>()
  for (const al of detail.value?.albums ?? []) {
    const key = ALBUM_TYPE_ORDER.includes(al.type) ? al.type : 'other'
    if (!groups.has(key)) groups.set(key, [])
    groups.get(key)!.push(al)
  }
  return ALBUM_TYPE_ORDER
    .filter(type => groups.has(type))
    .map(type => {
      const albums = groups.get(type)!
      return {
        type,
        label: ALBUM_TYPE_LABELS[type] ?? type,
        albums,
        have: albums.reduce((n, al) => n + al.tracks_have, 0),
        total: albums.reduce((n, al) => n + al.tracks_total, 0),
        size: albums.reduce((n, al) => n + al.size_bytes, 0),
      }
    })
})
function albumTone(al: ManagerAlbumView): string {
  if (al.tracks_total <= 0) return 'none'
  if (al.tracks_have <= 0) return 'bad'
  if (al.tracks_have < al.tracks_total) return 'warn'
  return 'good'
}

const FILE_KIND_LABELS: Record<string, string> = {
  primary: 'Movie', part: 'Part', file: 'File',
}
</script>

<template>
  <div>
    <div v-if="loading" class="mgr-loading">Loading item…</div>

    <template v-else-if="detail && item">
      <NuxtLink :to="backLink" class="det-back">
        <Icon name="back" :size="13" /> {{ detail.library.name }}
      </NuxtLink>

      <!-- ── Hero ────────────────────────────────────────────────────── -->
      <div class="det-hero">
        <NuxtImg
          :src="useBackdropUrl({ id: item.id }) ?? undefined"
          class="det-hero-backdrop"
          :alt="''"
          width="1280"
          loading="lazy"
        />
        <div class="det-hero-scrim" />
        <div class="det-hero-content">
          <div class="det-poster">
            <Poster :src="usePosterUrl({ id: item.id })" :title="item.title" :aspect="posterAspect" :width="240" />
          </div>
          <div class="det-facts">
            <div class="det-title-row">
              <button
                type="button" class="det-monitor" :class="{ on: item.monitored }" :disabled="busy"
                :aria-label="item.monitored ? 'Unmonitor' : 'Monitor'"
                @click="toggleMonitor"
              >
                <Icon name="bookmark" :size="22" :weight="item.monitored ? 'fill' : 'regular'" />
              </button>
              <h1 class="det-title">{{ item.title }}</h1>
            </div>
            <div class="det-meta-line">
              <span v-if="yearsLine">{{ yearsLine }}</span>
              <span v-if="detail.runtime_minutes">{{ detail.runtime_minutes }} min</span>
              <span v-if="detail.rating">★ {{ detail.rating.toFixed(1) }}</span>
              <span v-if="detail.genres?.length">{{ detail.genres.slice(0, 4).join(', ') }}</span>
            </div>
            <div class="det-chips">
              <span v-if="detail.path" class="det-chip mono" :title="detail.path"><Icon name="folder" :size="12" /> {{ detail.path }}</span>
              <span class="det-chip"><Icon name="hard-drives" :size="12" /> {{ fmtBytes(item.size_on_disk) }} · {{ item.file_count }} files</span>
              <span v-if="detail.profile_name" class="det-chip gold"><Icon name="sliders" :size="12" /> {{ detail.profile_name }}</span>
              <span v-if="item.status" class="det-chip">{{ statusLabel(item.status) }}</span>
              <span v-if="detail.language" class="det-chip">{{ detail.language.toUpperCase() }}</span>
              <span class="det-chip" :class="item.missing_count > 0 ? 'warn' : 'good'">
                {{ item.have_count }}/{{ item.total_count }}<template v-if="item.missing_count"> · {{ item.missing_count }} missing</template>
              </span>
              <a v-for="link in externalLinks" :key="link.label" class="det-chip link" :href="link.href" target="_blank" rel="noopener">
                <Icon name="link" :size="12" /> {{ link.label }}
              </a>
            </div>
            <p v-if="detail.overview" class="det-overview">{{ detail.overview }}</p>
            <div class="det-actions">
              <select class="det-select" :value="item.quality_profile_id ?? ''" :disabled="busy" aria-label="Quality profile" @change="setProfile">
                <option value="">No profile</option>
                <option v-for="p in domainProfiles" :key="p.id" :value="p.id">{{ p.name }}</option>
              </select>
              <NuxtLink :to="publicLink" class="mgr-btn"><Icon name="play" :size="13" /> Open in Heya</NuxtLink>
            </div>
          </div>
        </div>
      </div>

      <div v-if="flash" class="mgr-flash" :class="flashError ? 'err' : 'ok'">{{ flash }}</div>

      <!-- ── TV: seasons ─────────────────────────────────────────────── -->
      <div v-if="detail.seasons?.length" class="det-sections">
        <section v-for="season in detail.seasons" :key="season.number" class="det-section">
          <button type="button" class="det-section-head" @click="toggleSeason(season.number)">
            <Icon :name="expandedSeasons.has(season.number) ? 'chevdown' : 'chevright'" :size="14" />
            <span class="det-section-title">{{ season.number === 0 ? 'Specials' : `Season ${season.number}` }}</span>
            <span class="det-badge" :class="`tone-${seasonTone(season)}`">{{ season.have }} / {{ season.aired }}</span>
            <span class="det-section-sub mono">{{ season.episodes?.length ?? 0 }} episodes · {{ fmtBytes(season.size_on_disk) }}</span>
          </button>
          <div v-if="expandedSeasons.has(season.number)" class="det-tablewrap">
            <table class="det-table">
              <thead>
                <tr><th class="num">#</th><th>Title</th><th>Air date</th><th class="num">Size</th><th class="det-right">Status</th></tr>
              </thead>
              <tbody>
                <tr v-for="episode in season.episodes ?? []" :key="episode.id">
                  <td class="num mono">{{ episode.number }}</td>
                  <td class="det-ep-title">{{ episode.title || '—' }}</td>
                  <td class="mono">{{ episode.air_date || '—' }}</td>
                  <td class="num mono">{{ episode.size_bytes ? fmtBytes(episode.size_bytes) : '' }}</td>
                  <td class="det-right"><span class="det-badge" :class="`tone-${episodeState(episode).tone}`">{{ episodeState(episode).label }}</span></td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>

      <!-- ── Movie / book: files ─────────────────────────────────────── -->
      <div v-else-if="detail.files?.length" class="det-sections">
        <section class="det-section">
          <div class="det-section-head static">
            <span class="det-section-title">Files</span>
            <span class="det-section-sub mono">{{ detail.files.length }} on disk · {{ fmtBytes(item.size_on_disk) }}</span>
          </div>
          <div class="det-tablewrap">
            <table class="det-table">
              <thead>
                <tr><th>Relative path</th><th>Type</th><th>Quality</th><th>Video</th><th>Audio</th><th class="num">Size</th><th class="num">Added</th></tr>
              </thead>
              <tbody>
                <tr v-for="file in detail.files" :key="file.id">
                  <td class="det-file-path mono" :title="file.path">{{ file.path }}</td>
                  <td><span class="det-badge tone-none">{{ FILE_KIND_LABELS[file.kind] ?? file.kind }}</span></td>
                  <td><span v-if="file.quality" class="det-badge tone-good">{{ file.quality }}</span></td>
                  <td class="mono">{{ file.video_codec || '—' }}</td>
                  <td class="mono">{{ file.audio_codec || '—' }}</td>
                  <td class="num mono">{{ fmtBytes(file.size_bytes) }}</td>
                  <td class="num mono">{{ timeAgoShort(file.added_at) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>

      <!-- ── Music: albums by type ───────────────────────────────────── -->
      <div v-else-if="albumGroups.length" class="det-sections">
        <section v-for="group in albumGroups" :key="group.type" class="det-section">
          <div class="det-section-head static">
            <span class="det-section-title">{{ group.label }}</span>
            <span class="det-badge" :class="group.have >= group.total ? 'tone-good' : 'tone-warn'">{{ group.have }} / {{ group.total }}</span>
            <span class="det-section-sub mono">{{ group.albums.length }} releases · {{ fmtBytes(group.size) }}</span>
          </div>
          <div class="det-tablewrap">
            <table class="det-table">
              <thead>
                <tr><th class="det-cover-col" /><th>Title</th><th>Released</th><th class="num">Tracks</th><th class="num">Size</th></tr>
              </thead>
              <tbody>
                <tr v-for="album in group.albums" :key="album.id">
                  <td class="det-cover-col">
                    <NuxtImg
                      :src="useAlbumCoverUrl(item.slug, album.slug) ?? undefined"
                      class="det-cover" :alt="''" width="64" loading="lazy"
                    />
                  </td>
                  <td class="det-ep-title">{{ album.title }}</td>
                  <td class="mono">{{ album.release_date || album.year || '—' }}</td>
                  <td class="num"><span class="det-badge" :class="`tone-${albumTone(album)}`">{{ album.tracks_have }} / {{ album.tracks_total }}</span></td>
                  <td class="num mono">{{ fmtBytes(album.size_bytes) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </template>
  </div>
</template>

<style scoped>
.det-back {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 12px;
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--fg-2);
  text-decoration: none;
}
.det-back:hover { color: var(--gold-bright); }

/* ── Hero ────────────────────────────────────────────────────────────── */
.det-hero {
  position: relative;
  border-radius: var(--r-lg);
  overflow: hidden;
  border: 1px solid var(--border);
  margin-bottom: 16px;
  background: var(--bg-2);
}
.det-hero-backdrop {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  filter: blur(2px) saturate(1.05);
  transform: scale(1.04);
}
.det-hero-scrim {
  position: absolute;
  inset: 0;
  /* Artwork scrim — literal by convention. */
  background: linear-gradient(90deg, rgb(0 0 0 / 0.88) 0%, rgb(0 0 0 / 0.72) 45%, rgb(0 0 0 / 0.55) 100%);
}
.det-hero-content {
  position: relative;
  display: flex;
  gap: 20px;
  padding: 20px;
}
.det-poster {
  flex: 0 0 150px;
  align-self: flex-start;
}
.det-poster :deep(.poster) { border-radius: var(--r-md); box-shadow: 0 6px 24px rgb(0 0 0 / 0.5); }
@media (max-width: 720px) {
  .det-poster { flex-basis: 100px; }
}

.det-facts { min-width: 0; flex: 1; }
.det-title-row { display: flex; align-items: center; gap: 10px; }
.det-monitor {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: none;
  border: none;
  padding: 2px;
  cursor: pointer;
  /* Sits on the artwork scrim, not a themed surface — literal like the scrim. */
  color: rgb(255 255 255 / 0.75);
}
.det-monitor:hover { color: rgb(255 255 255 / 0.95); }
.det-monitor.on { color: var(--gold-bright); }
.det-title {
  font-size: clamp(20px, 3.4vw, 30px);
  font-weight: 700;
  color: rgb(255 255 255 / 0.96);
  line-height: 1.15;
  margin: 0;
}
.det-meta-line {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
  margin-top: 6px;
  font-size: 13px;
  color: rgb(255 255 255 / 0.75);
}
.det-chips {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  margin-top: 12px;
}
.det-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 420px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding: 4px 10px;
  border-radius: 999px;
  background: rgb(0 0 0 / 0.45);
  border: 1px solid rgb(255 255 255 / 0.16);
  color: rgb(255 255 255 / 0.85);
  font-size: 11.5px;
  text-decoration: none;
}
.det-chip.mono { font-family: var(--font-mono); font-size: 10.5px; }
.det-chip.gold { color: var(--gold-bright); border-color: color-mix(in srgb, var(--gold) 50%, transparent); }
.det-chip.good { color: var(--good); border-color: color-mix(in srgb, var(--good) 45%, transparent); }
.det-chip.warn { color: var(--gold-bright); border-color: color-mix(in srgb, var(--gold) 45%, transparent); }
.det-chip.link:hover { border-color: rgb(255 255 255 / 0.45); color: rgb(255 255 255 / 1); }
.det-overview {
  margin: 12px 0 0;
  max-width: 900px;
  font-size: 13px;
  line-height: 1.55;
  color: rgb(255 255 255 / 0.82);
  display: -webkit-box;
  -webkit-line-clamp: 4;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.det-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 14px;
  flex-wrap: wrap;
}
.det-select {
  height: 32px;
  padding: 0 8px;
  border-radius: var(--r-sm);
  background: rgb(0 0 0 / 0.5);
  border: 1px solid rgb(255 255 255 / 0.2);
  color: rgb(255 255 255 / 0.9);
  font-size: 12.5px;
  cursor: pointer;
}
.det-actions .mgr-btn { background: rgb(0 0 0 / 0.45); color: rgb(255 255 255 / 0.85); }

/* ── Sections ────────────────────────────────────────────────────────── */
.det-sections { display: flex; flex-direction: column; gap: 12px; }
.det-section {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
}
.det-section-head {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 12px 14px;
  background: none;
  border: none;
  color: var(--fg-1);
  text-align: left;
  cursor: pointer;
}
.det-section-head.static { cursor: default; }
.det-section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--fg-0);
}
.det-section-sub {
  margin-left: auto;
  font-size: 11px;
  color: var(--fg-3);
}
.det-badge {
  display: inline-flex;
  align-items: center;
  padding: 2px 9px;
  border-radius: 999px;
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  border: 1px solid var(--border);
  color: var(--fg-2);
  background: rgb(var(--ink) / 0.06);
}
.det-badge.tone-good { color: var(--good); border-color: color-mix(in srgb, var(--good) 40%, transparent); background: color-mix(in srgb, var(--good) 9%, transparent); }
.det-badge.tone-warn { color: var(--gold-bright); border-color: color-mix(in srgb, var(--gold) 45%, transparent); background: var(--gold-soft); }
.det-badge.tone-bad { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 40%, transparent); background: color-mix(in srgb, var(--bad) 9%, transparent); }

.det-tablewrap { overflow-x: auto; border-top: 1px solid var(--border); }
.det-table { width: 100%; min-width: 640px; border-collapse: collapse; }
.det-table th {
  padding: 8px 12px;
  text-align: left;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--fg-3);
  white-space: nowrap;
}
.det-table th.num,
.det-table td.num { text-align: right; }
.det-table th.det-right,
.det-table td.det-right { text-align: right; }
.det-table tbody tr { border-top: 1px solid var(--border); }
.det-table tbody tr:hover { background: rgb(var(--ink) / 0.04); }
.det-table td {
  padding: 8px 12px;
  font-size: 12.5px;
  color: var(--fg-1);
  white-space: nowrap;
}
.det-table td.mono { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-2); }
.det-ep-title { max-width: 460px; overflow: hidden; text-overflow: ellipsis; color: var(--fg-0); }
.det-file-path { max-width: 480px; overflow: hidden; text-overflow: ellipsis; }
.det-cover-col { width: 48px; }
.det-cover {
  display: block;
  width: 32px;
  height: 32px;
  border-radius: 5px;
  object-fit: cover;
  background: var(--bg-3);
}
</style>
