<template>
  <div class="ms-my page-pad" :style="toneStyle">
    <MusicPageHead title="My Music" subtitle="Everything you've saved, loved, or built.">
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
    </MusicPageHead>

    <!-- My Playlists -->
    <MusicScrollRow
      v-if="playlists.length"
      title="My Playlists"
      :card-size="170"
      :items="playlists"
      :item-key="(pl, i) => `pl-${pl.id}`"
    >
      <template #default="{ item: pl }">
      <AppContextMenu
        :items="playlistMenu.menuFor({ id: pl.id, name: pl.name, track_count: pl.track_count, slug: pl.slug })"
      >
      <NuxtLink
        :to="`/music/playlist/${pl.slug || pl.id}`"
        class="ms-card-link"
      >
        <MusicCard
          :src="playlistCoverSrc(pl)"
          :alt="pl.name"
          :title="pl.name"
          :subtitle="`${pl.track_count} ${pl.track_count === 1 ? 'track' : 'tracks'}`"
          badge-tl="Playlist"
          no-play
        />
      </NuxtLink>
      </AppContextMenu>
      </template>
    </MusicScrollRow>

    <!-- Liked Artists -->
    <MusicScrollRow
      v-if="lovedArtists.length"
      title="Liked Artists"
      title-href="/music/my/artists"
      :card-size="160"
      :items="lovedArtists"
      :item-key="(ar, i) => `la-${ar.id}`"
    >
      <template #default="{ item: ar }">
      <AppContextMenu
        :items="actions.forArtist({ id: ar.id, name: ar.name, slug: ar.slug, media_item_id: ar.media_item_id })"
      >
      <NuxtLink
        :to="`/music/artist/${ar.slug}`"
        class="ms-card-link"
      >
        <MusicCard
          variant="square"
          :src="usePosterUrl({ id: ar.media_item_id, public_id: ar.media_item_public_id }) ?? undefined"
          :alt="ar.name"
          :title="ar.name"
          no-play
        />
      </NuxtLink>
      </AppContextMenu>
      </template>
    </MusicScrollRow>

    <!-- Liked Albums -->
    <MusicScrollRow
      v-if="lovedAlbums.length"
      title="Liked Albums"
      title-href="/music/my/albums"
      :card-size="170"
      :items="lovedAlbums"
      :item-key="(al, i) => `lal-${al.id}`"
    >
      <template #default="{ item: al }">
      <AppContextMenu
        :items="actions.forAlbum({ id: al.id, title: al.title, artist_slug: al.artist_slug, album_slug: al.slug, artist_name: al.artist_name })"
      >
      <NuxtLink
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
      </AppContextMenu>
      </template>
    </MusicScrollRow>

    <!-- Loved Songs — rated 1★+, capped at 8 so it doesn't dominate. Page-owned
         head markup (not a MusicScrollRow rail), so it takes the 2.0 mono
         SectionHeader — same split MusicHome uses. -->
    <section v-if="lovedTracks.length" class="ms-section">
      <SectionHeader title="Loved Songs" :subtitle="String(lovedTracksCount)">
        <template #actions>
          <NuxtLink to="/music/loved" class="ms-see-all">See all &rarr;</NuxtLink>
        </template>
      </SectionHeader>
      <ul class="ms-track-list">
        <li
          v-for="(t, i) in lovedTracks"
          :key="`lt-${t.track_id}`"
          class="ms-track-row"
          role="button"
          tabindex="0"
          :aria-label="`Play ${t.track_title}`"
          @click="playLovedTracks(i)"
          @keydown.enter="playLovedTracks(i)"
          @keydown.space.prevent="playLovedTracks(i)"
        >
          <div class="ms-track-art">
            <LoadingImage :src="useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? ''" :alt="t.album_title" :width="160" :quality="80" densities="1x 2x" loading="lazy" />
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
    <MusicEmptyState
      v-if="!isLoading && !playlists.length && !lovedArtists.length && !lovedAlbums.length && !lovedTracks.length"
      icon="heart"
      title="Nothing here yet"
    >
      Rate the artists, albums, and songs you like — anything 1★ and up
      lands here. Start from <NuxtLink to="/music/songs">All Songs</NuxtLink>
      or any artist page.
    </MusicEmptyState>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { ImageTone } from '~/composables/useImageTone'
