<template>
  <div class="ms-my page-pad">
    <header class="ms-my-head">
      <div>
        <h1 class="ms-my-title">My Music</h1>
        <div class="ms-my-sub">Everything you've saved, loved, or built.</div>
      </div>
      <div class="ms-stat-row">
        <NuxtLink to="/music/my/artists" class="ms-stat">
          <div class="ms-stat-num">{{ lovedArtistsCount.toLocaleString() }}</div>
          <div class="ms-stat-lbl">Artists</div>
        </NuxtLink>
        <NuxtLink to="/music/my/albums" class="ms-stat">
          <div class="ms-stat-num">{{ lovedAlbumsCount.toLocaleString() }}</div>
          <div class="ms-stat-lbl">Albums</div>
        </NuxtLink>
        <NuxtLink to="/music/loved" class="ms-stat">
          <div class="ms-stat-num">{{ lovedTracksCount.toLocaleString() }}</div>
          <div class="ms-stat-lbl">Loved Songs</div>
        </NuxtLink>
        <NuxtLink to="/music/stats" class="ms-stat">
          <div class="ms-stat-num"><Icon name="sparkle" :size="20" /></div>
          <div class="ms-stat-lbl">My Sound</div>
        </NuxtLink>
      </div>
    </header>

    <!-- My Playlists -->
    <MusicScrollRow
      v-if="playlists.length"
      title="My Playlists"
      :card-size="170"
    >
      <NuxtLink
        v-for="pl in playlists"
        :key="`pl-${pl.id}`"
        :to="`/music/playlist/${pl.id}`"
        class="ms-card-link"
      >
        <MusicCard
          :src="pl.cover_path || null"
          :alt="pl.name"
          :title="pl.name"
          :subtitle="`${pl.track_count} ${pl.track_count === 1 ? 'track' : 'tracks'}`"
          badge-tl="Playlist"
          no-play
        />
      </NuxtLink>
    </MusicScrollRow>

    <!-- Liked Artists -->
    <MusicScrollRow
      v-if="lovedArtists.length"
      title="Liked Artists"
      title-href="/music/my/artists"
      :card-size="160"
    >
      <NuxtLink
        v-for="ar in lovedArtists"
        :key="`la-${ar.id}`"
        :to="`/music/artist/${ar.slug}`"
        class="ms-card-link"
      >
        <MusicCard
          variant="circle"
          :src="usePosterUrl(ar.media_item_id) ?? undefined"
          :alt="ar.name"
          :title="ar.name"
          no-play
        />
        <div class="ms-circle-label">{{ ar.name }}</div>
      </NuxtLink>
    </MusicScrollRow>

    <!-- Liked Albums -->
    <MusicScrollRow
      v-if="lovedAlbums.length"
      title="Liked Albums"
      title-href="/music/my/albums"
      :card-size="170"
    >
      <NuxtLink
        v-for="al in lovedAlbums"
        :key="`lal-${al.id}`"
        :to="`/music/artist/${al.artist_slug}/${al.slug}`"
        class="ms-card-link"
      >
        <MusicCard
          :src="useAlbumCoverUrl(al.artist_slug, al.slug) ?? undefined"
          :alt="al.title"
          :title="al.title"
          :subtitle="`${al.artist_name}${al.year ? ' · ' + al.year : ''}`"
          @play="playLovedAlbum(al)"
        />
      </NuxtLink>
    </MusicScrollRow>

    <!-- Loved Songs — rated 1★+, capped at 8 so it doesn't dominate. -->
    <section v-if="lovedTracks.length" class="ms-section">
      <div class="ms-section-head">
        <h2 class="section-title-lg">
          <Icon name="star" :size="18" class="ms-loved-icon" weight="fill" />
          Loved Songs
        </h2>
        <NuxtLink to="/music/loved" class="ms-see-all">See all →</NuxtLink>
      </div>
      <ul class="ms-track-list">
        <li
          v-for="(t, i) in lovedTracks"
          :key="`lt-${t.track_id}`"
          class="ms-track-row"
          @click="playLovedTracks(i)"
        >
          <div class="ms-track-art">
            <img :src="useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? ''" :alt="t.album_title" loading="lazy" />
            <div class="ms-track-play"><Icon name="play" :size="14" /></div>
          </div>
          <div class="ms-track-meta">
            <div class="ms-track-title">{{ t.track_title }}</div>
            <div class="ms-track-sub">{{ t.artist_name }} · {{ t.album_title }}</div>
          </div>
          <div class="ms-track-dur">{{ formatDuration(t.duration) }}</div>
        </li>
      </ul>
    </section>

    <!-- Empty state — nothing loved yet. -->
    <div v-if="!isLoading && !playlists.length && !lovedArtists.length && !lovedAlbums.length && !lovedTracks.length" class="ms-empty">
      <Icon name="heart" :size="40" />
      <h3>Nothing here yet</h3>
      <p>Tap the heart on artists, albums, or songs you like.<br/>They'll show up here so you can find them again.</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

