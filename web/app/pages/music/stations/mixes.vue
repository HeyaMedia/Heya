<template>
  <div class="ms-mixes page-pad">
    <header class="ms-mixes-head">
      <div>
        <h1 class="ms-mixes-title">Mixes for You</h1>
        <div class="ms-mixes-sub">Auto-generated from your recent listening. Each mix is seeded on an artist and grown via sonic neighbors.</div>
      </div>
      <NuxtLink to="/music/stations/builder" class="ms-mixes-builder-cta">
        <Icon name="sparkle" :size="14" />
        <span>Build your own</span>
      </NuxtLink>
    </header>

    <div v-if="isLoading && !mixes.length" class="ms-mixes-loading">Building your mixes…</div>

    <div v-if="!isLoading && !mixes.length" class="ms-mixes-empty">
      <Icon name="sparkle" :size="40" />
      <h3>No mixes yet</h3>
      <p>Play some music — once you've listened to a few tracks, we'll build mixes seeded on your favorite artists.</p>
    </div>

    <div v-if="mixes.length" class="ms-mixes-grid">
      <AppContextMenu
        v-for="mix in mixes"
        :key="`mix-${mix.seed_artist_id}`"
        :items="actions.forMix({ name: mix.name, seed_artist_slug: mix.seed_artist_slug, tracks: mix.tracks.map(mixTrackToEntity) })"
      >
      <NuxtLink
        :to="`/music/mix/${mix.seed_artist_slug}`"
        class="ms-mix-card"
      >
        <div class="ms-mix-art">
          <img
            v-if="mix.seed_artist_media_item_id"
            :src="usePosterUrl(mix.seed_artist_media_item_id) ?? ''"
            :alt="mix.name"
            loading="lazy"
          />
          <div v-else class="ms-mix-art-fallback"><Icon name="sparkle" :size="36" /></div>
          <div class="ms-mix-art-gradient" />
          <div class="ms-mix-art-badge">Mix</div>
          <button class="ms-mix-play" type="button" :title="`Play ${mix.name}`" @click.stop.prevent="playMix(mix)">
            <Icon name="play" :size="18" />
          </button>
        </div>
        <div class="ms-mix-meta">
          <div class="ms-mix-name">{{ mix.name }}</div>
          <div class="ms-mix-sub">{{ mix.tracks.length }} tracks</div>
        </div>
      </NuxtLink>
      </AppContextMenu>
    </div>
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
  name: string
  tracks: MixTrack[]
}

const mixesQuery = useQuery({
  queryKey: ['music', 'stations', 'mixes', 'all'],
  queryFn: async () => {
    const r = await $heya('/api/music/home/mixes-for-you', { query: { max: 20 } }) as unknown as { items: Mix[] }
    return r.items ?? []
  },
  staleTime: 1000 * 60 * 15,
})
const mixes = computed<Mix[]>(() => mixesQuery.data.value ?? [])
const isLoading = computed(() => mixesQuery.isLoading.value)

async function playMix(mix: Mix) {
  if (!mix.tracks.length) return
  const built: Track[] = mix.tracks.map((t) => ({
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
  }))
  queue.value = built
  await play(built[0]!)
}
</script>

<style scoped>
.ms-mixes { max-width: 1400px; }

.ms-mixes-head {
  display: flex; align-items: flex-end; justify-content: space-between; gap: 32px;
  margin-bottom: 24px;
  padding-bottom: 20px;
  border-bottom: 1px solid var(--border);
}
.ms-mixes-title { font-size: 30px; font-weight: 700; letter-spacing: -0.01em; }
.ms-mixes-sub { color: var(--fg-3); font-size: 13px; margin-top: 4px; max-width: 540px; }
.ms-mixes-builder-cta {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 8px 14px;
  background: rgba(255,255,255,0.04);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-1);
  text-decoration: none;
  font-size: 12px;
  font-weight: 600;
  transition: all 0.15s;
}
.ms-mixes-builder-cta:hover { background: var(--gold-soft); color: var(--gold); border-color: var(--gold-soft); }

.ms-mixes-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 20px;
}

.ms-mix-card {
  text-decoration: none;
  color: inherit;
  transition: transform 0.18s ease-out;
}
.ms-mix-card:hover { transform: translateY(-3px); }

.ms-mix-art {
  position: relative;
  aspect-ratio: 1 / 1;
  background: var(--bg-3);
  overflow: hidden;
  border-radius: var(--r-md);
  box-shadow: 0 8px 18px rgba(0, 0, 0, 0.45);
}
.ms-mix-art img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ms-mix-art-fallback {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  color: var(--gold);
  background: linear-gradient(135deg, rgba(255, 196, 50, 0.10), rgba(255, 196, 50, 0.02));
}
.ms-mix-art-gradient {
  position: absolute; inset: 0;
  background: linear-gradient(0deg, rgba(0,0,0,0.55) 0%, rgba(0,0,0,0.1) 45%, transparent 75%);
  pointer-events: none;
}
.ms-mix-art-badge {
  position: absolute; top: 10px; left: 10px;
  padding: 3px 10px;
  background: var(--gold);
  color: var(--bg-0);
  border-radius: 999px;
  font-size: 10px;
  font-weight: 700;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.ms-mix-play {
  position: absolute; right: 14px; bottom: 14px;
  width: 48px; height: 48px;
  border-radius: 50%;
  background: var(--gold);
  color: var(--bg-0);
  border: 0;
  display: flex; align-items: center; justify-content: center;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.4);
  cursor: pointer;
  opacity: 0;
  transform: translateY(8px);
  transition: opacity 0.2s, transform 0.2s, filter 0.15s;
}
.ms-mix-card:hover .ms-mix-play { opacity: 1; transform: translateY(0); }
.ms-mix-play:hover { filter: brightness(1.1); }

.ms-mix-meta { margin-top: 10px; }
.ms-mix-name {
  font-size: 14px;
  font-weight: 700;
  color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ms-mix-sub {
  font-size: 12px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
  margin-top: 2px;
}

.ms-mixes-loading {
  color: var(--fg-3); font-size: 13px; padding: 60px 0; text-align: center;
}
.ms-mixes-empty {
  text-align: center;
  padding: 80px 20px;
  color: var(--fg-3);
}
.ms-mixes-empty :deep(svg) { color: var(--gold); margin-bottom: 12px; opacity: 0.6; }
.ms-mixes-empty h3 { font-size: 16px; color: var(--fg-1); margin-bottom: 8px; font-weight: 600; }
.ms-mixes-empty p { font-size: 13px; max-width: 400px; margin: 0 auto; line-height: 1.5; }
</style>
