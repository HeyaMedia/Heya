<template>
  <div v-if="loading" class="m-loading page-pad">Loading…</div>
  <div v-else-if="!artist" class="m-empty page-pad">Artist not found.</div>
  <div v-else class="artist-page">
    <!-- Backdrop hero — plexify layout with floating round actions -->
    <section class="hero">
      <div class="hero-backdrop" :style="backdropStyle" />
      <div class="hero-fade" />
      <div class="hero-content">
        <div class="hero-meta">
          <div class="hero-kind">Artist</div>
          <h1 class="hero-title">{{ artist.name }}</h1>
          <div v-if="artist.disambiguation" class="hero-disambig">{{ artist.disambiguation }}</div>
          <div class="hero-counts">
            <span>{{ totalAlbums }} {{ totalAlbums === 1 ? 'release' : 'releases' }}</span>
            <span class="dot">·</span>
            <span>{{ totalTracks }} tracks</span>
            <span v-if="totalDuration > 0" class="dot">·</span>
            <span v-if="totalDuration > 0">{{ formatDuration(totalDuration) }}</span>
          </div>
          <ExternalLinks
            kind="artist"
            :external-ids="detail?.media_item?.external_ids ?? {}"
          />
        </div>
        <Poster :idx="artist.id" :src="artistPosterUrl" aspect="1/1" class="hero-poster" />
      </div>
      <!-- Floating round actions, bottom-right of the hero (plexify style) -->
      <div class="hero-floating-actions">
        <button class="hero-round hero-round-primary" @click="playAll(false)" title="Play">
          <Icon name="play" :size="22" />
        </button>
        <button class="hero-round" @click="playAll(true)" title="Shuffle">
          <Icon name="shuffle" :size="18" />
        </button>
        <button
          class="hero-round"
          :class="{ active: lovedArtist.isLoved(artist.id) }"
          @click="lovedArtist.toggle(artist.id)"
          :title="lovedArtist.isLoved(artist.id) ? 'Remove from My Artists' : 'Add to My Artists'"
        >
          <Icon :name="lovedArtist.isLoved(artist.id) ? 'heartfill' : 'heart'" :size="18" />
        </button>
        <button class="hero-round" @click="addAllToQueue" title="Add to queue">
          <Icon name="plus" :size="18" />
        </button>
      </div>
    </section>

    <!-- Bio -->
    <section v-if="artist.biography" class="bio page-pad">
      <p class="bio-text" :class="{ collapsed: !bioOpen && (artist.biography.length > 600) }">
        {{ artist.biography }}
      </p>
      <button v-if="artist.biography.length > 600" class="bio-toggle" @click="bioOpen = !bioOpen">
        {{ bioOpen ? 'Show less' : 'Read more' }}
      </button>
    </section>

    <!-- Discography by release kind — compact grid, click → album page -->
    <section
      v-for="group in groupedDiscography"
      :key="group.kind"
      class="discog page-pad"
    >
      <div class="section-row-head">
        <h2 class="section-title-lg">{{ group.label }}</h2>
        <span class="more">{{ group.albums.length }}</span>
      </div>
      <div class="discog-grid">
        <NuxtLink
          v-for="album in group.albums"
          :key="album.id"
          :to="`/music/artist/${route.params.slug}/${album.slug}`"
          class="discog-tile card-tile"
        >
          <div class="discog-art-wrap">
            <Poster :idx="album.id" :src="useAlbumCoverUrl(album.id)" aspect="1/1" class="discog-art" />
            <button class="discog-play" @click.stop.prevent="playAlbum(album, false)" title="Play album">
              <Icon name="play" :size="14" />
            </button>
          </div>
          <div class="discog-meta">
            <div class="discog-title">{{ album.title }}</div>
            <div class="discog-sub">
              {{ album.year || '—' }}
              <span v-if="album.tracks.length" class="dot">·</span>
              <span v-if="album.tracks.length">{{ album.tracks.length }} tracks</span>
            </div>
          </div>
        </NuxtLink>
      </div>
    </section>

    <!-- Sonic similar — local pgvector centroids -->
    <section v-if="sonicSimilar.length" class="similar page-pad">
      <div class="section-row-head">
        <h2 class="section-title-lg">Sounds Like</h2>
        <span class="more">{{ sonicSimilar.length }}</span>
      </div>
      <div class="similar-row">
        <NuxtLink
          v-for="row in sonicSimilar"
          :key="row.id"
          :to="`/music/artist/${row.media_slug}`"
          class="similar-tile card-tile"
          :title="`${row.name} — cosine distance ${row.distance.toFixed(3)}`"
        >
          <Poster :idx="row.id" :src="`/api/media/${row.media_item_id}/image/poster`" aspect="1/1" style="border-radius: 50%" />
          <div class="similar-tile-name">{{ row.name }}</div>
          <div class="similar-tile-source">sonic match</div>
        </NuxtLink>
      </div>
    </section>

    <!-- Similar artists — Last.fm + ListenBrainz via heya.media -->
    <section v-if="similar.length" class="similar page-pad">
      <div class="section-row-head">
        <h2 class="section-title-lg">Similar Artists</h2>
        <span class="more">{{ similar.length }}</span>
      </div>
      <div class="similar-row">
        <component
          :is="row.local_slug ? 'NuxtLink' : 'div'"
          v-for="(row, i) in similar"
          :key="row.name + i"
          :to="row.local_slug ? `/music/artist/${row.local_slug}` : undefined"
          class="similar-tile card-tile"
          :class="{ 'similar-external': !row.local_slug }"
          :title="row.local_slug ? `Open ${row.name}` : `${row.name} (not in library)`"
        >
          <Poster :idx="i" :src="row.image" aspect="1/1" style="border-radius: 50%" />
          <div class="similar-tile-name">{{ row.name }}</div>
          <div class="similar-tile-source">{{ row.source }}</div>
        </component>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import type { AlbumView, Artist, MediaDetail, TrackView } from '~~/shared/types'
