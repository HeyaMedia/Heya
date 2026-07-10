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
        <div class="hero-left">
          <div class="hero-poster">
            <Poster :idx="0" :src="usePosterUrl(detail.media_item)" :title="detail.media_item.title" aspect="2/3" :width="600" />
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
            <button v-if="playableFileRef" class="btn btn-primary" @click="play">
              <Icon name="play" :size="16" /> {{ resumeInProgress ? 'Resume' : 'Play' }}
            </button>
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
        <div v-if="(detail.external_ratings && detail.external_ratings.length) || (detail.available && playableFileRef)" class="hero-side">
          <MediaRatings :ratings="detail.external_ratings" />
          <MediaPlaybackPanel v-if="detail.available && playableFileRef" :media-item-id="detail.media_item.id" />
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
      <CastCrewTabs v-if="detail.cast?.length || detail.crew?.length" :cast="detail.cast" :crew="detail.crew" />

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
                <NuxtImg v-if="e.thumbnail_path" :src="`/api/extras/${e.id}/thumbnail`" :width="400" :quality="80" alt="" class="extra-thumb-img" loading="lazy" />
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
          <AppContextMenu v-for="r in detail.recommendations" :key="r.id" :items="recContextItems(r)" :disabled="!r.local_media_item_id">
            <NuxtLink :to="r.local_media_item_id ? mediaUrl({ id: r.local_media_item_id, public_id: r.local_public_id ?? undefined, title: r.title ?? '', slug: r.local_slug ?? undefined, media_type: r.media_type }) : ''" class="rec-card" :class="{ 'rec-external': !r.local_media_item_id }">
              <MediaCard
                :idx="r.id"
                :src="recPosterUrl(r)"
                aspect="2/3"
                :title="r.title ?? 'Untitled'"
                :badge-tr="r.vote_average ? `★ ${formatVote(r.vote_average)}` : ''"
              />
            </NuxtLink>
          </AppContextMenu>
        </div>
        <div v-else class="rec-grid">
          <AppContextMenu v-for="r in detail.recommendations" :key="r.id" :items="recContextItems(r)" :disabled="!r.local_media_item_id">
            <NuxtLink :to="r.local_media_item_id ? mediaUrl({ id: r.local_media_item_id, public_id: r.local_public_id ?? undefined, title: r.title ?? '', slug: r.local_slug ?? undefined, media_type: r.media_type }) : ''" class="rec-card" :class="{ 'rec-external': !r.local_media_item_id }">
              <MediaCard
                :idx="r.id"
                :src="recPosterUrl(r)"
                aspect="2/3"
                :title="r.title ?? 'Untitled'"
                :badge-tr="r.vote_average ? `★ ${formatVote(r.vote_average)}` : ''"
              />
            </NuxtLink>
          </AppContextMenu>
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
import type { MediaDetail, MediaExtra, MediaItem, StreamInfoResponse, UserList } from '~~/shared/types'
import { useQuery } from '@tanstack/vue-query'

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const lightbox = useLightbox()

// Main media detail — cached across remounts so back-navigation from /watch
// or another movie page is instant. Reactive key on slug means a new movie
// URL re-fetches naturally.
const { $heya } = useNuxtApp()
const detailQuery = useQuery({
  queryKey: ['media', 'detail', slug],
  queryFn: async () => (await $heya('/api/media/{id}', { path: { id: slug.value as never } })) as MediaDetail,
  staleTime: 1000 * 60 * 5,
  retry: false,
})
const detail = computed<MediaDetail | null>(() => detailQuery.data.value ?? null)
const loading = computed(() => detailQuery.isPending.value)

// Redirect on confirmed failure rather than every transient error.
watch(detailQuery.error, (err) => { if (err) navigateTo('/movies') })

// Drives the Play button label switch — shows "Resume" when there's saved
// progress on this movie's media_item, "Play" otherwise.
const movieEntityId = computed(() => detail.value?.media_item.id ?? 0)
const { inProgress: resumeInProgress } = useWatchResume('movie', movieEntityId)

// Live refresh — a debounced re-enrich (or a metadata edit) lands new data
// server-side while this page is open; without this the user has to
// manually reload to see it. Filtered on this movie's media_item_id so
// another item's update doesn't retrigger a refetch here.
useLiveRefresh([
  {
    events: ['media.updated'],
    filter: (e) => {
      const payload = e.payload as { media_item_id?: number } | undefined
      return payload?.media_item_id === movieEntityId.value
    },
    keys: [['media', 'detail', slug]],
  },
])

