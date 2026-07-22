<template>
  <div v-if="loading" class="page-pad m-loading">Loading mix…</div>
  <div v-else-if="!mix" class="page-pad m-empty">
    <MusicEmptyState icon="music" title="Mix not found">
      Your generated mixes rotate as your taste and library change.
    </MusicEmptyState>
  </div>
  <div v-else class="mix-page" :style="toneStyle">
    <MusicCollectionDetail
      kind="Mix"
      :title="mix.name"
      :description="mix.description"
      :images="artistArtUrls"
      :backdrop="firstAlbumCover"
      :ledger-cells="ledgerCells"
      :tracks="tlRows"
      :tracks-meta="`${mix.tracks.length} ${mix.tracks.length === 1 ? 'track' : 'tracks'}`"
      :columns="columns"
      storage-key="mix"
      :context-items="contextItemsFor"
      :active-track-id="currentTrack?.id ?? null"
      :playing="playing"
      vu-meter-in="art"
      :duration-formatter="formatTime"
      @image="currentBgArt = $event"
      @row-click="playFrom"
    >
      <template #stats>
        <NuxtLink
          v-if="mix.seed_artist_slug"
          :to="`/music/artist/${mix.seed_artist_slug}`"
          class="mix-seed-link"
        >
          Seeded from {{ mix.seed_artist_name }}
        </NuxtLink>
        <span v-if="mix.seed_artist_slug" class="dot">·</span>
        <span>{{ mix.tracks.length }} {{ mix.tracks.length === 1 ? 'track' : 'tracks' }}</span>
        <span v-if="totalDuration" class="dot">·</span>
        <span v-if="totalDuration">{{ formatRunTime(totalDuration) }}</span>
      </template>

      <template #actions>
        <button class="btn-play collection-half" :disabled="!mix.tracks.length" @click="playAll(false)">
          <span class="tri" /> Play <small>{{ mix.tracks.length }} {{ mix.tracks.length === 1 ? 'TRACK' : 'TRACKS' }}</small>
        </button>
        <button class="pill collection-half" :disabled="!mix.tracks.length" @click="playAll(true)">
          <Icon name="shuffle" :size="15" /> Shuffle
        </button>
      </template>

      <template #empty>
        <MusicEmptyState icon="music" title="This mix is empty" compact>
          It will fill up as Heya learns more about your library and listening.
        </MusicEmptyState>
      </template>
    </MusicCollectionDetail>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { TrackListColumn, TrackListRow } from '~/components/music/TrackList.vue'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import type { ImageTone } from '~/composables/useImageTone'
import { useQuery } from '@pinia/colada'
import { musicMixesQuery, type MusicMix as Mix, type MusicMixTrack as MixTrack } from '~/queries/music'

definePageMeta({ layout: 'default' })

const route = useRoute()
const slug = computed(() => String(route.params.slug ?? ''))
const { currentTrack, playing, formatTime, playTracks } = usePlayerBindings()
const actions = useMusicActions()

// Shares the cache key with MusicHome's mixes query, so opening a card is
// immediate when the home shelf already has the generated mix payload.
const mixesQuery = useQuery(musicMixesQuery())
await waitForQuery(mixesQuery)
const mix = computed<Mix | null>(() => (mixesQuery.data.value ?? []).find(m => m.slug === slug.value) ?? null)
const loading = computed(() => mixesQuery.isPending.value)

const totalDuration = computed(() =>
  (mix.value?.tracks ?? []).reduce((sum, track) => sum + (track.duration || 0), 0),
)

const firstAlbumCover = computed(() => {
  const first = mix.value?.tracks[0]
  return first ? useAlbumCoverUrl(first.artist_slug, first.album_slug) : null
})
const currentBgArt = ref<string | null>(null)

const artistArtUrls = computed(() => {
  const seen = new Set<string>()
  const urls: string[] = []
  for (const track of mix.value?.tracks ?? []) {
    if (!track.artist_slug || seen.has(track.artist_slug)) continue
    seen.add(track.artist_slug)
    urls.push(`/api/media/${track.artist_slug}/image/poster`)
    if (urls.length === 12) break
  }
  return urls
})

const ledgerCells = computed<LedgerCell[]>(() => {
  const tracks = mix.value?.tracks ?? []
  if (!tracks.length) return []
  return [
    { k: 'Tracks', v: String(tracks.length) },
    { k: 'Runtime', v: formatRunTime(totalDuration.value) },
    { k: 'Artists', v: String(new Set(tracks.map(track => track.artist_id)).size) },
    { k: 'Albums', v: String(new Set(tracks.map(track => track.album_id)).size) },
    { k: 'Plays', v: tracks.reduce((sum, track) => sum + (track.play_count || 0), 0).toLocaleString(), tone: true },
  ]
})

