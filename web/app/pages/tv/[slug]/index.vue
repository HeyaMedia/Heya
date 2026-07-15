<template>
  <div v-if="loading" class="scroll hero-flush" style="height: 100%">
    <div style="height: 380px; background: var(--bg-2)" />
  </div>

  <!-- `hero-flush` opts this page out of the .app-main topbar offset so the
       series art rides up under the glass topbar (the hero's own inner padding
       keeps text clear of the bar) — mirrors the sibling season/episode pages.
       See heya.css .app-main. -->
  <div v-else-if="detail" class="scroll tv2 hero-flush" :style="toneStyle" style="height: 100%">
    <!-- ── HERO: crossfading backdrops as sharp art, hard-clipped at the seam ── -->
    <section class="tv-hero">
      <HeroCanvas :src="backdropA || ''" :src-b="backdropB" :show-a="showA" object-position="center 28%" />

      <!-- Backdrop tools: expand-to-lightbox + the shared prev/pause/next ring.
           Same functionality as the old hero, restyled top-right below the bar. -->
      <div v-if="backdropAssets.length" class="hero-tools">
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

      <div class="tv-hero-inner">
        <div class="postercard">
          <LoadingImage :src="usePosterUrl(detail.media_item)" :width="600" :quality="80" :alt="heroTitle" @error="hideBroken" />
          <button class="zoom-btn" aria-label="Expand poster" @click="openPosterLightbox"><Icon name="expand" :size="13" /></button>
        </div>

        <div class="grow">
          <div class="eyebrow">
            <span>{{ kindLabel }}</span>
            <template v-if="presentSeasonCount">
              <span class="sep">&middot;</span>
              <span>{{ presentSeasonCount }} season{{ presentSeasonCount !== 1 ? 's' : '' }}</span>
            </template>
            <template v-if="statusLabel">
              <span class="sep">&middot;</span>
              <span>{{ statusLabel }}</span>
            </template>
          </div>

          <h1 v-if="heroLogoUrl && !heroLogoFailed" class="title title-art">
            <LoadingImage :src="heroLogoUrl" :alt="heroTitle" :width="600" class="title-logo" @error="heroLogoFailed = true" />
          </h1>
          <h1 v-else class="title">{{ heroTitle }}</h1>

          <p v-if="detail.media_item.tagline" class="tagline">{{ detail.media_item.tagline }}</p>

          <p class="metaline">
            <span v-if="yearRange">{{ yearRange }}</span>
            <template v-if="certification">
              <span class="dot">&middot;</span><span>{{ certification }}</span>
            </template>
            <template v-if="rating">
              <span class="dot">&middot;</span>
              <span class="rating"><Icon name="star" :size="11" /> {{ rating }}</span>
            </template>
            <template v-if="genres.length">
              <span class="dot">&middot;</span>
              <NuxtLink v-for="g in genres" :key="g" :to="`/genre/${encodeURIComponent(g)}`" class="genre-link">{{ g }}</NuxtLink>
            </template>
          </p>

          <div class="actions">
            <button v-if="firstEpisodeFileRef" class="btn-play" @click="playFirstEpisode">
              <span class="tri" /> {{ episodeInProgress ? 'Resume' : 'Play' }}
              <small v-if="upNextSmall">{{ upNextSmall }}</small>
            </button>
            <button v-else class="btn-play" disabled><span class="tri" /> No Files</button>

            <button class="pill" @click="showListModal = true"><Icon name="plus" :size="15" /> My List</button>

            <button
              class="pill icon" :class="{ 'is-on': isFavorited }"
              :aria-label="isFavorited ? 'Remove from favorites' : 'Add to favorites'"
              :aria-pressed="isFavorited"
              :title="isFavorited ? 'Remove from favorites' : 'Add to favorites'"
              @click="toggleFavorite"
            >
              <Icon :name="isFavorited ? 'heartfill' : 'heart'" :size="16" />
            </button>
            <button
              class="pill icon" :class="{ 'is-on': showFullyWatched }"
              :aria-label="showFullyWatched ? 'Mark as unwatched' : 'Mark as watched'"
              :aria-pressed="showFullyWatched"
              :title="showFullyWatched ? 'Mark as unwatched' : 'Mark as watched'"
              @click="toggleShowWatched"
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

    <!-- ── LEDGER at the hard-clip seam — user-facing facts only ── -->
    <LedgerStrip :cells="ledgerCells" />

    <!-- ── BODY ── -->
    <main class="page">
      <!-- Story + Credits -->
      <section class="section">
        <div class="cols">
          <div>
            <SectionHeader title="Story" />
            <div v-if="storyText" class="prose"><p class="lede">{{ storyText }}</p></div>
            <p v-else class="prose-empty">No synopsis available.</p>
          </div>
          <div v-if="creditRows.length">
            <SectionHeader title="Credits" />
            <div class="credits">
              <div v-for="r in creditRows" :key="r.k" class="credit-row">
                <span class="ck">{{ r.k }}</span>
                <span class="cv">{{ r.values.join(' · ') }}</span>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- Seasons -->
      <section v-if="displaySeasons.length" class="section">
        <SectionHeader title="Seasons">
          <template #subtitle>{{ displaySeasons.length }}</template>
          <template v-if="seasonsOverflows || seasonsExpanded" #actions>
            <div class="scroll-controls">
              <button v-if="!seasonsExpanded" class="scroll-ctrl-btn" aria-label="Scroll left" @click="scrollSeasons('left')"><Icon name="chevleft" :size="14" /></button>
              <button v-if="!seasonsExpanded" class="scroll-ctrl-btn" aria-label="Scroll right" @click="scrollSeasons('right')"><Icon name="chevright" :size="14" /></button>
              <button class="scroll-ctrl-btn expand" aria-label="Toggle expanded view" :aria-expanded="seasonsExpanded" @click="seasonsExpanded = !seasonsExpanded">
                <Icon name="chevdown" :size="14" :style="{ transform: seasonsExpanded ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
              </button>
            </div>
          </template>
        </SectionHeader>

        <div ref="seasonsScrollEl" :class="seasonsExpanded ? 'seasons-grid' : 'card-rail'">
          <AppContextMenu v-for="s in displaySeasons" :key="s.season_number" :items="seasonContextItems(s)">
            <div class="season-card-wrap">
              <NuxtLink :to="seasonUrl(s)" class="season-card">
                <MediaCard
                  :idx="s.season_number"
                  :src="seasonPosterUrl(s)"
                  aspect="2/3"
                  :width="260"
                  :title="seasonLabel(s)"
                  :subtitle="seasonSubtitle(s)"
                  :progress-pct="seasonWatchInfo(s) ? seasonWatchPct(s) : 0"
                >
                  <template #badges>
                    <div v-if="seasonWatchInfo(s)" class="season-badge" :class="{ complete: seasonWatchInfo(s)!.remaining === 0 }">
                      <Icon v-if="seasonWatchInfo(s)!.remaining === 0" name="check" :size="10" />
                      <span v-else>{{ seasonWatchInfo(s)!.remaining }}</span>
                    </div>
                  </template>
                </MediaCard>
              </NuxtLink>
              <!-- Sibling of the NuxtLink, not a descendant — real buttons can't
                   nest inside a real anchor. -->
              <div class="season-overlay">
                <button
                  class="season-action" :class="{ loved: isSeasonFavorited(s) }"
                  :aria-label="isSeasonFavorited(s) ? 'Remove season from loved' : 'Add season to loved'"
                  :aria-pressed="isSeasonFavorited(s)"
                  @click.stop.prevent="toggleSeasonFavorite(s)"
                >
                  <Icon :name="isSeasonFavorited(s) ? 'heartfill' : 'heart'" :size="14" />
                </button>
                <button
                  class="season-action" :class="{ watched: seasonFullyWatched(s) }"
                  :aria-label="seasonFullyWatched(s) ? 'Mark season unwatched' : 'Mark season watched'"
                  :aria-pressed="seasonFullyWatched(s)"
                  @click.stop.prevent="toggleSeasonWatched(s)"
                >
                  <Icon name="check" :size="14" />
                </button>
                <button class="season-action" aria-label="Expand season poster" @click.stop.prevent="openSeasonLightbox(s)">
                  <Icon name="expand" :size="14" />
                </button>
              </div>
            </div>
          </AppContextMenu>
        </div>
      </section>

      <!-- Cast & Crew (shared component, consumed as-is) -->
      <section v-if="detail.cast?.length || detail.crew?.length" class="section">
        <CastCrewTabs :cast="detail.cast" :crew="detail.crew" />
      </section>

      <!-- Videos -->
      <section v-if="detail.videos?.length" class="section">
        <SectionHeader title="Videos">
          <template #subtitle>{{ detail.videos.length }}</template>
        </SectionHeader>
        <div class="card-rail">
          <button v-for="(v, i) in detail.videos" :key="v.id" class="vid-card" @click="openVideo(v.video_key, v.name)">
            <MediaCard
              :idx="i"
              :src="`https://img.youtube.com/vi/${v.video_key}/mqdefault.jpg`"
              aspect="16/9"
              :width="360"
              :title="v.name"
              :badge-tl="v.video_type"
            >
              <template #badges>
                <div class="video-play"><Icon name="play" :size="20" /></div>
              </template>
            </MediaCard>
          </button>
        </div>
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

      <!-- Recommendations: library titles by default; the appearance toggle adds
           external provider links for titles not in this library. -->
      <section v-if="visibleRecs.length" class="section">
        <SectionHeader title="More Like This">
          <template v-if="recsOverflows || recsExpanded" #actions>
            <div class="scroll-controls">
              <button v-if="!recsExpanded" class="scroll-ctrl-btn" aria-label="Scroll left" @click="scrollRecs('left')"><Icon name="chevleft" :size="14" /></button>
              <button v-if="!recsExpanded" class="scroll-ctrl-btn" aria-label="Scroll right" @click="scrollRecs('right')"><Icon name="chevright" :size="14" /></button>
              <button class="scroll-ctrl-btn expand" aria-label="Toggle expanded view" :aria-expanded="recsExpanded" @click="recsExpanded = !recsExpanded">
                <Icon name="chevdown" :size="14" :style="{ transform: recsExpanded ? 'rotate(180deg)' : '', transition: 'transform 0.2s' }" />
              </button>
            </div>
          </template>
        </SectionHeader>
        <div ref="recsScrollEl" :class="recsExpanded ? 'rec-grid' : 'card-rail'">
          <AppContextMenu v-for="r in visibleRecs" :key="r.id" :items="recContextItems(r)" :disabled="!r.local_media_item_id">
            <NuxtLink :to="recTo(r)" :target="r.local_media_item_id ? undefined : '_blank'" class="rec-card" :class="{ 'rec-external': !r.local_media_item_id }">
              <MediaCard
                :idx="r.id"
                :src="recPosterUrl(r)"
                aspect="2/3"
                :width="260"
                :title="r.title ?? 'Untitled'"
                :badge-tl="r.local_media_item_id ? '' : 'provider ↗'"
                :badge-tr="r.vote_average ? `★ ${formatVote(r.vote_average)}` : ''"
              />
            </NuxtLink>
          </AppContextMenu>
        </div>
      </section>

      <!-- Ratings — full external-ratings panel -->
      <section v-if="detail.external_ratings?.length" class="section">
        <SectionHeader title="Ratings" />
        <div class="ratings-panel"><MediaRatings :ratings="detail.external_ratings" /></div>
      </section>

      <!-- Details footer -->
      <section v-if="showDetails" class="section">
        <SectionHeader title="Details" />
        <dl class="detail-grid">
          <div v-if="keywords.length">
            <dt>Keywords</dt>
            <dd class="dd-keywords">
              <template v-for="(k, i) in keywords" :key="k.id">
                <NuxtLink :to="`/keyword/${encodeURIComponent(k.name)}`">{{ k.name }}</NuxtLink><span v-if="i < keywords.length - 1">, </span>
              </template>
            </dd>
          </div>
          <div v-if="certsList">
            <dt>Certifications</dt>
            <dd>{{ certsList }}</dd>
          </div>
          <div v-if="hasExternal">
            <dt>External</dt>
            <dd><DetailLinksRow :media-item="detail.media_item" /></dd>
          </div>
        </dl>
      </section>
    </main>

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
import type { ContextMenuItem, MediaDetail, MediaItem, MediaRecommendation, UserList } from '~~/shared/types'
import type { ImageTone } from '~/composables/useImageTone'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import { useQuery } from '@pinia/colada'
import { mediaDetailQuery } from '~/queries/media'

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const lightbox = useLightbox()

