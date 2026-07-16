<template>
  <div v-if="loading" class="scroll hero-flush" style="height: 100%">
    <div style="height: 200px; background: var(--bg-2)" />
  </div>

  <!-- `hero-flush` opts this page out of the .app-main topbar offset so the
       season art fills up under the glass topbar (the hero's own inner padding
       keeps text clear of the bar) — mirrors the sibling episode page. See
       heya.css .app-main. -->
  <div v-else-if="detail" class="scroll season2 hero-flush" :style="toneStyle" style="height: 100%">
    <!-- ── HERO: series art as sharp, hard-clipped at the ledger seam ── -->
    <section class="season-hero">
      <HeroCanvas :src="heroArtUrl || ''" object-position="center 18%" />
      <div class="bignum" aria-hidden="true">{{ bignumLabel }}</div>

      <div class="season-hero-inner">
        <NuxtLink :to="`/tv/${slug}`" class="postercard" :aria-label="`Back to ${detail.media_item.title}`">
          <LoadingImage :src="seasonPosterUrl" :width="500" :quality="80" :alt="seasonTitle" @error="hideBroken" />
        </NuxtLink>

        <div class="grow hero-ink">
          <div class="eyebrow">
            <NuxtLink :to="`/tv/${slug}`">{{ detail.media_item.title }}</NuxtLink>
            <span class="sep">&middot;</span>
            <span>{{ eyebrowSeason }}</span>
          </div>

          <h1 class="season-title">{{ seasonTitle }}</h1>

          <p class="metaline">
            <span v-if="seasonYear">{{ seasonYear }}</span>
            <template v-if="episodes.length">
              <span class="dot">&middot;</span><span>{{ episodes.length }} EPISODE{{ episodes.length !== 1 ? 'S' : '' }}</span>
            </template>
            <template v-if="airedRange.v">
              <span class="dot">&middot;</span><span>AIRED {{ airedRange.v }}</span>
            </template>
          </p>

          <div class="actions">
            <button v-if="seasonUpNext" class="btn-play" @click="playSeasonUpNext">
              <span class="tri" /> {{ seasonUpNext.resume ? 'Resume' : 'Play' }}
              <small>{{ upNextSmall }}</small>
            </button>
            <button v-else class="btn-play" disabled>
              <span class="tri" /> No File
            </button>

            <button class="pill" @click="toggleSeasonWatched">
              <Icon name="check" :size="15" /> {{ allWatched ? 'Unmark season' : 'Mark season watched' }}
            </button>

            <button
              class="pill icon"
              :class="{ 'is-on': seasonFavorited }"
              :aria-label="seasonFavorited ? 'Remove season from loved' : 'Add season to loved'"
              :aria-pressed="seasonFavorited"
              :title="seasonFavorited ? 'Remove season from loved' : 'Add season to loved'"
              @click="toggleFavorite"
            >
              <Icon :name="seasonFavorited ? 'heartfill' : 'heart'" :size="16" />
            </button>
            <button class="pill icon" title="Edit Metadata" aria-label="Edit metadata" @click="showMetadataEditor = true">
              <Icon name="settings" :size="15" />
            </button>
          </div>
        </div>
      </div>
    </section>

    <!-- ── LEDGER at the hard-clip seam ── -->
    <LedgerStrip :cells="ledgerCells" />

    <!-- ── BODY ── -->
    <main class="page">
      <!-- Season switcher pills -->
      <section class="section tabs-row">
        <nav class="seasontabs" aria-label="Seasons">
          <NuxtLink
            v-for="s in allSeasons"
            :key="s.season_number"
            :to="seasonLink(s)"
            class="stab"
            :class="{ on: s.season_number === currentSeasonNum }"
          >
            <span>{{ s.season_number === 0 ? 'SPECIALS' : `SEASON ${s.season_number}` }}</span>
            <Icon v-if="isSeasonWatched(s)" name="check" :size="11" class="stab-check" />
          </NuxtLink>
        </nav>
      </section>

      <!-- Episode ledger -->
      <section class="section">
        <SectionHeader title="Episodes">
          <template #subtitle>{{ episodes.length }}</template>
        </SectionHeader>

        <div v-if="episodes.length" class="eplist">
          <AppContextMenu v-for="ep in episodes" :key="ep.id" :items="episodeContextItems(ep)">
            <EpisodeLedgerRow
              :to="episodeLink(ep)"
              :still-url="episodeStillUrl(ep)"
              :episode-number="ep.episode_number"
              :title="ep.preferred_title || ep.title || `Episode ${ep.episode_number}`"
              :air-date="ep.air_date"
              :runtime-minutes="ep.runtime_minutes"
              :rating="ep.rating"
              :overview="ep.preferred_overview || ep.overview"
              :watched="isWatched(ep.id)"
              :has-file="!!episodeFileId(ep)"
              :progress-pct="episodeProgressPct(ep.id)"
              :remaining-minutes="remainingMin(ep.id)"
              @play="playEpisode(ep)"
              @toggle-watched="toggleEpisodeWatched(ep)"
            />
          </AppContextMenu>
        </div>
        <p v-else class="prose-empty">No episodes found for this season.</p>
      </section>

      <!-- About this season -->
      <section v-if="seasonOverview" class="section">
        <SectionHeader title="About this season" />
        <div class="prose">
          <p>{{ seasonOverview }}</p>
        </div>
      </section>
    </main>

    <MetadataEditorModal
      v-if="detail && season"
      :media-id="detail.media_item.id"
      :season-id="(season as any).id"
      :show="showMetadataEditor"
      @close="showMetadataEditor = false"
    />
  </div>
