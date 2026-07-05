<template>
  <div v-if="loading" class="m-loading page-pad">Loading…</div>
  <div v-else-if="!artist" class="m-empty page-pad">Artist not found.</div>
  <div v-else class="artist-page">
    <!-- Hero (Plexify style): full-bleed backdrop with a circular poster
         on the left, bio + tags inline beside the name, and a stats line
         with rating below. Floating round actions on the right. -->
    <section class="hero">
      <div class="hero-backdrop" :style="backdropStyle" />
      <div class="hero-fade" />
      <div class="hero-content">
        <div class="hero-left">
          <Poster :idx="artist.id" :src="artistPosterUrl" aspect="1/1" class="hero-poster" :width="320" />
        </div>
        <div class="hero-meta">
          <div class="hero-kind">{{ heroKindLabel }}</div>
          <h1 class="hero-title">{{ artist.name }}</h1>
          <div v-if="(artist.tags?.length ?? 0) > 0" class="tag-row">
            <NuxtLink
              v-for="tag in (artist.tags ?? []).slice(0, 8)"
              :key="tag"
              :to="`/music/browse/genre/${encodeURIComponent(tag)}`"
              class="tag-chip"
            >{{ tag }}</NuxtLink>
          </div>
          <p v-if="artist.biography" class="hero-bio" :class="{ collapsed: !bioOpen && artist.biography.length > 320 }">
            {{ artist.biography }}
          </p>
          <button v-if="artist.biography && artist.biography.length > 320" class="hero-bio-toggle" @click="bioOpen = !bioOpen">
            {{ bioOpen ? 'Less' : 'More' }}
          </button>
          <div class="hero-stats">
            <div class="hero-stats-stars" @click.stop>
              <StarRating
                :model-value="artistRatings.get(artist.id) ?? 0"
                size="sm"
                @update:model-value="(v) => onRateArtist(artist!.id, v)"
              />
            </div>
            <template v-if="(artist.listeners ?? 0) > 0">
              <span class="stat-dot">·</span>
              <span class="stat">{{ formatBigInt(artist.listeners!) }} listeners</span>
            </template>
            <template v-if="(artist.playcount ?? 0) > 0">
              <span class="stat-dot">·</span>
              <span class="stat">{{ formatBigInt(artist.playcount!) }} plays</span>
            </template>
            <template v-if="lifecycleLabel">
              <span class="stat-dot">·</span>
              <span class="stat">{{ artist.artist_type === 'Group' ? 'Active' : 'Born' }} {{ lifecycleLabel }}</span>
            </template>
            <template v-if="originLabel">
              <span class="stat-dot">·</span>
              <span class="stat">{{ originLabel }}</span>
            </template>
            <template v-if="totalAlbums > 0">
              <span class="stat-dot">·</span>
              <span class="stat">{{ totalAlbums }} {{ totalAlbums === 1 ? 'release' : 'releases' }} · {{ totalTracks }} tracks</span>
            </template>
          </div>
          <ExternalLinks
            kind="artist"
            :external-ids="detail?.media_item?.external_ids ?? {}"
            class="hero-ext"
          />
        </div>
      </div>
      <!-- Floating round actions -->
      <div class="hero-floating-actions">
        <span v-if="!artistPlayable" class="hero-missing"><Icon name="trash" :size="13" /> Missing on disk</span>
        <button class="hero-round hero-round-primary" :disabled="!artistPlayable" @click="playAll(false)" title="Play">
          <Icon name="play" :size="22" />
        </button>
        <button class="hero-round" :disabled="!artistPlayable" @click="playAll(true)" title="Shuffle">
          <Icon name="shuffle" :size="18" />
        </button>
        <button class="hero-round" :disabled="!artistPlayable" @click="addAllToQueue" title="Add to queue">
          <Icon name="plus" :size="18" />
        </button>
        <button
          class="hero-round"
          @click="startArtistRadio"
          :disabled="radio.starting.value || !artistPlayable"
          title="Start radio from this artist"
        >
          <Icon name="radio" :size="18" />
        </button>
        <button v-if="isAdmin" class="hero-round hero-edit" title="Edit Metadata" @click="showMetadataEditor = true">
          <Icon name="pencil" :size="17" />
        </button>
      </div>
    </section>

    <!-- Popular Tracks: Plexify-style numbered list with star + duration -->
    <section v-if="topTracks.length" class="top-tracks artist-section">
      <div class="section-row-head tt-head">
        <h2 class="section-title-lg">Popular Tracks</h2>
        <button class="pill-btn" @click="playTopAll(false)" :disabled="!hasPlayableTopTracks">
          <Icon name="play" :size="13" /><span>Play</span>
        </button>
        <button class="pill-btn pill-btn-ghost" @click="playTopAll(true)" :disabled="!hasPlayableTopTracks">
          <Icon name="shuffle" :size="13" /><span>Shuffle</span>
        </button>
      </div>
      <ol class="tt-list">
        <li
          v-for="(t, idx) in topTracks.slice(0, ttExpanded ? topTracks.length : 8)"
          :key="`tt-${t.local_track_id}-${idx}`"
          class="tt-row"
          :class="{ 'tt-row-missing': !isTopTrackPlayable(t) }"
        >
          <div class="tt-leader">
            <span v-if="isTopTrackPlayable(t)" class="tt-rank">{{ idx + 1 }}</span>
            <Icon v-else name="trash" :size="12" class="tt-missing-icon" :title="`${t.title} — missing on disk`" />
            <button
              v-if="isTopTrackPlayable(t)"
              class="tt-hover-play"
              type="button"
              @click="playTopTrack(t)"
              :title="`Play ${t.title}`"
            >
              <Icon name="play" :size="12" />
            </button>
          </div>
          <div class="tt-meta">
            <span class="tt-title">{{ t.title }}</span>
            <template v-if="t.local_album_title">
              <span class="tt-album-sep">·</span>
              <NuxtLink
                :to="`/music/artist/${route.params.slug}/${t.local_album_slug}`"
                class="tt-album"
              >{{ t.local_album_title }}</NuxtLink>
            </template>
          </div>
          <div class="tt-stars" @click.stop>
            <StarRating
              :model-value="trackRatings.get(t.local_track_id!) ?? 0"
              size="sm"
              @update:model-value="(v) => onRateTrack(t.local_track_id!, v)"
            />
          </div>
          <div v-if="t.local_duration" class="tt-duration">{{ formatTime(t.local_duration) }}</div>
          <div v-else class="tt-duration" />
        </li>
      </ol>
      <button v-if="topTracks.length > 8" class="tt-more" @click="ttExpanded = !ttExpanded">
        {{ ttExpanded ? 'Show fewer' : `See all ${topTracks.length}` }}
      </button>
    </section>

    <!-- Band lifecycle: members of this group / groups this person plays in -->
    <section v-if="(artist.members?.length ?? 0) > 0" class="members artist-section">
      <div class="section-row-head">
        <h2 class="section-title-lg">Members</h2>
        <span class="more">{{ artist.members!.length }}</span>
      </div>
      <div class="member-grid">
        <div v-for="m in artist.members" :key="`mem-${m.name}`" class="member-chip">
          <div class="member-name">{{ m.name }}</div>
          <div v-if="m.begin_year || m.end_year" class="member-years">
            {{ m.begin_year || '?' }}–{{ m.end_year || 'present' }}
          </div>
        </div>
      </div>
    </section>

    <section v-if="(artist.groups?.length ?? 0) > 0" class="members artist-section">
      <div class="section-row-head">
        <h2 class="section-title-lg">Member of</h2>
        <span class="more">{{ artist.groups!.length }}</span>
      </div>
      <div class="member-grid">
        <div v-for="g in artist.groups" :key="`grp-${g.name}`" class="member-chip">
          <div class="member-name">{{ g.name }}</div>
          <div v-if="g.begin_year || g.end_year" class="member-years">
            {{ g.begin_year || '?' }}–{{ g.end_year || 'present' }}
          </div>
        </div>
      </div>
    </section>

    <!-- Discography by release kind -->
    <section
      v-for="group in groupedDiscography"
      :key="group.kind"
      class="discog artist-section"
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
          :class="{ 'discog-missing': !albumPlayable(album) }"
        >
          <div class="discog-art-wrap">
            <Poster :idx="album.id" :src="useAlbumCoverUrl(route.params.slug as string, album.slug)" aspect="1/1" class="discog-art" />
            <MediaMissingBadge v-if="!albumPlayable(album)" />
            <button v-if="albumPlayable(album)" class="discog-play" @click.stop.prevent="playAlbum(album, false)" title="Play album">
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
    <section v-if="sonicSimilar.length" class="similar artist-section">
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
          <Poster :idx="row.id" :src="`/api/media/${row.media_item_id}/image/poster`" aspect="1/1" :width="200" style="border-radius: 50%" />
          <div class="similar-tile-name">{{ row.name }}</div>
          <div class="similar-tile-source">sonic match</div>
        </NuxtLink>
      </div>
    </section>

    <!-- Similar artists — Last.fm + ListenBrainz via heya.media -->
    <section v-if="similar.length" class="similar artist-section">
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
          <Poster :idx="i" :src="row.image" aspect="1/1" :width="200" style="border-radius: 50%" />
          <div class="similar-tile-name">{{ row.name }}</div>
          <div class="similar-tile-source">{{ row.source }}</div>
        </component>
      </div>
    </section>

    <!-- External links + Wikipedia -->
    <section v-if="externalLinks.length || wikipediaLinks.length" class="links artist-section">
      <div class="section-row-head">
        <h2 class="section-title-lg">Around the web</h2>
      </div>
      <div v-if="externalLinks.length" class="link-grid">
        <a
          v-for="(l, i) in externalLinks"
          :key="`url-${i}`"
          :href="l.url"
          target="_blank"
          rel="noopener"
          class="link-chip"
        >
          <Icon name="link" :size="12" />
          <span>{{ l.type }}</span>
        </a>
      </div>
      <details v-if="wikipediaLinks.length" class="wiki-details">
        <summary class="wiki-summary">Wikipedia ({{ wikipediaLinks.length }} languages)</summary>
        <div class="link-grid wiki-grid">
          <a
            v-for="w in wikipediaLinks"
            :key="`wiki-${w.lang}`"
            :href="w.url"
            target="_blank"
            rel="noopener"
            class="link-chip"
          >
            <Icon name="link" :size="12" />
            <span>{{ w.lang }}</span>
          </a>
        </div>
      </details>
    </section>

    <div v-if="(artist.aliases?.length ?? 0) > 0" class="alias-row artist-section">
      <span class="alias-label">Also known as</span>
      <span class="alias-list">{{ artist.aliases!.join(' · ') }}</span>
    </div>

    <MetadataEditorModal
      v-if="detail"
      :media-id="detail.media_item.id"
      :show="showMetadataEditor"
      @close="onEditorClose"
    />
  </div>
