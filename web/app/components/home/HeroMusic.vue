<template>
  <section class="hero-music">
    <div class="music-bg" :class="{ 'ambient-extended': ambientEnabled }">
      <!-- Real artist art: rotating backdrops of the recent artists (idle) or
           the playing artist's backdrop. When no artist has one, the newest
           album cover blurs into ambience instead. -->
      <Transition name="mbg">
        <NuxtImg
          v-if="activeBg"
          :key="activeBg"
          :src="activeBg"
          :width="1920"
          :quality="80"
          class="music-bg-img"
          alt=""
        />
        <NuxtImg
          v-else-if="fallbackArt"
          :key="`blur:${fallbackArt}`"
          :src="fallbackArt"
          :width="1280"
          :quality="85"
          class="music-bg-blur"
          alt=""
        />
      </Transition>
      <div class="music-bg-gradient" />
    </div>

    <!-- Playing: now-playing lead + big art over the playing artist's backdrop. -->
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
          <NuxtLink to="/music/library" class="btn btn-ghost">Library</NuxtLink>
        </div>
      </div>
      <div class="music-now-art" v-if="currentTrack.poster">
        <NuxtImg :src="currentTrack.poster" alt="" />
      </div>
    </div>

    <!-- Idle: lead on top, horizontal shelf of the newest albums below. The
         lead line names whichever artist's backdrop is currently showing. -->
    <div v-else class="music-inner idle">
      <div class="music-lead-row">
        <div class="music-lead-block">
          <div class="music-eyebrow">Music</div>
          <h1 class="music-title">Pick up the needle</h1>
          <p class="music-sub" v-if="shownArtist">
            <NuxtLink :to="mediaUrl(shownArtist)" class="music-artist-link">{{ shownArtist.title }}</NuxtLink>
            <span class="music-sub-note"> — {{ shownArtistSub }}</span>
          </p>
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
// "Music" — big album/artist art, no gimmicks. The background is the real
// backdrop of a relevant artist: idle it rotates through the recent artists
// that have one (the lead line follows along), playing it pins to the
// playing artist. Artists without backdrops are skipped via a tiny probe
// (image URLs are unconditional; the endpoint 404s when the asset is
// missing), and when nobody has one the newest album cover blurs in instead.
import type { MediaItem } from '~~/shared/types'

type Albumish = MediaItem & { sub?: string; poster_src?: string; artist_slug?: string; album_slug?: string }
type Artistish = MediaItem & { sub?: string }

const props = defineProps<{
  albums: MediaItem[]
  artists: Artistish[]
}>()

const { playing, currentTrack } = usePlayer()

// ---- Idle backdrop pool ----------------------------------------------------
// Probe each recent artist's backdrop URL at thumbnail size (?w=64) and keep
// the survivors. Probing beats @error juggling: the pool only ever holds
// URLs known to resolve, so rotation and the artist-name lead stay in sync.
const pool = ref<{ url: string; artist: Artistish }[]>([])
const poolIdx = ref(0)

function probe(url: string) {
  return new Promise<boolean>((resolve) => {
    const img = new Image()
    img.onload = () => resolve(true)
    img.onerror = () => resolve(false)
    img.src = `${url}?w=64`
  })
}

let probeToken = 0
watch(() => props.artists, async (list) => {
  if (import.meta.server) return
  const token = ++probeToken
  const found: { url: string; artist: Artistish }[] = []
  for (const a of list.slice(0, 8)) {
    const url = useBackdropUrl(a)
    if (url && await probe(url)) found.push({ url, artist: a })
    if (token !== probeToken) return // newer artist list superseded this pass
  }
  pool.value = found
  poolIdx.value = 0
}, { immediate: true })

const ROTATE_MS = 20_000
let rotateTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  rotateTimer = setInterval(() => {
    if (!playing.value && pool.value.length > 1) poolIdx.value = (poolIdx.value + 1) % pool.value.length
  }, ROTATE_MS)
})
onUnmounted(() => {
  if (rotateTimer) clearInterval(rotateTimer)
})

const shownEntry = computed(() => pool.value.length ? pool.value[poolIdx.value % pool.value.length] : null)
const shownArtist = computed<Artistish | undefined>(() => shownEntry.value?.artist ?? props.artists[0])
const shownArtistSub = computed(() => {
  const s = shownArtist.value?.sub
  return s === 'New artist' ? 'New in your library' : (s ?? 'Recently updated')
})

// ---- Playing backdrop -------------------------------------------------------
// The image endpoint resolves slugs, so the track's artist_slug addresses the
// playing artist's backdrop directly — no detail fetch needed.
const playingBg = ref<string | null>(null)
watch(() => (playing.value ? currentTrack.value?.artist_slug : null), async (slug) => {
  if (!slug) { playingBg.value = null; return }
  const url = `/api/media/${slug}/image/backdrop`
  const ok = await probe(url)
  // Only land the result if this is still the playing artist.
  if (slug === (playing.value ? currentTrack.value?.artist_slug : null)) playingBg.value = ok ? url : null
}, { immediate: true })

const activeBg = computed(() => (playing.value && currentTrack.value ? playingBg.value : shownEntry.value?.url ?? null))

// Blur fallback when no backdrop resolves: the playing track's cover, else
// the newest album cover.
const idleArt = computed(() => (props.albums[0] as Albumish | undefined)?.poster_src ?? null)
const fallbackArt = computed(() => (playing.value && currentTrack.value ? currentTrack.value.poster ?? idleArt.value : idleArt.value))