</template>

<script setup lang="ts">
import type { ContextMenuItem, MediaDetail } from '~~/shared/types'
import type { ImageTone } from '~/composables/useImageTone'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import { useQuery } from '@pinia/colada'
import { mediaDetailQuery } from '~/queries/media'

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const numParam = computed(() => route.params.num as string)

const currentSeasonNum = computed(() => {
  if (numParam.value === 'specials') return 0
  return parseInt(numParam.value) || 1
})

// Same cache key as the parent /tv/:slug page — the season view shares the
// underlying MediaDetail (series) document, so opening a season from the
// series page hits the cache instantly.
const { $heya } = useNuxtApp()
const detailQuery = useQuery(() => mediaDetailQuery(slug.value))
await waitForQuery(detailQuery)
const detail = computed<MediaDetail | null>(() => detailQuery.data.value ?? null)
const loading = computed(() => detailQuery.isPending.value)
watch(detailQuery.error, (err) => { if (err) navigateTo('/tv') }, { immediate: true })

// Live refresh — a re-enrich or metadata edit lands new data server-side while
// this page is open. It reads the shared ['media','detail', slug] doc, but the
// parent series page isn't mounted on a direct/deep visit here, so it needs its
// own subscription: a shared cache key does not imply a shared listener.
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

const watchedEpisodes = ref<Set<number>>(new Set())
const episodeProgress = ref<Map<number, { progress: number; total: number }>>(new Map())
const seasonFavorited = ref(false)
const showMetadataEditor = ref(false)

const heroArtUrl = computed(() => detail.value ? useBackdropUrl(detail.value.media_item) : null)
const seasonPosterUrl = computed(() =>
  detail.value ? `/api/media/${useMediaImageKey(detail.value.media_item)}/image/poster?label=season-${currentSeasonNum.value}` : '')

const allSeasons = computed(() => {
  if (!detail.value?.seasons) return []
  return [...detail.value.seasons].sort((a: any, b: any) => a.season_number - b.season_number)
})

const season = computed(() => {
  return allSeasons.value.find((s: any) => s.season_number === currentSeasonNum.value) || null
})

