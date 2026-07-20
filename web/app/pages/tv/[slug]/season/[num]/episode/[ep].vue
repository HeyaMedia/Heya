<template>
  <div v-if="loading" class="scroll hero-flush" style="height: 100%">
    <div style="height: 200px; background: var(--bg-2)" />
  </div>

  <!-- `hero-flush` opts this page out of the .app-main topbar offset so the
       episode still fills up under the glass topbar (the hero's own inner
       padding keeps text clear of the bar). See heya.css .app-main. -->
  <div v-else-if="detail && episode" class="scroll ep2 hero-flush" :style="toneStyle" style="height: 100%">
    <!-- ── HERO: episode still as sharp art, hard-clipped at the ledger seam ── -->
    <section class="ep-hero">
      <HeroCanvas :src="stillUrl" object-position="center 25%" />
      <div class="bignum" aria-hidden="true">E{{ epNumPadded }}</div>

      <div class="ep-hero-inner">
        <div class="grow hero-ink">
          <div class="eyebrow">
            <NuxtLink :to="`/tv/${slug}`">{{ detail.media_item.title }}</NuxtLink>
            <span class="sep">&middot;</span>
            <NuxtLink :to="seasonLink">{{ seasonLabel }}</NuxtLink>
            <span class="sep">&middot;</span>
            <span>Episode {{ currentEpNum }} of {{ episodes.length }}</span>
          </div>

          <h1 class="ep-title">{{ episodeTitle }}</h1>

          <p class="metaline">
            <span v-if="episode.air_date">{{ formatDate(episode.air_date) }}</span>
            <template v-if="episode.runtime_minutes">
              <span class="dot">&middot;</span><span>{{ episode.runtime_minutes }}M</span>
            </template>
            <template v-if="ratingStr(episode.rating)">
              <span class="dot">&middot;</span>
              <span class="rating"><Icon name="star" :size="11" /> {{ ratingStr(episode.rating) }}</span>
              <span v-if="episode.vote_count" class="votes">({{ episode.vote_count }})</span>
            </template>
          </p>

          <div class="actions">
            <button v-if="fileRef" class="btn-play" @click="play">
              <span class="tri" /> Play
            </button>
            <button v-else class="btn-play" disabled>
              <span class="tri" /> No File
            </button>

            <button
              class="pill icon"
              :class="{ 'is-on': watched }"
              :aria-label="watched ? 'Mark as unwatched' : 'Mark as watched'"
              :aria-pressed="watched"
              :title="watched ? 'Mark as unwatched' : 'Mark as watched'"
              @click="toggleWatched"
            >
              <Icon name="check" :size="16" />
            </button>
            <button class="pill icon" title="Edit Metadata" aria-label="Edit metadata" @click="showMetadataEditor = true">
              <Icon name="settings" :size="15" />
            </button>

            <span class="actions-spacer" />

            <NuxtLink v-if="prevEpisode" :to="episodeLink(prevEpisode)" class="pill nav-pill">
              &larr; E{{ pad(prevEpisode.episode_number) }}
            </NuxtLink>
            <NuxtLink v-if="nextEpisode" :to="episodeLink(nextEpisode)" class="pill nav-pill">
              Next: E{{ pad(nextEpisode.episode_number) }} &rarr;
            </NuxtLink>
          </div>
        </div>
      </div>
    </section>

    <!-- ── LEDGER at the hard-clip seam ── -->
    <LedgerStrip :cells="ledgerCells" />

    <!-- ── BODY ── -->
    <main class="page">
      <section class="section cols">
        <div>
          <SectionHeader title="Story" />
          <div v-if="episodeOverview" class="prose">
            <p class="lede">{{ episodeOverview }}</p>
          </div>
          <p v-else class="prose-empty">No synopsis available for this episode.</p>
        </div>

        <div v-if="nextEpisode">
          <SectionHeader title="Up next" />
          <NuxtLink :to="episodeLink(nextEpisode)" class="upnext">
            <LoadingImage
              :src="episodeStillUrl(nextEpisode)"
              :width="640"
              :quality="80"
              alt=""
              @error="hideBroken"
            />
            <div class="upnext-pad">
              <div class="upnext-k">{{ epCodeFor(nextEpisode) }}</div>
              <h3>{{ nextEpisode.preferred_title || nextEpisode.title || `Episode ${nextEpisode.episode_number}` }}</h3>
              <div class="upnext-meta">
                <span v-if="nextEpisode.air_date">{{ formatDate(nextEpisode.air_date) }}</span>
                <template v-if="nextEpisode.runtime_minutes"> &middot; {{ nextEpisode.runtime_minutes }}m</template>
                <template v-if="ratingStr(nextEpisode.rating)"> &middot; &#9733; {{ ratingStr(nextEpisode.rating) }}</template>
              </div>
              <p v-if="nextEpisode.preferred_overview">{{ nextEpisode.preferred_overview }}</p>
            </div>
          </NuxtLink>
        </div>
      </section>

      <!-- ── Season strip: every present episode, current one ringed ── -->
      <section class="section">
        <SectionHeader :title="seasonLabel">
          <template #subtitle>{{ episodes.length }} episode{{ episodes.length !== 1 ? 's' : '' }}</template>
          <template #actions>
            <NuxtLink :to="seasonLink" class="more">Season view &rarr;</NuxtLink>
          </template>
        </SectionHeader>

        <AppRail :items="episodes" :tile-width="172" :phone-tile-width="150" aspect="16/9" :gap="14" :phone-gap="14" memory-key="episode-strip">
          <template #default="{ item: ep }">
            <NuxtLink
              :to="episodeLink(ep)"
              class="epstrip-it"
              :class="{ on: ep.episode_number === currentEpNum, seen: watchedEpisodes.has(ep.id) }"
            >
              <LoadingImage
                :src="episodeStillUrl(ep)"
                :width="400"
                :quality="80"
                alt=""
                @error="hideBroken"
              />
              <span class="epstrip-n">E{{ pad(ep.episode_number) }}</span>
              <div class="epstrip-t">{{ ep.preferred_title || ep.title || `Episode ${ep.episode_number}` }}</div>
            </NuxtLink>
          </template>
        </AppRail>
      </section>

      <!-- ── Details: stream info + audio/subtitle preferences ── -->
      <section v-if="streamInfo || fileRef" class="section">
        <SectionHeader title="Details" />
        <div class="details-grid">
          <MediaStreamInfo v-if="streamInfo" :stream="streamInfo" />
          <PlaybackPrefs v-if="fileRef" :media-item-id="detail.media_item.id" always-open />
        </div>
      </section>
    </main>

    <MetadataEditorModal
      v-if="detail && episode"
      :media-id="detail.media_item.id"
      :episode-id="episode.id"
      :show="showMetadataEditor"
      @close="showMetadataEditor = false"
    />
  </div>
