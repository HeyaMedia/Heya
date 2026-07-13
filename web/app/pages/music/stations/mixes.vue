<template>
  <div class="ms-mixes page-pad">
    <MusicPageHead title="Mixes for You" subtitle="Auto-generated from your recent listening. Each mix is seeded on an artist and grown via sonic neighbors.">
      <NuxtLink to="/music/stations/builder" class="ms-mixes-builder-cta steer-glass">
        <Icon name="sparkle" :size="14" />
        <span>Build your own</span>
      </NuxtLink>
    </MusicPageHead>

    <div v-if="isLoading && !mixes.length" class="ms-mixes-loading">Building your mixes…</div>

    <MusicEmptyState v-if="!isLoading && !mixes.length" icon="sparkle" title="No mixes yet">
      Play a few tracks and Heya starts seeding mixes on the artists you
      return to. Or skip the wait — <NuxtLink to="/music/stations/builder">build your own</NuxtLink>.
    </MusicEmptyState>

    <!-- Featured marquee — the first mix opened up: its collage, a play/
         shuffle pair, and the opening run of tracks. One mix gets the stage;
         the rest wait in the grid below. -->
    <section v-if="featured" class="ms-feat" :style="featTone">
      <AppContextMenu :items="actions.forMix({ name: featured.name, seed_artist_slug: featured.seed_artist_slug, tracks: featured.tracks.map(mixTrackToEntity) })">
        <div class="ms-feat-inner">
          <NuxtLink :to="`/music/mix/${featured.seed_artist_slug}`" class="ms-feat-art" :aria-label="featured.name">
            <MixCollage :tracks="featured.tracks" :seed-src="seedArt(featured)" :alt="featured.name" />
          </NuxtLink>
          <div class="ms-feat-body">
            <div class="ms-feat-kicker">Mix · seeded on {{ featured.seed_artist_name }}</div>
            <NuxtLink :to="`/music/mix/${featured.seed_artist_slug}`" class="ms-feat-name">{{ featured.name }}</NuxtLink>
            <div class="ms-feat-meta">{{ featured.tracks.length }} tracks · {{ mixLength(featured) }}</div>
            <div class="ms-feat-actions">
              <button type="button" class="ms-feat-play" @click="playMix(featured)">
                <Icon name="play" :size="15" /><span>Play</span>
              </button>
              <button type="button" class="ms-feat-shuffle steer-glass" @click="playMix(featured, { shuffle: true })">
                <Icon name="shuffle" :size="14" /><span>Shuffle</span>
              </button>
            </div>
            <ol class="ms-feat-tracks">
              <li v-for="(t, i) in featured.tracks.slice(0, 5)" :key="t.track_id">
                <button type="button" class="ms-feat-track" @click="playMix(featured, { startIdx: i })">
                  <span class="ms-feat-track-title">{{ t.track_title }}</span>
                  <span class="ms-feat-track-artist">{{ t.artist_name }}</span>
                </button>
              </li>
            </ol>
          </div>
        </div>
      </AppContextMenu>
    </section>

    <div v-if="rest.length" class="ms-mixes-grid">
      <AppContextMenu
        v-for="mix in rest"
        :key="`mix-${mix.seed_artist_id}`"
        :items="actions.forMix({ name: mix.name, seed_artist_slug: mix.seed_artist_slug, tracks: mix.tracks.map(mixTrackToEntity) })"
      >
      <NuxtLink
        :to="`/music/mix/${mix.seed_artist_slug}`"
        class="ms-mix-card"
      >
        <MixCollage :tracks="mix.tracks" :seed-src="seedArt(mix)" :alt="mix.name">
          <div class="ms-mix-art-badge">Mix</div>
          <!-- span, not <button>: this tile is a NuxtLink, and a real button
               nested inside an anchor is invalid interactive-in-interactive
               HTML — see MusicCard.vue's .mc-play for the same fix. Always
               visible (semi-transparent glass) so touch gets a one-tap play
               without stealing the tile's navigate tap. -->
          <span
            role="button"
            tabindex="0"
            class="ms-mix-play"
            aria-label="Play mix"
            :title="`Play ${mix.name}`"
            @click.stop.prevent="playMix(mix)"
            @keydown.enter.stop.prevent="playMix(mix)"
            @keydown.space.stop.prevent="playMix(mix)"
          >
            <Icon name="play" :size="18" />
          </span>
        </MixCollage>
        <div class="ms-mix-meta">
          <div class="ms-mix-name">{{ mix.name }}</div>
          <div class="ms-mix-sub">{{ mix.tracks.length }} tracks · {{ mixLength(mix) }}</div>
        </div>
      </NuxtLink>
      </AppContextMenu>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import { useQuery } from '@pinia/colada'