// Only surface episodes we actually hold — a currently-airing season carries
// the full provider catalog (e.g. all 13 from TMDB) even when just a few have
// aired/downloaded. `presentEpisodes` derives presence from the detail doc's
// `episode_files` map (full-list fallback), the same helper the series page
// routes every count/grid through — governs the ledger counts, the episode
// list, and the season-tab watched checks.
const episodes = computed(() => {
  const eps = presentEpisodes(detail.value?.episode_files as any, currentSeasonNum.value, (season.value as any)?.episodes) as any[]
  return eps.slice().sort((a: any, b: any) => a.episode_number - b.episode_number)
})

const seasonTitle = computed(() => {
  if (currentSeasonNum.value === 0) return 'Specials'
  return (season.value as any)?.title || (season.value as any)?.name || `Season ${currentSeasonNum.value}`
})

const seasonOverview = computed(() => (season.value as any)?.overview || '')

const regularSeasonCount = computed(() => allSeasons.value.filter((s: any) => s.season_number !== 0).length)

const eyebrowSeason = computed(() => {
  if (currentSeasonNum.value === 0) return 'Specials'
  return `Season ${currentSeasonNum.value} of ${regularSeasonCount.value}`
})

const bignumLabel = computed(() => currentSeasonNum.value === 0 ? 'SP' : String(currentSeasonNum.value).padStart(2, '0'))

const seasonYear = computed(() => {
  const d = (season.value as any)?.air_date
  return d ? String(d).slice(0, 4) : ''
})

function monShort(d?: string | null) {
  if (!d) return ''
  try { return new Date(`${d}T00:00:00`).toLocaleDateString('en-US', { month: 'short' }).toUpperCase() }
  catch { return '' }
}

const airedRange = computed<{ v: string; year: string }>(() => {
  const start = (season.value as any)?.air_date
  const end = (season.value as any)?.end_date
  if (!start) return { v: '', year: '' }
  const a = monShort(start)
  const b = monShort(end)
  const v = b && b !== a ? `${a} – ${b}` : a
  return { v, year: String(start).slice(0, 4) }
})

const watchedCount = computed(() => {
  let count = 0
  for (const ep of episodes.value) if (watchedEpisodes.value.has(ep.id)) count++
  return count
})

const inProgressCount = computed(() =>
  episodes.value.filter((e: any) => episodeProgress.value.has(e.id)).length)

const allWatched = computed(() => episodes.value.length > 0 && watchedCount.value >= episodes.value.length)

const avgRating = computed(() => {
  const rated = episodes.value
    .map((e: any) => parseFloat(String(e.rating ?? '')))
    .filter((n: number) => Number.isFinite(n) && n > 0)
  if (!rated.length) return ''
  return (rated.reduce((s: number, n: number) => s + n, 0) / rated.length).toFixed(1)
})

const avgRuntime = computed(() => {
  const rts = episodes.value.map((e: any) => e.runtime_minutes).filter((n: any) => n > 0)
  if (!rts.length) return 0
  return Math.round(rts.reduce((s: number, n: number) => s + n, 0) / rts.length)
})

// ── Ledger cells (user-facing facts only) ──────────────────────────────────
const ledgerCells = computed<LedgerCell[]>(() => {
  const cells: LedgerCell[] = []
  const total = episodes.value.length
  if (!total) return cells

  cells.push({
    k: 'Watched',
    v: String(watchedCount.value),
    unit: `of ${total}`,
    tone: true,
    sub: inProgressCount.value > 0 ? `+${inProgressCount.value} in progress` : undefined,
  })
  cells.push({ k: 'Episodes', v: String(total) })
  if (avgRating.value) cells.push({ k: 'Avg rating', v: avgRating.value })
  if (airedRange.value.v) cells.push({ k: 'Aired', v: airedRange.value.v, sub: airedRange.value.year })
  if (avgRuntime.value) cells.push({ k: 'Runtime', v: `~${avgRuntime.value}M`, sub: 'per episode' })
  return cells
})