</template>

<script setup lang="ts">
import type { AlbumView, Artist, ArtistTopTrackRow, MediaDetail, TrackView } from '~~/shared/types'
import type { Track } from '~/composables/usePlayer'
import { useQuery, useQueryClient } from '@tanstack/vue-query'

// slug keys + addresses the detail query so it shares the vue-query cache
// entry with the parent page's ['media','detail',slug] fetch — keying by
// mediaId created a second cache entry and re-ran the heaviest endpoint on
// every artist page view, sequentially after the page's own copy.
const props = defineProps<{ mediaId: number; slug: string }>()

const route = useRoute()
const { play, queue, formatTime } = usePlayer()
const radio = useRadio()

const artistRatings = useRatings('artist')
const trackRatings = useRatings('track')
async function onRateArtist(id: number, v: number) {
  try { await artistRatings.set(id, v) } catch { /* rollback handled */ }
}
async function onRateTrack(id: number, v: number) {
  try { await trackRatings.set(id, v) } catch { /* rollback handled */ }
}

async function startArtistRadio() {
  await radio.startRadio({ kind: 'artist', artist_slug: route.params.slug as string })
}

const bioOpen = ref(false)
const ttExpanded = ref(false)

const { user } = useAuth()
const isAdmin = computed(() => user.value?.is_admin === true)
const showMetadataEditor = ref(false)
const queryClient = useQueryClient()

