<template>
  <!-- Tone vars publish on the page root (not the scroll root — the music shell
       owns that), mirroring the artist page + the playbar's --pb-accent. Every
       descendant inherits --tone/--tone-rgb/--tone-ink from the ambient pool's
       sampled colour. --pad-fluid is pinned to the page gutter so the full-bleed
       ledger's cells line up with the greeting head and the rails below. -->
  <div class="music-home" :style="toneStyle">

    <!-- ── Greeting head (heya2.css .lib-head): mono eyebrow · weekday period,
         Archivo greeting, right-side library tools. ── -->
    <header class="mh-head">
      <div class="mh-head-text">
        <div class="mh-eyebrow">Music <span class="sep">&middot;</span> {{ timeContext }}</div>
        <h1 class="mh-greeting">{{ greeting }}</h1>
      </div>
      <div class="mh-tools">
        <button class="mh-pill" :disabled="shuffling" @click="shuffleLibrary">
          <Icon name="shuffle" :size="14" /> Shuffle library
        </button>
        <NuxtLink to="/music/stations" class="mh-pill">
          <Icon name="radio" :size="14" /> Start station
        </NuxtLink>
      </div>
    </header>

    <!-- ── Music ledger — user-facing facts only, sourced entirely from queries
         this page already runs (no totals endpoint, no ops telemetry). Cells
         self-omit when their shelf is empty/disabled. Sits on plain themed
         canvas (no hero seam here), so `canvas` gives it theme-aware ink. ── -->
    <LedgerStrip v-if="ledgerCells.length" :cells="ledgerCells" canvas />

    <div class="page-pad mh-body">

    <!-- 1. Mixes for You — Heya 2.0 gradient .mix-card tiles. -->
    <MusicScrollRow
      v-if="mixes.length"
      class="mh-mix-rail"
      title="Mixes for You"
      aside="rotates daily"
      title-href="/music/stations/mixes"
      :card-size="260"
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
        <MusicMixCard
          :name="mix.name"
          :track-count="mix.tracks.length"
          :artists="mixArtistsLine(mix)"
          :gradient="mixGradient(mix.name)"
          :no-play="mix.tracks.length === 0"
          @play="playMix(mix)"
        />
      </NuxtLink>
      </AppContextMenu>
    </MusicScrollRow>

    <!-- 2. Recently Added — album_type as a chip when EP/single/etc. -->
    <MusicScrollRow
      v-if="recentAlbums.length"
      title="Recently Added"
      :aside="addedThisWeek ? `${addedThisWeek} this week` : undefined"
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
          :hearted="(albumRatingValues.get(al.id) ?? 0) >= 9"
          :badge-tl="al.album_type && al.album_type !== 'album' ? al.album_type : ''"
          :missing="al.available === false"
          @play="playAlbum(al)"
        />
      </NuxtLink>
      </AppContextMenu>
    </MusicScrollRow>

    <!-- 3. Recently Played Artists — circular portraits with the name + count
         caption below (heya2.css .artist-card), the mockup's artist idiom. -->
    <MusicScrollRow
      v-if="recentArtists.length"
      title="Recently Played"
      title-href="/music/artists"
      :card-size="150"
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
          variant="circle"
          captioned
          :src="usePosterUrl({ id: a.media_item_id, public_id: a.media_item_public_id })"
          :alt="a.artist_name"
          :title="a.artist_name"
          :subtitle="`${a.album_count} albums · ${a.track_count} tracks`"
          :hearted="(artistRatingValues.get(a.artist_id) ?? 0) >= 9"
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
      title-href="/music/playlists"
      :card-size="170"
    >
      <AppContextMenu
        v-for="p in recentPlaylists"
        :key="`pl-${p.id}`"
        :items="playlistMenu.menuFor({ id: p.id, name: p.name, track_count: p.track_count, slug: p.slug })"
      >
      <NuxtLink
        :to="`/music/playlist/${p.slug || p.id}`"
        class="mh-card-link"
      >
        <MusicCard
          :src="playlistCoverSrc(p)"
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
        aside="rotates"
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

    <!-- 7. More in <Genre> — hairline-ruled name list (heya2.css .credits/.trk
         idiom), mono album·track counts. Rotates every 5 min. -->
    <section v-if="genreShelf && genreShelf.enabled && genreShelf.artists.length" class="mh-genre">
      <SectionHeader>
        <template #title>More in <span class="mh-tone">{{ genreShelf.genre }}</span></template>
        <template #subtitle>{{ genreShelf.artists.length }} artists</template>
      </SectionHeader>
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
    </section>

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
      <SectionHeader class="mh-lapsed-head" :title="lapsedShelf.since_label" />
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
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { ImageTone } from '~/composables/useImageTone'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import type { StationTrack } from '~/components/music/StationResults.vue'
import { useQuery } from '@pinia/colada'
import { musicAlbumDetailQuery, musicMixesQuery, type MusicMix as Mix, type MusicMixTrack as MixTrack } from '~/queries/music'