const { $heya } = useNuxtApp()
const detailQuery = useQuery(() => mediaDetailQuery(slug.value))
await waitForQuery(detailQuery)
const detail = computed<MediaDetail | null>(() => detailQuery.data.value ?? null)
const loading = computed(() => detailQuery.isPending.value)
watch(detailQuery.error, (err) => { if (err) navigateTo('/tv') }, { immediate: true })
const showMetadataEditor = ref(false)
const videoModal = ref<{ key: string; title: string } | null>(null)

const heroTitle = computed(() => detail.value?.preferred_title || detail.value?.media_item.title || '')
const kindLabel = computed(() => detail.value?.media_item.media_type === 'anime' ? 'Anime' : 'Series')

// A logo asset is title artwork; the detail payload tells us whether a logo row
// exists so pages without one keep their text heading without a probe/404.
const heroLogoFailed = ref(false)
const heroLogoUrl = computed(() => {
  if (!detail.value?.media_item || !detail.value.assets?.some(asset => asset.asset_type === 'logo')) return null
  return useImageUrl(detail.value.media_item, 'logo')
})
watch(heroLogoUrl, () => { heroLogoFailed.value = false })

// Live refresh — a debounced re-enrich folding in new seasons/episodes (or a
// metadata edit) lands server-side while this page is open. Filtered on this
// series' media_item_id so another item's update doesn't retrigger here. The
// season/episode pages share this ['media','detail', slug] key.
const seriesEntityId = computed(() => detail.value?.media_item.id ?? 0)
useLiveRefresh([
  {
    events: ['media.updated'],
    filter: (e) => {
      const payload = e.payload as { media_item_id?: number } | undefined
      return payload?.media_item_id === seriesEntityId.value
    },
    keys: [['media', 'detail', slug]],
  },
])

