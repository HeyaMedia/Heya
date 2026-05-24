<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div style="height: 380px; background: var(--bg-2)" />
  </div>

  <div v-else-if="detail" class="scroll" style="height: 100%">
    <!-- Hero with crossfade backdrops -->
    <div class="hero-section">
      <div class="hero-bg">
        <img v-if="backdropA" :src="backdropA" class="hero-bg-img" :class="{ visible: showA }" />
        <img v-if="backdropB" :src="backdropB" class="hero-bg-img" :class="{ visible: !showA }" />
        <div class="hero-bg-fade" />
      </div>

      <div class="hero-content">
        <div class="hero-poster">
          <Poster :idx="0" :src="usePosterUrl(detail.media_item.id)" :title="detail.media_item.title" aspect="2/3" />
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
            <span>{{ detail.tv_series?.number_of_seasons }} season{{ detail.tv_series?.number_of_seasons !== 1 ? 's' : '' }}</span>
            <span class="dot" />
            <span>{{ detail.tv_series?.number_of_episodes }} episodes</span>
          </div>

          <div v-if="genres.length" style="display: flex; gap: 6px; flex-wrap: wrap; margin: 12px 0">
            <NuxtLink v-for="g in genres" :key="g" :to="`/genre/${encodeURIComponent(g)}`"><Chip>{{ g }}</Chip></NuxtLink>
          </div>

          <div class="detail-actions">
            <button v-if="firstEpisodeFileId" class="btn btn-primary" @click="playFirstEpisode"><Icon name="play" :size="16" /> Play {{ nextEpisodeFull }}</button>
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
            <div class="season-poster-wrap">
              <Poster :idx="s.season_number" :src="seasonPosterUrl(s)" :title="seasonLabel(s)" aspect="2/3" />
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
            </div>
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ seasonLabel(s) }}</div>
              <div class="grid-tile-sub">
                <span v-if="s.episodes?.length">{{ s.episodes.length }} ep{{ s.episodes.length !== 1 ? 's' : '' }}</span>
                <span v-if="s.air_date"> &middot; {{ formatYear(s.air_date) }}</span>
              </div>
              <div v-if="seasonWatchInfo(s)" class="season-progress-mini">
                <div class="season-progress-mini-fill" :style="{ width: seasonWatchPct(s) + '%' }" />
              </div>
            </div>
          </NuxtLink>
        </div>
      </div>

      <!-- Cast & Crew -->
      <div v-if="detail.cast?.length || detail.crew?.length" class="detail-section">
        <div class="section-row-head" style="margin-bottom: 0">
          <div class="tab-bar" style="margin-bottom: 0">
            <button class="tab-btn" :class="{ active: peopleTab === 'cast' }" @click="peopleTab = 'cast'">
              Cast <span class="tab-count">{{ detail.cast?.length || 0 }}</span>
            </button>
            <button class="tab-btn" :class="{ active: peopleTab === 'crew' }" @click="peopleTab = 'crew'">
              Crew <span class="tab-count">{{ detail.crew?.length || 0 }}</span>
            </button>
          </div>
          <div v-if="peopleTab === 'cast' && castOverflows" class="scroll-controls">
            <button class="scroll-ctrl-btn" @click="scrollCast('left')"><Icon name="chevleft" :size="14" /></button>
            <button class="scroll-ctrl-btn" @click="scrollCast('right')"><Icon name="chevright" :size="14" /></button>
            <button v-if="detail.cast && detail.cast.length > 8" class="scroll-ctrl-btn expand" @click="castExpanded = !castExpanded">
              <Icon name="chevdown" :size="14" :style="{ transform: castExpanded ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
            </button>
          </div>
        </div>

        <div v-if="peopleTab === 'cast'" style="margin-top: 16px">
          <!-- Scroll mode -->
          <div v-if="!castExpanded" ref="castScrollEl" class="hscroll">
            <NuxtLink v-for="c in detail.cast" :key="c.id" :to="personUrl(c)" class="cast-card">
              <div v-if="c.profile_path && !c.profile_path.startsWith('http')" class="cast-photo-wrap">
                <img :src="`/api/person/${c.id}/image`" class="cast-photo" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
                <button class="zoom-btn round" @click.stop.prevent="lightbox.open(`/api/person/${c.id}/image`)"><Icon name="expand" :size="10" /></button>
              </div>
              <div v-else class="cast-avatar">{{ c.name.split(' ').map((n: string) => n[0]).join('').slice(0, 2) }}</div>
              <div class="cast-name">{{ c.name }}</div>
              <div class="cast-role">{{ c.character }}</div>
            </NuxtLink>
          </div>
          <!-- Expanded grid mode -->
          <div v-else class="cast-grid">
            <NuxtLink v-for="c in detail.cast" :key="c.id" :to="personUrl(c)" class="cast-card">
              <div v-if="c.profile_path && !c.profile_path.startsWith('http')" class="cast-photo-wrap">
                <img :src="`/api/person/${c.id}/image`" class="cast-photo" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
                <button class="zoom-btn round" @click.stop.prevent="lightbox.open(`/api/person/${c.id}/image`)"><Icon name="expand" :size="10" /></button>
              </div>
              <div v-else class="cast-avatar">{{ c.name.split(' ').map((n: string) => n[0]).join('').slice(0, 2) }}</div>
              <div class="cast-name">{{ c.name }}</div>
              <div class="cast-role">{{ c.character }}</div>
            </NuxtLink>
          </div>
        </div>

        <div v-if="peopleTab === 'crew'" style="margin-top: 16px">
          <div v-for="dept in crewByDepartment" :key="dept.name" class="crew-dept">
            <div class="crew-dept-label">{{ dept.name }}</div>
            <div class="crew-dept-grid">
              <NuxtLink v-for="c in dept.members" :key="`${c.id}-${c.job}`" :to="personUrl(c)" class="crew-card">
                <div v-if="c.profile_path && !c.profile_path.startsWith('http')" class="crew-photo-wrap">
                  <img :src="`/api/person/${c.id}/image`" class="crew-photo" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
                  <button class="zoom-btn round crew-zoom" @click.stop.prevent="lightbox.open(`/api/person/${c.id}/image`)"><Icon name="expand" :size="8" /></button>
                </div>
                <div v-else class="crew-initials">{{ c.name.split(' ').map((n: string) => n[0]).join('').slice(0, 2) }}</div>
                <div class="crew-text">
                  <div class="crew-name">{{ c.name }}</div>
                  <div class="crew-job">{{ c.job }}</div>
                </div>
              </NuxtLink>
            </div>
          </div>
        </div>
      </div>

      <!-- Videos -->
      <div v-if="detail.videos?.length" class="detail-section">
        <div class="section-row-head"><h3 class="section-title-lg">Videos</h3></div>
        <div class="hscroll">
          <button v-for="v in detail.videos" :key="v.id" class="video-card" @click="openVideo(v.video_key, v.name)">
            <div class="video-thumb">
              <img :src="`https://img.youtube.com/vi/${v.video_key}/mqdefault.jpg`" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
              <div class="video-play"><Icon name="play" :size="20" /></div>
            </div>
            <div class="video-name">{{ v.name }}</div>
            <div class="video-type">{{ v.video_type }}</div>
          </button>
        </div>
      </div>

      <!-- Video modal -->
      <Teleport to="body">
        <Transition name="modal">
          <div v-if="videoModal" class="modal-overlay" @click.self="videoModal = null">
            <div class="video-modal-card">
              <div class="video-modal-header">
                <span class="video-modal-title">{{ videoModal.title }}</span>
                <button class="btn-icon" @click="videoModal = null"><Icon name="close" :size="16" /></button>
              </div>
              <div class="video-modal-body">
                <iframe
                  :src="`https://www.youtube-nocookie.com/embed/${videoModal.key}?autoplay=1&rel=0`"
                  frameborder="0"
                  allow="autoplay; encrypted-media; picture-in-picture"
                  allowfullscreen
                />
              </div>
            </div>
          </div>
        </Transition>
      </Teleport>

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
            <Poster :idx="r.id" :src="recPosterUrl(r)" aspect="2/3" :title="r.title" />
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ r.title }}</div>
              <div v-if="r.vote_average" class="rec-rating"><Icon name="star" :size="9" /> {{ formatVote(r.vote_average) }}</div>
            </div>
          </NuxtLink>
        </div>
        <div v-else class="rec-grid">
          <NuxtLink v-for="r in detail.recommendations" :key="r.id" :to="r.local_media_item_id ? mediaUrl({ id: r.local_media_item_id, title: r.title, year: '', media_type: r.media_type }) : ''" class="rec-card" :class="{ 'rec-external': !r.local_media_item_id }">
            <Poster :idx="r.id" :src="recPosterUrl(r)" aspect="2/3" :title="r.title" />
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ r.title }}</div>
              <div v-if="r.vote_average" class="rec-rating"><Icon name="star" :size="9" /> {{ formatVote(r.vote_average) }}</div>
            </div>
          </NuxtLink>
        </div>
      </div>

    </div>

    <!-- List modal -->
    <Teleport to="body">
      <Transition name="modal">
        <div v-if="showListModal" class="modal-overlay" @click.self="showListModal = false">
          <div class="modal-card">
            <div class="modal-header">
              <h3>Add to List</h3>
              <button class="btn-icon" @click="showListModal = false"><Icon name="close" :size="16" /></button>
            </div>
            <div class="modal-body">
              <div v-if="!showCreateList">
                <button
                  v-for="l in userLists" :key="l.id"
                  class="list-option" :class="{ active: l.contains }"
                  @click="toggleListItem(l)"
                >
                  <Icon :name="l.contains ? 'check' : 'plus'" :size="14" />
                  <span>{{ l.name }}</span>
                  <span class="list-option-count">{{ l.item_count }}</span>
                </button>
                <div v-if="!userLists.length" style="padding: 16px 0; color: var(--fg-3); font-size: 13px; text-align: center">No lists yet</div>
                <button class="list-create-btn" @click="showCreateList = true">
                  <Icon name="plus" :size="14" /> Create new list
                </button>
              </div>
              <div v-else>
                <input v-model="newListName" class="modal-input" placeholder="List name" @keydown.enter="createList" />
                <input v-model="newListDesc" class="modal-input" placeholder="Description (optional)" style="margin-top: 8px" />
                <div style="display: flex; gap: 8px; margin-top: 12px">
                  <button class="btn btn-primary" @click="createList" :disabled="!newListName.trim()">Create</button>
                  <button class="btn btn-secondary" @click="showCreateList = false">Cancel</button>
                </div>
              </div>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>

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

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const lightbox = useLightbox()

