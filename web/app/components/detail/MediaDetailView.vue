<template>
  <div v-if="loading" class="scroll" style="height: 100%">
    <div style="height: 380px; background: var(--bg-2)" />
  </div>

  <div v-else-if="detail" class="scroll" style="height: 100%">
    <!-- Hero: backdrop + poster + info merged -->
    <div class="hero-section">
      <div class="hero-bg">
        <NuxtImg
          v-if="backdropA"
          :src="backdropA"
          :width="1920"
          :quality="80"
          class="hero-bg-img"
          :class="{ visible: showA }"
          @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
        />
        <NuxtImg
          v-if="backdropB"
          :src="backdropB"
          :width="1920"
          :quality="80"
          class="hero-bg-img"
          :class="{ visible: !showA }"
          @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
        />
        <div class="hero-bg-fade" />
      </div>

      <div class="hero-content">
        <div class="hero-poster">
          <Poster :idx="0" :src="usePosterUrl(detail.media_item.id)" :title="detail.media_item.title" aspect="2/3" :width="600" />
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
      <CastCrewTabs v-if="detail.cast?.length || detail.crew?.length" :cast="detail.cast" :crew="detail.crew" variant="underline" />

      <!-- Content tabs: Videos / Extras / Seasons -->
      <TabsRoot v-if="contentTabs.length" v-model="contentTab" class="detail-section">
        <div class="section-row-head" style="margin-bottom: 0">
          <TabsList class="tab-bar" style="margin-bottom: 0">
            <TabsTrigger
              v-for="t in contentTabs"
              :key="t.id"
              :value="t.id"
              class="tab-btn"
            >
              {{ t.label }} <span class="tab-count">{{ t.count }}</span>
            </TabsTrigger>
          </TabsList>
          <div v-if="contentTab === 'videos'" style="display: flex; gap: 8px">
            <button class="scroll-arrow" @click="scrollContentLeft"><Icon name="chevleft" :size="16" /></button>
            <button class="scroll-arrow" @click="scrollContentRight"><Icon name="chevright" :size="16" /></button>
            <button class="scroll-arrow" @click="contentExpanded = !contentExpanded">
              <Icon name="chevdown" :size="16" :style="{ transform: contentExpanded ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
            </button>
          </div>
        </div>

        <TabsContent value="videos" style="margin-top: 16px">
          <div :class="contentExpanded ? 'expanded-grid videos-expanded' : 'hscroll'" ref="videosScroll">
            <a
              v-for="(v, i) in detail.videos"
              :key="v.id"
              :href="`https://www.youtube.com/watch?v=${v.video_key}`"
              target="_blank"
              class="video-card"
            >
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
            </a>
          </div>
        </TabsContent>

        <TabsContent value="extras" style="margin-top: 16px">
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
                  <NuxtImg v-if="e.thumbnail_path" :src="`/api/extras/${e.id}/thumbnail`" :width="400" :quality="80" alt="" class="extra-thumb-img" loading="lazy" />
                  <Icon v-else name="play" :size="20" />
                </div>
                <div class="extra-title">{{ e.title }}</div>
              </div>
            </div>
          </div>
        </TabsContent>

        <TabsContent value="seasons" style="display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 16px; margin-top: 16px">
          <div v-for="s in detail.seasons" :key="s.id" class="card-tile">
            <MediaCard
              :idx="s.season_number"
              aspect="2/3"
              :title="s.title"
              :subtitle="`${s.aired_episodes} episodes`"
            />
          </div>
        </TabsContent>
      </TabsRoot>

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
            <MediaCard
              :idx="r.id"
              :src="recPosterUrl(r)"
              aspect="2/3"
              :title="r.title"
              :subtitle="r.release_date?.slice(0, 4) || '?'"
            >
              <template v-if="r.local_media_item_id" #badges>
                <div class="rec-in-library-badge">In library</div>
              </template>
            </MediaCard>
          </component>
        </div>
      </div>
    </div>

    <!-- List modal -->
    <AddToListDialog v-model:open="showListModal" :media-item-id="detail.media_item.id" />
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
import { TabsRoot, TabsList, TabsTrigger, TabsContent } from 'reka-ui'

const props = defineProps<{ mediaId: number }>()
const lightbox = useLightbox()

const detail = ref<MediaDetail | null>(null)
const loading = ref(true)
const contentTab = ref('')
const contentExpanded = ref(false)

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

// Watched (for movies)
const isWatched = ref(false)
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

// Lists — AddToListDialog owns loading/creation/toggling.
const showListModal = ref(false)
const showMetadataEditor = ref(false)
const extrasExpanded = reactive<Record<string, boolean>>({})
const recScrollEl = ref<HTMLElement>()
const videosScroll = ref<HTMLElement>()
const scrollRefs: Record<string, HTMLElement> = {}

function setScrollRef(key: string, el: any) {
  if (el) scrollRefs[key] = el
}