import { musicMixesQuery, type MusicMix as Mix, type MusicMixTrack as MixTrack } from '~/queries/music'

definePageMeta({ layout: 'default' })

const { play, queue, playTracks } = usePlayerBindings()
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

const mixesQuery = useQuery(musicMixesQuery())
await waitForQuery(mixesQuery)
const mixes = computed<Mix[]>(() => mixesQuery.data.value ?? [])
const isLoading = computed(() => mixesQuery.isLoading.value)

const featured = computed(() => mixes.value[0] ?? null)
const rest = computed(() => mixes.value.slice(1))

function seedArt(mix: Mix) {
  return usePosterUrl({ id: mix.seed_artist_media_item_id, public_id: mix.seed_artist_media_item_public_id })
}

function mixLength(mix: Mix) {
  const secs = mix.tracks.reduce((s, t) => s + (t.duration || 0), 0)
  const mins = Math.round(secs / 60)
  if (mins < 60) return `${mins} min`
  return `${Math.floor(mins / 60)} hr ${mins % 60} min`
}

// Marquee tone — the featured panel wears its seed artist's sampled palette
// as a soft radial wash (same recipe as the Mix Builder's gold radial, hue
// swapped per mix). sampleImageTone memoizes per URL, so this is one canvas
// read per artist ever.
const featTone = ref<Record<string, string> | undefined>()
watch(featured, (mix) => {
  featTone.value = undefined
  if (!mix || !import.meta.client) return
  const src = seedArt(mix)
  if (!src) return
  sampleImageTone(src).then((t) => {
    if (t && featured.value === mix) featTone.value = { '--feat-tone': t.main }
  })
}, { immediate: true })

async function playMix(mix: Mix, opts: { shuffle?: boolean; startIdx?: number } = {}) {
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
  await playTracks(built, built[opts.startIdx ?? 0], { shuffle: opts.shuffle })
}
</script>

<style scoped>
.ms-mixes { max-width: 1400px; }

.ms-mixes-builder-cta {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 8px 14px;
  border-radius: var(--r-sm);
  color: var(--fg-1);
  text-decoration: none;
  font-size: 12px;
  font-weight: 600;
  transition: all 0.15s;
}
.ms-mixes-builder-cta:hover { border-color: var(--gold-soft); }

/* ── Featured marquee ─────────────────────────────────────────────── */
.ms-feat {
  margin-bottom: 28px;
  border-radius: var(--r-lg);
  overflow: hidden;
  /* Solid glass (ambient art behind) + the seed artist's sampled tone as a
     corner wash. --feat-tone lands via inline style once sampled; --gold is
     the pre-sample fallback so the panel never flashes colorless. */
  background:
    radial-gradient(ellipse 60% 90% at 0% 0%, color-mix(in srgb, var(--feat-tone, var(--gold)) 16%, transparent), transparent 55%),
    color-mix(in oklab, var(--bg-2) 88%, transparent);
  -webkit-backdrop-filter: blur(14px) saturate(140%);
  backdrop-filter: blur(14px) saturate(140%);
  border: 1px solid color-mix(in srgb, var(--feat-tone, var(--gold)) 26%, var(--border));
  box-shadow: var(--shadow-el);
}
.ms-feat-inner {
  display: grid;
  grid-template-columns: 240px 1fr;
  gap: 24px;
  padding: 22px;
}
.ms-feat-art { display: block; align-self: start; }
.ms-feat-body { min-width: 0; display: flex; flex-direction: column; }
.ms-feat-kicker {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--gold);
  margin-bottom: 6px;
}
.ms-feat-name {
  font-size: 24px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--fg-0);
  text-decoration: none;
  width: fit-content;
}
.ms-feat-name:hover { color: var(--gold); }
.ms-feat-meta {
  margin-top: 4px;
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.04em;
  color: var(--fg-2);
}
.ms-feat-actions { display: flex; gap: 8px; margin: 14px 0 4px; }
.ms-feat-play {
  display: inline-flex; align-items: center; gap: 7px;
  padding: 9px 18px;
  border-radius: 999px;
  background: var(--gold);
  color: var(--bg-0);
  font-size: 12.5px;
  font-weight: 700;
  cursor: pointer;
  transition: filter 0.15s;
}
.ms-feat-play:hover { filter: brightness(1.1); }
.ms-feat-shuffle {
  display: inline-flex; align-items: center; gap: 7px;
  padding: 9px 16px;
  border-radius: 999px;
  color: var(--fg-1);
  font-size: 12.5px;
  font-weight: 600;
  cursor: pointer;
}

