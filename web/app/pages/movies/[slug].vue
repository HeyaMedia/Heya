<template>
  <div v-if="loading" class="scroll hero-flush" style="height: 100%">
    <div style="height: 380px; background: var(--bg-2)" />
  </div>

  <!-- `hero-flush` opts this page out of the .app-main topbar offset so the
       movie hero art fills up under the glass topbar (the hero's own inner
       padding keeps text clear of the bar). See heya.css .app-main. The tone
       vars (--tone/--tone-rgb/--tone-ink) are published on the scroll root,
       mirroring the episode/season ports + the playbar's --pb-accent. -->
  <div v-else-if="detail" class="scroll movie2 hero-flush" :style="toneStyle" style="height: 100%">
    <!-- ── HERO: A/B backdrop crossfade as sharp art, hard-clipped at the
         ledger seam. HeroCanvas also publishes the shared hero art claim to
         the global AmbientBackdrop, so the blurred underlay follows the
         carousel. ── -->
    <section class="hero-section movie-hero">
      <HeroCanvas
        :src="backdropA || ''"
        :src-b="backdropB"
        :show-a="showA"
        object-position="center 28%"
      />

      <!-- Backdrop tools — expand-to-lightbox + the shared prev/pause/next
           ring together, top-right of the hero. Drives rotation. -->
      <div v-if="backdropAssets.length > 0" class="hero-tools">
        <button class="hero-expand" aria-label="Expand backdrop" @click="openBackdropLightbox">
          <Icon name="expand" :size="13" />
        </button>
        <CycleControls
          v-if="backdropAssets.length > 1"
          v-model:paused="carouselPaused"
          :cycle-key="cycleKey"
          :duration="BACKDROP_INTERVAL"
          item-label="backdrop"
          @prev="retreatBackdrop"
          @next="advanceBackdrop"
        />
      </div>

      <div class="hero-inner">
        <!-- Poster record-card — layered directional shadow, poster lightbox
             zoom. Hidden ≤tablet per the mockup. -->
        <div class="hero-poster postercard">
          <Poster :idx="0" :src="usePosterUrl(detail.media_item)" :title="detail.media_item.title" aspect="2/3" :width="600" />
          <button class="zoom-btn" aria-label="Expand poster" @click="openPosterLightbox"><Icon name="expand" :size="14" /></button>
        </div>

        <div class="grow hero-ink">
          <div class="eyebrow">
            <span>Movie</span>
            <template v-if="detail.collection">
              <span class="sep">&middot;</span>
              <NuxtLink :to="`/collection/${detail.collection.id}`">{{ detail.collection.name }}</NuxtLink>
            </template>
            <template v-if="collectionPart">
              <span class="sep">&middot;</span>
              <span>Part {{ collectionPart.n }} of {{ collectionPart.total }}</span>
            </template>
          </div>

          <h1 v-if="heroLogoUrl && !heroLogoFailed" class="title title-art">
            <LoadingImage
              :src="heroLogoUrl"
              :alt="detail.preferred_title || detail.media_item.title"
              :width="600"
              class="title-logo"
              @error="heroLogoFailed = true"
            />
          </h1>
          <h1 v-else class="title">{{ detail.preferred_title || detail.media_item.title }}</h1>

          <p v-if="detail.movie?.tagline" class="tagline">{{ detail.movie.tagline }}</p>

          <p class="metaline">
            <span v-if="detail.media_item.year">{{ detail.media_item.year }}</span>
            <template v-if="runtimeUpper">
              <span class="dot">&middot;</span><span>{{ runtimeUpper }}</span>
            </template>
            <template v-if="certification">
              <span class="dot">&middot;</span><span>{{ certification }}</span>
            </template>
            <template v-if="genres.length">
              <span class="dot">&middot;</span>
              <NuxtLink v-for="g in genres" :key="g" :to="`/genre/${encodeURIComponent(g)}`" class="genre">{{ g }}</NuxtLink>
            </template>
          </p>

          <div class="actions">
            <button v-if="playableFileRef" class="btn-play" @click="play">
              <span class="tri" /> {{ resumeInProgress ? 'Resume' : 'Play' }}
              <small v-if="runtimeUpper">{{ runtimeUpper }}</small>
            </button>
            <button v-else class="btn-play" disabled>
              <span class="tri" /> No File
            </button>

            <button v-if="trailerVideo" class="pill" @click="openTrailer">Trailer</button>
            <button class="pill" @click="showListModal = true"><Icon name="plus" :size="15" /> My List</button>

            <button
              class="pill icon"
              :class="{ 'is-on': isFavorited }"
              :aria-label="isFavorited ? 'Remove from loved' : 'Add to loved'"
              :aria-pressed="isFavorited"
              :title="isFavorited ? 'Remove from loved' : 'Add to loved'"
              @click="toggleFavorite"
            >
              <Icon :name="isFavorited ? 'heartfill' : 'heart'" :size="16" />
            </button>
            <button
              class="pill icon"
              :class="{ 'is-on': isWatched }"
              :aria-label="isWatched ? 'Mark as unwatched' : 'Mark as watched'"
              :aria-pressed="isWatched"
              :title="isWatched ? 'Mark as unwatched' : 'Mark as watched'"
              @click="toggleWatched"
            >
              <Icon name="check" :size="16" />
            </button>
            <button class="pill icon" title="Edit Metadata" aria-label="Edit metadata" @click="showMetadataEditor = true">
              <Icon name="settings" :size="15" />
            </button>
          </div>
        </div>
      </div>
    </section>

    <!-- ── LEDGER at the hard-clip seam: headline ratings + runtime/cert +
         codec/resolution + playback decision (user-facing facts only). ── -->
    <LedgerStrip :cells="ledgerCells" />

    <!-- ── BODY ── -->
    <main class="page">
      <!-- Story + Credits -->
      <section class="section cols">
        <div>
          <SectionHeader title="Story" />
          <div v-if="storyText" class="prose">
            <p class="lede">{{ storyText }}</p>
          </div>
          <p v-else class="prose-empty">No synopsis available.</p>
        </div>

        <div v-if="detail.crew?.length || hasCreditExtras">
          <SectionHeader title="Credits" />
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
        </div>
      </section>

      <!-- Cast & Crew -->
      <section v-if="detail.cast?.length || detail.crew?.length" class="section">
        <CastCrewTabs :cast="detail.cast" :crew="detail.crew" />
      </section>

      <!-- Extras -->
      <section v-if="groupedExtras.length" class="section">
        <SectionHeader title="Extras" />
        <div v-for="group in groupedExtras" :key="group.type" class="extras-group">
          <div class="extras-group-head">
            <div class="extras-group-label">{{ formatExtraType(group.type) }} <span class="tab-count">{{ group.items.length }}</span></div>
            <div class="scroll-controls">
              <template v-if="!extrasExpanded[group.type]">
                <AppHoldButton class="scroll-ctrl-btn" aria-label="Scroll left" title="Hold to jump to start" @click="scrollExtras(group.type, 'left')" @hold="extrasRailRefs[group.type]?.scrollToStart()"><Icon name="chevleft" :size="14" /></AppHoldButton>
                <button class="scroll-ctrl-btn" aria-label="Scroll right" @click="scrollExtras(group.type, 'right')"><Icon name="chevright" :size="14" /></button>
              </template>
              <button
                class="scroll-ctrl-btn expand" aria-label="Toggle expanded view"
                :aria-expanded="!!extrasExpanded[group.type]"
                @click="extrasExpanded[group.type] = !extrasExpanded[group.type]"
              >
                <Icon name="chevdown" :size="14" :style="{ transform: extrasExpanded[group.type] ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
              </button>
            </div>
          </div>
          <div v-if="extrasExpanded[group.type]" class="extras-grid">
            <div v-for="e in group.items" :key="e.id" class="extra-card">
              <div class="extra-thumb">
                <LoadingImage v-if="e.thumbnail_path" :src="`/api/extras/${e.id}/thumbnail`" :width="400" :quality="80" alt="" class="extra-thumb-img" loading="lazy" />
                <Icon v-else name="play" :size="20" />
              </div>
              <div class="extra-meta">
                <div class="extra-title">{{ e.title }}</div>
                <div v-if="e.duration_ms" class="extra-sub">{{ formatExtraDuration(e.duration_ms) }}</div>
              </div>
            </div>
          </div>
          <!-- extra-card is a fixed-height list-item tile (thumb + meta), not
               an image-box aspect tile — explicit tileHeight measured from the
               rendered card: 48px thumb + 2×10px padding + 2×1px border. -->
          <AppRail
            v-else
            :ref="(el: any) => setExtrasRailRef(group.type, el)"
            :items="group.items"
            :tile-width="280"
            :tile-height="70"
            :gap="16"
            :phone-gap="16"
            :memory-key="`extras-${group.type}`"
          >
            <template #default="{ item: e }">
              <div class="extra-card">
                <div class="extra-thumb">
                  <LoadingImage v-if="e.thumbnail_path" :src="`/api/extras/${e.id}/thumbnail`" :width="400" :quality="80" alt="" class="extra-thumb-img" loading="lazy" />
                  <Icon v-else name="play" :size="20" />
                </div>
                <div class="extra-meta">
                  <div class="extra-title">{{ e.title }}</div>
                  <div v-if="e.duration_ms" class="extra-sub">{{ formatExtraDuration(e.duration_ms) }}</div>
                </div>
              </div>
            </template>
          </AppRail>
        </div>
      </section>

      <!-- Videos / Trailers — swipe-only, no arrows; no phone-specific size in
           the original CSS, so pin phone-tile-width to the same 300px. -->
      <section v-if="detail.videos?.length" class="section">
        <SectionHeader title="Videos" />
        <AppRail :items="detail.videos" :tile-width="300" :phone-tile-width="300" aspect="16/9" :gap="16" :phone-gap="16" memory-key="movie-videos">
          <template #default="{ item: v, index: i }">
            <button class="video-card" @click="openVideo(v.video_key, v.name)">
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
          </template>
        </AppRail>
      </section>

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
          :src="videoEmbedSrc(videoModal.key)"
          frameborder="0"
          allow="autoplay; encrypted-media; picture-in-picture"
          allowfullscreen
        />
      </AppDialog>

      <!-- Part of a collection: the full set as its own row (View collection
           covers the parts not in the library yet). -->
      <section v-if="detail.collection && collectionMovies.length" class="section collection-row">
        <ContentRow
          :title="detail.collection.name"
          :subtitle="`${collectionMovies.length} in library`"
          :items="collectionMovies"
          more="View collection"
          @tile="(item) => navigateTo(mediaUrl(item as MediaItem))"
          @more="navigateTo(`/collection/${detail!.collection!.id}`)"
        />
      </section>

      <!-- Recommendations: library titles by default; the appearance toggle
           adds external provider links for titles not in this library. -->
      <section v-if="visibleRecs.length" class="section">
        <SectionHeader title="More Like This">
          <template #actions>
            <div v-if="recsOverflows" class="scroll-controls">
              <AppHoldButton class="scroll-ctrl-btn" aria-label="Scroll left" title="Hold to jump to start" @click="recsRail?.scrollByDir(-1)" @hold="recsRail?.scrollToStart()"><Icon name="chevleft" :size="14" /></AppHoldButton>
              <button class="scroll-ctrl-btn" aria-label="Scroll right" @click="recsRail?.scrollByDir(1)"><Icon name="chevright" :size="14" /></button>
              <button class="scroll-ctrl-btn expand" aria-label="Toggle expanded view" :aria-expanded="recsExpanded" @click="recsExpanded = !recsExpanded">
                <Icon name="chevdown" :size="14" :style="{ transform: recsExpanded ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
              </button>
            </div>
          </template>
        </SectionHeader>

        <div v-if="recsExpanded" class="rec-grid">
          <AppContextMenu v-for="r in visibleRecs" :key="r.id" :items="recContextItems(r)" :disabled="!r.local_media_item_id">
            <NuxtLink :to="recTo(r)" :target="r.local_media_item_id ? undefined : '_blank'" class="rec-card" :class="{ 'rec-external': !r.local_media_item_id }">
              <MediaCard
                :idx="r.id"
                :src="recPosterUrl(r)"
                aspect="2/3"
                :title="r.title ?? 'Untitled'"
                :badge-tl="r.local_media_item_id ? '' : 'provider ↗'"
                :badge-tr="r.vote_average ? `★ ${formatVote(r.vote_average)}` : ''"
              />
            </NuxtLink>
          </AppContextMenu>
        </div>
        <!-- rec-card is a fixed 150px column (no phone-specific size in the
             original CSS) — pin phone-tile-width to match. -->
        <AppRail v-else ref="recsRail" :items="visibleRecs" :tile-width="150" :phone-tile-width="150" aspect="2/3" :gap="16" :phone-gap="16" memory-key="movie-recs">
          <template #default="{ item: r }">
            <AppContextMenu :items="recContextItems(r)" :disabled="!r.local_media_item_id">
              <NuxtLink :to="recTo(r)" :target="r.local_media_item_id ? undefined : '_blank'" class="rec-card" :class="{ 'rec-external': !r.local_media_item_id }">
                <MediaCard
                  :idx="r.id"
                  :src="recPosterUrl(r)"
                  aspect="2/3"
                  :title="r.title ?? 'Untitled'"
                  :badge-tl="r.local_media_item_id ? '' : 'provider ↗'"
                  :badge-tr="r.vote_average ? `★ ${formatVote(r.vote_average)}` : ''"
                />
              </NuxtLink>
            </AppContextMenu>
          </template>
        </AppRail>
      </section>

      <!-- Ratings: the full color-graded meter panel (the ledger carries only
           the headlines). Survives here as its own section. -->
      <section v-if="detail.external_ratings?.length" class="section">
        <SectionHeader title="Ratings" />
        <div class="ratings-wrap">
          <MediaRatings :ratings="detail.external_ratings" />
        </div>
      </section>

      <!-- Details footer -->
      <section class="section">
        <SectionHeader title="Details" />
        <dl class="detail-grid">
          <div v-if="detail.keywords?.length">
            <dt>Keywords</dt>
            <dd><MediaKeywords :keywords="detail.keywords" /></dd>
          </div>
          <div v-if="detail.movie?.release_date || certification">
            <dt>Release</dt>
            <dd>
              <span v-if="detail.movie?.release_date">{{ formatDate(detail.movie.release_date) }}</span>
              <template v-if="certification"><br>Rated {{ certification }}</template>
            </dd>
          </div>
          <div v-if="hasProvenanceLinks">
            <dt>External</dt>
            <dd><DetailLinksRow :media-item="detail.media_item" :collection="detail.collection" /></dd>
          </div>
        </dl>

        <div v-if="streamInfo || (detail.available && playableFileRef)" class="details-tech">
          <MediaStreamInfo v-if="streamInfo" :stream="streamInfo" />
          <MediaPlaybackPanel v-if="detail.available && playableFileRef" :media-item-id="detail.media_item.id" />
        </div>
      </section>
    </main>

    <!-- List modal -->
    <AddToListDialog v-model:open="showListModal" :media-item-id="detail.media_item.id" :media-type="detail.media_item.media_type" />

    <MetadataEditorModal
      v-if="detail"
      :media-id="detail.media_item.id"
      :show="showMetadataEditor"
      @close="showMetadataEditor = false"
    />
  </div>
