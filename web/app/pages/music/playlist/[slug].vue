<template>
  <div v-if="loading" class="page-pad m-loading">Loading…</div>
  <div v-else-if="!detail" class="page-pad">
    <MusicEmptyState icon="music" title="Playlist not found">
      It may have been deleted or renamed. Your playlists live in the
      <NuxtLink to="/music">sidebar</NuxtLink>.
    </MusicEmptyState>
  </div>
  <!-- Tone-follow: every descendant (hero buttons, ledger tone cells)
       inherits --tone/--tone-rgb/--tone-ink published here. -->
  <div v-else class="pl-page" :style="toneStyle">
    <!-- Shared collection hero (MusicCollectionHero owns the ambient-extended
         convention — with ambient ON the app's background layer rotates
         through this playlist's artists and the hero paints nothing). -->
    <MusicCollectionHero
      kind="Playlist"
      :title="pl.name"
      :description="plainDescription"
      :images="artistArtUrls"
      :backdrop="backdropSrc"
      @image="currentBgArt = $event"
    >
      <template #art>
        <!-- Cover: the user's uploaded image when set; otherwise a collage
             built from the playlist's own albums (MixCollage dedupes to 4,
             falls back to the first album cover, then the icon tile).
             @art reports the image the collage ACTUALLY rendered (post
             error-cascade) — the tone sampler follows it, never a candidate
             URL that may 404. -->
        <LoadingImage v-if="customCoverUrl" :src="customCoverUrl" :width="400" :quality="85" :alt="`${pl.name} cover`" />
        <MixCollage v-else-if="tracks.length" :tracks="tracks" :seed-src="firstAlbumCover" :alt="`${pl.name} cover`" class="pl-hero-collage" @art="collageArt = $event" />
        <Icon v-else name="heart" :size="48" />
      </template>
      <template #stats>
        <span>{{ tracks.length }} {{ tracks.length === 1 ? 'track' : 'tracks' }}</span>
        <span v-if="totalDuration > 0" class="dot">·</span>
        <span v-if="totalDuration > 0">{{ formatRunTime(totalDuration) }}</span>
      </template>
      <template #actions>
        <button class="btn-play" :disabled="!tracks.length" @click="playAll(false)">
          <span class="tri" /> Play <small>{{ tracks.length }} {{ tracks.length === 1 ? 'TRACK' : 'TRACKS' }}</small>
        </button>
        <button class="pill" :disabled="!tracks.length" @click="playAll(true)">
          <Icon name="shuffle" :size="15" /> Shuffle
        </button>
        <button class="pill" :disabled="syncBusy" @click="playlistSyncAction">
          <Icon :name="listenBrainzSync ? 'refresh' : 'globe'" :size="15" /> {{ playlistSyncActionLabel }}
        </button>
        <AppMenu trigger-class="pill icon" trigger-title="Playlist options" trigger-aria-label="Playlist options">
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
        <input ref="coverInput" type="file" accept="image/jpeg,image/png,image/webp,.jpg,.jpeg,.png,.webp" class="pl-cover-input" @change="onCoverPicked" />
      </template>
    </MusicCollectionHero>

    <!-- The Heya 2.0 spec strip at the hero's hard-clip seam — same element
         the album/artist detail pages carry. -->
    <LedgerStrip :cells="ledgerCells" />

    <section v-if="!tracks.length" class="page-pad">
      <MusicEmptyState icon="music" title="This playlist is empty" compact>
        Right-click any track (long-press on touch) and pick
        <strong>Add to playlist</strong> — from <NuxtLink to="/music/songs">All Songs</NuxtLink>,
        an album, or search.
      </MusicEmptyState>
    </section>

    <!-- One TrackList for every width — the shared component brings the
         glass panel, phone layout, virtualization (page-mode RecycleScroller
         for thousand-track playlists), drag, and the context/action sheet.
         The playlist extras (Added date, per-row remove) ride the
         kind:'custom' cell slots; on phone those columns don't render and
         Remove lives in the row's ⋯ sheet via contextItemsFor. -->
    <section v-else class="page-pad pl-tracks">
      <TrackList
        :tracks="tlRows"
        :columns="columns"
        storage-key="playlist"
        :context-items="contextItemsFor"
        :active-track-id="currentTrack?.id ?? null"
        :playing="playing"
        vu-meter-in="art"
        :duration-formatter="formatTime"
        :on-rating-change="onRatingChange"
        :virtualized="tlRows.length > 200"
        @row-click="playFrom"
      >
        <template #cell-remove="{ index }">
          <button
            type="button"
            class="pl-remove"
            :aria-label="`Remove ${tracks[index]!.track_title} from playlist`"
            title="Remove from playlist"
            @click.stop="removeRow(tracks[index]!.track_id)"
          >
            <Icon name="close" :size="14" />
          </button>
        </template>
      </TrackList>
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
            <span v-if="listenBrainzConnected">{{ listenBrainzSync ? (listenBrainzSync.last_error || (listenBrainzSync.sync_mode === 'pull_only' ? 'Pull-only sync is active — ListenBrainz controls this playlist' : 'Two-way sync is active')) : 'Keep this playlist synchronized in both directions' }}</span>
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
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import type { ContextMenuItem } from '~~/shared/types'
import type { ImageTone } from '~/composables/useImageTone'
import { DropdownMenuItem } from 'reka-ui'
import { useQuery, useQueryCache } from '@pinia/colada'
import { playlistDetailQuery, type PlaylistDetailResponse, type PlaylistTrackRow } from '~/queries/music'
import { musicServicesQuery } from '~/queries/settings'

