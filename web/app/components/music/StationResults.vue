<script setup lang="ts">
// Shared results panel for Quick Stations (Library Radio / Deep Cuts /
// Time Travel / Random Album). Each station page passes the tracks it
// loaded plus the label and a re-roll handler — this component renders
// the header + track list + Play/Save/Re-roll action buttons.
import type { Track } from '~/composables/usePlayer'

export interface StationTrack {
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

const props = defineProps<{
  label: string
  tracks: StationTrack[]
  loading?: boolean
  error?: string | null
  /** Label for the re-roll button (per-station copy, e.g. "Pick another album"). */
  rerollLabel?: string
  /** Default-name suffix when saving as a playlist. */
  saveLabel?: string
}>()

const emit = defineEmits<{ reroll: [] }>()

const { play, queue } = usePlayer()
const playlistsApi = usePlaylists()
const saveError = ref<string | null>(null)

function trackToPlayable(t: StationTrack): Track {
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
    source: 'station',
  }
}

async function playAll() {
  if (!props.tracks.length) return
  const built = props.tracks.map(trackToPlayable)
  queue.value = built
  await play(built[0]!)
}

async function playFrom(i: number) {
  const built = props.tracks.map(trackToPlayable)
  queue.value = built
  await play(built[i]!)
}

async function onSaveAsPlaylist() {
  if (!props.tracks.length) return
  const suggested = `${props.saveLabel || 'Mix'} — ${props.label}`
  const name = prompt('Playlist name', suggested)
  if (!name) return
  try {
    const created = await playlistsApi.create(name, '')
    for (const t of props.tracks) {
      await playlistsApi.addTrack(created.id, t.track_id)
    }
    navigateTo(`/music/playlist/${created.id}`)
  } catch {
    saveError.value = 'Could not save playlist.'
  }
}

function formatTotalDuration(rows: StationTrack[]): string {
  const total = rows.reduce((acc, r) => acc + (r.duration || 0), 0)
  const m = Math.round(total / 60)
  if (m < 60) return `${m} min`
  const h = Math.floor(m / 60)
  const rm = m % 60
  return `${h}h ${rm}m`
}
</script>

<template>
  <div class="sr-wrap">
    <div v-if="error" class="sr-error">{{ error }}</div>

    <div v-if="loading && !tracks.length" class="sr-loading">Building your station…</div>

    <template v-if="tracks.length">
      <div class="sr-head">
        <div>
          <div class="sr-label">{{ label }}</div>
          <div class="sr-meta">{{ tracks.length }} tracks · {{ formatTotalDuration(tracks) }}</div>
        </div>
        <div class="sr-actions">
          <button class="sr-action-btn primary" @click="playAll">
            <Icon name="play" :size="14" />
            <span>Play All</span>
          </button>
          <button class="sr-action-btn" @click="onSaveAsPlaylist">
            <Icon name="plus" :size="14" />
            <span>Save as Playlist</span>
          </button>
          <button class="sr-action-btn" @click="emit('reroll')">
            <Icon name="refresh" :size="14" />
            <span>{{ rerollLabel || 'Re-roll' }}</span>
          </button>
        </div>
      </div>

      <div v-if="saveError" class="sr-error">{{ saveError }}</div>

      <ul class="sr-track-list">
        <li
          v-for="(t, i) in tracks"
          :key="`st-${t.track_id}-${i}`"
          class="sr-track-row"
          @click="playFrom(i)"
        >
          <div class="sr-track-idx">{{ i + 1 }}</div>
          <div class="sr-track-art">
            <NuxtImg :src="useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? ''" :alt="t.album_title" :width="112" :quality="80" densities="1x 2x" loading="lazy" />
            <div class="sr-track-play"><Icon name="play" :size="13" /></div>
          </div>
          <div class="sr-track-meta">
            <div class="sr-track-title">{{ t.track_title }}</div>
            <div class="sr-track-sub">{{ t.artist_name }} · {{ t.album_title }}{{ t.album_year ? ' · ' + t.album_year : '' }}</div>
          </div>
          <div class="sr-track-dur">{{ formatDuration(t.duration) }}</div>
        </li>
      </ul>
    </template>
  </div>
</template>

<style scoped>
.sr-wrap { margin-top: 8px; }

.sr-loading { color: var(--fg-3); font-size: 13px; padding: 40px 0; text-align: center; }
.sr-error {
  color: #ff7676;
  font-size: 13px;
  padding: 12px 14px;
  border-radius: var(--r-sm);
  background: rgba(255, 118, 118, 0.06);
  border: 1px solid rgba(255, 118, 118, 0.2);
  margin-bottom: 16px;
}

.sr-head {
  display: flex; align-items: flex-end; justify-content: space-between;
  margin-bottom: 16px;
}
.sr-label {
  font-size: 22px;
  font-weight: 700;
  color: var(--fg-0);
  letter-spacing: -0.01em;
}
.sr-meta {
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  margin-top: 4px;
  letter-spacing: 0.04em;
}
.sr-actions { display: flex; gap: 6px; }
.sr-action-btn {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 7px 14px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-1);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s;
}
.sr-action-btn:hover { background: rgb(var(--ink) / 0.09); border-color: var(--fg-3); }
.sr-action-btn.primary {
  background: var(--gold);
  color: var(--bg-0);
  border-color: var(--gold);
}
.sr-action-btn.primary:hover { filter: brightness(1.1); border-color: var(--gold); }

.sr-track-list { display: flex; flex-direction: column; gap: 2px; }
.sr-track-row {
  display: grid;
  grid-template-columns: 28px 44px 1fr auto;
  gap: 12px;
  align-items: center;
  padding: 6px 8px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.15s;
}
.sr-track-row:hover { background: rgb(var(--ink) / 0.04); }
.sr-track-idx {
  text-align: right;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
}
.sr-track-art {
  position: relative;
  width: 44px; height: 44px;
  border-radius: 4px; overflow: hidden;
  background: var(--bg-3);
}
.sr-track-art img { width: 100%; height: 100%; object-fit: cover; display: block; }
.sr-track-play {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0,0,0,0.55); /* on artwork — stays literal */
  color: #fff; /* on artwork — stays literal */
  opacity: 0;
  transition: opacity 0.15s;
}
.sr-track-row:hover .sr-track-play { opacity: 1; }
.sr-track-meta { min-width: 0; }
.sr-track-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.sr-track-sub {
  font-size: 12px;
  color: var(--fg-3);
  margin-top: 2px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.sr-track-dur {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
  letter-spacing: 0.04em;
}
</style>