const { play, queue } = usePlayer()
const { $heya } = useNuxtApp()

interface PlaylistRow {
  id: number
  name: string
  cover_path: string
  track_count: number
}
interface LovedTrackRow {
  track_id: number
  track_title: string
  duration: number
  album_id: number
  album_title: string
  album_cover_path: string
  album_year: string
  album_slug: string
  artist_id: number
  artist_name: string
  artist_slug: string
}
interface LovedArtistRow {
  id: number
  name: string
  slug: string
  media_item_id: number
}
interface LovedAlbumRow {
  id: number
  title: string
  slug: string
  year: string
  album_type: string
  cover_path: string
  artist_id: number
  artist_name: string
  artist_slug: string
}
interface ListBody<T> { items: T[]; total: number }

// All four feeds in parallel; counts come from the `total` field of each.
const playlistsQuery = useQuery({
  queryKey: ['me', 'playlists'],
  queryFn: async () => (await $heya('/api/me/playlists')) as unknown as { items: PlaylistRow[] },
  staleTime: 1000 * 30,
})
// Loved Songs is now driven by the rating system — any track rated 1★+
// counts. Keeps the "playlist of stuff you like" feel without requiring the
// user to pick between two parallel love/rate mechanisms.
interface RatedTrackRow extends LovedTrackRow { rating: number }
const lovedTracksQuery = useQuery({
  queryKey: ['me', 'ratings', 'tracks', 'shelf'],
  queryFn: async () => (await $heya('/api/me/ratings/tracks', { query: { min_rating: 1, limit: 8 } })) as unknown as ListBody<RatedTrackRow>,
  staleTime: 1000 * 30,
})
const lovedArtistsQuery = useQuery({
  queryKey: ['me', 'loved', 'artists', 'shelf'],
  queryFn: async () => (await $heya('/api/me/ratings/artists', { query: { min_rating: 1, limit: 12 } })) as unknown as ListBody<LovedArtistRow>,
  staleTime: 1000 * 30,
})
const lovedAlbumsQuery = useQuery({
  queryKey: ['me', 'loved', 'albums', 'shelf'],
  queryFn: async () => (await $heya('/api/me/ratings/albums', { query: { min_rating: 1, limit: 12 } })) as unknown as ListBody<LovedAlbumRow>,
  staleTime: 1000 * 30,
})

const playlists = computed(() => playlistsQuery.data.value?.items ?? [])
const lovedTracks = computed(() => lovedTracksQuery.data.value?.items ?? [])
const lovedArtists = computed(() => lovedArtistsQuery.data.value?.items ?? [])
const lovedAlbums = computed(() => lovedAlbumsQuery.data.value?.items ?? [])

const lovedTracksCount = computed(() => lovedTracksQuery.data.value?.total ?? 0)
const lovedArtistsCount = computed(() => lovedArtistsQuery.data.value?.total ?? 0)
const lovedAlbumsCount = computed(() => lovedAlbumsQuery.data.value?.total ?? 0)

const isLoading = computed(() =>
  playlistsQuery.isLoading.value
    || lovedTracksQuery.isLoading.value
    || lovedArtistsQuery.isLoading.value
    || lovedAlbumsQuery.isLoading.value,
)