import type { LovedAlbumRow } from '~/queries/music'
import { useQuery } from '@pinia/colada'
import { lovedAlbumsQuery, lovedArtistsQuery, musicAlbumDetailQuery, userPlaylistsQuery } from '~/queries/music'

definePageMeta({ layout: 'default' })

// ── Page tone: follow the ambient music pool (mirrors MusicHome / library).
const bgTone = useBackgroundTone()
const { toneFollowEnabled } = useAppearance()
const toneStyle = computed(() => {
  if (!toneFollowEnabled.value) return undefined
  const t: ImageTone | null = bgTone.value
  if (!t) return undefined
  const m = t.main.match(/\d+/g)
  if (!m) return undefined
  return { '--tone': t.main, '--tone-rgb': m.slice(0, 3).join(' '), '--tone-ink': t.ink }
})

const { play, queue, playTracks } = usePlayerBindings()
const { $heya } = useNuxtApp()
// Right-click on desktop, long-press on touch — the card shelves' only
// play/queue path on coarse pointers (hover-play is hidden there).
const actions = useMusicActions()
const playlistMenu = usePlaylistMenu()
const loadQuery = useQueryLoader()

interface PlaylistRow {
  id: number
  slug: string
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
interface ListBody<T> { items: T[]; total: number }

// All four feeds in parallel; counts come from the `total` field of each.
const playlistsQuery = useQuery(userPlaylistsQuery())
// Loved Songs is now driven by the rating system — any track rated 1★+
// counts. Keeps the "playlist of stuff you like" feel without requiring the
// user to pick between two parallel love/rate mechanisms.
interface RatedTrackRow extends LovedTrackRow { rating: number }
const lovedTracksQuery = useQuery({
  key: ['me', 'ratings', 'tracks', 'shelf'],
  query: async () => (await $heya('/api/me/ratings/tracks', { query: { min_rating: 1, limit: 8 } })) as unknown as ListBody<RatedTrackRow>,
  staleTime: 1000 * 30,
})
// Shelf preview — capped at 12, same query factory (and cache key shape) the
// dedicated My Artists/My Albums pages use at limit 500.
const lovedArtistsShelfQuery = useQuery(lovedArtistsQuery(12))
const lovedAlbumsShelfQuery = useQuery(lovedAlbumsQuery(12))
await Promise.all([
  waitForQuery(playlistsQuery),
  waitForQuery(lovedTracksQuery),
  waitForQuery(lovedArtistsShelfQuery),
  waitForQuery(lovedAlbumsShelfQuery),
])

const playlists = computed(() => playlistsQuery.data.value?.items ?? [])
const lovedTracks = computed(() => lovedTracksQuery.data.value?.items ?? [])
const lovedArtists = computed(() => lovedArtistsShelfQuery.data.value?.items ?? [])
const lovedAlbums = computed(() => lovedAlbumsShelfQuery.data.value?.items ?? [])

const lovedTracksCount = computed(() => lovedTracksQuery.data.value?.total ?? 0)
const lovedArtistsCount = computed(() => lovedArtistsShelfQuery.data.value?.total ?? 0)
const lovedAlbumsCount = computed(() => lovedAlbumsShelfQuery.data.value?.total ?? 0)

const isLoading = computed(() =>
  playlistsQuery.isLoading.value
    || lovedTracksQuery.isLoading.value
    || lovedArtistsShelfQuery.isLoading.value
    || lovedAlbumsShelfQuery.isLoading.value,
)

async function playLovedAlbum(al: LovedAlbumRow) {
  try {
    const detail = await loadQuery(musicAlbumDetailQuery({ artistSlug: al.artist_slug, albumSlug: al.slug }))
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
    await playTracks(built)
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
  await playTracks(built, built[startIdx])
}
</script>

<style scoped>
.ms-my { max-width: 1400px; }

.ms-stat-row { display: flex; gap: 8px; }
/* 2.0 stat tiles — hairline card, mono-numeric value, mono label, tone-kiss
   on hover. Stay clickable (this page has no separate nav row). */
.ms-stat {
  min-width: 104px;
  padding: 12px 18px 13px;
  background: rgb(var(--ink) / 0.03);
  border: 1px solid var(--hair);
  box-shadow: var(--shadow-card);
  border-radius: var(--r-md);
  text-decoration: none;
  text-align: center;
  transition: transform 0.18s ease, box-shadow 0.28s ease, border-color 0.15s, background 0.15s;
}
.ms-stat:hover {
  transform: translateY(-3px);
  border-color: rgb(var(--tone-rgb) / 0.35);
  background: rgb(var(--tone-rgb) / 0.05);
  box-shadow: var(--shadow-card-hover), 0 0 26px rgb(var(--tone-rgb) / 0.1);
}
.ms-stat-num {
  font: 700 22px var(--font-mono);
  color: var(--fg-0);
  letter-spacing: -0.01em;
  font-variant-numeric: tabular-nums;
  display: flex; align-items: center; justify-content: center;
  min-height: 28px;
}
.ms-stat:hover .ms-stat-num { color: var(--tone); }
.ms-stat-lbl {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  margin-top: 5px;
}

.ms-card-link { text-decoration: none; color: inherit; display: block; }

.ms-section { margin-bottom: 40px; }
.ms-see-all {
  font: 550 12px var(--font-mono);
  color: var(--fg-2);
  text-decoration: none;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.ms-see-all:hover { color: var(--tone); }

/* ── Loved-Songs preview — 2.0 .trk ledger rows (art thumb + title/artist +
   duration), hairline-separated, tone active on hover. Glass panel kept for
   readability over the bright ambient pool (no hero grade to sit on here). ── */
.ms-track-list {
  display: flex; flex-direction: column;
  padding: 4px 10px;
  background: color-mix(in oklab, var(--bg-2) 76%, transparent);
  -webkit-backdrop-filter: blur(10px);
  backdrop-filter: blur(10px);
  border: 1px solid var(--hair);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-el);
}
.ms-track-row {
  display: grid;
  grid-template-columns: 44px 1fr auto;
  gap: 14px;
  align-items: center;
  padding: 8px 6px;
  border-bottom: 1px solid var(--hair);
  cursor: pointer;
  transition: background 0.15s;
}
.ms-track-row:last-child { border-bottom: 0; }
.ms-track-row:hover { background: rgb(var(--tone-rgb) / 0.06); }
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
  background: rgba(0,0,0,0.55); /* on artwork — stays literal */
  color: #fff; /* on artwork — stays literal */
  opacity: 0;
  transition: opacity 0.15s;
}
.ms-track-row:hover .ms-track-play { opacity: 1; }
.ms-track-meta { min-width: 0; }
.ms-track-title {
  font-size: 14.5px;
  font-weight: 600;
  color: rgb(var(--ink) / 0.92);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-track-sub {
  font: 500 11.5px var(--font-mono);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--fg-3);
  margin-top: 3px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-track-dur {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--fg-3);
  letter-spacing: 0.04em;
  font-variant-numeric: tabular-nums;
}

@media (max-width: 720px) {
  /* music.vue's phone section header already reads "My Music" directly
     above this page — the sub line stays, it's not duplicated elsewhere. */
  :deep(.mhd-title) { display: none; }
  .ms-stat-row { display: grid; grid-template-columns: repeat(2, 1fr); gap: 8px; }
  .ms-stat { min-width: 0; padding: 12px 8px; }

  .ms-track-row { padding: 10px 8px; }
}
</style>