</template>

<script setup lang="ts">
import type { MediaDetail, StreamInfoResponse } from '~~/shared/types'
import type { ImageTone } from '~/composables/useImageTone'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import { withAuthHeaders } from '~/composables/useAuth'
import { useQuery } from '@pinia/colada'
import { mediaDetailQuery } from '~/queries/media'

const route = useRoute()
const slug = computed(() => route.params.slug as string)
const numParam = computed(() => route.params.num as string)
const epParam = computed(() => route.params.ep as string)

const currentSeasonNum = computed(() => {
  if (numParam.value === 'specials') return 0
  return parseInt(numParam.value) || 1
})

const currentEpNum = computed(() => parseInt(epParam.value) || 1)

// Shared MediaDetail cache key with the series + season pages so navigating
// down a TV tree is instant after the first fetch.
const { $heya } = useNuxtApp()
const detailQuery = useQuery(() => mediaDetailQuery(slug.value))
await waitForQuery(detailQuery)
const detail = computed<MediaDetail | null>(() => detailQuery.data.value ?? null)
const loading = computed(() => detailQuery.isPending.value)
watch(detailQuery.error, (err) => { if (err) navigateTo(`/tv/${slug.value}`) }, { immediate: true })

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
const streamInfo = ref<StreamInfoResponse | null>(null)
const showMetadataEditor = ref(false)

const allSeasons = computed(() => {
  if (!detail.value?.seasons) return []
  return [...detail.value.seasons].sort((a: any, b: any) => a.season_number - b.season_number)
})

const season = computed(() => {
  return allSeasons.value.find((s: any) => s.season_number === currentSeasonNum.value) || null
})

// Only the episodes we actually hold — a currently-airing season carries the
// full provider catalog. `presentEpisodes` derives presence from the detail
// doc's episode_files map (full-list fallback), the same helper the season and
// series pages route every count/grid through. Governs the epstrip, the
// "Episode N of X" count, and prev/next navigation.
const episodes = computed(() => {
  const eps = presentEpisodes(detail.value?.episode_files as any, currentSeasonNum.value, (season.value as any)?.episodes) as any[]
  return eps.slice().sort((a: any, b: any) => a.episode_number - b.episode_number)
})

