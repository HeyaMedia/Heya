<template>
  <div class="music-home page-pad">
    <h1 class="mh-greeting">{{ greeting }}</h1>

    <!-- 1. Mixes for You — seed-artist poster, "MIX" chip top-left. -->
    <MusicScrollRow
      v-if="mixes.length"
      title="Mixes for You"
      :card-size="220"
    >
      <AppContextMenu
        v-for="mix in mixes"
        :key="`mix-${mix.seed_artist_id}`"
        :items="actions.forMix({ name: mix.name, seed_artist_slug: mix.seed_artist_slug, tracks: mix.tracks.map(mixTrackToEntity) })"
      >
      <NuxtLink
        :to="`/music/mix/${mix.seed_artist_slug}`"
        class="mh-card-link"
      >
        <MusicCard
          :src="usePosterUrl({ id: mix.seed_artist_media_item_id, public_id: mix.seed_artist_media_item_public_id })"
          :alt="mix.name"
          :title="mix.name"
          :subtitle="`${mix.tracks.length} tracks`"
          badge-tl="Mix"
          @play="playMix(mix)"
        />
      </NuxtLink>
      </AppContextMenu>
    </MusicScrollRow>

    <!-- 2. Recently Added — album_type as a chip when EP/single/etc. -->
    <MusicScrollRow
      v-if="recentAlbums.length"
      title="Recently Added"
      title-href="/music/albums"
      :card-size="170"
    >
      <AppContextMenu
        v-for="al in recentAlbums"
        :key="`ra-${al.id}`"
        :items="actions.forAlbum({ id: al.id, title: al.title, artist_slug: al.artist_slug, album_slug: al.slug, artist_name: al.artist_name, available: al.available })"
      >
      <NuxtLink
        :to="`/music/artist/${al.artist_slug}/${al.slug}`"
        class="mh-card-link"
        :draggable="!isCoarse"
        @dragstart="onDragStart($event, { kind: 'album', title: al.title, artist_slug: al.artist_slug, album_slug: al.slug })"
        @dragend="onDragEnd"
      >
        <MusicCard
          :src="useAlbumCoverUrl(al.artist_slug, al.slug)"
          :alt="al.title"
          :title="al.title"
          :subtitle="`${al.artist_name}${al.year ? ' · ' + al.year : ''}`"
          :badge-tl="al.album_type && al.album_type !== 'album' ? al.album_type : ''"
          :missing="al.available === false"
          @play="playAlbum(al)"
        />
      </NuxtLink>
      </AppContextMenu>
    </MusicScrollRow>

    <!-- 3. Recently Played Artists — square card with "Artist" badge,
         so the visual style matches every other shelf. -->
    <MusicScrollRow
      v-if="recentArtists.length"
      title="Recently Played"
      title-href="/music/artists"
      :card-size="170"
    >
      <AppContextMenu
        v-for="a in recentArtists"
        :key="`artist-${a.artist_id}`"
        :items="actions.forArtist({ id: a.artist_id, name: a.artist_name, slug: a.artist_slug, media_item_id: a.media_item_id, available: a.available })"
      >
      <NuxtLink
        :to="`/music/artist/${a.artist_slug}`"
        class="mh-card-link"
      >
        <MusicCard
          :src="usePosterUrl({ id: a.media_item_id, public_id: a.media_item_public_id })"
          :alt="a.artist_name"
          :title="a.artist_name"
          :subtitle="`${a.album_count} albums · ${a.track_count} tracks`"
          badge-tl="Artist"
          :missing="a.available === false"
          @play="playArtist(a.artist_slug, a.artist_name)"
        />
      </NuxtLink>
      </AppContextMenu>
    </MusicScrollRow>

    <!-- 4. On This Day — year chip top-left, anniversary releases. -->
    <MusicScrollRow
      v-if="onThisDay.length"
      title="On This Day"
      :card-size="170"
    >
      <AppContextMenu
        v-for="al in onThisDay"
        :key="`otd-${al.id}`"
        :items="actions.forAlbum({ id: al.id, title: al.title, artist_slug: al.artist_slug, album_slug: al.slug, artist_name: al.artist_name })"
      >
      <NuxtLink
        :to="`/music/artist/${al.artist_slug}/${al.slug}`"
        class="mh-card-link"
        :draggable="!isCoarse"
        @dragstart="onDragStart($event, { kind: 'album', title: al.title, artist_slug: al.artist_slug, album_slug: al.slug })"
        @dragend="onDragEnd"
      >
        <MusicCard
          :src="useAlbumCoverUrl(al.artist_slug, al.slug)"
          :alt="al.title"
          :title="al.title"
          :subtitle="al.artist_name"
          :badge-tl="String(al.release_year)"
          @play="playOnThisDay(al)"
        />
      </NuxtLink>
      </AppContextMenu>
    </MusicScrollRow>

    <!-- 5. Recent Playlists — "Playlist" chip differentiates from albums. -->
    <MusicScrollRow
      v-if="recentPlaylists.length"
      title="Your Playlists"
      :card-size="170"
    >
      <AppContextMenu
        v-for="p in recentPlaylists"
        :key="`pl-${p.id}`"
        :items="actions.forPlaylist({ id: p.id, name: p.name, track_count: p.track_count })"
      >
      <NuxtLink
        :to="`/music/playlist/${p.id}`"
        class="mh-card-link"
      >
        <MusicCard
          :src="p.cover_path || null"
          :alt="p.name"
          :title="p.name"
          :subtitle="`${p.track_count} tracks`"
          badge-tl="Playlist"
          :no-play="p.track_count === 0"
          @play="playPlaylist(p.id, p.name)"
        />
      </NuxtLink>
      </AppContextMenu>
    </MusicScrollRow>

    <!-- 6. More By <Artist> — rotates every 5 minutes. -->
    <template v-if="moreByArtists.length">
      <MusicScrollRow
        v-for="entry in moreByArtists"
        :key="`mb-${entry.artist_id}`"
        :title="`More by ${entry.artist_name}`"
        :title-href="`/music/artist/${entry.artist_slug}`"
        :card-size="170"
      >
        <AppContextMenu
          v-for="al in entry.albums"
          :key="`mb-al-${al.id}`"
          :items="actions.forAlbum({ id: al.id, title: al.title, artist_slug: entry.artist_slug, album_slug: al.slug, artist_name: entry.artist_name })"
        >
        <NuxtLink
          :to="`/music/artist/${entry.artist_slug}/${al.slug}`"
          class="mh-card-link"
          :draggable="!isCoarse"
          @dragstart="onDragStart($event, { kind: 'album', title: al.title, artist_slug: entry.artist_slug, album_slug: al.slug })"
          @dragend="onDragEnd"
        >
          <MusicCard
            :src="useAlbumCoverUrl(entry.artist_slug, al.slug)"
            :alt="al.title"
            :title="al.title"
            :subtitle="al.year || '—'"
            :badge-tl="al.album_type && al.album_type !== 'album' ? al.album_type : ''"
            @play="playArtistAlbum(entry.artist_slug, al)"
          />
        </NuxtLink>
        </AppContextMenu>
      </MusicScrollRow>
    </template>

    <!-- 7. More in <Genre> — single column, names only. Rotates every 5min. -->
    <div v-if="genreShelf && genreShelf.enabled && genreShelf.artists.length" class="mh-section mh-genre">
      <h2 class="section-title-lg mh-section-title">More in <span class="mh-genre-name">{{ genreShelf.genre }}</span></h2>
      <div class="mh-genre-grid">
        <AppContextMenu
          v-for="a in genreShelf.artists"
          :key="`g-${a.artist_id}`"
          :items="actions.forArtist({ id: a.artist_id, name: a.artist_name, slug: a.artist_slug })"
        >
        <NuxtLink
          :to="`/music/artist/${a.artist_slug}`"
          class="mh-genre-row"
        >
          <div class="mh-genre-name-cell">{{ a.artist_name }}</div>
          <div class="mh-genre-counts mono">{{ a.album_count }} · {{ a.track_count }}</div>
        </NuxtLink>
        </AppContextMenu>
      </div>
    </div>

    <!-- 8. Most Played in <Month> — play-count chip top-right. -->
    <MusicScrollRow
      v-if="mostPlayedShelf && mostPlayedShelf.enabled && mostPlayedShelf.albums.length"
      :title="mostPlayedShelf.window_label"
      :card-size="170"
    >
      <AppContextMenu
        v-for="al in mostPlayedShelf.albums"
        :key="`mp-${al.album_id}`"
        :items="actions.forAlbum({ id: al.album_id, title: al.album_title, artist_slug: al.artist_slug, album_slug: al.album_slug, artist_name: al.artist_name })"
      >
      <NuxtLink
        :to="`/music/artist/${al.artist_slug}/${al.album_slug}`"
        class="mh-card-link"
        :draggable="!isCoarse"
        @dragstart="onDragStart($event, { kind: 'album', title: al.album_title, artist_slug: al.artist_slug, album_slug: al.album_slug })"
        @dragend="onDragEnd"
      >
        <MusicCard
          :src="useAlbumCoverUrl(al.artist_slug, al.album_slug)"
          :alt="al.album_title"
          :title="al.album_title"
          :subtitle="al.artist_name"
          :badge-tr="`${al.play_count}×`"
          @play="playMostPlayedAlbum(al)"
        />
      </NuxtLink>
      </AppContextMenu>
    </MusicScrollRow>

    <!-- 9. Haven't Played in a While — one scroll-row per lapsed artist. -->
    <template v-if="lapsedShelf && lapsedShelf.enabled && lapsedShelf.artists.length">
      <h2 class="section-title-lg mh-section-title mh-lapsed-heading">{{ lapsedShelf.since_label }}</h2>
      <MusicScrollRow
        v-for="a in lapsedShelf.artists"
        :key="`lapsed-${a.artist_id}`"
        :title="`${formatLapsed(a)} ${a.artist_name}`"
        :title-href="`/music/artist/${a.artist_slug}`"
        :card-size="170"
      >
        <AppContextMenu
          v-for="al in a.albums"
          :key="`lapsed-al-${al.id}`"
          :items="actions.forAlbum({ id: al.id, title: al.title, artist_slug: a.artist_slug, album_slug: al.slug, artist_name: a.artist_name })"
        >
        <NuxtLink
          :to="`/music/artist/${a.artist_slug}/${al.slug}`"
          class="mh-card-link"
          :draggable="!isCoarse"
          @dragstart="onDragStart($event, { kind: 'album', title: al.title, artist_slug: a.artist_slug, album_slug: al.slug })"
          @dragend="onDragEnd"
        >
          <MusicCard
            :src="useAlbumCoverUrl(a.artist_slug, al.slug)"
            :alt="al.title"
            :title="al.title"
            :subtitle="al.year || '—'"
            @play="playArtistAlbum(a.artist_slug, { id: al.id, slug: al.slug, title: al.title, year: al.year, album_type: '' })"
          />
        </NuxtLink>
        </AppContextMenu>
      </MusicScrollRow>
    </template>

    <!-- 10. More from <Label> — rotates every 5 minutes. -->
    <MusicScrollRow
      v-if="labelShelf && labelShelf.enabled && labelShelf.albums.length"
      :title="`More from ${labelShelf.label}`"
      :card-size="170"
    >
      <AppContextMenu
        v-for="al in labelShelf.albums"
        :key="`label-${al.album_id}`"
        :items="actions.forAlbum({ id: al.album_id, title: al.album_title, artist_slug: al.artist_slug, album_slug: al.album_slug, artist_name: al.artist_name })"
      >
      <NuxtLink
        :to="`/music/artist/${al.artist_slug}/${al.album_slug}`"
        class="mh-card-link"
        :draggable="!isCoarse"
        @dragstart="onDragStart($event, { kind: 'album', title: al.album_title, artist_slug: al.artist_slug, album_slug: al.album_slug })"
        @dragend="onDragEnd"
      >
        <MusicCard
          :src="useAlbumCoverUrl(al.artist_slug, al.album_slug)"
          :alt="al.album_title"
          :title="al.album_title"
          :subtitle="`${al.artist_name}${al.album_year ? ' · ' + al.album_year : ''}`"
          @play="playLabelAlbum(al)"
        />
      </NuxtLink>
      </AppContextMenu>
    </MusicScrollRow>

    <!-- Empty fallback — first-launch state when nothing has loaded yet. -->
    <div
      v-if="!hasAnyContent"
      class="mh-empty"
    >
      No music yet — add a music library and let the scanner run.
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import { useQuery } from '@tanstack/vue-query'