const detail = ref<MediaDetail | null>(null)
const loading = ref(true)
const peopleTab = ref<'cast' | 'crew'>('cast')
const castExpanded = ref(false)
const showMetadataEditor = ref(false)
const castScrollEl = ref<HTMLElement | null>(null)
const castOverflows = ref(false)
const videoModal = ref<{ key: string; title: string } | null>(null)

function checkCastOverflow() {
  nextTick(() => {
    if (castScrollEl.value) {
      castOverflows.value = castScrollEl.value.scrollWidth > castScrollEl.value.clientWidth
    } else {
      castOverflows.value = (detail.value?.cast?.length || 0) > 8
    }
  })
}

function scrollCast(dir: 'left' | 'right') {
  if (!castScrollEl.value) return
  const amount = castScrollEl.value.clientWidth * 0.75
  castScrollEl.value.scrollBy({ left: dir === 'left' ? -amount : amount, behavior: 'smooth' })
}

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

function formatVote(v: any): string {
  const n = typeof v === 'number' ? v : parseFloat(String(v))
  return isNaN(n) ? '' : n.toFixed(1)
}

function openVideo(key: string, title: string) {
  videoModal.value = { key, title }
}

// Crossfade backdrops
const showA = ref(true)
const backdropA = ref<string | null>(null)
const backdropB = ref<string | null>(null)
const backdropIdx = ref(0)
const carouselPaused = ref(false)

