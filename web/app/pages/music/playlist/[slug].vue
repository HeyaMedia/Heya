<template>
  <div v-if="loading" class="page-pad m-loading">Loading…</div>
  <div v-else-if="!detail" class="page-pad">
    <MusicEmptyState icon="music" title="Playlist not found">
      It may have been deleted or renamed. Your playlists live in the
      <NuxtLink to="/music">sidebar</NuxtLink>.
    </MusicEmptyState>
  </div>
  <div v-else class="pl-page">
    <!-- Ambient-extended (house hero convention, same as artist/movie/TV):
         with ambient backdrops ON, the layer behind the app rotates through
         this playlist's artists and the hero paints NOTHING of its own — a
         local band would seam against the full-page art. Only with ambient
         OFF does the hero paint its blurred-cover backdrop inside itself. -->
    <header class="pl-hero" :class="{ 'ambient-extended': ambientEnabled }">
      <div class="pl-hero-bg" :style="coverStyle" />
      <div class="pl-hero-fade" />
      <div class="pl-hero-content">
        <!-- Cover: the user's uploaded image when set; otherwise a collage
             built from the playlist's own albums (MixCollage dedupes to 4,
             falls back to the first album cover, then the icon tile). -->
        <div class="pl-hero-art">
          <NuxtImg v-if="customCoverUrl" :src="customCoverUrl" :width="400" :quality="85" :alt="`${pl.name} cover`" />
          <MixCollage v-else-if="tracks.length" :tracks="tracks" :seed-src="firstAlbumCover" :alt="`${pl.name} cover`" class="pl-hero-collage" />
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
            <button class="btn btn-primary" :style="heroToneStyle" :disabled="!tracks.length" @click="playAll(false)">
              <Icon name="play" :size="16" /> Play
            </button>
            <button class="btn" :disabled="!tracks.length" @click="playAll(true)">
              <Icon name="shuffle" :size="16" /> Shuffle
            </button>
            <AppMenu trigger-class="btn-ghost pl-more" trigger-title="Playlist options" trigger-aria-label="Playlist options">
              <template #trigger><Icon name="more" :size="16" /></template>
              <DropdownMenuItem class="surface-item" @select="openEdit">
                <Icon name="pencil" :size="14" class="surface-item-icon" /> Edit details…
              </DropdownMenuItem>
              <DropdownMenuItem class="surface-item" @select="coverInput?.click()">
                <Icon name="image" :size="14" class="surface-item-icon" /> {{ hasCover ? 'Replace cover…' : 'Set custom cover…' }}
              </DropdownMenuItem>
              <DropdownMenuItem v-if="hasCover" class="surface-item" @select="removeCover">
                <Icon name="undo" :size="14" class="surface-item-icon" /> Use generated cover
              </DropdownMenuItem>
              <div class="surface-divider" />
              <DropdownMenuItem class="surface-item surface-item-destructive" @select="onDelete">
                <Icon name="trash" :size="14" class="surface-item-icon" /> Delete playlist
              </DropdownMenuItem>
            </AppMenu>
            <!-- Hidden picker for the custom cover (same raw-multipart flow
                 as the metadata editor's artwork upload). -->
            <input ref="coverInput" type="file" accept="image/*" class="pl-cover-input" @change="onCoverPicked" />
          </div>
        </div>
      </div>
    </header>

    <section v-if="!tracks.length" class="page-pad">
      <MusicEmptyState icon="music" title="This playlist is empty" compact>
        Right-click any track (long-press on touch) and pick
        <strong>Add to playlist</strong> — from <NuxtLink to="/music/songs">All Songs</NuxtLink>,
        an album, or search.
      </MusicEmptyState>
    </section>

    <!-- Desktop keeps the virtualized RecycleScroller table untouched — see
         script comment for why TrackList only takes over at phone width. -->
    <section v-else-if="!isPhone" class="page-pad pl-tracks">
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
          <div
            class="list-row pl-cols"
            :class="{ 'pl-missing': t.available === false }"
            :draggable="!isCoarse"
            :role="t.available === false ? undefined : 'button'"
            :tabindex="t.available === false ? -1 : 0"
            :aria-label="t.available === false ? undefined : `Play ${t.track_title}`"
            @click="t.available !== false && playFrom(i)"
            @keydown="onRowKeydown($event, i, t)"
            @dragstart="onDragStart($event, { kind: 'track', track: { id: t.track_id, title: t.track_title } })"
            @dragend="onDragEnd"
          >
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
            <button class="pl-remove" @click.stop="removeRow(t.track_id)" title="Remove from playlist" aria-label="Remove from playlist">
              <Icon name="close" :size="14" />
            </button>
            <div class="pl-dur mono">{{ formatTime(t.duration) }}</div>
          </div>
        </RecycleScroller>
      </div>
    </section>

    <section v-else class="page-pad pl-tracks">
      <TrackList
        :tracks="tlRows"
        :columns="columns"
        grid-template-columns="40px 2fr 1.2fr 100px 36px 70px"
        :show-header="false"
        :context-items="contextItemsFor"
        :active-track-id="currentTrack?.id ?? null"
        :duration-formatter="formatTime"
        @row-click="playFrom"
      />
    </section>

    <!-- Edit details — rename regenerates the slug server-side; we re-route
         to the new URL after saving so the address always mirrors the name. -->
    <AppDialog v-model="editOpen" title="Edit playlist" size="sm">
      <div class="pl-edit-form">
        <label class="pl-edit-label" for="pl-edit-name">Name</label>
        <input id="pl-edit-name" v-model="editName" type="text" class="pl-edit-input" maxlength="200" @keydown.enter.prevent="saveEdit" />
        <label class="pl-edit-label" for="pl-edit-desc">Description</label>
        <textarea id="pl-edit-desc" v-model="editDescription" class="pl-edit-input pl-edit-desc" rows="3" maxlength="1000" placeholder="Optional" />
        <div class="pl-sync-heading">Playlist sync</div>
        <div class="pl-sync-service">
          <div>
            <strong>ListenBrainz</strong>
            <span v-if="listenBrainzConnected">{{ listenBrainzSync ? (listenBrainzSync.last_error || 'Two-way sync is active') : 'Keep this playlist synchronized in both directions' }}</span>
            <span v-else>Connect ListenBrainz in Settings → Music services first</span>
          </div>
          <div class="pl-sync-actions">
            <button v-if="listenBrainzSync" class="btn btn-sm" :disabled="syncBusy" @click="syncNow('listenbrainz')">Sync now</button>
            <AppSwitch
              :model-value="!!listenBrainzSync"
              :disabled="!listenBrainzConnected || syncBusy"
              size="md"
              aria-label="Synchronize this playlist with ListenBrainz"
              @update:model-value="togglePlaylistSync('listenbrainz', $event)"
            />
          </div>
        </div>
        <div class="pl-sync-service unavailable">
          <div>
            <strong>Last.fm</strong>
            <span>Last.fm retired its playlist API, so playlist synchronization is unavailable.</span>
          </div>
        </div>
      </div>
      <template #footer>
        <button class="btn" @click="editOpen = false">Cancel</button>
        <button class="btn btn-primary" :disabled="!editName.trim() || saving" @click="saveEdit">
          {{ saving ? 'Saving…' : 'Save' }}
        </button>
      </template>
    </AppDialog>
  </div>
</template>

<script setup lang="ts">
import type { Track } from '~/composables/usePlayer'
import type { TrackListColumn, TrackListRow } from '~/components/music/TrackList.vue'
import type { ContextMenuItem } from '~~/shared/types'
import { DropdownMenuItem } from 'reka-ui'
import { useQuery, useQueryCache } from '@pinia/colada'
import { playlistDetailQuery, type PlaylistDetailResponse, type PlaylistTrackRow } from '~/queries/music'
import { musicServicesQuery } from '~/queries/settings'

definePageMeta({ layout: 'default' })

// This page's desktop table drives a virtualized RecycleScroller (playlists
// can run to thousands of tracks) plus a per-row hover "remove" button.
// TrackList has no virtualization and no matching column kind for that
// button — forcing it onto desktop would be a real perf regression for long
// playlists, not just a styling mismatch. So: desktop keeps this page's own
// table untouched, and TrackList only takes over at phone width, where the
// list is short enough in practice and "Remove from Playlist" moves into
// the row's ⋯ menu (see contextItemsFor below).
const { isPhone, isCoarse } = useViewport()
const { onDragStart, onDragEnd } = useMusicDragDrop()

const route = useRoute()
const router = useRouter()
// Slug in canonical URLs; numeric ids still resolve (legacy links, fresh
// creates that only know the id). Everything downstream keys on String(ref).
const playlistRef = computed(() => String(route.params.slug ?? ''))

const { play, queue, currentTrack, playing, formatTime } = usePlayerBindings()
const playlists = usePlaylists()
const queryClient = useQueryCache()
const { $heya } = useNuxtApp()
const { flash } = useFlash()

const detailQuery = useQuery(() => playlistDetailQuery(playlistRef.value))
await waitForQuery(detailQuery)
const loading = computed(() => detailQuery.isPending.value)
const detail = computed<PlaylistDetailResponse | null>(() => detailQuery.data.value ?? null)

const pl = computed(() => detail.value!.playlist)
const playlistId = computed(() => detail.value?.playlist.id ?? 0)
const tracks = computed(() => detail.value?.tracks ?? [])
const musicServices = useQuery(musicServicesQuery())
const listenBrainzConnected = computed(() => musicServices.data.value?.find(s => s.service === 'listenbrainz')?.token_set ?? false)
const listenBrainzSync = computed(() => detail.value?.syncs?.find(s => s.service === 'listenbrainz'))
const syncBusy = ref(false)
const totalDuration = computed(() => tracks.value.reduce((s, t) => s + (t.duration || 0), 0))

// ── Cover ────────────────────────────────────────────────────────────
// coverBust invalidates the <img> URL after an upload — the endpoint path
// never changes, only the bytes behind it.
const coverBust = ref(0)
const hasCover = computed(() => detail.value?.has_cover ?? false)
const customCoverUrl = computed(() =>
  hasCover.value ? `/api/me/playlists/${playlistId.value}/cover?v=${Date.parse(pl.value.updated_at) || 0}-${coverBust.value}` : null,
)
const firstAlbumCover = computed(() => {
  const first = tracks.value[0]
  return first ? useAlbumCoverUrl(first.artist_slug, first.album_slug) : null
})
// The blurred hero backdrop uses whatever the art block shows.
const backdropSrc = computed(() => customCoverUrl.value || firstAlbumCover.value)
const coverStyle = computed(() => backdropSrc.value ? { backgroundImage: `url(${backdropSrc.value})` } : {})

// Tone-follow (same recipe as the artist hero / mixes marquee): the Play
// button wears the cover's sampled palette. Sequence-guarded against a slow
// sample landing after navigation.
const heroToneStyle = ref<Record<string, string> | undefined>()
let toneSeq = 0
watch(backdropSrc, (src) => {
  const seq = ++toneSeq
  heroToneStyle.value = undefined
  if (!src || !import.meta.client) return
  sampleImageTone(src).then((t) => {
    if (seq !== toneSeq) return
    heroToneStyle.value = t ? { background: t.main, color: t.ink } : undefined
  })
}, { immediate: true })

// ── Ambient backdrop — the playlist's artists ────────────────────────
// With ambient on, this page claims the background layer and walks it
// through the distinct artists in the playlist (their portraits — posters
// fall back through media_assets, so they're the reliable image; backdrops
// are spotty for artists). set() replaces the claim in place, so this is
// the pool experience with page-owned content; BG_ROTATE_MS keeps cadence
// identical to the library pools.
const { ambientEnabled } = useAppearance()
const background = useBackground()
const bgTools = useBackgroundImageTools()

const artistArtUrls = computed(() => {
  const seen = new Set<string>()
  const urls: string[] = []
  for (const t of tracks.value) {
    if (!t.artist_slug || seen.has(t.artist_slug)) continue
    seen.add(t.artist_slug)
    urls.push(`/api/media/${t.artist_slug}/image/poster`)
    if (urls.length === 12) break
  }
  return urls
})

let bgTimer: ReturnType<typeof setInterval> | undefined
let bgIdx = 0
watch([artistArtUrls, ambientEnabled], ([urls, on]) => {
  if (bgTimer) { clearInterval(bgTimer); bgTimer = undefined }
  if (!on || !urls.length) { background.clear(); return }
  bgIdx = 0
  background.set(urls[0])
  if (urls.length > 1) {
    bgTools.warm(urls[1]!)
    bgTimer = setInterval(() => {
      bgIdx = (bgIdx + 1) % urls.length
      background.set(urls[bgIdx])
      bgTools.warm(urls[(bgIdx + 1) % urls.length]!)
    }, BG_ROTATE_MS)
  }
}, { immediate: true })
onBeforeUnmount(() => { if (bgTimer) clearInterval(bgTimer) })

const coverInput = ref<HTMLInputElement>()
async function onCoverPicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  await playlists.setCover(playlistId.value, file)
  coverBust.value++
  detailQuery.refetch()
}
async function removeCover() {
  await playlists.clearCover(playlistId.value)
  detailQuery.refetch()
}

