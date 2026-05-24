<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div style="height: 380px; background: var(--bg-2)" />
  </div>

  <div v-else-if="detail" class="scroll" style="height: 100%">
    <!-- Hero: backdrop + poster + info merged -->
    <div class="hero-section">
      <div class="hero-bg">
        <img
          v-if="backdropA"
          :src="backdropA"
          class="hero-bg-img"
          :class="{ visible: showA }"
          @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'"
        />
        <img
          v-if="backdropB"
          :src="backdropB"
          class="hero-bg-img"
          :class="{ visible: !showA }"
          @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'"
        />
        <div class="hero-bg-fade" />
      </div>

      <div class="hero-content">
        <div class="hero-poster">
          <Poster :idx="0" :src="usePosterUrl(detail.media_item.id)" :title="detail.media_item.title" aspect="2/3" />
          <button class="zoom-btn" @click="openPosterLightbox"><Icon name="expand" :size="14" /></button>
        </div>

        <div class="hero-info">
          <div class="detail-badges">
            <Chip gold>{{ mediaTypeLabel(detail.media_item.media_type) }}</Chip>
            <Chip v-if="certification">{{ certification }}</Chip>
            <Chip v-if="detail.media_item.year">{{ detail.media_item.year }}</Chip>
            <Chip v-if="detail.movie?.runtime_minutes">{{ Math.floor(detail.movie.runtime_minutes / 60) }}h {{ detail.movie.runtime_minutes % 60 }}m</Chip>
            <Chip v-if="detail.tv_series?.status">{{ detail.tv_series.status }}</Chip>
          </div>

          <h1 class="detail-title">{{ detail.preferred_title || detail.media_item.title }}</h1>
          <p v-if="detail.movie?.tagline" class="detail-tagline">{{ detail.movie.tagline }}</p>

          <div class="hero-meta-row" v-if="rating">
            <Icon name="star" :size="14" style="color: var(--gold)" />
            <span style="color: var(--gold)">{{ rating }}/10</span>
          </div>

          <div v-if="detail.external_ratings?.length" class="ratings-row">
            <div v-for="er in detail.external_ratings" :key="er.source" class="ext-rating">
              <span class="ext-rating-source">{{ ratingSourceLabel(er.source) }}</span>
              <span class="ext-rating-value">{{ er.value }}</span>
            </div>
          </div>

          <div v-if="genres.length" style="display: flex; gap: 6px; flex-wrap: wrap; margin: 12px 0">
            <NuxtLink v-for="g in genres" :key="g" :to="`/genre/${encodeURIComponent(g)}`"><Chip>{{ g }}</Chip></NuxtLink>
          </div>

          <div class="detail-actions">
            <button v-if="playableFileId" class="btn btn-primary" @click="navigateToPlayer"><Icon name="play" :size="16" /> Play</button>
            <button v-else class="btn btn-primary" disabled style="opacity: 0.4"><Icon name="play" :size="16" /> No File</button>
            <button class="btn btn-secondary" @click="showListModal = true"><Icon name="plus" :size="16" /> My List</button>
            <button class="btn-icon" :style="{ color: isFavorited ? 'var(--bad)' : 'var(--fg-1)' }" @click="toggleFavorite">
              <Icon :name="isFavorited ? 'heartfill' : 'heart'" :size="20" />
            </button>
            <button class="btn-icon" :style="{ color: isWatched ? 'var(--good)' : 'var(--fg-1)' }" @click="toggleWatched">
              <Icon name="check" :size="20" />
            </button>
            <button class="btn-icon" title="Edit Metadata" @click="showMetadataEditor = true">
              <Icon name="settings" :size="18" />
            </button>
          </div>

          <!-- Playback preferences (audio/subtitle language selection) -->
          <PlaybackPrefs v-if="detail.available" :media-item-id="detail.media_item.id" />

          <p v-if="detail.preferred_overview || detail.media_item.description" class="detail-synopsis">{{ detail.preferred_overview || detail.media_item.description }}</p>

          <!-- Inline crew summary + keywords + media info -->
          <div class="info-grid">
            <template v-for="c in crewSummary" :key="c.label">
              <div class="info-label">{{ c.label }}</div>
              <div class="info-value">{{ c.value }}</div>
            </template>
            <template v-if="detail.production_companies?.length">
              <div class="info-label">Studio</div>
              <div class="info-value">{{ detail.production_companies.map(c => c.name).join(', ') }}</div>
            </template>
            <template v-if="detail.movie?.original_language">
              <div class="info-label">Language</div>
              <div class="info-value">{{ detail.movie.original_language.toUpperCase() }}</div>
            </template>
            <template v-if="detail.movie?.budget">
              <div class="info-label">Budget</div>
              <div class="info-value">${{ (detail.movie.budget / 1_000_000).toFixed(0) }}M</div>
            </template>
            <template v-if="detail.movie?.revenue">
              <div class="info-label">Revenue</div>
              <div class="info-value">${{ (detail.movie.revenue / 1_000_000).toFixed(0) }}M</div>
            </template>
          </div>

          <div v-if="detail.collection" class="collection-link" style="margin-top: 16px">
            <NuxtLink :to="`/collection/${detail.collection.id}`" class="collection-badge">
              <Icon name="folder" :size="14" />
              Part of <strong>{{ detail.collection.name }}</strong>
            </NuxtLink>
          </div>

          <div v-if="detail.keywords?.length" style="display: flex; gap: 5px; flex-wrap: wrap; margin-top: 16px">
            <NuxtLink v-for="k in detail.keywords" :key="k.id" :to="`/keyword/${encodeURIComponent(k.name)}`" class="keyword-tag">{{ k.name }}</NuxtLink>
          </div>
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
      <!-- Cast & Crew (tabbed) -->
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
          <div v-if="peopleTab === 'cast'" style="display: flex; gap: 8px">
            <button class="scroll-arrow" @click="scrollEl('castScroll', -1)"><Icon name="chevleft" :size="16" /></button>
            <button class="scroll-arrow" @click="scrollEl('castScroll', 1)"><Icon name="chevright" :size="16" /></button>
          </div>
        </div>

        <div v-if="peopleTab === 'cast'" class="hscroll" ref="castScroll" style="margin-top: 16px">
          <NuxtLink
            v-for="c in detail.cast"
            :key="c.id"
            :to="personUrl(c)"
            class="cast-card"
          >
            <div v-if="c.profile_path && !c.profile_path.startsWith('http')" class="cast-photo-wrap">
              <img :src="`/api/person/${c.id}/image`" class="cast-photo" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
              <button class="zoom-btn round" @click.stop.prevent="lightbox.open(`/api/person/${c.id}/image`)"><Icon name="expand" :size="10" /></button>
            </div>
            <div v-else class="cast-avatar">{{ c.name.split(' ').map((n: string) => n[0]).join('').slice(0, 2) }}</div>
            <div class="cast-name">{{ c.name }}</div>
            <div class="cast-role">{{ c.character }}</div>
          </NuxtLink>
        </div>

        <div v-if="peopleTab === 'crew'" style="margin-top: 16px">
          <div v-for="dept in crewByDepartment" :key="dept.name" style="margin-bottom: 24px">
            <div class="section-title" style="font-size: 11px; margin-bottom: 10px">{{ dept.name }}</div>
            <div class="crew-dept-grid">
              <NuxtLink
                v-for="c in dept.members"
                :key="`${c.id}-${c.job}`"
                :to="personUrl(c)"
                class="crew-card"
              >
                <div v-if="c.profile_path && !c.profile_path.startsWith('http')" class="crew-photo-wrap">
                  <img :src="`/api/person/${c.id}/image`" class="crew-photo" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
                  <button class="zoom-btn round crew-zoom" @click.stop.prevent="lightbox.open(`/api/person/${c.id}/image`)"><Icon name="expand" :size="8" /></button>
                </div>
                <div v-else class="crew-initials">{{ c.name.split(' ').map((n: string) => n[0]).join('').slice(0, 2) }}</div>
                <div>
                  <div class="crew-name">{{ c.name }}</div>
                  <div class="crew-job">{{ c.job }}</div>
                </div>
              </NuxtLink>
            </div>
          </div>
        </div>
      </div>

      <!-- Content tabs: Videos / Extras / Seasons -->
      <div v-if="contentTabs.length" class="detail-section">
        <div class="section-row-head" style="margin-bottom: 0">
          <div class="tab-bar" style="margin-bottom: 0">
            <button
              v-for="t in contentTabs"
              :key="t.id"
              class="tab-btn"
              :class="{ active: contentTab === t.id }"
              @click="contentTab = t.id"
            >
              {{ t.label }} <span class="tab-count">{{ t.count }}</span>
            </button>
          </div>
          <div v-if="contentTab === 'videos'" style="display: flex; gap: 8px">
            <button class="scroll-arrow" @click="scrollContentLeft"><Icon name="chevleft" :size="16" /></button>
            <button class="scroll-arrow" @click="scrollContentRight"><Icon name="chevright" :size="16" /></button>
            <button class="scroll-arrow" @click="contentExpanded = !contentExpanded">
              <Icon name="chevdown" :size="16" :style="{ transform: contentExpanded ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
            </button>
          </div>
        </div>

        <div v-if="contentTab === 'videos'" style="margin-top: 16px">
          <div :class="contentExpanded ? 'expanded-grid videos-expanded' : 'hscroll'" ref="videosScroll">
            <a
              v-for="v in detail.videos"
              :key="v.id"
              :href="`https://www.youtube.com/watch?v=${v.video_key}`"
              target="_blank"
              class="video-card"
            >
              <div class="video-thumb">
                <img :src="`https://img.youtube.com/vi/${v.video_key}/mqdefault.jpg`" @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'" />
                <div class="video-play"><Icon name="play" :size="20" /></div>
              </div>
              <div class="video-name">{{ v.name }}</div>
              <div class="video-type">{{ v.video_type }}</div>
            </a>
          </div>
        </div>

        <div v-if="contentTab === 'extras'" style="margin-top: 16px">
          <div v-for="group in groupedExtras" :key="group.type" style="margin-bottom: 20px">
            <div class="section-row-head" style="margin-bottom: 8px">
              <div class="section-title" style="font-size: 11px">{{ formatExtraType(group.type) }} ({{ group.items.length }})</div>
              <div style="display: flex; gap: 6px">
                <template v-if="!extrasExpanded[group.type]">
                  <button class="scroll-arrow" @click="scrollEl(`extras-${group.type}`, -1)"><Icon name="chevleft" :size="14" /></button>
                  <button class="scroll-arrow" @click="scrollEl(`extras-${group.type}`, 1)"><Icon name="chevright" :size="14" /></button>
                </template>
                <button class="scroll-arrow" @click="extrasExpanded[group.type] = !extrasExpanded[group.type]">
                  <Icon name="chevdown" :size="14" :style="{ transform: extrasExpanded[group.type] ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
                </button>
              </div>
            </div>
            <div :class="extrasExpanded[group.type] ? 'fold-grid extras-expanded' : 'hscroll'" :ref="(el: any) => setScrollRef(`extras-${group.type}`, el)">
              <div v-for="e in group.items" :key="e.id" class="extra-card">
                <div class="extra-thumb">
                  <img v-if="e.thumbnail_path" :src="`/api/extras/${e.id}/thumbnail`" alt="" class="extra-thumb-img" loading="lazy" />
                  <Icon v-else name="play" :size="20" />
                </div>
                <div class="extra-title">{{ e.title }}</div>
              </div>
            </div>
          </div>
        </div>

        <div v-if="contentTab === 'seasons'" style="display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 16px; margin-top: 16px">
          <div v-for="s in detail.seasons" :key="s.id" class="card-tile">
            <Poster :idx="s.season_number" aspect="2/3" :title="s.title" />
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ s.title }}</div>
              <div class="grid-tile-sub">{{ s.aired_episodes }} episodes</div>
            </div>
          </div>
        </div>
      </div>

      <!-- More Like This (horizontal scroll) -->
      <div v-if="detail.recommendations?.length" class="detail-section">
        <div class="section-row-head">
          <h3 class="section-title-lg">More Like This</h3>
          <div style="display: flex; gap: 8px">
            <button class="scroll-arrow" @click="scrollRecs(-1)"><Icon name="chevleft" :size="16" /></button>
            <button class="scroll-arrow" @click="scrollRecs(1)"><Icon name="chevright" :size="16" /></button>
          </div>
        </div>
        <div class="rec-scroll" ref="recScrollEl">
          <component
            v-for="r in detail.recommendations"
            :key="r.id"
            :is="r.local_media_item_id ? 'NuxtLink' : 'div'"
            :to="r.local_media_item_id ? mediaUrl({ id: r.local_media_item_id, title: r.title, media_type: r.media_type } as any) : undefined"
            class="rec-tile"
            :class="{ dimmed: !r.local_media_item_id }"
          >
            <Poster
              :idx="r.id"
              :src="recPosterUrl(r)"
              aspect="2/3"
              :title="r.title"
            />
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ r.title }}</div>
              <div class="grid-tile-sub">
                {{ r.release_date?.slice(0, 4) || '?' }}
                <span v-if="r.local_media_item_id" class="rec-in-library">In library</span>
              </div>
            </div>
          </component>
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
                <button v-for="l in userLists" :key="l.id" class="list-option" :class="{ active: l.contains }" @click="toggleListItem(l)">
                  <Icon :name="l.contains ? 'check' : 'plus'" :size="14" />
                  <span>{{ l.name }}</span>
                  <span class="list-option-count">{{ l.item_count }}</span>
                </button>
                <div v-if="!userLists.length" style="padding: 16px 0; color: var(--fg-3); font-size: 13px; text-align: center">No lists yet</div>
                <button class="list-create-btn" @click="showCreateList = true"><Icon name="plus" :size="14" /> Create new list</button>
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
  </div>

  <div v-else class="scroll" style="height: 100%; display: flex; align-items: center; justify-content: center">
    <div style="text-align: center; color: var(--fg-2)">
      <p style="font-size: 18px">Media not found</p>
      <button class="btn btn-secondary" style="margin-top: 16px" @click="$router.back()">Go back</button>
    </div>
  </div>

  <MetadataEditorModal
    v-if="detail"
    :media-id="detail.media_item.id"
    :show="showMetadataEditor"
    @close="showMetadataEditor = false"
  />
