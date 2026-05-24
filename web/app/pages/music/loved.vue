<template>
  <div class="page-pad">
    <h2 class="m-h2">Loved Songs</h2>
    <div v-if="pending" class="m-loading">Loading…</div>
    <div v-else-if="!rows.length" class="m-empty">No loved songs yet — tap the heart on any track.</div>
    <div v-else class="list-rows">
      <div class="list-row list-row-head" style="grid-template-columns: 2fr 1fr 1fr 36px 0.5fr">
        <div>Title</div><div>Artist</div><div>Album</div><div></div><div style="text-align: right">Duration</div>
      </div>
      <div
        v-for="row in rows"
        :key="row.track_id"
        class="list-row"
        style="grid-template-columns: 2fr 1fr 1fr 36px 0.5fr"
        @click="playRow(row)"
      >
        <div class="list-title-cell">
          <VuMeter v-if="currentTrack?.id === row.track_id" :playing="playing" />
          <Poster v-else :idx="row.track_id" :src="row.album_cover_path || null" aspect="1/1" style="width: 40px; height: 40px; border-radius: 4px; flex-shrink: 0" />
          <div>
            <div class="list-title" :style="currentTrack?.id === row.track_id ? { color: 'var(--gold)' } : {}">{{ row.track_title }}</div>
          </div>
        </div>
        <div class="list-cell">{{ row.artist_name }}</div>
        <div class="list-cell">{{ row.album_title }}</div>
        <button
          class="loved-heart"
          @click.stop="loved.toggle(row.track_id)"
          title="Remove from Loved"
        >
          <Icon name="heartfill" :size="14" />
        </button>
        <div class="list-cell-right mono">{{ formatTime(row.duration) }}</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MusicListPage } from '~~/shared/types'
import type { Track } from '~/composables/usePlayer'

definePageMeta({ layout: 'default' })

interface LovedTrackRow {
  track_id: number
  track_title: string
  duration: number
  album_id: number
  album_title: string
  album_cover_path: string
  album_year: string
  album_slug: string
  artist_id: number
  artist_name: string
  artist_slug: string
}

const { play, queue, currentTrack, playing, formatTime } = usePlayer()
const loved = useLovedTracks()
if (import.meta.client) loved.ensureLoaded()

const { data, pending } = useApi<MusicListPage<LovedTrackRow>>('/api/me/loved/tracks?limit=500')
const rows = computed(() => data.value?.items ?? [])

function toPlayable(row: LovedTrackRow): Track {
  return {
    id: row.track_id,
    title: row.track_title,
    artist: row.artist_name,
    album: row.album_title,
    duration: row.duration,
    stream_url: `/api/tracks/${row.track_id}/stream`,
    album_id: row.album_id,
    artist_id: row.artist_id,
    poster: row.album_cover_path || undefined,
  }
}

async function playRow(row: LovedTrackRow) {
  queue.value = rows.value.map(toPlayable)
  await play(toPlayable(row))
}
</script>

<style scoped>
.m-h2 { font-size: 24px; font-weight: 600; margin-bottom: 20px; }
.m-loading, .m-empty { color: var(--fg-3); padding: 24px 0; font-size: 13px; }
.list-cell { font-size: 13px; color: var(--fg-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.list-cell-right { font-size: 12px; color: var(--fg-3); text-align: right; }
.mono { font-family: var(--font-mono); }
.loved-heart {
  background: transparent;
  border: 0;
  color: var(--gold);
  padding: 6px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.15s;
}
.loved-heart:hover { background: rgba(255,255,255,0.06); }
</style>
