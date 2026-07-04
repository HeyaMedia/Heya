<template>
  <div v-if="loading" class="page-pad m-loading">Loading…</div>
  <div v-else-if="!detail" class="page-pad m-empty">Playlist not found.</div>
  <div v-else class="pl-page">
    <header class="pl-hero">
      <div class="pl-hero-bg" :style="coverStyle" />
      <div class="pl-hero-fade" />
      <div class="pl-hero-content">
        <div class="pl-hero-art">
          <NuxtImg v-if="coverUrl" :src="coverUrl" :width="400" :quality="80" :alt="`${pl.name} cover`" />
          <Icon v-else name="heart" :size="48" />
        </div>
        <div class="pl-hero-meta">
          <div class="m-kind">Playlist</div>
          <h1 class="m-title">{{ pl.name }}</h1>
          <p v-if="pl.description" class="m-sub">{{ pl.description }}</p>
          <div class="pl-hero-stats">
            <span>{{ tracks.length }} {{ tracks.length === 1 ? 'track' : 'tracks' }}</span>
            <span v-if="totalDuration > 0" class="dot">·</span>
            <span v-if="totalDuration > 0">{{ formatRunTime(totalDuration) }}</span>
          </div>
          <div class="m-actions">
            <button class="btn btn-primary" :disabled="!tracks.length" @click="playAll(false)">
              <Icon name="play" :size="16" /> Play
            </button>
            <button class="btn" :disabled="!tracks.length" @click="playAll(true)">
              <Icon name="shuffle" :size="16" /> Shuffle
            </button>
            <button class="btn btn-ghost" @click="onDelete" title="Delete playlist">
              <Icon name="close" :size="14" />
            </button>
          </div>
        </div>
      </div>
    </header>

    <section v-if="!tracks.length" class="page-pad m-empty-state">
      <Icon name="music" :size="40" class="m-empty-icon" />
      <h3>This playlist is empty</h3>
      <p>Open any track's context menu (right-click) or use the "Add to playlist" action to add songs.</p>
    </section>

    <section v-else class="page-pad pl-tracks">
      <div class="list-rows">
        <div class="list-row list-row-head pl-cols">
          <div>#</div><div>Title</div><div>Album</div><div>Added</div><div></div><div style="text-align: right">Duration</div>
        </div>
        <RecycleScroller
          :items="tracks"
          :item-size="62"
          key-field="track_id"
          page-mode
          v-slot="{ item: t, index: i }"
        >
          <div class="list-row pl-cols" :class="{ 'pl-missing': t.available === false }" @click="t.available !== false && playFrom(i)">
            <div class="pl-num mono">{{ i + 1 }}</div>
            <div class="pl-title-cell">
              <VuMeter v-if="currentTrack?.id === t.track_id" :playing="playing" />
              <Poster v-else :idx="t.track_id" :src="useAlbumCoverUrl(t.artist_slug, t.album_slug)" aspect="1/1" class="pl-thumb" :class="{ 'poster--missing': t.available === false }" />
              <div class="pl-title-text">
                <div class="pl-title" :style="currentTrack?.id === t.track_id ? { color: 'var(--gold)' } : {}">
                  {{ t.track_title }}
                  <Icon v-if="t.available === false" name="trash" :size="11" class="pl-missing-icon" />
                </div>
                <div class="pl-artist">{{ t.artist_name }}</div>
              </div>
            </div>
            <div class="pl-album">
              <NuxtLink :to="`/music/artist/${t.artist_slug}/${t.album_slug}`" class="pl-album-link" @click.stop>{{ t.album_title }}</NuxtLink>
            </div>
            <div class="pl-added mono">{{ formatDate(t.added_at) }}</div>
            <button class="pl-remove" @click.stop="removeRow(t.track_id)" title="Remove from playlist">
              <Icon name="close" :size="14" />
            </button>
            <div class="pl-dur mono">{{ formatTime(t.duration) }}</div>
          </div>
        </RecycleScroller>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import { useQuery, useQueryClient } from '@tanstack/vue-query'

definePageMeta({ layout: 'default' })

interface PlaylistTrackRow {
  track_id: number
  track_title: string
  duration: number
  disc_number: number
  track_number: number
  album_id: number
  album_title: string
  album_cover_path: string
  album_year: string
  album_slug: string
  artist_id: number
  artist_name: string
  artist_slug: string
  position: number
  added_at: string
  available?: boolean
}
interface PlaylistDetailResponse {
  playlist: {
    id: number
    user_id: number
    name: string
    description: string
    cover_path: string
    created_at: string
    updated_at: string
  }
  tracks: PlaylistTrackRow[]
}