const BACKDROP_INTERVAL = 8000
let bdTimeout: ReturnType<typeof setTimeout> | null = null
let bdStart = 0
let bdRemaining = BACKDROP_INTERVAL

const backdropAssets = computed(() => {
  if (!detail.value?.assets) return []
  const seen = new Set<number>()
  return detail.value.assets
    .filter(a => a.asset_type === 'backdrop' && a.sort_order < 1000)
    .sort((a, b) => a.sort_order - b.sort_order)
    .filter(a => { if (seen.has(a.sort_order)) return false; seen.add(a.sort_order); return true })
})

function getBackdropUrl(idx: number) {
  if (backdropAssets.value.length > 0) {
    const asset = backdropAssets.value[idx % backdropAssets.value.length]!
    return `/api/media/${detail.value?.media_item.id}/image/backdrop?sort=${asset.sort_order}`
  }
  return detail.value ? useBackdropUrl(detail.value.media_item.id) : null
}

async function advanceBackdrop() {
  if (backdropAssets.value.length <= 1) return
  backdropIdx.value = (backdropIdx.value + 1) % backdropAssets.value.length
  const url = getBackdropUrl(backdropIdx.value)
  if (showA.value) { backdropB.value = url } else { backdropA.value = url }
  await nextTick()
  showA.value = !showA.value
}