function onEditorClose() {
  showMetadataEditor.value = false
  // Edits and refreshes land server-side; drop the cached detail so the
  // page (and this component) re-reads the updated artist.
  queryClient.invalidateQueries({ queryKey: ['media', 'detail', props.slug] })
}

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

interface SonicSimilarArtistRow {
  id: number
  name: string
  media_item_id: number
  media_slug: string
  distance: number
}

const { $heya } = useNuxtApp()
const detailQuery = useQuery({
  queryKey: ['media', 'detail', () => props.slug],
  queryFn: async () => (await $heya('/api/media/{id}', { path: { id: props.slug } })) as MediaDetail,
  staleTime: 1000 * 60 * 5,
})
const detail = computed<MediaDetail | null>(() => detailQuery.data.value ?? null)
const loading = computed(() => detailQuery.isPending.value)

const artistSlugForQueries = computed(() => detail.value?.media_item?.slug ?? (route.params.slug as string | undefined) ?? '')

const similarQuery = useQuery({
  queryKey: ['music', 'artist', 'similar', artistSlugForQueries],
  queryFn: async () => (await $heya('/api/music/artists/{slug}/similar', { path: { slug: artistSlugForQueries.value } })) as SimilarArtistRow[],
  enabled: () => artistSlugForQueries.value.length > 0,
  staleTime: 1000 * 60 * 30,
  retry: false,
})
const similar = computed<SimilarArtistRow[]>(() => similarQuery.data.value ?? [])