// Inline row shape declarations — these mirror the sqlc-generated Go types
// 1:1, but kept local since they're only used in this file and the OpenAPI
// types are wider (carry pgtype shapes etc.) than we want to bind against.
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
  /** pgtype.Timestamptz — feeds the "added this week" ledger cell. */
  added_at?: unknown
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
  slug: string
  name: string
  cover_path: string
  track_count: number
  has_cover?: boolean
  updated_at?: string
  auto_artist_slug?: string
  auto_album_slug?: string
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
const loadQuery = useQueryLoader()

// All home shelves are cached by Pinia Colada — the query cache (registered in
// plugins/Pinia Colada.client.ts) is module-scoped, so navigating to an
// album/artist page and back is a no-op against the cache: the second
// mount reads each query's cached payload synchronously, no flash.
//
// staleTime per shelf reflects the server-side Cache-Control:
//   - 30s for "fresh data" shelves (recently added, recently played etc.)
//   - 5min (300s) for the rotating shelves; autoRefetch also fires at
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

const mixesQuery = useQuery(musicMixesQuery())
const mixes = computed<Mix[]>(() => (mixesQuery.data.value ?? []).slice(0, 6))

const recentAlbumsQuery = useQuery({
  key: ['music', 'home', 'recently-added'],
  query: () => fetchItems<RecentAlbumRow>('/api/music/home/recently-added'),
  staleTime: 1000 * 30,
})
const recentAlbums = computed<RecentAlbumRow[]>(() => recentAlbumsQuery.data.value ?? [])

const recentArtistsQuery = useQuery({
  key: ['music', 'home', 'recently-played-artists'],
  query: () => fetchItems<RecentArtistRow>('/api/music/home/recently-played-artists'),
  staleTime: 1000 * 30,
})
const recentArtists = computed<RecentArtistRow[]>(() => recentArtistsQuery.data.value ?? [])

const albumRatings = useRatings('album')
const artistRatings = useRatings('artist')
const albumRatingValues = albumRatings.ratings
const artistRatingValues = artistRatings.ratings
watch(recentAlbums, items => { if (items.length) void albumRatings.primeBulk(items.map(al => al.id)) }, { immediate: true })
watch(recentArtists, items => { if (items.length) void artistRatings.primeBulk(items.map(a => a.artist_id)) }, { immediate: true })

const onThisDayQuery = useQuery({
  key: ['music', 'home', 'on-this-day'],
  query: () => fetchItems<OnThisDayRow>('/api/music/home/on-this-day'),
  staleTime: 1000 * 60 * 60 * 6,
})
const onThisDay = computed<OnThisDayRow[]>(() => onThisDayQuery.data.value ?? [])

const recentPlaylistsQuery = useQuery({
  key: ['music', 'home', 'recent-playlists'],
  query: () => fetchItems<PlaylistRow>('/api/music/home/recent-playlists'),
  staleTime: 1000 * 30,
})
const recentPlaylists = computed<PlaylistRow[]>(() => recentPlaylistsQuery.data.value ?? [])