import type { Track } from '~/composables/usePlayer'

const props = defineProps<{ mediaId: number }>()

const route = useRoute()
const { play, queue, formatTime } = usePlayer()
const lovedArtist = useLovedEntity('artist')
if (import.meta.client) lovedArtist.ensureLoaded()

const detail = ref<MediaDetail | null>(null)
const loading = ref(true)
const bioOpen = ref(false)

interface SimilarArtistRow {
  name: string
  mbid?: string
  image?: string
  score: number
  source: string
  url?: string
  local_slug?: string
  local_artist_id?: number
}
const similar = ref<SimilarArtistRow[]>([])

// Sonic-similarity from the local pgvector artist centroids.
// Distinct from `similar` above (Last.fm/LB graph) — these only
// exist for artists whose tracks have been analyzed.
interface SonicSimilarArtistRow {
  id: number
  name: string
  media_item_id: number
  media_slug: string
  distance: number
}
const sonicSimilar = ref<SonicSimilarArtistRow[]>([])

const artist = computed<Artist | null>(() => detail.value?.artist ?? null)
const albums = computed<AlbumView[]>(() => detail.value?.albums ?? [])

const artistPosterUrl = computed(() => {
  if (!detail.value?.media_item) return null
  return `/api/media/${detail.value.media_item.id}/image/poster`
})
const backdropStyle = computed(() => {
  if (!detail.value?.media_item) return {}
  return { backgroundImage: `url(/api/media/${detail.value.media_item.id}/image/backdrop)` }
})

const totalAlbums = computed(() => albums.value.length)
const totalTracks = computed(() => albums.value.reduce((sum, al) => sum + al.tracks.length, 0))
const totalDuration = computed(() =>
  albums.value.reduce((sum, al) => sum + al.tracks.reduce((s, t) => s + (t.duration || 0), 0), 0),
)

const KIND_ORDER = ['album', 'ep', 'single', 'compilation', 'live', 'soundtrack', 'remix', 'demo', 'other']
const KIND_LABEL: Record<string, string> = {
  album: 'Albums',
  ep: 'EPs',
  single: 'Singles',
  compilation: 'Compilations',
  live: 'Live',
  soundtrack: 'Soundtracks',
  remix: 'Remixes',
  demo: 'Demos',
  other: 'Other',
}

const groupedDiscography = computed(() => {
  const byKind = new Map<string, AlbumView[]>()
  for (const al of albums.value) {
    const kind = (al.album_type || 'album').toLowerCase()
    const bucket = KIND_LABEL[kind] ? kind : 'other'
    if (!byKind.has(bucket)) byKind.set(bucket, [])
    byKind.get(bucket)!.push(al)
  }
  for (const list of byKind.values()) {
    list.sort((a, b) => {
      const ay = parseInt(a.year || '0', 10) || 0
      const by = parseInt(b.year || '0', 10) || 0
      return by - ay
    })
  }
  return KIND_ORDER
    .filter((k) => byKind.has(k))
    .map((kind) => ({ kind, label: KIND_LABEL[kind] ?? kind, albums: byKind.get(kind)! }))
})