// ── Recommendations rail state ──────────────────────────────────────────────
const recsExpanded = ref(false)
const recsScrollEl = ref<HTMLElement | null>(null)
const recsOverflows = ref(false)

function checkRecsOverflow() {
  nextTick(() => {
    if (recsScrollEl.value && !recsExpanded.value) {
      recsOverflows.value = recsScrollEl.value.scrollWidth > recsScrollEl.value.clientWidth
    } else {
      recsOverflows.value = visibleRecs.value.length > 6
    }
  })
}

function scrollRecs(dir: 'left' | 'right') {
  if (!recsScrollEl.value) return
  const amount = recsScrollEl.value.clientWidth * 0.75
  recsScrollEl.value.scrollBy({ left: dir === 'left' ? -amount : amount, behavior: 'smooth' })
}

// ── Seasons rail state ──────────────────────────────────────────────────────
const seasonsExpanded = ref(false)
const seasonsScrollEl = ref<HTMLElement | null>(null)
const seasonsOverflows = ref(false)

function checkSeasonsOverflow() {
  nextTick(() => {
    if (seasonsScrollEl.value && !seasonsExpanded.value) {
      seasonsOverflows.value = seasonsScrollEl.value.scrollWidth > seasonsScrollEl.value.clientWidth
    } else {
      seasonsOverflows.value = displaySeasons.value.length > 6
    }
  })
}

