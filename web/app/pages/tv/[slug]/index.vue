<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div style="height: 380px; background: var(--bg-2)" />
  </div>

  <div v-else-if="detail" class="scroll" style="height: 100%">
    <!-- Hero with crossfade backdrops -->
    <div class="hero-section">
      <div class="hero-bg">
        <NuxtImg v-if="backdropA" :src="backdropA" :width="1920" :quality="80" class="hero-bg-img" :class="{ visible: showA }" />
        <NuxtImg v-if="backdropB" :src="backdropB" :width="1920" :quality="80" class="hero-bg-img" :class="{ visible: !showA }" />
        <div class="hero-bg-fade" />
      </div>

      <div class="hero-content">
        <div class="hero-poster">
          <Poster :idx="0" :src="usePosterUrl(detail.media_item.id)" :title="detail.media_item.title" aspect="2/3" :width="600" />
          <button class="zoom-btn" @click="openPosterLightbox"><Icon name="expand" :size="14" /></button>
        </div>

        <div class="hero-info">
          <div class="detail-badges">
            <Chip gold>TV Show</Chip>
            <Chip v-if="certification">{{ certification }}</Chip>
            <Chip v-if="detail.media_item.year">{{ detail.media_item.year }}</Chip>
            <Chip v-if="detail.tv_series?.status">{{ detail.tv_series.status }}</Chip>
          </div>

          <h1 class="detail-title">{{ detail.preferred_title || detail.media_item.title }}</h1>

          <div class="hero-meta-row" v-if="rating">
            <Icon name="star" :size="14" style="color: var(--gold)" />
            <span style="color: var(--gold)">{{ rating }}/10</span>
            <span class="dot" />
            <span>{{ presentSeasonCount }} season{{ presentSeasonCount !== 1 ? 's' : '' }}</span>
            <span class="dot" />
            <span>{{ presentEpisodeTotal }} episode{{ presentEpisodeTotal !== 1 ? 's' : '' }}</span>
          </div>

          <div v-if="genres.length" style="display: flex; gap: 6px; flex-wrap: wrap; margin: 12px 0">
            <NuxtLink v-for="g in genres" :key="g" :to="`/genre/${encodeURIComponent(g)}`"><Chip>{{ g }}</Chip></NuxtLink>
          </div>

          <div class="detail-actions">
            <button v-if="firstEpisodeFileId" class="btn btn-primary" @click="playFirstEpisode">
              <Icon name="play" :size="16" /> {{ episodeInProgress ? 'Resume' : 'Play' }} {{ nextEpisodeFull }}
            </button>
            <button v-else class="btn btn-primary" disabled style="opacity: 0.4"><Icon name="play" :size="16" /> No Files</button>
            <button class="btn btn-secondary" @click="showListModal = true"><Icon name="plus" :size="16" /> My List</button>
            <button class="btn-icon" :style="{ color: isFavorited ? 'var(--bad)' : 'var(--fg-1)' }" @click="toggleFavorite">
              <Icon :name="isFavorited ? 'heartfill' : 'heart'" :size="20" />
            </button>
            <button class="btn-icon" :style="{ color: showFullyWatched ? 'var(--good)' : 'var(--fg-1)' }" @click="toggleShowWatched" :title="showFullyWatched ? 'Mark as unwatched' : 'Mark as watched'">
              <Icon name="check" :size="20" />
            </button>
            <button class="btn-icon" title="Edit Metadata" @click="showMetadataEditor = true">
              <Icon name="settings" :size="18" />
            </button>
          </div>

          <MediaSynopsis :text="detail.preferred_overview || detail.media_item.description" />

          <MediaCrewSummary :crew="detail.crew">
            <template #extra>
              <template v-if="(detail.tv_series as any)?.networks?.length">
                <div class="info-label">Network</div>
                <div class="info-value">{{ (detail.tv_series as any).networks.join(', ') }}</div>
              </template>
              <template v-if="(detail.tv_series as any)?.created_by?.length">
                <div class="info-label">Created By</div>
                <div class="info-value">{{ (detail.tv_series as any).created_by.join(', ') }}</div>
              </template>
              <template v-if="detail.production_companies?.length">
                <div class="info-label">Studio</div>
                <div class="info-value">{{ detail.production_companies.map((c: any) => c.name).join(', ') }}</div>
              </template>
              <template v-if="detail.tv_series?.first_air_date">
                <div class="info-label">First Aired</div>
                <div class="info-value">{{ formatDate(detail.tv_series.first_air_date) }}</div>
              </template>
            </template>
          </MediaCrewSummary>

          <MediaKeywords :keywords="detail.keywords" />
        </div>

        <!-- Right column: ratings -->
        <div v-if="detail.external_ratings?.length" class="hero-side">
          <MediaRatings :ratings="detail.external_ratings" />
        </div>
      </div>

      <!-- Backdrop indicators -->
      <div v-if="backdropAssets.length > 1" class="bd-indicators" @mouseenter="pauseCarousel" @mouseleave="resumeCarousel">
        <button
          v-for="(_, i) in backdropAssets"
          :key="`bd-${i}-${backdropIdx}`"
          class="bd-bar"
          :class="{ active: i === backdropIdx, paused: carouselPaused && i === backdropIdx }"
          @click="jumpToBackdrop(i)"
        />
      </div>

      <!-- Expand backdrop -->
      <button v-if="backdropAssets.length > 0" class="hero-expand" @click="openBackdropLightbox">
        <Icon name="expand" :size="14" />
      </button>
    </div>

    <div class="detail-body-below">
      <!-- Seasons -->
      <div class="detail-section">
        <div class="section-row-head">
          <h3 class="section-title-lg">Seasons</h3>
        </div>
        <div class="seasons-grid">
          <NuxtLink
            v-for="s in displaySeasons"
            :key="s.season_number"
            :to="seasonUrl(s)"
            class="season-card"
          >
            <MediaCard
              :idx="s.season_number"
              :src="seasonPosterUrl(s)"
              aspect="2/3"
              :title="seasonLabel(s)"
              :subtitle="seasonSubtitle(s)"
              :progress-pct="seasonWatchInfo(s) ? seasonWatchPct(s) : 0"
            >
              <template #badges>
                <div v-if="seasonWatchInfo(s)" class="season-badge" :class="{ complete: seasonWatchInfo(s)!.remaining === 0 }">
                  <Icon v-if="seasonWatchInfo(s)!.remaining === 0" name="check" :size="10" />
                  <span v-else>{{ seasonWatchInfo(s)!.remaining }}</span>
                </div>
                <div class="season-overlay">
                  <button class="season-action" :class="{ loved: isSeasonFavorited(s) }" @click.stop.prevent="toggleSeasonFavorite(s)">
                    <Icon :name="isSeasonFavorited(s) ? 'heartfill' : 'heart'" :size="14" />
                  </button>
                  <button class="season-action" :class="{ watched: seasonFullyWatched(s) }" @click.stop.prevent="toggleSeasonWatched(s)">
                    <Icon name="check" :size="14" />
                  </button>
                  <button class="season-action" @click.stop.prevent="openSeasonLightbox(s)">
                    <Icon name="expand" :size="14" />
                  </button>
                </div>
              </template>
            </MediaCard>
          </NuxtLink>
        </div>
      </div>

      <!-- Cast & Crew -->
      <CastCrewTabs v-if="detail.cast?.length || detail.crew?.length" :cast="detail.cast" :crew="detail.crew" />

      <!-- Videos -->
      <div v-if="detail.videos?.length" class="detail-section">
        <div class="section-row-head"><h3 class="section-title-lg">Videos</h3></div>
        <div class="hscroll">
          <button v-for="(v, i) in detail.videos" :key="v.id" class="video-card" @click="openVideo(v.video_key, v.name)">
            <MediaCard
              :idx="i"
              :src="`https://img.youtube.com/vi/${v.video_key}/mqdefault.jpg`"
              aspect="16/9"
              :title="v.name"
              :badge-tl="v.video_type"
            >
              <template #badges>
                <div class="video-play"><Icon name="play" :size="20" /></div>
              </template>
            </MediaCard>
          </button>
        </div>
      </div>

      <!-- Video modal -->
      <AppDialog
        :model-value="!!videoModal"
        :title="videoModal?.title"
        size="lg"
        prevent-auto-focus
        content-class="video-dialog"
        @update:model-value="(v) => v ? null : videoModal = null"
      >
        <iframe
          v-if="videoModal"
          class="video-dialog-iframe"
          :src="`https://www.youtube-nocookie.com/embed/${videoModal.key}?autoplay=1&rel=0`"
          frameborder="0"
          allow="autoplay; encrypted-media; picture-in-picture"
          allowfullscreen
        />
      </AppDialog>

      <!-- Recommendations -->
      <div v-if="detail.recommendations?.length" class="detail-section">
        <div class="section-row-head">
          <h3 class="section-title-lg">More Like This</h3>
          <div v-if="recsOverflows" class="scroll-controls">
            <button class="scroll-ctrl-btn" @click="scrollRecs('left')"><Icon name="chevleft" :size="14" /></button>
            <button class="scroll-ctrl-btn" @click="scrollRecs('right')"><Icon name="chevright" :size="14" /></button>
            <button class="scroll-ctrl-btn expand" @click="recsExpanded = !recsExpanded">
              <Icon name="chevdown" :size="14" :style="{ transform: recsExpanded ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
            </button>
          </div>
        </div>
        <div v-if="!recsExpanded" ref="recsScrollEl" class="hscroll">
          <NuxtLink v-for="r in detail.recommendations" :key="r.id" :to="r.local_media_item_id ? mediaUrl({ id: r.local_media_item_id, title: r.title, year: '', media_type: r.media_type }) : ''" class="rec-card" :class="{ 'rec-external': !r.local_media_item_id }">
            <MediaCard
              :idx="r.id"
              :src="recPosterUrl(r)"
              aspect="2/3"
              :title="r.title"
              :badge-tr="r.vote_average ? `★ ${formatVote(r.vote_average)}` : ''"
            />
          </NuxtLink>
        </div>
        <div v-else class="rec-grid">
          <NuxtLink v-for="r in detail.recommendations" :key="r.id" :to="r.local_media_item_id ? mediaUrl({ id: r.local_media_item_id, title: r.title, year: '', media_type: r.media_type }) : ''" class="rec-card" :class="{ 'rec-external': !r.local_media_item_id }">
            <MediaCard
              :idx="r.id"
              :src="recPosterUrl(r)"
              aspect="2/3"
              :title="r.title"
              :badge-tr="r.vote_average ? `★ ${formatVote(r.vote_average)}` : ''"
            />
          </NuxtLink>
        </div>
      </div>

    </div>

    <!-- List modal -->
    <AddToListDialog v-model:open="showListModal" :media-item-id="detail.media_item.id" />

    <MetadataEditorModal
      v-if="detail"
      :media-id="detail.media_item.id"
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
const lightbox = useLightbox()

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
const showMetadataEditor = ref(false)
const videoModal = ref<{ key: string; title: string } | null>(null)