function startCarouselTimer() {
  bdStart = Date.now()
  bdRemaining = BACKDROP_INTERVAL
  bdTimeout = setTimeout(() => {
    advanceBackdrop()
    startCarouselTimer()
  }, BACKDROP_INTERVAL)
}

function pauseCarousel() {
  carouselPaused.value = true
  if (bdTimeout) clearTimeout(bdTimeout)
  bdRemaining -= Date.now() - bdStart
}

function resumeCarousel() {
  carouselPaused.value = false
  bdStart = Date.now()
  bdTimeout = setTimeout(() => {
    advanceBackdrop()
    startCarouselTimer()
  }, bdRemaining)
}

function jumpToBackdrop(idx: number) {
  if (idx === backdropIdx.value) return
  if (bdTimeout) clearTimeout(bdTimeout)
  backdropIdx.value = idx
  const url = getBackdropUrl(idx)
  if (showA.value) { backdropB.value = url } else { backdropA.value = url }
  showA.value = !showA.value
  if (!carouselPaused.value) startCarouselTimer()
}

// Lightbox openers
function openPosterLightbox() {
  const src = usePosterUrl(detail.value!.media_item.id)
  if (src) lightbox.open(src)
}

function openBackdropLightbox() {
  const urls = backdropAssets.value.map((_, i) => getBackdropUrl(i)!)
  if (urls.length) lightbox.open(urls, backdropIdx.value)
  else {
    const src = useBackdropUrl(detail.value!.media_item.id)
    if (src) lightbox.open(src)
  }
}

function openSeasonLightbox(s: any) {
  const url = seasonPosterUrl(s)
  if (url) lightbox.open(url)
}

const displaySeasons = computed(() => {
  if (!detail.value?.seasons) return []
  return [...detail.value.seasons].sort((a: any, b: any) => a.season_number - b.season_number)
})

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

const crewByDepartment = computed(() => {
  const crew = detail.value?.crew || []
  const depts = new Map<string, any[]>()
  for (const c of crew) {
    const d = c.department || 'Other'
    if (!depts.has(d)) depts.set(d, [])
    depts.get(d)!.push(c)
  }
  return Array.from(depts.entries()).map(([name, members]) => ({ name, members }))
})

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
  navigateTo(`/watch/${firstEpisodeFileId.value}?${params}`)
}

async function loadUpNext() {
  if (!detail.value) return
  try {
    upNext.value = await apiFetch<UpNextData>(`/api/media/${detail.value.media_item.id}/up-next`)
  } catch { /* empty */ }
}

// Favorites
const isFavorited = ref(false)

async function toggleFavorite() {
  if (!detail.value) return
  const res = await apiFetch<{ favorited: boolean }>('/api/me/favorites', {
    method: 'POST',
    body: JSON.stringify({ entity_type: 'media_item', entity_id: detail.value.media_item.id }),
  })
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
  const res = await apiFetch<{ favorited: boolean }>('/api/me/favorites', {
    method: 'POST',
    body: JSON.stringify({ entity_type: 'season', entity_id: s.id }),
  })
  if (res.favorited) seasonFavorites.value.add(s.id)
  else seasonFavorites.value.delete(s.id)
}

// Season watched tracking
const seasonStates = ref<Map<number, { season_id: number; total_episodes: number; watched_episodes: number }>>(new Map())

const showFullyWatched = computed(() => {
  if (seasonStates.value.size === 0) return false
  for (const s of seasonStates.value.values()) {
    if (s.total_episodes === 0 || s.watched_episodes < s.total_episodes) return false
  }
  return true
})

