<script setup lang="ts">
import type { MusicAlbumDetail, TrackFile, TrackView } from '~~/shared/types'
// playFromFile picks an explicit file for the track (e.g. the FLAC over the
// MP3 fallback) and routes the player through the bit-perfect endpoint.
import type { Track } from '~/composables/usePlayer'

definePageMeta({ layout: 'default' })

const route = useRoute()
const artistSlug = computed(() => route.params.slug as string)
const albumSlug = computed(() => route.params.album as string)

const detail = ref<MusicAlbumDetail | null>(null)
const loading = ref(true)

const { play, queue, currentTrack, playing, formatTime } = usePlayer()
const loved = useLovedTracks()
const lovedAlbum = useLovedEntity('album')
if (import.meta.client) {
  loved.ensureLoaded()
  lovedAlbum.ensureLoaded()
}

async function load() {
  loading.value = true
  try {
    const { $heya } = useNuxtApp()
    detail.value = await $heya('/api/music/artists/{artist_slug}/albums/{album_slug}', {
      path: { artist_slug: artistSlug.value, album_slug: albumSlug.value },
    }) as MusicAlbumDetail
  } catch {
    detail.value = null
  } finally {
    loading.value = false
  }
}

watch([artistSlug, albumSlug], load, { immediate: true })

// Refresh on media.updated for this album's parent artist (album loudness
// finishing also goes through media.updated with the artist's media_item_id).
if (import.meta.client) {
  const bus = useEventBus()
  bus.connect()
  const off = bus.on('media.updated', (e) => {
    const payload = e.payload as { media_item_id?: number } | undefined
    if (payload && detail.value && payload.media_item_id === detail.value.media_item_id) {
      load()
    }
  })
  onBeforeUnmount(() => { off() })
}

const album = computed(() => detail.value?.album)
const tracks = computed<TrackView[]>(() => {
  if (!detail.value) return []
  return [...detail.value.tracks].sort((a, b) => {
    if (a.disc_number !== b.disc_number) return a.disc_number - b.disc_number
    return a.track_number - b.track_number
  })
})
const artistName = computed(() => detail.value?.artist?.name ?? '')

const totalDuration = computed(() => tracks.value.reduce((s, t) => s + (t.duration || 0), 0))
const hasMultipleDiscs = computed(() => {
  const seen = new Set<number>()
  for (const t of tracks.value) seen.add(t.disc_number)
  return seen.size > 1
})

const coverUrl = computed(() => useAlbumCoverUrl(album.value?.id))

const albumExternalIds = computed<Record<string, string>>(() => {
  // Albums carry just musicbrainz_id today. Synthesize the map so the
  // ExternalLinks component can render the MB chip without special-casing
  // the album shape.
  const ids: Record<string, string> = {}
  if (album.value?.musicbrainz_id) ids.mbid = album.value.musicbrainz_id
  return ids
})
const backdropStyle = computed(() => {
  if (!detail.value) return {}
  return { backgroundImage: `url(/api/media/${detail.value.media_item_id}/image/backdrop)` }
})

function trackToPlayable(t: TrackView): Track {
  const primary = t.files[0]
  return {
    id: t.id,
    title: t.title,
    artist: artistName.value,
    album: album.value?.title ?? '',
    duration: t.duration,
    stream_url: `/api/tracks/${t.id}/stream`,
    album_id: album.value?.id,
    artist_id: detail.value?.artist?.id,
    poster: useAlbumCoverUrl(album.value?.id) ?? undefined,
    integrated_lufs: primary?.integrated_lufs != null ? parseFloat(primary.integrated_lufs) : null,
    true_peak_db: primary?.true_peak_db != null ? parseFloat(primary.true_peak_db) : null,
  }
}

async function playAll(shuffle: boolean) {
  let pl = tracks.value.map(trackToPlayable)
  if (shuffle) pl = [...pl].sort(() => Math.random() - 0.5)
  if (!pl.length) return
  queue.value = pl
  await play(pl[0])
}

async function playFrom(track: TrackView) {
  queue.value = tracks.value.map(trackToPlayable)
  await play(trackToPlayable(track))
}