</template>

<script setup lang="ts">
import type { MediaDetail, MediaExtra } from '~~/shared/types'

const props = defineProps<{ mediaId: number }>()
const lightbox = useLightbox()

const detail = ref<MediaDetail | null>(null)
const loading = ref(true)
const backdropIdx = ref(0)
const peopleTab = ref<'cast' | 'crew'>('cast')
const contentTab = ref('')
const contentExpanded = ref(false)

// Favorites
const isFavorited = ref(false)
async function toggleFavorite() {
  if (!detail.value) return
  const res = await apiFetch<{ favorited: boolean }>('/api/me/favorites', {
    method: 'POST', body: JSON.stringify({ entity_type: 'media_item', entity_id: detail.value.media_item.id }),
  })
  isFavorited.value = res.favorited
}

// Watched (for movies)
const isWatched = ref(false)
async function toggleWatched() {
  if (!detail.value) return
  await apiFetch(`/api/me/watched/media/${detail.value.media_item.id}`, { method: 'POST', body: JSON.stringify({ watched: !isWatched.value }) })
  isWatched.value = !isWatched.value
}

// Lists
const showListModal = ref(false)
const showCreateList = ref(false)
const showMetadataEditor = ref(false)
const newListName = ref('')
const newListDesc = ref('')
const userLists = ref<any[]>([])

