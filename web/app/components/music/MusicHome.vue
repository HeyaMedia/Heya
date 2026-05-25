<template>
  <div class="music-home page-pad">
    <h1 class="mh-greeting">{{ greeting }}</h1>

    <div v-if="pending" class="mh-loading">Loading…</div>

    <!-- Recently Added Albums — mosaic at the top for at-a-glance new music -->
    <div v-else-if="data" class="mh-mosaic">
      <NuxtLink
        v-for="(al, i) in mosaicAlbums"
        :key="al.id"
        :to="`/music/artist/${al.artist_slug}/${al.slug}`"
        class="mh-mosaic-card card-tile"
      >
        <Poster :idx="i" :src="useAlbumCoverUrl(al.id)" aspect="1/1" class="mh-mosaic-art" />
        <div class="mh-mosaic-info">
          <div class="mh-mosaic-title">{{ al.title }}</div>
          <div class="mh-mosaic-sub">{{ al.artist_name }}</div>
        </div>
        <button class="mh-play-btn" @click.stop.prevent="playAlbum(al)" title="Play">
          <Icon name="play" :size="16" />
        </button>
      </NuxtLink>
    </div>

    <!-- Recently Added Artists row -->
    <MusicScrollRow
      v-if="data && data.recent_artists.length"
      title="Recently Added Artists"
      title-href="/music/artists"
      :card-size="150"
    >
      <NuxtLink
        v-for="a in data.recent_artists"
        :key="a.id"
        :to="`/music/artist/${a.slug}`"
        class="mh-artist-tile card-tile"
      >
        <Poster :idx="a.id" :src="artistPosterUrl(a)" aspect="1/1" class="mh-artist-art" />
        <div class="mh-tile-meta">
          <div class="mh-tile-title">{{ a.name }}</div>
          <div class="mh-tile-sub">{{ a.album_count }} · {{ a.track_count }}</div>
        </div>
      </NuxtLink>
    </MusicScrollRow>

    <!-- Recently Added Albums row -->
    <MusicScrollRow
      v-if="data && data.recent_albums.length"
      title="Recently Added Albums"
      title-href="/music/albums"
      :card-size="170"
    >
      <NuxtLink
        v-for="al in data.recent_albums"
        :key="al.id"
        :to="`/music/artist/${al.artist_slug}/${al.slug}`"
        class="mh-album-tile card-tile"
      >
        <Poster :idx="al.id" :src="useAlbumCoverUrl(al.id)" aspect="1/1" class="mh-album-art" />
        <div class="mh-tile-meta">
          <div class="mh-tile-title">{{ al.title }}</div>
          <div class="mh-tile-sub">{{ al.artist_name }}{{ al.year ? ' · ' + al.year : '' }}</div>
        </div>
      </NuxtLink>
    </MusicScrollRow>

    <div v-if="data && !data.recent_artists.length && !data.recent_albums.length" class="mh-empty">
      No music yet — add a music library and let the scanner run.
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { MusicArtistRow, MusicAlbumRow } from '~~/shared/types'

defineEmits<{ 'see-artists': []; 'see-albums': [] }>()

interface HomeData {
  recent_artists: MusicArtistRow[]
  recent_albums: MusicAlbumRow[]
}

const homeRes = await useHeya('/api/music/home', { query: { limit: 24 } })
// Spec-generated types are stricter than the hand-maintained MusicArtistRow /
// MusicAlbumRow (e.g. pgtype.Timestamp vs string). Cast through `unknown`
// since downstream consumers only touch the fields both shapes agree on.
const data = homeRes.data as unknown as Ref<HomeData | null>
const pending = homeRes.pending

const greeting = computed(() => {
  const h = new Date().getHours()
  if (h < 12) return 'Good morning'
  if (h < 18) return 'Good afternoon'
  return 'Good evening'
})

const mosaicAlbums = computed(() => (data.value?.recent_albums ?? []).slice(0, 6))