async function playLovedAlbum(al: LovedAlbumRow) {
  try {
    const detail = await $heya('/api/music/artists/{artist_slug}/albums/{album_slug}', {
      path: { artist_slug: al.artist_slug, album_slug: al.slug },
    }) as unknown as { tracks: { id: number; title: string; duration: number; files?: unknown[] }[] }
    const playable = (detail.tracks ?? []).filter((t) => (t.files?.length ?? 0) > 0)
    if (!playable.length) return
    const built: Track[] = playable.map((t) => ({
      id: t.id,
      title: t.title,
      artist: al.artist_name,
      album: al.title,
      duration: t.duration,
      stream_url: `/api/music/tracks/${t.id}/stream`,
      album_id: al.id,
      artist_slug: al.artist_slug,
      album_slug: al.slug,
      poster: useAlbumCoverUrl(al.artist_slug, al.slug) ?? undefined,
      source: 'my-music',
    }))
    queue.value = built
    await play(built[0]!)
  } catch {
    // fall through — outer link still navigates
  }
}

async function playLovedTracks(startIdx: number) {
  const built: Track[] = lovedTracks.value.map((t) => ({
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
    source: 'loved',
  }))
  queue.value = built
  await play(built[startIdx]!)
}
</script>

<style scoped>
.ms-my { max-width: 1400px; }

.ms-my-head {
  display: flex; align-items: flex-end; justify-content: space-between; gap: 32px;
  margin-bottom: 32px;
  padding-bottom: 24px;
  border-bottom: 1px solid var(--border);
}
.ms-my-title { font-size: 32px; font-weight: 700; letter-spacing: -0.01em; }
.ms-my-sub { color: var(--fg-3); font-size: 13px; margin-top: 4px; }

.ms-stat-row { display: flex; gap: 8px; }
.ms-stat {
  min-width: 100px;
  padding: 12px 20px;
  background: rgba(255,255,255,0.03);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  text-decoration: none;
  text-align: center;
  transition: all 0.15s;
}
.ms-stat:hover {
  background: rgba(255,255,255,0.06);
  border-color: var(--gold-soft);
  transform: translateY(-2px);
}
.ms-stat-num {
  font-size: 22px;
  font-weight: 700;
  color: var(--fg-0);
  letter-spacing: -0.01em;
  display: flex; align-items: center; justify-content: center;
  min-height: 28px;
}
.ms-stat:hover .ms-stat-num { color: var(--gold); }
.ms-stat-lbl {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  margin-top: 4px;
}

.ms-card-link { text-decoration: none; color: inherit; display: block; }
.ms-circle-label {
  text-align: center;
  margin-top: 8px;
  font-size: 13px;
  font-weight: 500;
  color: var(--fg-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ms-section { margin-bottom: 36px; }
.ms-section-head {
  display: flex; align-items: baseline; justify-content: space-between;
  margin-bottom: 14px;
}
.ms-loved-icon { color: var(--gold); vertical-align: -2px; margin-right: 4px; }
.ms-see-all {
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  text-decoration: none;
  letter-spacing: 0.04em;
}
.ms-see-all:hover { color: var(--fg-0); }

.ms-track-list { display: flex; flex-direction: column; gap: 2px; }
.ms-track-row {
  display: grid;
  grid-template-columns: 44px 1fr auto;
  gap: 12px;
  align-items: center;
  padding: 6px 8px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.15s;
}
.ms-track-row:hover { background: rgba(255,255,255,0.04); }
.ms-track-art {
  position: relative;
  width: 44px; height: 44px;
  border-radius: 4px; overflow: hidden;
  background: var(--bg-3);
}
.ms-track-art img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ms-track-play {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.55);
  color: #fff;
  opacity: 0;
  transition: opacity 0.15s;
}
.ms-track-row:hover .ms-track-play { opacity: 1; }
.ms-track-meta { min-width: 0; }
.ms-track-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-track-sub {
  font-size: 12px;
  color: var(--fg-3);
  margin-top: 2px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-track-dur {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
  letter-spacing: 0.04em;
}

.ms-empty {
  text-align: center;
  padding: 80px 20px;
  color: var(--fg-3);
}
.ms-empty :deep(svg) { color: var(--fg-3); margin-bottom: 12px; }
.ms-empty h3 { font-size: 18px; color: var(--fg-1); margin-bottom: 8px; font-weight: 600; }
.ms-empty p { font-size: 13px; line-height: 1.6; max-width: 400px; margin: 0 auto; }
</style>