async function playFromFile(track: TrackView, file: TrackFile) {
  // Queue stays album-ordered; only the chosen track switches to the
  // explicit file URL. Other tracks fall back to /stream picking primary.
  const playable = trackToPlayable(track)
  playable.stream_url = `/api/tracks/${track.id}/file/${file.id}`
  playable.track_file_id = file.id
  playable.integrated_lufs = file.integrated_lufs != null ? parseFloat(file.integrated_lufs) : null
  playable.true_peak_db = file.true_peak_db != null ? parseFloat(file.true_peak_db) : null
  queue.value = tracks.value.map((t) => (t.id === track.id ? playable : trackToPlayable(t)))
  await play(playable)
}

function queueAll() {
  queue.value = [...queue.value, ...tracks.value.map(trackToPlayable)]
}

function primaryFile(t: TrackView): TrackFile | null { return t.files[0] ?? null }

function formatRunTime(seconds: number) {
  if (seconds < 3600) return formatTime(seconds)
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  return `${h}h ${m}m`
}

function isDiscBoundary(idx: number) {
  if (idx === 0) return false
  const prev = tracks.value[idx - 1]
  const curr = tracks.value[idx]
  return !!prev && !!curr && prev.disc_number !== curr.disc_number
}
</script>

<template>
  <div v-if="loading" class="page-pad m-loading">Loading…</div>
  <div v-else-if="!album" class="page-pad m-empty">Album not found.</div>
  <div v-else class="album-page">
    <section class="hero">
      <div class="hero-backdrop" :style="backdropStyle" />
      <div class="hero-fade" />
      <div class="hero-content">
        <Poster :idx="album.id" :src="coverUrl" aspect="1/1" class="hero-cover" />
        <div class="hero-meta">
          <div class="hero-kind">{{ (album.album_type || 'album').toUpperCase() }}</div>
          <h1 class="hero-title">{{ album.title }}</h1>
          <div class="hero-sub">
            <NuxtLink :to="`/music/artist/${artistSlug}`" class="artist-link">{{ artistName }}</NuxtLink>
            <span v-if="album.year" class="dot">·</span>
            <span v-if="album.year">{{ album.year }}</span>
            <span class="dot">·</span>
            <span>{{ tracks.length }} tracks</span>
            <span v-if="totalDuration > 0" class="dot">·</span>
            <span v-if="totalDuration > 0">{{ formatRunTime(totalDuration) }}</span>
          </div>
          <ExternalLinks
            kind="album"
            :external-ids="albumExternalIds"
          />
        </div>
      </div>
      <!-- Floating round actions -->
      <div class="hero-floating-actions">
        <button class="hero-round hero-round-primary" @click="playAll(false)" title="Play">
          <Icon name="play" :size="22" />
        </button>
        <button class="hero-round" @click="playAll(true)" title="Shuffle">
          <Icon name="shuffle" :size="18" />
        </button>
        <button
          class="hero-round"
          :class="{ active: lovedAlbum.isLoved(album.id) }"
          @click="lovedAlbum.toggle(album.id)"
          :title="lovedAlbum.isLoved(album.id) ? 'Remove from My Albums' : 'Add to My Albums'"
        >
          <Icon :name="lovedAlbum.isLoved(album.id) ? 'heartfill' : 'heart'" :size="18" />
        </button>
        <button class="hero-round" @click="queueAll" title="Add to queue">
          <Icon name="plus" :size="18" />
        </button>
      </div>
    </section>

    <section class="tracklist page-pad">
      <div class="list-rows">
        <div class="list-row list-row-head tl-cols">
          <div>#</div>
          <div>Title</div>
          <div v-if="!hasMultipleDiscs" />
          <div v-else>Disc</div>
          <div style="text-align: right">Duration</div>
        </div>
        <template v-for="(t, i) in tracks" :key="t.id">
          <div class="disc-marker" v-if="hasMultipleDiscs && isDiscBoundary(i)">Disc {{ t.disc_number }}</div>
          <div class="list-row tl-cols" @click="playFrom(t)">
            <div class="tl-num mono">{{ t.track_number || i + 1 }}</div>
            <div class="tl-title-cell">
              <VuMeter v-if="currentTrack?.id === t.id" :playing="playing" />
              <div class="tl-text">
                <div class="tl-title" :style="currentTrack?.id === t.id ? { color: 'var(--gold)' } : {}">{{ t.title }}</div>
                <div v-if="t.files.length" class="tl-format-row" @click.stop>
                  <TrackQualityPicker
                    :files="t.files"
                    :selected-id="primaryFile(t)?.id"
                    @pick="playFromFile(t, $event)"
                  />
                </div>
              </div>
            </div>
            <button
              class="tl-love"
              :class="{ active: loved.isLoved(t.id) }"
              @click.stop="loved.toggle(t.id)"
              :title="loved.isLoved(t.id) ? 'Remove from Loved' : 'Add to Loved'"
            >
              <Icon :name="loved.isLoved(t.id) ? 'heartfill' : 'heart'" :size="14" />
            </button>
            <div class="tl-disc mono">{{ hasMultipleDiscs ? t.disc_number : '' }}</div>
            <div class="tl-dur mono">{{ formatTime(t.duration) }}</div>
          </div>
        </template>
      </div>
    </section>
  </div>
