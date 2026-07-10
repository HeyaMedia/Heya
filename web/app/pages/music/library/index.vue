<template>
  <div class="ms-lib page-pad">
    <header class="ms-lib-head">
      <div>
        <h1 class="ms-lib-title">Library</h1>
        <div class="ms-lib-sub">Everything in your music collection.</div>
      </div>
      <div class="ms-stat-row">
        <NuxtLink to="/music/artists" class="ms-stat">
          <div class="ms-stat-num">{{ statsLoading ? '—' : artistCount.toLocaleString() }}</div>
          <div class="ms-stat-lbl">Artists</div>
        </NuxtLink>
        <NuxtLink to="/music/albums" class="ms-stat">
          <div class="ms-stat-num">{{ statsLoading ? '—' : albumCount.toLocaleString() }}</div>
          <div class="ms-stat-lbl">Albums</div>
        </NuxtLink>
        <NuxtLink to="/music/songs" class="ms-stat">
          <div class="ms-stat-num">{{ statsLoading ? '—' : trackCount.toLocaleString() }}</div>
          <div class="ms-stat-lbl">Songs</div>
        </NuxtLink>
      </div>
    </header>

    <!-- Quick browse cards — visually loud entries into the sub-lists. -->
    <section class="ms-nav-row">
      <NuxtLink to="/music/artists" class="ms-nav-card">
        <Icon name="user" :size="22" />
        <div class="ms-nav-card-text">
          <div class="ms-nav-card-title">Artists</div>
          <div class="ms-nav-card-sub">Browse alphabetically</div>
        </div>
        <Icon name="chevright" :size="16" class="ms-nav-card-arrow" />
      </NuxtLink>
      <NuxtLink to="/music/albums" class="ms-nav-card">
        <Icon name="music" :size="22" />
        <div class="ms-nav-card-text">
          <div class="ms-nav-card-title">Albums</div>
          <div class="ms-nav-card-sub">Every release, every artist</div>
        </div>
        <Icon name="chevright" :size="16" class="ms-nav-card-arrow" />
      </NuxtLink>
      <NuxtLink to="/music/songs" class="ms-nav-card">
        <Icon name="list" :size="22" />
        <div class="ms-nav-card-text">
          <div class="ms-nav-card-title">Songs</div>
          <div class="ms-nav-card-sub">Every track in your library</div>
        </div>
        <Icon name="chevright" :size="16" class="ms-nav-card-arrow" />
      </NuxtLink>
    </section>

    <!-- Recently Added Albums -->
    <MusicScrollRow
      v-if="recentAlbums.length"
      title="Recently Added Albums"
      title-href="/music/albums"
      :card-size="170"
    >
      <AppContextMenu
        v-for="(al, i) in recentAlbums"
        :key="`ra-${al.id}`"
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
          :badge-tl="al.album_type && al.album_type !== 'album' ? al.album_type : ''"
          @play="playAlbum(al, i)"
        />
      </NuxtLink>
      </AppContextMenu>
    </MusicScrollRow>

    <!-- Recently Added Artists -->
    <MusicScrollRow
      v-if="recentArtists.length"
      title="Recently Added Artists"
      title-href="/music/artists"
      :card-size="170"
    >
      <AppContextMenu
        v-for="ar in recentArtists"
        :key="`ar-${ar.id}`"
        :items="actions.forArtist({ id: ar.id, name: ar.name, slug: ar.slug, media_item_id: ar.media_item_id })"
      >
      <NuxtLink
        :to="`/music/artist/${ar.slug}`"
        class="ms-card-link"
      >
        <MusicCard
          :src="usePosterUrl({ id: ar.media_item_id, public_id: ar.media_item_public_id }) ?? undefined"
          :alt="ar.name"
          :title="ar.name"
          :subtitle="`${ar.album_count} ${ar.album_count === 1 ? 'album' : 'albums'} · ${ar.track_count} tracks`"
          badge-tl="Artist"
          no-play
        />
      </NuxtLink>
      </AppContextMenu>
    </MusicScrollRow>

    <div v-if="homeLoading && !recentAlbums.length" class="ms-loading">Loading library overview…</div>

    <div v-if="!homeLoading && !recentAlbums.length && !recentArtists.length" class="ms-empty">
      <Icon name="music" :size="40" />
      <h3>Your library is empty</h3>
      <p>Add a music library from <NuxtLink to="/settings/libraries">Settings → Libraries</NuxtLink> to populate it.</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

const { play, queue } = usePlayer()
const { $heya } = useNuxtApp()
// Right-click on desktop, long-press on touch — the card shelves' only
// play/queue path on coarse pointers (hover-play is hidden there).
const actions = useMusicActions()

interface RecentAlbumRow {
  id: number
  title: string
  slug: string
  year: string
  album_type: string
  cover_path: string
  artist_name: string
  artist_slug: string
  artist_id: number
}
interface RecentArtistRow {
  id: number
  name: string
  slug: string
  media_item_id: number
  media_item_public_id?: string
  album_count: number
  track_count: number
}
interface MusicHomeBody {
  recent_albums: RecentAlbumRow[]
  recent_artists: RecentArtistRow[]
}
interface MusicCounts { artists: number; albums: number; tracks: number }