async function loadLists() {
  if (!detail.value) return
  try { userLists.value = await apiFetch<any[]>(`/api/me/lists?media_item_id=${detail.value.media_item.id}`) } catch { /* empty */ }
}
async function createList() {
  if (!newListName.value.trim()) return
  await apiFetch('/api/me/lists', { method: 'POST', body: JSON.stringify({ name: newListName.value.trim(), description: newListDesc.value.trim() }) })
  newListName.value = ''; newListDesc.value = ''; showCreateList.value = false
  await loadLists()
}
async function toggleListItem(l: any) {
  if (!detail.value) return
  if (l.contains) { await apiFetch(`/api/me/lists/${l.id}/items/${detail.value.media_item.id}`, { method: 'DELETE' }) }
  else { await apiFetch(`/api/me/lists/${l.id}/items`, { method: 'POST', body: JSON.stringify({ media_item_id: detail.value.media_item.id }) }) }
  await loadLists()
}
watch(showListModal, (v) => { if (v) loadLists() })
const extrasExpanded = reactive<Record<string, boolean>>({})
const recScrollEl = ref<HTMLElement>()
const videosScroll = ref<HTMLElement>()
const castScroll = ref<HTMLElement>()
const scrollRefs: Record<string, HTMLElement> = {}

// Carousel state
const carouselPaused = ref(false)
const BACKDROP_INTERVAL = 8000
let bdTimeout: ReturnType<typeof setTimeout> | null = null
let bdStart = 0
let bdRemaining = BACKDROP_INTERVAL

