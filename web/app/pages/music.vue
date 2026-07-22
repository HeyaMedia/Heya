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

</style>
