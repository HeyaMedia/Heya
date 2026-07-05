<template>
  <div class="music-shell">
    <div class="music-body">
      <MusicSidebar
        v-if="!isPhone"
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
      page, so it's owned here. Built as a flat list of MusicSidebar's own
      links rather than reusing <MusicSidebar/> verbatim: that component is a
      fixed 256px `<aside>` with its own collapsible groups and a
      `coverShown` state tied to the now-playing fold-out cover — overriding
      all of that from an unscoped stylesheet (required since AppSheet
      content is portaled) fought the component's own scoped CSS harder than
      just re-listing its ~20 links flatly here. Tapping any link (or the
      Create Playlist row) closes the sheet.
    -->
    <AppSheet v-if="isPhone" v-model:open="browseOpen" title="Browse" size="full">
      <nav class="mnav">
        <NuxtLink to="/music" class="mnav-item" :class="{ active: currentSection === 'home' }" @click="browseOpen = false">
          <Icon name="home" :size="18" /> <span>Home</span>
        </NuxtLink>
        <NuxtLink to="/music/search" class="mnav-item" :class="{ active: currentSection === 'search' }" @click="browseOpen = false">
          <Icon name="search" :size="18" /> <span>Search</span>
        </NuxtLink>

        <div class="mnav-group-label">Library</div>
        <NuxtLink to="/music/library" class="mnav-item" :class="{ active: currentSection === 'library' }" @click="browseOpen = false">
          <Icon name="music" :size="18" /> <span>Overview</span>
        </NuxtLink>
        <NuxtLink to="/music/artists" class="mnav-item mnav-sub" :class="{ active: currentSection === 'artists' }" @click="browseOpen = false">Artists</NuxtLink>
        <NuxtLink to="/music/albums" class="mnav-item mnav-sub" :class="{ active: currentSection === 'albums' }" @click="browseOpen = false">Albums</NuxtLink>
        <NuxtLink to="/music/songs" class="mnav-item mnav-sub" :class="{ active: currentSection === 'songs' }" @click="browseOpen = false">Songs</NuxtLink>

        <div class="mnav-group-label">My Music</div>
        <NuxtLink to="/music/my" class="mnav-item" :class="{ active: currentSection === 'my' }" @click="browseOpen = false">
          <Icon name="user" :size="18" /> <span>Overview</span>
        </NuxtLink>
        <NuxtLink to="/music/my/artists" class="mnav-item mnav-sub" :class="{ active: currentSection === 'my-artists' }" @click="browseOpen = false">Artists</NuxtLink>
        <NuxtLink to="/music/my/albums" class="mnav-item mnav-sub" :class="{ active: currentSection === 'my-albums' }" @click="browseOpen = false">Albums</NuxtLink>
        <NuxtLink to="/music/my/favorites" class="mnav-item mnav-sub" :class="{ active: currentSection === 'my-favorites' }" @click="browseOpen = false">My Favorites</NuxtLink>
        <NuxtLink to="/music/stats" class="mnav-item mnav-sub" :class="{ active: currentSection === 'stats' }" @click="browseOpen = false">My Sound</NuxtLink>

        <div class="mnav-group-label">Stations</div>
        <NuxtLink to="/music/stations" class="mnav-item" :class="{ active: currentSection === 'stations' }" @click="browseOpen = false">
          <Icon name="compass" :size="18" /> <span>Overview</span>
        </NuxtLink>
        <NuxtLink to="/music/stations/mixes" class="mnav-item mnav-sub" :class="{ active: currentSection === 'stations-mixes' }" @click="browseOpen = false">Mixes</NuxtLink>
        <NuxtLink to="/music/stations/builder" class="mnav-item mnav-sub" :class="{ active: currentSection === 'stations-builder' }" @click="browseOpen = false">Mix Builder</NuxtLink>
        <NuxtLink to="/music/browse" class="mnav-item mnav-sub" :class="{ active: currentSection?.startsWith('browse') }" @click="browseOpen = false">Moods · Genres · Tempo</NuxtLink>

        <NuxtLink to="/music/podcasts" class="mnav-item" :class="{ active: currentSection === 'podcasts' }" @click="browseOpen = false">
          <Icon name="mic" :size="18" /> <span>Podcasts</span>
        </NuxtLink>
        <NuxtLink to="/music/radio" class="mnav-item" :class="{ active: currentSection === 'radio' }" @click="browseOpen = false">
          <Icon name="radio" :size="18" /> <span>Internet Radio</span>
        </NuxtLink>

        <div class="mnav-group-label">Playlists</div>
        <NuxtLink to="/music/loved" class="mnav-item" :class="{ active: currentSection === 'loved' }" @click="browseOpen = false">
          <Icon name="star" :size="18" /> <span>Loved Songs</span>
        </NuxtLink>
        <NuxtLink
          v-for="pl in sidebarPlaylists"
          :key="pl.id"
          :to="`/music/playlist/${pl.id}`"
          class="mnav-item mnav-sub"
          :class="{ active: currentSection === 'playlist-' + pl.id }"
          @click="browseOpen = false"
        >{{ pl.name }}</NuxtLink>
        <button type="button" class="mnav-item mnav-create" @click="browseOpen = false; createOpen = true">
          <Icon name="plus" :size="18" /> <span>Create Playlist</span>
        </button>
      </nav>
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

const { isPhone } = useViewport()

const eqOpen = useState('music_eq_open', () => false)
const createOpen = useState('music_create_playlist_open', () => false)
const browseOpen = ref(false)

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

<!--
  The browse AppSheet's content is portaled to <body> (docs/ui.md gotcha #2
  — same reason NowPlayingSheet/QueuePane keep their body styles unscoped),
  so `.mnav-*` below lives in its own unscoped block rather than the scoped
  one above.
-->
<style>
.mnav {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.mnav-group-label {
  padding: 16px 10px 4px;
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
}
.mnav-item {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  min-height: 44px;
  padding: 0 10px;
  border-radius: var(--r-sm);
  background: transparent;
  border: 0;
  color: var(--fg-1);
  font-size: 15px;
  font-weight: 500;
  text-align: left;
  text-decoration: none;
  cursor: pointer;
}
.mnav-item:active { background: rgba(255, 255, 255, 0.06); }
.mnav-item.active { color: var(--gold); background: var(--gold-soft); }
.mnav-sub {
  margin-left: 28px;
  width: calc(100% - 28px);
  min-height: 40px;
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-2);
}
.mnav-sub.active { color: var(--gold); }
.mnav-create { margin-top: 10px; color: var(--fg-2); }
</style>