</template>

<script setup lang="ts">
import type { ExternalRating, MediaDetail, MediaExtra, MediaItem, MediaRecommendation, StreamInfoResponse, UserList } from '~~/shared/types'
import type { ImageTone } from '~/composables/useImageTone'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import { withAuthHeaders } from '~/composables/useAuth'
import { useQuery } from '@pinia/colada'
import { mediaDetailQuery } from '~/queries/media'
import { collectionDetailQuery } from '~/queries/discovery'

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const lightbox = useLightbox()

// Main media detail — cached across remounts so back-navigation from /watch
// or another movie page is instant. Reactive key on slug means a new movie
// URL re-fetches naturally.
const { $heya } = useNuxtApp()
const detailQuery = useQuery(() => mediaDetailQuery(slug.value))
await waitForQuery(detailQuery)
const detail = computed<MediaDetail | null>(() => detailQuery.data.value ?? null)
const loading = computed(() => detailQuery.isPending.value)

// The home hero uses the logo asset as its title treatment. Clearart is
// transparent decorative artwork rather than a reliable wordmark, so the
// textual title remains the fallback when no logo exists or loading fails.
const heroLogoFailed = ref(false)
const heroLogoUrl = computed(() => {
  if (!detail.value?.media_item || !detail.value.assets?.some(asset => asset.asset_type === 'logo')) return null
  return useImageUrl(detail.value.media_item, 'logo')
})
watch(heroLogoUrl, () => { heroLogoFailed.value = false })