function scrollSeasons(dir: 'left' | 'right') {
  if (!seasonsScrollEl.value) return
  const amount = seasonsScrollEl.value.clientWidth * 0.75
  seasonsScrollEl.value.scrollBy({ left: dir === 'left' ? -amount : amount, behavior: 'smooth' })
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

// Autoplay is a motion trigger — skip it under prefers-reduced-motion.
function videoEmbedSrc(key: string): string {
  const reduceMotion = typeof window !== 'undefined' && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  return `https://www.youtube-nocookie.com/embed/${key}?autoplay=${reduceMotion ? 0 : 1}&rel=0`
}

// ── Crossfade backdrops — shared carousel engine. HeroCanvas renders the A/B
// pair and owns the graded ambient claim; the CycleControls ring drives the
// 30s rotation clock. ────────────────────────────────────────────────────────
const {
  showA, backdropA, backdropB, carouselPaused, cycleKey, backdropAssets,
  advanceBackdrop, retreatBackdrop, seedCarousel, openBackdropLightbox,
} = useBackdropCarousel(detail, { maxSortOrder: 1000 })

const { prefs } = useAppearance()
const currentHeroBackdrop = computed(() => (showA.value ? backdropA.value : backdropB.value) || null)

// ── Tone follow: publish --tone/--tone-rgb/--tone-ink on the page root.
// Primary source is the AmbientBackdrop's own sampled tone (re-samples on every
// crossfade); a direct sample of the current backdrop is the ambient-off
// fallback (sequence-guarded, Playbar's --pb-accent pattern). ────────────────
const bgTone = useBackgroundTone()
const localTone = ref<ImageTone | null>(null)
let toneSeq = 0
watch(currentHeroBackdrop, (src) => {
  const seq = ++toneSeq
  if (!src) { localTone.value = null; return }
  sampleImageTone(src).then((t) => { if (seq === toneSeq) localTone.value = t })
}, { immediate: true })

const toneStyle = computed(() => {
  const t = bgTone.value || localTone.value
  if (!t) return undefined
  const m = t.main.match(/\d+/g)
  if (!m) return undefined
  return { '--tone': t.main, '--tone-rgb': m.slice(0, 3).join(' '), '--tone-ink': t.ink }
})

// ── Recommendations ─────────────────────────────────────────────────────────
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

// ── Lightbox openers ────────────────────────────────────────────────────────
function openPosterLightbox() {
  const src = usePosterUrl(detail.value!.media_item)
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

// Hero/ledger counts reflect what we actually hold, not the provider catalog
// (tv_series.number_of_episodes counts unaired episodes too). Regular seasons
// only; specials (season 0) are excluded to match the catalog convention.
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

const tmdbVotes = computed(() => {
  const r = (detail.value?.external_ratings || []).find(x => x.source === 'tmdb')
  return r?.votes || 0
})

const certification = computed(() => {
  if (detail.value?.preferred_certification) return detail.value.preferred_certification
  const certs = detail.value?.certifications
  if (!certs?.length) return null
  const us = certs.find((c: any) => c.country === 'US')
  return (us || certs[0])?.certification || null
})

const genres = computed(() => detail.value?.tv_series?.genres || [])

// Provider status can arrive snake_cased ("returning_series"); render it as
// spaced words (the eyebrow/ledger CSS uppercases it).
const statusLabel = computed(() => (detail.value?.tv_series?.status || '').replace(/_/g, ' '))

const yearRange = computed(() => {
  const s = detail.value?.tv_series
  const a = s?.first_air_date?.slice(0, 4)
  const b = s?.last_air_date?.slice(0, 4)
  if (a && b && a !== b) return `${a} – ${b}`
  return a || detail.value?.media_item.year || ''
})

const storyText = computed(() => detail.value?.preferred_overview || detail.value?.media_item.description || '')

// ── Credits (mockup-style rows, same data as MediaCrewSummary + its extras) ──
const creditRows = computed<{ k: string; values: string[] }[]>(() => {
  const rows: { k: string; values: string[] }[] = []
  const s = detail.value?.tv_series as any
  const crew = detail.value?.crew || []
  const byJob = (jobs: string[]) => {
    const names: string[] = []
    for (const c of crew) if (jobs.includes(c.job) && !names.includes(c.name)) names.push(c.name)
    return names
  }
  const createdBy: string[] = s?.created_by || []
  if (createdBy.length) rows.push({ k: 'Created by', values: createdBy.slice(0, 3) })
  const director = byJob(['Director'])
  if (director.length) rows.push({ k: 'Director', values: director.slice(0, 3) })
  const writer = byJob(['Writer', 'Screenplay'])
  if (writer.length) rows.push({ k: 'Writer', values: writer.slice(0, 3) })
  const music = byJob(['Original Music Composer'])
  if (music.length) rows.push({ k: 'Music', values: music.slice(0, 3) })
  const cinema = byJob(['Director of Photography'])
  if (cinema.length) rows.push({ k: 'Cinematography', values: cinema.slice(0, 3) })
  const studios = (detail.value?.production_companies || []).map((c: any) => c.name).filter(Boolean)
  if (studios.length) rows.push({ k: 'Studio', values: studios.slice(0, 3) })
  if (s?.first_air_date) rows.push({ k: 'First aired', values: [formatDate(s.first_air_date)] })
  const networks: string[] = s?.networks || []
  if (networks.length) rows.push({ k: 'Network', values: networks })
  return rows
})

// ── Details footer ──────────────────────────────────────────────────────────
const keywords = computed(() => detail.value?.keywords || [])
const certsList = computed(() => {
  const cs = detail.value?.certifications || []
  if (!cs.length) return ''
  return cs
    .map((c: any) => `${c.certification}${c.country ? ` (${c.country})` : ''}`.trim())
    .filter((x: string) => x && x !== '()')
    .join(' · ')
})
const hasExternal = computed(() => Object.keys(detail.value?.media_item.external_ids || {}).length > 0)
const showDetails = computed(() => keywords.value.length > 0 || !!certsList.value || hasExternal.value)

// ── Up-next / Play ──────────────────────────────────────────────────────────
interface UpNextData {
  has_next: boolean
  episode_id?: number
  episode_number?: number
  episode_title?: string
  season_number?: number
  media_item_id?: number
  file_id?: number
  file_public_id?: string
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

const firstEpisodeFileRef = computed(() => {
  if (!nextEpisodeKey.value || !detail.value?.episode_files) return null
  const entry = detail.value.episode_files[nextEpisodeKey.value]
  return entry?.file_public_id || entry?.file_id || null
})

const nextEpisodeLabel = computed(() => {
  if (!nextEpisodeKey.value) return ''
  const match = nextEpisodeKey.value.match(/^s(\d+)e(\d+)$/)
  if (!match) return ''
  const [, s, e] = match
  if (!s || !e) return ''
  return `S${s.padStart(2, '0')}E${e.padStart(2, '0')}`
})

const nextEpisodeTitle = computed(() => {
  const key = nextEpisodeKey.value
  if (!key) return ''
  const match = key.match(/^s(\d+)e(\d+)$/)
  if (!match) return ''
  const sNum = parseInt(match[1]!)
  const eNum = parseInt(match[2]!)
  const season = detail.value?.seasons?.find((s: any) => s.season_number === sNum)
  const ep = season?.episodes?.find((e: any) => e.episode_number === eNum)
  return ep?.preferred_title || ep?.title || upNext.value?.episode_title || ''
})

// Primary-button subtitle: "S01E03 · TITLE" (S01E03 · 22M LEFT when resuming).
const upNextSmall = computed(() => {
  if (!nextEpisodeLabel.value) return ''
  const t = nextEpisodeTitle.value
  return t ? `${nextEpisodeLabel.value} · ${t}` : nextEpisodeLabel.value
})

function playFirstEpisode() {
  if (!firstEpisodeFileRef.value || !detail.value || !nextEpisodeKey.value) return
  const params = new URLSearchParams({
    media_item_id: String(detail.value.media_item.id),
    title: `${detail.value.media_item.title} - ${nextEpisodeLabel.value}`,
  })
  if (upNext.value?.episode_id) {
    params.set('entity_type', 'episode')
    params.set('entity_id', String(upNext.value.episode_id))
  }
  navigateTo(`/watch/${firstEpisodeFileRef.value}?${params}`)
}

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

// ── Ledger cells (user-facing facts only) ───────────────────────────────────
function fmtVotes(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}K votes`
  return `${n} votes`
}

const ledgerCells = computed<LedgerCell[]>(() => {
  const cells: LedgerCell[] = []
  const s = detail.value?.tv_series as any
  const r = rating.value
  if (r) cells.push({ k: 'Rating', v: r, sub: tmdbVotes.value ? fmtVotes(tmdbVotes.value) : undefined })
  const present = presentEpisodeTotal.value
  if (present) {
    const catalog = s?.number_of_episodes
    cells.push({ k: 'In library', v: String(present), unit: catalog && catalog > present ? `of ${catalog} eps` : 'eps' })
  }
  if (presentSeasonCount.value) cells.push({ k: 'Seasons', v: String(presentSeasonCount.value) })
  if (s?.status) cells.push({ k: 'Status', v: statusLabel.value.toUpperCase(), sub: yearRange.value || undefined })
  const cert = certification.value
  if (cert) cells.push({ k: 'Rated', v: cert })
  const networks: string[] = s?.networks || []
  if (networks[0]) cells.push({ k: 'Network', v: networks[0] })
  if (nextEpisodeLabel.value) cells.push({ k: 'Next up', v: nextEpisodeLabel.value, tone: true, sub: nextEpisodeTitle.value || undefined })
  return cells
})

// ── Favorites / watched state ───────────────────────────────────────────────
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
  seasonFavorites.value = new Set(seasonFavorites.value)
}

// Season watched tracking
const seasonStates = ref<Map<number, { season_id: number; total_episodes: number; watched_episodes: number }>>(new Map())

const showFullyWatched = computed(() => {
  const seasons = detail.value?.seasons || []
  if (seasons.length === 0 || seasonStates.value.size === 0) return false
  return seasons.every((s: any) => seasonFullyWatched(s))
})

// Watched math is against the present-episode total, not the provider catalog.
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

const invalidateContinueWatching = useInvalidateContinueWatching()
const recUserLists = ref<UserList[]>([])
const recWatchedSet = ref<Set<number>>(new Set())
const recFavoritedSet = ref<Set<number>>(new Set())
const { buildItems: buildCardCtxItems } = useCardContextItems()

async function toggleSeasonWatched(s: any) {
  const watched = seasonFullyWatched(s)
  const { $heya } = useNuxtApp()
  await $heya('/api/me/watched/season/{id}', {
    method: 'POST',
    path: { id: s.id },
    body: { watched: !watched } as any,
  })
  await loadState()
  invalidateContinueWatching()
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
  invalidateContinueWatching()
}

// User lists — AddToListDialog owns loading/creation/toggling.
const showListModal = ref(false)

function seasonUrl(s: any) {
  const num = s.season_number === 0 ? 'specials' : String(s.season_number)
  return `/tv/${slug.value}/season/${num}`
}

function seasonContextItems(s: any): ContextMenuItem[] {
  const watched = seasonFullyWatched(s)
  const loved = isSeasonFavorited(s)
  return [
    { label: 'View Season', icon: 'info', action: () => navigateTo(seasonUrl(s)) },
    { label: '', separator: true },
    { label: watched ? 'Mark Season Unwatched' : 'Mark Season Watched', icon: 'eye', action: () => toggleSeasonWatched(s) },
    { label: loved ? 'Remove from Loved' : 'Add to Loved', icon: loved ? 'heartfill' : 'heart', action: () => toggleSeasonFavorite(s) },
    { label: 'View Poster', icon: 'expand', action: () => openSeasonLightbox(s) },
  ]
}

function recToMediaItem(r: any): MediaItem {
  return {
    id: r.local_media_item_id,
    public_id: r.local_public_id,
    title: r.title,
    slug: r.local_slug ?? undefined,
    year: r.year ?? '',
    media_type: r.media_type || 'tv',
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
        await $heya('/api/me/watched/media/{id}', { method: 'POST', path: { id }, body: { watched } as any })
        const next = new Set(recWatchedSet.value)
        if (watched) next.add(id)
        else next.delete(id)
        recWatchedSet.value = next
        if (id === detail.value?.media_item.id) await loadState()
        invalidateContinueWatching()
      } catch { /* ignore */ }
    },
    onToggleFavorite: async (id: number, favorited: boolean) => {
      try {
        await $heya('/api/me/favorites', { method: 'POST', body: { entity_type: 'media_item', entity_id: id } as any })
        const next = new Set(recFavoritedSet.value)
        if (favorited) next.add(id)
        else next.delete(id)
        recFavoritedSet.value = next
        if (id === detail.value?.media_item.id) isFavorited.value = favorited
      } catch { /* ignore */ }
    },
    onAddToList: async (listId: number, mediaId: number) => {
      try {
        await $heya('/api/me/lists/{id}/items', { method: 'POST', path: { id: listId }, body: { media_item_id: mediaId } as any })
      } catch { /* ignore */ }
    },
  })
}

async function loadRecommendationContextState() {
  try {
    const [stateRes, listsRes] = await Promise.allSettled([
      fetchUserState('series'),
      $heya('/api/me/lists') as Promise<UserList[]>,
    ])
    if (stateRes.status === 'fulfilled') {
      recWatchedSet.value = new Set((stateRes.value.shows || [])
        .filter(s => s.total_episodes > 0 && s.watched_episodes >= s.total_episodes)
        .map(s => s.media_item_id))
      recFavoritedSet.value = new Set(stateRes.value.favorited || [])
    }
    if (listsRes.status === 'fulfilled') recUserLists.value = listsRes.value
  } catch { /* ignore */ }
}

// Image URLs are unconditional: always emit the app's own image endpoint
// (walks media_assets, resolves the materialized season poster, and falls back
// to the show poster) rather than the raw provider `poster_path`, which can be
// a stale/unreachable provider URL. Mirrors the season detail page.
function seasonPosterUrl(s: NonNullable<MediaDetail['seasons']>[number]) {
  return `/api/media/${useMediaImageKey(detail.value?.media_item)}/image/poster?label=season-${s.season_number}`
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
function hideBroken(e: Event | string) {
  if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none'
}

// Re-run side effects whenever the detail data arrives or changes.
watch(detail, async (d) => {
  if (!d) return
  await nextTick()
  seedCarousel()
  loadState()
  loadRecommendationContextState()
  loadUpNext()
  checkRecsOverflow()
  checkSeasonsOverflow()
}, { immediate: true })

watch([visibleRecs, recsExpanded], () => checkRecsOverflow())
watch([displaySeasons, seasonsExpanded], () => checkSeasonsOverflow())
</script>

<style scoped>
/* ═══ HERO ═══════════════════════════════════════════════════════════════ */
.tv-hero {
  position: relative;
  min-height: 62vh;
  display: flex;
  align-items: flex-end;
  /* Over-art ink: the hero text rides the literal-dark art grade, so it stays
     light in every theme (dark/oled/light) — themed --ink would flip near-black
     in the light theme and vanish against the dark grade. */
  --oink: 233 236 242;
}

/* Backdrop tools cluster, top-right below the glass topbar. */
.hero-tools {
  position: absolute;
  top: calc(var(--topbar-h) + 14px);
  right: var(--pad-fluid);
  z-index: 5;
  display: flex;
  align-items: center;
  gap: 8px;
}
.hero-expand {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-1);
  background: color-mix(in oklab, var(--bg-2) 78%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-el);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.hero-expand:hover { background: var(--bg-3); color: var(--fg-0); }

.tv-hero-inner {
  position: relative;
  z-index: 2;
  width: 100%;
  padding: 120px var(--pad-fluid) 40px;
  display: flex;
  align-items: flex-end;
  gap: 44px;
}
.tv-hero-inner > .grow { flex: 1; min-width: 0; }

/* poster record-card */
.postercard {
  flex: 0 0 232px;
  position: relative;
  border-radius: var(--r-md);
}
.postercard :deep(img) {
  width: 100%;
  aspect-ratio: 2/3;
  object-fit: cover;
  border-radius: var(--r-md);
  background: var(--bg-2);
  box-shadow:
    0 0 0 1px rgb(var(--oink) / 0.16),
    10px 18px 34px -12px rgb(0 0 0 / 0.8),
    24px 44px 90px -20px rgb(0 0 0 / 0.95);
}
.zoom-btn {
  position: absolute;
  bottom: 10px;
  right: 10px;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background: rgb(6 7 10 / 0.6);
  backdrop-filter: blur(6px);
  border: 1px solid rgb(255 255 255 / 0.15);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s, background 0.15s;
}
.postercard:hover .zoom-btn { opacity: 1; }
.zoom-btn:hover { background: rgb(6 7 10 / 0.85); }

/* mono eyebrow */
.eyebrow {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
  font: 600 11.5px var(--font-mono);
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--tone);
}
.eyebrow .sep { color: rgb(var(--oink) / 0.3); }

/* Archivo display title / logo */
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
}
.title-art { line-height: 0; min-height: 96px; display: flex; align-items: flex-end; }
.title-logo {
  display: block;
  width: auto;
  height: auto;
  max-width: min(460px, 100%);
  max-height: 132px;
  object-fit: contain;
  object-position: left bottom;
  filter: drop-shadow(0 6px 24px rgb(0 0 0 / 0.55));
}

.tagline {
  margin-top: 14px;
  font-style: italic;
  color: rgb(var(--oink) / 0.62);
  font-size: 15.5px;
  max-width: 60ch;
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
.metaline .rating { display: inline-flex; align-items: center; gap: 4px; color: var(--tone); }
.metaline .genre-link {
  border-bottom: 1px solid rgb(var(--oink) / 0.25);
  padding-bottom: 1px;
  transition: color 0.15s, border-color 0.15s;
}
.metaline .genre-link:hover { color: rgb(var(--oink)); border-color: rgb(var(--tone-rgb) / 0.6); }

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
.btn-play small { font: 500 11px var(--font-mono); opacity: 0.72; letter-spacing: 0.06em; text-transform: uppercase; }

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
  gap: 52px;
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

/* credits (heya2.css .credits) */
.credits { border-top: 1px solid var(--hair); }
.credit-row {
  display: grid;
  grid-template-columns: 130px 1fr;
  gap: 18px;
  padding: 11px 0;
  border-bottom: 1px solid var(--hair);
  align-items: baseline;
}
.credit-row .ck {
  font: 600 10.5px var(--font-mono);
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.45);
}
.credit-row .cv { font-size: 14px; color: rgb(var(--ink) / 0.88); }

/* rails (shadow-room trick: pronounced directional shadows clear the scroller
   edge). Applies to seasons/videos/recs rails. */
.card-rail {
  display: flex;
  align-items: flex-start;
  gap: 18px;
  overflow-x: auto;
  padding: 30px 44px 60px;
  margin: -30px -44px -44px;
  scrollbar-width: none;
}
.card-rail::-webkit-scrollbar { display: none; }

/* season tiles — MediaCard supplies the embedded label + progress; the wrap
   carries the directional float shadow + hover elevation. In a `.card-rail` (a
   scroll container: overflow-x:auto forces overflow-y:auto), the tile needs an
   EXPLICIT height, not just width: MediaCard's Poster paints via an
   absolutely-positioned <img> + aspect-ratio, so it contributes no in-flow
   intrinsic height for the flex line to measure and the card would overflow a
   collapsed rail. Height = width × 3/2. The expanded grid needs no height —
   grid rows size to the Poster's aspect-ratio (proven by the old grid). */
.seasons-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(158px, 1fr)); gap: 22px 18px; }
.card-rail .season-card-wrap { width: 178px; height: 267px; flex-shrink: 0; }
.season-card-wrap {
  position: relative;
  border-radius: 10px;
  box-shadow: var(--shadow-card);
  transition: transform 0.18s ease, box-shadow 0.28s ease;
}
.season-card-wrap:hover { transform: translateY(-4px); box-shadow: var(--shadow-card-hover); }
.season-card { display: block; border-radius: 10px; overflow: hidden; text-decoration: none; color: inherit; }

/* Season badge (episodes remaining / checkmark) — slotted into MediaCard. */
.season-badge {
  position: absolute; top: 8px; left: 8px; z-index: 3;
  min-width: 22px; height: 22px; padding: 0 6px;
  border-radius: 100px; font-size: 11px; font-weight: 700; font-family: var(--font-mono);
  background: rgb(6 7 10 / 0.7); backdrop-filter: blur(6px); color: #fff;
  display: flex; align-items: center; justify-content: center;
}
.season-badge.complete { background: rgb(var(--tone-rgb) / 0.9); color: #0a0c10; }

/* Season hover actions — sibling overlay of the NuxtLink (real buttons can't
   nest inside a real anchor). */
.season-overlay {
  position: absolute; top: 8px; right: 8px; z-index: 4;
  display: flex; gap: 4px;
  opacity: 0; transition: opacity 0.15s;
}
.season-card-wrap:hover .season-overlay { opacity: 1; }
.season-action {
  width: 26px; height: 26px; border-radius: var(--r-sm);
  background: rgb(6 7 10 / 0.6); backdrop-filter: blur(6px);
  color: rgb(255 255 255 / 0.78);
  display: flex; align-items: center; justify-content: center;
  cursor: pointer; transition: background 0.15s, color 0.15s;
}
.season-action:hover { background: rgb(6 7 10 / 0.85); color: #fff; }
.season-action.loved { color: var(--bad); }
.season-action.watched { color: var(--tone); }

/* video tiles */
.vid-card {
  width: 300px;
  height: 169px;
  flex-shrink: 0;
  position: relative;
  text-align: left;
  background: none; border: 0; padding: 0; color: inherit; cursor: pointer;
  border-radius: 9px;
  box-shadow: 0 0 0 1px rgb(var(--ink) / 0.1), 7px 14px 30px -12px rgb(var(--shade) / 0.8);
  transition: transform 0.18s ease, box-shadow 0.28s ease;
}
.vid-card:hover { transform: translateY(-3px); box-shadow: 0 0 0 1px rgb(var(--ink) / 0.16), 10px 18px 34px -12px rgb(var(--shade) / 0.85), 0 0 26px rgb(var(--tone-rgb) / 0.14); }
.vid-card :deep(.poster) { border-radius: 9px; overflow: hidden; }
.video-play {
  position: absolute; inset: 0; z-index: 3;
  display: flex; align-items: center; justify-content: center;
  background: rgb(0 0 0 / 0.35); opacity: 0; transition: opacity 0.15s;
  color: #fff; pointer-events: none;
}
.vid-card:hover .video-play { opacity: 1; }

/* Video dialog — iframe edge-to-edge, 16:9. */
.video-dialog .app-dialog-body { padding: 0; }
.video-dialog-iframe { width: 100%; aspect-ratio: 16 / 9; display: block; border: 0; }

/* recs */
.card-rail .rec-card { width: 148px; height: 222px; flex-shrink: 0; }
.rec-card {
  position: relative;
  text-decoration: none; color: inherit; display: block;
  border-radius: 9px;
  box-shadow: var(--shadow-card);
  transition: transform 0.18s ease, box-shadow 0.28s ease;
}
.rec-card :deep(.poster) { border-radius: 9px; overflow: hidden; }
.rec-card:hover { transform: translateY(-4px); box-shadow: var(--shadow-card-hover); }
.rec-card.rec-external { opacity: 0.62; }
.rec-card.rec-external:hover { opacity: 1; }
.rec-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 22px 18px; }

/* ratings panel */
.ratings-panel { max-width: 480px; }

/* details footer (heya2.css .detail-grid) */
.detail-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 36px 56px; }
.detail-grid dt {
  font: 600 10.5px var(--font-mono);
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.45);
  margin-bottom: 8px;
}
.detail-grid dd { font-size: 13.5px; line-height: 1.8; color: rgb(var(--ink) / 0.75); }
.dd-keywords a { border-bottom: 1px solid rgb(var(--ink) / 0.18); transition: color 0.15s, border-color 0.15s; }
.dd-keywords a:hover { color: var(--tone); border-color: rgb(var(--tone-rgb) / 0.5); }

/* ═══ RESPONSIVE ══════════════════════════════════════════════════════════ */
@media (max-width: 1200px) {
  .cols { grid-template-columns: 1fr; gap: 36px; }
}

@media (max-width: 960px) {
  .tv-hero-inner { padding: 100px var(--pad-fluid) 30px; gap: 28px; }
  .postercard { flex-basis: 184px; }
  .title { font-size: clamp(2rem, 6vw, 3rem); }
}

@media (max-width: 720px) {
  .tv-hero { min-height: 54vh; }
  .tv-hero-inner { padding: 84px var(--pad-fluid) 26px; gap: 20px; }
  .postercard { display: none; }
  .tagline { display: none; }
  .actions { gap: 8px; }
  .btn-play { height: 48px; padding: 0 22px 0 18px; flex: 1 1 100%; white-space: normal; }
  .btn-play small { display: none; }
  /* Play takes its own full-width row; My List + the icon pills wrap onto the
     next row (all reachable — My List must not vanish on phone). */
  .pill.icon { width: 48px; height: 48px; }
  .card-rail { padding: 24px 16px 50px; margin: -24px -16px -36px; }
  .card-rail .season-card-wrap { width: 132px; height: 198px; }
  .card-rail .rec-card { width: 124px; height: 186px; }
  .vid-card { width: 240px; height: 135px; }
  .detail-grid { grid-template-columns: 1fr; gap: 26px; }
}

/* Touch: hover-only affordances need a permanent visible state. */
@media (pointer: coarse) {
  .scroll-controls .scroll-ctrl-btn:not(.expand) { display: none; }
  .season-overlay { opacity: 1; transition: none; }
  .zoom-btn { opacity: 1; }
  .season-action { position: relative; }
  .season-action::before { content: ''; position: absolute; inset: -10px; }
}
</style>