const recsExpanded = ref(false)
const recsScrollEl = ref<HTMLElement | null>(null)
const recsOverflows = ref(false)

function checkRecsOverflow() {
  nextTick(() => {
    if (recsScrollEl.value) {
      recsOverflows.value = recsScrollEl.value.scrollWidth > recsScrollEl.value.clientWidth
    } else {
      recsOverflows.value = (detail.value?.recommendations?.length || 0) > 6
    }
  })
}

function scrollRecs(dir: 'left' | 'right') {
  if (!recsScrollEl.value) return
  const amount = recsScrollEl.value.clientWidth * 0.75
  recsScrollEl.value.scrollBy({ left: dir === 'left' ? -amount : amount, behavior: 'smooth' })
}

function recPosterUrl(r: any): string {
  if (r.local_media_item_id) return usePosterUrl(r.local_media_item_id) ?? ''
  if (!r.poster_path) return ''
  if (r.poster_path.startsWith('http')) return r.poster_path
  return `/api/tmdb/image${r.poster_path}?size=w342`
}

function openVideo(key: string, title: string) {
  videoModal.value = { key, title }
}

// Crossfade backdrops — shared carousel engine.
const {
  showA, backdropA, backdropB, backdropIdx, carouselPaused, backdropAssets,
  pauseCarousel, resumeCarousel, jumpToBackdrop, seedCarousel, openBackdropLightbox,
} = useBackdropCarousel(detail, { maxSortOrder: 1000 })

