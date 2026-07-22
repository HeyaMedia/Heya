<template>
  <div class="ms-lib page-pad" :style="toneStyle">
    <MusicPageHead title="Library" subtitle="Everything in your music collection." />

    <!-- Library facts — the counts endpoint returns real totals (user-facing
         facts, not ops telemetry), so the 2.0 ledger carries them at the top.
         Sits on plain themed canvas (no hero seam), so `canvas` gives it
         theme-aware ink. -->
    <LedgerStrip v-if="ledgerCells.length" class="ms-ledger" :cells="ledgerCells" canvas />

    <!-- Quick browse cards — visually loud entries into the sub-lists. -->
    <section class="ms-nav-row">
      <NuxtLink to="/music/artists" class="ms-nav-card">
        <span class="ms-nav-glyph"><Icon name="user" :size="20" /></span>
        <div class="ms-nav-card-text">
          <div class="ms-nav-card-title">Artists</div>
          <div class="ms-nav-card-sub">Browse alphabetically</div>
        </div>
        <Icon name="chevright" :size="16" class="ms-nav-card-arrow" />
      </NuxtLink>
      <NuxtLink to="/music/albums" class="ms-nav-card">
        <span class="ms-nav-glyph"><Icon name="music" :size="20" /></span>
        <div class="ms-nav-card-text">
          <div class="ms-nav-card-title">Albums</div>
          <div class="ms-nav-card-sub">Every release, every artist</div>
        </div>
        <Icon name="chevright" :size="16" class="ms-nav-card-arrow" />
      </NuxtLink>
      <NuxtLink to="/music/songs" class="ms-nav-card">
        <span class="ms-nav-glyph"><Icon name="list" :size="20" /></span>
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
      :items="recentAlbums"
      :item-key="(al, i) => `ra-${al.id}`"
      :has-more="albumsQuery.hasNextPage.value"
      :loading-more="albumsQuery.asyncStatus.value === 'loading'"
      @load-more="loadMoreAlbums"
    >
      <template #default="{ item: al, index: i }">
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
          :badge-tl="al.album_type && al.album_type !== 'album' ? al.album_type : ''"
          @play="playAlbum(al, i)"
        />
      </NuxtLink>
      </AppContextMenu>
      </template>
    </MusicScrollRow>

    <!-- Recently Added Artists -->
    <MusicScrollRow
      v-if="recentArtists.length"
      title="Recently Added Artists"
      title-href="/music/artists"
      :card-size="170"
      :items="recentArtists"
      :item-key="(ar, i) => `ar-${ar.id}`"
      :has-more="artistsQuery.hasNextPage.value"
      :loading-more="artistsQuery.asyncStatus.value === 'loading'"
      @load-more="loadMoreArtists"
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
          :src="usePosterUrl({ id: ar.media_item_id, public_id: ar.media_item_public_id }) ?? undefined"
          :alt="ar.name"
          :title="ar.name"
          :subtitle="`${ar.album_count} ${ar.album_count === 1 ? 'album' : 'albums'} · ${ar.track_count} tracks`"
          badge-tl="Artist"
          no-play
        />
      </NuxtLink>
      </AppContextMenu>
      </template>
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
import type { ImageTone } from '~/composables/useImageTone'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import { useInfiniteQuery, useQuery } from '@pinia/colada'
import { musicAlbumDetailQuery } from '~/queries/music'
import {
  recentAlbumsInfinite,
  recentArtistsInfinite,
  type RecentAlbumRow,
  type RecentArtistEntry,
} from '~/queries/rails'

definePageMeta({ layout: 'default' })

const { play, queue, playTracks } = usePlayerBindings()
const { $heya } = useNuxtApp()
// Right-click on desktop, long-press on touch — the card shelves' only
// play/queue path on coarse pointers (hover-play is hidden there).
const actions = useMusicActions()
const loadQuery = useQueryLoader()

interface MusicCounts { artists: number; albums: number; tracks: number }

// Counts — one dedicated endpoint. The old limit=1 list calls each ran the
// full list pipeline (join + sort of the whole table) server-side just to
// read `total`; the tracks one alone cost ~900ms per landing view.
const countsQuery = useQuery({
  key: ['music', 'library', 'counts'],
  query: async () => await $heya('/api/music/counts') as unknown as MusicCounts,
  staleTime: 1000 * 60 * 5,
})

const artistCount = computed(() => countsQuery.data.value?.artists ?? 0)
const albumCount = computed(() => countsQuery.data.value?.albums ?? 0)
const trackCount = computed(() => countsQuery.data.value?.tracks ?? 0)
const statsLoading = computed(() => countsQuery.isLoading.value)

// Share the same persisted, paginated shelves as Home. A library revisit
// paints the already-loaded pages synchronously and can keep walking deeper.
const albumsQuery = useInfiniteQuery(() => recentAlbumsInfinite())
const artistsQuery = useInfiniteQuery(() => recentArtistsInfinite())
await Promise.all([waitForQuery(countsQuery), waitForQuery(albumsQuery), waitForQuery(artistsQuery)])

