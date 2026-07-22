<script setup lang="ts">
import type { AlbumEdition, MusicAlbumDetail, TrackFile, TrackView } from '~~/shared/types'
import type { Track } from '~/composables/usePlayer'
import type { ImageTone } from '~/composables/useImageTone'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import { useQuery, useQueryCache } from '@pinia/colada'
import { musicAlbumDetailQuery } from '~/queries/music'

definePageMeta({ layout: 'default' })

const { isPhone, isCoarse } = useViewport()
const route = useRoute()
const artistSlug = computed(() => route.params.slug as string)
const albumSlug = computed(() => route.params.album as string)

const { playContext, playTracks, addToQueue, currentTrack, playing, formatTime } = usePlayerBindings()
const radio = useRadio()
const { onDragStart, onDragEnd } = useMusicDragDrop()
const actions = useMusicActions()
const trackInfo = useTrackInfo()
const lightbox = useLightbox()

const albumRatings = useRatings('album')
const trackRatings = useRatings('track')
async function onRateAlbum(id: number, v: number) {
  try { await albumRatings.set(id, v) } catch { /* rollback handled */ }
}
async function onRateTrack(id: number, v: number) {
  try { await trackRatings.set(id, v) } catch { /* rollback handled */ }
}

const { $heya } = useNuxtApp()
const queryClient = useQueryCache()
const detailQuery = useQuery(() => musicAlbumDetailQuery({ artistSlug: artistSlug.value, albumSlug: albumSlug.value }))
await waitForQuery(detailQuery)
const detail = computed<MusicAlbumDetail | null>(() => detailQuery.data.value ?? null)
const loading = computed(() => detailQuery.isPending.value)

const album = computed(() => detail.value?.album)
const artistName = computed(() => detail.value?.artist?.name ?? '')

watch(album, (a) => {
  if (a?.id && a.id > 0) albumRatings.load(a.id).catch(() => 0)
}, { immediate: true })

// Tracks sorted disc-then-track, exactly as the old page.
const tracks = computed<TrackView[]>(() => {
  if (!detail.value) return []
  return [...detail.value.tracks].sort((a, b) => {
    if (a.disc_number !== b.disc_number) return a.disc_number - b.disc_number
    return a.track_number - b.track_number
  })
})

// Prime the per-track rating cache + register metadata already present in the
// album response. Physical paths are resolved by MusicTrackDetail on demand.
watch(tracks, (list) => {
  if (!list.length) return
  trackRatings.primeBulk(list.map((t) => t.id)).catch(() => 0)
  trackInfo.prime(list.map((t) => {
    const rt = t as TrackView & { recording_mbid?: string; isrc?: string; explicit?: boolean }
    return { id: t.id, recording_mbid: rt.recording_mbid, isrc: rt.isrc, explicit: rt.explicit, files: t.files, credits: t.credits }
  }))
}, { immediate: true })

const totalDuration = computed(() => tracks.value.reduce((s, t) => s + (t.duration || 0), 0))
const discNumbers = computed(() => {
  const seen = new Set<number>()
  for (const t of tracks.value) seen.add(t.disc_number)
  return [...seen].sort((a, b) => a - b)
})
const hasMultipleDiscs = computed(() => discNumbers.value.length > 1)

// Playability — a track needs a live file (server-filtered); the album is
// playable when any track is. Missing items still render, can't be played.
function isPlayable(t: TrackView) { return t.files.length > 0 }
const playableTracks = computed(() => tracks.value.filter(isPlayable))
const albumPlayable = computed(() => playableTracks.value.length > 0)

const coverUrl = computed(() => useAlbumCoverUrl(artistSlug.value, albumSlug.value))
const bgImg = useBackgroundImageTools()

const albumTypeLabel = computed(() => (album.value?.album_type || 'album').toUpperCase())

// Best-quality file across the album → a single user-facing "Quality" ledger
// fact (e.g. "FLAC 24/96"). quality_score is server-ranked best-first per track.
const bestFile = computed<TrackFile | null>(() => {
  let best: TrackFile | null = null
  for (const t of tracks.value) {
    for (const f of t.files) {
      if (!best || f.quality_score > best.quality_score) best = f
    }
  }
  return best
})
const qualityLabel = computed(() => (bestFile.value ? formatTrackQuality(bestFile.value) : null))

const albumExternalIds = computed<Record<string, string>>(() => {
  const ids: Record<string, string> = {}
  if (album.value?.musicbrainz_id) ids.mbid = album.value.musicbrainz_id
  return ids
})

// ── Ambient claim (shared hero presentation) ─────────────────────────────────
// The cover is the artwork this page owns; push it to the global AmbientBackdrop
// as a hero so the blurred wash mirrors it and pops back to the music
// pool on unmount. Honors the user's ambient toggle (off → flat canvas).
const { ambientEnabled, toneFollowEnabled } = useAppearance()
const background = useBackground()
watch([coverUrl, ambientEnabled], ([url, on]) => {
  if (on && url) background.set(url, { presentation: 'hero' })
  else background.clear()
}, { immediate: true })

// Local blurred-cover hero backdrop — always present (independent of the
// ambient toggle) so the hero identity text keeps its dark grade for contrast,
// and it hard-clips at the ledger seam (the .hero section is overflow:hidden).
// A blurred wash sidesteps the square-cover-into-a-wide-hero crop problem.
const heroArtStyle = computed(() => (coverUrl.value
  ? { backgroundImage: `url("${bgImg.ambientVariant(coverUrl.value)}")` }
  : {}))