// Lightbox openers
function openPosterLightbox() {
  const src = usePosterUrl(detail.value!.media_item.id)
  if (src) lightbox.open(src)
}

function openSeasonLightbox(s: any) {
  const url = seasonPosterUrl(s)
  if (url) lightbox.open(url)
}

const displaySeasons = computed(() => {
  if (!detail.value?.seasons) return []
  return [...detail.value.seasons].sort((a: any, b: any) => a.season_number - b.season_number)
})

// Hero counts reflect what we actually hold, not the provider catalog
// (tv_series.number_of_episodes counts unaired episodes too — 30 for Silo when
// we only have 22). Regular seasons only; specials (season 0) are excluded to
// match the catalog convention the hero used before. `detail.seasons` is
// already limited to seasons we have files for (server-side availableSeasons),
// so its regular-season count is our season count.
const regularSeasons = computed(() =>
  (detail.value?.seasons || []).filter((s: any) => s.season_number > 0))

const presentSeasonCount = computed(() => regularSeasons.value.length)

const presentEpisodeTotal = computed(() =>
  regularSeasons.value.reduce(
    (sum: number, s: any) => sum + presentEpisodeCount(detail.value?.episode_files as any, s.season_number, s.episodes),
    0))

const rating = computed(() => {
  const r = detail.value?.tv_series?.rating
  if (r == null || r === '') return null
  const n = typeof r === 'number' ? r : parseFloat(String(r))
  return isNaN(n) || n === 0 ? null : n.toFixed(1)
})

