<template>
  <div class="album-card">
    <div class="album-head">
      <NuxtLink :to="albumHref" class="album-cover-wrap">
        <Poster :idx="album.id" :src="coverUrl" aspect="1/1" class="album-cover" />
      </NuxtLink>
      <div class="album-info">
        <NuxtLink :to="albumHref" class="album-title">{{ album.title }}</NuxtLink>
        <div class="album-meta">
          <span v-if="album.year">{{ album.year }}</span>
          <span v-if="album.album_type && album.album_type !== 'album'" class="album-kind-chip">{{ album.album_type }}</span>
          <span class="album-meta-dot">·</span>
          <span>{{ album.tracks.length }} tracks</span>
          <span v-if="totalDuration > 0" class="album-meta-dot">·</span>
          <span v-if="totalDuration > 0">{{ formatRunTime(totalDuration) }}</span>
        </div>
        <div class="album-actions">
          <button class="btn btn-sm btn-primary" @click="$emit('play-album', { shuffle: false })">
            <Icon name="play" :size="12" /> Play
          </button>
          <button class="btn btn-sm" @click="$emit('play-album', { shuffle: true })">
            <Icon name="shuffle" :size="12" /> Shuffle
          </button>
          <button class="btn btn-sm" @click="$emit('queue-album')">
            <Icon name="plus" :size="12" /> Queue
          </button>
          <button class="btn btn-sm btn-ghost" @click="expanded = !expanded">
            <Icon :name="expanded ? 'chevdown' : 'chevright'" :size="12" />
            {{ expanded ? 'Hide tracks' : 'Show tracks' }}
          </button>
        </div>
      </div>
    </div>

    <div v-if="expanded" class="album-tracks">
      <div
        v-for="(t, i) in groupedTracks"
        :key="t.id"
        class="track-row"
        :class="{ 'disc-first': isDiscBoundary(i) }"
        @click="$emit('play-track', t)"
      >
        <div class="track-num">{{ t.track_number || (i + 1) }}</div>
        <div class="track-title-cell">
          <div class="track-title" :style="currentTrack?.id === t.id ? { color: 'var(--gold)' } : {}">{{ t.title }}</div>
          <div v-if="t.files.length" class="track-format" @click.stop>
            <TrackQualityPicker
              :files="t.files"
              :selected-id="primaryFile(t)?.id"
              @pick="$emit('play-track-file', { track: t, file: $event })"
            />
          </div>
        </div>
        <div class="track-dur mono">{{ formatTime(t.duration) }}</div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { AlbumView, Artist, TrackFile, TrackView } from '~~/shared/types'

const props = defineProps<{
  album: AlbumView
  artist: Artist
  artistSlug: string
}>()

defineEmits<{
  'play-track': [TrackView]
  'play-track-file': [{ track: TrackView; file: TrackFile }]
  'play-album': [{ shuffle: boolean }]
  'queue-album': []
}>()

const { currentTrack, formatTime } = usePlayer()

const expanded = ref(false)

const coverUrl = computed(() => props.album.cover_path || null)

const albumHref = computed(() => {
  if (!props.album.slug) return ''
  return `/music/artist/${props.artistSlug}/${props.album.slug}`
})

const totalDuration = computed(() =>
  props.album.tracks.reduce((sum, t) => sum + (t.duration || 0), 0),
)

const groupedTracks = computed(() => {
  // Sort by disc then track so multi-disc albums render in physical order.
  return [...props.album.tracks].sort((a, b) => {
    if (a.disc_number !== b.disc_number) return a.disc_number - b.disc_number
    return a.track_number - b.track_number
  })
})

function isDiscBoundary(idx: number) {
  if (idx === 0) return false
  const prev = groupedTracks.value[idx - 1]
  const curr = groupedTracks.value[idx]
  return !!prev && !!curr && prev.disc_number !== curr.disc_number
}

function primaryFile(t: TrackView): TrackFile | null {
  // Files are returned by the API best-quality-first.
  return t.files[0] ?? null
}

function formatRunTime(seconds: number) {
  if (seconds < 3600) return formatTime(seconds)
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return `${h}h ${m}m`
}
</script>

<style scoped>
.album-card {
  background: var(--bg-3);
  border-radius: var(--r-md);
  padding: 16px;
}
.album-head {
  display: flex;
  gap: 16px;
}
.album-cover-wrap { flex-shrink: 0; }
.album-cover {
  width: 140px;
  height: 140px;
  border-radius: var(--r-sm);
}
.album-info { flex: 1; min-width: 0; display: flex; flex-direction: column; justify-content: center; }
.album-title {
  font-size: 18px;
  font-weight: 600;
  color: var(--fg-0);
  text-decoration: none;
}
.album-title:hover { color: var(--gold); }
.album-meta {
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  color: var(--fg-2);
  margin-top: 4px;
  margin-bottom: 12px;
}
.album-meta-dot { color: var(--fg-3); }
.album-kind-chip {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  padding: 1px 6px;
  border-radius: 4px;
  background: var(--gold-soft);
  color: var(--gold);
}
.album-actions { display: flex; flex-wrap: wrap; gap: 6px; }
.btn-sm {
  font-size: 12px;
  padding: 6px 10px;
  height: 28px;
}
.btn-ghost { background: transparent; color: var(--fg-2); }
.btn-ghost:hover { background: rgba(255,255,255,0.04); color: var(--fg-0); }

.album-tracks {
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
  display: flex;
  flex-direction: column;
}
.track-row {
  display: grid;
  grid-template-columns: 36px 1fr 60px;
  align-items: center;
  gap: 12px;
  padding: 6px 8px;
  border-radius: var(--r-sm);
  cursor: pointer;
}
.track-row:hover { background: rgba(255,255,255,0.04); }
.track-row.disc-first { border-top: 1px solid var(--border); margin-top: 8px; padding-top: 12px; }
.track-num {
  text-align: right;
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--fg-3);
}
.track-title-cell { min-width: 0; }
.track-title {
  font-size: 14px;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.track-format { margin-top: 2px; }
.track-dur {
  font-size: 12px;
  color: var(--fg-3);
  text-align: right;
}
.mono { font-family: var(--font-mono); }
</style>