// Inline row shape declarations — these mirror the sqlc-generated Go types
// 1:1, but kept local since they're only used in this file and the OpenAPI
// types are wider (carry pgtype shapes etc.) than we want to bind against.
interface MixTrack {
  track_id: number
  track_title: string
  duration: number
  album_id: number
  album_title: string
  album_slug: string
  album_cover_path: string
  album_year: string
  artist_id: number
  artist_name: string
  artist_slug: string
  play_count: number
}
interface Mix {
  seed_artist_id: number
  seed_artist_name: string
  seed_artist_slug: string
  seed_artist_media_item_id: number
  seed_artist_media_item_public_id?: string
  name: string
  tracks: MixTrack[]
}

interface RecentAlbumRow {
  id: number
  title: string
  slug: string
  year: string
  artist_name: string
  artist_slug: string
  album_type: string
  cover_path: string
  available?: boolean
}

interface RecentArtistRow {
  artist_id: number
  artist_name: string
  artist_slug: string
  media_item_id: number
  media_item_public_id?: string
  album_count: number
  track_count: number
  available?: boolean
}

interface OnThisDayRow {
  id: number
  title: string
  slug: string
  artist_name: string
  artist_slug: string
  release_year: number
}

interface PlaylistRow {
  id: number
  name: string
  cover_path: string
  track_count: number
}