function setScrollRef(key: string, el: any) {
  if (el) scrollRefs[key] = el
}

function scrollEl(refName: string, dir: number) {
  let el: HTMLElement | undefined
  if (refName === 'videosScroll') el = videosScroll.value
  else if (refName === 'castScroll') el = castScroll.value
  else el = scrollRefs[refName]
  el?.scrollBy({ left: dir * 500, behavior: 'smooth' })
}

function scrollContentLeft() { scrollActiveContent(-1) }
function scrollContentRight() { scrollActiveContent(1) }

function scrollActiveContent(dir: number) {
  if (contentTab.value === 'videos') {
    videosScroll.value?.scrollBy({ left: dir * 500, behavior: 'smooth' })
  } else if (contentTab.value === 'extras') {
    const firstKey = Object.keys(scrollRefs).find(k => k.startsWith('extras-'))
    if (firstKey) scrollRefs[firstKey]?.scrollBy({ left: dir * 500, behavior: 'smooth' })
  }
}

const backdropAssets = computed(() => {
  if (!detail.value?.assets) return []
  const seen = new Set<number>()
  return detail.value.assets
    .filter(a => a.asset_type === 'backdrop')
    .sort((a, b) => a.sort_order - b.sort_order)
    .filter(a => {
      if (seen.has(a.sort_order)) return false
      seen.add(a.sort_order)
      return true
    })
})

const showA = ref(true)
const backdropA = ref<string | null>(null)
const backdropB = ref<string | null>(null)

function getBackdropUrl(idx: number) {
  if (backdropAssets.value.length > 0) {
    const asset = backdropAssets.value[idx % backdropAssets.value.length]
    if (!asset) return null
    return `/api/media/${detail.value?.media_item.id}/image/backdrop?sort=${asset.sort_order}`
  }
  return detail.value ? useBackdropUrl(detail.value.media_item.id) : null
}

