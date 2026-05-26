<template>
  <div class="ml page-pad">
    <header class="ml-head">
      <div>
        <h1 class="ml-title">Loved Songs</h1>
        <div class="ml-sub">Every track you've rated, sorted by score. Rate ½★ to add — clear the rating to remove.</div>
      </div>
      <div class="ml-meta">
        <div class="ml-count">{{ total.toLocaleString() }}</div>
        <div class="ml-count-lbl">tracks</div>
      </div>
    </header>

    <div v-if="pending && !rows.length" class="ml-loading">Loading…</div>

    <div v-else-if="!rows.length" class="ml-empty">
      <Icon name="star" :size="40" />
      <h3>No rated tracks yet</h3>
      <p>Rate a track from the <NuxtLink to="/music/songs">Songs page</NuxtLink>, the player, or an album page. It'll appear here as soon as you give it any rating.</p>
    </div>

    <ul v-else class="ml-list">
      <AppContextMenu
        v-for="(t, i) in rows"
        :key="t.track_id"
        :items="actions.forTrack({ id: t.track_id, title: t.track_title, artist: t.artist_name, album: t.album_title, duration: t.duration, album_id: t.album_id, artist_id: t.artist_id, artist_slug: t.artist_slug, album_slug: t.album_slug })"
      >
      <li
        class="ml-row"
        :class="{ playing: currentTrack?.id === t.track_id }"
        @click="playFrom(i)"
      >
        <div class="ml-idx">{{ i + 1 }}</div>
        <div class="ml-art">
          <VuMeter v-if="currentTrack?.id === t.track_id" :playing="playing" />
          <template v-else>
            <img :src="useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? ''" :alt="t.album_title" loading="lazy" />
            <div class="ml-play"><Icon name="play" :size="13" /></div>
          </template>
        </div>
        <div class="ml-title-col">
          <div class="ml-title-cell">{{ t.track_title }}</div>
          <NuxtLink
            :to="`/music/artist/${t.artist_slug}`"
            class="ml-artist"
            @click.stop
          >{{ t.artist_name }}</NuxtLink>
        </div>
        <NuxtLink
          :to="`/music/artist/${t.artist_slug}/${t.album_slug}`"
          class="ml-album-cell"
          @click.stop
        >{{ t.album_title }}</NuxtLink>
        <div class="ml-rating-cell" @click.stop>
          <StarRating
            :model-value="ratings.get(t.track_id) ?? t.rating"
            size="sm"
            @update:model-value="(v) => onRatingChange(t.track_id, v)"
          />
        </div>
        <div class="ml-dur">{{ formatTime(t.duration) }}</div>
      </li>
      </AppContextMenu>
    </ul>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import { useQuery } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

interface RatedTrackRow {
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
  rating: number
}

const { play, queue, currentTrack, playing, formatTime } = usePlayer()
const { $heya } = useNuxtApp()
const trackRatings = useTrackRatings()
const ratings = trackRatings.ratings
const actions = useMusicActions()

const lovedQuery = useQuery({
  queryKey: ['me', 'ratings', 'loved-list'],
  queryFn: async () => {
    const r = await $heya('/api/me/ratings/tracks', { query: { min_rating: 1, limit: 500 } }) as unknown as { items: RatedTrackRow[]; total: number }
    trackRatings.primeMany(r.items.map((t) => [t.track_id, t.rating] as [number, number]))
    return r
  },
  staleTime: 1000 * 30,
})
const pending = computed(() => lovedQuery.isPending.value)
const rows = computed(() => lovedQuery.data.value?.items ?? [])
const total = computed(() => lovedQuery.data.value?.total ?? 0)

async function onRatingChange(trackId: number, v: number) {
  try {
    await trackRatings.set(trackId, v)
    // Clearing the rating drops the track out of this view; refetch so the
    // row disappears rather than lingering with empty stars.
    if (v === 0) lovedQuery.refetch()
  } catch {
    // optimistic rollback handled by composable
  }
}

function toPlayable(row: RatedTrackRow): Track {
  return {
    id: row.track_id,
    title: row.track_title,
    artist: row.artist_name,
    album: row.album_title,
    duration: row.duration,
    stream_url: `/api/music/tracks/${row.track_id}/stream`,
    album_id: row.album_id,
    artist_id: row.artist_id,
    poster: useAlbumCoverUrl(row.artist_slug, row.album_slug) ?? undefined,
    source: 'loved',
  }
}

async function playFrom(i: number) {
  const built = rows.value.map(toPlayable)
  queue.value = built
  await play(built[i]!)
}
</script>

<style scoped>
.ml { max-width: 1300px; }

.ml-head {
  display: flex; align-items: flex-end; justify-content: space-between; gap: 32px;
  margin-bottom: 28px;
}
.ml-title { font-size: 30px; font-weight: 700; letter-spacing: -0.01em; }
.ml-sub { color: var(--fg-3); font-size: 13px; margin-top: 4px; max-width: 540px; }
.ml-meta { text-align: right; }
.ml-count { font-size: 28px; font-weight: 700; color: var(--gold); }
.ml-count-lbl {
  font-size: 10px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.1em;
  color: var(--fg-3); margin-top: -2px;
}

.ml-loading { color: var(--fg-3); font-size: 13px; padding: 40px 0; text-align: center; }
.ml-empty {
  text-align: center; padding: 80px 20px; color: var(--fg-3);
}
.ml-empty :deep(svg) { color: var(--fg-3); margin-bottom: 12px; }
.ml-empty h3 { font-size: 18px; color: var(--fg-1); margin-bottom: 8px; font-weight: 600; }
.ml-empty p { font-size: 13px; line-height: 1.6; max-width: 440px; margin: 0 auto; }
.ml-empty a { color: var(--gold); text-decoration: none; }
.ml-empty a:hover { text-decoration: underline; }

.ml-list { display: flex; flex-direction: column; gap: 2px; }
.ml-row {
  display: grid;
  grid-template-columns: 32px 44px 1fr minmax(160px, 1.2fr) 130px 60px;
  gap: 12px;
  align-items: center;
  padding: 6px 10px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.15s;
}
.ml-row:hover { background: rgba(255,255,255,0.04); }
.ml-row.playing { background: var(--gold-soft); }
.ml-row.playing .ml-title-cell { color: var(--gold); }

.ml-idx { text-align: right; font-family: var(--font-mono); font-size: 11px; color: var(--fg-3); }
.ml-art {
  position: relative;
  width: 44px; height: 44px;
  border-radius: 4px; overflow: hidden;
  background: var(--bg-3);
}
.ml-art img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ml-play {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.55); color: #fff;
  opacity: 0; transition: opacity 0.15s;
}
.ml-row:hover .ml-play { opacity: 1; }

.ml-title-col { min-width: 0; }
.ml-title-cell {
  font-size: 14px; font-weight: 500; color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.ml-artist {
  font-size: 12px; color: var(--fg-3);
  text-decoration: none;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  display: inline-block; max-width: 100%; margin-top: 1px;
}
.ml-artist:hover { color: var(--fg-1); text-decoration: underline; }
.ml-album-cell {
  font-size: 13px; color: var(--fg-2);
  text-decoration: none;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  display: block;
}
.ml-album-cell:hover { color: var(--fg-0); text-decoration: underline; }
.ml-rating-cell { display: flex; align-items: center; }
.ml-dur {
  font-family: var(--font-mono); font-size: 11px;
  color: var(--fg-3); letter-spacing: 0.04em;
  text-align: right;
}
</style>
