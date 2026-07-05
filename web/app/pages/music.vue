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
             with a section title + a Browse button that opens the nav sheet
             below. Desktop/tablet are unchanged (MusicSidebar stays). -->
        <div v-if="isPhone" class="music-phone-header">
          <!-- The title doubles as "back to music home" — same destination
               as the bottom nav's Music tab, one fewer reach. -->
          <NuxtLink to="/music" class="mph-title">{{ phoneSectionTitle }}</NuxtLink>
          <button type="button" class="mph-browse-btn" @click="browseOpen = true">
            <Icon name="list" :size="16" />
            <span>Browse</span>
          </button>
        </div>
        <NuxtPage />
      </main>
      <!-- QueuePanel handles its own compact-band overlay styling internally
           (fixed position, no layout squeeze) — mount gate stays `!isPhone`,
           same as desktop. -->
      <QueuePanel v-if="!isPhone" />
    </div>
    <Playbar v-if="!isPhone" />
    <MusicBigCover v-if="!isPhone" />
    <EQPanel v-if="!isPhone" :open="eqOpen" @close="eqOpen = false" />
    <CreatePlaylistModal :open="createOpen" @close="createOpen = false" @created="onCreated" />
    <VisualizerFullscreen v-if="!isPhone" />
    <HotkeyHelp v-if="!isPhone" />

    <!--
      Phone nav sheet — the global MiniPlayer/NowPlayingSheet live
      in layouts/default.vue, but the music section nav is specific to this
      page, so it's owned here. MusicNavSheet holds the actual flat link
      list (see its own header comment for why it's not a re-skinned
      <MusicSidebar/>). Tapping any link (or the Create Playlist row) closes
      the sheet.
    -->
    <AppSheet v-if="isPhone" v-model:open="browseOpen" title="Browse" size="full">
      <MusicNavSheet
        :current-section="currentSection"
        :playlists="sidebarPlaylists"
        @navigate="browseOpen = false"
        @create-playlist="createOpen = true"
      />
    </AppSheet>
    <!-- Compact band (720.02-1200px): same nav list as the phone sheet
         above, but as a left-side drawer opened by AppTopBar's burger
         (useSectionSidebar's shared `open` ref) instead of the phone
         header's "Browse" button (that button only renders `v-if="isPhone"`). -->
    <AppSheet v-if="isCompact" side="left" v-model:open="sectionSidebar.open.value" title="Music">
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

const route = useRoute()
const router = useRouter()

const { isPhone, isCompact } = useViewport()

const eqOpen = useState('music_eq_open', () => false)
const createOpen = useState('music_create_playlist_open', () => false)
const browseOpen = ref(false)
// Compact-band (720.02-1200px) left drawer, opened by AppTopBar's burger —
// shared singleton state (module-level ref), see useSectionSidebar.ts.
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
    case 'search':
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
  search: 'Search',
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
    const id = Number(s.slice('playlist-'.length))
    return sidebarPlaylists.value.find((p) => p.id === id)?.name ?? 'Playlist'
  }
  return 'Music'
})

function onCreated(id: number) {
  // Jump to the new playlist so the user lands on something concrete.
  router.push(`/music/playlist/${id}`)
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

/* Phone-only compact header — replaces MusicSidebar's persistent presence
   with a section title + a button that opens the nav sheet. */
.music-phone-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
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
.mph-browse-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 36px;
  padding: 0 14px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.06);
  border: 1px solid var(--border);
  color: var(--fg-1);
  font-size: 13px;
  font-weight: 500;
  flex-shrink: 0;
}
.mph-browse-btn:active { background: rgba(255, 255, 255, 0.12); color: var(--fg-0); }
</style>
