<template>
  <div class="music-shell">
    <div class="music-body">
      <MusicSidebar :section="section" :playlists="playlists" @nav="section = $event" />
      <div class="music-main scroll">
        <MusicHome v-if="section === 'home'" />

        <div v-else-if="section === 'songs' || section === 'loved'" class="page-pad">
          <h2 style="font-size: 24px; font-weight: 600; margin-bottom: 20px">
            {{ section === 'loved' ? 'Loved Songs' : 'All Songs' }}
          </h2>
          <div class="list-rows">
            <div class="list-row list-row-head" style="grid-template-columns: 2fr 1fr 0.5fr">
              <div>Title</div><div>Album</div><div style="text-align: right">Duration</div>
            </div>
            <div
              v-for="t in mockTracks"
              :key="t.id"
              class="list-row"
              style="grid-template-columns: 2fr 1fr 0.5fr"
              @click="playerPlay(t)"
            >
              <div class="list-title-cell">
                <VuMeter v-if="currentTrack?.id === t.id" :playing="playing" />
                <Poster v-else :idx="t.id" aspect="1/1" style="width: 40px; height: 40px; border-radius: 4px; flex-shrink: 0" />
                <div>
                  <div class="list-title" :style="currentTrack?.id === t.id ? { color: 'var(--gold)' } : {}">{{ t.title }}</div>
                  <div class="list-sub">{{ t.artist }}</div>
                </div>
              </div>
              <div style="font-size: 13px; color: var(--fg-2)">{{ t.album }}</div>
              <div style="font-size: 12px; font-family: var(--font-mono); color: var(--fg-3); text-align: right">{{ formatTime(t.duration) }}</div>
            </div>
          </div>
        </div>

        <div v-else-if="section === 'artists'" class="page-pad">
          <h2 style="font-size: 24px; font-weight: 600; margin-bottom: 20px">Artists</h2>
          <div style="display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 24px">
            <div v-for="a in mockArtists" :key="a" class="card-tile" style="text-align: center">
              <Poster :idx="mockArtists.indexOf(a)" aspect="1/1" style="border-radius: 50%" />
              <div style="margin-top: 10px; font-size: 13px; font-weight: 500">{{ a }}</div>
            </div>
          </div>
        </div>

        <div v-else-if="section === 'albums'" class="page-pad">
          <h2 style="font-size: 24px; font-weight: 600; margin-bottom: 20px">Albums</h2>
          <div class="grid-posters" style="grid-template-columns: repeat(auto-fill, minmax(160px, 1fr))">
            <div v-for="(a, i) in mockAlbums" :key="i" class="grid-tile card-tile">
              <Poster :idx="i" aspect="1/1" />
              <div class="grid-tile-meta">
                <div class="grid-tile-title">{{ a.title }}</div>
                <div class="grid-tile-sub">{{ a.artist }}</div>
              </div>
            </div>
          </div>
        </div>

        <div v-else-if="section === 'podcasts'" class="page-pad">
          <h2 style="font-size: 24px; font-weight: 600; margin-bottom: 20px">Podcasts</h2>
          <div class="grid-posters" style="grid-template-columns: repeat(auto-fill, minmax(180px, 1fr))">
            <div v-for="(p, i) in mockPodcasts" :key="i" class="grid-tile card-tile">
              <Poster :idx="i" aspect="1/1" :label="p" />
              <div class="grid-tile-meta">
                <div class="grid-tile-title">{{ p }}</div>
              </div>
            </div>
          </div>
        </div>

        <div v-else-if="section === 'radio'" class="page-pad">
          <h2 style="font-size: 24px; font-weight: 600; margin-bottom: 20px">Internet Radio</h2>
          <div class="radio-now" style="margin-bottom: 32px">
            <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 10px">
              <span class="live-dot" />
              <span style="font-size: 11px; font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.1em; color: var(--fg-2)">Now on Air</span>
            </div>
            <div style="background: var(--bg-3); border-radius: var(--r-lg); padding: 24px; display: flex; align-items: center; gap: 20px">
              <Poster :idx="0" aspect="1/1" style="width: 80px; height: 80px; border-radius: var(--r-md)" />
              <div>
                <div style="font-size: 20px; font-weight: 600">SomaFM Drone Zone</div>
                <div style="font-size: 13px; color: var(--fg-2); margin-top: 4px">Ambient textures with minimal beats</div>
              </div>
              <div style="margin-left: auto">
                <button class="btn btn-primary" style="border-radius: 999px">
                  <Icon name="play" :size="16" />
                  Listen
                </button>
              </div>
            </div>
          </div>
          <div class="grid-posters" style="grid-template-columns: repeat(auto-fill, minmax(280px, 1fr))">
            <div v-for="(s, i) in mockStations" :key="i" class="grid-tile card-tile" style="display: flex; align-items: center; gap: 14px; background: var(--bg-3); border-radius: var(--r-md); padding: 14px">
              <Poster :idx="i + 3" aspect="1/1" style="width: 52px; height: 52px; border-radius: var(--r-sm); flex-shrink: 0" />
              <div style="flex: 1">
                <div style="font-size: 14px; font-weight: 500">{{ s.name }}</div>
                <div style="font-size: 11px; color: var(--fg-2)">{{ s.genre }}</div>
              </div>
              <Chip>{{ s.country }}</Chip>
            </div>
          </div>
        </div>

        <div v-else class="page-pad">
          <h2 style="font-size: 24px; font-weight: 600; margin-bottom: 20px">{{ section }}</h2>
          <p style="color: var(--fg-2)">Content for this section</p>
        </div>
      </div>
      <QueuePanel />
    </div>
    <Playbar />
    <EQPanel :open="eqOpen" @close="eqOpen = false" />
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'