const episode = computed(() => {
  return episodes.value.find((e: any) => e.episode_number === currentEpNum.value) || null
})

const epIndex = computed(() => episodes.value.findIndex((e: any) => e.episode_number === currentEpNum.value))
const prevEpisode = computed(() => epIndex.value > 0 ? episodes.value[epIndex.value - 1] : null)
const nextEpisode = computed(() => epIndex.value >= 0 && epIndex.value < episodes.value.length - 1 ? episodes.value[epIndex.value + 1] : null)

const episodeTitle = computed(() =>
  episode.value?.preferred_title || episode.value?.title || `Episode ${currentEpNum.value}`)
// Story uses ONLY the library-language overview the server resolved
// (preferred_overview). Falling back to the raw `overview` surfaced the
// provider's original-language text (e.g. Japanese on an English library), so
// when there's no localized synopsis we show the empty state instead.
const episodeOverview = computed(() => episode.value?.preferred_overview || '')

const seasonLabel = computed(() => {
  if (currentSeasonNum.value === 0) return 'Specials'
  return (season.value as any)?.title || (season.value as any)?.name || `Season ${currentSeasonNum.value}`
})

const seasonLink = computed(() => {
  const num = currentSeasonNum.value === 0 ? 'specials' : String(currentSeasonNum.value)
  return `/tv/${slug.value}/season/${num}`
})

const fileRef = computed(() => {
  const key = `s${currentSeasonNum.value}e${currentEpNum.value}`
  const entry = detail.value?.episode_files?.[key]
  return entry?.file_public_id || entry?.file_id || null
})

const stillUrl = computed(() => {
  if (!detail.value) return ''
  const label = `s${String(currentSeasonNum.value).padStart(2, '0')}e${String(currentEpNum.value).padStart(2, '0')}`
  return `/api/media/${useMediaImageKey(detail.value.media_item)}/image/still?label=${label}`
})

const epNumPadded = computed(() => pad(currentEpNum.value))
const watched = computed(() => episode.value ? watchedEpisodes.value.has(episode.value.id) : false)

function pad(n: number) { return String(n).padStart(2, '0') }
function epCodeFor(ep: any) { return `S${pad(currentSeasonNum.value)}E${pad(ep.episode_number)}` }
function ratingStr(r: unknown) {
  const n = typeof r === 'number' ? r : parseFloat(String(r ?? ''))
  return (!Number.isFinite(n) || n <= 0) ? '' : n.toFixed(1)
}

function episodeStillUrl(ep: any) {
  if (!detail.value) return ''
  const label = `s${pad(currentSeasonNum.value)}e${pad(ep.episode_number)}`
  return `/api/media/${useMediaImageKey(detail.value.media_item)}/image/still?label=${label}`
}

function episodeLink(ep: any) {
  const num = currentSeasonNum.value === 0 ? 'specials' : String(currentSeasonNum.value)
  return `/tv/${slug.value}/season/${num}/episode/${ep.episode_number}`
}

function hideBroken(e: Event | string) {
  if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none'
}

// ── Ledger cells (user-facing facts only) ──────────────────────────────────
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
function airMonthDay(d?: string | null) {
  if (!d) return ''
  try { return new Date(`${d}T00:00:00`).toLocaleDateString('en-US', { month: 'short', day: 'numeric' }).toUpperCase() }
  catch { return d }
}

const ledgerCells = computed<LedgerCell[]>(() => {
  const cells: LedgerCell[] = []
  const ep = episode.value
  if (!ep) return cells

  const r = ratingStr(ep.rating)
  if (r) cells.push({ k: 'Rating', v: r })
  if (ep.runtime_minutes) cells.push({ k: 'Runtime', v: `${ep.runtime_minutes}M` })
  if (ep.air_date) cells.push({ k: 'Aired', v: airMonthDay(ep.air_date), sub: String(ep.air_date).slice(0, 4) })

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
    const subs = si.subtitle || []
    if (subs.length) {
      const first = subs[0]
      cells.push({
        k: 'Subtitles',
        v: first?.language ? first.language.toUpperCase() : String(subs.length),
        sub: subs.length > 1 ? `+${subs.length - 1} more` : undefined,
      })
    }
    if (si.playback?.action) cells.push({ k: 'Playback', v: playbackUpper(si.playback.action), tone: true })
  }
  return cells
})