function seasonWatchInfo(s: any): { remaining: number } | null {
  const info = seasonStates.value.get(s.id)
  if (!info || info.total_episodes === 0) return null
  return { remaining: info.total_episodes - info.watched_episodes }
}

function seasonWatchPct(s: any): number {
  const info = seasonStates.value.get(s.id)
  if (!info || info.total_episodes === 0) return 0
  return Math.round((info.watched_episodes / info.total_episodes) * 100)
}

function seasonFullyWatched(s: any): boolean {
  const info = seasonStates.value.get(s.id)
  return !!info && info.total_episodes > 0 && info.watched_episodes >= info.total_episodes
}

async function toggleSeasonWatched(s: any) {
  const watched = seasonFullyWatched(s)
  await apiFetch(`/api/me/watched/season/${s.id}`, { method: 'POST', body: JSON.stringify({ watched: !watched }) })
  await loadState()
}

async function toggleShowWatched() {
  if (!detail.value) return
  await apiFetch(`/api/me/watched/media/${detail.value.media_item.id}`, { method: 'POST', body: JSON.stringify({ watched: !showFullyWatched.value }) })
  await loadState()
}

// User lists
const showListModal = ref(false)
const showCreateList = ref(false)
const newListName = ref('')
const newListDesc = ref('')
const userLists = ref<any[]>([])

async function loadLists() {
  if (!detail.value) return
  try {
    userLists.value = await apiFetch<any[]>(`/api/me/lists?media_item_id=${detail.value.media_item.id}`)
  } catch { /* empty */ }
}

async function createList() {
  if (!newListName.value.trim()) return
  await apiFetch('/api/me/lists', { method: 'POST', body: JSON.stringify({ name: newListName.value.trim(), description: newListDesc.value.trim() }) })
  newListName.value = ''
  newListDesc.value = ''
  showCreateList.value = false
  await loadLists()
}

async function toggleListItem(l: any) {
  if (!detail.value) return
  if (l.contains) {
    await apiFetch(`/api/me/lists/${l.id}/items/${detail.value.media_item.id}`, { method: 'DELETE' })
  } else {
    await apiFetch(`/api/me/lists/${l.id}/items`, { method: 'POST', body: JSON.stringify({ media_item_id: detail.value.media_item.id }) })
  }
  await loadLists()
}

watch(showListModal, (v) => { if (v) loadLists() })

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

function formatDate(d: string) {
  if (!d) return ''
  try { return new Date(d + 'T00:00:00').toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' }) }
  catch { return d }
}

function formatYear(d: string) { return d?.slice(0, 4) || '' }

onMounted(async () => {
  try {
    detail.value = await apiFetch<MediaDetail>(`/api/media/${slug.value}`)
    await nextTick()
    backdropA.value = getBackdropUrl(0)
    backdropB.value = getBackdropUrl(0)
    if (backdropAssets.value.length > 1) {
      startCarouselTimer()
    }
    loadState()
    loadUpNext()
    checkCastOverflow()
    checkRecsOverflow()
  } catch { navigateTo('/tv') }
  loading.value = false
})

onUnmounted(() => { if (bdTimeout) clearTimeout(bdTimeout) })
</script>

<style scoped>
/* Hero — matches movie detail page */
.hero-section { position: relative; min-height: 520px; }
.hero-bg { position: absolute; inset: 0; overflow: hidden; }
.hero-bg-img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; opacity: 0; transition: opacity 1.5s ease; }
.hero-bg-img.visible { opacity: 1; }
.hero-bg-fade {
  position: absolute; inset: 0;
  background:
    linear-gradient(to right, var(--bg-1) 0%, rgba(12,12,16,0.7) 40%, rgba(12,12,16,0.4) 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 50%);
}
.hero-content {
  position: relative; z-index: 2;
  display: grid; grid-template-columns: 260px minmax(0, 1fr) 260px;
  gap: 36px; padding: 40px 40px 48px;
}
.hero-poster { align-self: start; position: relative; }
.hero-info { display: flex; flex-direction: column; justify-content: center; min-width: 0; }
.hero-side { display: flex; flex-direction: column; gap: 14px; align-self: start; min-width: 0; }