const streamInfo = ref<StreamInfoResponse | null>(null)
const videoModal = ref<{ key: string; title: string } | null>(null)
const showMetadataEditor = ref(false)
const extrasExpanded = reactive<Record<string, boolean>>({})
const extrasRefs: Record<string, HTMLElement> = {}

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
  if (r.local_media_item_id || r.local_public_id) return usePosterUrl({ id: r.local_media_item_id, public_id: r.local_public_id }) ?? ''
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
  const src = usePosterUrl(detail.value!.media_item)
  if (src) lightbox.open(src)
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
const playableFileRef = computed(() => detail.value?.files?.[0]?.public_id || detail.value?.files?.[0]?.id || null)

function play() {
  if (!playableFileRef.value || !detail.value) return
  const params = new URLSearchParams({
    media_item_id: String(detail.value.media_item.id),
    title: detail.value.preferred_title || detail.value.media_item.title,
    entity_type: 'movie',
    entity_id: String(detail.value.media_item.id),
  })
  navigateTo(`/watch/${playableFileRef.value}?${params}`)
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

const invalidateContinueWatching = useInvalidateContinueWatching()
const recUserLists = ref<UserList[]>([])
const recWatchedSet = ref<Set<number>>(new Set())
const recFavoritedSet = ref<Set<number>>(new Set())
const { buildItems: buildCardCtxItems } = useCardContextItems()
async function toggleWatched() {
  if (!detail.value) return
  const next = !isWatched.value
  const id = detail.value.media_item.id
  isWatched.value = next
  const local = new Set(recWatchedSet.value)
  if (next) local.add(id)
  else local.delete(id)
  recWatchedSet.value = local
  try {
    await $heya('/api/me/watched/media/{id}', {
      method: 'POST',
      path: { id },
      body: { watched: next } as any,
    })
    invalidateContinueWatching()
  } catch {
    isWatched.value = !next
    const rollback = new Set(recWatchedSet.value)
    if (next) rollback.delete(id)
    else rollback.add(id)
    recWatchedSet.value = rollback
  }
}

async function loadState() {
  if (!detail.value) return
  try {
    const [stateRes, listsRes] = await Promise.allSettled([
      fetchUserState('movies'),
      $heya('/api/me/lists') as Promise<UserList[]>,
    ])
    if (stateRes.status === 'fulfilled') {
      const st = stateRes.value
      recFavoritedSet.value = new Set(st.favorited || [])
      recWatchedSet.value = new Set(st.watched || [])
      isFavorited.value = recFavoritedSet.value.has(detail.value.media_item.id)
      isWatched.value = recWatchedSet.value.has(detail.value.media_item.id)
    }
    if (listsRes.status === 'fulfilled') recUserLists.value = listsRes.value
  } catch { /* empty */ }
}

function recToMediaItem(r: any): MediaItem {
  return {
    id: r.local_media_item_id,
    public_id: r.local_public_id,
    title: r.title,
    slug: r.local_slug ?? undefined,
    year: r.year ?? '',
    media_type: r.media_type || 'movie',
    available: true,
  } as unknown as MediaItem
}

function recContextItems(r: any) {
  if (!r.local_media_item_id) return []
  return buildCardCtxItems(recToMediaItem(r), {
    watchedSet: recWatchedSet.value,
    favoritedSet: recFavoritedSet.value,
    userLists: recUserLists.value,
    onToggleWatched: async (id: number, watched: boolean) => {
      try {
        await $heya('/api/me/watched/media/{id}', {
          method: 'POST',
          path: { id },
          body: { watched } as any,
        })
        const next = new Set(recWatchedSet.value)
        if (watched) next.add(id)
        else next.delete(id)
        recWatchedSet.value = next
        if (id === detail.value?.media_item.id) isWatched.value = watched
        invalidateContinueWatching()
      } catch { /* ignore */ }
    },
    onToggleFavorite: async (id: number, favorited: boolean) => {
      try {
        await $heya('/api/me/favorites', {
          method: 'POST',
          body: { entity_type: 'media_item', entity_id: id } as any,
        })
        const next = new Set(recFavoritedSet.value)
        if (favorited) next.add(id)
        else next.delete(id)
        recFavoritedSet.value = next
        if (id === detail.value?.media_item.id) isFavorited.value = favorited
      } catch { /* ignore */ }
    },
    onAddToList: async (listId: number, mediaId: number) => {
      try {
        await $heya('/api/me/lists/{id}/items', {
          method: 'POST',
          path: { id: listId },
          body: { media_item_id: mediaId } as any,
        })
      } catch { /* ignore */ }
    },
  })
}

// User lists — AddToListDialog owns loading/creation/toggling.
const showListModal = ref(false)

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

async function loadStreamInfo() {
  if (!playableFileRef.value) return
  try {
    const caps = useClientCaps()
    const capsQuery = capsToQueryString(caps)
    const url = `/api/stream/${playableFileRef.value}/info${capsQuery ? `?${capsQuery}` : ''}`
    const token = useAuth().token.value
    streamInfo.value = await $fetch<StreamInfoResponse>(url, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
  } catch { /* empty */ }
}

// When detail data arrives (or changes via slug), reinitialize all the
// side effects that depend on it: backdrop carousel, state, stream info,
// overflow detection.
watch(detail, async (d) => {
  if (!d) return
  await nextTick()
  seedCarousel()
  loadState()
  loadStreamInfo()
  checkRecsOverflow()
}, { immediate: true })
</script>

<style scoped>
/* Hero — matches TV detail page. The shared backdrop/carousel/zoom chrome
   (.hero-bg*, .bd-*, .hero-expand, .zoom-btn, .hero-side, .detail-body-below,
   .scroll-controls, .hscroll) lives in heya.css; only per-page deltas here. */
.hero-section { min-height: 520px; }
.hero-content {
  position: relative; z-index: 2;
  display: grid; grid-template-columns: 260px minmax(0, 1fr) 260px;
  gap: 36px; padding: 40px 40px 48px;
}
.hero-left { display: flex; flex-direction: column; gap: 14px; align-self: start; min-width: 0; }
.hero-poster { position: relative; }
.hero-info { display: flex; flex-direction: column; justify-content: center; min-width: 0; }

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

/* Body */
.detail-section { margin-top: 36px; }
.section-row-head { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 16px; }

/* Count badge in section labels (extras group heads) */
.tab-count { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); margin-left: 4px; }

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
.video-play {
  position: absolute; inset: 0; z-index: 3;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.35); opacity: 0; transition: opacity 0.15s;
  color: #fff; pointer-events: none; /* on artwork — stays literal */
}
.video-card:hover .video-play { opacity: 1; }

