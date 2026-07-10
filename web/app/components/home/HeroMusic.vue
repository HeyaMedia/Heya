<template>
  <section class="hero-music">
    <div class="music-bg" :class="{ 'ambient-extended': ambientEnabled }">
      <!-- Playing: the analyser owns the background. Idle: newest album art,
           blurred into ambience. -->
      <VisualizerSpectrum v-if="playing" variant="bars" :active="true" class="music-viz" />
      <template v-else>
        <NuxtImg
          v-if="idleArt"
          :src="idleArt"
          :width="1280"
          :quality="85"
          class="music-bg-art"
          alt=""
          @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }"
        />
      </template>
      <div class="music-bg-gradient" :class="{ playing }" />
    </div>

    <!-- Playing: now-playing lead + big art. -->
    <div v-if="playing && currentTrack" class="music-inner playing">
      <div class="music-lead">
        <div class="music-eyebrow">Now playing</div>
        <h1 class="music-title">{{ currentTrack.title }}</h1>
        <p class="music-sub">{{ currentTrack.artist }}<span v-if="currentTrack.album"> · {{ currentTrack.album }}</span></p>
        <div class="music-actions">
          <NuxtLink to="/music" class="btn btn-primary">
            <Icon name="music" :size="16" />
            Open player
          </NuxtLink>
        </div>
      </div>
      <div class="music-now-art" v-if="currentTrack.poster">
        <NuxtImg :src="currentTrack.poster" alt="" />
      </div>
    </div>

    <!-- Idle: lead on top, horizontal shelf of the newest albums below. -->
    <div v-else class="music-inner idle">
      <div class="music-lead-row">
        <div>
          <div class="music-eyebrow">Music</div>
          <h1 class="music-title">Pick up the needle</h1>
          <p class="music-sub" v-if="artists[0]">{{ artistLine }}</p>
        </div>
        <div class="music-actions">
          <NuxtLink to="/music" class="btn btn-primary">
            <Icon name="play" :size="16" />
            Open Music
          </NuxtLink>
          <NuxtLink to="/music/library" class="btn btn-ghost">Library</NuxtLink>
        </div>
      </div>

      <div class="music-shelf">
        <NuxtLink
          v-for="(al, i) in albums.slice(0, 8)"
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
    </div>
  </section>
</template>

<script setup lang="ts">
// "Music" — now-playing front and center with the live spectrum as the hero
// background; idle, a horizontal shelf of the newest albums. The visualizer
// is the existing engine-fed VisualizerSpectrum — no extra audio nodes.
import type { MediaItem } from '~~/shared/types'

type Albumish = MediaItem & { sub?: string; poster_src?: string; artist_slug?: string; album_slug?: string }

const props = defineProps<{
  albums: MediaItem[]
  artists: (MediaItem & { sub?: string })[]
}>()

const { playing, currentTrack } = usePlayer()

const idleArt = computed(() => (props.albums[0] as Albumish | undefined)?.poster_src ?? null)

// Ambient extension: idle mode's newest-album art becomes the full-page
// layer. Playing mode has no static backdrop (the visualizer owns it), so
// the watcher clears the override then and ambient falls back to the route
// pool. Either way the local `.music-bg-art` hides and the fade softens
// while ambientEnabled is on, matching whatever full-page image is showing.
const { ambientEnabled } = useAppearance()
const ambientArt = useAmbientArt()
const currentBg = computed(() => (!playing.value ? idleArt.value : null))
watch([currentBg, ambientEnabled], ([url, on]) => {
  if (on && url) ambientArt.set(url)
  else ambientArt.clear()
}, { immediate: true })

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
    linear-gradient(to right, var(--bg-1) 0%, color-mix(in srgb, var(--bg-1) 50%, transparent) 55%, transparent 100%),
    linear-gradient(to top, var(--bg-1) 0%, color-mix(in srgb, var(--bg-1) 70%, transparent) 25%, transparent 55%);
}
.music-bg-gradient.playing {
  background:
    linear-gradient(to right, var(--bg-1) 0%, color-mix(in srgb, var(--bg-1) 35%, transparent) 60%, transparent 100%),
    linear-gradient(to top, var(--bg-1) 0%, transparent 30%);
}
/* Ambient extension: the AmbientBackdrop layer shows the idle-mode album art
   (or, while playing, falls back to the route pool since the visualizer has
   no static image to hand off) full-page, so the local art hides — its
   different crop would seam at the hero edges — and the fade softens so
   the artwork continues past the hero bottom instead of ending at solid
   canvas. */