// ── Edit details ─────────────────────────────────────────────────────
const editOpen = ref(false)
const editName = ref('')
const editDescription = ref('')
const saving = ref(false)
function openEdit() {
  editName.value = pl.value.name
  editDescription.value = pl.value.description
  editOpen.value = true
}
async function saveEdit() {
  if (!editName.value.trim() || saving.value) return
  saving.value = true
  try {
    const updated = await playlists.update(playlistId.value, {
      name: editName.value.trim(),
      description: editDescription.value.trim(),
    })
    editOpen.value = false
    // Renames regenerate the slug — keep the address bar honest. The param
    // change re-keys detailQuery, which refetches under the new slug.
    if (updated.slug && updated.slug !== playlistRef.value) {
      router.replace(`/music/playlist/${updated.slug}`)
    } else {
      detailQuery.refetch()
    }
  } finally {
    saving.value = false
  }
}

async function togglePlaylistSync(service: 'listenbrainz' | 'lastfm', enabled: boolean) {
  if (syncBusy.value) return
  syncBusy.value = true
  try {
    await $heya('/api/me/playlists/{id}/sync/{service}', {
      method: 'PUT',
      path: { id: playlistId.value, service },
      body: { enabled },
    })
    await detailQuery.refetch()
    flash.value = { kind: 'ok', text: enabled ? 'Playlist sync enabled' : 'Playlist sync disabled' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail || 'Could not update playlist sync' }
  } finally {
    syncBusy.value = false
  }
}