.detail-badges { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px; }
.detail-title { font-size: 44px; font-weight: 600; letter-spacing: -0.025em; line-height: 1.05; margin: 0 0 4px; }
.hero-meta-row { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg-2); margin-top: 8px; }
.dot { width: 3px; height: 3px; border-radius: 50%; background: var(--fg-3); }
.detail-actions { display: flex; align-items: center; gap: 10px; margin: 16px 0; }
.btn-icon { background: none; border: none; cursor: pointer; padding: 4px; }

/* Backdrop indicators */
.bd-indicators {
  position: absolute;
  bottom: 24px;
  right: 48px;
  z-index: 4;
  display: flex;
  gap: 5px;
}
.bd-bar {
  width: 28px;
  height: 3px;
  border-radius: 2px;
  background: rgba(255,255,255,0.2);
  position: relative;
  overflow: hidden;
  cursor: pointer;
  transition: background 0.15s;
}
.bd-bar:hover { background: rgba(255,255,255,0.4); }
.bd-bar.active { background: rgba(255,255,255,0.12); }
.bd-bar.active::after {
  content: '';
  position: absolute;
  left: 0; top: 0; bottom: 0;
  background: var(--gold);
  border-radius: 2px;
  animation: bd-fill 8s linear forwards;
}
.bd-bar.paused::after { animation-play-state: paused; }
@keyframes bd-fill { from { width: 0; } to { width: 100%; } }