// Counts — one dedicated endpoint. The old limit=1 list calls each ran the
// full list pipeline (join + sort of the whole table) server-side just to
// read `total`; the tracks one alone cost ~900ms per landing view.
const countsQuery = useQuery({
  queryKey: ['music', 'library', 'counts'],
  queryFn: async () => await $heya('/api/music/counts') as unknown as MusicCounts,
  staleTime: 1000 * 60 * 5,
})

const artistCount = computed(() => countsQuery.data.value?.artists ?? 0)
const albumCount = computed(() => countsQuery.data.value?.albums ?? 0)
const trackCount = computed(() => countsQuery.data.value?.tracks ?? 0)
const statsLoading = computed(() => countsQuery.isLoading.value)

// Recent shelves come from the existing music-home aggregator (single call).
const homeQuery = useQuery({
  queryKey: ['music', 'library', 'home'],
  queryFn: async () => {
    const r = await $heya('/api/music/home', { query: { limit: 18 } }) as unknown as MusicHomeBody
    return r
  },
  staleTime: 1000 * 60,
})

const recentAlbums = computed<RecentAlbumRow[]>(() => homeQuery.data.value?.recent_albums ?? [])
const recentArtists = computed<RecentArtistRow[]>(() => homeQuery.data.value?.recent_artists ?? [])
const homeLoading = computed(() => homeQuery.isLoading.value)

// --- Play actions ---
async function playAlbum(al: RecentAlbumRow, _i: number) {
  try {
    const detail = await $heya('/api/music/artists/{artist_slug}/albums/{album_slug}', {
      path: { artist_slug: al.artist_slug, album_slug: al.slug },
    }) as unknown as { tracks: { id: number; title: string; duration: number; files?: unknown[] }[] }
    // Only queue tracks that still have a file on disk.
    const list = (detail.tracks ?? []).filter((t) => (t.files?.length ?? 0) > 0)
    if (!list.length) return
    const built: Track[] = list.map((t) => ({
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
      source: 'library',
    }))
    queue.value = built
    await play(built[0]!)
  } catch {
    // outer link still navigates to album page
  }
}
</script>

<style scoped>
.ms-lib { max-width: 1400px; }

.ms-lib-head {
  display: flex; align-items: flex-end; justify-content: space-between; gap: 32px;
  margin-bottom: 32px;
  padding-bottom: 24px;
  border-bottom: 1px solid var(--border);
}
.ms-lib-title { font-size: 32px; font-weight: 700; letter-spacing: -0.01em; }
.ms-lib-sub { color: var(--fg-3); font-size: 13px; margin-top: 4px; }

.ms-stat-row { display: flex; gap: 8px; }
.ms-stat {
  min-width: 100px;
  padding: 12px 20px;
  background: rgb(var(--ink) / 0.03);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  text-decoration: none;
  text-align: center;
  transition: all 0.15s;
}
.ms-stat:hover {
  background: rgb(var(--ink) / 0.06);
  border-color: var(--gold-soft);
  transform: translateY(-2px);
}
.ms-stat-num {
  font-size: 22px;
  font-weight: 700;
  color: var(--fg-0);
  letter-spacing: -0.01em;
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

.ms-nav-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 12px;
  margin-bottom: 40px;
}
.ms-nav-card {
  display: flex; align-items: center; gap: 14px;
  padding: 18px 20px;
  background: linear-gradient(135deg, rgb(var(--ink) / 0.03), rgb(var(--ink) / 0.01));
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  text-decoration: none; color: inherit;
  transition: all 0.15s;
}
.ms-nav-card :deep(svg) { color: var(--gold); flex-shrink: 0; }
.ms-nav-card:hover {
  background: linear-gradient(135deg, color-mix(in srgb, var(--gold) 8%, transparent), color-mix(in srgb, var(--gold) 2%, transparent));
  border-color: var(--gold-soft);
  transform: translateY(-1px);
}
.ms-nav-card-text { flex: 1; min-width: 0; }
.ms-nav-card-title {
  font-size: 16px; font-weight: 700;
  color: var(--fg-0);
}
.ms-nav-card-sub {
  font-size: 12px;
  color: var(--fg-3);
  margin-top: 2px;
}
.ms-nav-card-arrow { color: var(--fg-3); transition: transform 0.15s; }
.ms-nav-card:hover .ms-nav-card-arrow { color: var(--gold); transform: translateX(2px); }

.ms-card-link { text-decoration: none; color: inherit; display: block; }

.ms-loading {
  color: var(--fg-3); font-size: 13px; padding: 40px 0; text-align: center;
}
.ms-empty {
  text-align: center;
  padding: 80px 20px;
  color: var(--fg-3);
}
.ms-empty :deep(svg) { color: var(--fg-3); margin-bottom: 12px; }
.ms-empty h3 { font-size: 16px; color: var(--fg-1); margin-bottom: 8px; font-weight: 600; }
.ms-empty a { color: var(--gold); text-decoration: none; }
.ms-empty a:hover { text-decoration: underline; }

@media (max-width: 720px) {
  .ms-lib-head { flex-direction: column; align-items: stretch; gap: 16px; margin-bottom: 24px; padding-bottom: 20px; }
  /* music.vue's phone section header already reads "Library" directly
     above this page — the sub line ("Everything in your music
     collection.") stays since it's not duplicated anywhere else. */
  .ms-lib-title { display: none; }
  .ms-stat-row { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; }
  .ms-stat { min-width: 0; padding: 12px 8px; }

  .ms-nav-row { grid-template-columns: 1fr; margin-bottom: 28px; }
}
</style>