const certification = computed(() => {
  if (detail.value?.preferred_certification) return detail.value.preferred_certification
  const certs = detail.value?.certifications
  if (!certs?.length) return null
  const us = certs.find((c: any) => c.country === 'US')
  return (us || certs[0])?.certification || null
})

const genres = computed(() => detail.value?.tv_series?.genres || [])

interface UpNextData {
  has_next: boolean
  episode_id?: number
  episode_number?: number
  episode_title?: string
  season_number?: number
  media_item_id?: number
}
const upNext = ref<UpNextData | null>(null)

const nextEpisodeKey = computed(() => {
  if (upNext.value?.has_next && upNext.value.season_number && upNext.value.episode_number) {
    return `s${upNext.value.season_number}e${upNext.value.episode_number}`
  }
  if (!detail.value?.episode_files) return null
  const keys = Object.keys(detail.value.episode_files).sort()
  return keys.length > 0 ? keys[0] : null
})

const firstEpisodeFileId = computed(() => {
  if (!nextEpisodeKey.value || !detail.value?.episode_files) return null
  return detail.value.episode_files[nextEpisodeKey.value]?.file_id ?? null
})

const nextEpisodeLabel = computed(() => {
  if (!nextEpisodeKey.value) return ''
  const match = nextEpisodeKey.value.match(/^s(\d+)e(\d+)$/)
  if (!match) return ''
  const [, s, e] = match
  if (!s || !e) return ''
  return `S${s.padStart(2, '0')}E${e.padStart(2, '0')}`
})

const nextEpisodeFull = computed(() => {
  const key = nextEpisodeKey.value
  if (!key || !nextEpisodeLabel.value) return ''
  const match = key.match(/^s(\d+)e(\d+)$/)
  if (!match) return nextEpisodeLabel.value
  const [, sStr, eStr] = match
  if (!sStr || !eStr) return nextEpisodeLabel.value
  const sNum = parseInt(sStr)
  const eNum = parseInt(eStr)
  const season = detail.value?.seasons?.find((s: any) => s.season_number === sNum)
  const ep = season?.episodes?.find((e: any) => e.episode_number === eNum)
  const title = ep?.preferred_title || ep?.title || upNext.value?.episode_title
  if (title) return `${nextEpisodeLabel.value} - ${title}`
  return nextEpisodeLabel.value
})