function trackFromAlbum(album: AlbumView, t: TrackView): Track {
  const primary = t.files[0]
  return {
    id: t.id,
    title: t.title,
    artist: artist.value?.name ?? '',
    album: album.title,
    duration: t.duration,
    stream_url: `/api/tracks/${t.id}/stream`,
    album_id: album.id,
    artist_id: artist.value?.id,
    poster: useAlbumCoverUrl(album.id) ?? undefined,
    integrated_lufs: primary?.integrated_lufs != null ? parseFloat(primary.integrated_lufs) : null,
    true_peak_db: primary?.true_peak_db != null ? parseFloat(primary.true_peak_db) : null,
  }
}

async function playAlbum(album: AlbumView, shuffle: boolean) {
  let tracks = album.tracks.map((t) => trackFromAlbum(album, t))
  if (shuffle) tracks = [...tracks].sort(() => Math.random() - 0.5)
  if (!tracks.length) return
  queue.value = tracks
  await play(tracks[0])
}

async function playAll(shuffle: boolean) {
  let tracks: Track[] = []
  for (const al of albums.value) {
    for (const t of al.tracks) tracks.push(trackFromAlbum(al, t))
  }
  if (shuffle) tracks = [...tracks].sort(() => Math.random() - 0.5)
  if (!tracks.length) return
  queue.value = tracks
  await play(tracks[0])
}

function addAllToQueue() {
  const tracks: Track[] = []
  for (const al of albums.value) {
    for (const t of al.tracks) tracks.push(trackFromAlbum(al, t))
  }
  queue.value = [...queue.value, ...tracks]
}

function formatDuration(seconds: number) {
  if (seconds < 3600) return formatTime(seconds)
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return `${h}h ${m}m`
}

async function loadDetail() {
  loading.value = true
  try {
    const { $heya } = useNuxtApp()
    // /api/media/{id} accepts slug or numeric ID — spec types id as string.
    detail.value = await $heya('/api/media/{id}', { path: { id: String(props.mediaId) } }) as MediaDetail
    if (artist.value?.id) {
      const artistId = artist.value.id
      // Fire both off in parallel — they're cheap and the artist page is the
      // hot path. Each promise swallows its own failure so a missing sonic
      // index doesn't blow away the similar-artists list.
      $heya('/api/music/artists/{id}/similar', { path: { id: artistId } })
        .then((rows) => { similar.value = (rows as SimilarArtistRow[]) ?? [] })
        .catch(() => { similar.value = [] })
      $heya('/api/music/artists/{id}/sonic-similar', { path: { id: artistId }, query: { limit: 12 } })
        .then((res) => { sonicSimilar.value = ((res as { items: SonicSimilarArtistRow[] }).items) ?? [] })
        .catch(() => { sonicSimilar.value = [] })
    }
  } catch {
    detail.value = null
    similar.value = []
    sonicSimilar.value = []
  } finally {
    loading.value = false
  }
}

watch(() => props.mediaId, loadDetail, { immediate: true })

if (import.meta.client) {
  const bus = useEventBus()
  bus.connect()
  const off = bus.on('media.updated', (e) => {
    const payload = e.payload as { media_item_id?: number } | undefined
    if (payload?.media_item_id === props.mediaId) loadDetail()
  })
  onBeforeUnmount(() => { off() })
}
</script>

<style scoped>
.artist-page { padding-bottom: 80px; }
.m-loading, .m-empty { color: var(--fg-3); padding: 32px 40px; }