// Ambient extension: whatever the hero shows becomes the full-page layer and
// the local copies hide (their different crop would seam at the hero edges).
const { ambientEnabled } = useAppearance()
const ambientArt = useAmbientArt()
watch([activeBg, fallbackArt, ambientEnabled], ([bg, fb, on]) => {
  const url = bg ?? fb
  if (on && url) ambientArt.set(url)
  else ambientArt.clear()
}, { immediate: true })

function albumTo(al: MediaItem) {
  const a = al as Albumish
  if (a.artist_slug && a.album_slug) return `/music/artist/${a.artist_slug}/${a.album_slug}`
  return '/music/library'
}
</script>

<style scoped>
.hero-music { position: relative; height: 100%; }
.music-bg {
  position: absolute;
  inset: 0;
  background: var(--bg-0);
  overflow: hidden;
}
.music-bg-img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.music-bg-blur {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  filter: blur(42px) brightness(0.5) saturate(1.2);
  transform: scale(1.15);
}
/* Crossfade between rotating backdrops (both frames stay absolute, so the
   outgoing image fades under the incoming one). */
.mbg-enter-active, .mbg-leave-active { transition: opacity 0.9s ease; }
.mbg-enter-from, .mbg-leave-to { opacity: 0; }
.music-bg-gradient {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(to right, var(--bg-1) 0%, color-mix(in srgb, var(--bg-1) 55%, transparent) 52%, color-mix(in srgb, var(--bg-1) 12%, transparent) 100%),
    linear-gradient(to top, var(--bg-1) 0%, color-mix(in srgb, var(--bg-1) 65%, transparent) 22%, transparent 55%);
}
/* Ambient extension: the AmbientBackdrop layer owns the artwork full-page,
   so the hero paints nothing of its own. */
.music-bg.ambient-extended .music-bg-img,
.music-bg.ambient-extended .music-bg-blur,
.music-bg.ambient-extended .music-bg-gradient { display: none; }
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
/* Blended readability wash behind the lead text — same recipe as the hero:
   --bg-1-derived, long falloff, heavy blur, no locatable edge. z:-1 resolves
   against .music-inner's stacking context (z-index: 2), so the wash sits
   between the backdrop and the text. */
.music-lead, .music-lead-block { position: relative; }
.music-lead::before, .music-lead-block::before {
  content: '';
  position: absolute;
  inset: -90px -140px;
  z-index: -1;
  pointer-events: none;
  background: radial-gradient(ellipse 75% 70% at 30% 45%,
    color-mix(in srgb, var(--bg-1) 58%, transparent) 0%,
    color-mix(in srgb, var(--bg-1) 40%, transparent) 40%,
    color-mix(in srgb, var(--bg-1) 18%, transparent) 68%,
    transparent 92%);
  filter: blur(28px);
}
.music-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: var(--gold);
  margin-bottom: 8px;
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1);
}
.music-title {
  font-size: 38px;
  font-weight: 600;
  letter-spacing: -0.025em;
  line-height: 1.05;
  margin: 0 0 6px;
  text-wrap: balance;
  text-shadow:
    0 1px 2px var(--bg-1),
    0 0 10px var(--bg-1),
    0 0 24px var(--bg-1);
}
.music-inner.playing .music-title { font-size: 44px; margin-bottom: 10px; }
.music-sub {
  font-size: 14px;
  color: var(--fg-1);
  margin: 0;
  text-shadow:
    0 1px 2px var(--bg-1),
    0 0 10px var(--bg-1),
    0 0 24px var(--bg-1);
}
.music-inner.playing .music-sub { font-size: 15px; margin-bottom: 24px; }
.music-artist-link {
  color: var(--fg-0);
  font-weight: 600;
  transition: color 0.15s;
}
.music-artist-link:hover { color: var(--gold); }
.music-actions { display: flex; gap: 10px; flex-shrink: 0; }
/* Shelf scroller with shadow escape: pad the scroller so hover shadows and
   lift survive the overflow clip, pull the box back with negative margins so
   layout doesn't move. scroll-padding keeps snap/keyboard alignment. */
.music-shelf {
  display: flex;
  gap: 14px;
  overflow-x: auto;
  scrollbar-width: none;
  padding: 16px 40px 46px;
  margin: 0 -40px -24px;
  scroll-padding-left: 40px;
}
.music-shelf::-webkit-scrollbar { display: none; }
.music-cover {
  position: relative;
  width: 208px;
  flex-shrink: 0;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--bg-3);
  text-decoration: none;
  color: inherit;
  box-shadow: var(--shadow-card);
  transition: transform 0.18s, box-shadow 0.18s;
}
.music-cover:hover {
  transform: translateY(-3px);
  box-shadow: var(--shadow-card-hover);
}
.music-cover-label {
  position: absolute;
  inset: auto 0 0 0;
  padding: 24px 10px 8px;
  background: linear-gradient(to top, rgba(0,0,0,0.85), transparent); /* on artwork — stays literal */
}
.music-cover-title {
  font-size: 12.5px;
  font-weight: 600;
  color: #fff; /* on artwork scrim — stays literal */
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.music-cover-artist {
  font-size: 11px;
  color: rgba(255,255,255,0.72); /* on artwork scrim — stays literal */
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
  .music-shelf {
    padding: 12px 20px 40px;
    margin: 0 -20px -20px;
    scroll-padding-left: 20px;
  }
  .music-cover { width: 150px; }
  .music-now-art { display: none; }
}
</style>
