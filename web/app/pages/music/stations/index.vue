<template>
  <div class="ms-stations page-pad">
    <header class="ms-st-head">
      <div>
        <h1 class="ms-st-title">Stations</h1>
        <div class="ms-st-sub">Endless, personalised streams from your own library.</div>
      </div>
      <NuxtLink to="/music/stations/builder" class="ms-st-builder-cta">
        <Icon name="sparkle" :size="16" />
        <span>Mix Builder</span>
      </NuxtLink>
    </header>

    <!-- Mixes for You -->
    <MusicScrollRow
      v-if="mixes.length"
      title="Mixes for You"
      :card-size="200"
    >
      <AppContextMenu
        v-for="mix in mixes"
        :key="`mix-${mix.seed_artist_id}`"
        :items="actions.forMix({ name: mix.name, seed_artist_slug: mix.seed_artist_slug, tracks: mix.tracks.map(mixTrackToEntity) })"
      >
      <NuxtLink
        :to="`/music/mix/${mix.seed_artist_slug}`"
        class="ms-card-link"
      >
        <MusicCard
          :src="usePosterUrl({ id: mix.seed_artist_media_item_id, public_id: mix.seed_artist_media_item_public_id }) ?? undefined"
          :alt="mix.name"
          :title="mix.name"
          :subtitle="`${mix.tracks.length} tracks`"
          badge-tl="Mix"
          @play="playMix(mix)"
        />
      </NuxtLink>
      </AppContextMenu>
    </MusicScrollRow>

    <!-- Quick Stations: gold-tinted cards. Each links to a per-station page. -->
    <section class="ms-section">
      <h2 class="section-title-lg ms-section-title">Quick Stations</h2>
      <div class="ms-station-grid">
        <NuxtLink
          v-for="s in stations"
          :key="s.slug"
          :to="`/music/stations/${s.slug}`"
          class="ms-station-card"
          :style="{ background: s.gradient }"
        >
          <div class="ms-station-body">
            <Icon :name="s.icon" :size="22" class="ms-station-icon" />
            <div class="ms-station-name">{{ s.name }}</div>
            <div class="ms-station-desc">{{ s.desc }}</div>
          </div>
          <div class="ms-station-arrow">
            <Icon name="play" :size="14" />
          </div>
        </NuxtLink>
      </div>
    </section>

    <!-- Browse by feel — bridge to existing /music/browse. -->
    <section class="ms-section">
      <h2 class="section-title-lg ms-section-title">Browse by Feel</h2>
      <div class="ms-browse-grid">
        <NuxtLink to="/music/browse" class="ms-browse-card mood">
          <div class="ms-browse-emoji">💭</div>
          <div class="ms-browse-name">Moods</div>
          <div class="ms-browse-desc">Tracks tagged with how they feel</div>
        </NuxtLink>
        <NuxtLink to="/music/browse" class="ms-browse-card genre">
          <div class="ms-browse-emoji">🎼</div>
          <div class="ms-browse-name">Genres</div>
          <div class="ms-browse-desc">By style, decade, and scene</div>
        </NuxtLink>
        <NuxtLink to="/music/browse" class="ms-browse-card tempo">
          <div class="ms-browse-emoji">⚡</div>
          <div class="ms-browse-name">Tempo</div>
          <div class="ms-browse-desc">Slow burns to peak-time bangers</div>
        </NuxtLink>
      </div>
    </section>

    <!-- Loading -->
    <div v-if="mixesLoading && !mixes.length" class="ms-loading">Loading mixes…</div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

const { play, queue } = usePlayer()
const { $heya } = useNuxtApp()
const actions = useMusicActions()

function mixTrackToEntity(t: MixTrack) {
  return {
    id: t.track_id,
    title: t.track_title,
    artist: t.artist_name,
    album: t.album_title,
    duration: t.duration,
    album_id: t.album_id,
    artist_id: t.artist_id,
    artist_slug: t.artist_slug,
    album_slug: t.album_slug,
  }
}

interface MixTrack {
  track_id: number
  track_title: string
  duration: number
  album_id: number
  album_title: string
  album_slug: string
  album_year: string
  artist_id: number
  artist_name: string
  artist_slug: string
}
interface Mix {
  seed_artist_id: number
  seed_artist_name: string
  seed_artist_slug: string
  seed_artist_media_item_id: number
  seed_artist_media_item_public_id?: string
  name: string
  tracks: MixTrack[]
}

const mixesQuery = useQuery({
  queryKey: ['music', 'stations', 'mixes'],
  queryFn: async () => {
    const r = await $heya('/api/music/home/mixes-for-you', { query: { max: 8 } }) as unknown as { items: Mix[] }
    return r.items ?? []
  },
  staleTime: 1000 * 60 * 30,
})
const mixes = computed<Mix[]>(() => mixesQuery.data.value ?? [])
const mixesLoading = computed(() => mixesQuery.isLoading.value)