</template>

<style scoped>
.album-page { padding-bottom: 80px; }

.hero {
  position: relative;
  min-height: 300px;
  display: flex;
  align-items: flex-end;
  overflow: hidden;
  border-radius: 0 0 var(--r-md) var(--r-md);
}
.hero-backdrop {
  position: absolute; inset: 0;
  background-size: cover;
  background-position: center 30%;
  filter: blur(60px) brightness(0.45) saturate(2.2);
  transform: scale(1.4);
  z-index: 0;
}
.hero-fade {
  position: absolute; inset: 0;
  background:
    linear-gradient(180deg, transparent 0%, rgba(0,0,0,0.25) 60%, var(--bg-0) 100%);
  z-index: 1;
}
.hero-content {
  position: relative; z-index: 2;
  display: flex; align-items: flex-end; gap: 28px;
  padding: 32px 40px; width: 100%;
}
.hero-cover {
  width: 220px; height: 220px;
  border-radius: var(--r-md);
  box-shadow: 0 24px 48px rgba(0,0,0,0.6), 0 0 0 1px rgba(255,255,255,0.05);
  flex-shrink: 0;
}
.hero-meta { flex: 1; min-width: 0; }
.hero-kind {
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.12em;
  color: var(--fg-2);
  margin-bottom: 6px;
}
.hero-title {
  font-size: clamp(36px, 4.5vw, 56px);
  font-weight: 800;
  line-height: 1.02;
  margin-bottom: 8px;
  color: var(--fg-0);
  letter-spacing: -0.02em;
}
.hero-sub { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg-2); margin-bottom: 18px; font-family: var(--font-mono); }
.dot { color: var(--fg-3); }
.artist-link { color: var(--fg-1); text-decoration: none; font-weight: 500; font-family: var(--font-sans, inherit); }
.artist-link:hover { color: var(--gold); }
.hero-floating-actions {
  position: absolute;
  bottom: 28px;
  right: 32px;
  z-index: 3;
  display: flex;
  align-items: center;
  gap: 10px;
}
.hero-round {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  border: 1px solid rgba(255,255,255,0.12);
  background: rgba(0,0,0,0.4);
  color: var(--fg-0);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  backdrop-filter: blur(8px);
  transition: background 0.15s, transform 0.1s, color 0.15s;
}
.hero-round:hover { background: rgba(0,0,0,0.55); transform: scale(1.05); }
.hero-round:active { transform: scale(0.95); }
.hero-round.active { color: var(--gold); }
.hero-round-primary {
  width: 64px;
  height: 64px;
  background: var(--gold);
  color: var(--bg-0);
  border-color: transparent;
  box-shadow: 0 10px 24px var(--gold-glow);
}
.hero-round-primary:hover { background: var(--gold-bright); }

.tracklist { padding-top: 24px; }
.tl-cols { grid-template-columns: 48px 1fr 36px 80px 80px !important; }
.tl-love {
  background: transparent;
  border: 0;
  padding: 6px;
  border-radius: var(--r-sm);
  color: var(--fg-3);
  cursor: pointer;
  transition: color 0.15s, background 0.15s;
}
.tl-love:hover { background: rgba(255,255,255,0.06); color: var(--fg-1); }
.tl-love.active { color: var(--gold); }
.tl-num { text-align: right; color: var(--fg-3); }
.tl-title-cell { display: flex; align-items: center; gap: 12px; min-width: 0; }
.tl-text { min-width: 0; }
.tl-title { font-size: 14px; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tl-format-row { margin-top: 2px; }
.tl-disc { text-align: center; color: var(--fg-3); }
.tl-dur { text-align: right; color: var(--fg-3); }
.mono { font-family: var(--font-mono); }

.disc-marker {
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-2);
  padding: 18px 12px 6px;
  border-bottom: 1px solid var(--border);
  margin-bottom: 4px;
}
</style>