// ── Tone follow: --tone / --tone-rgb / --tone-ink on the page root ───────────
// Primary source is the ambient's own sampled tone; a direct cover sample is
// the ambient-off fallback, sequence-guarded against a slow sample landing
// after the route already changed albums (same pattern as the artist hero).
const bgTone = useBackgroundTone()
const localTone = ref<ImageTone | null>(null)
let toneSeq = 0
watch(coverUrl, (src) => {
  const seq = ++toneSeq
  if (!src) { localTone.value = null; return }
  sampleImageTone(src).then((t) => { if (seq === toneSeq) localTone.value = t })
}, { immediate: true })
const toneStyle = computed(() => {
  if (!toneFollowEnabled.value) return undefined
  const t = bgTone.value || localTone.value
  if (!t) return undefined
  const m = t.main.match(/\d+/g)
  if (!m) return undefined
  return toneStyleVars(t)
})

// ── Ledger (user-facing facts only) ──────────────────────────────────────────
const ledgerCells = computed<LedgerCell[]>(() => {
  const a = album.value
  const cells: LedgerCell[] = []
  if (!a) return cells
  if (a.year) cells.push({ k: 'Released', v: a.year })
  cells.push({
    k: 'Tracks',
    v: String(tracks.value.length),
    sub: hasMultipleDiscs.value ? `${discNumbers.value.length} discs` : undefined,
  })
  if (totalDuration.value > 0) cells.push({ k: 'Runtime', v: formatRunTime(totalDuration.value) })
  if (qualityLabel.value) cells.push({ k: 'Quality', v: qualityLabel.value, tone: true })
  if (a.label) cells.push({ k: 'Label', v: a.label })
  if (a.script) cells.push({ k: 'Script', v: a.script })
  // Provider-native ratings — scales differ per system, so each cell says
  // its own denominator instead of pretending they're comparable.
  for (const r of (detail.value?.ratings ?? []).slice(0, 2)) {
    cells.push({
      k: providerLabel(r.system),
      v: String(r.value),
      unit: `/ ${r.scale_max}`,
      sub: r.votes ? `${r.votes} votes` : undefined,
    })
  }
  if ((a.sales ?? 0) > 0) cells.push({ k: 'Sales', v: formatBigInt(a.sales!) })
  return cells
})