// ── Season-local "up next" — the primary Play/Resume CTA. Prefers an
// in-progress present episode, then the first unwatched with a file, then the
// first playable episode (rewatch). Disabled when nothing has a file. ────────
const seasonUpNext = computed<{ ep: any; resume: boolean } | null>(() => {
  const eps = episodes.value
  if (!eps.length) return null
  const inProg = eps.find((e: any) => episodeProgress.value.has(e.id) && episodeFileRef(e))
  if (inProg) return { ep: inProg, resume: true }
  const unwatched = eps.find((e: any) => !watchedEpisodes.value.has(e.id) && episodeFileRef(e))
  if (unwatched) return { ep: unwatched, resume: false }
  const first = eps.find((e: any) => episodeFileRef(e))
  return first ? { ep: first, resume: false } : null
})

const upNextSmall = computed(() => {
  const un = seasonUpNext.value
  if (!un) return ''
  const code = `E${pad(un.ep.episode_number)}`
  const rem = remainingMin(un.ep.id)
  return un.resume && rem ? `${code} · ${rem}M LEFT` : code
})

function playSeasonUpNext() {
  if (seasonUpNext.value) playEpisode(seasonUpNext.value.ep)
}

function isWatched(epId: number) { return watchedEpisodes.value.has(epId) }

function isSeasonWatched(s: any) {
  // Only the episodes we hold count — an airing season is fully watched once
  // every present episode is, not once the unaired rest is.
  const eps = presentEpisodes(detail.value?.episode_files as any, s.season_number, s.episodes)
  if (!eps.length) return false
  return eps.every((ep: any) => watchedEpisodes.value.has(ep.id))
}

function episodeProgressPct(epId: number): number {
  const p = episodeProgress.value.get(epId)
  if (!p || p.total === 0) return 0
  return Math.min(100, Math.round((p.progress / p.total) * 100))
}

function remainingMin(epId: number): number {
  const p = episodeProgress.value.get(epId)
  if (!p || !p.total || p.total <= p.progress) return 0
  return Math.max(1, Math.round((p.total - p.progress) / 60))
}

const invalidateContinueWatching = useInvalidateContinueWatching()

async function toggleEpisodeWatched(ep: any) {
  const watched = isWatched(ep.id)
  const { $heya } = useNuxtApp()
  if (watched) {
    await $heya('/api/me/watched/episode/{id}', { method: 'DELETE', path: { id: ep.id } })
    watchedEpisodes.value.delete(ep.id)
  } else {
    await $heya('/api/me/watched/episode/{id}', { method: 'POST', path: { id: ep.id } })
    watchedEpisodes.value.add(ep.id)
  }
  watchedEpisodes.value = new Set(watchedEpisodes.value)
  invalidateContinueWatching()
}

async function toggleSeasonWatched() {
  if (!season.value) return
  const s = season.value as any
  const { $heya } = useNuxtApp()
  await $heya('/api/me/watched/season/{id}', {
    method: 'POST',
    path: { id: s.id },
    body: { watched: !allWatched.value } as any,
  })
  await loadWatchState()
  invalidateContinueWatching()
}

async function toggleFavorite() {
  if (!season.value) return
  const s = season.value as any
  const { $heya } = useNuxtApp()
  const res = await $heya('/api/me/favorites', {
    method: 'POST',
    body: { entity_type: 'season', entity_id: s.id } as any,
  }) as { favorited: boolean }
  seasonFavorited.value = res.favorited
}

async function loadWatchState() {
  if (!detail.value) return
  try {
    const st = await fetchUserState('episodes', detail.value.media_item.id)
    watchedEpisodes.value = new Set(st.watched_episode_ids || [])
    const pm = new Map<number, { progress: number; total: number }>()
    for (const ep of (st.episode_progress || [])) {
      if (!ep.completed && ep.progress_seconds > 0) {
        pm.set(ep.episode_id, { progress: ep.progress_seconds, total: ep.total_seconds })
      }
    }
    episodeProgress.value = pm
    if (season.value) {
      const s = season.value as any
      seasonFavorited.value = (st.favorited_seasons || []).includes(s.id)
    }
  } catch { /* empty */ }
}