/* Video dialog — content-class hook on AppDialog. The dialog body
   already provides scrolling/padding; we override to drop padding so
   the iframe fills the panel edge-to-edge, and keep a 16:9 aspect. */
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
  .hero-ratings { grid-template-columns: repeat(4, minmax(0, 1fr)); }
}

/* Tablet (folded from the previous 900px collapse point onto the ratified
   960px convention — docs/ui.md "Responsive conventions"): single-column
   hero, small poster, no other structural change. */
@media (max-width: 960px) {
  .hero-content { grid-template-columns: 1fr; gap: 20px; padding: 32px 20px 24px; }
  .hero-poster { max-width: 200px; }
  .hero-left { flex-direction: column; }
  .hero-ratings { grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); }
  .detail-title { font-size: 32px; }
}

/* Phone: tighter padding/poster, meta rows wrap instead of clipping, primary
   CTA takes its own full-width row (labels like "Resume S01E03 - Title" don't
   fit alongside the secondary buttons), and every actionable button meets the
   44px touch target minimum. */
@media (max-width: 720px) {
  .hero-content { padding: 24px 16px 20px; gap: 16px; }
  .hero-poster { max-width: 140px; }
  .detail-title { font-size: 26px; }
  .hero-meta-row { flex-wrap: wrap; row-gap: 6px; }
  .detail-actions { flex-wrap: wrap; row-gap: 10px; }
  .detail-actions .btn { height: 44px; }
  .detail-actions .btn-primary { flex: 1 1 100%; white-space: normal; text-align: left; line-height: 1.3; height: auto; min-height: 44px; padding: 10px 16px; }
  .detail-actions .btn-icon { width: 44px; height: 44px; }
  .extras-group-head { flex-wrap: wrap; row-gap: 8px; }
}

/* Touch: swipe replaces the mouse-only scroll arrows on the extras/recs
   section-head controls. The fold/expand toggle (`.expand`) stays — it's a
   real affordance on touch too, not a mouse-only convenience. */
@media (pointer: coarse) {
  .scroll-controls .scroll-ctrl-btn:not(.expand) { display: none; }
}
</style>