interface ShelfAlbum {
  id: number
  slug: string
  title: string
  year: string
  album_type: string
}
interface MoreByEntry {
  artist_id: number
  artist_name: string
  artist_slug: string
  albums: ShelfAlbum[]
}

interface GenreArtist {
  artist_id: number
  artist_name: string
  artist_slug: string
  album_count: number
  track_count: number
}
interface GenreShelf {
  enabled: boolean
  genre: string
  artists: GenreArtist[]
}

interface MostPlayedAlbum {
  album_id: number
  album_title: string
  album_slug: string
  artist_name: string
  artist_slug: string
  play_count: number
}
interface MostPlayedShelf {
  enabled: boolean
  window_label: string
  albums: MostPlayedAlbum[]
}

interface LapsedAlbum {
  id: number
  slug: string
  title: string
  year: string
}
interface LapsedArtist {
  artist_id: number
  artist_name: string
  artist_slug: string
  last_played_at: string
  months_lapsed: number
  albums: LapsedAlbum[]
}
interface LapsedShelf {
  enabled: boolean
  since_label: string
  artists: LapsedArtist[]
}

interface LabelAlbum {
  album_id: number
  album_title: string
  album_slug: string
  album_year: string
  artist_name: string
  artist_slug: string
}
interface LabelShelf {
  enabled: boolean
  label: string
  albums: LabelAlbum[]
}

