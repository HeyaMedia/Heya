<template>
  <div class="ch-scroll scroll">
    <div v-if="loading" class="ch-hero-skeleton" />

    <template v-else-if="collection">
      <!-- Hero: crossfades through the backdrop of each film we own in the
           franchise (falls back to the collection's own backdrop). Poster +
           franchise name + aggregated genres + watch progress + "up next" CTA
           sit over the gradient. -->
      <div class="ch-hero">
        <div class="ch-bd-wrap" :class="{ 'ambient-extended': ambientEnabled }">
          <div class="ch-bd" :class="{ 'ch-bd-on': showA }" :style="bdStyle(bdA)" />
          <div class="ch-bd" :class="{ 'ch-bd-on': !showA }" :style="bdStyle(bdB)" />
          <div class="ch-hero-fade" />
        </div>

        <!-- Backdrop cycle cluster — the shared prev/pause/next controls. -->
        <CycleControls
          v-if="backdropUrls.length > 1"
          v-model:paused="carouselPaused"
          :cycle-key="cycleKey"
          :duration="BACKDROP_INTERVAL"
          item-label="backdrop"
          class="hero-cycle"
          @prev="retreat"
          @next="advance"
        />

        <div class="ch-hero-body page-pad">
          <div v-if="collection.poster_path" class="ch-poster">
            <NuxtImg :src="collection.poster_path" alt="" @error="onImgError" />
          </div>

          <div class="ch-info">
            <div class="ch-eyebrow">Franchise</div>
            <h1 class="ch-title">{{ franchiseLabel(collection.name) }}</h1>

            <div v-if="genres.length || heroKeywords.length" class="ch-tagrow">
              <span v-for="g in genres" :key="'g-' + g" class="ch-chip ch-chip-genre">{{ g }}</span>
              <NuxtLink
                v-for="k in heroKeywords"
                :key="'k-' + k"
                :to="`/keyword/${encodeURIComponent(k)}`"
                class="ch-chip ch-chip-tag"
              >{{ k }}</NuxtLink>
            </div>

            <div class="ch-stats">
              <span v-if="hasFullMembership">{{ ownedCount }} of {{ parts.length }} films in your library</span>
              <span v-else>{{ films.length }} {{ films.length === 1 ? 'film' : 'films' }}</span>
              <template v-if="ownedParts.length">
                <span class="ch-dot">·</span>
                <span>{{ allSeen ? 'Seen them all' : `Seen ${seenCount} of ${ownedParts.length}` }}</span>
              </template>
            </div>
            <div v-if="ownedParts.length" class="ch-progress">
              <div class="ch-progress-fill" :style="{ width: progressPct + '%' }" />
            </div>

            <p v-if="collection.overview" class="ch-overview">{{ collection.overview }}</p>

            <div class="ch-cta">
              <NuxtLink v-if="nextUp" :to="partUrl(nextUp)" class="ch-cta-btn" :style="ctaStyle">
                <Icon name="play" :size="15" />
                <span>{{ ctaVerb }}: {{ nextUp.title }}</span>
              </NuxtLink>
              <div v-else-if="allSeen" class="ch-complete">
                <Icon name="check" :size="15" /> You've seen every film in the library
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- Films in release order — owned link to the movie + track watched;
           missing render dimmed. The next unwatched owned film is flagged. -->
      <div class="page-pad ch-list-wrap">
        <div class="ch-list-head">
          <h2 class="ch-list-title">Films</h2>
          <span class="ch-list-sub">Release order</span>
        </div>

        <div class="ch-list">
          <component
            :is="p.local_media_item_id ? NuxtLink : 'div'"
            v-for="(p, i) in films"
            :key="p.tmdb_id || p.title"
            :to="p.local_media_item_id ? partUrl(p) : undefined"
            class="ch-row"
            :class="{ 'ch-row-missing': !p.local_media_item_id, 'ch-row-next': p === nextUp, 'ch-row-seen': isWatched(p) }"
          >
            <div class="ch-row-idx">{{ i + 1 }}</div>
            <div class="ch-row-poster">
              <NuxtImg v-if="partPoster(p)" :src="partPoster(p)" alt="" @error="onImgError" />
            </div>
            <div class="ch-row-main">
              <div class="ch-row-title">
                <span class="ch-row-name">{{ p.title }}</span>
                <span v-if="p === nextUp" class="ch-next-badge">Up next</span>
              </div>
              <div class="ch-row-meta">
                <span v-if="p.year">{{ p.year }}</span>
                <span v-if="p.vote_average" class="ch-star"><Icon name="star" :size="10" weight="fill" />{{ p.vote_average.toFixed(1) }}</span>
                <span v-if="!p.local_media_item_id" class="ch-missing-tag">Not in library</span>
              </div>
            </div>
            <button
              v-if="p.local_media_item_id"
              type="button"
              class="ch-watch-toggle"
              :class="{ on: isWatched(p) }"
              :aria-label="isWatched(p) ? 'Mark unwatched' : 'Mark watched'"
              :title="isWatched(p) ? 'Mark unwatched' : 'Mark watched'"
              @click.prevent.stop="toggleWatched(p)"
            >
              <Icon name="check" :size="14" />
            </button>
          </component>
        </div>
      </div>
    </template>

    <div v-else class="ch-notfound">
      <p>Collection not found</p>
      <button class="btn btn-secondary" @click="$router.back()">Go back</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'
import type { ImageTone } from '~/composables/useImageTone'
import { useQuery } from '@pinia/colada'
import { collectionDetailQuery } from '~/queries/discovery'

interface CollectionDetail {
  id: number
  name: string
  overview: string
  poster_path: string
  backdrop_path: string
}

// One franchise film, resolved server-side to a local movie (owned) or not.
interface CollectionPart {
  title: string
  year?: number
  tmdb_id?: number
  poster_path?: string
  vote_average?: number
  local_media_item_id?: number | null
  local_public_id?: string | null
  local_slug?: string | null
}

const route = useRoute()
const { $heya } = useNuxtApp()
const id = computed(() => Number(route.params.id))

// Resolve NuxtLink to the real component — `<component :is="'NuxtLink'">` with a
// string name renders an inert <nuxtlink> element that doesn't navigate.
const NuxtLink = resolveComponent('NuxtLink')

const collection = ref<CollectionDetail | null>(null)
const parts = ref<CollectionPart[]>([])
const movies = ref<MediaItem[]>([])
const genres = ref<string[]>([])
const keywords = ref<string[]>([])
const ownedCount = ref(0)
const loading = ref(true)
const watchedIds = ref<Set<number>>(new Set())

// The collection payload is a real destination query now: intent hovering can
// warm it, and a cold navigation remains on the previous page until it lands.
// User watch state is awaited alongside it so the first collection paint does
// not briefly show every film as unwatched.
const collectionQuery = useQuery(() => collectionDetailQuery(id.value))
const watchedStatePromise = fetchUserState('movies')
const [, watchedState] = await Promise.allSettled([
  waitForQuery(collectionQuery),
  watchedStatePromise,
])
const payload = collectionQuery.data.value
if (payload) {
  collection.value = payload.collection
  parts.value = payload.parts || []
  movies.value = payload.movies || []
  genres.value = payload.genres || []
  keywords.value = payload.keywords || []
  ownedCount.value = payload.owned_count || 0
}
if (watchedState.status === 'fulfilled') watchedIds.value = new Set(watchedState.value.watched || [])
loading.value = false

// NuxtImg types its `error` payload as `string | Event`; narrow before use.
function onImgError(e: Event | string) {
  if (typeof e === 'string') return
  const el = e.target as HTMLImageElement
  el.style.visibility = 'hidden'
}

// heya.media's franchise membership (parts) is the source of truth once a member
// movie has been enriched with the collection block. Until then parts is empty,
// so fall back to the local owned movies (release order) as synthetic parts —
// the page stays useful during the metadata backfill instead of showing nothing.
const films = computed<CollectionPart[]>(() => {
  if (parts.value.length) return parts.value
  return movies.value.map(m => ({
    title: m.title,
    year: m.year ? Number(m.year) : undefined,
    local_media_item_id: m.id,
    local_public_id: m.public_id,
    local_slug: m.slug,
  }))
})
const hasFullMembership = computed(() => parts.value.length > 0)
// Keywords shown in the hero — genres are broad, keywords are many, so cap the
// tag cluster to the most-common few to keep the hero clean.
const heroKeywords = computed(() => keywords.value.slice(0, 12))

// ── Watch tracking ──────────────────────────────────────────────────────
const ownedParts = computed(() => films.value.filter(p => p.local_media_item_id))
const seenCount = computed(() => ownedParts.value.filter(p => watchedIds.value.has(p.local_media_item_id!)).length)
const allSeen = computed(() => ownedParts.value.length > 0 && seenCount.value === ownedParts.value.length)
const progressPct = computed(() => ownedParts.value.length ? Math.round((seenCount.value / ownedParts.value.length) * 100) : 0)
// Next film to watch: the first owned part, in release order, not yet seen.
const nextUp = computed(() => ownedParts.value.find(p => !watchedIds.value.has(p.local_media_item_id!)) ?? null)
const ctaVerb = computed(() => seenCount.value === 0 ? 'Start' : 'Continue')

function isWatched(p: CollectionPart) {
  return !!p.local_media_item_id && watchedIds.value.has(p.local_media_item_id)
}
function partUrl(p: CollectionPart) {
  return mediaUrl({ id: p.local_media_item_id!, title: p.title, slug: p.local_slug ?? undefined, media_type: 'movie' })
}
// Owned films use our local artwork; missing ones use heya.media's CDN poster.
function partPoster(p: CollectionPart) {
  if (p.local_media_item_id || p.local_public_id) return usePosterUrl({ id: p.local_media_item_id, public_id: p.local_public_id }) ?? ''
  return p.poster_path || ''
}

async function toggleWatched(p: CollectionPart) {
  const mid = p.local_media_item_id
  if (!mid) return
  const mark = !watchedIds.value.has(mid)
  const next = new Set(watchedIds.value)
  if (mark) next.add(mid); else next.delete(mid)
  watchedIds.value = next // optimistic
  try {
    await $heya('/api/me/watched/media/{id}', { method: 'POST', path: { id: mid }, body: { watched: mark } as any })
  } catch {
    const rollback = new Set(watchedIds.value)
    if (mark) rollback.delete(mid); else rollback.add(mid)
    watchedIds.value = rollback
  }
}

// ── Backdrop carousel — cycle each owned film's backdrop ─────────────────
const backdropUrls = computed(() => {
  const urls = ownedParts.value
    .map(p => useBackdropUrl({ id: p.local_media_item_id, public_id: p.local_public_id }))
    .filter((u): u is string => !!u)
  if (urls.length) return urls
  return collection.value?.backdrop_path ? [collection.value.backdrop_path] : []
})

const showA = ref(true)
const bdA = ref<string | null>(null)
const bdB = ref<string | null>(null)
const bdIdx = ref(0)
const cycleKey = ref(0)
const carouselPaused = ref(false)

function bdStyle(url: string | null) {
  return url ? { backgroundImage: `url("${url}")` } : {}
}
// The clock is CycleControls' ring (BACKDROP_INTERVAL): its animationend
// calls advance(); every move re-keys the ring for a fresh window. Pausing
// freezes the ring, so nothing fires while paused — no timer bookkeeping.
function showIdx(idx: number) {
  const url = backdropUrls.value[idx] ?? null
  bdIdx.value = idx
  if (showA.value) bdB.value = url; else bdA.value = url
  showA.value = !showA.value
  cycleKey.value++
}
function advance() {
  const n = backdropUrls.value.length
  if (n <= 1) return
  showIdx((bdIdx.value + 1) % n)
}
function retreat() {
  const n = backdropUrls.value.length
  if (n <= 1) return
  showIdx((bdIdx.value - 1 + n) % n)
}
function seedCarousel() {
  const urls = backdropUrls.value
  bdIdx.value = 0
  bdA.value = urls[0] ?? null
  bdB.value = urls[0] ?? null
  showA.value = true
  cycleKey.value++
}

// Ambient extension + artwork-adaptive CTA, same recipe as the detail
// heroes: the current backdrop becomes the full-page layer (the local copy
// hides via .ambient-extended), and the Start/Continue button wears its
// dominant tone. External CDN backdrops that block canvas readback simply
// fall back to the gold coat.
const { ambientEnabled } = useAppearance()
const background = useBackground()
const currentBd = computed(() => (showA.value ? bdA.value : bdB.value) || null)
watch([currentBd, ambientEnabled], ([url, on]) => {
  if (on && url) background.set(url)
  else background.clear()
}, { immediate: true })

const heroTone = ref<ImageTone | null>(null)
watch(currentBd, async (url) => {
  heroTone.value = url ? await sampleImageTone(url) : null
}, { immediate: true })
const ctaStyle = computed(() =>
  heroTone.value ? { background: heroTone.value.main, color: heroTone.value.ink } : undefined)

onMounted(async () => {
  await nextTick()
  seedCarousel()
})
</script>

<style scoped>
.ch-scroll { height: 100%; }

/* ── Hero ─────────────────────────────────────────────────────────────── */
.ch-hero {
  position: relative;
  min-height: 480px;
  display: flex;
  align-items: flex-end;
  /* NO overflow clipping here — the poster's drop shadow bleeds past the
     hero bottom by design (a clip renders it as a hard line). The scaled
     backdrop images are clipped by .ch-bd-wrap instead. */
}
.ch-hero-skeleton { height: 480px; background: var(--bg-2); }
.ch-bd-wrap { position: absolute; inset: 0; overflow: hidden; }
.ch-bd {
  position: absolute; inset: 0;
  background-size: cover; background-position: center 22%;
  opacity: 0; transition: opacity 1.3s ease;
  transform: scale(1.04);
}
.ch-bd-on { opacity: 1; }
.ch-hero-fade {
  position: absolute; inset: 0;
  background:
    linear-gradient(to top, var(--bg-1) 3%, color-mix(in srgb, var(--bg-1) 55%, transparent) 34%, transparent 72%),
    linear-gradient(to right, var(--bg-1) 2%, color-mix(in srgb, var(--bg-1) 40%, transparent) 42%, transparent 68%);
}
/* Ambient extension: the AmbientBackdrop layer owns the artwork full-page —
   the hero paints nothing of its own (a local copy would seam at the hero's
   edges against the continuing full-page art). */
.ch-bd-wrap.ambient-extended .ch-bd,
.ch-bd-wrap.ambient-extended .ch-hero-fade { display: none; }
.ch-hero-body {
  position: relative; z-index: 2;
  display: flex; gap: 34px; align-items: flex-end;
  width: 100%; padding-top: 120px; padding-bottom: 40px;
}
.ch-poster {
  width: 184px; flex-shrink: 0;
  border-radius: var(--r-md); overflow: hidden;
  /* Shade-token shadow (theme-aware) instead of literal black — it bleeds
     past the hero bottom now, over whatever canvas/artwork sits there. */
  box-shadow: 0 18px 50px rgb(var(--shade) / 0.45), 0 0 0 1px rgb(var(--ink) / 0.06);
  aspect-ratio: 2/3; background: var(--bg-3);
}
.ch-poster img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ch-info { display: flex; flex-direction: column; min-width: 0; max-width: 720px; }
.ch-eyebrow {
  font-size: 10px; font-family: var(--font-mono); font-weight: 700;
  letter-spacing: 0.18em; text-transform: uppercase; color: var(--gold);
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1);
}
.ch-title {
  font-size: 40px; font-weight: 700; letter-spacing: -0.02em; margin: 4px 0 0; line-height: 1.05;
  text-shadow: 0 2px 20px rgb(var(--shade) / 0.30), 0 0 14px var(--bg-1);
}