// Don't gate on a.poster_path — the /api/media/{id}/image/poster endpoint
// falls back through media_assets when the column is empty (common for
// freshly-scanned artists whose local-detector results landed in
// media_assets but haven't been mirrored to media_items.poster_path yet).
// Poster's imgError handler shows the gradient placeholder on 404.
const artistPosterUrl = (a: MusicArtistRow) => usePosterUrl(a.media_item_id)

const { play, queue } = usePlayer()

async function playAlbum(al: MusicAlbumRow) {
  try {
    const { $heya } = useNuxtApp()
    const detail = await $heya('/api/music/artists/{artist_slug}/albums/{album_slug}', {
      path: { artist_slug: al.artist_slug, album_slug: al.slug },
    }) as { tracks: { id: number; title: string; duration: number; files: { integrated_lufs: string | null; true_peak_db: string | null }[] }[] }
    if (!detail.tracks.length) return
    const tracks: Track[] = detail.tracks.map((t) => {
      const primary = t.files[0]
      return {
        id: t.id,
        title: t.title,
        artist: al.artist_name,
        album: al.title,
        duration: t.duration,
        stream_url: `/api/tracks/${t.id}/stream`,
        album_id: al.id,
        poster: useAlbumCoverUrl(al.id) ?? undefined,
        integrated_lufs: primary?.integrated_lufs != null ? parseFloat(primary.integrated_lufs) : null,
        true_peak_db: primary?.true_peak_db != null ? parseFloat(primary.true_peak_db) : null,
      }
    })
    queue.value = tracks
    await play(tracks[0])
  } catch {
    // swallow — clicking the title still routes
  }
}
</script>

<style scoped>
.mh-greeting { font-size: 30px; font-weight: 700; margin-bottom: 24px; letter-spacing: -0.01em; }
.mh-loading, .mh-empty { color: var(--fg-3); font-size: 14px; padding: 32px 0; }

.mh-mosaic {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px;
  margin-bottom: 36px;
}
.mh-mosaic-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px;
  background: rgba(255,255,255,0.04);
  border-radius: var(--r-md);
  cursor: pointer;
  position: relative;
  overflow: hidden;
  transition: background 0.15s;
  text-decoration: none;
  color: inherit;
}
.mh-mosaic-card:hover { background: rgba(255,255,255,0.08); }
.mh-mosaic-art { width: 56px; height: 56px; border-radius: 4px; flex-shrink: 0; }
.mh-mosaic-info { flex: 1; min-width: 0; overflow: hidden; }
.mh-mosaic-title { font-size: 13px; font-weight: 600; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mh-mosaic-sub { font-size: 11px; color: var(--fg-2); margin-top: 2px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.mh-play-btn {
  width: 36px; height: 36px;
  border-radius: 50%;
  background: var(--gold);
  color: var(--bg-0);
  display: flex; align-items: center; justify-content: center;
  opacity: 0;
  transform: translateY(4px);
  transition: opacity 0.2s, transform 0.2s;
  flex-shrink: 0;
  border: 0;
  cursor: pointer;
  box-shadow: 0 4px 14px var(--gold-glow);
}
.mh-mosaic-card:hover .mh-play-btn { opacity: 1; transform: none; }

/* Scrolling-row tiles */
.mh-artist-tile, .mh-album-tile {
  text-decoration: none;
  color: inherit;
  display: block;
}
.mh-artist-art { border-radius: 50%; box-shadow: 0 8px 18px rgba(0,0,0,0.45); }
.mh-album-art { border-radius: var(--r-md); box-shadow: 0 8px 18px rgba(0,0,0,0.45); }
.mh-tile-meta { margin-top: 12px; text-align: left; padding: 0 2px; }
.mh-artist-tile .mh-tile-meta { text-align: center; }
.mh-tile-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mh-tile-sub {
  font-size: 11px;
  color: var(--fg-2);
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