// Redirect on confirmed failure rather than every transient error.
watch(detailQuery.error, (err) => { if (err) navigateTo('/movies') }, { immediate: true })

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
const extrasRailRefs: Record<string, { scrollByDir: (dir: number, step?: number) => void; scrollToStart: () => void } | null> = {}

const recsExpanded = ref(false)
// AppRail is generic, so InstanceType<> can't name it — type the exposed
// surface directly (ContentRow/MusicScrollRow pattern).
const recsRail = ref<{ scrollByDir: (dir: number, step?: number) => void; scrollToStart: () => void; overflows: boolean } | null>(null)
// AppRail unmounts (v-if) while expanded, so remember the last known overflow
// answer — otherwise the chevron/expand cluster (itself gated on this flag)
// would vanish the moment the user expands, trapping them in the grid.
const lastRecsOverflow = ref(false)
watchEffect(() => { if (recsRail.value) lastRecsOverflow.value = recsRail.value.overflows })
const recsOverflows = computed(() => (recsExpanded.value ? lastRecsOverflow.value : (recsRail.value?.overflows ?? false)))

function setExtrasRailRef(key: string, el: any) {
  extrasRailRefs[key] = el
}

function scrollExtras(key: string, dir: 'left' | 'right') {
  extrasRailRefs[key]?.scrollByDir(dir === 'left' ? -1 : 1)
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

// The hero "Trailer" pill opens the first trailer (falling back to any video)
// in the same YouTube modal the Videos row uses.
const trailerVideo = computed(() => {
  const vids = detail.value?.videos ?? []
  return vids.find(v => /trailer/i.test(v.video_type || '')) || vids[0] || null
})
function openTrailer() {
  if (trailerVideo.value) openVideo(trailerVideo.value.video_key, trailerVideo.value.name)
}

// Autoplay is a motion trigger — skip it under prefers-reduced-motion so
// opening the trailer dialog doesn't immediately start moving video.
function videoEmbedSrc(key: string): string {
  const reduceMotion = typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  return `https://www.youtube-nocookie.com/embed/${key}?autoplay=${reduceMotion ? 0 : 1}&rel=0`
}

// Crossfade backdrops — shared carousel engine. HeroCanvas renders the sharp
// A/B pair and claims the shared blurred ambient underlay; the old
// ambientEnabled-gated background.set() is retired (HeroCanvas owns the claim).
const {
  showA, backdropA, backdropB, carouselPaused, cycleKey, backdropAssets,
  advanceBackdrop, retreatBackdrop, seedCarousel, openBackdropLightbox,
} = useBackdropCarousel(detail, { maxSortOrder: 1000 })

const currentHeroBackdrop = computed(() => (showA.value ? backdropA.value : backdropB.value) || null)

const { prefs } = useAppearance()

// ── Tone follow: publish --tone/--tone-rgb/--tone-ink on the page root.
// Primary source is the AmbientBackdrop's own sampled tone (useBackgroundTone),
// which re-samples on every crossfade; a direct sample of the current backdrop
// is the ambient-off fallback (sequence-guarded, Playbar's --pb-accent pattern).
const bgTone = useBackgroundTone()
const localTone = ref<ImageTone | null>(null)
let toneSeq = 0
watch(currentHeroBackdrop, (src) => {
  const seq = ++toneSeq
  if (!src) { localTone.value = null; return }
  sampleImageTone(src).then((t) => { if (seq === toneSeq) localTone.value = t })
}, { immediate: true })

const { toneFollowEnabled } = useAppearance()
const toneStyle = computed(() => {
  if (!toneFollowEnabled.value) return undefined
  const t = bgTone.value || localTone.value
  if (!t) return undefined
  const m = t.main.match(/\d+/g)
  if (!m) return undefined
  return toneStyleVars(t)
})

// Part of a collection → the full set as a bottom row (View collection
// covers the parts not in the library yet). Chronological by year.
const collectionQuery = useQuery(() => ({
  ...collectionDetailQuery(detail.value?.collection?.id ?? 0),
  enabled: !!detail.value?.collection?.id,
}))
const collectionMovies = computed<MediaItem[]>(() => {
  const list = collectionQuery.data.value?.movies ?? []
  return [...list].sort((a: any, b: any) => (Number(a.year) || 0) - (Number(b.year) || 0))
})

// "Part N of M" in the eyebrow — position within the (in-library) collection.
const collectionPart = computed<{ n: number; total: number } | null>(() => {
  if (!detail.value?.collection || !collectionMovies.value.length) return null
  const idx = collectionMovies.value.findIndex(m => m.id === detail.value!.media_item.id)
  if (idx < 0) return null
  return { n: idx + 1, total: collectionMovies.value.length }
})

// "More Like This": library titles only by default; the appearance toggle
// adds the rest as links to their strongest public metadata provider.
// Externals without a usable provider id are dropped rather than rendered —
// a card whose link resolves nowhere would open a junk tab.
const visibleRecs = computed(() => {
  const recs = detail.value?.recommendations ?? []
  if (!prefs.value.showUnavailableRecs) return recs.filter(r => r.local_media_item_id)
  return recs.filter(r => r.local_media_item_id || externalProviderUrl(r.media_type, r.external_ids))
})

function recTo(r: MediaRecommendation): string {
  if (r.local_media_item_id) {
    return mediaUrl({ id: r.local_media_item_id, public_id: r.local_public_id ?? undefined, title: r.title ?? '', slug: r.local_slug ?? undefined, media_type: r.media_type })
  }
  return externalProviderUrl(r.media_type, r.external_ids)
}

// Lightbox openers
function openPosterLightbox() {
  const src = usePosterUrl(detail.value!.media_item)
  if (src) lightbox.open(src)
}

const certification = computed(() => {
  if (detail.value?.preferred_certification) return detail.value.preferred_certification
  const certs = detail.value?.certifications
  if (!certs?.length) return null
  const us = certs.find((c: any) => c.country === 'US')
  return (us || certs[0])?.certification || null
})

const genres = computed(() => detail.value?.movie?.genres || [])

// Story uses the library-language overview the server resolved, falling back
// to the raw media_item description.
const storyText = computed(() => detail.value?.preferred_overview || detail.value?.media_item.description || '')

// Whether the Credits column has any #extra rows (so the column renders even
// when crew is empty but studio/budget/etc. exist).
const hasCreditExtras = computed(() => {
  const m = detail.value?.movie
  return !!(detail.value?.production_companies?.length || m?.original_language || m?.original_title || m?.budget || m?.revenue)
})

// Whether DetailLinksRow will render anything (collection or any external id).
const hasProvenanceLinks = computed(() => {
  if (detail.value?.collection) return true
  const ids = (detail.value?.media_item.external_ids ?? {}) as Record<string, unknown>
  return !!(ids.imdb || ids.tmdb || ids.tvdb || ids.anidb)
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

// ── Formatters ──────────────────────────────────────────────────────────────
const runtimeUpper = computed(() => {
  const mins = detail.value?.movie?.runtime_minutes
  if (!mins) return ''
  const h = Math.floor(mins / 60)
  const m = mins % 60
  return [h ? `${h}H` : '', m ? `${m}M` : ''].filter(Boolean).join(' ')
})

function formatMoney(n: number) {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(2)}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}K`
  return n.toLocaleString()
}

// ── Ledger (user-facing facts only) ─────────────────────────────────────────
function parseNum(v: unknown): number | null {
  if (v == null || v === '') return null
  const m = String(v).match(/-?\d+(\.\d+)?/)
  if (!m) return null
  const n = parseFloat(m[0])
  return isNaN(n) ? null : n
}
function fmtVotes(v?: number | null): string | undefined {
  if (!v) return undefined
  if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M votes`
  if (v >= 1_000) return `${Math.round(v / 1_000)}K votes`
  return `${v} votes`
}
function resLabel(h?: number) {
  if (!h) return '—'
  if (h >= 2160) return '4K'
  if (h >= 1440) return '1440P'
  if (h >= 1080) return '1080P'
  if (h >= 720) return '720P'
  if (h >= 576) return '576P'
  if (h >= 480) return '480P'
  return `${h}P`
}
function bitDepth(pix?: string) {
  if (!pix) return ''
  if (pix.includes('p12')) return '12-bit'
  if (pix.includes('p10')) return '10-bit'
  return '8-bit'
}
function chanLabel(ch?: number) {
  if (!ch) return ''
  return ({ 1: '1.0', 2: '2.0', 6: '5.1', 7: '6.1', 8: '7.1' } as Record<number, string>)[ch] || `${ch}ch`
}
function playbackUpper(a: string) {
  return ({ direct_play: 'DIRECT PLAY', remux: 'REMUX', transcode: 'TRANSCODE' } as Record<string, string>)[a] || a.toUpperCase()
}

// Headline ratings pulled from external_ratings (present ones only), in a
// stable priority order; each renders in the mockup's native unit.
const RATING_LEDGER: { src: string; alt?: string; label: string; kind: 'ten' | 'pct' | 'raw' }[] = [
  { src: 'imdb', label: 'IMDb', kind: 'ten' },
  { src: 'rotten_tomatoes', alt: 'rottentomatoes', label: 'Rotten Tomatoes', kind: 'pct' },
  { src: 'metacritic', label: 'Metacritic', kind: 'raw' },
  { src: 'tmdb', label: 'TMDB', kind: 'ten' },
]

const ledgerCells = computed<LedgerCell[]>(() => {
  const cells: LedgerCell[] = []

  const bySrc = new Map<string, ExternalRating>()
  for (const r of detail.value?.external_ratings ?? []) bySrc.set(r.source, r)
  for (const def of RATING_LEDGER) {
    const r = bySrc.get(def.src) || (def.alt ? bySrc.get(def.alt) : undefined)
    if (!r) continue
    const n = parseNum(r.value)
    if (n == null) continue
    if (def.kind === 'pct') cells.push({ k: def.label, v: String(Math.round(n)), unit: '%' })
    else if (def.kind === 'raw') cells.push({ k: def.label, v: String(Math.round(n)) })
    else cells.push({ k: def.label, v: n.toFixed(1), sub: def.src === 'imdb' ? fmtVotes(r.votes) : undefined })
  }

  if (runtimeUpper.value) cells.push({ k: 'Runtime', v: runtimeUpper.value })
  if (certification.value) cells.push({ k: 'Rated', v: certification.value })

  const si = streamInfo.value
  if (si) {
    const v = si.video?.[0]
    if (v) {
      const sub = [v.codec?.toUpperCase(), bitDepth(v.pix_fmt)].filter(Boolean).join(' · ')
      cells.push({ k: 'Video', v: resLabel(v.height), sub: sub || undefined })
    }
    const a = si.audio?.[0]
    if (a) {
      const sub = [chanLabel(a.channels), (a.language || '').toUpperCase()].filter(Boolean).join(' · ')
      cells.push({ k: 'Audio', v: a.codec?.toUpperCase() || '—', sub: sub || undefined })
    }
    if (si.playback?.action) cells.push({ k: 'Playback', v: playbackUpper(si.playback.action), tone: true })
  }
  return cells
})

async function loadStreamInfo() {
  if (!playableFileRef.value) { streamInfo.value = null; return }
  try {
    const caps = useClientCaps()
    const capsQuery = capsToQueryString(caps)
    const url = `/api/stream/${playableFileRef.value}/info${capsQuery ? `?${capsQuery}` : ''}`
    streamInfo.value = await $fetch<StreamInfoResponse>(url, {
      headers: withAuthHeaders(url),
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
}, { immediate: true })
</script>

<style scoped>
/* ═══ HERO ═══════════════════════════════════════════════════════════════
   The shared backdrop/carousel/zoom chrome (.hero-cycle, .hero-expand,
   .zoom-btn, .hero-section positioning, .hscroll, .scroll-ctrl-btn) lives in
   heya.css; only per-page deltas here. Hero text rides HeroCanvas's literal-
   dark art grade, so --oink keeps it light in every theme (dark/oled/light) —
   themed --ink would flip near-black in light mode and vanish. */
.movie-hero {
  min-height: 60vh;
  display: flex;
  align-items: flex-end;
  --oink: 233 236 242;
}

.hero-inner {
  position: relative;
  z-index: 2;
  width: 100%;
  padding: 120px var(--pad-fluid) 44px;
  display: flex;
  align-items: flex-end;
  gap: 44px;
}
.hero-inner > .grow { flex: 1; min-width: 0; }

/* .hero-cycle occlusion (topbar clearance) is now handled globally in
   heya.css — the hero is `hero-flush`, and the global rule tucks the cluster
   clear of the fixed glass topbar. No page-local override needed. */

/* poster record-card — layered directional shadow (heya2.css .postercard) */
.postercard {
  position: relative;
  flex: 0 0 232px;
  align-self: flex-end;
}
.postercard :deep(.poster) {
  width: 100%;
  border-radius: var(--r-md);
  overflow: hidden;
  box-shadow:
    0 0 0 1px rgb(var(--oink) / 0.16),
    10px 18px 34px -12px rgb(0 0 0 / 0.8),
    24px 44px 90px -20px rgb(0 0 0 / 0.95);
  transition: transform 0.18s ease, box-shadow 0.28s ease;
}
.postercard:hover :deep(.poster) { transform: translateY(-3px); }

/* mono eyebrow */
.eyebrow {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-bottom: 18px;
  font: 600 11.5px var(--font-mono);
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--tone);
}
.eyebrow a { color: rgb(var(--oink) / 0.55); transition: color 0.15s; }
.eyebrow a:hover { color: rgb(var(--oink) / 0.9); }
.eyebrow .sep { color: rgb(var(--oink) / 0.3); }

/* Archivo display title (+ logo-as-title art) */
.title {
  font-family: var(--font-display);
  font-size: clamp(2.5rem, 5.2vw, 4.4rem);
  font-weight: 800;
  font-variation-settings: "wdth" 115;
  letter-spacing: -0.022em;
  line-height: 0.99;
  text-wrap: balance;
  max-width: 18ch;
  color: rgb(var(--oink) / 0.98);
  text-shadow: 0 2px 30px rgb(0 0 0 / 0.45);
  margin: 0;
}
.title-art { line-height: 0; }
.title-logo {
  display: block;
  width: auto;
  height: auto;
  max-width: min(460px, 100%);
  max-height: 130px;
  object-fit: contain;
  object-position: left center;
  filter: drop-shadow(0 6px 24px rgb(0 0 0 / 0.55));
}

.tagline {
  margin-top: 14px;
  font-style: italic;
  color: rgb(var(--oink) / 0.6);
  font-size: 15.5px;
}

.metaline {
  margin-top: 14px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 12px;
  font: 500 12.5px var(--font-mono);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: rgb(var(--oink) / 0.72);
}
.metaline .dot { color: rgb(var(--tone-rgb) / 0.85); }
.metaline .genre {
  border-bottom: 1px solid rgb(var(--oink) / 0.25);
  padding-bottom: 1px;
  transition: color 0.15s, border-color 0.15s;
}
.metaline .genre:hover { color: rgb(var(--oink) / 0.95); border-color: rgb(var(--tone-rgb) / 0.6); }

/* actions */
.actions {
  margin-top: 26px;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

/* tone-glowing primary Play (heya2.css .btn-play) */
.btn-play {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 13px 26px 13px 20px;
  border: 0;
  border-radius: 999px;
  cursor: pointer;
  background: var(--tone);
  color: var(--tone-ink, #0a0c10);
  font: 650 14px var(--font-sans);
  letter-spacing: 0.01em;
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.45),
    0 0 24px rgb(var(--tone-rgb) / 0.4),
    6px 10px 36px -8px rgb(var(--tone-rgb) / 0.75);
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}
.btn-play:hover {
  transform: translateY(-1px);
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.6),
    0 0 40px rgb(var(--tone-rgb) / 0.6),
    8px 14px 48px -8px rgb(var(--tone-rgb) / 0.9);
}
.btn-play[disabled] {
  cursor: not-allowed;
  opacity: 0.4;
  box-shadow: 0 0 0 1px rgb(var(--ink) / 0.14);
  transform: none;
}
.btn-play .tri {
  width: 0; height: 0;
  border-left: 11px solid var(--tone-ink, #0a0c10);
  border-top: 7px solid transparent;
  border-bottom: 7px solid transparent;
}
.btn-play small { font: 500 11px var(--font-mono); opacity: 0.72; letter-spacing: 0.06em; }

/* tone-tinted secondary pills (heya2.css .pill) */
.pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 11px 18px;
  border-radius: 999px;
  cursor: pointer;
  border: 1px solid rgb(var(--tone-rgb) / 0.3);
  background: rgb(var(--tone-rgb) / 0.08);
  color: rgb(var(--oink) / 0.9);
  font: 550 13px var(--font-sans);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  box-shadow: 0 0 16px rgb(var(--tone-rgb) / 0.14), 5px 8px 22px -10px rgb(0 0 0 / 0.7);
  transition: border-color 0.15s, background 0.15s, box-shadow 0.15s, transform 0.15s, color 0.15s;
}
.pill:hover {
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.15);
  color: rgb(var(--oink));
  box-shadow: 0 0 24px rgb(var(--tone-rgb) / 0.28), 6px 10px 26px -10px rgb(0 0 0 / 0.75);
  transform: translateY(-1px);
}
.pill.icon { width: 42px; height: 42px; padding: 0; justify-content: center; }
.pill.icon.is-on { border-color: rgb(var(--tone-rgb) / 0.6); background: rgb(var(--tone-rgb) / 0.2); color: var(--tone); }

/* ═══ BODY ════════════════════════════════════════════════════════════════ */
.page { padding: 0 var(--pad-fluid) 90px; }
.section { margin-top: 52px; }

.cols {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(0, 1fr);
  gap: 56px;
  align-items: start;
}

.prose { font-size: 16px; line-height: 1.75; color: rgb(var(--ink) / 0.82); max-width: 64ch; }
.prose .lede::first-letter {
  font-family: var(--font-display);
  font-weight: 800;
  font-size: 3.1em;
  float: left;
  line-height: 0.82;
  padding: 6px 10px 0 0;
  color: var(--tone);
}
.prose-empty { font-size: 14px; color: rgb(var(--ink) / 0.5); font-style: italic; }

/* Extras — the grouped scrollers keep their per-group heads + expand toggle. */
.extras-group { margin-top: 22px; }
.extras-group:first-of-type { margin-top: 4px; }
.extras-group-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px; }
.extras-group-label { font-size: 11px; font-weight: 700; font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.08em; color: rgb(var(--ink) / 0.6); }
.tab-count { font-size: 10px; color: rgb(var(--ink) / 0.4); font-family: var(--font-mono); margin-left: 4px; }
.extra-card {
  display: flex; align-items: center; gap: 12px;
  padding: 10px; min-width: 280px;
  background: var(--bg-2); border: 1px solid var(--hair);
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
  width: 300px; text-align: left;
  background: none; border: none; cursor: pointer; color: inherit; padding: 0;
}
.video-play {
  position: absolute; inset: 0; z-index: 3;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.35); opacity: 0; transition: opacity 0.15s;
  color: #fff; pointer-events: none; /* on artwork — stays literal */
}
.video-card:hover .video-play { opacity: 1; }