async function syncNow(service: 'listenbrainz' | 'lastfm') {
  if (syncBusy.value) return
  syncBusy.value = true
  try {
    await $heya('/api/me/playlists/{id}/sync/{service}', {
      method: 'POST',
      path: { id: playlistId.value, service },
    })
    await detailQuery.refetch()
    flash.value = { kind: 'ok', text: 'Playlist synchronized' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail || 'Playlist sync failed' }
  } finally {
    syncBusy.value = false
  }
}

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

// Keyboard mirror of the row's @click. Guarded on target===currentTarget so
// Enter/Space on the nested album link or "Remove" doesn't also playFrom.
function onRowKeydown(e: KeyboardEvent, i: number, t: PlaylistTrackRow) {
  if (e.target !== e.currentTarget) return
  if (e.key !== 'Enter' && e.key !== ' ') return
  e.preventDefault()
  if (t.available !== false) playFrom(i)
}

async function playAll(shuffle: boolean) {
  let list = tracks.value.filter(isPlayable).map(toPlayable)
  if (shuffle) list = [...list].sort(() => Math.random() - 0.5)
  if (!list.length) return
  queue.value = list
  await play(list[0])
}

async function playFrom(idx: number) {
  const target = tracks.value[idx]
  if (!target || !isPlayable(target)) return
  queue.value = tracks.value.filter(isPlayable).map(toPlayable)
  await play(toPlayable(target))
}

