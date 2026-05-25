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
        <div class="hero-left">
          <div class="hero-poster">
            <Poster :idx="0" :src="usePosterUrl(detail.media_item.id)" :title="detail.media_item.title" aspect="2/3" />
            <button class="zoom-btn" @click="openPosterLightbox"><Icon name="expand" :size="14" /></button>
          </div>

          <!-- Stream / media info, beneath the poster -->
          <MediaStreamInfo v-if="streamInfo" :stream="streamInfo" />
        </div>

        <div class="hero-info">
          <div class="detail-badges">
            <Chip gold>Movie</Chip>
            <Chip v-if="certification">{{ certification }}</Chip>
            <Chip v-if="detail.media_item.year">{{ detail.media_item.year }}</Chip>
            <Chip v-if="detail.movie?.runtime_minutes">{{ formatRuntime(detail.movie.runtime_minutes) }}</Chip>
          </div>

          <h1 class="detail-title">{{ detail.preferred_title || detail.media_item.title }}</h1>
          <p v-if="detail.movie?.tagline" class="detail-tagline">{{ detail.movie.tagline }}</p>

          <div class="hero-meta-row" v-if="rating || detail.movie?.release_date">
            <template v-if="rating">
              <Icon name="star" :size="14" style="color: var(--gold)" />
              <span style="color: var(--gold)">{{ rating }}/10</span>
            </template>
            <template v-if="rating && detail.movie?.release_date"><span class="dot" /></template>
            <span v-if="detail.movie?.release_date">{{ formatDate(detail.movie.release_date) }}</span>
          </div>

          <div v-if="genres.length" style="display: flex; gap: 6px; flex-wrap: wrap; margin: 12px 0">
            <NuxtLink v-for="g in genres" :key="g" :to="`/genre/${encodeURIComponent(g)}`"><Chip>{{ g }}</Chip></NuxtLink>
          </div>

          <div class="detail-actions">
            <button v-if="playableFileId" class="btn btn-primary" @click="play"><Icon name="play" :size="16" /> Play</button>
            <button v-else class="btn btn-primary" disabled style="opacity: 0.4"><Icon name="play" :size="16" /> No File</button>
            <button class="btn btn-secondary" @click="showListModal = true"><Icon name="plus" :size="16" /> My List</button>
            <button class="btn-icon" :style="{ color: isFavorited ? 'var(--bad)' : 'var(--fg-1)' }" @click="toggleFavorite">
              <Icon :name="isFavorited ? 'heartfill' : 'heart'" :size="20" />
            </button>
            <button class="btn-icon" :style="{ color: isWatched ? 'var(--good)' : 'var(--fg-1)' }" @click="toggleWatched" :title="isWatched ? 'Mark as unwatched' : 'Mark as watched'">
              <Icon name="check" :size="20" />
            </button>
            <button class="btn-icon" title="Edit Metadata" @click="showMetadataEditor = true">
              <Icon name="settings" :size="18" />
            </button>
          </div>

          <MediaSynopsis :text="detail.preferred_overview || detail.media_item.description" />

          <MediaCrewSummary :crew="detail.crew">
            <template #extra>
              <template v-if="detail.production_companies?.length">
                <div class="info-label">Studio</div>
                <div class="info-value">{{ detail.production_companies.map((c: any) => c.name).join(', ') }}</div>
              </template>
              <template v-if="detail.movie?.original_language">
                <div class="info-label">Language</div>
                <div class="info-value">{{ detail.movie.original_language.toUpperCase() }}</div>
              </template>
              <template v-if="detail.movie?.original_title && detail.movie.original_title !== (detail.preferred_title || detail.media_item.title)">
                <div class="info-label">Original Title</div>
                <div class="info-value">{{ detail.movie.original_title }}</div>
              </template>
              <template v-if="detail.movie?.budget">
                <div class="info-label">Budget</div>
                <div class="info-value">${{ formatMoney(detail.movie.budget) }}</div>
              </template>
              <template v-if="detail.movie?.revenue">
                <div class="info-label">Revenue</div>
                <div class="info-value">${{ formatMoney(detail.movie.revenue) }}</div>
              </template>
            </template>
          </MediaCrewSummary>

          <div v-if="detail.collection" class="collection-wrap">
            <NuxtLink :to="`/collection/${detail.collection.id}`" class="collection-badge">
              <Icon name="folder" :size="14" />
              Part of <strong>{{ detail.collection.name }}</strong>
            </NuxtLink>
          </div>

          <MediaKeywords :keywords="detail.keywords" />
        </div>

        <!-- Right column: ratings + audio/subtitle prefs -->
        <div v-if="(detail.external_ratings && detail.external_ratings.length) || (detail.available && playableFileId)" class="hero-side">
          <MediaRatings :ratings="detail.external_ratings" />
          <MediaPlaybackPanel v-if="detail.available && playableFileId" :media-item-id="detail.media_item.id" />
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

      <!-- Extras -->
      <div v-if="groupedExtras.length" class="detail-section">
        <div class="section-row-head">
          <h3 class="section-title-lg">Extras</h3>
        </div>
        <div v-for="group in groupedExtras" :key="group.type" class="extras-group">
          <div class="extras-group-head">
            <div class="extras-group-label">{{ formatExtraType(group.type) }} <span class="tab-count">{{ group.items.length }}</span></div>
            <div class="scroll-controls">
              <template v-if="!extrasExpanded[group.type]">
                <button class="scroll-ctrl-btn" @click="scrollExtras(group.type, 'left')"><Icon name="chevleft" :size="14" /></button>
                <button class="scroll-ctrl-btn" @click="scrollExtras(group.type, 'right')"><Icon name="chevright" :size="14" /></button>
              </template>
              <button class="scroll-ctrl-btn expand" @click="extrasExpanded[group.type] = !extrasExpanded[group.type]">
                <Icon name="chevdown" :size="14" :style="{ transform: extrasExpanded[group.type] ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
              </button>
            </div>
          </div>
          <div :class="extrasExpanded[group.type] ? 'extras-grid' : 'hscroll'" :ref="(el: any) => setExtrasRef(group.type, el)">
            <div v-for="e in group.items" :key="e.id" class="extra-card">
              <div class="extra-thumb">
                <img v-if="e.thumbnail_path" :src="`/api/extras/${e.id}/thumbnail`" alt="" class="extra-thumb-img" loading="lazy" />
                <Icon v-else name="play" :size="20" />
              </div>
              <div class="extra-meta">
                <div class="extra-title">{{ e.title }}</div>
                <div v-if="e.duration_ms" class="extra-sub">{{ formatExtraDuration(e.duration_ms) }}</div>
              </div>
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
import type { MediaDetail, MediaExtra, StreamInfoResponse } from '~~/shared/types'

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const lightbox = useLightbox()