function playFirstEpisode() {
  if (!firstEpisodeFileId.value || !detail.value || !nextEpisodeKey.value) return
  const params = new URLSearchParams({
    media_item_id: String(detail.value.media_item.id),
    title: `${detail.value.media_item.title} - ${nextEpisodeLabel.value}`,
  })
  if (upNext.value?.episode_id) {
    params.set('entity_type', 'episode')
    params.set('entity_id', String(upNext.value.episode_id))
  }
  navigateTo(`/watch/${firstEpisodeFileId.value}?${params}`)
}

// Resume label for the Play button — driven by saved progress on the
// next-to-play episode. When the user has watched some of S01E03 already
// the button reads "Resume S01E03 - …" instead of "Play".
const upNextEpisodeId = computed(() => upNext.value?.episode_id ?? 0)
const { inProgress: episodeInProgress } = useWatchResume('episode', upNextEpisodeId)

async function loadUpNext() {
  if (!detail.value) return
  try {
    const { $heya } = useNuxtApp()
    upNext.value = await $heya('/api/media/{id}/up-next', {
      path: { id: detail.value.media_item.id as any },
    }) as UpNextData
  } catch { /* empty */ }
}

// Favorites
const isFavorited = ref(false)

async function toggleFavorite() {
  if (!detail.value) return
  const { $heya } = useNuxtApp()
  const res = await $heya('/api/me/favorites', {
    method: 'POST',
    body: { entity_type: 'media_item', entity_id: detail.value.media_item.id } as any,
  }) as { favorited: boolean }
  isFavorited.value = res.favorited
}

async function loadState() {
  if (!detail.value) return
  try {
    const st = await fetchUserState('seasons', detail.value.media_item.id)
    seasonStates.value = new Map((st.seasons || []).map(s => [s.season_id, s]))
    isFavorited.value = (st.favorited_media || []).includes(detail.value.media_item.id)
    seasonFavorites.value = new Set(st.favorited_seasons || [])
  } catch { /* empty */ }
}

// Season favorites
const seasonFavorites = ref<Set<number>>(new Set())

function isSeasonFavorited(s: any) { return seasonFavorites.value.has(s.id) }

async function toggleSeasonFavorite(s: any) {
  const { $heya } = useNuxtApp()
  const res = await $heya('/api/me/favorites', {
    method: 'POST',
    body: { entity_type: 'season', entity_id: s.id } as any,
  }) as { favorited: boolean }
  if (res.favorited) seasonFavorites.value.add(s.id)
  else seasonFavorites.value.delete(s.id)
}

// Season watched tracking
const seasonStates = ref<Map<number, { season_id: number; total_episodes: number; watched_episodes: number }>>(new Map())

const showFullyWatched = computed(() => {
  const seasons = detail.value?.seasons || []
  if (seasons.length === 0 || seasonStates.value.size === 0) return false
  return seasons.every((s: any) => seasonFullyWatched(s))
})

// Watched math is against the present-episode total, not the provider catalog —
// otherwise an airing season reads "8 remaining" over unaired episodes while its
// subtitle says "2 eps". watched is clamped to that total ("mark season watched"
// flags the whole catalog, so raw watched_episodes can exceed what we hold).
function seasonPresentWatch(s: any): { total: number, watched: number } {
  const total = presentEpisodeCount(detail.value?.episode_files as any, s.season_number, s.episodes)
  const info = seasonStates.value.get(s.id)
  const watched = Math.min(info?.watched_episodes ?? 0, total)
  return { total, watched }
}

function seasonWatchInfo(s: any): { remaining: number } | null {
  const { total, watched } = seasonPresentWatch(s)
  if (total === 0) return null
  return { remaining: total - watched }
}

function seasonWatchPct(s: any): number {
  const { total, watched } = seasonPresentWatch(s)
  if (total === 0) return 0
  return Math.round((watched / total) * 100)
}

function seasonFullyWatched(s: any): boolean {
  const { total, watched } = seasonPresentWatch(s)
  return total > 0 && watched >= total
}

async function toggleSeasonWatched(s: any) {
  const watched = seasonFullyWatched(s)
  const { $heya } = useNuxtApp()
  await $heya('/api/me/watched/season/{id}', {
    method: 'POST',
    path: { id: s.id },
    body: { watched: !watched } as any,
  })
  await loadState()
}