// Match the tone-follow contract used by Playlist and Loved Songs. The
// current sharp hero image is sampled only as a fallback; AmbientBackdrop's
// shared sample remains the primary source.
const bgTone = useBackgroundTone()
const localTone = ref<ImageTone | null>(null)
let toneSeq = 0
watch(() => currentBgArt.value || firstAlbumCover.value, (src) => {
  const seq = ++toneSeq
  if (!src || !import.meta.client) { localTone.value = null; return }
  sampleImageTone(src).then((tone) => {
    if (seq === toneSeq) localTone.value = tone
  })
}, { immediate: true })

const { toneFollowEnabled } = useAppearance()
const toneStyle = computed(() => {
  if (!toneFollowEnabled.value) return undefined
  const tone = bgTone.value || localTone.value
  return tone ? toneStyleVars(tone) : undefined
})

function mixTrackToTrack(track: MixTrack): Track {
  return {
    id: track.track_id,
    title: track.track_title,
    artist: track.artist_name,
    album: track.album_title,
    duration: track.duration,
    stream_url: `/api/music/tracks/${track.track_id}/stream`,
    album_id: track.album_id,
    artist_id: track.artist_id,
    artist_slug: track.artist_slug,
    album_slug: track.album_slug,
    poster: useAlbumCoverUrl(track.artist_slug, track.album_slug) ?? undefined,
    source: 'mix',
  }
}

async function playAll(shuffle: boolean) {
  if (!mix.value?.tracks.length) return
  let tracks = mix.value.tracks.map(mixTrackToTrack)
  if (shuffle) tracks = [...tracks].sort(() => Math.random() - 0.5)
  await playTracks(tracks)
}

async function playFrom(index: number) {
  if (!mix.value?.tracks.length) return
  const tracks = mix.value.tracks.map(mixTrackToTrack)
  await playTracks(tracks, tracks[index])
}

const columns: TrackListColumn[] = [
  { key: 'idx', kind: 'index', label: '#', width: '40px' },
  { key: 'art', kind: 'art', width: '48px' },
  { key: 'title', kind: 'title', subtitle: 'artist-link', label: 'Title', width: 'minmax(220px, 1fr)', sortable: true },
  { key: 'album', kind: 'album', label: 'Album', width: 'minmax(180px, 1fr)', optional: true, defaultOn: true, sortable: true },
  { key: 'plays', kind: 'meta', label: 'Plays', width: '76px', optional: true, defaultOn: true, format: row => (row.play_count ?? 0).toLocaleString(), sortable: true, sortValue: row => row.play_count ?? 0 },
  { key: 'duration', kind: 'duration', headerIcon: 'clock', width: '64px', sortable: true },
]

const tlRows = computed<TrackListRow[]>(() => (mix.value?.tracks ?? []).map(track => ({
  id: track.track_id,
  title: track.track_title,
  artist: track.artist_name,
  artist_slug: track.artist_slug,
  album: track.album_title,
  album_slug: track.album_slug,
  album_year: track.album_year,
  duration: track.duration,
  poster: useAlbumCoverUrl(track.artist_slug, track.album_slug),
  play_count: track.play_count,
})))

function contextItemsFor(_row: TrackListRow, index: number) {
  const track = mix.value!.tracks[index]!
  return actions.forTrack({
    id: track.track_id,
    title: track.track_title,
    artist: track.artist_name,
    album: track.album_title,
    duration: track.duration,
    album_id: track.album_id,
    artist_id: track.artist_id,
    artist_slug: track.artist_slug,
    album_slug: track.album_slug,
  })
}
</script>

<style scoped>
.mix-page { padding-bottom: 0; }
.m-loading, .m-empty { color: var(--fg-3); font-size: 14px; padding-top: 32px; }

.mix-seed-link { color: inherit; text-decoration: none; font-weight: 600; transition: color 0.15s; }
.mix-seed-link:hover { color: var(--tone, var(--gold)); }
.dot { opacity: 0.45; }

:deep(.tl-body) { gap: 2px; }
:deep(.tl-c-art) { width: 44px; height: 44px; }
:deep(.tl-c-index) { font-size: 11px; }
:deep(.tl-c-duration) { font-size: 11px; letter-spacing: 0.04em; }
:deep(.tl-track.tl-active .tl-c-index) { color: var(--fg-3); }
</style>