defineEmits<{ 'see-artists': []; 'see-albums': [] }>()

const { $heya } = useNuxtApp()

// All home shelves are vue-query'd — the QueryClient (registered in
// plugins/vue-query.client.ts) is module-scoped, so navigating to an
// album/artist page and back is a no-op against the cache: the second
// mount reads each query's cached payload synchronously, no flash.
//
// staleTime per shelf reflects the server-side Cache-Control:
//   - 30s for "fresh data" shelves (recently added, recently played etc.)
//   - 5min (300s) for the rotating shelves; refetchInterval also fires at
//     the same cadence so the seed-bucket rotation surfaces automatically.
//   - 1h for mixes-for-you since the underlying KNN computation is heavy.
//   - 6h for on-this-day since the input is the calendar date.
//
// Each query is independent so a slow one doesn't gate the others.

// Helper for our common envelope shape — every endpoint returns { items: T[] }.
async function fetchItems<T>(path: string): Promise<T[]> {
  const res = await $heya(path as never) as { items: T[] }
  return res.items ?? []
}

const mixesQuery = useQuery({
  queryKey: ['music', 'home', 'mixes-for-you'],
  queryFn: () => fetchItems<Mix>('/api/music/home/mixes-for-you'),
  staleTime: 1000 * 60 * 60,
})
const mixes = computed<Mix[]>(() => mixesQuery.data.value ?? [])