// Rotating shelves — server rotates the seed every 5 minutes, so we
// autoRefetch at the same cadence. Each query refreshes independently,
// no shared setInterval to clean up on unmount (Pinia Colada handles it).
const moreByArtistsQuery = useQuery({
  key: ['music', 'home', 'more-by-artists'],
  query: () => fetchItems<MoreByEntry>('/api/music/home/more-by-artists'),
  staleTime: 1000 * 60 * 5,
  autoRefetch: 1000 * 60 * 5,
})
const moreByArtists = computed<MoreByEntry[]>(() => moreByArtistsQuery.data.value ?? [])

const genreShelfQuery = useQuery({
  key: ['music', 'home', 'more-in-genre'],
  query: async () => (await $heya('/api/music/home/more-in-genre')) as GenreShelf,
  staleTime: 1000 * 60 * 5,
  autoRefetch: 1000 * 60 * 5,
})
const genreShelf = computed<GenreShelf | null>(() => genreShelfQuery.data.value ?? null)

const mostPlayedShelfQuery = useQuery({
  key: ['music', 'home', 'most-played-last-month'],
  query: async () => (await $heya('/api/music/home/most-played-last-month')) as MostPlayedShelf,
  staleTime: 1000 * 60 * 5,
})
const mostPlayedShelf = computed<MostPlayedShelf | null>(() => mostPlayedShelfQuery.data.value ?? null)

const lapsedShelfQuery = useQuery({
  key: ['music', 'home', 'lapsed-artists'],
  query: async () => (await $heya('/api/music/home/lapsed-artists')) as LapsedShelf,
  staleTime: 1000 * 60 * 5,
  autoRefetch: 1000 * 60 * 5,
})
const lapsedShelf = computed<LapsedShelf | null>(() => lapsedShelfQuery.data.value ?? null)

const labelShelfQuery = useQuery({
  key: ['music', 'home', 'more-from-label'],
  query: async () => (await $heya('/api/music/home/more-from-label')) as LabelShelf,
  staleTime: 1000 * 60 * 5,
  autoRefetch: 1000 * 60 * 5,
})
const labelShelf = computed<LabelShelf | null>(() => labelShelfQuery.data.value ?? null)

// Live refresh: a track/album file match (media.added) or the discography
// refresh landing new albums (media.updated) both carry the artist's
// media_type='music' — see useLiveRefresh for why this is coalesced rather
// than invalidating on every event.
useLiveRefresh([
  { events: ['media.added', 'media.updated'], filter: byMediaType('music'), keys: [['music', 'home', 'recently-added']] },
])

// ── Greeting head ───────────────────────────────────────────────────────────
const now = new Date()
const period = computed(() => {
  const h = now.getHours()
  if (h < 12) return 'morning'
  if (h < 18) return 'afternoon'
  return 'evening'
})
const greeting = computed(() => `Good ${period.value}`)
// Mono eyebrow context, e.g. "Tuesday evening". Weekday pinned to en-US so it
// stays coherent with the hardcoded English "Good <period>" greeting (the
// system locale otherwise yields e.g. "onsdag morning").
const timeContext = computed(() =>
  `${now.toLocaleDateString('en-US', { weekday: 'long' })} ${period.value}`,
)

// ── Page tone: follow the ambient music pool's sampled colour (the shell owns
// the pool claim; we only publish the vars). Falls back to the :root accent
// alias when ambient is off (toneStyle undefined → --tone stays var(--accent)).
const bgTone = useBackgroundTone()
const toneStyle = computed(() => {
  const t: ImageTone | null = bgTone.value
  if (!t) return undefined
  const m = t.main.match(/\d+/g)
  if (!m) return undefined
  return { '--tone': t.main, '--tone-rgb': m.slice(0, 3).join(' '), '--tone-ink': t.ink }
})

// ── Music ledger (user-facing facts only) ───────────────────────────────────
// Every cell is derived from a query this page already ran. Recency reads the
// newest-first recently-added rail (this-week arrivals live in its first page,
// so the count is stable as older pages load). No library totals — that
// endpoint doesn't exist and the rule forbids inventing one.
const WEEK_MS = 7 * 24 * 60 * 60 * 1000
function toMs(v: unknown): number {
  if (!v) return NaN
  const iso = typeof v === 'string' ? v : (v as { Time?: string })?.Time
  return iso ? new Date(iso).getTime() : NaN
}
const addedThisWeek = computed(() => {
  const cut = Date.now() - WEEK_MS
  return recentAlbums.value.reduce((n, al) => {
    const t = toMs(al.added_at)
    return !isNaN(t) && t >= cut ? n + 1 : n
  }, 0)
})