.music-bg.ambient-extended .music-bg-art { display: none; }
.music-bg.ambient-extended .music-bg-gradient {
  background:
    linear-gradient(to right,
      color-mix(in srgb, var(--bg-1) 68%, transparent) 0%,
      color-mix(in srgb, var(--bg-1) 34%, transparent) 55%,
      transparent 100%),
    linear-gradient(to top,
      color-mix(in srgb, var(--bg-1) 24%, transparent) 0%,
      color-mix(in srgb, var(--bg-1) 48%, transparent) 25%,
      transparent 55%);
}
.music-bg.ambient-extended .music-bg-gradient.playing {
  background:
    linear-gradient(to right,
      color-mix(in srgb, var(--bg-1) 68%, transparent) 0%,
      color-mix(in srgb, var(--bg-1) 24%, transparent) 60%,
      transparent 100%),
    linear-gradient(to top,
      color-mix(in srgb, var(--bg-1) 24%, transparent) 0%,
      transparent 30%);
}
.music-inner {
  position: relative;
  z-index: 2;
  height: 100%;
}
.music-inner.playing {
  display: grid;
  grid-template-columns: minmax(300px, 1fr) auto;
  align-items: center;
  gap: 48px;
  padding: 48px 40px;
  max-width: 1240px;
}
.music-inner.idle {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 44px 40px 24px;
}
.music-lead-row {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 24px;
}
.music-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--gold);
  margin-bottom: 8px;
}
.music-title {
  font-size: 38px;
  font-weight: 600;
  letter-spacing: -0.025em;
  line-height: 1.05;
  margin: 0 0 6px;
  text-wrap: balance;
}
.music-inner.playing .music-title { font-size: 44px; margin-bottom: 10px; }
.music-sub {
  font-size: 14px;
  color: var(--fg-1);
  margin: 0;
}
.music-inner.playing .music-sub { font-size: 15px; margin-bottom: 24px; }
.music-actions { display: flex; gap: 10px; flex-shrink: 0; }
.music-shelf {
  display: flex;
  gap: 14px;
  overflow-x: auto;
  scrollbar-width: none;
  padding-top: 16px;
}
.music-shelf::-webkit-scrollbar { display: none; }
.music-cover {
  position: relative;
  width: 208px;
  flex-shrink: 0;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--bg-3);
  border: 1px solid var(--border);
  text-decoration: none;
  color: inherit;
  transition: transform 0.15s, border-color 0.15s;
}
.music-cover:hover { transform: translateY(-2px); border-color: var(--border-strong); }
.music-cover-label {
  position: absolute;
  inset: auto 0 0 0;
  padding: 24px 10px 8px;
  background: linear-gradient(to top, rgba(0,0,0,0.85), transparent); /* on artwork — stays literal */
}
.music-cover-title {
  font-size: 12.5px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.music-cover-artist {
  font-size: 11px;
  color: var(--fg-2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.music-now-art {
  width: 240px;
  aspect-ratio: 1;
  border-radius: var(--r-lg);
  overflow: hidden;
  box-shadow: 0 30px 80px rgba(0,0,0,0.7), 0 0 0 1px rgb(var(--ink) / 0.08);
}
.music-now-art img { width: 100%; height: 100%; object-fit: cover; display: block; }
@media (max-width: 900px) {
  .music-inner.idle { padding: 20px; }
  .music-inner.playing { grid-template-columns: 1fr; gap: 20px; padding: 24px 20px; align-content: center; }
  .music-title { font-size: 28px; }
  .music-lead-row { flex-direction: column; align-items: flex-start; gap: 12px; }
  .music-cover { width: 150px; }
  .music-now-art { display: none; }
}
</style>
