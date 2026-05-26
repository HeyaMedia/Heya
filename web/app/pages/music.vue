<template>
  <div class="music-shell">
    <div class="music-body">
      <MusicSidebar
        :section="currentSection"
        :playlists="sidebarPlaylists"
        @create-playlist="createOpen = true"
      />
      <main class="music-main scroll">
        <NuxtPage />
      </main>
      <QueuePanel />
    </div>
    <Playbar />
    <EQPanel :open="eqOpen" @close="eqOpen = false" />
    <CreatePlaylistModal :open="createOpen" @close="createOpen = false" @created="onCreated" />
  </div>
</template>

<script setup lang="ts">
definePageMeta({ layout: 'default' })

const route = useRoute()
const router = useRouter()

const eqOpen = useState('music_eq_open', () => false)
const createOpen = useState('music_create_playlist_open', () => false)

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
</style>