const ledgerCells = computed<LedgerCell[]>(() => {
  const cells: LedgerCell[] = []

  if (addedThisWeek.value) cells.push({ k: 'Added', v: String(addedThisWeek.value), unit: 'this week' })
  if (mixes.value.length) cells.push({ k: 'Mixes', v: String(mixes.value.length), unit: 'for you' })
  if (onThisDay.value.length) {
    cells.push({ k: 'On this day', v: String(onThisDay.value.length), unit: onThisDay.value.length === 1 ? 'release' : 'releases' })
  }
  const g = genreShelf.value
  if (g?.enabled && g.genre) cells.push({ k: 'In rotation', v: g.genre.toUpperCase() })
  const mp = mostPlayedShelf.value
  if (mp?.enabled && mp.albums.length) {
    const top = mp.albums[0]!
    cells.push({ k: 'Most played', v: top.artist_name, sub: `${top.play_count}×`, tone: true })
  }
  return cells
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

// Deterministic gradient per mix name (never random at render — the mockup's
// .mix-card colour idiom). Hash → hue, two analogous stops. hsl() is computed
// (not a static literal); dark ink over it is token-clean via --shade.
function mixGradient(name: string): string {
  let h = 2166136261
  for (let i = 0; i < name.length; i++) { h ^= name.charCodeAt(i); h = Math.imul(h, 16777619) }
  const hue = (h >>> 0) % 360
  const hue2 = (hue + 42) % 360
  return `linear-gradient(135deg, hsl(${hue} 66% 60%), hsl(${hue2} 74% 73%))`
}
// Seed-artist line for a mix: distinct track artists (top 3), uppercased.
function mixArtistsLine(mix: Mix): string {
  const seen = new Set<string>()
  const names: string[] = []
  for (const t of mix.tracks) {
    const n = t.artist_name?.trim()
    if (!n || seen.has(n.toLowerCase())) continue
    seen.add(n.toLowerCase())
    names.push(n)
    if (names.length >= 3) break
  }
  if (!names.length && mix.seed_artist_name) names.push(mix.seed_artist_name)
  return names.join(' · ').toUpperCase()
}

const { play, queue, playTracks } = usePlayerBindings()
const actions = useMusicActions()
const playlistMenu = usePlaylistMenu()
const { isCoarse } = useViewport()
const { onDragStart, onDragEnd } = useMusicDragDrop()

// "Shuffle library" head tool — the existing Library Radio station (random
// tracks from across the catalog). Real endpoint; no new backend action.
const shuffling = ref(false)
async function shuffleLibrary() {
  if (shuffling.value) return
  shuffling.value = true
  try {
    const res = await $heya('/api/music/stations/library-radio', { query: { limit: 50 } }) as { tracks?: StationTrack[] }
    const tracks: Track[] = (res.tracks ?? []).map(t => ({
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
      source: 'station',
    }))
    if (tracks.length) await playTracks(tracks)
  } catch {
    // Silent — the pill just no-ops if the library has nothing to shuffle.
  } finally {
    shuffling.value = false
  }
}

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
  await playTracks(tracks)
}

async function playAlbumByArtistSlug(artistSlug: string, albumSlug: string, artistName: string, albumTitle: string, albumId: number) {
  try {
    const detail = await loadQuery(musicAlbumDetailQuery({ artistSlug, albumSlug }))
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
    await playTracks(tracks)
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
    await playTracks(tracks)
  } catch {
    // outer NuxtLink still navigates — that's the fallback
  }
}

async function playPlaylist(id: number, name: string) {
  try {
    const res = await $heya('/api/me/playlists/{id}', {
      // The detail route takes slug-or-id; the spec types the param as string.
      path: { id: String(id) },
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
    await playTracks(tracks)
  } catch {
    // outer NuxtLink to /music/playlist/:id still navigates
  }
  void name
}

</script>

<style scoped>
.music-home {
  /* Pin the ledger's fluid gutter to the page gutter so its full-bleed cells
     line up with the greeting head and the padded rails below (the shell's
     content column has no hero, so nothing wants the wider --pad-fluid inset). */
  --pad-fluid: var(--page-pad-x);
}

/* ── Greeting head (heya2.css .lib-head) ── */
.mh-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px 24px;
  flex-wrap: wrap;
  padding: 28px var(--page-pad-x) 20px;
}
.mh-head-text { min-width: 0; }
.mh-eyebrow {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
  font: 600 11.5px var(--font-mono);
  letter-spacing: 0.22em;
  text-transform: uppercase;
  color: var(--tone);
  text-shadow: 0 1px 2px var(--bg-1), 0 0 12px var(--bg-1);
}
.mh-eyebrow .sep { color: rgb(var(--ink) / 0.3); }
.mh-greeting {
  font-family: var(--font-display);
  font-size: clamp(2rem, 3.4vw, 3rem);
  font-weight: 800;
  font-variation-settings: 'wdth' 115;
  letter-spacing: -0.02em;
  line-height: 1;
  color: var(--fg-0);
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1), 0 0 24px var(--bg-1);
}

/* Tone-tinted head tools (heya2.css .lib-tools .pill.mono). */
.mh-tools { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.mh-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 9px 15px;
  border-radius: 999px;
  border: 1px solid rgb(var(--tone-rgb) / 0.3);
  background: rgb(var(--tone-rgb) / 0.08);
  color: rgb(var(--ink) / 0.9);
  font: 550 11px var(--font-mono);
  letter-spacing: 0.08em;
  text-transform: uppercase;
  cursor: pointer;
  text-decoration: none;
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  box-shadow: 0 0 16px rgb(var(--tone-rgb) / 0.12), 5px 8px 22px -10px rgb(var(--shade) / 0.7);
  transition: border-color 0.15s, background 0.15s, box-shadow 0.15s, transform 0.15s;
}
.mh-pill:hover {
  border-color: rgb(var(--tone-rgb) / 0.55);
  background: rgb(var(--tone-rgb) / 0.15);
  box-shadow: 0 0 24px rgb(var(--tone-rgb) / 0.24), 6px 10px 26px -10px rgb(var(--shade) / 0.75);
  transform: translateY(-1px);
}
.mh-pill:disabled { opacity: 0.5; cursor: default; transform: none; }

/* ── Rails body ── */
.page-pad.mh-body { padding-top: 40px; }

.mh-empty { color: var(--fg-3); font-size: 14px; padding: 32px 0; }

/* Shared link wrapper around cards — strips underlines, lets the card own its
   hover state internally. */
.mh-card-link {
  text-decoration: none;
  color: inherit;
  display: block;
}

/* Tone accent inside a SectionHeader slot (More in <Genre>). */
.mh-tone { color: var(--tone); }

/* "Haven't played in a while" heading — a touch of top room over the rails. */
.mh-lapsed-head { margin-top: 8px; }

/* Mixes rail: the gradient cards are wider (16/10) than square covers. Keep a
   readable width on phones instead of MusicScrollRow's 140px square default. */
@media (max-width: 720px) {
  .mh-mix-rail :deep(.msr-scroller) > * { width: 208px !important; }
}

/* ── More in <Genre> — hairline name list, mono counts ── */
.mh-genre { margin-bottom: 36px; }
.mh-genre-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 2px 32px;
  border-top: 1px solid var(--hair-strong);
}
.mh-genre-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  padding: 11px 4px;
  border-bottom: 1px solid var(--hair);
  text-decoration: none;
  color: inherit;
  transition: background 0.15s, color 0.15s;
}
.mh-genre-row:hover { background: rgb(var(--tone-rgb) / 0.05); }
.mh-genre-name-cell {
  flex: 1;
  font-size: 14px;
  font-weight: 550;
  color: var(--fg-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mh-genre-row:hover .mh-genre-name-cell { color: var(--tone); }
.mh-genre-counts {
  font: 500 11px var(--font-mono);
  color: var(--fg-3);
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.04em;
}

.mono { font-family: var(--font-mono); }
</style>