const recentAlbumsQuery = useQuery({
  queryKey: ['music', 'home', 'recently-added'],
  queryFn: () => fetchItems<RecentAlbumRow>('/api/music/home/recently-added'),
  staleTime: 1000 * 30,
})
const recentAlbums = computed<RecentAlbumRow[]>(() => recentAlbumsQuery.data.value ?? [])

const recentArtistsQuery = useQuery({
  queryKey: ['music', 'home', 'recently-played-artists'],
  queryFn: () => fetchItems<RecentArtistRow>('/api/music/home/recently-played-artists'),
  staleTime: 1000 * 30,
})
const recentArtists = computed<RecentArtistRow[]>(() => recentArtistsQuery.data.value ?? [])

const onThisDayQuery = useQuery({
  queryKey: ['music', 'home', 'on-this-day'],
  queryFn: () => fetchItems<OnThisDayRow>('/api/music/home/on-this-day'),
  staleTime: 1000 * 60 * 60 * 6,
})
const onThisDay = computed<OnThisDayRow[]>(() => onThisDayQuery.data.value ?? [])

const recentPlaylistsQuery = useQuery({
  queryKey: ['music', 'home', 'recent-playlists'],
  queryFn: () => fetchItems<PlaylistRow>('/api/music/home/recent-playlists'),
  staleTime: 1000 * 30,
})
const recentPlaylists = computed<PlaylistRow[]>(() => recentPlaylistsQuery.data.value ?? [])

// Rotating shelves — server rotates the seed every 5 minutes, so we
// refetchInterval at the same cadence. Each query refreshes independently,
// no shared setInterval to clean up on unmount (vue-query handles it).
const moreByArtistsQuery = useQuery({
  queryKey: ['music', 'home', 'more-by-artists'],
  queryFn: () => fetchItems<MoreByEntry>('/api/music/home/more-by-artists'),
  staleTime: 1000 * 60 * 5,
  refetchInterval: 1000 * 60 * 5,
})
const moreByArtists = computed<MoreByEntry[]>(() => moreByArtistsQuery.data.value ?? [])

const genreShelfQuery = useQuery({
  queryKey: ['music', 'home', 'more-in-genre'],
  queryFn: async () => (await $heya('/api/music/home/more-in-genre')) as GenreShelf,
  staleTime: 1000 * 60 * 5,
  refetchInterval: 1000 * 60 * 5,
})
const genreShelf = computed<GenreShelf | null>(() => genreShelfQuery.data.value ?? null)

const mostPlayedShelfQuery = useQuery({
  queryKey: ['music', 'home', 'most-played-last-month'],
  queryFn: async () => (await $heya('/api/music/home/most-played-last-month')) as MostPlayedShelf,
  staleTime: 1000 * 60 * 5,
})
const mostPlayedShelf = computed<MostPlayedShelf | null>(() => mostPlayedShelfQuery.data.value ?? null)

const lapsedShelfQuery = useQuery({
  queryKey: ['music', 'home', 'lapsed-artists'],
  queryFn: async () => (await $heya('/api/music/home/lapsed-artists')) as LapsedShelf,
  staleTime: 1000 * 60 * 5,
  refetchInterval: 1000 * 60 * 5,
})
const lapsedShelf = computed<LapsedShelf | null>(() => lapsedShelfQuery.data.value ?? null)

const labelShelfQuery = useQuery({
  queryKey: ['music', 'home', 'more-from-label'],
  queryFn: async () => (await $heya('/api/music/home/more-from-label')) as LabelShelf,
  staleTime: 1000 * 60 * 5,
  refetchInterval: 1000 * 60 * 5,
})
const labelShelf = computed<LabelShelf | null>(() => labelShelfQuery.data.value ?? null)

// Live refresh: a track/album file match (media.added) or the discography
// refresh landing new albums (media.updated) both carry the artist's
// media_type='music' — see useLiveRefresh for why this is coalesced rather
// than invalidating on every event.
useLiveRefresh([
  { events: ['media.added', 'media.updated'], filter: byMediaType('music'), keys: [['music', 'home', 'recently-added']] },
])