.hero {
  position: relative;
  min-height: 380px;
  display: flex;
  align-items: flex-end;
  overflow: hidden;
  border-radius: 0 0 var(--r-md) var(--r-md);
}
.hero-backdrop {
  position: absolute;
  inset: 0;
  background-size: cover;
  background-position: center 25%;
  z-index: 0;
}
.hero-fade {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(180deg, rgba(0,0,0,0.2) 0%, rgba(0,0,0,0.55) 60%, var(--bg-0) 100%);
  z-index: 1;
}
.hero-content {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: flex-end;
  gap: 32px;
  padding: 32px 40px 36px;
  width: 100%;
}
.hero-meta { flex: 1; min-width: 0; }
.hero-poster {
  width: 160px;
  height: 160px;
  border-radius: 50%;
  box-shadow: 0 24px 48px rgba(0,0,0,0.6), 0 0 0 1px rgba(255,255,255,0.05);
  flex-shrink: 0;
  display: none; /* hidden when backdrop already shows the face */
}
@media (max-width: 700px) {
  .hero-poster { display: block; }
}
.hero-kind {
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.12em;
  color: var(--fg-2);
  margin-bottom: 6px;
}
.hero-title {
  font-size: clamp(44px, 6vw, 72px);
  font-weight: 800;
  color: var(--fg-0);
  line-height: 0.98;
  margin-bottom: 8px;
  letter-spacing: -0.025em;
  text-shadow: 0 2px 24px rgba(0,0,0,0.55);
}
.hero-disambig { font-size: 14px; color: var(--fg-2); margin-bottom: 10px; }
.hero-counts {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--fg-1);
  margin-bottom: 14px;
  font-family: var(--font-mono);
}
.dot { color: var(--fg-3); }

.hero-floating-actions {
  position: absolute;
  bottom: 28px;
  right: 32px;
  z-index: 3;
  display: flex;
  align-items: center;
  gap: 10px;
}
.hero-round {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  border: 1px solid rgba(255,255,255,0.12);
  background: rgba(0,0,0,0.4);
  color: var(--fg-0);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  backdrop-filter: blur(8px);
  transition: background 0.15s, transform 0.1s, color 0.15s;
}
.hero-round:hover { background: rgba(0,0,0,0.55); transform: scale(1.05); }
.hero-round:active { transform: scale(0.95); }
.hero-round.active { color: var(--gold); }
.hero-round-primary {
  width: 64px;
  height: 64px;
  background: var(--gold);
  color: var(--bg-0);
  border-color: transparent;
  box-shadow: 0 10px 24px var(--gold-glow);
}
.hero-round-primary:hover { background: var(--gold-bright); }

.bio { padding-top: 24px; max-width: 80ch; }
.bio-text { color: var(--fg-1); line-height: 1.65; font-size: 14px; white-space: pre-line; }
.bio-text.collapsed {
  display: -webkit-box;
  -webkit-line-clamp: 4;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.bio-toggle {
  font-size: 12px;
  color: var(--gold);
  margin-top: 8px;
  background: none;
  border: none;
  cursor: pointer;
}
.bio-toggle:hover { color: var(--gold-bright); }

.discog { padding-top: 18px; }
.discog-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 18px;
}
.discog-tile {
  text-decoration: none;
  color: inherit;
  display: block;
}
.discog-art-wrap { position: relative; }
.discog-art { border-radius: var(--r-md); box-shadow: 0 8px 18px rgba(0,0,0,0.45); }
.discog-play {
  position: absolute;
  right: 8px;
  bottom: 8px;
  width: 38px;
  height: 38px;
  border-radius: 50%;
  border: 0;
  background: var(--gold);
  color: var(--bg-0);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  opacity: 0;
  transform: translateY(6px);
  transition: opacity 0.2s, transform 0.2s;
  box-shadow: 0 6px 18px var(--gold-glow);
}
.discog-tile:hover .discog-play { opacity: 1; transform: none; }
.discog-meta { margin-top: 10px; padding: 0 2px; }
.discog-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.discog-sub {
  font-size: 11px;
  color: var(--fg-2);
  font-family: var(--font-mono);
  margin-top: 3px;
  display: flex;
  align-items: center;
  gap: 6px;
}

.similar { padding-top: 18px; }
.similar-row {
  display: grid;
  grid-auto-flow: column;
  grid-auto-columns: 130px;
  gap: 16px;
  overflow-x: auto;
  padding-bottom: 8px;
  scroll-snap-type: x proximity;
}
.similar-tile {
  text-align: center;
  text-decoration: none;
  color: inherit;
  scroll-snap-align: start;
}
.similar-tile.similar-external { cursor: default; opacity: 0.7; }
.similar-tile.similar-external:hover { opacity: 1; }
.similar-tile-name {
  margin-top: 8px;
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.similar-tile-source {
  margin-top: 2px;
  font-size: 9px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-3);
}
</style>