const route = useRoute()
const router = useRouter()
const playlistId = computed(() => Number(route.params.id))

const { play, queue, currentTrack, playing, formatTime } = usePlayer()
const playlists = usePlaylists()
const queryClient = useQueryClient()
const { $heya } = useNuxtApp()

// Keyed by playlist id so each playlist gets its own cache slot. Reactive
// key (playlistId) automatically triggers a refetch when navigating
// between playlists via URL.
const detailQuery = useQuery({
  queryKey: ['music', 'playlist', playlistId],
  queryFn: async () => await $heya('/api/me/playlists/{id}', { path: { id: playlistId.value } }) as unknown as PlaylistDetailResponse,
  staleTime: 1000 * 30,
})
const loading = computed(() => detailQuery.isPending.value)
const detail = computed<PlaylistDetailResponse | null>(() => detailQuery.data.value ?? null)

const pl = computed(() => detail.value!.playlist)
const tracks = computed(() => detail.value?.tracks ?? [])
const totalDuration = computed(() => tracks.value.reduce((s, t) => s + (t.duration || 0), 0))
const coverUrl = computed(() => {
  // Playlist's own cover_path is a user-uploaded image URL (kept as-is) —
  // synthesize from the first track's album cover otherwise so the hero
  // isn't blank for fresh playlists.
  if (pl.value.cover_path) return pl.value.cover_path
  const first = tracks.value[0]
  return first ? useAlbumCoverUrl(first.artist_slug, first.album_slug) : null
})
const coverStyle = computed(() => coverUrl.value ? { backgroundImage: `url(${coverUrl.value})` } : {})

function toPlayable(row: PlaylistTrackRow): Track {
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
    available: row.available,
  }
}

function isPlayable(row: PlaylistTrackRow) { return row.available !== false }

async function playAll(shuffle: boolean) {
  let pl = tracks.value.filter(isPlayable).map(toPlayable)
  if (shuffle) pl = [...pl].sort(() => Math.random() - 0.5)
  if (!pl.length) return
  queue.value = pl
  await play(pl[0])
}

async function playFrom(idx: number) {
  const target = tracks.value[idx]
  if (!target || !isPlayable(target)) return
  queue.value = tracks.value.filter(isPlayable).map(toPlayable)
  await play(toPlayable(target))
}

async function removeRow(trackId: number) {
  await playlists.removeTrack(playlistId.value, trackId)
  // Optimistic update via the query cache so the row disappears immediately
  // while the server reflects the same change. Avoids a refetch round-trip.
  queryClient.setQueryData(['music', 'playlist', playlistId.value], (prev: PlaylistDetailResponse | undefined) => {
    if (!prev) return prev
    return { ...prev, tracks: prev.tracks.filter(t => t.track_id !== trackId) }
  })
  // Recent Playlists shelf on the home page derives last-played from
  // playlist tracks; invalidate so a possible last-played change shows up.
  queryClient.invalidateQueries({ queryKey: ['music', 'home', 'recent-playlists'] })
}

async function onDelete() {
  if (!detail.value) return
  const ok = await useConfirm().confirm({
    title: `Delete "${detail.value.playlist.name}"?`,
    message: "This can't be undone.",
    confirmLabel: 'Delete',
    destructive: true,
  })
  if (!ok) return
  await playlists.remove(playlistId.value)
  router.push('/music')
}

function formatDate(iso: string) {
  if (!iso) return ''
  try {
    return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
  } catch { return '' }
}
</script>

<style scoped>
.pl-page { padding-bottom: 80px; }
.m-loading, .m-empty { color: var(--fg-3); padding: 32px 40px; font-size: 13px; }