const greeting = computed(() => {
  const h = new Date().getHours()
  if (h < 12) return 'Good morning'
  if (h < 18) return 'Good afternoon'
  return 'Good evening'
})

const hasAnyContent = computed(() => {
  return mixes.value.length || recentAlbums.value.length || recentArtists.value.length
    || onThisDay.value.length || recentPlaylists.value.length || moreByArtists.value.length
    || (genreShelf.value?.enabled) || (mostPlayedShelf.value?.enabled)
    || (lapsedShelf.value?.enabled) || (labelShelf.value?.enabled)
})

function formatLapsed(a: LapsedArtist) {
  if (a.months_lapsed >= 12) {
    const years = Math.floor(a.months_lapsed / 12)
    return `${years}y ago — back to`
  }
  if (a.months_lapsed >= 1) return `${a.months_lapsed}mo ago — back to`
  return 'a while ago — back to'
}

const { play, queue } = usePlayer()
const actions = useMusicActions()
const { isCoarse } = useViewport()
const { onDragStart, onDragEnd } = useMusicDragDrop()

function mixTrackToEntity(t: MixTrack) {
  return {
    id: t.track_id, title: t.track_title, artist: t.artist_name, album: t.album_title,
    duration: t.duration, album_id: t.album_id, artist_id: t.artist_id,
    artist_slug: t.artist_slug, album_slug: t.album_slug,
  }
}

function mixTrackToTrack(t: MixTrack): Track {
  return {
    id: t.track_id,
    title: t.track_title,
    artist: t.artist_name,
    album: t.album_title,
    duration: t.duration,
    stream_url: `/api/music/tracks/${t.track_id}/stream`,
    album_id: t.album_id,
    artist_id: t.artist_id,
    artist_slug: t.artist_slug,
    album_slug: t.album_slug,
    poster: useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? undefined,
    source: 'mix',
  }
}

async function playMix(mix: Mix) {
  if (!mix.tracks.length) return
  const tracks = mix.tracks.map(mixTrackToTrack)
  queue.value = tracks
  await play(tracks[0]!)
}

async function playAlbumByArtistSlug(artistSlug: string, albumSlug: string, artistName: string, albumTitle: string, albumId: number) {
  try {
    const detail = await $heya('/api/music/artists/{artist_slug}/albums/{album_slug}', {
      path: { artist_slug: artistSlug, album_slug: albumSlug },
    }) as { tracks: { id: number; title: string; duration: number; files: { integrated_lufs: string | null; true_peak_db: string | null }[] }[] }
    if (!detail.tracks.length) return
    const tracks: Track[] = detail.tracks.map((t) => {
      const primary = t.files[0]
      return {
        id: t.id,
        title: t.title,
        artist: artistName,
        album: albumTitle,
        duration: t.duration,
        stream_url: `/api/music/tracks/${t.id}/stream`,
        album_id: albumId,
        artist_slug: artistSlug,
        album_slug: albumSlug,
        poster: useAlbumCoverUrl(artistSlug, albumSlug) ?? undefined,
        source: 'album',
        integrated_lufs: primary?.integrated_lufs != null ? parseFloat(primary.integrated_lufs) : null,
        true_peak_db: primary?.true_peak_db != null ? parseFloat(primary.true_peak_db) : null,
      }
    })
    queue.value = tracks
    await play(tracks[0]!)
  } catch {
    // Swallow — the NuxtLink wrapper still routes to the detail page on
    // outer click; we lose the play-from-mosaic gesture only.
  }
}

function playAlbum(al: RecentAlbumRow) {
  return playAlbumByArtistSlug(al.artist_slug, al.slug, al.artist_name, al.title, al.id)
}
function playArtistAlbum(artistSlug: string, al: ShelfAlbum) {
  return playAlbumByArtistSlug(artistSlug, al.slug, '', al.title, al.id)
}
function playMostPlayedAlbum(al: MostPlayedAlbum) {
  return playAlbumByArtistSlug(al.artist_slug, al.album_slug, al.artist_name, al.album_title, al.album_id)
}
function playOnThisDay(al: OnThisDayRow) {
  return playAlbumByArtistSlug(al.artist_slug, al.slug, al.artist_name, al.title, al.id)
}
function playLabelAlbum(al: LabelAlbum) {
  return playAlbumByArtistSlug(al.artist_slug, al.album_slug, al.artist_name, al.album_title, al.album_id)
}