/* Opening run — first five tracks as quiet, clickable rows. */
.ms-feat-tracks {
  list-style: none;
  margin: 10px 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
}
.ms-feat-track {
  display: flex;
  align-items: baseline;
  gap: 10px;
  width: 100%;
  padding: 5px 8px;
  margin-left: -8px;
  border-radius: var(--r-sm);
  text-align: left;
  cursor: pointer;
  min-width: 0;
  transition: background 0.12s;
}
.ms-feat-track:hover { background: rgb(var(--ink) / 0.05); }
.ms-feat-track-title {
  font-size: 13px;
  font-weight: 550;
  color: var(--fg-1);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.ms-feat-track:hover .ms-feat-track-title { color: var(--fg-0); }
.ms-feat-track-artist {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  flex-shrink: 0;
  max-width: 40%;
}

/* ── Grid of the rest ─────────────────────────────────────────────── */
.ms-mixes-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 20px;
}

.ms-mix-card {
  text-decoration: none;
  color: inherit;
  transition: transform 0.18s ease-out;
  display: block;
}
.ms-mix-card:hover { transform: translateY(-3px); }

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
/* Always-visible glass play — same treatment as the artist-page discography
   buttons: art reads through at rest, solidifies on hover/focus. One-tap
   play on touch; the rest of the tile still navigates. */
.ms-mix-play {
  position: absolute; right: 12px; bottom: 12px;
  width: 44px; height: 44px;
  border-radius: 50%;
  background: color-mix(in srgb, var(--gold) 52%, transparent);
  color: var(--bg-0);
  -webkit-backdrop-filter: blur(8px) saturate(140%);
  backdrop-filter: blur(8px) saturate(140%);
  border: 0;
  display: flex; align-items: center; justify-content: center;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.38); /* over artwork — literal */
  cursor: pointer;
  transition: background 0.18s, transform 0.15s;
}
.ms-mix-card:hover .ms-mix-play,
.ms-mix-play:focus-visible {
  background: color-mix(in srgb, var(--gold) 94%, transparent);
  transform: scale(1.06);
}

.ms-mix-meta { margin-top: 10px; }
.ms-mix-name {
  font-size: 14px;
  font-weight: 700;
  color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
.ms-mix-sub {
  font-size: 12px;
  color: var(--fg-2);
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
  margin-top: 2px;
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}

.ms-mixes-loading {
  color: var(--fg-2); font-size: 13px; padding: 60px 0; text-align: center;
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}

@media (max-width: 720px) {
  /* music.vue's phone header for this route reads "Mixes" — the title is
     redundant weight here; description + CTA carry real info and stay. */
  :deep(.mhd-title) { display: none; }
  .ms-mixes-builder-cta { align-self: flex-start; }

  .ms-feat-inner { grid-template-columns: 1fr; gap: 16px; padding: 16px; }
  .ms-feat-art { max-width: 260px; }
  .ms-feat-name { font-size: 20px; }

  .ms-mixes-grid { grid-template-columns: repeat(auto-fill, minmax(130px, 1fr)); gap: 14px; }
  .ms-mix-play { width: 40px; height: 40px; right: 10px; bottom: 10px; }
}
</style>