async function toggleShowWatched() {
  if (!detail.value) return
  const { $heya } = useNuxtApp()
  await $heya('/api/me/watched/media/{id}', {
    method: 'POST',
    path: { id: detail.value.media_item.id },
    body: { watched: !showFullyWatched.value } as any,
  })
  await loadState()
}

// User lists — AddToListDialog owns loading/creation/toggling.
const showListModal = ref(false)

function seasonUrl(s: any) {
  const num = s.season_number === 0 ? 'specials' : String(s.season_number)
  return `/tv/${slug.value}/season/${num}`
}

function seasonPosterUrl(s: any) {
  return `/api/media/${detail.value?.media_item.id}/image/poster?label=season-${s.season_number}`
}

function seasonLabel(s: any) {
  if (s.season_number === 0) return 'Specials'
  return s.title || `Season ${s.season_number}`
}

function seasonSubtitle(s: any): string {
  const parts: string[] = []
  const n = presentEpisodeCount(detail.value?.episode_files as any, s.season_number, s.episodes)
  if (n) parts.push(`${n} ep${n !== 1 ? 's' : ''}`)
  const y = formatYear(s.air_date)
  if (y) parts.push(y)
  return parts.join(' · ')
}

function formatYear(d: string) { return d?.slice(0, 4) || '' }

// Re-run side effects whenever the detail data arrives or changes (route
// param change triggers a re-fetch via the reactive query key).
watch(detail, async (d) => {
  if (!d) return
  await nextTick()
  seedCarousel()
  loadState()
  loadUpNext()
  checkRecsOverflow()
}, { immediate: true })
</script>

<style scoped>
/* Hero — matches movie detail page. The shared backdrop/carousel/zoom chrome
   (.hero-bg*, .bd-*, .hero-expand, .zoom-btn, .hero-side, .detail-body-below,
   .scroll-controls, .hscroll) lives in heya.css; only per-page deltas here. */
.hero-section { min-height: 520px; }
.hero-content {
  position: relative; z-index: 2;
  display: grid; grid-template-columns: 260px minmax(0, 1fr) 260px;
  gap: 36px; padding: 40px 40px 48px;
}
.hero-poster { align-self: start; position: relative; }
.hero-info { display: flex; flex-direction: column; justify-content: center; min-width: 0; }

.detail-badges { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px; }
.detail-title { font-size: 44px; font-weight: 600; letter-spacing: -0.025em; line-height: 1.05; margin: 0 0 4px; }
.hero-meta-row { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg-2); margin-top: 8px; }
.dot { width: 3px; height: 3px; border-radius: 50%; background: var(--fg-3); }
.detail-actions { display: flex; align-items: center; gap: 10px; margin: 16px 0; }
.btn-icon { background: none; border: none; cursor: pointer; padding: 4px; }

/* Season badge (episodes remaining / checkmark) — slotted into MediaCard.
   z-index 3 puts it above the gradient. */
.season-badge {
  position: absolute; top: 8px; left: 8px; z-index: 3;
  min-width: 22px; height: 22px; padding: 0 6px;
  border-radius: 100px; font-size: 11px; font-weight: 700; font-family: var(--font-mono);
  background: rgba(0,0,0,0.6); backdrop-filter: blur(6px); color: var(--fg-0);
  display: flex; align-items: center; justify-content: center;
}
.season-badge.complete { background: var(--good); color: #000; }

/* Season hover actions (heart, check, expand) — sits in MediaCard's badges
   slot, anchored top-right above the title overlay so it never collides
   with the bottom info text. */
.season-overlay {
  position: absolute; top: 8px; right: 8px; z-index: 4;
  display: flex; gap: 4px;
  opacity: 0; transition: opacity 0.15s;
}
.season-card:hover .season-overlay { opacity: 1; }
.season-action {
  width: 26px; height: 26px; border-radius: var(--r-sm);
  background: rgba(0,0,0,0.6); backdrop-filter: blur(6px);
  color: rgba(255,255,255,0.75);
  display: flex; align-items: center; justify-content: center;
  cursor: pointer; transition: background 0.15s, color 0.15s;
}
.season-action:hover { background: rgba(0,0,0,0.8); color: #fff; }
.season-action.loved { color: var(--bad); }
.season-action.watched { color: var(--good); }

/* Seasons grid */
.seasons-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 20px; }
.season-card { text-decoration: none; color: inherit; position: relative; display: block; }

/* Body */
.detail-section { margin-top: 36px; }
.section-row-head { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 16px; }

/* Videos */
.video-card {
  width: 280px; flex-shrink: 0; text-align: left;
  background: none; border: none; cursor: pointer; color: inherit; padding: 0;
}
.video-play {
  position: absolute; inset: 0; z-index: 3;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.35); opacity: 0; transition: opacity 0.15s;
  color: #fff; pointer-events: none;
}
.video-card:hover .video-play { opacity: 1; }