.video-dialog .app-dialog-body { padding: 0; }
.video-dialog-iframe { width: 100%; aspect-ratio: 16 / 9; display: block; border: 0; }

/* The collection row (ContentRow) already carries the shadow-room trick; drop
   the default section top-gap so the section header sits tight to the rail. */
.collection-row { margin-top: 40px; }

/* Recs */
.rec-card { width: 150px; text-decoration: none; color: inherit; display: block; }
.rec-card.rec-external { opacity: 0.65; transition: opacity 0.15s; }
.rec-card.rec-external:hover { opacity: 1; }
.rec-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 20px 18px; }
.rec-grid .rec-card { width: auto; }

/* Ratings — the full color-graded panel, laid out as a responsive grid so it
   uses the section's width (MediaRatings' own layout is a single column). */
.ratings-wrap :deep(.ratings) {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
  gap: 10px;
  max-width: 900px;
}

/* Details footer (heya2.css .detail-grid) */
.detail-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 36px 56px; }
.detail-grid dt { font: 600 10.5px var(--font-mono); letter-spacing: 0.2em; text-transform: uppercase; color: rgb(var(--ink) / 0.45); margin-bottom: 10px; }
.detail-grid dd { font-size: 13.5px; line-height: 1.8; color: rgb(var(--ink) / 0.75); }
/* Component chips (MediaKeywords/DetailLinksRow) drop their own top margin
   inside a dd. */
