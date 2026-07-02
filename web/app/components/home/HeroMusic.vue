<template>
  <section class="hero-music">
    <div class="music-bg">
      <!-- Playing: the analyser owns the background. Idle: newest album art,
           blurred into ambience. -->
      <VisualizerSpectrum v-if="playing" variant="bars" :active="true" class="music-viz" />
      <template v-else>
        <img
          v-if="idleArt"
          :src="idleArt"
          class="music-bg-art"
          alt=""
          @error="(e) => ((e.target as HTMLImageElement).style.display = 'none')"
        >
      </template>
      <div class="music-bg-gradient" :class="{ playing }" />
    </div>

    <div class="music-inner">
      <div class="music-lead">
        <div class="music-eyebrow">{{ playing ? 'Now playing' : 'Music' }}</div>

        <template v-if="playing && currentTrack">
          <h1 class="music-title">{{ currentTrack.title }}</h1>
          <p class="music-sub">{{ currentTrack.artist }}<span v-if="currentTrack.album"> · {{ currentTrack.album }}</span></p>
          <div class="music-actions">
            <NuxtLink to="/music" class="btn btn-primary">
              <Icon name="music" :size="16" />
              Open player
            </NuxtLink>
          </div>
        </template>

        <template v-else>
          <h1 class="music-title">Pick up the needle</h1>
          <p class="music-sub" v-if="artists[0]">{{ artistLine }}</p>
          <div class="music-actions">
            <NuxtLink to="/music" class="btn btn-primary">
              <Icon name="play" :size="16" />
              Open Music
            </NuxtLink>
            <NuxtLink to="/music/library" class="btn btn-ghost">Library</NuxtLink>
          </div>
        </template>
      </div>

      <div class="music-shelf" v-if="!playing">
        <NuxtLink
          v-for="(al, i) in albums.slice(0, 4)"
          :key="al.id"
          :to="albumTo(al)"
          class="music-cover"
          :title="al.title"
        >
          <Poster :idx="i" :src="(al as Albumish).poster_src" :aspect="'1/1'" />
          <div class="music-cover-label">
            <div class="music-cover-title">{{ al.title }}</div>
            <div class="music-cover-artist">{{ (al as Albumish).sub }}</div>
          </div>
        </NuxtLink>
      </div>
      <div class="music-shelf-art" v-else-if="currentTrack?.poster">
        <img :src="currentTrack.poster" alt="">
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
// "Music" — now-playing front and center with the live spectrum as the hero
// background; idle, a quiet shelf of the newest albums. The visualizer is the
// existing engine-fed VisualizerSpectrum — no extra audio nodes.
import type { MediaItem } from '~~/shared/types'

type Albumish = MediaItem & { sub?: string; poster_src?: string; artist_slug?: string; album_slug?: string }

const props = defineProps<{
  albums: MediaItem[]
  artists: (MediaItem & { sub?: string })[]
}>()

const { playing, currentTrack } = usePlayer()

const idleArt = computed(() => (props.albums[0] as Albumish | undefined)?.poster_src ?? null)

const artistLine = computed(() => {
  const a = props.artists[0] as (MediaItem & { sub?: string }) | undefined
  if (!a) return ''
  return a.sub === 'New artist' ? `New in your library: ${a.title}` : `${a.title} — ${a.sub ?? 'recently updated'}`
})

function albumTo(al: MediaItem) {
  const a = al as Albumish
  if (a.artist_slug && a.album_slug) return `/music/artist/${a.artist_slug}/${a.album_slug}`
  return '/music/library'
}
</script>

<style scoped>
.hero-music { position: relative; height: 100%; }
.music-bg { position: absolute; inset: 0; background: var(--bg-0); }
.music-viz {
  position: absolute;
  inset: auto 0 0 0;
  height: 78%;
  opacity: 0.5;
  mask-image: linear-gradient(to top, rgba(0,0,0,1) 60%, transparent 100%);
}
.music-bg-art {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  filter: blur(42px) brightness(0.45) saturate(1.2);
  transform: scale(1.15);
}
.music-bg-gradient {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(to right, var(--bg-1) 0%, rgba(12,12,16,0.5) 55%, transparent 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 42%);
}
.music-bg-gradient.playing {
  background:
    linear-gradient(to right, var(--bg-1) 0%, rgba(12,12,16,0.35) 60%, transparent 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 30%);
}
.music-inner {
  position: relative;
  z-index: 2;
  display: grid;
  grid-template-columns: minmax(300px, 1fr) auto;
  align-items: center;
  gap: 48px;
  height: 100%;
  padding: 48px 40px;
  max-width: 1240px;
}
.music-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--gold);
  margin-bottom: 10px;
}
.music-title {
  font-size: 44px;
  font-weight: 600;
  letter-spacing: -0.025em;
  line-height: 1.05;
  margin: 0 0 10px;
  text-wrap: balance;
}
.music-sub {
  font-size: 15px;
  color: var(--fg-1);
  margin: 0 0 24px;
}
.music-actions { display: flex; gap: 10px; }
.music-shelf {
  display: grid;
  grid-template-columns: repeat(2, 150px);
  gap: 14px;
}
.music-cover {
  position: relative;
  width: 150px;
  aspect-ratio: 1;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--bg-3);
  border: 1px solid var(--border);
  text-decoration: none;
  color: inherit;
  transition: transform 0.15s, border-color 0.15s;
}
.music-cover:hover { transform: translateY(-2px); border-color: var(--border-strong); }
.music-cover img { width: 100%; height: 100%; object-fit: cover; display: block; }
.music-cover-label {
  position: absolute;
  inset: auto 0 0 0;
  padding: 20px 10px 8px;
  background: linear-gradient(to top, rgba(0,0,0,0.85), transparent);
}
.music-cover-title {
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.music-cover-artist {
  font-size: 10.5px;
  color: var(--fg-2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.music-shelf-art {
  width: 240px;
  aspect-ratio: 1;
  border-radius: var(--r-lg);
  overflow: hidden;
  box-shadow: 0 30px 80px rgba(0,0,0,0.7), 0 0 0 1px rgba(255,255,255,0.08);
}
.music-shelf-art img { width: 100%; height: 100%; object-fit: cover; display: block; }
@media (max-width: 900px) {
  .music-inner { grid-template-columns: 1fr; gap: 20px; padding: 24px 20px; align-content: center; }
  .music-title { font-size: 32px; }
  .music-shelf { grid-template-columns: repeat(2, 120px); }
  .music-cover { width: 120px; }
  .music-shelf-art { display: none; }
}
</style>