definePageMeta({ layout: 'default' })

// One shared TrackList at every width (the old hand-rolled desktop table's
// rationale — "TrackList has no virtualization" — went stale when TrackList
// gained the `virtualized` prop; the per-row remove button and Added date
// ride kind:'custom' cell slots).
const route = useRoute()
const router = useRouter()
// Slug in canonical URLs; numeric ids still resolve (legacy links, fresh
// creates that only know the id). Everything downstream keys on String(ref).
const playlistRef = computed(() => String(route.params.slug ?? ''))

const { play, queue, currentTrack, playing, formatTime, playTracks, playContext } = usePlayerBindings()
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
const playlistSyncActionLabel = computed(() => {
  if (!listenBrainzConnected.value) return 'Connect ListenBrainz'
  if (listenBrainzSync.value?.sync_mode === 'pull_only') return 'Refresh from ListenBrainz'
  if (listenBrainzSync.value) return 'Sync ListenBrainz'
  return 'Sync to ListenBrainz'
})
const totalDuration = computed(() => tracks.value.reduce((s, t) => s + (t.duration || 0), 0))

// Synced playlists carry service-authored HTML descriptions (ListenBrainz
// wraps paragraphs in <p>) — the hero renders plain text, so strip tags.
const plainDescription = computed(() => {
  const d = pl.value?.description ?? ''
  const text = d.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim()
  return text || undefined
})

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
// What MixCollage ACTUALLY rendered (post error-cascade) — null until its
// first image lands or when it fell through to the icon tile.
const collageArt = ref<string | null>(null)
// The blurred hero backdrop (ambient-off mode) follows the healed art too.
const backdropSrc = computed(() => customCoverUrl.value || collageArt.value || firstAlbumCover.value)

// ── Ledger strip — playlist facts from the (fully loaded) tracklist ────
const LOSSLESS_FORMATS = new Set(['flac', 'alac', 'wav', 'aiff'])
const ledgerCells = computed<LedgerCell[]>(() => {
  const n = tracks.value.length
  if (!n) return []
  const artistCount = new Set(tracks.value.map((t) => t.artist_id)).size
  const lossless = tracks.value.filter((t) => LOSSLESS_FORMATS.has((t.format ?? '').toLowerCase())).length
  const plays = tracks.value.reduce((s, t) => s + (t.play_count ?? 0), 0)
  const cells: LedgerCell[] = [
    { k: 'Tracks', v: String(n) },
    { k: 'Runtime', v: formatRunTime(totalDuration.value) },
    { k: 'Artists', v: String(artistCount) },
    { k: 'Lossless', v: `${Math.round((lossless / n) * 100)}%` },
    { k: 'Plays', v: plays.toLocaleString(), tone: plays > 0 },
  ]
  if (pl.value.updated_at) cells.push({ k: 'Updated', v: timeAgoShort(pl.value.updated_at) })
  return cells
})

// Current hero image (declared ahead of the tone watch below, which reads
// it with immediate: true). Driven by the rotation block further down.
const currentBgArt = ref<string | null>(null)

// ── Tone-follow: publish --tone/--tone-rgb/--tone-ink on the page root.
// Primary source is the AmbientBackdrop's own sampled tone (HeroCanvas
// claims the ambient layer with the hero image, so useBackgroundTone
// re-samples on every rotation); a direct sample of the current hero image
// is the fallback, sequence-guarded — same pattern as the artist/album
// heroes.
const bgTone = useBackgroundTone()
const localTone = ref<ImageTone | null>(null)
let toneSeq = 0
watch(() => currentBgArt.value || customCoverUrl.value || collageArt.value, (src) => {
  const seq = ++toneSeq
  if (!src || !import.meta.client) { localTone.value = null; return }
  sampleImageTone(src).then((t) => {
    if (seq === toneSeq) localTone.value = t
  })
}, { immediate: true })

const { toneFollowEnabled } = useAppearance()
const toneStyle = computed(() => {
  if (!toneFollowEnabled.value) return undefined
  const t = bgTone.value || localTone.value
  return t ? toneStyleVars(t) : undefined
})

// ── Hero art — the playlist's artists ────────────────────────────────
// One rotating pick drives the sharp hero band; HeroCanvas mirrors it to
// the app's ambient layer, so the blur below the ledger seam is ALWAYS the
// same image. BG_ROTATE_MS keeps cadence identical to the library pools.

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

// MusicCollectionHero owns the rotation (CycleControls owns the clock;
// prev/pause/next/expand live in its tools cluster) — this page just
// supplies the pool above and mirrors the shown image via @image for the
// tone fallback sample.