// ── Tone follow: publish --tone/--tone-rgb/--tone-ink on the page root ──
// Primary source is the AmbientBackdrop's own sampled tone (useBackgroundTone):
// it samples the still AFTER decode and re-samples on every crossfade, so the
// buttons update in lockstep with the blurred backdrop actually appearing — no
// desync, and no permanently-null cache from sampling a still that 202'd while
// it was still generating. A direct sample is kept only as the ambient-off
// fallback (sequence-guarded, Playbar's --pb-accent pattern). --tone-rgb is the
// space-separated triple twin of --tone parsed out of the "rgb(r g b)" main.
const bgTone = useBackgroundTone()
const localTone = ref<ImageTone | null>(null)
let toneSeq = 0
watch(stillUrl, (src) => {
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

// ── Mutations ───────────────────────────────────────────────────────────────
const invalidateContinueWatching = useInvalidateContinueWatching()
async function toggleWatched() {
  if (!episode.value) return
  const { $heya } = useNuxtApp()
  if (watched.value) {
    await $heya('/api/me/watched/episode/{id}', { method: 'DELETE', path: { id: episode.value.id } })
    watchedEpisodes.value.delete(episode.value.id)
  } else {
    await $heya('/api/me/watched/episode/{id}', { method: 'POST', path: { id: episode.value.id } })
    watchedEpisodes.value.add(episode.value.id)
  }
  watchedEpisodes.value = new Set(watchedEpisodes.value)
  invalidateContinueWatching()
}

function play() {
  if (!fileRef.value || !detail.value) return
  const title = `${detail.value.media_item.title} - S${pad(currentSeasonNum.value)}E${pad(currentEpNum.value)} - ${episode.value?.title || ''}`
  const params = new URLSearchParams({
    media_item_id: String(detail.value.media_item.id),
    title,
  })
  // Record progress against the episode, not the series — otherwise it lands
  // as ('movie', series_id) and mark-watched (keyed by episode) can't clear
  // it from Continue Watching.
  if (episode.value?.id) {
    params.set('entity_type', 'episode')
    params.set('entity_id', String(episode.value.id))
  }
  navigateTo(`/watch/${fileRef.value}?${params}`)
}

async function loadWatchState() {
  if (!detail.value) return
  try {
    const st = await fetchUserState('episodes', detail.value.media_item.id)
    watchedEpisodes.value = new Set(st.watched_episode_ids || [])
  } catch { /* empty */ }
}

async function loadStreamInfo() {
  if (!fileRef.value) { streamInfo.value = null; return }
  try {
    const caps = useClientCaps()
    const capsQuery = capsToQueryString(caps)
    const url = `/api/stream/${fileRef.value}/info${capsQuery ? `?${capsQuery}` : ''}`
    streamInfo.value = await $fetch<StreamInfoResponse>(url, {
      headers: withAuthHeaders(url),
    })
  } catch { /* empty */ }
}

// When detail data arrives, load watch state + stream info in parallel.
watch(detail, async (d) => {
  if (d) await Promise.all([loadWatchState(), loadStreamInfo()])
}, { immediate: true })

watch([numParam, epParam], async () => {
  streamInfo.value = null
  await Promise.all([loadWatchState(), loadStreamInfo()])
})
</script>

<style scoped>
/* ═══ HERO ═══════════════════════════════════════════════════════════════ */
.ep-hero {
  position: relative;
  min-height: 54vh;
  display: flex;
  align-items: flex-end;
  /* Over-art ink: the hero text rides the literal-dark art grade, so it stays
     light in every theme (dark/oled/light) — the same rule as poster labels
     painted over a still. Themed --ink would flip to near-black in the light
     theme and vanish against the dark grade. */
  --oink: 233 236 242;
}

/* Ghost tabular E-numeral (heya2.css .bignum) — sits behind the identity
   block, dips just below the seam. */
.bignum {
  position: absolute;
  right: var(--pad-fluid);
  bottom: -0.14em;
  z-index: 1;
  font: 700 clamp(150px, 22vw, 300px)/1 var(--font-mono);
  letter-spacing: -0.06em;
  color: transparent;
  -webkit-text-stroke: 1px rgb(var(--oink) / 0.2);
  pointer-events: none;
  user-select: none;
}

.ep-hero-inner {
  position: relative;
  z-index: 2;
  width: 100%;
  padding: 96px var(--pad-fluid) 36px;
  display: flex;
  align-items: flex-end;
}
.ep-hero-inner > .grow { flex: 1; min-width: 0; }

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
.ep-title {
  font-family: var(--font-display);
  font-size: clamp(2.2rem, 4.6vw, 3.8rem);
  font-weight: 800;
  font-variation-settings: "wdth" 115;
  letter-spacing: -0.022em;
  line-height: 0.99;
  text-wrap: balance;
  max-width: 20ch;
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
.metaline .rating { display: inline-flex; align-items: center; gap: 4px; color: var(--tone); }
.metaline .votes { color: rgb(var(--oink) / 0.45); }

/* actions */
.actions {
  margin-top: 24px;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.actions-spacer { flex: 1; }

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
  /* luminance-picked ink on the sampled tone (dark tone ⇒ light ink) */
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
.nav-pill { font-family: var(--font-mono); font-size: 12px; letter-spacing: 0.04em; }

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

/* up-next card (heya2.css .upnext) */
.upnext {
  display: block;
  border: 1px solid var(--hair-strong);
  border-radius: 12px;
  overflow: hidden;
  background: var(--bg-2);
  box-shadow: 8px 14px 30px -12px rgb(0 0 0 / 0.75), 18px 34px 70px -18px rgb(0 0 0 / 0.9);
  transition: transform 0.18s ease, box-shadow 0.28s ease;
}
.upnext:hover {
  transform: translateY(-3px);
  box-shadow: 10px 18px 36px -12px rgb(0 0 0 / 0.8), 22px 44px 84px -18px rgb(0 0 0 / 0.92), 0 0 34px rgb(var(--tone-rgb) / 0.16);
}
.upnext :deep(img) { width: 100%; aspect-ratio: 16/9; object-fit: cover; display: block; background: var(--bg-3); }
.upnext-pad { padding: 18px 20px 20px; }
.upnext-k { font: 650 10px var(--font-mono); letter-spacing: 0.2em; text-transform: uppercase; color: var(--tone); }
.upnext-pad h3 { font-size: 16px; font-weight: 650; margin-top: 6px; }
.upnext-meta {
  margin: 4px 0 10px;
  font: 500 10.5px var(--font-mono);
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.5);
}
.upnext-pad p {
  font-size: 13px;
  line-height: 1.55;
  color: rgb(var(--ink) / 0.65);
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

/* episode mini-strip (heya2.css .epstrip) — AppRail owns the scroller/
   shadow-room chrome now. epstrip-it was a flex item before (which
   blockifies regardless of its own `display`); now a plain AppRail slot
   child, it needs `display: block` explicitly to keep the same box. */
.epstrip-it { display: block; position: relative; text-decoration: none; color: inherit; }
.epstrip-it :deep(img) {
  width: 172px;
  aspect-ratio: 16/9;
  object-fit: cover;
  border-radius: 7px;
  background: var(--bg-3);
  box-shadow: 0 0 0 1px rgb(var(--ink) / 0.09);
  transition: box-shadow 0.2s ease, opacity 0.2s ease;
}
.epstrip-it.on :deep(img) { box-shadow: 0 0 0 2px var(--tone), 0 0 26px rgb(var(--tone-rgb) / 0.35); }
.epstrip-it.seen :deep(img) { opacity: 0.45; }
.epstrip-it:hover :deep(img) { box-shadow: 0 0 0 1px rgb(var(--ink) / 0.2), 6px 10px 24px -10px rgb(0 0 0 / 0.7); }
.epstrip-n {
  position: absolute;
  top: 8px;
  left: 8px;
  font: 700 10px var(--font-mono);
  letter-spacing: 0.08em;
  padding: 3px 7px;
  border-radius: 4px;
  background: rgb(6 7 10 / 0.8);
  color: rgb(var(--ink) / 0.85);
}
.epstrip-it.on .epstrip-n { color: var(--tone); }
.epstrip-t {
  margin-top: 8px;
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* details */
.details-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 320px);
  gap: 24px;
  align-items: start;
}

/* ═══ RESPONSIVE ══════════════════════════════════════════════════════════ */
@media (max-width: 960px) {
  .cols { grid-template-columns: 1fr; gap: 36px; }
  .details-grid { grid-template-columns: 1fr; }
  .ep-hero-inner { padding: 84px var(--pad-fluid) 30px; }
}

@media (max-width: 720px) {
  .ep-hero { min-height: 50vh; }
  .ep-hero-inner { padding-top: 72px; }
  .bignum { display: none; }
  .actions { gap: 8px; }
  .actions-spacer { display: none; }
  /* keep prev/next reachable via the epstrip; hide the wordy nav pills */
  .nav-pill { display: none; }
  .btn-play { height: 48px; padding: 0 22px 0 18px; }
  .pill.icon { width: 48px; height: 48px; }
  .epstrip-it :deep(img) { width: 150px; }
}
</style>