// Quick Stations definitions. Slugs match the placeholder per-station pages
// the user can browse to today; backend resolution lands as we build each one.
const stations = [
  {
    slug: 'library',
    name: 'Library Radio',
    desc: 'Your whole catalog, intelligently shuffled',
    icon: 'radio',
    gradient: 'linear-gradient(135deg, #2a1d4a, #5b3aa1)',
  },
  {
    slug: 'deep-cuts',
    name: 'Deep Cuts',
    desc: "Tracks you own but rarely play",
    icon: 'compass',
    gradient: 'linear-gradient(135deg, #1d3a4a, #3a8aa1)',
  },
  {
    slug: 'time-travel',
    name: 'Time Travel',
    desc: 'Drop into a decade or a single year',
    icon: 'clock',
    gradient: 'linear-gradient(135deg, #4a2d1d, #a16d3a)',
  },
  {
    slug: 'random-album',
    name: 'Random Album',
    desc: 'A full album, drawn at random — end to end',
    icon: 'music',
    gradient: 'linear-gradient(135deg, #4a1d3a, #a13a7d)',
  },
]

function mixTrackToTrack(t: MixTrack): Track {
  return {
    id: t.track_id,
    title: t.track_title,
    artist: t.artist_name,
    album: t.album_title,
    duration: t.duration,
    stream_url: `/api/music/tracks/${t.track_id}/stream`,
    album_id: t.album_id,
    artist_id: t.artist_id,
    artist_slug: t.artist_slug,
    album_slug: t.album_slug,
    poster: useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? undefined,
    source: 'mix',
  }
}

async function playMix(mix: Mix) {
  if (!mix.tracks.length) return
  const built = mix.tracks.map(mixTrackToTrack)
  queue.value = built
  await play(built[0]!)
}
</script>

<style scoped>
.ms-stations { max-width: 1400px; }

.ms-st-head {
  display: flex; align-items: flex-end; justify-content: space-between; gap: 32px;
  margin-bottom: 32px;
  padding-bottom: 24px;
  border-bottom: 1px solid var(--border);
}
.ms-st-title { font-size: 32px; font-weight: 700; letter-spacing: -0.01em; }
.ms-st-sub { color: var(--fg-3); font-size: 13px; margin-top: 4px; }
.ms-st-builder-cta {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 10px 18px;
  background: var(--gold);
  color: var(--bg-0);
  border-radius: var(--r-sm);
  text-decoration: none;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.02em;
  transition: filter 0.15s;
}
.ms-st-builder-cta:hover { filter: brightness(1.1); }

.ms-card-link { text-decoration: none; color: inherit; display: block; }

.ms-section { margin-bottom: 40px; }
.ms-section-title { margin-bottom: 16px; }

.ms-station-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 14px;
}
.ms-station-card {
  position: relative;
  min-height: 130px;
  padding: 20px;
  border-radius: var(--r-md);
  text-decoration: none;
  color: #fff; /* on the fixed gradient tile — stays literal */
  display: flex; align-items: flex-end; gap: 14px;
  overflow: hidden;
  transition: transform 0.18s ease-out, box-shadow 0.18s ease-out;
  box-shadow: 0 8px 24px rgb(var(--shade) / 0.35);
}
.ms-station-card:hover { transform: translateY(-3px); box-shadow: 0 12px 32px rgb(var(--shade) / 0.45); }
.ms-station-body { flex: 1; }
.ms-station-icon {
  display: block;
  margin-bottom: 12px;
  opacity: 0.7;
}
.ms-station-name {
  font-size: 18px;
  font-weight: 700;
  letter-spacing: -0.01em;
  text-shadow: 0 1px 4px rgba(0,0,0,0.3);
}
.ms-station-desc {
  font-size: 12px;
  opacity: 0.75;
  margin-top: 4px;
  text-shadow: 0 1px 3px rgba(0,0,0,0.3);
}
.ms-station-arrow {
  width: 40px; height: 40px;
  border-radius: 50%;
  /* badge painted over the gradient tile — stays literal */
  background: rgba(255,255,255,0.2);
  backdrop-filter: blur(8px);
  display: flex; align-items: center; justify-content: center;
  color: #fff;
  flex-shrink: 0;
  transition: background 0.15s;
}
.ms-station-card:hover .ms-station-arrow { background: rgba(255,255,255,0.35); }

.ms-browse-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 14px;
}
.ms-browse-card {
  padding: 24px 22px;
  background: rgb(var(--ink) / 0.03);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  text-decoration: none;
  color: inherit;
  transition: all 0.15s;
}
.ms-browse-card:hover {
  background: rgb(var(--ink) / 0.06);
  border-color: var(--gold-soft);
  transform: translateY(-2px);
}
.ms-browse-emoji {
  font-size: 28px;
  margin-bottom: 8px;
}
.ms-browse-name {
  font-size: 16px; font-weight: 700;
  color: var(--fg-0);
}
.ms-browse-desc {
  font-size: 12px;
  color: var(--fg-3);
  margin-top: 2px;
}

.ms-loading {
  color: var(--fg-3); font-size: 13px; padding: 40px 0; text-align: center;
}

@media (max-width: 720px) {
  .ms-st-head { flex-direction: column; align-items: stretch; gap: 14px; margin-bottom: 24px; padding-bottom: 20px; }
  /* music.vue's phone section header already reads "Stations" directly
     above this page — the sub line and the Mix Builder CTA both stay. */
  .ms-st-title { display: none; }
  .ms-st-builder-cta { justify-content: center; }

  .ms-station-grid { gap: 10px; }
  .ms-station-card { min-height: 100px; padding: 16px; }
  .ms-browse-grid { grid-template-columns: repeat(2, 1fr); gap: 10px; }
  .ms-browse-card { padding: 16px 14px; }
}
</style>