function advanceBackdrop() {
  if (backdropAssets.value.length <= 1) return
  backdropIdx.value = (backdropIdx.value + 1) % backdropAssets.value.length
  const url = getBackdropUrl(backdropIdx.value)
  if (showA.value) { backdropB.value = url } else { backdropA.value = url }
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

// Lightbox
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

const playableFileId = computed(() => detail.value?.files?.[0]?.id)

function navigateToPlayer() {
  if (!playableFileId.value || !detail.value) return
  const params = new URLSearchParams({
    media_item_id: String(detail.value.media_item.id),
    title: detail.value.media_item.title,
  })
  navigateTo(`/watch/${playableFileId.value}?${params}`)
}

const genres = computed(() => detail.value?.movie?.genres || detail.value?.tv_series?.genres || detail.value?.book?.genres || [])

const rating = computed(() => {
  const r = detail.value?.movie?.rating || detail.value?.tv_series?.rating || detail.value?.book?.rating
  return r ? parseFloat(String(r)).toFixed(1) : ''
})

const certification = computed(() => {
  if (detail.value?.preferred_certification) return detail.value.preferred_certification
  if (!detail.value?.certifications?.length) return ''
  const us = detail.value.certifications.find(c => c.country === 'US' && c.certification)
  return us?.certification || detail.value.certifications.find(c => c.certification)?.certification || ''
})

const crewSummary = computed(() => {
  if (!detail.value?.crew?.length) return []
  const byJob: Record<string, string[]> = {}
  for (const c of detail.value.crew) {
    if (['Director', 'Screenplay', 'Writer', 'Producer', 'Original Music Composer', 'Director of Photography'].includes(c.job)) {
      const list = byJob[c.job] ?? (byJob[c.job] = [])
      if (!list.includes(c.name)) list.push(c.name)
    }
  }
  const order = ['Director', 'Screenplay', 'Producer', 'Original Music Composer', 'Director of Photography']
  const labels: Record<string, string> = {
    Director: 'Director', Screenplay: 'Writer', Writer: 'Writer', Producer: 'Producer',
    'Original Music Composer': 'Music', 'Director of Photography': 'Cinematography',
  }
  return order
    .map(j => ({ job: j, names: byJob[j] }))
    .filter((r): r is { job: string; names: string[] } => !!r.names)
    .map(r => ({ label: labels[r.job] || r.job, value: r.names.slice(0, 3).join(', ') }))
})

type CrewMember = NonNullable<MediaDetail['crew']>[number]
const crewByDepartment = computed(() => {
  if (!detail.value?.crew?.length) return []
  const depts: Record<string, CrewMember[]> = {}
  for (const c of detail.value.crew) {
    const d = c.department || 'Other'
    const list = depts[d] ?? (depts[d] = [])
    list.push(c)
  }
  const order = ['Directing', 'Writing', 'Production', 'Camera', 'Sound', 'Editing', 'Art', 'Costume & Make-Up', 'Visual Effects', 'Lighting', 'Crew']
  const sorted: { name: string; members: CrewMember[] }[] = []
  for (const d of order) {
    const members = depts[d]
    if (members) sorted.push({ name: d, members })
  }
  for (const d of Object.keys(depts)) {
    if (!order.includes(d)) {
      const members = depts[d]
      if (members) sorted.push({ name: d, members })
    }
  }
  return sorted
})

const contentTabs = computed(() => {
  const tabs: { id: string; label: string; count: number }[] = []
  if (detail.value?.videos?.length) tabs.push({ id: 'videos', label: 'Videos', count: detail.value.videos.length })
  if (detail.value?.extras?.length) tabs.push({ id: 'extras', label: 'Extras', count: detail.value.extras.length })
  if (detail.value?.seasons?.length) tabs.push({ id: 'seasons', label: 'Seasons', count: detail.value.seasons.length })
  return tabs
})

const groupedExtras = computed(() => {
  if (!detail.value?.extras?.length) return []
  const groups: Record<string, MediaExtra[]> = {}
  for (const e of detail.value.extras) {
    const list = groups[e.extra_type] ?? (groups[e.extra_type] = [])
    list.push(e)
  }
  const order = ['trailer', 'behind_the_scenes', 'featurette', 'other', 'teaser', 'deleted_scene', 'interview']
  return order
    .map(t => ({ type: t, items: groups[t] }))
    .filter((g): g is { type: string; items: MediaExtra[] } => !!g.items)
})

function formatExtraType(t: string) {
  return ({ trailer: 'Trailers', behind_the_scenes: 'Behind the Scenes', featurette: 'Featurettes', other: 'Other', teaser: 'Teasers', deleted_scene: 'Deleted Scenes', interview: 'Interviews' } as Record<string, string>)[t] || t
}

function scrollRecs(dir: number) {
  recScrollEl.value?.scrollBy({ left: dir * 500, behavior: 'smooth' })
}

const RATING_LABELS: Record<string, string> = {
  imdb: 'IMDb',
  rottentomatoes: 'Rotten Tomatoes',
  metacritic: 'Metacritic',
  tmdb: 'TMDB',
  letterboxd: 'Letterboxd',
  trakt: 'Trakt',
}

function ratingSourceLabel(source: string): string {
  return RATING_LABELS[source] || source.charAt(0).toUpperCase() + source.slice(1)
}

function recPosterUrl(r: any): string {
  if (r.local_media_item_id) return usePosterUrl(r.local_media_item_id) ?? ''
  if (r.poster_path) return `/api/tmdb/image${r.poster_path}?size=w342`
  return ''
}

onMounted(async () => {
  try {
    detail.value = await apiFetch<MediaDetail>(`/api/media/${props.mediaId}`)
  } catch { /* empty */ }
  loading.value = false

  const first = contentTabs.value[0]
  if (first) contentTab.value = first.id

  backdropA.value = getBackdropUrl(0)

  if (backdropAssets.value.length > 1) {
    backdropB.value = getBackdropUrl(1)
    startCarouselTimer()
  }

  if (detail.value) {
    const res = await apiFetch<{ favorited: boolean }>(`/api/me/favorites/check?entity_type=media_item&entity_id=${detail.value.media_item.id}`)
    isFavorited.value = res.favorited
  }
})

onUnmounted(() => { if (bdTimeout) clearTimeout(bdTimeout) })
</script>

<style scoped>
.hero-section { position: relative; min-height: 520px; }
.hero-bg { position: absolute; inset: 0; }
.hero-bg-img {
  position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover;
  opacity: 0; transition: opacity 1.8s ease-in-out;
}
.hero-bg-img.visible { opacity: 1; }
.hero-bg-fade {
  position: absolute; inset: 0;
  background:
    linear-gradient(to right, var(--bg-1) 0%, rgba(12,12,16,0.7) 40%, rgba(12,12,16,0.4) 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 50%);
}
.hero-content {
  position: relative; z-index: 2;
  display: grid; grid-template-columns: 240px 1fr;
  gap: 40px; padding: 40px 40px 48px; max-width: 1300px;
}
.hero-poster {
  position: relative;
  box-shadow: 0 24px 60px rgba(0,0,0,0.6), 0 0 0 1px rgba(255,255,255,0.06);
  border-radius: var(--r-md); overflow: hidden; align-self: start;
}
.hero-info { display: flex; flex-direction: column; justify-content: center; }
.detail-title { font-size: 44px; font-weight: 600; letter-spacing: -0.025em; line-height: 1.05; margin: 0 0 4px; }
.detail-tagline { font-style: italic; color: var(--fg-2); font-size: 15px; margin: 4px 0 12px; }
.detail-synopsis { font-size: 14px; line-height: 1.65; color: var(--fg-1); max-width: 640px; margin: 12px 0 0; }
.ratings-row {
  display: flex; gap: 12px; flex-wrap: wrap; margin-top: 8px;
}
.ext-rating {
  display: flex; align-items: center; gap: 6px;
  padding: 4px 10px; border-radius: var(--r-sm);
  background: rgba(255,255,255,0.04); border: 1px solid var(--border);
  font-size: 12px;
}
.ext-rating-source {
  font-family: var(--font-mono); font-weight: 600; color: var(--fg-3);
  font-size: 10px; text-transform: uppercase; letter-spacing: 0.04em;
}
.ext-rating-value { color: var(--fg-0); font-weight: 600; }

.detail-actions { display: flex; align-items: center; gap: 10px; margin: 16px 0; }

.info-grid { display: grid; grid-template-columns: auto 1fr; gap: 4px 20px; margin-top: 16px; max-width: 500px; }
.info-label { font-size: 11px; font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.08em; color: var(--fg-3); padding-top: 2px; }
.info-value { font-size: 13px; color: var(--fg-1); line-height: 1.5; }

.keyword-tag {
  font-size: 10px; font-family: var(--font-mono); padding: 3px 8px; border-radius: 999px;
  background: rgba(255,255,255,0.04); border: 1px solid var(--border); color: var(--fg-2); letter-spacing: 0.02em;
  transition: background 0.15s, color 0.15s;
}
.keyword-tag:hover { background: var(--gold-soft); color: var(--gold); border-color: transparent; }

.collection-badge {
  display: inline-flex; align-items: center; gap: 8px;
  font-size: 12px; color: var(--fg-2); padding: 6px 14px;
  border-radius: var(--r-md); border: 1px solid var(--border);
  transition: all 0.15s;
}
.collection-badge:hover { background: var(--gold-soft); color: var(--gold); border-color: transparent; }
.collection-badge strong { color: var(--fg-0); font-weight: 500; }
.collection-badge:hover strong { color: var(--gold); }

/* Modal */
.modal-overlay { position: fixed; inset: 0; z-index: 9000; background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center; }
.modal-card { background: var(--bg-2); border: 1px solid var(--border-strong); border-radius: var(--r-lg); width: 380px; max-width: 90vw; max-height: 80vh; overflow: auto; }
.modal-header { display: flex; align-items: center; justify-content: space-between; padding: 16px 20px; border-bottom: 1px solid var(--border); }
.modal-header h3 { font-size: 16px; font-weight: 600; margin: 0; }
.modal-body { padding: 12px 20px 20px; }
.modal-input { width: 100%; padding: 10px 14px; background: var(--bg-3); border: 1px solid var(--border); border-radius: var(--r-md); color: var(--fg-0); font-size: 14px; outline: none; }
.modal-input:focus { border-color: var(--gold); }
.list-option { display: flex; align-items: center; gap: 10px; width: 100%; padding: 10px 12px; border-radius: var(--r-sm); font-size: 13px; color: var(--fg-1); transition: background 0.12s; text-align: left; }
.list-option:hover { background: rgba(255,255,255,0.04); }
.list-option.active { color: var(--gold); }
.list-option-count { margin-left: auto; font-size: 10px; font-family: var(--font-mono); color: var(--fg-4); }
.list-create-btn { display: flex; align-items: center; gap: 8px; width: 100%; padding: 10px 12px; margin-top: 4px; border-top: 1px solid var(--border); font-size: 13px; color: var(--fg-2); transition: color 0.12s; }
.list-create-btn:hover { color: var(--gold); }
.modal-enter-active, .modal-leave-active { transition: opacity 0.15s; }
.modal-enter-from, .modal-leave-to { opacity: 0; }

/* Backdrop indicators */
.bd-indicators {
  position: absolute; bottom: 24px; right: 48px; z-index: 4;
  display: flex; gap: 5px;
}
.bd-bar {
  width: 28px; height: 3px; border-radius: 2px;
  background: rgba(255,255,255,0.2); position: relative; overflow: hidden;
  cursor: pointer; transition: background 0.15s;
}
.bd-bar:hover { background: rgba(255,255,255,0.4); }
.bd-bar.active { background: rgba(255,255,255,0.12); }
.bd-bar.active::after {
  content: ''; position: absolute; left: 0; top: 0; bottom: 0;
  background: var(--gold); border-radius: 2px;
  animation: bd-fill 8s linear forwards;
}
.bd-bar.paused::after { animation-play-state: paused; }
@keyframes bd-fill { from { width: 0; } to { width: 100%; } }

/* Expand button */
.hero-expand {
  position: absolute; bottom: 24px; right: 16px; z-index: 4;
  width: 30px; height: 30px; border-radius: var(--r-sm);
  background: rgba(0,0,0,0.4); border: 1px solid rgba(255,255,255,0.1);
  color: rgba(255,255,255,0.6); display: flex; align-items: center; justify-content: center;
  cursor: pointer; transition: all 0.15s; opacity: 0;
}
.hero-section:hover .hero-expand { opacity: 1; }
.hero-expand:hover { background: rgba(0,0,0,0.6); color: #fff; }

/* Zoom button on images */
.zoom-btn {
  position: absolute; top: 8px; right: 8px;
  width: 28px; height: 28px; border-radius: var(--r-sm);
  background: rgba(0,0,0,0.55); color: rgba(255,255,255,0.7);
  display: flex; align-items: center; justify-content: center;
  opacity: 0; transition: opacity 0.15s, background 0.15s;
  cursor: zoom-in; z-index: 2;
}
.zoom-btn:hover { background: rgba(0,0,0,0.8); color: #fff; }
.zoom-btn.round { border-radius: 50%; top: 2px; right: 2px; width: 20px; height: 20px; }
.zoom-btn.crew-zoom { top: 0; right: 0; width: 16px; height: 16px; }
.hero-poster:hover .zoom-btn,
.cast-photo-wrap:hover .zoom-btn,
.crew-photo-wrap:hover .zoom-btn { opacity: 1; }

.cast-photo-wrap { position: relative; width: 76px; height: 76px; border-radius: 50%; overflow: hidden; margin: 0 auto 8px; }
.crew-photo-wrap { position: relative; width: 36px; height: 36px; border-radius: 50%; overflow: hidden; flex-shrink: 0; }

.detail-body-below { padding: 0 40px 80px; }
.detail-section { margin-top: 40px; }

.tab-bar { display: flex; gap: 0; border-bottom: 1px solid var(--border); margin-bottom: 20px; }
.tab-btn {
  padding: 10px 20px; font-size: 13px; font-weight: 500; color: var(--fg-2);
  border-bottom: 2px solid transparent; transition: color 0.15s, border-color 0.15s;
}
.tab-btn:hover { color: var(--fg-0); }
.tab-btn.active { color: var(--gold); border-bottom-color: var(--gold); }
.tab-count { font-family: var(--font-mono); font-size: 11px; color: var(--fg-3); margin-left: 6px; }

.cast-card {
  width: 100px; flex-shrink: 0;
  text-align: center; text-decoration: none; color: inherit; cursor: pointer;
}
.cast-card:hover .cast-name { color: var(--gold); }
.cast-photo { width: 76px; height: 76px; border-radius: 50%; object-fit: cover; margin: 0 auto; display: block; }
.cast-avatar {
  width: 76px; height: 76px; border-radius: 50%;
  background: linear-gradient(135deg, var(--bg-4), var(--bg-3));
  display: flex; align-items: center; justify-content: center; margin: 0 auto 8px;
  font-size: 16px; font-weight: 600; color: var(--fg-2);
}
.cast-name { font-size: 12px; font-weight: 500; color: var(--fg-0); transition: color 0.15s; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cast-role { font-size: 10px; color: var(--fg-3); margin-top: 2px; font-family: var(--font-mono); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.crew-dept-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 6px; }
.crew-card {
  display: flex; align-items: center; gap: 12px;
  padding: 8px 12px; border-radius: var(--r-sm);
  text-decoration: none; color: inherit; transition: background 0.12s;
}
.crew-card:hover { background: rgba(255,255,255,0.04); }
.crew-card:hover .crew-name { color: var(--gold); }
.crew-photo { width: 36px; height: 36px; border-radius: 50%; object-fit: cover; flex-shrink: 0; display: block; }
.crew-initials {
  width: 36px; height: 36px; border-radius: 50%; flex-shrink: 0;
  background: var(--bg-3); display: flex; align-items: center; justify-content: center;
  font-size: 12px; font-weight: 600; color: var(--fg-3);
}
.crew-name { font-size: 13px; font-weight: 500; color: var(--fg-0); transition: color 0.15s; }
.crew-job { font-size: 11px; color: var(--fg-3); font-family: var(--font-mono); }

.hscroll { display: flex; gap: 14px; overflow-x: auto; scrollbar-width: none; padding-bottom: 4px; }
.hscroll::-webkit-scrollbar { display: none; }

.expanded-grid, .fold-grid {
  display: grid; gap: 14px;
  animation: fold-open 0.35s cubic-bezier(0.4, 0, 0.2, 1);
}
.videos-expanded { grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); }
.extras-expanded { grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); }
.expanded-grid .video-card, .fold-grid .video-card { width: auto; }
.expanded-grid .extra-card, .fold-grid .extra-card { min-width: 0; }

@keyframes fold-open {
  from { max-height: 200px; opacity: 0.6; overflow: hidden; }
  to { max-height: 2000px; opacity: 1; }
}

.video-card { width: 240px; flex-shrink: 0; text-decoration: none; color: inherit; }
.video-card:hover .video-name { color: var(--gold); }
.video-thumb { position: relative; aspect-ratio: 16/9; border-radius: var(--r-md); overflow: hidden; background: var(--bg-3); }
.video-thumb img { width: 100%; height: 100%; object-fit: cover; }
.video-play { position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; background: rgba(0,0,0,0.3); opacity: 0; transition: opacity 0.15s; }
.video-card:hover .video-play { opacity: 1; }
.video-name { font-size: 12px; font-weight: 500; margin-top: 8px; color: var(--fg-0); transition: color 0.15s; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.video-type { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); text-transform: uppercase; }

.extra-card {
  display: flex; align-items: center; gap: 12px; padding: 10px; min-width: 260px; flex-shrink: 0;
  background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-sm);
  cursor: pointer; transition: background 0.12s;
}
.extra-card:hover { background: var(--bg-3); }
.extra-thumb { width: 80px; height: 45px; border-radius: var(--r-xs); background: var(--bg-4); display: flex; align-items: center; justify-content: center; color: var(--fg-2); flex-shrink: 0; overflow: hidden; }
.extra-thumb-img { width: 100%; height: 100%; object-fit: cover; }
.extra-title { font-size: 12px; font-weight: 500; color: var(--fg-0); white-space: nowrap; }

.rec-scroll { display: flex; gap: 16px; overflow-x: auto; scrollbar-width: none; padding-bottom: 4px; }
.rec-scroll::-webkit-scrollbar { display: none; }
.rec-tile { width: 140px; flex-shrink: 0; text-decoration: none; color: inherit; }
.rec-tile.dimmed { opacity: 0.5; }
.rec-tile:not(.dimmed):hover { transform: translateY(-3px); }
.rec-in-library {
  display: inline-block; font-size: 9px; font-weight: 700;
  color: var(--good); text-transform: uppercase; letter-spacing: 0.06em;
  margin-left: 4px;
}

.scroll-arrow {
  width: 28px; height: 28px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: rgba(255,255,255,0.06); border: 1px solid var(--border);
  color: var(--fg-2); transition: all 0.15s;
}
.scroll-arrow:hover { background: rgba(255,255,255,0.12); color: var(--fg-0); }

@media (max-width: 900px) {
  .hero-content { grid-template-columns: 1fr; gap: 20px; }
  .hero-poster { display: none; }
  .detail-title { font-size: 32px; }
  .detail-body-below { padding: 0 20px 60px; }
  .bd-indicators { right: 20px; bottom: 16px; }
  .hero-expand { display: none; }
}
</style>