/* Video dialog — same as movies/[slug]; iframe edge-to-edge, 16:9. */
.video-dialog .app-dialog-body { padding: 0; }
.video-dialog-iframe {
  width: 100%;
  aspect-ratio: 16 / 9;
  display: block;
  border: 0;
}

/* Recs */
.rec-card { width: 140px; flex-shrink: 0; text-decoration: none; color: inherit; display: block; }
.rec-card.rec-external { cursor: default; opacity: 0.65; }
.rec-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(130px, 1fr)); gap: 18px; }
.rec-grid .rec-card { width: auto; }

@media (max-width: 1200px) {
  .hero-content { grid-template-columns: 240px minmax(0, 1fr); }
  .hero-side { grid-column: 1 / -1; flex-direction: row; flex-wrap: wrap; gap: 14px; }
  .hero-side > * { flex: 1 1 280px; }
}

/* Tablet (folded from the previous 900px collapse point onto the ratified
   960px convention — docs/ui.md "Responsive conventions"). */
@media (max-width: 960px) {
  .hero-content { grid-template-columns: 1fr; gap: 20px; padding: 32px 20px 24px; }
  .hero-poster { max-width: 200px; }
  .detail-title { font-size: 32px; }
  .seasons-grid { grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 12px; }
}

/* Phone: tighter padding/poster, meta rows wrap, primary CTA ("Play/Resume
   S01E03 - Title" can be long) takes its own full-width row, every button
   meets the 44px touch target minimum, season tiles compress further. */
@media (max-width: 720px) {
  .hero-content { padding: 24px 16px 20px; gap: 16px; }
  .hero-poster { max-width: 140px; }
  .detail-title { font-size: 26px; }
  .hero-meta-row { flex-wrap: wrap; row-gap: 6px; }
  .detail-actions { flex-wrap: wrap; row-gap: 10px; }
  .detail-actions .btn { height: 44px; }
  .detail-actions .btn-primary { flex: 1 1 100%; white-space: normal; text-align: left; line-height: 1.3; height: auto; min-height: 44px; padding: 10px 16px; }
  .detail-actions .btn-icon { width: 44px; height: 44px; }
  .seasons-grid { grid-template-columns: repeat(auto-fill, minmax(100px, 1fr)); gap: 10px; }
  /* Forcing `.season-overlay` permanently visible for touch (below) leaves
     no room next to the episode-count `.season-badge` at this card width
     (~110px) — both shrink so the two corners stop overlapping. */
  .season-badge { min-width: 18px; height: 18px; font-size: 9px; padding: 0 4px; top: 6px; left: 6px; }
  .season-overlay { top: 6px; right: 6px; gap: 3px; }
  .season-action { width: 20px; height: 20px; }
}

/* Touch: swipe replaces the mouse-only scroll arrows on the recs section-head
   controls; the fold/expand toggle stays. The season-card quick actions
   (favorite/watched/expand) are hover-revealed on desktop — on a touch
   device there's no hover, so they'd be permanently unreachable without this. */
@media (pointer: coarse) {
  .scroll-controls .scroll-ctrl-btn:not(.expand) { display: none; }
  /* `transition: none` matters here, not just cosmetics: without it, headless
     Chrome under CDP touch emulation (no active compositor pump) can freeze
     the opacity transition at its pre-change value indefinitely — verified
     empirically via Heya Eye. A real device's continuous render loop would
     finish the 150ms transition regardless, but forcing it instant removes
     any doubt for a state that's supposed to be permanently on anyway. */
  .season-overlay { opacity: 1; transition: none; }
}
</style>