const sonicSimilarQuery = useQuery({
  queryKey: ['music', 'artist', 'sonic-similar', artistSlugForQueries, { limit: 12 }],
  queryFn: async () => ((await $heya('/api/music/artists/{slug}/sonic-similar', { path: { slug: artistSlugForQueries.value }, query: { limit: 12 } })) as { items: SonicSimilarArtistRow[] }).items ?? [],
  enabled: () => artistSlugForQueries.value.length > 0,
  staleTime: 1000 * 60 * 30,
  retry: false,
})
const sonicSimilar = computed<SonicSimilarArtistRow[]>(() => sonicSimilarQuery.data.value ?? [])

const topTracksQuery = useQuery({
  queryKey: ['music', 'artist', 'top-tracks', artistSlugForQueries, { limit: 25 }],
  queryFn: async () => ((await $heya('/api/music/artists/{slug}/top-tracks', { path: { slug: artistSlugForQueries.value }, query: { limit: 25 } })) as { items: ArtistTopTrackRow[] }).items ?? [],
  enabled: () => artistSlugForQueries.value.length > 0,
  staleTime: 1000 * 60 * 30,
  retry: false,
})
// Owned-only filter — Last.fm rows we can't play are noise on a library page.
// External links to Last.fm still live in the "Around the web" section.
// Deduped by local_track_id so "Usseewa" + "うっせぇわ" (which both resolve
// to the same recording) collapse to one rail entry.
const topTracks = computed<ArtistTopTrackRow[]>(() => {
  const seen = new Set<number>()
  const out: ArtistTopTrackRow[] = []
  for (const t of topTracksQuery.data.value ?? []) {
    if (!t.local_track_id || seen.has(t.local_track_id)) continue
    seen.add(t.local_track_id)
    out.push(t)
  }
  return out
})

const hasPlayableTopTracks = computed(() => topTracks.value.some(isTopTrackPlayable))

const artist = computed<Artist | null>(() => detail.value?.artist ?? null)
watch(artist, (a) => {
  if (a?.id && a.id > 0) artistRatings.load(a.id).catch(() => 0)
}, { immediate: true })

// Prime the per-track rating cache once the top-tracks list lands so the
// star widgets paint at correct values rather than starting at 0.
watch(topTracks, (rows) => {
  const ids = rows.filter((r) => r.local_track_id).map((r) => r.local_track_id!) as number[]
  if (ids.length) trackRatings.primeBulk(ids).catch(() => 0)
})

const albums = computed<AlbumView[]>(() => detail.value?.albums ?? [])

// Playability — a track needs a live file (TrackView.files is server-filtered
// to live files), an album needs a playable track, the artist needs a playable
// album. Missing items still render but can't be played.
function isTrackPlayable(t: TrackView) { return t.files.length > 0 }
function albumPlayable(al: AlbumView) { return al.tracks.some(isTrackPlayable) }
const artistPlayable = computed(() => albums.value.some(albumPlayable))
const playableTrackIds = computed(() => {
  const s = new Set<number>()
  for (const al of albums.value) for (const t of al.tracks) if (isTrackPlayable(t)) s.add(t.id)
  return s
})
function isTopTrackPlayable(t: ArtistTopTrackRow) {
  return !!t.local_track_id && playableTrackIds.value.has(t.local_track_id)
}

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