definePageMeta({ layout: 'default' })


const { playing, currentTrack, queue, formatTime, play: playerPlay } = usePlayer()

const section = ref('home')
const eqOpen = ref(false)

const playlists = [
  { id: 1, name: 'Late Night Coding', count: 42 },
  { id: 2, name: 'Morning Ambient', count: 28 },
  { id: 3, name: 'Workout Mix', count: 65 },
  { id: 4, name: 'Chill Electronic', count: 33 },
  { id: 5, name: 'Focus Flow', count: 51 },
]

const mockTracks: Track[] = [
  { id: 1, title: 'Everything In Its Right Place', artist: 'Radiohead', album: 'Kid A', duration: 251 },
  { id: 2, title: 'Around the World', artist: 'Daft Punk', album: 'Homework', duration: 427 },
  { id: 3, title: 'Hyperballad', artist: 'Björk', album: 'Post', duration: 326 },
  { id: 4, title: 'Teardrop', artist: 'Massive Attack', album: 'Mezzanine', duration: 328 },
  { id: 5, title: 'Wandering Star', artist: 'Portishead', album: 'Dummy', duration: 291 },
  { id: 6, title: 'Cherry Blossom Girl', artist: 'Air', album: 'Talkie Walkie', duration: 228 },
  { id: 7, title: 'Dawn Chorus', artist: 'Thom Yorke', album: 'ANIMA', duration: 274 },
  { id: 8, title: 'Open Eye Signal', artist: 'Jon Hopkins', album: 'Immunity', duration: 473 },
  { id: 9, title: 'Kerala', artist: 'Bonobo', album: 'Migration', duration: 249 },
  { id: 10, title: 'Baby', artist: 'Four Tet', album: 'New Energy', duration: 371 },
]

const mockArtists = ['Radiohead', 'Daft Punk', 'Björk', 'Massive Attack', 'Portishead', 'Air', 'Bonobo', 'Jon Hopkins', 'Four Tet', 'Floating Points', 'Aphex Twin', 'Boards of Canada']
const mockAlbums = [
  { title: 'Kid A', artist: 'Radiohead' },
  { title: 'Homework', artist: 'Daft Punk' },
  { title: 'Post', artist: 'Björk' },
  { title: 'Mezzanine', artist: 'Massive Attack' },
  { title: 'Dummy', artist: 'Portishead' },
  { title: 'ANIMA', artist: 'Thom Yorke' },
  { title: 'Immunity', artist: 'Jon Hopkins' },
  { title: 'Migration', artist: 'Bonobo' },
]
const mockPodcasts = ['Song Exploder', 'Dissect', 'Switched on Pop', 'All Songs Considered', 'Broken Record', 'Sound Opinions']
const mockStations = [
  { name: 'SomaFM Drone Zone', genre: 'Ambient', country: 'US' },
  { name: 'FIP', genre: 'Eclectic', country: 'FR' },
  { name: 'NTS Radio', genre: 'Experimental', country: 'UK' },
  { name: 'KEXP', genre: 'Indie', country: 'US' },
  { name: 'Radio Paradise', genre: 'Eclectic', country: 'US' },
  { name: 'Dublab', genre: 'Electronic', country: 'US' },
]

onMounted(() => {
  queue.value = mockTracks
  if (!currentTrack.value && mockTracks.length) {
    currentTrack.value = mockTracks[0]
  }
})
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
.live-dot {
  width: 8px; height: 8px;
  border-radius: 50%;
  background: #e34;
  box-shadow: 0 0 6px rgba(238, 51, 68, 0.6);
  animation: pulse-dot 2s infinite;
}
@keyframes pulse-dot {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}
</style>