/* Expand button */
.hero-expand {
  position: absolute;
  bottom: 24px;
  right: 16px;
  z-index: 4;
  width: 30px;
  height: 30px;
  border-radius: var(--r-sm);
  background: rgba(0,0,0,0.4);
  border: 1px solid rgba(255,255,255,0.1);
  color: rgba(255,255,255,0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.15s;
  opacity: 0;
}
.hero-section:hover .hero-expand { opacity: 1; }
.hero-expand:hover { background: rgba(0,0,0,0.6); color: #fff; }

/* Zoom button on images */
.zoom-btn {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  border-radius: var(--r-sm);
  background: rgba(0,0,0,0.55);
  color: rgba(255,255,255,0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0;
  transition: opacity 0.15s, background 0.15s;
  cursor: zoom-in;
  z-index: 2;
}
.zoom-btn:hover { background: rgba(0,0,0,0.8); color: #fff; }
.zoom-btn.sm { width: 22px; height: 22px; top: 6px; right: 6px; }
.zoom-btn.round { border-radius: 50%; top: 2px; right: 2px; width: 20px; height: 20px; }
.zoom-btn.crew-zoom { top: 0; right: 0; width: 16px; height: 16px; }
.hero-poster:hover .zoom-btn,
.season-poster-wrap:hover .zoom-btn,
.cast-photo-wrap:hover .zoom-btn,
.crew-photo-wrap:hover .zoom-btn { opacity: 1; }

/* Season poster wrap */
.season-poster-wrap { position: relative; border-radius: var(--r-md); overflow: hidden; }

/* Season badge (episodes remaining / checkmark) */
.season-badge {
  position: absolute; top: 8px; left: 8px; z-index: 2;
  min-width: 22px; height: 22px; padding: 0 6px;
  border-radius: 100px; font-size: 11px; font-weight: 700; font-family: var(--font-mono);
  background: rgba(0,0,0,0.7); color: var(--fg-0);
  display: flex; align-items: center; justify-content: center;
}
.season-badge.complete { background: var(--good); color: #000; }

/* Season overlay actions (heart, check, expand) */
.season-overlay {
  position: absolute; bottom: 0; left: 0; right: 0; z-index: 2;
  display: flex; gap: 4px; padding: 6px;
  background: linear-gradient(to top, rgba(0,0,0,0.7), transparent);
  opacity: 0; transition: opacity 0.15s;
}
.season-poster-wrap:hover .season-overlay { opacity: 1; }
.season-action {
  width: 28px; height: 28px; border-radius: var(--r-sm);
  background: rgba(0,0,0,0.5); color: rgba(255,255,255,0.7);
  display: flex; align-items: center; justify-content: center;
  cursor: pointer; transition: background 0.15s, color 0.15s;
}
.season-action:hover { background: rgba(0,0,0,0.8); color: #fff; }
.season-action.loved { color: var(--bad); }
.season-action.watched { color: var(--good); }

/* Cast / crew photo wrap */
.cast-photo-wrap { position: relative; width: 90px; height: 90px; border-radius: 50%; overflow: hidden; margin: 0 auto; }
.crew-photo-wrap { position: relative; width: 38px; height: 38px; border-radius: 50%; overflow: hidden; flex-shrink: 0; }

/* Seasons grid */
.seasons-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 20px; }
.season-card { text-decoration: none; color: inherit; }
.season-card:hover .grid-tile-title { color: var(--gold); }

.season-progress-mini {
  width: 100%; height: 2px; margin-top: 5px;
  background: rgba(255,255,255,0.08); border-radius: 1px; overflow: hidden;
}
.season-progress-mini-fill {
  height: 100%; background: var(--gold); border-radius: 1px;
  transition: width 0.4s ease;
}

/* Body */
.detail-body-below { padding: 0 48px 80px; }
.detail-section { margin-top: 36px; }
.section-row-head { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 16px; }

/* Tabs */
.tab-bar { display: flex; gap: 4px; }
.tab-btn { padding: 8px 16px; border-radius: var(--r-md); font-size: 13px; font-weight: 500; color: var(--fg-2); background: none; border: none; cursor: pointer; transition: all 0.15s; }
.tab-btn:hover { background: rgba(255,255,255,0.04); }
.tab-btn.active { background: var(--bg-3); color: var(--fg-0); font-weight: 600; }
.tab-count { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); margin-left: 4px; }

/* Scroll controls (top-right of section header) */
.scroll-controls { display: flex; gap: 4px; align-items: center; }
.scroll-ctrl-btn {
  width: 28px; height: 28px; border-radius: var(--r-sm);
  background: rgba(255,255,255,0.04); border: 1px solid var(--border);
  color: var(--fg-2);
  display: flex; align-items: center; justify-content: center;
  cursor: pointer; transition: all 0.15s;
}
.scroll-ctrl-btn:hover { background: rgba(255,255,255,0.08); color: var(--fg-0); border-color: var(--border-strong); }
.scroll-ctrl-btn.expand { margin-left: 2px; }

.hscroll { display: flex; gap: 16px; overflow-x: auto; scrollbar-width: none; padding-bottom: 4px; }
.hscroll::-webkit-scrollbar { display: none; }

/* Cast cards */
.cast-card { width: 120px; flex-shrink: 0; text-decoration: none; color: inherit; text-align: center; }
.cast-card:hover .cast-name { color: var(--gold); }
.cast-photo { width: 90px; height: 90px; border-radius: 50%; object-fit: cover; display: block; }
.cast-avatar { width: 90px; height: 90px; border-radius: 50%; margin: 0 auto; background: linear-gradient(135deg, var(--bg-4), var(--bg-3)); display: flex; align-items: center; justify-content: center; font-size: 22px; font-weight: 600; color: var(--fg-2); }
.cast-name { font-size: 12px; font-weight: 500; margin-top: 8px; transition: color 0.15s; }
.cast-role { font-size: 10px; color: var(--fg-3); margin-top: 2px; }

/* Cast expanded grid */
.cast-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 16px;
}
.cast-grid .cast-card { width: auto; }


/* Crew */
.crew-dept { margin-bottom: 20px; }
.crew-dept-label {
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em; color: var(--fg-3);
  margin-bottom: 8px; padding-left: 2px;
}
.crew-dept-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 4px; }
.crew-card {
  display: flex; align-items: center; gap: 10px;
  padding: 8px 10px; border-radius: var(--r-md);
  text-decoration: none; color: inherit;
  transition: background 0.15s;
}
.crew-card:hover { background: rgba(255,255,255,0.04); }
.crew-photo { width: 38px; height: 38px; border-radius: 50%; object-fit: cover; display: block; }
.crew-initials {
  width: 38px; height: 38px; border-radius: 50%; flex-shrink: 0;
  background: var(--bg-4); display: flex; align-items: center; justify-content: center;
  font-size: 12px; font-weight: 600; color: var(--fg-2);
}
.crew-text { min-width: 0; }
.crew-name { font-size: 13px; font-weight: 500; }
.crew-job { font-size: 11px; color: var(--fg-3); }

/* Videos */
.video-card {
  width: 280px; flex-shrink: 0; text-align: left;
  background: none; border: none; cursor: pointer; color: inherit; padding: 0;
}
.video-card:hover .video-name { color: var(--gold); }
.video-thumb { position: relative; aspect-ratio: 16/9; border-radius: var(--r-md); overflow: hidden; background: var(--bg-3); }
.video-thumb img { width: 100%; height: 100%; object-fit: cover; }
.video-play { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; background: rgba(0,0,0,0.4); opacity: 0; transition: opacity 0.15s; color: #fff; }
.video-card:hover .video-play { opacity: 1; }
.video-name { font-size: 12px; font-weight: 500; margin-top: 8px; transition: color 0.15s; }
.video-type { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); text-transform: uppercase; }

/* Video modal */
.video-modal-card {
  background: var(--bg-1); border: 1px solid var(--border-strong); border-radius: var(--r-lg);
  width: 900px; max-width: 95vw; overflow: hidden;
}
.video-modal-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 12px 16px; border-bottom: 1px solid var(--border);
}
.video-modal-title { font-size: 14px; font-weight: 500; color: var(--fg-0); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
.video-modal-body { aspect-ratio: 16/9; }
.video-modal-body iframe { width: 100%; height: 100%; display: block; }

/* Recs */
.rec-card { width: 140px; flex-shrink: 0; text-decoration: none; color: inherit; }
.rec-card:hover .grid-tile-title { color: var(--gold); }
.rec-card.rec-external { cursor: default; }
.rec-rating { font-size: 10px; color: var(--gold); display: inline-flex; align-items: center; gap: 2px; margin-top: 1px; }
.rec-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(130px, 1fr)); gap: 18px; }
.rec-grid .rec-card { width: auto; }


