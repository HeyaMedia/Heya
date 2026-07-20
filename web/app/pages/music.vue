<template>
  <div class="music-shell">
    <div class="music-body">
      <MusicSidebar
        v-if="!isPhone && !isCompact"
        :section="currentSection"
        :playlists="sidebarPlaylists"
        @create-playlist="createOpen = true"
      />
      <main class="music-main scroll">
        <!-- Phone-only compact header: replaces the persistent MusicSidebar
             with a section title. The nav itself opens from AppTopBar's burger
             (the standardized section trigger — same as tablet), so there's no
             per-page Browse button here anymore. Desktop/tablet are unchanged
             (MusicSidebar stays). -->
        <div v-if="isPhone" class="music-phone-header">
          <!-- The title doubles as "back to music home" — same destination
               as the bottom nav's Music tab, one fewer reach. -->
          <NuxtLink to="/music" class="mph-title">{{ phoneSectionTitle }}</NuxtLink>
        </div>
        <NuxtPage />
      </main>
      <!-- QueuePanel handles its own compact-band overlay styling internally
           (fixed position, no layout squeeze) — mount gate stays `!isPhone`,
           same as desktop. -->
      <QueuePanel v-if="!isPhone" />
    </div>
    <MusicBigCover v-if="!isPhone" />
    <CreatePlaylistModal :open="createOpen" @close="createOpen = false" @created="onCreated" />

    <!-- Section nav left drawer — phone (<=720px) and the compact band
         (720.02-1200px) both open it from AppTopBar's burger
         (useSectionSidebar's shared `open` ref). MusicNavSheet holds the flat
         link list (see its own header comment for why it's not a re-skinned
         <MusicSidebar/>). The global MiniPlayer/NowPlayingSheet live in
         layouts/default.vue, but the music section nav is specific to this
         page, so it's owned here. Tapping any link (or Create Playlist) closes
         the drawer. -->
    <AppSheet v-if="isPhone || isCompact" side="left" v-model:open="sectionSidebar.open.value" title="Music">
      <MusicNavSheet
        :current-section="currentSection"
        :playlists="sidebarPlaylists"
        @navigate="sectionSidebar.close()"
        @create-playlist="createOpen = true"
      />
    </AppSheet>
  </div>
</template>

<script setup lang="ts">
definePageMeta({ layout: 'default' })

// Global transport hotkeys (space / arrows / m-s-r-q-l), active across the
// music shell. Suppressed while typing.
useGlobalHotkeys()

// Ambient background: the whole music shell rides an artist-artwork pool.
// Child pages that own a specific image (artist/album detail) push their
// art on TOP of this claim and pop back to the pool when they unmount.
useBackground().pool('music')

const route = useRoute()
const router = useRouter()

const { isPhone, isCompact } = useViewport()

const createOpen = useState('music_create_playlist_open', () => false)
// Section-nav left drawer (phone + compact band), opened by AppTopBar's
// burger — shared singleton state (module-level ref), see useSectionSidebar.ts.
const sectionSidebar = useSectionSidebar()

// Map the current route to a sidebar highlight key.
const currentSection = computed(() => {
  const segs = route.path.split('/').filter(Boolean)
  if (segs[0] !== 'music') return ''
  if (segs.length === 1) return 'home'
  const second = segs[1]
  switch (second) {
    case 'artists':
    case 'albums':
    case 'songs':
    case 'loved':
    case 'podcasts':
    case 'radio':
    case 'stats':
    case 'library':
      return second
    case 'my':
      if (!segs[2]) return 'my'
      if (segs[2] === 'artists') return 'my-artists'
      if (segs[2] === 'albums') return 'my-albums'
      if (segs[2] === 'favorites') return 'my-favorites'
      return 'my'
    case 'stations':
      if (!segs[2]) return 'stations'
      if (segs[2] === 'mixes') return 'stations-mixes'
      if (segs[2] === 'builder') return 'stations-builder'
      return 'stations'
    case 'browse':
      return segs[2] ? `browse-${segs[2]}` : 'browse'
    case 'playlist':
      return segs[2] ? `playlist-${segs[2]}` : ''
    default:
      return ''
  }
})

// Real playlists — hydrate once on first paint of the shell.
const playlistsApi = usePlaylists()
if (import.meta.client) playlistsApi.ensureLoaded()
const sidebarPlaylists = playlistsApi.sidebarRows

// Phone-only compact header title — same section keys as the sidebar
// highlight above, mapped to a human label. Playlist sections look up the
// name from sidebarPlaylists since the key only carries the id.
const SECTION_TITLES: Record<string, string> = {
  home: 'Home',
  library: 'Library',
  artists: 'Artists',
  albums: 'Albums',
  songs: 'Songs',
  loved: 'Loved Songs',
  my: 'My Music',
  'my-artists': 'My Artists',
  'my-albums': 'My Albums',
  'my-favorites': 'My Favorites',
  stats: 'My Sound',
  stations: 'Stations',
  'stations-mixes': 'Mixes',
  'stations-builder': 'Mix Builder',
  podcasts: 'Podcasts',
  radio: 'Internet Radio',
}
const phoneSectionTitle = computed(() => {
  const s = currentSection.value
  if (s in SECTION_TITLES) return SECTION_TITLES[s]
  if (s?.startsWith('browse')) return 'Browse'
  if (s?.startsWith('playlist-')) {
    // URL segment is the slug now (numeric id still resolves for old links).
    const playlistRef = s.slice('playlist-'.length)
    return sidebarPlaylists.value.find((p) => p.slug === playlistRef || String(p.id) === playlistRef)?.name ?? 'Playlist'
  }
  return 'Music'
})

function onCreated(row: { id: number; slug: string }) {
  // Jump to the new playlist so the user lands on something concrete.
  router.push(`/music/playlist/${row.slug || row.id}`)
}
</script>

<style scoped>
.music-shell {
  display: flex;
  flex-direction: column;
  height: 100%;
  /* Positioning context for the fold-out MusicBigCover (absolute, bottom-left). */
  position: relative;
}
.music-body {
  display: flex;
  flex: 1;
  min-height: 0;
}
.music-main {
  flex: 1;
  min-width: 0;
}

/* Hero-flush child (the artist detail page) opts the whole shell out of the
   `.app-main` topbar offset so its art rides up under the glass bar. That would
   also slide the MusicSidebar's first nav item under the bar, so when a flush
   child is present we re-pad the sidebar by --topbar-h (+ its usual 16px). The
   `.music-main` column stays flush, so only the hero rides up; non-flush music
   pages (home/albums/…) are untouched — they keep the `.app-main` offset and the
   sidebar's default 16px. Net: the sidebar's first item lands at the same y in
   both worlds. */
.music-shell:has(.hero-flush) :deep(.music-sidebar) {
  padding-top: calc(var(--topbar-h) + 16px);
}

/* Phone-only compact header — replaces MusicSidebar's persistent presence
   with a section title (the nav opens from AppTopBar's burger). */
.music-phone-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px 10px;
}
.mph-title {
  font-size: 20px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