async function removeRow(trackId: number) {
  await playlists.removeTrack(playlistId.value, trackId)
  // Optimistic update on THIS page's cache entry (keyed by the route ref).
  queryClient.setQueryData(['music', 'playlist', playlistRef.value], (prev: PlaylistDetailResponse | undefined) => {
    if (!prev) return prev
    return { ...prev, tracks: prev.tracks.filter(t => t.track_id !== trackId) }
  })
  queryClient.invalidateQueries({ key: ['music', 'home', 'recent-playlists'] })
}

// Phone-only TrackList render (see script-top comment).
const actions = useMusicActions()

const columns: TrackListColumn[] = [
  { key: 'idx', kind: 'index' },
  { key: 'art', kind: 'art' },
  { key: 'title', kind: 'title', subtitle: 'artist-plain' },
  { key: 'album', kind: 'album' },
  { key: 'duration', kind: 'duration' },
]

const tlRows = computed<TrackListRow[]>(() => tracks.value.map((t) => ({
  id: t.track_id,
  title: t.track_title,
  artist: t.artist_name,
  artist_slug: t.artist_slug,
  album: t.album_title,
  album_slug: t.album_slug,
  duration: t.duration,
  available: t.available,
  poster: useAlbumCoverUrl(t.artist_slug, t.album_slug),
  quality: formatTrackQuality(t),
})))