/* Modal */
.modal-overlay {
  position: fixed; inset: 0; z-index: 9000;
  background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center;
}
.modal-card {
  background: var(--bg-2); border: 1px solid var(--border-strong); border-radius: var(--r-lg);
  width: 380px; max-width: 90vw; max-height: 80vh; overflow: auto;
}
.modal-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 16px 20px; border-bottom: 1px solid var(--border);
}
.modal-header h3 { font-size: 16px; font-weight: 600; margin: 0; }
.modal-body { padding: 12px 20px 20px; }
.modal-input {
  width: 100%; padding: 10px 14px; background: var(--bg-3); border: 1px solid var(--border);
  border-radius: var(--r-md); color: var(--fg-0); font-size: 14px; outline: none;
}
.modal-input:focus { border-color: var(--gold); }

.list-option {
  display: flex; align-items: center; gap: 10px; width: 100%;
  padding: 10px 12px; border-radius: var(--r-sm); font-size: 13px;
  color: var(--fg-1); transition: background 0.12s; text-align: left;
}
.list-option:hover { background: rgba(255,255,255,0.04); }
.list-option.active { color: var(--gold); }
.list-option-count { margin-left: auto; font-size: 10px; font-family: var(--font-mono); color: var(--fg-4); }

.list-create-btn {
  display: flex; align-items: center; gap: 8px; width: 100%;
  padding: 10px 12px; margin-top: 4px; border-top: 1px solid var(--border);
  font-size: 13px; color: var(--fg-2); transition: color 0.12s;
}
.list-create-btn:hover { color: var(--gold); }

.modal-enter-active, .modal-leave-active { transition: opacity 0.15s; }
.modal-enter-from, .modal-leave-to { opacity: 0; }

@media (max-width: 1200px) {
  .hero-content { grid-template-columns: 240px minmax(0, 1fr); }
  .hero-side { grid-column: 1 / -1; flex-direction: row; flex-wrap: wrap; gap: 14px; }
  .hero-side > * { flex: 1 1 280px; }
}

@media (max-width: 900px) {
  .hero-content { grid-template-columns: 1fr; gap: 20px; padding: 32px 20px 24px; }
  .hero-poster { max-width: 200px; }
  .detail-title { font-size: 32px; }
  .detail-body-below { padding: 0 20px 60px; }
  .seasons-grid { grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 12px; }
  .bd-indicators { right: 20px; bottom: 16px; }
  .hero-expand { display: none; }
}
</style>