.pl-hero {
  position: relative;
  min-height: 280px;
  display: flex;
  align-items: flex-end;
  overflow: hidden;
  border-radius: 0 0 var(--r-md) var(--r-md);
}
.pl-hero-bg {
  position: absolute; inset: 0;
  background-size: cover;
  background-position: center;
  filter: blur(60px) brightness(0.45) saturate(2.2);
  transform: scale(1.4);
  z-index: 0;
  background-color: #2a1f3b;
}
.pl-hero-fade {
  position: absolute; inset: 0;
  background:
    linear-gradient(135deg, rgba(230, 70, 130, 0.35), rgba(120, 50, 200, 0.35) 60%, rgba(0,0,0,0.25)),
    linear-gradient(180deg, transparent 0%, rgba(0,0,0,0.3) 60%, var(--bg-0) 100%);
  z-index: 1;
}
.pl-hero-content {
  position: relative; z-index: 2;
  display: flex; align-items: flex-end; gap: 28px;
  padding: 32px 40px;
  width: 100%;
}
.pl-hero-art {
  width: 200px; height: 200px;
  border-radius: var(--r-md);
  background: linear-gradient(135deg, #e64682, #7832c8);
  display: flex; align-items: center; justify-content: center;
  color: rgba(255,255,255,0.9);
  box-shadow: 0 24px 48px rgba(0,0,0,0.6), 0 0 0 1px rgba(255,255,255,0.05);
  flex-shrink: 0;
  overflow: hidden;
}
.pl-hero-art img { width: 100%; height: 100%; object-fit: cover; }
.pl-hero-meta { flex: 1; min-width: 0; }
.m-kind {
  font-size: 11px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.12em;
  color: var(--fg-2); margin-bottom: 6px;
}
.m-title {
  font-size: clamp(36px, 4.5vw, 60px);
  font-weight: 800;
  line-height: 1.02;
  margin-bottom: 8px;
  color: var(--fg-0);
  letter-spacing: -0.02em;
}
.m-sub { color: var(--fg-1); margin-bottom: 12px; max-width: 64ch; font-size: 14px; }
.pl-hero-stats {
  display: flex; align-items: center; gap: 8px;
  font-size: 12px; color: var(--fg-2);
  font-family: var(--font-mono);
  margin-bottom: 16px;
}
.dot { color: var(--fg-3); }
.m-actions { display: flex; gap: 10px; align-items: center; }
.m-actions :deep(.btn-primary) {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 0 20px; height: 40px;
  border-radius: 999px; font-weight: 600;
}
.btn-ghost {
  background: transparent;
  border: 1px solid rgba(255,255,255,0.12);
  color: var(--fg-2);
  width: 40px; height: 40px;
  border-radius: 50%;
  display: inline-flex; align-items: center; justify-content: center;
}
.btn-ghost:hover { background: rgba(255,255,255,0.06); color: var(--fg-0); }

.pl-tracks { padding-top: 24px; }
.pl-cols { grid-template-columns: 40px 2fr 1.2fr 100px 36px 70px !important; }
.pl-num { text-align: right; color: var(--fg-3); }
.pl-title-cell { display: flex; align-items: center; gap: 12px; min-width: 0; }
.pl-thumb { width: 40px; height: 40px; border-radius: 4px; flex-shrink: 0; }
.pl-title-text { min-width: 0; }
.pl-title { font-size: 14px; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
.pl-missing { opacity: 0.5; cursor: default; }
.pl-missing-icon { color: #d96b6b; vertical-align: -1px; margin-left: 4px; }
.pl-artist { font-size: 12px; color: var(--fg-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pl-album { font-size: 13px; color: var(--fg-2); overflow: hidden; }
.pl-album-link { color: inherit; text-decoration: none; }
.pl-album-link:hover { color: var(--gold); }
.pl-added { font-size: 11px; color: var(--fg-3); }
.pl-dur { font-size: 12px; color: var(--fg-3); text-align: right; }
.pl-remove {
  background: transparent;
  border: 0;
  color: var(--fg-3);
  padding: 4px;
  cursor: pointer;
  border-radius: var(--r-sm);
  opacity: 0;
  transition: opacity 0.15s, color 0.15s, background 0.15s;
}
.list-row:hover .pl-remove { opacity: 1; }
.pl-remove:hover { background: rgba(255,255,255,0.06); color: var(--fg-0); }
.mono { font-family: var(--font-mono); }

.m-empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 80px 24px;
  color: var(--fg-2);
}
.m-empty-icon { color: var(--fg-3); margin-bottom: 16px; }
.m-empty-state h3 { font-size: 18px; font-weight: 600; color: var(--fg-1); margin-bottom: 8px; }
.m-empty-state p { font-size: 13px; max-width: 50ch; color: var(--fg-2); }
</style>