// Play every track an artist has, ordered the same way the artist detail
// page lists them. One round-trip to /artists/{slug}/tracks. We don't need
// per-file replay-gain here — the track-stream endpoint resolves to the
// primary file and the engine reads loudness from /api/music/tracks/{id}/files
// when the player loads the next track.
async function playArtist(slug: string, artistName: string) {
  try {
    // Top-played-first endpoint so the queue opens on the artist's hits
    // and rolls into deeper cuts naturally.
    const res = await $heya('/api/music/artists/{slug}/play-queue', {
      path: { slug },
      query: { limit: 500 },
    }) as { items: { track_id: number; track_title: string; duration: number; album_id: number; album_title: string; album_slug: string; artist_id: number; artist_name: string; artist_slug: string }[] }
    const tracks: Track[] = (res.items ?? []).map(t => ({
      id: t.track_id,
      title: t.track_title,
      artist: t.artist_name || artistName,
      album: t.album_title,
      duration: t.duration,
      stream_url: `/api/music/tracks/${t.track_id}/stream`,
      album_id: t.album_id,
      artist_id: t.artist_id,
      artist_slug: t.artist_slug,
      album_slug: t.album_slug,
      poster: useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? undefined,
      source: 'artist',
    }))
    if (!tracks.length) return
    queue.value = tracks
    await play(tracks[0]!)
  } catch {
    // outer NuxtLink still navigates — that's the fallback
  }
}

async function playPlaylist(id: number, name: string) {
  try {
    const res = await $heya('/api/me/playlists/{id}', {
      path: { id },
    }) as { tracks: { id: number; title: string; duration: number; album_id: number; album_title: string; album_slug: string; artist_id: number; artist_name: string; artist_slug: string }[] }
    const tracks: Track[] = (res.tracks ?? []).map(t => ({
      id: t.id,
      title: t.title,
      artist: t.artist_name,
      album: t.album_title,
      duration: t.duration,
      stream_url: `/api/music/tracks/${t.id}/stream`,
      album_id: t.album_id,
      artist_id: t.artist_id,
      artist_slug: t.artist_slug,
      album_slug: t.album_slug,
      poster: useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? undefined,
      source: 'playlist',
    }))
    if (!tracks.length) return
    queue.value = tracks
    await play(tracks[0]!)
  } catch {
    // outer NuxtLink to /music/playlist/:id still navigates
  }
  void name
}

</script>

<style scoped>
.mh-greeting { font-size: 30px; font-weight: 700; margin-bottom: 24px; letter-spacing: -0.01em; }
.mh-empty { color: var(--fg-3); font-size: 14px; padding: 32px 0; }
.mh-section { margin-bottom: 36px; }
.mh-section-title { margin-bottom: 16px; }
.mh-lapsed-heading { margin-bottom: 12px; color: var(--fg-1); }

/* Shared link wrapper around MusicCard — strips default underlines and lets
   the card own its hover state internally. */
.mh-card-link {
  text-decoration: none;
  color: inherit;
  display: block;
}

/* More in Genre: name-only list, no art. */
.mh-genre-name { color: var(--gold); }
.mh-genre-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 4px 24px;
}
.mh-genre-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 4px;
  border-bottom: 1px solid var(--border-soft, rgba(255, 255, 255, 0.04));
  text-decoration: none;
  color: inherit;
  transition: background 0.15s, color 0.15s;
}
.mh-genre-row:hover { background: rgba(255, 196, 50, 0.04); color: var(--gold); }
.mh-genre-name-cell {
  flex: 1;
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mh-genre-row:hover .mh-genre-name-cell { color: var(--gold); }
.mh-genre-counts { font-size: 11px; color: var(--fg-3); }

.mono { font-family: var(--font-mono); }
</style>