const detail = ref<MediaDetail | null>(null)
const streamInfo = ref<StreamInfoResponse | null>(null)
const loading = ref(true)
const peopleTab = ref<'cast' | 'crew'>('cast')
const castExpanded = ref(false)
const castScrollEl = ref<HTMLElement | null>(null)
const castOverflows = ref(false)
const videoModal = ref<{ key: string; title: string } | null>(null)
const showMetadataEditor = ref(false)
const extrasExpanded = reactive<Record<string, boolean>>({})
const extrasRefs: Record<string, HTMLElement> = {}

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

function setExtrasRef(key: string, el: any) {
  if (el) extrasRefs[key] = el
}

function scrollExtras(key: string, dir: 'left' | 'right') {
  const el = extrasRefs[key]
  if (!el) return
  const amount = el.clientWidth * 0.75
  el.scrollBy({ left: dir === 'left' ? -amount : amount, behavior: 'smooth' })
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

const rating = computed(() => {
  const r = detail.value?.movie?.rating
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

const genres = computed(() => detail.value?.movie?.genres || [])

const crewByDepartment = computed(() => {
  const crew = detail.value?.crew || []
  const depts = new Map<string, any[]>()
  for (const c of crew) {
    const d = c.department || 'Other'
    if (!depts.has(d)) depts.set(d, [])
    depts.get(d)!.push(c)
  }
  const order = ['Directing', 'Writing', 'Production', 'Camera', 'Sound', 'Editing', 'Art', 'Costume & Make-Up', 'Visual Effects', 'Lighting', 'Crew']
  const sorted: { name: string; members: any[] }[] = []
  for (const name of order) {
    if (depts.has(name)) sorted.push({ name, members: depts.get(name)! })
  }
  for (const [name, members] of depts.entries()) {
    if (!order.includes(name)) sorted.push({ name, members })
  }
  return sorted
})

const groupedExtras = computed(() => {
  if (!detail.value?.extras?.length) return []
  const groups: Record<string, MediaExtra[]> = {}
  for (const e of detail.value.extras) {
    if (!groups[e.extra_type]) groups[e.extra_type] = []
    groups[e.extra_type]!.push(e)
  }
  const order = ['trailer', 'teaser', 'behind_the_scenes', 'featurette', 'deleted_scene', 'interview', 'other']
  const result: { type: string; items: MediaExtra[] }[] = []
  for (const t of order) {
    if (groups[t]) result.push({ type: t, items: groups[t]! })
  }
  for (const t of Object.keys(groups)) {
    if (!order.includes(t)) result.push({ type: t, items: groups[t]! })
  }
  return result
})

function formatExtraType(t: string) {
  const map: Record<string, string> = {
    trailer: 'Trailers', behind_the_scenes: 'Behind the Scenes', featurette: 'Featurettes',
    other: 'Other', teaser: 'Teasers', deleted_scene: 'Deleted Scenes', interview: 'Interviews',
  }
  return map[t] || t
}

function formatExtraDuration(ms: number) {
  const total = Math.floor(ms / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

// Play
const playableFileId = computed(() => detail.value?.files?.[0]?.id ?? null)

function play() {
  if (!playableFileId.value || !detail.value) return
  const params = new URLSearchParams({
    media_item_id: String(detail.value.media_item.id),
    title: detail.value.preferred_title || detail.value.media_item.title,
  })
  navigateTo(`/watch/${playableFileId.value}?${params}`)
}

// Favorites / watched
const isFavorited = ref(false)
const isWatched = ref(false)

async function toggleFavorite() {
  if (!detail.value) return
  const { $heya } = useNuxtApp()
  const res = await $heya('/api/me/favorites', {
    method: 'POST',
    body: { entity_type: 'media_item', entity_id: detail.value.media_item.id } as any,
  }) as { favorited: boolean }
  isFavorited.value = res.favorited
}

async function toggleWatched() {
  if (!detail.value) return
  const { $heya } = useNuxtApp()
  await $heya('/api/me/watched/media/{id}', {
    method: 'POST',
    path: { id: detail.value.media_item.id },
    body: { watched: !isWatched.value } as any,
  })
  isWatched.value = !isWatched.value
}

async function loadState() {
  if (!detail.value) return
  try {
    const st = await fetchUserState('movies')
    isFavorited.value = st.favorited.includes(detail.value.media_item.id)
    isWatched.value = st.watched.includes(detail.value.media_item.id)
  } catch { /* empty */ }
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
    const { $heya } = useNuxtApp()
    userLists.value = await $heya('/api/me/lists', {
      query: { media_item_id: detail.value.media_item.id },
    }) as any[]
  } catch { /* empty */ }
}

async function createList() {
  if (!newListName.value.trim()) return
  const { $heya } = useNuxtApp()
  await $heya('/api/me/lists', {
    method: 'POST',
    body: { name: newListName.value.trim(), description: newListDesc.value.trim() } as any,
  })
  newListName.value = ''
  newListDesc.value = ''
  showCreateList.value = false
  await loadLists()
}

async function toggleListItem(l: any) {
  if (!detail.value) return
  const { $heya } = useNuxtApp()
  if (l.contains) {
    await $heya('/api/me/lists/{id}/items/{media_id}', {
      method: 'DELETE',
      path: { id: l.id, media_id: detail.value.media_item.id },
    })
  } else {
    await $heya('/api/me/lists/{id}/items', {
      method: 'POST',
      path: { id: l.id },
      body: { media_item_id: detail.value.media_item.id } as any,
    })
  }
  await loadLists()
}

watch(showListModal, (v) => { if (v) loadLists() })

function formatDate(d: string) {
  if (!d) return ''
  try { return new Date(d + 'T00:00:00').toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' }) }
  catch { return d }
}

function formatRuntime(mins: number) {
  if (!mins) return ''
  const h = Math.floor(mins / 60)
  const m = mins % 60
  if (h === 0) return `${m}m`
  if (m === 0) return `${h}h`
  return `${h}h ${m}m`
}

function formatMoney(n: number) {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(2)}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`
  return n.toLocaleString()
}

function formatVote(v: any) {
  const n = typeof v === 'number' ? v : parseFloat(String(v))
  return isNaN(n) ? '' : n.toFixed(1)
}

async function loadStreamInfo() {
  if (!playableFileId.value) return
  try {
    const caps = useClientCaps()
    const capsQuery = capsToQueryString(caps)
    const url = `/api/stream/${playableFileId.value}/info${capsQuery ? `?${capsQuery}` : ''}`
    const token = useAuth().token.value
    streamInfo.value = await $fetch<StreamInfoResponse>(url, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
  } catch { /* empty */ }
}

onMounted(async () => {
  try {
    const { $heya } = useNuxtApp()
    detail.value = await $heya('/api/media/{id}', {
      path: { id: slug.value as any },
    }) as MediaDetail
    await nextTick()
    backdropA.value = getBackdropUrl(0)
    backdropB.value = getBackdropUrl(0)
    if (backdropAssets.value.length > 1) {
      startCarouselTimer()
    }
    loadState()
    loadStreamInfo()
    checkCastOverflow()
    checkRecsOverflow()
  } catch { navigateTo('/movies') }
  loading.value = false
})

onUnmounted(() => { if (bdTimeout) clearTimeout(bdTimeout) })
</script>

<style scoped>
/* Hero — matches TV detail page */
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
.hero-left { display: flex; flex-direction: column; gap: 14px; align-self: start; min-width: 0; }
.hero-poster { position: relative; }
.hero-info { display: flex; flex-direction: column; justify-content: center; min-width: 0; }
.hero-side { display: flex; flex-direction: column; gap: 14px; align-self: start; min-width: 0; }

.detail-badges { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px; }
.detail-title { font-size: 44px; font-weight: 600; letter-spacing: -0.025em; line-height: 1.05; margin: 0 0 4px; }
.detail-tagline { font-style: italic; color: var(--fg-2); font-size: 15px; margin: 4px 0 8px; }
.hero-meta-row { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg-2); margin-top: 8px; }
.dot { width: 3px; height: 3px; border-radius: 50%; background: var(--fg-3); }
.detail-actions { display: flex; align-items: center; gap: 10px; margin: 16px 0; }
.btn-icon { background: none; border: none; cursor: pointer; padding: 4px; }

.collection-wrap { margin-top: 16px; }
.collection-badge {
  display: inline-flex; align-items: center; gap: 8px;
  font-size: 12px; color: var(--fg-2); padding: 6px 14px;
  border-radius: var(--r-md); border: 1px solid var(--border);
  transition: all 0.15s;
}
.collection-badge:hover { background: var(--gold-soft); color: var(--gold); border-color: transparent; }
.collection-badge strong { color: var(--fg-0); font-weight: 500; }
.collection-badge:hover strong { color: var(--gold); }

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
.zoom-btn.round { border-radius: 50%; top: 2px; right: 2px; width: 20px; height: 20px; }
.zoom-btn.crew-zoom { top: 0; right: 0; width: 16px; height: 16px; }
.hero-poster:hover .zoom-btn,
.cast-photo-wrap:hover .zoom-btn,
.crew-photo-wrap:hover .zoom-btn { opacity: 1; }

/* Cast / crew photo wrap */
.cast-photo-wrap { position: relative; width: 90px; height: 90px; border-radius: 50%; overflow: hidden; margin: 0 auto; }
.crew-photo-wrap { position: relative; width: 38px; height: 38px; border-radius: 50%; overflow: hidden; flex-shrink: 0; }

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
.cast-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(110px, 1fr)); gap: 16px; }
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

/* Extras */
.extras-group { margin-top: 18px; }
.extras-group-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
.extras-group-label { font-size: 11px; font-weight: 700; font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.08em; color: var(--fg-2); }
.extra-card {
  display: flex; align-items: center; gap: 12px;
  padding: 10px; min-width: 280px; flex-shrink: 0;
  background: var(--bg-2); border: 1px solid var(--border);
  border-radius: var(--r-md); transition: background 0.12s;
}
.extra-card:hover { background: var(--bg-3); }
.extra-thumb { width: 86px; height: 48px; border-radius: var(--r-xs); background: var(--bg-4); display: flex; align-items: center; justify-content: center; color: var(--fg-2); flex-shrink: 0; overflow: hidden; }
.extra-thumb-img { width: 100%; height: 100%; object-fit: cover; }
.extra-meta { min-width: 0; }
.extra-title { font-size: 12px; font-weight: 500; color: var(--fg-0); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.extra-sub { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); margin-top: 2px; }
.extras-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px; }
.extras-grid .extra-card { min-width: 0; }

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
  .hero-ratings { grid-template-columns: repeat(4, minmax(0, 1fr)); }
}

@media (max-width: 900px) {
  .hero-content { grid-template-columns: 1fr; gap: 20px; padding: 32px 20px 24px; }
  .hero-poster { max-width: 200px; }
  .hero-left { flex-direction: column; }
  .hero-ratings { grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); }
  .detail-title { font-size: 32px; }
  .detail-body-below { padding: 0 20px 60px; }
  .bd-indicators { right: 20px; bottom: 16px; }
  .hero-expand { display: none; }
}
</style>