function contextItemsFor(_row: TrackListRow, i: number): ContextMenuItem[] {
  const t = tracks.value[i]
  if (!t) return []
  const items = actions.forTrack({
    id: t.track_id,
    title: t.track_title,
    artist: t.artist_name,
    album: t.album_title,
    duration: t.duration,
    album_id: t.album_id,
    artist_id: t.artist_id,
    artist_slug: t.artist_slug,
    album_slug: t.album_slug,
    available: t.available,
  })
  return [
    ...items,
    { label: '', separator: true },
    { label: 'Remove from Playlist', icon: 'close', action: () => removeRow(t.track_id) },
  ]
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
.pl-sync-heading { margin-top: 6px; padding-top: 14px; border-top: 1px solid var(--border); color: var(--fg-0); font-size: 12px; font-weight: 650; }
.pl-sync-service { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 10px 11px; border: 1px solid var(--border); border-radius: var(--r-md); background: var(--bg-2); }
.pl-sync-service > div:first-child { min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.pl-sync-service strong { color: var(--fg-0); font-size: 12px; }
.pl-sync-service span { color: var(--fg-3); font-size: 10.5px; line-height: 1.35; }
.pl-sync-service.unavailable { opacity: .62; }
.pl-sync-actions { display: flex; align-items: center; gap: 8px; flex: none; }
.m-loading { color: var(--fg-2); padding: 32px 40px; font-size: 13px; text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1); }

.pl-hero {
  position: relative;
  min-height: 280px;
  display: flex;
  align-items: flex-end;
  overflow: hidden;
  border-radius: 0 0 var(--r-md) var(--r-md);
}

/* Ambient-extended: the layer behind the app owns the art (this playlist's
   artists) — the hero paints nothing, or its local band would seam against
   the continuing full-page artwork. Text flips from on-artwork literals to
   theme tokens + --bg-1 halos because the ambient scrim is theme-aware
   (paper in light mode, where literal white would vanish). */
.pl-hero.ambient-extended { min-height: 0; overflow: visible; }
.pl-hero.ambient-extended .pl-hero-bg,
.pl-hero.ambient-extended .pl-hero-fade { display: none; }
.pl-hero.ambient-extended .m-kind {
  color: var(--fg-2);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
.pl-hero.ambient-extended .m-title {
  color: var(--fg-0);
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1), 0 0 24px var(--bg-1);
}
.pl-hero.ambient-extended .m-sub {
  color: var(--fg-1);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
.pl-hero.ambient-extended .pl-hero-stats {
  color: var(--fg-2);
  text-shadow: 0 0 12px var(--bg-1), 0 1px 3px var(--bg-1);
}
.pl-hero.ambient-extended .pl-hero-stats .dot { color: var(--fg-3); }
.pl-hero-bg {
  position: absolute; inset: 0;
  background-size: cover;
  background-position: center;
  filter: blur(60px) brightness(0.45) saturate(2.2);
  transform: scale(1.4);
  z-index: 0;
  background-color: var(--bg-3); /* fallback fill behind the blurred cover art */
}
.pl-hero-fade {
  position: absolute; inset: 0;
  /* Accent-derived decorative wash + the on-artwork black scrim (stays
     literal per house rule), fading to var(--bg-0) at the page canvas. */
  background:
    linear-gradient(135deg,
      color-mix(in srgb, var(--gold) 30%, transparent),
      color-mix(in srgb, var(--gold-deep, var(--gold)) 16%, transparent) 60%,
      rgba(0, 0, 0, 0.25)),
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
  /* Accent-derived placeholder (same pair as the avatar / Loved tile). */
  background: linear-gradient(135deg, var(--gold-deep, var(--gold)), var(--gold));
  display: flex; align-items: center; justify-content: center;
  color: rgba(255,255,255,0.9); /* icon on the generated placeholder art — stays literal */
  box-shadow: 0 24px 48px rgb(var(--shade) / 0.6), 0 0 0 1px rgb(var(--ink) / 0.05);
  flex-shrink: 0;
  overflow: hidden;
}
.pl-hero-art img { width: 100%; height: 100%; object-fit: cover; }
/* The collage manages its own radius/shadow — flatten inside the frame. */
.pl-hero-collage { width: 100%; height: 100%; border-radius: 0; box-shadow: none; }
.pl-hero-meta { flex: 1; min-width: 0; }
/* Hero backdrop is the cover blurred + darkened in BOTH themes → hero text
   is on-artwork: lock to literal light tones per house rule. */
.m-kind {
  font-size: 11px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.12em;
  color: rgba(255,255,255,0.72); /* on darkened artwork — stays literal */
  margin-bottom: 6px;
}
.m-title {
  font-size: clamp(36px, 4.5vw, 60px);
  font-weight: 800;
  line-height: 1.02;
  margin-bottom: 8px;
  color: #fff; /* on darkened artwork — stays literal */
  text-shadow: 0 2px 24px rgba(0,0,0,0.55); /* on artwork — stays literal */
  letter-spacing: -0.02em;
}
.m-sub {
  color: rgba(255,255,255,0.85); /* on darkened artwork — stays literal */
  margin-bottom: 12px; max-width: 64ch; font-size: 14px;
}
.pl-hero-stats {
  display: flex; align-items: center; gap: 8px;
  font-size: 12px;
  color: rgba(255,255,255,0.75); /* on darkened artwork — stays literal */
  font-family: var(--font-mono);
  margin-bottom: 16px;
}
.pl-hero-stats .dot { color: rgba(255,255,255,0.45); /* on artwork — stays literal */ }
.dot { color: var(--fg-3); }
.m-actions { display: flex; gap: 10px; align-items: center; }
.m-actions :deep(.btn-primary) {
  display: inline-flex; align-items: center; gap: 8px;
  padding: 0 20px; height: 40px;
  border-radius: 999px; font-weight: 600;
  transition: background 0.4s ease, color 0.4s ease, filter 0.15s;
}
.m-actions :deep(.btn-primary:hover) { filter: brightness(1.1); }
.pl-cover-input { display: none; }

.pl-tracks { padding-top: 24px; }
/* Same glass coat as the shared TrackList (.tl) — this page's desktop table
   is a hand-rolled RecycleScroller (virtualization TrackList doesn't have),
   but it sits over the same ambient art, now the playlist's rotating artist
   portraits: bare rows were unreadable over a bright face. Keep the two
   recipes in lockstep with TrackList.vue's. */
.pl-tracks .list-rows {
  background: color-mix(in oklab, var(--bg-2) 76%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-el);
  padding: 4px 10px 8px;
}
.pl-cols {
  grid-template-columns: 40px 2fr 1.2fr 100px 36px 70px !important;
  /* Inside the glass panel now — no halo needed, quiet like .tl-head. */
  color: var(--fg-2);
}
.pl-num { text-align: right; color: var(--fg-3); }
.pl-title-cell { display: flex; align-items: center; gap: 12px; min-width: 0; }
.pl-thumb { width: 40px; height: 40px; border-radius: 4px; flex-shrink: 0; }
.pl-title-text { min-width: 0; }
.pl-title { font-size: 14px; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
.pl-missing { opacity: 0.5; cursor: default; }
.pl-missing-icon { color: var(--bad); vertical-align: -1px; margin-left: 4px; }
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
.list-row:hover .pl-remove,
.pl-remove:focus-visible { opacity: 1; }
.pl-remove:hover { background: rgb(var(--ink) / 0.06); color: var(--fg-0); }
.mono { font-family: var(--font-mono); }

/* Edit dialog */
.pl-edit-form { display: flex; flex-direction: column; gap: 6px; }
.pl-edit-label {
  font-size: 11px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-2);
  margin-top: 8px;
}
.pl-edit-label:first-child { margin-top: 0; }
.pl-edit-input {
  width: 100%;
  padding: 10px 12px;
  background: rgb(var(--shade) / 0.4);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font: inherit;
  font-size: 14px;
  outline: none;
  transition: border-color 0.15s;
}
.pl-edit-input:focus { border-color: var(--gold); }
.pl-edit-desc { resize: vertical; min-height: 70px; line-height: 1.5; }

/* Phone (<=720px): stack the hero, center the cover, wrap the action row. */
@media (max-width: 720px) {
  .pl-hero { min-height: 0; }
  .pl-hero-content {
    flex-direction: column;
    align-items: center;
    text-align: center;
    padding: 24px 20px 20px;
    gap: 14px;
  }
  .pl-hero-art { width: min(55vw, 240px); height: min(55vw, 240px); }
  .pl-hero-meta { width: 100%; }
  .pl-hero-stats { justify-content: center; }
  .m-actions { justify-content: center; flex-wrap: wrap; }
}
</style>

<!-- AppMenu owns its trigger element — scoped selectors don't reach it
     (docs/ui.md gotcha #2), so the round ⋯ button styles live unscoped. -->
<style>
.pl-more {
  background: transparent;
  border: 1px solid rgba(255, 255, 255, 0.12); /* over the hero backdrop — literal */
  color: rgba(255, 255, 255, 0.85); /* on darkened artwork — literal */
  width: 40px;
  height: 40px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.pl-more:hover { background: rgba(255, 255, 255, 0.08); color: #fff; }

/* Ambient-extended: the ⋯ button sits on the theme wash, not a darkened
   hero — swap the literal-white ghost coat for theme glass. */
.pl-hero.ambient-extended .pl-more {
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  border-color: var(--border);
  color: var(--fg-1);
}
.pl-hero.ambient-extended .pl-more:hover { background: var(--bg-3); color: var(--fg-0); }
</style>