function scrollEl(refName: string, dir: number) {
  let el: HTMLElement | undefined
  if (refName === 'videosScroll') el = videosScroll.value
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

// Crossfade backdrops — shared carousel engine. This view historically
// imposed no sort_order cap and preloaded the second backdrop.
const {
  showA, backdropA, backdropB, backdropIdx, carouselPaused, backdropAssets,
  pauseCarousel, resumeCarousel, jumpToBackdrop, seedCarousel, openBackdropLightbox,
} = useBackdropCarousel(detail, { preloadSecond: true })

// Lightbox
function openPosterLightbox() {
  const src = usePosterUrl(detail.value!.media_item.id)
  if (src) lightbox.open(src)
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
  const { $heya } = useNuxtApp()
  try {
    detail.value = await $heya('/api/media/{id}', {
      path: { id: props.mediaId as any },
    }) as MediaDetail
  } catch { /* empty */ }
  loading.value = false

  const first = contentTabs.value[0]
  if (first) contentTab.value = first.id

  seedCarousel()

  if (detail.value) {
    const res = await $heya('/api/me/favorites/check', {
      query: { entity_type: 'media_item', entity_id: detail.value.media_item.id },
    }) as { favorited: boolean }
    isFavorited.value = res.favorited
  }
})
</script>

<style scoped>
/* Shared backdrop/carousel/zoom chrome (.hero-bg*, .bd-*, .hero-expand,
   .zoom-btn) lives in heya.css; only per-view deltas stay scoped here. */
.hero-section { min-height: 520px; }
/* Slightly slower crossfade than the movie/tv pages (1.5s global). */
.hero-bg-img { transition: opacity 1.8s ease-in-out; }
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

/* Narrower body padding than the movie/tv pages (48px global). */
.detail-body-below { padding: 0 40px 80px; }
.detail-section { margin-top: 40px; }

.tab-bar { display: flex; gap: 0; border-bottom: 1px solid var(--border); margin-bottom: 20px; }
.tab-btn {
  padding: 10px 20px; font-size: 13px; font-weight: 500; color: var(--fg-2);
  border-bottom: 2px solid transparent; transition: color 0.15s, border-color 0.15s;
}
.tab-btn:hover { color: var(--fg-0); }
.tab-btn[data-state="active"] { color: var(--gold); border-bottom-color: var(--gold); }
.tab-count { font-family: var(--font-mono); font-size: 11px; color: var(--fg-3); margin-left: 6px; }

/* Tighter gap than the global .hscroll (16px). */
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

.video-card { width: 240px; flex-shrink: 0; text-decoration: none; color: inherit; display: block; }
/* Hover play overlay sits in MediaCard's badges slot. Covers the full thumb
   and reveals on hover, sitting above the gradient via z-index. */
.video-play {
  position: absolute; inset: 0; z-index: 3;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.3);
  opacity: 0; transition: opacity 0.15s;
  pointer-events: none; color: #fff;
}
.video-card:hover .video-play { opacity: 1; }

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
.rec-in-library-badge {
  position: absolute; top: 8px; right: 8px; z-index: 3;
  font-size: 9px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em;
  padding: 3px 8px; border-radius: 999px;
  background: rgba(0, 0, 0, 0.6); backdrop-filter: blur(6px);
  color: var(--good); pointer-events: none;
}

.scroll-arrow {
  width: 28px; height: 28px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  background: rgba(255,255,255,0.06); border: 1px solid var(--border);
  color: var(--fg-2); transition: all 0.15s;
}
.scroll-arrow:hover { background: rgba(255,255,255,0.12); color: var(--fg-0); }

/* Tablet (folded from the previous 900px collapse point onto the ratified
   960px convention — docs/ui.md "Responsive conventions"). No structural
   rework: same single-column collapse, poster still hidden entirely (this
   view's own long-standing choice, not something introduced here). */
@media (max-width: 960px) {
  .hero-content { grid-template-columns: 1fr; gap: 20px; }
  .hero-poster { display: none; }
  .detail-title { font-size: 32px; }
  .detail-body-below { padding: 0 20px 60px; }
}

/* Phone: tighter padding, meta rows wrap, action row gets 44px touch targets. */
@media (max-width: 720px) {
  .hero-content { padding: 24px 16px 20px; gap: 14px; }
  .detail-title { font-size: 26px; }
  .hero-meta-row { flex-wrap: wrap; row-gap: 6px; }
  .ratings-row { row-gap: 6px; }
  .detail-actions { flex-wrap: wrap; row-gap: 10px; }
  .detail-actions .btn { height: 44px; }
  .detail-actions .btn-primary { flex: 1 1 100%; }
  .detail-actions .btn-icon { width: 44px; height: 44px; }
  .detail-body-below { padding: 0 16px 60px; }
  .tab-bar { overflow-x: auto; scrollbar-width: none; }
  .tab-bar::-webkit-scrollbar { display: none; }
}

/* Touch: swipe replaces the mouse-only scroll arrows (always-visible on this
   view's underline variant, unlike the pill variant's overflow-gated ones). */
@media (pointer: coarse) {
  .scroll-arrow { display: none; }
}
</style>