const coverInput = ref<HTMLInputElement>()
async function onCoverPicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  // File.type is allowed to be empty. The server validates decoded image
  // bytes; this check is only an early hint when the browser has a MIME type.
  if (file.type && !['image/jpeg', 'image/png', 'image/webp'].includes(file.type)) {
    flash.value = { kind: 'err', text: 'Choose a JPEG, PNG, or WebP image' }
    return
  }
  if (file.size > 25 * 1024 * 1024) {
    flash.value = { kind: 'err', text: 'Images must be 25 MiB or smaller' }
    return
  }
  try {
    await playlists.setCover(playlistId.value, file)
    coverBust.value++
    await detailQuery.refetch()
  } catch (error: any) {
    flash.value = { kind: 'err', text: error?.data?.detail || 'Could not upload the playlist cover' }
  }
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

async function playlistSyncAction() {
  if (!listenBrainzConnected.value) {
    await navigateTo('/settings/services')
    return
  }
  if (listenBrainzSync.value) {
    await syncNow('listenbrainz')
    return
  }
  await togglePlaylistSync('listenbrainz', true)
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

async function playAll(shuffle: boolean) {
  let list = tracks.value.filter(isPlayable).map(toPlayable)
  if (shuffle) list = [...list].sort(() => Math.random() - 0.5)
  if (!list.length) return
  await playTracks(list)
}

async function playFrom(idx: number) {
  const target = tracks.value[idx]
  if (!target || !isPlayable(target)) return
  // Semantic source: the server owns the playlist order + availability.
  await playContext({ kind: 'playlist', id: playlistId.value }, { startTrackId: target.track_id })
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

// Shared TrackList wiring (see script-top comment). The fixed column set
// matches the old table; the rich optional set (plays, bitrate, BPM, key,
// …) rides the column picker, persisted under the "playlist" storage key.
const actions = useMusicActions()
const trackRatings = useTrackRatings()
const ratings = trackRatings.ratings

// Server rows carry the caller's rating — prime the shared ratings map so
// the (optional) rating column and React submenu agree without a batch call.
watch(tracks, (rows) => {
  if (rows.length) trackRatings.primeMany(rows.map((t) => [t.track_id, t.rating ?? 0] as [number, number]))
}, { immediate: true })

async function onRatingChange(trackId: number, v: number) {
  try { await trackRatings.set(trackId, v) } catch { /* rollback handled */ }
}

// Column order contract: title → artist → album → added → everything
// else → duration (the remove button hugs duration at the end).
const columns: TrackListColumn[] = [
  { key: 'idx', kind: 'index', label: '#', width: '48px' },
  { key: 'art', kind: 'art', width: '56px' },
  { key: 'title', kind: 'title', label: 'Title', subtitle: 'artist-plain', width: 'minmax(200px, 1fr)', sortable: true },
  artistTrackColumn(),
  { key: 'album', kind: 'album', label: 'Album', width: 'minmax(160px, 1.2fr)', optional: true, defaultOn: true, sortable: true },
  { key: 'added', kind: 'meta', label: 'Added', width: '96px', optional: true, defaultOn: true, format: (r) => (r.added_at ? formatShortDate(r.added_at) : '—'), sortable: true, sortValue: (r) => (r.added_at ? Date.parse(r.added_at) || null : null), tooltip: (r) => formatFullDateTime(r.added_at) },
  ...richTrackColumns(),
  { key: 'rating', kind: 'rating', label: 'Rating', width: '130px', optional: true, sortable: true },
  { key: 'remove', kind: 'custom', width: '32px' },
  { key: 'duration', kind: 'duration', headerIcon: 'clock', width: '64px', sortable: true },
]

const tlRows = computed<TrackListRow[]>(() => tracks.value.map((t) => ({
  id: t.track_id,
  title: t.track_title,
  artist: t.artist_name,
  artist_slug: t.artist_slug,
  album: t.album_title,
  album_slug: t.album_slug,
  album_year: t.album_year,
  duration: t.duration,
  available: t.available,
  poster: useAlbumCoverUrl(t.artist_slug, t.album_slug),
  rating: ratings.value.get(t.track_id) ?? t.rating ?? 0,
  added_at: t.added_at,
  ...pickRichFields(t),
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

/* Hero grammar lives in MusicCollectionHero now — this page keeps only its
   own extras (collage flattening, hidden file input, tracklist cells). */
/* The collage manages its own radius/shadow — flatten inside the frame. */
.pl-hero-collage { width: 100%; height: 100%; border-radius: 0; box-shadow: none; }
.dot { color: var(--fg-3); }
.pl-cover-input { display: none; }

.pl-tracks { padding-top: 24px; }
/* Playlist-specific cells inside the shared TrackList — the glass panel,
   grid, and row chrome all come from TrackList itself. Scoped rules reach
   this content because it renders through OUR cell-slot templates. */
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
:deep(.tl-track:hover) .pl-remove,
.pl-remove:focus-visible { opacity: 1; }
.pl-remove:hover { background: rgb(var(--ink) / 0.06); color: var(--fg-0); }

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

</style>