.detail-grid dd :deep(.keywords),
.detail-grid dd :deep(.dlr) { margin-top: 0; }

.details-tech {
  margin-top: 40px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 340px);
  gap: 24px;
  align-items: start;
}

/* ═══ RESPONSIVE ══════════════════════════════════════════════════════════ */
@media (max-width: 1200px) {
  .details-tech { grid-template-columns: 1fr; }
}

/* Tablet: single-column story/details, hide the poster record-card (mockup). */
@media (max-width: 960px) {
  .postercard { display: none; }
  .hero-inner { padding: 96px var(--pad-fluid) 32px; gap: 28px; }
  .cols { grid-template-columns: 1fr; gap: 36px; }
  .detail-grid { grid-template-columns: 1fr 1fr; }
}

/* Phone: tighter hero, the primary CTA takes its own full-width row, every
   actionable button meets the 44px touch target. */
@media (max-width: 720px) {
  .movie-hero { min-height: 52vh; }
  .hero-inner { padding: 84px var(--pad-fluid) 26px; gap: 20px; }
  .tagline { display: none; }
  .metaline { font-size: 11.5px; }
  .actions { gap: 8px; row-gap: 10px; }
  .btn-play { flex: 1 1 100%; justify-content: center; height: 48px; }
  .pill.icon { width: 48px; height: 48px; }
  .detail-grid { grid-template-columns: 1fr; gap: 26px; }
  .details-tech { grid-template-columns: 1fr; }
}

/* Touch: swipe replaces the mouse-only scroll arrows; the fold/expand toggle
   stays (a real affordance on touch too). */
@media (pointer: coarse) {
  .scroll-controls .scroll-ctrl-btn:not(.expand) { display: none; }
}
</style>