const recentAlbums = computed<RecentAlbumRow[]>(() => (albumsQuery.data.value?.pages ?? []).flat())
const recentArtists = computed<RecentArtistEntry[]>(() => (artistsQuery.data.value?.pages ?? []).flat())
const homeLoading = computed(() => albumsQuery.isLoading.value || artistsQuery.isLoading.value)
const loadMoreAlbums = railLoadMore(albumsQuery)
const loadMoreArtists = railLoadMore(artistsQuery)

// ── Page tone: follow the ambient music pool's sampled colour (the shell owns
// the pool claim; we only publish the vars), mirroring MusicHome. Falls back to
// the :root accent alias when tone-follow is off (toneStyle undefined).
const bgTone = useBackgroundTone()
const { toneFollowEnabled } = useAppearance()
const toneStyle = computed(() => {
  if (!toneFollowEnabled.value) return undefined
  const t: ImageTone | null = bgTone.value
  if (!t) return undefined
  return toneStyleVars(t)
})

// ── Library ledger — real totals from /api/music/counts (user-facing facts).
const ledgerCells = computed<LedgerCell[]>(() => {
  if (statsLoading.value) return []
  const cells: LedgerCell[] = []
  if (artistCount.value) cells.push({ k: 'Artists', v: artistCount.value.toLocaleString() })
  if (albumCount.value) cells.push({ k: 'Albums', v: albumCount.value.toLocaleString() })
  if (trackCount.value) cells.push({ k: 'Songs', v: trackCount.value.toLocaleString(), tone: true })
  return cells
})

// --- Play actions ---
async function playAlbum(al: RecentAlbumRow, _i: number) {
  try {
    const detail = await loadQuery(musicAlbumDetailQuery({ artistSlug: al.artist_slug, albumSlug: al.slug }))
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
    await playTracks(built)
  } catch {
    // outer link still navigates to album page
  }
}
</script>

<style scoped>
/* Full-bleed ledger reaches to the page gutter (the shell content column has
   no hero, so it wants the page pad, not the wider --pad-fluid inset). */
.ms-ledger { --pad-fluid: 0px; margin-bottom: 28px; }

/* Complement-tinted, centered page head — this landing page has no header
   actions, so centering the lone title/subtitle block reads better than the
   left-aligned default other music list pages use. */
.ms-lib :deep(.mhd) { justify-content: center; text-align: center; }
.ms-lib :deep(.mhd-title) { color: rgb(var(--tone-comp-rgb, var(--ink)) / 0.95); }
.ms-lib :deep(.mhd-sub) { color: rgb(var(--tone-comp-rgb, var(--ink)) / 0.6); }

/* Center the ledger's cells to match the centered head above it. */
.ms-ledger :deep(.ledger-strip) { justify-content: center; }

/* ── Quick-browse nav cards — 2.0 hairline grammar: tone-tinted glyph well,
   mono sub, directional card shadow, tone-kiss on hover. ── */
.ms-nav-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 14px;
  margin-bottom: 44px;
}
.ms-nav-card {
  display: flex; align-items: center; gap: 14px;
  padding: 18px 20px;
  background: rgb(var(--ink) / 0.03);
  border: 1px solid var(--hair);
  border-radius: var(--r-md);
  box-shadow: var(--shadow-card);
  text-decoration: none; color: inherit;
  transition: transform 0.18s ease, box-shadow 0.28s ease, border-color 0.15s, background 0.15s;
}
.ms-nav-glyph {
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
  width: 44px; height: 44px;
  border-radius: 999px;
  color: var(--tone);
  background: rgb(var(--tone-rgb) / 0.1);
  box-shadow: inset 0 0 0 1px rgb(var(--tone-rgb) / 0.25);
}
.ms-nav-card:hover {
  transform: translateY(-3px);
  border-color: rgb(var(--tone-rgb) / 0.35);
  background: rgb(var(--tone-rgb) / 0.05);
  box-shadow: var(--shadow-card-hover), 0 0 30px rgb(var(--tone-rgb) / 0.12);
}
.ms-nav-card-text { flex: 1; min-width: 0; }
.ms-nav-card-title {
  font-size: 15px; font-weight: 650;
  color: rgb(var(--tone-comp-rgb, var(--ink)) / 0.9);
  letter-spacing: -0.01em;
}
.ms-nav-card-sub {
  font: 500 10.5px var(--font-mono);
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--fg-3);
  margin-top: 4px;
}
.ms-nav-card-arrow { color: var(--fg-3); transition: transform 0.15s, color 0.15s; }
.ms-nav-card:hover .ms-nav-card-arrow { color: var(--tone); transform: translateX(3px); }

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
  /* music.vue's phone section header already reads "Library" directly
     above this page — the sub line ("Everything in your music
     collection.") stays since it's not duplicated anywhere else. */
  :deep(.mhd-title) { display: none; }
  .ms-nav-row { grid-template-columns: 1fr; margin-bottom: 28px; }
}
</style>