const heroKindLabel = computed(() => {
  const t = artist.value?.artist_type ?? ''
  if (t === 'Group') return 'BAND'
  if (t === 'Person') return 'ARTIST'
  if (t === 'Character') return 'CHARACTER'
  if (t) return t.toUpperCase()
  return 'ARTIST'
})

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

// Birthplace can come through as a Wikidata QID we don't yet resolve; only
// show when it's a human-readable token.
const originLabel = computed(() => {
  const bp = artist.value?.birthplace ?? ''
  if (!bp) return ''
  if (/^Q\d+$/.test(bp)) return ''
  return bp
})

const lifecycleLabel = computed(() => {
  const a = artist.value
  if (!a) return ''
  const start = a.begin_year ? String(a.begin_year) : (a.begin_date || '')
  const end = a.deathday || a.end_date || (a.ended ? '?' : '')
  if (!start && !end) return ''
  if (a.artist_type === 'Group') {
    if (start && end) return `${start}–${end}`
    if (start) return `since ${start}`
    return end
  }
  if (start && a.deathday) return `${start} – ${a.deathday}`
  return start
})

const externalLinks = computed(() => {
  const seen = new Set<string>()
  const out: { type: string; url: string }[] = []
  for (const l of (artist.value?.urls ?? [])) {
    if (!l.url || seen.has(l.url)) continue
    seen.add(l.url)
    out.push({ type: l.type || 'link', url: l.url })
  }
  out.sort((a, b) => a.type.localeCompare(b.type))
  return out
})

const wikipediaLinks = computed(() => {
  const links = artist.value?.wikipedia_links ?? {}
  return Object.entries(links)
    .map(([lang, url]) => ({ lang, url }))
    .sort((a, b) => a.lang.localeCompare(b.lang))
})