function seasonLink(s: any) {
  const num = s.season_number === 0 ? 'specials' : String(s.season_number)
  return `/tv/${slug.value}/season/${num}`
}

function episodeStillUrl(ep: any) {
  if (!detail.value) return ''
  const label = `s${pad(currentSeasonNum.value)}e${pad(ep.episode_number)}`
  return `/api/media/${useMediaImageKey(detail.value.media_item)}/image/still?label=${label}`
}

function episodeFileId(ep: any): number | null {
  const key = `s${currentSeasonNum.value}e${ep.episode_number}`
  return detail.value?.episode_files?.[key]?.file_id ?? null
}

function episodeFileRef(ep: any): string | number | null {
  const key = `s${currentSeasonNum.value}e${ep.episode_number}`
  const entry = detail.value?.episode_files?.[key]
  return entry?.file_public_id || entry?.file_id || null
}

function episodeLink(ep: any) {
  const num = currentSeasonNum.value === 0 ? 'specials' : String(currentSeasonNum.value)
  return `/tv/${slug.value}/season/${num}/episode/${ep.episode_number}`
}

function episodeContextItems(ep: any): ContextMenuItem[] {
  const watched = isWatched(ep.id)
  const hasFile = !!episodeFileId(ep)
  return [
    { label: 'View Episode', icon: 'info', action: () => navigateTo(episodeLink(ep)) },
    ...(hasFile
      ? [{ label: 'Play', icon: 'play', action: () => playEpisode(ep) } as ContextMenuItem]
      : []),
    { label: '', separator: true },
    { label: watched ? 'Mark Unwatched' : 'Mark Watched', icon: 'eye', action: () => toggleEpisodeWatched(ep) },
  ]
}

function playEpisode(ep: any) {
  const fileRef = episodeFileRef(ep)
  if (!fileRef || !detail.value) return
  const params = new URLSearchParams({
    media_item_id: String(detail.value.media_item.id),
    title: `${detail.value.media_item.title} - S${pad(currentSeasonNum.value)}E${pad(ep.episode_number)} - ${ep.title}`,
  })
  // Progress must key on the episode, not the series — otherwise it lands as
  // ('movie', series_id) and mark-watched (keyed by episode) can't clear it
  // from Continue Watching.
  if (ep.id) {
    params.set('entity_type', 'episode')
    params.set('entity_id', String(ep.id))
  }
  navigateTo(`/watch/${fileRef}?${params}`)
}

function pad(n: number) { return String(n).padStart(2, '0') }
function hideBroken(e: Event | string) {
  if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none'
}