function formatBigInt(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1).replace(/\.0$/, '')}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, '')}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1).replace(/\.0$/, '')}K`
  return n.toLocaleString()
}

// ── About / review / editions (heya.media 2026-07 album expansion) ──────────
const aboutOpen = ref(false)
const reviewOpen = ref(false)
const albumDescription = computed(() => album.value?.description?.trim() ?? '')
const albumReview = computed(() => album.value?.review?.trim() ?? '')

// Editions: pressings with real detail (labels / formats / a provider page)
// lead; inside each band newest first. Collapsed past EDITIONS_SHOWN.
const EDITIONS_SHOWN = 8
const editionsExpanded = ref(false)
const sortedEditions = computed(() => {
  const list = [...(detail.value?.editions ?? [])]
  const weight = (e: AlbumEdition) => (e.labels?.length ? 2 : 0) + (e.formats?.length ? 2 : 0) + (e.link ? 1 : 0)
  return list.sort((a, b) => weight(b) - weight(a) || (b.date ?? '').localeCompare(a.date ?? ''))
})
const visibleEditions = computed(() =>
  editionsExpanded.value ? sortedEditions.value : sortedEditions.value.slice(0, EDITIONS_SHOWN))
function editionLine(e: AlbumEdition): string {
  const parts = [
    e.date || '',
    e.country || '',
    e.formats?.join(', ') || '',
    (e.labels ?? []).map((l) => l.catalog_number ? `${l.name} · ${l.catalog_number}` : l.name).join(', '),
  ].filter(Boolean)
  return parts.join('  ·  ')
}

// Release events (issued-release per-country dates) — a compact line above
// the Editions list, capped at 6 with a title-tooltip for the overflow.
function formatReleaseEventDate(d: string): string {
  const parts = d.split('-').map((n) => Number.parseInt(n, 10))
  if (parts.length === 3 && parts.every((n) => Number.isFinite(n))) {
    const dt = new Date(Date.UTC(parts[0]!, parts[1]! - 1, parts[2]!))
    return dt.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' })
  }
  return d
}
function releaseEventLabel(e: { date: string; country?: string }): string {
  return e.country ? `${formatReleaseEventDate(e.date)} (${e.country})` : formatReleaseEventDate(e.date)
}
const RELEASE_EVENTS_SHOWN = 6
const releaseEventsLine = computed(() => {
  const events = detail.value?.release_events ?? []
  if (!events.length) return null
  const shown = events.slice(0, RELEASE_EVENTS_SHOWN)
  const rest = events.slice(RELEASE_EVENTS_SHOWN)
  return {
    text: shown.map(releaseEventLabel).join(' · '),
    moreCount: rest.length,
    moreTitle: rest.map(releaseEventLabel).join(', '),
  }
})

// Credits — aggregated across every track, grouped by humanized role.
// Plain text everywhere (no local-artist linking yet, per PLAN).
const creditGroups = computed(() => groupTrackCredits(tracks.value.flatMap((t) => t.credits ?? [])))

// Artwork gallery — audiodb "extra render" classes (back/cdart/spine/case/
// flat/face). Small tiles; click opens the shared lightbox.
const ARTWORK_TYPE_LABELS: Record<string, string> = {
  back: 'Back',
  cdart: 'CD',
  spine: 'Spine',
  case: 'Case',
  flat: 'Flat',
  face: 'Face',
}
function artworkLabel(type: string): string {
  return ARTWORK_TYPE_LABELS[type] ?? humanizeCreditTerm(type)
}
const albumArtwork = computed(() => detail.value?.artwork ?? [])
function openArtwork(idx: number) {
  lightbox.open(albumArtwork.value.map((a) => a.url), idx)
}

// ── Now-playing markers ──────────────────────────────────────────────────────
function isTrackActive(t: TrackView) {
  const id = currentTrack.value?.id
  return id != null && id === t.id
}
function isDiscFirst(t: TrackView) {
  return hasMultipleDiscs.value && tracks.value.find((x) => x.disc_number === t.disc_number)?.id === t.id
}

// ── Playback ──────────────────────────────────────────────────────────────────
function trackToPlayable(t: TrackView): Track {
  const primary = t.files[0]
  return {
    id: t.id,
    title: t.title,
    artist: artistName.value,
    album: album.value?.title ?? '',
    duration: t.duration,
    stream_url: `/api/music/tracks/${t.id}/stream`,
    album_id: album.value?.id,
    artist_id: detail.value?.artist?.id,
    poster: useAlbumCoverUrl(artistSlug.value, albumSlug.value) ?? undefined,
    integrated_lufs: primary?.integrated_lufs != null ? parseFloat(primary.integrated_lufs) : null,
    true_peak_db: primary?.true_peak_db != null ? parseFloat(primary.true_peak_db) : null,
  }
}

async function playAll(shuffle: boolean) {
  if (!album.value) return
  // Semantic source: the server materializes (and truly shuffles) the album.
  await playContext({ kind: 'album', id: album.value.id }, { shuffle })
}

async function playFrom(track: TrackView) {
  if (!isPlayable(track)) return
  await playTracks(playableTracks.value.map(trackToPlayable), trackToPlayable(track))
}

async function playFromFile(track: TrackView, file: TrackFile) {
  // Queue stays album-ordered; only the chosen track switches to the explicit
  // file URL (e.g. the FLAC over the MP3). Others fall back to /stream.
  const playable = trackToPlayable(track)
  playable.stream_url = `/api/music/tracks/${track.id}/file/${file.id}`
  playable.track_file_id = file.id
  playable.integrated_lufs = file.integrated_lufs != null ? parseFloat(file.integrated_lufs) : null
  playable.true_peak_db = file.true_peak_db != null ? parseFloat(file.true_peak_db) : null
  await playTracks(tracks.value.filter(isPlayable).map(trackToPlayable), playable)
}

function queueAll() {
  void addToQueue(playableTracks.value.map(trackToPlayable))
}

async function startAlbumRadio() {
  if (!album.value) return
  await radio.startRadio({ kind: 'album', album_id: album.value.id })
}

// Row tap = play. Interactive children (quality picker, stars, ⋯) @click.stop.
function onRowTap(t: TrackView) {
  if (isPlayable(t)) void playFrom(t)
}
function primaryFile(t: TrackView): TrackFile | null { return t.files[0] ?? null }

// ── Context menu / ⋯ action sheet (shared builder) ───────────────────────────
function contextItemsFor(t: TrackView) {
  return actions.forTrack({
    id: t.id,
    title: t.title,
    artist: artistName.value,
    album: album.value?.title ?? '',
    duration: t.duration,
    album_id: album.value?.id,
    artist_id: detail.value?.artist?.id,
    artist_slug: artistSlug.value,
    album_slug: albumSlug.value,
    available: isPlayable(t),
  })
}
const sheetOpen = ref(false)
const sheetTrack = ref<TrackView | null>(null)
function openSheet(t: TrackView) {
  sheetTrack.value = t
  sheetOpen.value = true
}

function trackDragPayload(t: TrackView) {
  return { kind: 'track' as const, track: { id: t.id, title: t.title } }
}

// ── Admin: album metadata edit / identify ────────────────────────────────────
const { user } = useAuth()
const isAdmin = computed(() => user.value?.is_admin === true)
const showAlbumEdit = ref(false)
const showAlbumIdentify = ref(false)
function invalidateAlbum() {
  queryClient.invalidateQueries({ key: ['music', 'album', artistSlug.value, albumSlug.value] })
}
function onAlbumSaved() { showAlbumEdit.value = false; invalidateAlbum() }
function onAlbumIdentifyRequest() { showAlbumEdit.value = false; showAlbumIdentify.value = true }
function onAlbumIdentified() { showAlbumIdentify.value = false; invalidateAlbum() }

// ── Sonically similar albums (existing endpoint; 404 until analyzed) ──────────
interface SonicSimilarAlbumRow {
  id: number
  title: string
  album_slug: string
  artist_id: number
  artist_name: string
  artist_slug: string
  album_cover_path: string
  album_year: string
  distance: number
}
const sonicSimilar = ref<SonicSimilarAlbumRow[]>([])
async function loadSonicSimilar() {
  sonicSimilar.value = []
  if (!artistSlug.value || !albumSlug.value) return
  try {
    const res = await $heya('/api/music/artists/{artist_slug}/albums/{album_slug}/sonic-similar', {
      path: { artist_slug: artistSlug.value, album_slug: albumSlug.value },
      query: { limit: 8 },
    }) as { items: SonicSimilarAlbumRow[] }
    sonicSimilar.value = res.items ?? []
  } catch { /* no centroid yet */ }
}
watch([artistSlug, albumSlug], loadSonicSimilar, { immediate: true })

// ── More by this artist (existing albums endpoint; excludes current album) ────
interface ArtistAlbumRow {
  id: number
  title: string
  slug: string
  year: string
  album_type: string
  total_tracks: number
  track_count: number
  available: boolean
}
const moreByQuery = useQuery({
  key: () => ['music', 'artist', 'albums-brief', artistSlug.value],
  query: async () => ((await $heya('/api/music/artists/{slug}/albums', { path: { slug: artistSlug.value }, query: { limit: 40 } })) as { items: ArtistAlbumRow[] }).items ?? [],
  enabled: () => artistSlug.value.length > 0,
  staleTime: 1000 * 60 * 30,
  retry: 0,
})
const moreBy = computed<ArtistAlbumRow[]>(() => (moreByQuery.data.value ?? []).filter((a) => a.slug !== albumSlug.value).slice(0, 12))

// ── Live refresh ──────────────────────────────────────────────────────────────
if (import.meta.client) {
  const bus = useEventBus()
  bus.connect()
  const off = bus.on('media.updated', (e) => {
    const payload = e.payload as { media_item_id?: number } | undefined
    if (payload && detail.value && payload.media_item_id === detail.value.media_item_id) {
      invalidateAlbum()
    }
  })
  onBeforeUnmount(() => { off() })
}
</script>

<template>
  <div v-if="loading" class="m-state">Loading…</div>
  <div v-else-if="!album" class="m-state">Album not found.</div>

  <div v-else class="album2" :class="{ 'hero-flush': !isPhone }" :style="toneStyle">
    <!-- ── HERO: floating square cover record-card + identity, over a blurred-
         cover art band that hard-clips at the ledger seam. The sharp full-bleed
         HeroCanvas layer is intentionally skipped — a square cover stretched
         across a wide hero crops badly; the crisp cover lives as the record
         card and the blurred wash carries the art (see report). ── -->
    <section class="hero">
      <div class="hero-art" :style="heroArtStyle" aria-hidden="true" />
      <div class="hero-grade" aria-hidden="true" />
      <div class="hero-tone" aria-hidden="true" />

      <div class="hero-inner">
        <div class="postercard">
          <Poster :idx="album.id" :src="coverUrl" aspect="1/1" class="postercard-img" :width="480" />
          <div v-if="!albumPlayable" class="postercard-missing"><MediaMissingBadge /></div>
        </div>

        <div class="grow hero-ink">
          <div class="eyebrow">
            <span class="eb-kind">{{ albumTypeLabel }}</span>
            <span class="sep">&rsaquo;</span>
            <NuxtLink :to="`/music/artist/${artistSlug}`" class="eb-artist">{{ artistName }}</NuxtLink>
          </div>

          <h1 class="title">{{ album.title }}</h1>

          <p class="metaline">
            <span v-if="album.year">{{ album.year }}</span>
            <template v-if="tracks.length">
              <span class="dot">&middot;</span><span>{{ tracks.length }} tracks</span>
            </template>
            <template v-if="totalDuration > 0">
              <span class="dot">&middot;</span><span>{{ formatRunTime(totalDuration) }}</span>
            </template>
            <template v-if="qualityLabel">
              <span class="dot">&middot;</span><span class="q">{{ qualityLabel }}</span>
            </template>
          </p>

          <div class="actions">
            <span v-if="!albumPlayable" class="missing"><Icon name="trash" :size="13" /> Missing on disk</span>

            <button class="btn-play" :disabled="!albumPlayable" @click="playAll(false)">
              <span class="tri" /> Play
            </button>
            <button class="pill album-secondary-action" :disabled="!albumPlayable" @click="playAll(true)">
              <Icon name="shuffle" :size="15" /> Shuffle
            </button>
            <button class="pill album-secondary-action" :disabled="!albumPlayable" @click="queueAll">
              <Icon name="plus" :size="15" /> Add to queue
            </button>
            <button class="pill album-secondary-action" :disabled="radio.starting.value || !albumPlayable" @click="startAlbumRadio">
              <Icon name="radio" :size="15" /> Station
            </button>

            <div class="hero-rating" @click.stop>
              <ReactionControl
                :model-value="albumRatings.get(album.id) ?? 0"
                size="sm"
                @update:model-value="(v) => onRateAlbum(album!.id, v)"
              />
            </div>

            <button v-if="isAdmin" class="pill icon hero-edit" title="Edit Metadata" aria-label="Edit metadata" @click="showAlbumEdit = true">
              <Icon name="pencil" :size="15" />
            </button>
          </div>

          <ExternalLinks kind="album" :external-ids="albumExternalIds" class="hero-ext" />
        </div>
      </div>
    </section>

    <!-- ── LEDGER at the hard-clip seam — user-facing facts only. ── -->
    <LedgerStrip :cells="ledgerCells" />

    <!-- ── BODY ── -->
    <main class="page">
      <!-- Tracklist: .trk ledger rows, every feature preserved + Track info ⋯. -->
      <section class="section">
        <SectionHeader title="Tracks" :subtitle="String(tracks.length)" />

        <div class="trklist">
          <template v-for="t in tracks" :key="t.id">
            <div v-if="isDiscFirst(t)" class="disc-head">Disc {{ t.disc_number }}</div>

            <AppContextMenu :items="contextItemsFor(t)">
              <div
                class="trk"
                role="button"
                :class="{ 'trk-missing': !isPlayable(t), 'trk-active': isTrackActive(t) }"
                :draggable="!isCoarse && isPlayable(t)"
                @click="onRowTap(t)"
                @dragstart="isPlayable(t) && onDragStart($event, trackDragPayload(t))"
                @dragend="onDragEnd"
              >
                <div class="trk-n">
                  <VuMeter v-if="isTrackActive(t)" :playing="playing" class="trk-vu" />
                  <span v-else-if="isPlayable(t)" class="trk-num">{{ t.track_number || '—' }}</span>
                  <Icon v-else name="trash" :size="12" class="trk-missing-icon" :title="`${t.title} — missing on disk`" />
                  <button
                    v-if="isPlayable(t) && !isTrackActive(t)"
                    class="trk-hover-play"
                    type="button"
                    :title="`Play ${t.title}`"
                    @click.stop="playFrom(t)"
                  >
                    <Icon name="play" :size="13" />
                  </button>
                </div>

                <div class="trk-meta">
                  <div class="trk-t">{{ t.title }}</div>
                  <div v-if="t.files.length" class="trk-q" @click.stop>
                    <TrackQualityPicker
                      :files="t.files"
                      :selected-id="primaryFile(t)?.id"
                      @pick="playFromFile(t, $event)"
                    />
                  </div>
                </div>

                <div class="trk-stars" @click.stop>
                  <ReactionControl
                    :model-value="trackRatings.get(t.id) ?? 0"
                    size="sm"
                    @update:model-value="(v) => onRateTrack(t.id, v)"
                  />
                </div>

                <div class="trk-d">{{ t.duration ? formatTime(t.duration) : '' }}</div>

                <button
                  type="button"
                  class="trk-more"
                  aria-label="Track actions"
                  title="Track actions"
                  @click.stop="openSheet(t)"
                >
                  <Icon name="more" :size="18" />
                </button>
              </div>
            </AppContextMenu>
          </template>
        </div>
      </section>

      <!-- Sounds Like — sonic-similar albums (existing endpoint). -->
      <!-- About + editorial review — TheAudioDB via heya.media. Side by
           side when both exist; either alone takes the full row. -->
      <section v-if="albumDescription || albumReview" class="section">
        <div class="about-cols">
          <div v-if="albumDescription" class="about-col">
            <SectionHeader title="About" />
            <div class="prose">
              <p :class="{ collapsed: !aboutOpen && albumDescription.length > 480 }">{{ albumDescription }}</p>
              <button v-if="albumDescription.length > 480" class="see-all" @click="aboutOpen = !aboutOpen">
                {{ aboutOpen ? 'Less' : 'More' }}
              </button>
            </div>
          </div>
          <div v-if="albumReview" class="about-col">
            <SectionHeader title="Review" subtitle="TheAudioDB" />
            <div class="prose">
              <p :class="{ collapsed: !reviewOpen && albumReview.length > 480 }">{{ albumReview }}</p>
              <button v-if="albumReview.length > 480" class="see-all" @click="reviewOpen = !reviewOpen">
                {{ reviewOpen ? 'Less' : 'More' }}
              </button>
            </div>
          </div>
        </div>
      </section>

      <!-- Editions / pressings — labels+catalog numbers from MusicBrainz /
           Discogs, formats from Discogs/Bandcamp, external provider pages. -->
      <section v-if="sortedEditions.length" class="section">
        <SectionHeader title="Editions" :subtitle="String(sortedEditions.length)" />
        <p v-if="releaseEventsLine" class="ed-events">
          Released: {{ releaseEventsLine.text }}<span
            v-if="releaseEventsLine.moreCount"
            class="ed-events-more"
            :title="releaseEventsLine.moreTitle"
          > +{{ releaseEventsLine.moreCount }}</span>
        </p>
        <ul class="ed-list">
          <li v-for="(e, i) in visibleEditions" :key="`${e.provider}-${e.provider_id ?? i}`" class="ed-row">
            <span class="ed-provider">{{ providerLabel(e.provider) }}</span>
            <span class="ed-main">
              <span class="ed-title">{{ e.title || album?.title }}</span>
              <span v-if="editionLine(e)" class="ed-meta">{{ editionLine(e) }}</span>
            </span>
            <a
              v-if="e.link"
              :href="e.link"
              target="_blank"
              rel="noopener"
              class="ed-link"
              :title="`Open on ${providerLabel(e.provider)}`"
            ><Icon name="link" :size="12" /></a>
          </li>
        </ul>
        <button v-if="sortedEditions.length > EDITIONS_SHOWN" class="see-all" @click="editionsExpanded = !editionsExpanded">
          {{ editionsExpanded ? 'Show fewer' : `Show all ${sortedEditions.length}` }}
        </button>
      </section>

      <!-- Credits — performance credits aggregated across every track
           (MusicBrainz artist-relationships via the canonical recording
           document). Plain text — no local-artist linking yet. -->
      <section v-if="creditGroups.length" class="section">
        <SectionHeader title="Credits" :subtitle="String(creditGroups.length)" />
        <ul class="ed-list">
          <li v-for="g in creditGroups" :key="g.role" class="ed-row">
            <span class="ed-provider">{{ g.role }}</span>
            <span class="ed-main"><span class="ed-title">{{ g.names.join(', ') }}</span></span>
          </li>
        </ul>
      </section>

      <!-- Artwork — audiodb "extra render" gallery (back/CD/spine/case/flat/
           face). Albums have no media_item, so these ride the row as remote
           references through the image proxy. -->
      <section v-if="albumArtwork.length" class="section">
        <SectionHeader title="Artwork" :subtitle="String(albumArtwork.length)" />
        <div class="art-gallery">
          <button
            v-for="(a, i) in albumArtwork"
            :key="`${a.type}-${i}`"
            type="button"
            class="art-tile"
            :title="artworkLabel(a.type)"
            @click="openArtwork(i)"
          >
            <LoadingImage :src="a.url" :alt="artworkLabel(a.type)" class="art-tile-img" />
            <span class="art-tile-label">{{ artworkLabel(a.type) }}</span>
          </button>
        </div>
      </section>

      <section v-if="sonicSimilar.length" class="section">
        <SectionHeader title="Sounds Like" :subtitle="String(sonicSimilar.length)" />
        <AppRail :items="sonicSimilar" :tile-width="172" :phone-tile-width="132" aspect="1/1" :gap="18" snap :item-key="(r: any) => `sl-${r.id}`" memory-key="album-sounds-like">
          <template #default="{ item: r }">
            <AppContextMenu
              :items="actions.forAlbum({ id: r.id, title: r.title, artist_slug: r.artist_slug, album_slug: r.album_slug, artist_name: r.artist_name })"
            >
              <NuxtLink :to="`/music/artist/${r.artist_slug}/${r.album_slug}`" class="card-tile">
                <MusicCard
                  :src="useAlbumCoverUrl(r.artist_slug, r.album_slug)"
                  :title="r.title"
                  :subtitle="`${r.artist_name}${r.album_year ? ' · ' + r.album_year : ''}`"
                  :width="200"
                  @play="playContext({ kind: 'album', id: r.id })"
                />
              </NuxtLink>
            </AppContextMenu>
          </template>
        </AppRail>
      </section>

      <!-- More by this artist — existing albums endpoint, current album excluded. -->
      <section v-if="moreBy.length" class="section">
        <SectionHeader :title="`More by ${artistName}`" :subtitle="String(moreBy.length)">
          <template #actions>
            <NuxtLink :to="`/music/artist/${artistSlug}`" class="sec-more">All releases</NuxtLink>
          </template>
        </SectionHeader>
        <AppRail :items="moreBy" :tile-width="172" :phone-tile-width="132" aspect="1/1" :gap="18" snap :item-key="(a: any) => `mb-${a.id}`" memory-key="album-more-by">
          <template #default="{ item: a }">
            <AppContextMenu
              :items="actions.forAlbum({ id: a.id, title: a.title, artist_slug: artistSlug, album_slug: a.slug, artist_name: artistName, available: a.available })"
            >
              <NuxtLink :to="`/music/artist/${artistSlug}/${a.slug}`" class="card-tile">
                <MusicCard
                  :src="useAlbumCoverUrl(artistSlug, a.slug)"
                  :title="a.title"
                  :subtitle="a.year || '—'"
                  :badge-tl="a.album_type && a.album_type.toLowerCase() !== 'album' ? a.album_type.toUpperCase() : ''"
                  :missing="!a.available"
                  :width="200"
                  @play="playContext({ kind: 'album', id: a.id })"
                />
              </NuxtLink>
            </AppContextMenu>
          </template>
        </AppRail>
      </section>
    </main>

    <!-- Admin: album metadata edit + identify (preserved from the old page). -->
    <MetadataAlbumEditDialog
      :album="album"
      :show="showAlbumEdit"
      @saved="onAlbumSaved"
      @identify="onAlbumIdentifyRequest"
      @close="showAlbumEdit = false"
    />
    <MetadataAlbumIdentifyDialog
      :album="album ?? null"
      :show="showAlbumIdentify"
      @applied="onAlbumIdentified"
      @close="showAlbumIdentify = false"
    />

    <!-- Phone ⋯ target for track rows (play/queue/react/track-info/navigate). -->
    <ActionSheet
      v-model:open="sheetOpen"
      :items="sheetTrack ? contextItemsFor(sheetTrack) : []"
      :title="sheetTrack?.title"
    />
  </div>
</template>

<style scoped>
.m-state { color: var(--fg-3); padding: 32px var(--pad-fluid); }

/* The music shell owns the scroll root; this page publishes tone vars + lays
   out hero → ledger → body. On desktop/tablet `hero-flush` rides the hero band
   up under the fixed glass topbar (the shell re-pads its sidebar); phone drops
   it (`!isPhone`) so the compact music header keeps its clearance. */
.album2 { --oink: 233 236 242; padding-bottom: 48px; }

/* ═══ HERO ═════════════════════════════════════════════════════════════════ */
.hero {
  position: relative;
  min-height: 42vh;
  display: flex;
  align-items: flex-end;
  overflow: hidden; /* THE hard clip at the ledger seam */
}
/* Blurred-cover art band — always present (independent of the ambient toggle),
   dark grade for text contrast. Scrims below paint directly over artwork
   (CLAUDE.md exception → literal darks allowed). */
.hero-art {
  position: absolute;
  inset: 0;
  z-index: 0;
  background-size: cover;
  background-position: center;
  filter: blur(7px) brightness(0.75) saturate(1.2);
  transform: scale(1.06);
}
.hero-grade {
  position: absolute;
  inset: 0;
  z-index: 1;
  pointer-events: none;
  background:
    linear-gradient(90deg, rgb(10 12 16 / 0.72), rgb(10 12 16 / 0.28) 42%, rgb(10 12 16 / 0.05) 72%),
    linear-gradient(to top, rgb(10 12 16 / 0.7) 0%, rgb(10 12 16 / 0.24) 26%, rgb(10 12 16 / 0.1) 58%, rgb(10 12 16 / 0.3) 100%);
}
.hero-tone {
  position: absolute;
  inset: 0;
  z-index: 1;
  pointer-events: none;
  background: radial-gradient(90% 70% at 8% 100%, rgb(var(--tone-rgb) / 0.16), transparent 60%);
}

.hero-inner {
  position: relative;
  z-index: 2;
  width: 100%;
  display: flex;
  align-items: flex-end;
  gap: 40px;
  padding: 92px var(--pad-fluid) 34px;
}
.hero-inner > .grow { flex: 1; min-width: 0; }

/* poster record-card — square, directional shadow (top-left key light) */
.postercard { flex: 0 0 216px; }
.postercard-img {
  width: 100%;
  border-radius: var(--r-md);
  box-shadow:
    0 0 0 1px rgb(var(--oink) / 0.14),
    10px 18px 34px -12px rgb(0 0 0 / 0.8),
    24px 44px 90px -20px rgb(0 0 0 / 0.95);
}
.postercard { position: relative; }
.postercard-missing { position: absolute; inset: 0; }

/* mono eyebrow — album type (tone) › artist (muted link) */
.eyebrow {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 9px;
  margin-bottom: 16px;
  font: 600 11.5px var(--font-mono);
  letter-spacing: 0.22em;
  text-transform: uppercase;
}
.eyebrow .eb-kind { color: var(--tone); }
.eyebrow .sep { color: rgb(var(--oink) / 0.4); }
.eyebrow .eb-artist { color: rgb(var(--oink) / 0.6); text-decoration: none; transition: color 0.15s; }
.eyebrow .eb-artist:hover { color: rgb(var(--oink) / 0.95); }

/* Archivo display title (heya2.css .title, mixed-case wdth 115) */
.title {
  font-family: var(--font-display);
  font-size: clamp(2.1rem, 4.6vw, 3.9rem);
  font-weight: 800;
  font-variation-settings: "wdth" 115;
  letter-spacing: -0.022em;
  line-height: 0.99;
  color: rgb(var(--oink) / 0.98);
  text-shadow: 0 2px 30px rgb(0 0 0 / 0.45);
  text-wrap: balance;
  margin: 0;
  max-width: 20ch;
}

.metaline {
  margin-top: 14px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 12px;
  font: 500 12.5px var(--font-mono);
  letter-spacing: 0.04em;
  color: rgb(var(--oink) / 0.72);
}
.metaline .dot { color: rgb(var(--tone-rgb) / 0.85); }
.metaline .q { color: var(--tone); }

/* actions (shared with the artist hero grammar) */
.actions {
  margin-top: 24px;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.missing {
  display: inline-flex; align-items: center; gap: 5px;
  font: 600 11px var(--font-mono); text-transform: uppercase; letter-spacing: 0.08em;
  color: var(--bad); width: 100%;
}

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
  transition: transform 0.15s ease, box-shadow 0.15s ease,
    background 0.9s cubic-bezier(0.22, 1, 0.36, 1), color 0.9s cubic-bezier(0.22, 1, 0.36, 1);
}
.btn-play:hover:not([disabled]) {
  transform: translateY(-1px);
  box-shadow:
    0 0 0 1px rgb(var(--tone-rgb) / 0.6),
    0 0 40px rgb(var(--tone-rgb) / 0.6),
    8px 14px 48px -8px rgb(var(--tone-rgb) / 0.9);
}
.btn-play[disabled] { cursor: not-allowed; opacity: 0.4; box-shadow: 0 0 0 1px rgb(var(--oink) / 0.14); transform: none; }
.btn-play .tri {
  width: 0; height: 0;
  border-left: 11px solid var(--tone-ink, #0a0c10);
  border-top: 7px solid transparent;
  border-bottom: 7px solid transparent;
}

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
.pill:hover:not([disabled]) {
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.15);
  color: rgb(var(--oink));
  box-shadow: 0 0 24px rgb(var(--tone-rgb) / 0.28), 6px 10px 26px -10px rgb(0 0 0 / 0.75);
  transform: translateY(-1px);
}
.pill[disabled] { cursor: not-allowed; opacity: 0.4; }
.pill.icon { width: 42px; height: 42px; padding: 0; justify-content: center; }

.hero-rating {
  display: inline-flex;
  align-items: center;
  padding: 5px 10px;
  border-radius: 999px;
  background: rgb(var(--shade) / 0.4);
  border: 1px solid rgb(var(--oink) / 0.12);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
}
.hero-rating :deep(.reaction-btn) { color: rgb(var(--oink) / 0.7); }
.hero-rating :deep(.reaction-btn:hover) { color: rgb(var(--oink) / 0.95); }

.hero-ext { margin-top: 18px; }
.hero-ext :deep(a) { color: rgb(var(--oink) / 0.6); }
.hero-ext :deep(a:hover) { color: rgb(var(--oink) / 0.95); }

/* ═══ BODY ═════════════════════════════════════════════════════════════════ */
.page { padding: 0 var(--pad-fluid) 80px; }
.section { margin-top: 48px; }
.section:first-of-type { margin-top: 40px; }

.sec-more {
  font: 550 11px var(--font-mono);
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.5);
  text-decoration: none;
}
.sec-more:hover { color: var(--tone); }

/* ── Tracklist — .trk ledger rows ── */
.trklist { border-top: 1px solid var(--hair-strong); }
.disc-head {
  font: 600 10.5px var(--font-mono);
  letter-spacing: 0.2em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.45);
  padding: 20px 8px 8px;
  border-bottom: 1px solid var(--hair);
}
.trk {
  display: grid;
  /* Wider first column gives the centered hover-play breathing room and
     shifts the title block right. Last 40px column hosts the touch-only ⋯;
     collapsed on fine pointers (coarse override below). */
  grid-template-columns: 56px minmax(0, 1fr) auto 66px 0px;
  gap: 18px;
  align-items: center;
  padding: 9px 8px;
  border-bottom: 1px solid var(--hair);
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.12s;
  min-height: 46px;
}
@media (pointer: coarse) {
  .trk { grid-template-columns: 44px minmax(0, 1fr) auto 66px 40px; }
}
.trk:hover { background: rgb(var(--ink) / 0.03); }
.trk:hover .trk-num { opacity: 0; }
.trk:hover .trk-hover-play { opacity: 1; }
.trk-missing { opacity: 0.5; }
.trk-active { background: rgb(var(--tone-rgb) / 0.1); }
.trk-active:hover { background: rgb(var(--tone-rgb) / 0.12); }
.trk-active .trk-t { color: var(--tone); }

/* Number cell: everything (number / VU / hover-play) CENTERED in the column,
   and the hover-play overlays the number in place instead of hugging the
   title's edge (user 2026-07-15). */
.trk-n {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  height: 32px;
}
.trk-num {
  font: 600 13px var(--font-mono);
  color: rgb(var(--ink) / 0.4);
  font-variant-numeric: tabular-nums;
  transition: opacity 0.12s;
}
.trk-missing-icon { color: var(--bad); }
.trk-hover-play {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  width: 32px;
  height: 32px;
  border-radius: 50%;
  border: 0;
  background: var(--tone);
  color: var(--tone-ink, #0a0c10);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  opacity: 0;
  box-shadow: 0 0 0 1px rgb(var(--tone-rgb) / 0.4), 0 0 14px rgb(var(--tone-rgb) / 0.4);
  transition: opacity 0.12s, filter 0.12s;
}
.trk-hover-play:hover { filter: brightness(1.1); }

.trk-meta { min-width: 0; }
.trk-t {
  font-size: 14.5px;
  font-weight: 600;
  color: rgb(var(--ink) / 0.92);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.trk-q { margin-top: 2px; }
.trk-stars { display: inline-flex; justify-content: flex-end; }
.trk-d {
  font: 500 12px var(--font-mono);
  color: rgb(var(--ink) / 0.55);
  text-align: right;
  font-variant-numeric: tabular-nums;
}

/* ⋯ — touch-only affordance opening the shared ActionSheet (which includes
   Track info via the central builder). Desktop machines get the same menu via
   right-click (AppContextMenu), so the dot is omitted there entirely — the
   sheet reads as a mobile control on a fine-pointer device (user 2026-07-15). */
.trk-more {
  display: none;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: 0;
  background: transparent;
  color: rgb(var(--ink) / 0.5);
  cursor: pointer;
  border-radius: var(--r-sm);
  transition: color 0.12s, background 0.12s;
}
@media (pointer: coarse) {
  .trk-more { display: inline-flex; }
}
.trk-more:hover { color: rgb(var(--ink) / 0.9); background: rgb(var(--ink) / 0.06); }

/* ── Card rails (Sounds Like / More by) — AppRail owns the scroller/snap/
   shadow-room chrome now. ── */
.card-tile {
  display: block;
  text-decoration: none;
  color: inherit;
}

/* ═══ RESPONSIVE ═══════════════════════════════════════════════════════════ */
@media (max-width: 900px) {
  .postercard { flex-basis: 168px; }
}

@media (max-width: 720px) {
  .hero { min-height: 0; }
  .hero-inner {
    flex-direction: column;
    align-items: center;
    text-align: center;
    gap: 18px;
    padding: 22px var(--pad-fluid) 26px;
  }
  .postercard { flex-basis: auto; width: min(52vw, 220px); }
  .grow { width: 100%; }
  .eyebrow, .metaline, .actions { justify-content: center; }
  .title { max-width: 100%; font-size: clamp(1.7rem, 8vw, 2.6rem); }
  .actions { gap: 8px; row-gap: 10px; }
  .btn-play { flex: 1 1 100%; justify-content: center; height: 48px; }
  .pill:not(.icon) { flex: 1 1 auto; justify-content: center; height: 46px; }
  .pill.album-secondary-action {
    flex: 1 1 0;
    min-width: 0;
    gap: 5px;
    padding-inline: 7px;
    font-size: clamp(10px, 3vw, 12px);
    white-space: nowrap;
  }
  .pill.icon { width: 46px; height: 46px; }
  /* Album rating + metadata editor are desktop-sized affordances — the rows'
     ⋯ ActionSheet carries React + Track info on phone instead. */
  .hero-rating { display: none; }
  .hero-edit { display: none; }
  .hero-ext { display: none; }

  /* Track rows: drop the inline rating (ate the title at 390px) → ⋯ sheet
     carries React. Match Popular Tracks' two-line mobile ledger: the number,
     duration, and menu span both lines while quality sits under the title. */
  .trk {
    grid-template-columns: 30px minmax(0, 1fr) max-content 40px;
    grid-template-rows: auto auto;
    gap: 0 8px;
    padding: 9px 0;
    min-height: 58px;
  }
  .trk-stars { display: none; }
  .trk-n {
    grid-column: 1;
    grid-row: 1 / span 2;
  }
  .trk-meta { display: contents; }
  .trk-t {
    grid-column: 2;
    grid-row: 1;
    align-self: end;
  }
  .trk-q {
    grid-column: 2;
    grid-row: 2;
    align-self: start;
    min-width: 0;
    margin-top: -1px;
  }
  .trk-d {
    grid-column: 3;
    grid-row: 1 / span 2;
  }
  .trk-more {
    grid-column: 4;
    grid-row: 1 / span 2;
    opacity: 1;
    width: 40px;
    height: 40px;
  }
}

/* ── About / review prose (artist-page .prose vocabulary) ── */
.prose { font-size: 15.5px; line-height: 1.75; color: rgb(var(--ink) / 0.82); max-width: 64ch; }
.prose p.collapsed {
  display: -webkit-box;
  -webkit-line-clamp: 5;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.see-all {
  display: block;
  margin: 10px auto 0;
  background: none;
  border: 0;
  cursor: pointer;
  font: 600 12px var(--font-mono);
  letter-spacing: 0.08em;
  color: var(--tone, var(--gold));
}
.see-all:hover { text-decoration: underline; }
/* About + Review side by side (either alone spans the row). */
.about-cols {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(0, 1fr));
  gap: 48px;
  align-items: start;
}
.about-col { min-width: 0; }
@media (max-width: 1100px) {
  .about-cols { grid-template-columns: 1fr; gap: 36px; }
}

/* ── Editions — mono ledger rows ── */
.ed-list { list-style: none; margin: 0; padding: 0; }
.ed-row {
  display: flex;
  align-items: baseline;
  gap: 14px;
  padding: 9px 4px;
  border-bottom: 1px solid var(--hair);
  min-width: 0;
}
.ed-row:last-child { border-bottom: 0; }
.ed-provider {
  flex-shrink: 0;
  width: 92px;
  font: 550 10px var(--font-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.45);
}
.ed-main { display: flex; flex-wrap: wrap; gap: 4px 14px; align-items: baseline; min-width: 0; flex: 1; }
.ed-title { font-size: 13.5px; font-weight: 600; color: rgb(var(--ink) / 0.88); }
.ed-meta { font: 500 11.5px var(--font-mono); color: rgb(var(--ink) / 0.5); }
.ed-link { color: rgb(var(--ink) / 0.4); display: inline-flex; transition: color 0.15s; flex-shrink: 0; }
.ed-link:hover { color: var(--tone, var(--gold)); }

/* Release events — compact line above the Editions list. */
.ed-events {
  margin: 0 0 14px;
  font: 500 12px var(--font-mono);
  color: rgb(var(--ink) / 0.55);
}
.ed-events-more { color: var(--tone, var(--gold)); cursor: default; }

/* ── Artwork gallery — small square tiles, click opens the lightbox ── */
.art-gallery {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
}
.art-tile {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  width: 140px;
  padding: 0;
  border: 0;
  background: none;
  cursor: pointer;
}
.art-tile-img {
  display: block;
  width: 140px;
  aspect-ratio: 1 / 1;
  object-fit: cover;
  border-radius: var(--r-sm);
  box-shadow: 0 0 0 1px var(--hair);
  transition: box-shadow 0.15s, transform 0.15s;
}
.art-tile:hover .art-tile-img {
  box-shadow: 0 0 0 1px rgb(var(--tone-rgb) / 0.5);
  transform: translateY(-2px);
}
.art-tile-label {
  font: 550 10px var(--font-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: rgb(var(--ink) / 0.5);
}
</style>