function formatBigInt(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1).replace(/\.0$/, '')}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, '')}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1).replace(/\.0$/, '')}K`
  return n.toLocaleString()
}

function trackFromAlbum(album: AlbumView, t: TrackView): Track {
  const primary = t.files[0]
  return {
    id: t.id,
    title: t.title,
    artist: artist.value?.name ?? '',
    album: album.title,
    duration: t.duration,
    stream_url: `/api/music/tracks/${t.id}/stream`,
    album_id: album.id,
    artist_id: artist.value?.id,
    poster: useAlbumCoverUrl(route.params.slug as string, album.slug) ?? undefined,
    integrated_lufs: primary?.integrated_lufs != null ? parseFloat(primary.integrated_lufs) : null,
    true_peak_db: primary?.true_peak_db != null ? parseFloat(primary.true_peak_db) : null,
  }
}

async function playAlbum(album: AlbumView, shuffle: boolean) {
  let tracks = album.tracks.filter(isTrackPlayable).map((t) => trackFromAlbum(album, t))
  if (shuffle) tracks = [...tracks].sort(() => Math.random() - 0.5)
  if (!tracks.length) return
  queue.value = tracks
  await play(tracks[0])
}

async function playAll(shuffle: boolean) {
  let tracks: Track[] = []
  for (const al of albums.value) {
    for (const t of al.tracks) if (isTrackPlayable(t)) tracks.push(trackFromAlbum(al, t))
  }
  if (shuffle) tracks = [...tracks].sort(() => Math.random() - 0.5)
  if (!tracks.length) return
  queue.value = tracks
  await play(tracks[0])
}

function addAllToQueue() {
  const tracks: Track[] = []
  for (const al of albums.value) {
    for (const t of al.tracks) if (isTrackPlayable(t)) tracks.push(trackFromAlbum(al, t))
  }
  queue.value = [...queue.value, ...tracks]
}

function topTrackToTrack(t: ArtistTopTrackRow): Track {
  return {
    id: t.local_track_id!,
    title: t.title,
    artist: artist.value?.name ?? '',
    album: t.local_album_title ?? '',
    duration: t.local_duration ?? 0,
    stream_url: `/api/music/tracks/${t.local_track_id}/stream`,
    album_id: t.local_album_id ?? 0,
    artist_id: artist.value?.id,
    poster: useAlbumCoverUrl(route.params.slug as string, t.local_album_slug ?? '') ?? undefined,
  }
}

async function playTopTrack(t: ArtistTopTrackRow) {
  if (!isTopTrackPlayable(t)) return
  const built = topTrackToTrack(t)
  queue.value = [built]
  await play(built)
}

async function playTopAll(shuffle: boolean) {
  let owned = topTracks.value.filter(isTopTrackPlayable).map(topTrackToTrack)
  if (!owned.length) return
  if (shuffle) owned = [...owned].sort(() => Math.random() - 0.5)
  queue.value = owned
  await play(owned[0]!)
}

if (import.meta.client) {
  const bus = useEventBus()
  bus.connect()
  const off = bus.on('media.updated', (e) => {
    const payload = e.payload as { media_item_id?: number } | undefined
    if (payload?.media_item_id === props.mediaId) {
      queryClient.invalidateQueries({ queryKey: ['media', 'detail', props.slug] })
      queryClient.invalidateQueries({ queryKey: ['music', 'artist', 'similar', artistSlugForQueries.value] })
      queryClient.invalidateQueries({ queryKey: ['music', 'artist', 'sonic-similar', artistSlugForQueries.value, { limit: 12 }] })
      queryClient.invalidateQueries({ queryKey: ['music', 'artist', 'top-tracks', artistSlugForQueries.value, { limit: 25 }] })
    }
  })
  onBeforeUnmount(() => { off() })
}
</script>

<style scoped>
.artist-page { padding-bottom: 80px; }
.m-loading, .m-empty { color: var(--fg-3); padding: 32px 40px; }

/* Inner sections use side padding from `.page-pad` but skip the 80px bottom
   gap so the rails stack tight on this page. The page-level breathing room
   comes from `.artist-page { padding-bottom: 80px }`. */
.artist-section {
  padding: 18px 40px 0;
}
@media (max-width: 1100px) {
  .artist-section { padding: 16px 24px 0; }
}

/* Hero ============================================================ */
.hero {
  position: relative;
  min-height: 460px;
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
  filter: saturate(1.05);
}
.hero-fade {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(180deg, rgba(0,0,0,0.05) 0%, rgba(0,0,0,0.55) 55%, var(--bg-0) 100%),
    linear-gradient(90deg, rgba(0,0,0,0.45) 0%, transparent 60%);
  z-index: 1;
}
.hero-content {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: flex-end;
  gap: 28px;
  padding: 26px 40px 28px;
  width: 100%;
}
.hero-left { flex-shrink: 0; align-self: flex-end; }
.hero-poster {
  width: 200px;
  height: 200px;
  border-radius: 50%;
  box-shadow: 0 22px 48px rgba(0,0,0,0.7), 0 0 0 1px rgba(255,255,255,0.06);
}
.hero-meta { flex: 1; min-width: 0; }
.hero-kind {
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.16em;
  color: var(--fg-1);
  opacity: 0.9;
  margin-bottom: 4px;
}
.hero-title {
  font-size: clamp(44px, 6.6vw, 76px);
  font-weight: 800;
  color: var(--fg-0);
  line-height: 0.96;
  margin-bottom: 10px;
  letter-spacing: -0.025em;
  text-shadow: 0 2px 24px rgba(0,0,0,0.55);
}
.tag-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 10px;
}
.tag-chip {
  display: inline-flex;
  padding: 3px 10px;
  border-radius: 999px;
  background: rgba(255,255,255,0.08);
  border: 1px solid rgba(255,255,255,0.10);
  font-size: 11px;
  color: var(--fg-0);
  text-decoration: none;
  text-transform: lowercase;
  transition: all 0.12s;
  backdrop-filter: blur(6px);
}
.tag-chip:hover {
  background: var(--gold-soft);
  color: var(--gold);
  border-color: var(--gold-soft);
}
.hero-bio {
  color: var(--fg-1);
  line-height: 1.5;
  font-size: 13px;
  max-width: 72ch;
  margin: 0;
  text-shadow: 0 1px 8px rgba(0,0,0,0.5);
}
.hero-bio.collapsed {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.hero-bio-toggle {
  display: inline-flex;
  align-items: center;
  margin-top: 4px;
  font-size: 12px;
  color: var(--gold);
  background: none;
  border: none;
  cursor: pointer;
  padding: 0;
}
.hero-bio-toggle:hover { color: var(--gold-bright); }
.hero-bio-toggle::before { content: '▾ '; margin-right: 4px; opacity: 0.7; }

.hero-stats {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
  font-size: 12px;
  color: var(--fg-1);
  font-family: var(--font-mono);
  letter-spacing: 0.02em;
  text-shadow: 0 1px 8px rgba(0,0,0,0.5);
}
.hero-stats-stars {
  display: inline-flex;
  margin-right: 4px;
}
.stat-dot { color: var(--fg-3); }
.stat { color: var(--fg-1); }
.hero-ext { margin-top: 10px; }

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
  width: 44px;
  height: 44px;
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
.hero-round-primary {
  width: 58px;
  height: 58px;
  background: var(--gold);
  color: var(--bg-0);
  border-color: transparent;
  box-shadow: 0 10px 24px var(--gold-glow);
}
.hero-round-primary:hover { background: var(--gold-bright); }
.hero-round:disabled { opacity: 0.4; cursor: default; pointer-events: none; }
.hero-missing {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 11px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em;
  color: #d96b6b; margin-right: 6px;
}

/* Popular Tracks ================================================== */
.top-tracks {}
.section-row-head { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
.section-row-head .more {
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin-left: auto;
}
.tt-head { margin-bottom: 8px; }

.pill-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 14px;
  border-radius: 999px;
  border: 0;
  background: var(--gold);
  color: var(--bg-0);
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
  transition: filter 0.12s;
}
.pill-btn:hover { filter: brightness(1.1); }
.pill-btn:disabled { opacity: 0.4; cursor: not-allowed; filter: none; }
.pill-btn-ghost {
  background: rgba(255,255,255,0.06);
  color: var(--fg-1);
}
.pill-btn-ghost:hover { background: rgba(255,255,255,0.10); }

.tt-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
}
.tt-row {
  display: grid;
  grid-template-columns: 36px 1fr auto 50px;
  align-items: center;
  gap: 14px;
  padding: 5px 10px;
  border-radius: var(--r-sm);
  transition: background 0.12s;
  min-height: 32px;
}
.tt-row:hover { background: rgba(255,255,255,0.04); }
.tt-row:hover .tt-rank { opacity: 0; }
.tt-row:hover .tt-hover-play { opacity: 1; }
.tt-row-missing { opacity: 0.55; }
.tt-missing-icon { color: #d96b6b; }
.tt-leader {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  height: 22px;
}
.tt-rank {
  font-family: var(--font-mono);
  color: var(--fg-3);
  font-size: 12px;
  transition: opacity 0.12s;
}
.tt-hover-play {
  position: absolute;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 22px;
  height: 22px;
  border-radius: 50%;
  border: 0;
  background: var(--gold);
  color: var(--bg-0);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.12s, filter 0.12s;
}
.tt-hover-play:hover { filter: brightness(1.1); }
.tt-hover-play.tt-hover-play-disabled {
  background: rgba(255,255,255,0.06);
  color: var(--fg-3);
  cursor: default;
}
.tt-external .tt-title { color: var(--fg-2); }
.tt-external .tt-album { color: var(--fg-3); }
.tt-meta {
  min-width: 0;
  overflow: hidden;
  display: flex;
  align-items: baseline;
  gap: 6px;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.tt-title {
  font-size: 13px;
  color: var(--fg-0);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tt-album-sep { color: var(--fg-3); font-size: 11px; }
.tt-album {
  font-size: 12px;
  color: var(--fg-2);
  text-decoration: none;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tt-album:hover { color: var(--gold); }
.tt-album-missing { font-style: italic; color: var(--fg-3); opacity: 0.7; }
.tt-stars { display: inline-flex; }
.tt-duration {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--fg-3);
  text-align: right;
}
.tt-more {
  margin-top: 6px;
  background: none;
  border: none;
  color: var(--gold);
  cursor: pointer;
  font-size: 12px;
  padding: 4px 10px;
}
.tt-more:hover { color: var(--gold-bright); }

/* Members / Groups ================================================ */
.member-grid { display: flex; flex-wrap: wrap; gap: 8px; }
.member-chip {
  padding: 7px 12px;
  border-radius: var(--r-sm);
  background: rgba(255,255,255,0.04);
  border: 1px solid var(--border);
  min-width: 140px;
}
.member-name { font-size: 13px; color: var(--fg-0); font-weight: 600; }
.member-years {
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  margin-top: 1px;
  letter-spacing: 0.03em;
}

/* Discography ===================================================== */
.discog-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 14px;
}
.discog-tile { text-decoration: none; color: inherit; display: block; }
.discog-art-wrap { position: relative; }
.discog-art { border-radius: var(--r-md); box-shadow: 0 8px 18px rgba(0,0,0,0.45); }
.discog-missing .discog-art { filter: grayscale(1); opacity: 0.55; }
.discog-play {
  position: absolute;
  right: 8px;
  bottom: 8px;
  width: 36px;
  height: 36px;
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
.discog-meta { margin-top: 8px; padding: 0 2px; }
.discog-title {
  font-size: 13px;
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
  margin-top: 2px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.dot { color: var(--fg-3); }

/* Similar rails =================================================== */
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

/* External + Wikipedia ============================================ */
.link-grid { display: flex; flex-wrap: wrap; gap: 6px; }
.link-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 11px;
  border-radius: 999px;
  background: rgba(255,255,255,0.05);
  border: 1px solid var(--border);
  font-size: 11px;
  color: var(--fg-1);
  text-decoration: none;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
  transition: all 0.12s;
}
.link-chip:hover {
  background: var(--gold-soft);
  color: var(--gold);
  border-color: var(--gold-soft);
}
.link-chip :deep(svg) { color: currentColor; opacity: 0.7; }

.wiki-details { margin-top: 10px; }
.wiki-summary {
  font-size: 11px;
  color: var(--fg-3);
  cursor: pointer;
  user-select: none;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  margin-bottom: 6px;
}
.wiki-summary:hover { color: var(--fg-1); }
.wiki-grid { margin-top: 6px; }

/* Aliases ========================================================= */
.alias-row {
  font-size: 11px;
  color: var(--fg-3);
}
.alias-label {
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  margin-right: 8px;
}
.alias-list { color: var(--fg-2); }

/* Responsive: stack hero poster + meta on narrow screens. Aligned to the
   720px phone convention (docs/ui.md "Responsive conventions") — was 700px.
   Centering + the `.hero` min-height reset are the only additions beyond
   that rename; desktop and the rest of this component are untouched. */
@media (max-width: 720px) {
  /* `.hero-floating-actions` is a flex sibling of `.hero-content` (both
     direct children of `.hero`), not nested inside it — `.hero` itself
     needs to switch to a column too, or the actions float beside the
     content instead of wrapping below it. */
  .hero { min-height: 0; flex-direction: column; }
  .hero-content { flex-direction: column; align-items: center; text-align: center; gap: 14px; padding: 20px 20px 22px; }
  .hero-left { align-self: center; }
  .hero-poster { width: 120px; height: 120px; }
  .hero-meta { width: 100%; }
  .tag-row { justify-content: center; }
  .hero-stats { justify-content: center; }
  .hero-ext :deep(.ext-links) { justify-content: center; }
  .hero-floating-actions { position: static; justify-content: center; flex-wrap: wrap; margin-top: 4px; }
  .hero-floating-actions .hero-round { width: 44px; height: 44px; }
  .hero-floating-actions .hero-round-primary { width: 56px; height: 56px; }
  /* Desktop `.hero` bottom-aligns its row children (align-items: flex-end);
     after the column flip above that axis becomes horizontal, shoving this
     shelf-sized row against the right edge (the edit button rendered half
     off-screen). Center it on its own axis instead. */
  .hero-floating-actions { align-self: center; gap: 14px; }
  /* The metadata editor is a desktop-sized surface — no entry point on
     phones (same call as the album page). */
  .hero-edit { display: none; }

  /* Popular Tracks: the 5-star widget ate the title column (titles
     truncated to a few characters at 390px). Ratings are hidden on phone —
     rate from the track's long-press / ⋯ menus instead — and the freed
     column plus taller rows give the text room to breathe. */
  .tt-stars { display: none; }
  .tt-row { grid-template-columns: 32px 1fr max-content; gap: 10px; padding: 10px 8px; min-height: 44px; }
}
</style>