// ── Tone follow: publish --tone/--tone-rgb/--tone-ink on the page root.
// Primary source is the AmbientBackdrop's own sampled tone (useBackgroundTone),
// which re-samples on every crossfade; a direct sample of the hero art is the
// ambient-off fallback (sequence-guarded, Playbar's --pb-accent pattern). ────
const bgTone = useBackgroundTone()
const localTone = ref<ImageTone | null>(null)
let toneSeq = 0
watch(heroArtUrl, (src) => {
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

// Trigger watch-state load whenever detail data arrives.
watch(detail, async (d) => {
  if (d) await loadWatchState()
}, { immediate: true })

watch(numParam, async () => {
  await loadWatchState()
  if (season.value) {
    const s = season.value as any
    const { $heya } = useNuxtApp()
    const res = await $heya('/api/me/favorites/check', {
      query: { entity_type: 'season', entity_id: s.id },
    }) as { favorited: boolean }
    seasonFavorited.value = res.favorited
  }
})
</script>

<style scoped>
/* ═══ HERO ═══════════════════════════════════════════════════════════════ */
.season-hero {
  position: relative;
  min-height: 40vh;
  display: flex;
  align-items: flex-end;
  /* Over-art ink: the hero text rides the literal-dark art grade, so it stays
     light in every theme — the same rule as poster labels over a still. */
  --oink: 233 236 242;
}

/* Ghost tabular season numeral (heya2.css .bignum) — dips just below the seam. */
.bignum {
  position: absolute;
  right: var(--pad-fluid);
  bottom: -0.14em;
  z-index: 1;
  font: 700 clamp(160px, 24vw, 340px)/1 var(--font-mono);
  letter-spacing: -0.06em;
  color: transparent;
  -webkit-text-stroke: 1px rgb(var(--oink) / 0.2);
  pointer-events: none;
  user-select: none;
}

.season-hero-inner {
  position: relative;
  z-index: 2;
  width: 100%;
  padding: 96px var(--pad-fluid) 36px;
  display: flex;
  align-items: flex-end;
  gap: 44px;
}
.season-hero-inner > .grow { flex: 1; min-width: 0; }

/* season poster record-card (heya2.css .postercard, condensed to 172px) */
.postercard {
  flex: 0 0 172px;
  display: block;
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
  transition: transform 0.18s ease;
}
.postercard:hover :deep(img) { transform: translateY(-3px); }

/* mono eyebrow breadcrumb */
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
.eyebrow a { color: rgb(var(--oink) / 0.55); transition: color 0.15s; }
.eyebrow a:hover { color: rgb(var(--oink) / 0.9); }
.eyebrow .sep { color: rgb(var(--oink) / 0.3); }

/* Archivo display title */
.season-title {
  font-family: var(--font-display);
  font-size: clamp(2.2rem, 5vw, 3.8rem);
  font-weight: 800;
  font-variation-settings: "wdth" 115;
  letter-spacing: -0.022em;
  line-height: 0.99;
  text-wrap: balance;
  max-width: 18ch;
  color: rgb(var(--oink) / 0.98);
  text-shadow: 0 2px 30px rgb(0 0 0 / 0.45);
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

/* actions */
.actions {
  margin-top: 24px;
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
.tabs-row { margin-top: 34px; }

/* season switcher pills (heya2.css .seasontabs) */
.seasontabs {
  display: flex;
  gap: 8px;
  overflow-x: auto;
  scrollbar-width: none;
  padding-bottom: 2px;
}
.seasontabs::-webkit-scrollbar { display: none; }
.stab {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
  white-space: nowrap;
  padding: 8px 16px;
  border-radius: 999px;
  border: 1px solid rgb(var(--ink) / 0.18);
  color: rgb(var(--ink) / 0.6);
  font: 600 12px var(--font-mono);
  letter-spacing: 0.1em;
  text-decoration: none;
  transition: border-color 0.15s, background 0.15s, color 0.15s, box-shadow 0.15s;
}
.stab:hover:not(.on) { border-color: rgb(var(--ink) / 0.4); color: rgb(var(--ink) / 0.9); }
.stab.on {
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.12);
  color: var(--tone);
  box-shadow: 0 0 16px rgb(var(--tone-rgb) / 0.2);
}
.stab-check { color: var(--tone); opacity: 0.85; }
.stab:not(.on) .stab-check { color: rgb(var(--ink) / 0.5); }

/* episode ledger list (heya2.css .eplist) */
.eplist { border-top: 1px solid var(--hair-strong); }

.prose { font-size: 16px; line-height: 1.75; color: rgb(var(--ink) / 0.82); max-width: 72ch; }
.prose-empty { font-size: 14px; color: rgb(var(--ink) / 0.5); font-style: italic; }

/* ═══ RESPONSIVE ══════════════════════════════════════════════════════════ */
@media (max-width: 960px) {
  .postercard { display: none; }
  .season-hero-inner { padding: 84px var(--pad-fluid) 30px; }
}

@media (max-width: 720px) {
  .season-hero { min-height: 34vh; }
  .season-hero-inner { padding-top: 72px; gap: 24px; }
  .bignum { display: none; }
  .actions { gap: 8px; }
  .btn-play { height: 48px; padding: 0 22px 0 18px; }
  .pill.icon { width: 48px; height: 48px; }
}
</style>
