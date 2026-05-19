<template>
  <div class="music-home page-pad">
    <h1 class="mh-greeting">{{ greeting }}</h1>

    <div class="mh-mosaic">
      <div
        v-for="(item, i) in featured"
        :key="i"
        class="mh-mosaic-card card-tile"
        @click="play(item)"
      >
        <Poster :idx="i" aspect="1/1" style="width: 56px; height: 56px; border-radius: 6px; flex-shrink: 0" />
        <div class="mh-mosaic-info">
          <div style="font-size: 13px; font-weight: 500">{{ item.title }}</div>
          <div style="font-size: 11px; color: var(--fg-2)">{{ item.artist }}</div>
        </div>
        <button class="mh-play-btn" @click.stop="play(item)">
          <Icon name="play" :size="16" />
        </button>
      </div>
    </div>

    <div v-for="(row, i) in rows" :key="i" style="margin-top: 36px">
      <div class="section-row-head">
        <h2 class="section-title-lg">{{ row.title }}</h2>
        <span class="more">See all</span>
      </div>
      <div class="mh-row-grid">
        <div
          v-for="(item, j) in row.items"
          :key="j"
          class="card-tile"
          @click="play(item)"
        >
          <Poster :idx="j + i * 6" aspect="1/1" style="border-radius: var(--r-md)" />
          <div style="margin-top: 10px">
            <div style="font-size: 13px; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ item.title }}</div>
            <div style="font-size: 11px; color: var(--fg-2)">{{ item.artist }}</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'

const { play } = usePlayer()

const greeting = computed(() => {
  const h = new Date().getHours()
  if (h < 12) return 'Good morning'
  if (h < 18) return 'Good afternoon'
  return 'Good evening'
})

const makeTracks = (names: string[]): Track[] =>
  names.map((n, i) => ({
    id: 100 + i,
    title: n.split(' - ')[1] || n,
    artist: n.split(' - ')[0] || 'Unknown',
    album: 'Album',
    duration: 180 + Math.floor(Math.random() * 120),
  }))

const featured = makeTracks([
  'Radiohead - Everything In Its Right Place',
  'Daft Punk - Around the World',
  'Björk - Hyperballad',
  'Massive Attack - Teardrop',
  'Portishead - Wandering Star',
  'Air - Cherry Blossom Girl',
])

const rows = [
  { title: 'Recently Played', items: makeTracks(['Thom Yorke - Dawn Chorus', 'Jon Hopkins - Open Eye Signal', 'Bonobo - Kerala', 'Four Tet - Baby', 'Floating Points - Silhouettes', 'Burial - Archangel']) },
  { title: 'Made For You', items: makeTracks(['Aphex Twin - Xtal', 'Boards of Canada - Roygbiv', 'Autechre - Gantz Graf', 'Amon Tobin - Four Ton Mantis', 'Squarepusher - Tommib', 'Clark - Ted']) },
]
</script>

<style scoped>
.mh-greeting { font-size: 30px; font-weight: 600; margin-bottom: 24px; }
.mh-mosaic {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px;
}
.mh-mosaic-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px;
  background: var(--bg-3);
  border-radius: var(--r-md);
  cursor: pointer;
  position: relative;
  overflow: hidden;
  transition: background 0.15s;
}
.mh-mosaic-card:hover { background: var(--bg-4); }
.mh-mosaic-info { flex: 1; min-width: 0; }
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
}
.mh-mosaic-card:hover .mh-play-btn { opacity: 1; transform: none; }
.mh-row-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  gap: 20px;
}
</style>