/* One integrated cluster: genres read brighter/heavier, keyword tags dimmer
   and lowercase (and link to their keyword page). Theme glass — the old
   literal white washes were invisible chips on the light theme's paper. */
.ch-tagrow { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 14px; max-width: 660px; }
.ch-chip {
  padding: 3px 11px; border-radius: 100px; font-size: 11.5px; line-height: 1.55;
  border: 1px solid var(--border);
  backdrop-filter: blur(10px); -webkit-backdrop-filter: blur(10px);
  box-shadow: var(--shadow-el);
  text-decoration: none; white-space: nowrap;
}
.ch-chip-genre { background: color-mix(in oklab, var(--bg-2) 85%, transparent); color: var(--fg-0); font-weight: 500; }
.ch-chip-tag { background: color-mix(in oklab, var(--bg-2) 62%, transparent); color: var(--fg-1); transition: color 0.13s ease, border-color 0.13s ease; }
.ch-chip-tag:hover { color: var(--gold); border-color: var(--gold); }

.ch-stats {
  display: flex; align-items: center; gap: 8px; margin-top: 16px;
  font-size: 12.5px; font-family: var(--font-mono); color: var(--fg-1);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
.ch-dot { opacity: 0.5; }
.ch-progress {
  margin-top: 8px; width: 260px; max-width: 100%; height: 4px;
  background: rgb(var(--ink) / 0.15); border-radius: 100px; overflow: hidden;
  box-shadow: 0 0 0 1px rgb(var(--shade) / 0.18);
}
.ch-progress-fill { height: 100%; background: var(--gold); border-radius: 100px; transition: width 0.3s ease; }

.ch-overview {
  margin: 16px 0 0; font-size: 14px; line-height: 1.65; color: var(--fg-1);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
  display: -webkit-box; -webkit-line-clamp: 3; line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
}

.ch-cta { margin-top: 20px; }
.ch-cta-btn {
  display: inline-flex; align-items: center; gap: 9px;
  padding: 11px 20px; border-radius: var(--r-md);
  background: var(--gold); color: var(--bg-0);
  font-size: 14px; font-weight: 600; text-decoration: none;
  max-width: 100%;
  box-shadow: var(--shadow-el);
  /* Slow glide as the backdrop tone (inline style) rotates. */
  transition: filter 0.15s ease, transform 0.1s ease,
              background 0.9s cubic-bezier(0.22, 1, 0.36, 1),
              color 0.9s cubic-bezier(0.22, 1, 0.36, 1);
}
.ch-cta-btn span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ch-cta-btn:hover { filter: brightness(1.08); }
.ch-cta-btn:active { transform: translateY(1px); }
.ch-complete {
  display: inline-flex; align-items: center; gap: 8px;
  font-size: 13.5px; color: var(--good); font-weight: 500;
}

/* ── Films list ───────────────────────────────────────────────────────── */
.ch-list-wrap { padding-top: 28px; padding-bottom: 90px; }
.ch-list-head { display: flex; align-items: baseline; gap: 12px; margin-bottom: 16px; }
.ch-list-title { font-size: 20px; font-weight: 600; letter-spacing: -0.01em; margin: 0; }
.ch-list-sub {
  font-size: 11px; font-family: var(--font-mono); text-transform: uppercase;
  letter-spacing: 0.1em; color: var(--fg-3);
}
.ch-list { display: flex; flex-direction: column; gap: 2px; }
.ch-row {
  display: flex; align-items: center; gap: 16px;
  padding: 10px 12px; border-radius: var(--r-md);
  color: inherit; text-decoration: none; cursor: pointer;
  transition: background 0.12s ease;
}
.ch-row:hover { background: rgb(var(--ink) / 0.045); }
.ch-row-idx {
  width: 26px; flex-shrink: 0; text-align: center;
  font-family: var(--font-mono); font-size: 13px; color: var(--fg-3);
}
.ch-row-poster {
  width: 46px; height: 69px; flex-shrink: 0;
  border-radius: 5px; overflow: hidden; background: var(--bg-3);
}
.ch-row-poster img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ch-row-main { flex: 1; min-width: 0; }
.ch-row-title { display: flex; align-items: center; gap: 9px; }
.ch-row-name {
  font-size: 14.5px; font-weight: 500; color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ch-next-badge {
  flex-shrink: 0;
  font-size: 9.5px; font-family: var(--font-mono); font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.06em;
  padding: 2px 7px; border-radius: 4px;
  background: var(--gold-soft); color: var(--gold-bright);
}
.ch-row-meta {
  display: flex; align-items: center; gap: 10px; margin-top: 4px;
  font-size: 12px; color: var(--fg-3); font-family: var(--font-mono);
}
.ch-star { display: inline-flex; align-items: center; gap: 3px; color: var(--gold); }
.ch-missing-tag {
  text-transform: uppercase; letter-spacing: 0.05em; font-size: 10px;
  color: var(--fg-4);
}

/* Watched toggle — filled/green when seen, ghost otherwise. */
.ch-watch-toggle {
  flex-shrink: 0;
  width: 30px; height: 30px; border-radius: 50%;
  display: flex; align-items: center; justify-content: center;
  border: 1px solid var(--border); background: rgb(var(--ink) / 0.03);
  color: var(--fg-3); cursor: pointer;
  transition: all 0.13s ease;
}
.ch-watch-toggle:hover { color: var(--fg-0); border-color: var(--fg-3); }
.ch-watch-toggle.on {
  background: var(--good); border-color: var(--good); color: var(--bg-0);
}

.ch-row-seen .ch-row-name { color: var(--fg-2); }
.ch-row-next { background: color-mix(in srgb, var(--gold) 8%, transparent); }
.ch-row-next:hover { background: color-mix(in srgb, var(--gold) 12%, transparent); }

/* Missing films: dimmed, non-interactive. */
.ch-row-missing { cursor: default; }
.ch-row-missing:hover { background: none; }
.ch-row-missing .ch-row-poster img { filter: grayscale(0.85); opacity: 0.5; }
.ch-row-missing .ch-row-name { color: var(--fg-3); }

.ch-notfound {
  height: 100%; display: flex; flex-direction: column; gap: 16px;
  align-items: center; justify-content: center; color: var(--fg-2);
}
.ch-notfound p { font-size: 18px; }

/* ── Phone ────────────────────────────────────────────────────────────── */
@media (max-width: 720px) {
  .ch-hero { min-height: 420px; }
  .ch-hero-body { flex-direction: column; align-items: flex-start; gap: 18px; padding-top: 90px; padding-bottom: 28px; }
  .ch-poster { width: 116px; }
  .ch-title { font-size: 27px; }
  .ch-overview { -webkit-line-clamp: 4; line-clamp: 4; }
  .ch-cta-btn { width: 100%; justify-content: center; }
  .ch-row-poster { width: 40px; height: 60px; }
}
</style>
