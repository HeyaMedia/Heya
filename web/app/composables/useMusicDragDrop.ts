// Desktop HTML5 drag-and-drop for music entities → sidebar playlists.
// Parallel singleton to useDragDrop.ts (movies/tv "add to list"), kept
// separate rather than generalized: the payload shapes (track/album/
// playlist) and drop resolution (which needs N POSTs + partial-failure
// counting) don't fit useDragDrop's single-media-id/single-POST shape.
//
// Gate all call sites on `!useViewport().isCoarse` — touch keeps the
// long-press context menu as the only way to add to a playlist.

export interface DragTrackPayload {
  kind: 'track'
  track: { id: number; title: string }
}
export interface DragAlbumPayload {
  kind: 'album'
  title: string
  artist_slug: string
  album_slug: string
  /** Pre-loaded track ids (e.g. artist-detail discography, already has
   *  `album.tracks` in memory) — skips the album-detail fetch on drop.
   *  Omit to have onPlaylistDrop fetch + filter live tracks itself. */
  trackIds?: number[]
}
export interface DragPlaylistPayload {
  kind: 'playlist'
  playlistId: number
  title: string
}

export type MusicDragPayload = DragTrackPayload | DragAlbumPayload | DragPlaylistPayload

const dragState = reactive({
  dragging: false,
  payload: null as MusicDragPayload | null,
  overPlaylistId: null as number | null,
})

export function useMusicDragDrop() {
  function onDragStart(event: DragEvent, payload: MusicDragPayload) {
    dragState.dragging = true
    dragState.payload = payload
    if (event.dataTransfer) {
      event.dataTransfer.effectAllowed = 'copy'
      // Several drag sources are NuxtLinks, which stamp their own
      // text/uri-list + text/plain (the href) onto dataTransfer by default.
      // Overriding text/plain here makes sure a drop handler that falls back
      // to reading it doesn't see a URL instead of our payload description.
      event.dataTransfer.setData('text/plain', describePayload(payload))
    }
  }

  function onDragEnd() {
    dragState.dragging = false
    dragState.payload = null
    dragState.overPlaylistId = null
  }

  function onPlaylistDragOver(event: DragEvent, playlistId: number) {
    event.preventDefault()
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'copy'
    dragState.overPlaylistId = playlistId
  }

  function onPlaylistDragLeave() {
    dragState.overPlaylistId = null
  }

  async function onPlaylistDrop(event: DragEvent, playlistId: number, playlistName: string) {
    event.preventDefault()
    dragState.overPlaylistId = null

    const payload = dragState.payload
    dragState.dragging = false
    dragState.payload = null
    if (!payload) return

    const { toast } = useToast()

    if (payload.kind === 'playlist') {
      if (payload.playlistId === playlistId) {
        toast.info("Can't drop a playlist onto itself")
        return
      }
      await dropPlaylistOntoPlaylist(payload, playlistId, playlistName)
      return
    }

    if (payload.kind === 'track') {
      await dropTrackOntoPlaylist(payload, playlistId, playlistName)
      return
    }

    await dropAlbumOntoPlaylist(payload, playlistId, playlistName)
  }

  return {
    dragState,
    onDragStart,
    onDragEnd,
    onPlaylistDragOver,
    onPlaylistDragLeave,
    onPlaylistDrop,
  }
}

function describePayload(payload: MusicDragPayload): string {
  if (payload.kind === 'track') return payload.track.title
  if (payload.kind === 'album') return payload.title
  return payload.title
}

async function dropTrackOntoPlaylist(payload: DragTrackPayload, playlistId: number, playlistName: string) {
  const { toast } = useToast()
  const playlists = usePlaylists()
  try {
    await playlists.addTrack(playlistId, payload.track.id)
    toast.ok(`Added "${payload.track.title}" to ${playlistName}`)
  } catch {
    toast.err(`Could not add "${payload.track.title}" to ${playlistName}`)
  }
}

async function dropAlbumOntoPlaylist(payload: DragAlbumPayload, playlistId: number, playlistName: string) {
  const { toast } = useToast()
  const playlists = usePlaylists()

  const trackIds = payload.trackIds ?? await fetchAlbumTrackIds(payload.artist_slug, payload.album_slug)
  if (!trackIds.length) {
    toast.err(`"${payload.title}" has no tracks to add`)
    return
  }

  let ok = 0
  for (const id of trackIds) {
    try {
      await playlists.addTrack(playlistId, id)
      ok++
    } catch { /* count as failure, keep going */ }
  }

  if (ok === trackIds.length) {
    toast.ok(`Added ${ok} ${ok === 1 ? 'track' : 'tracks'} to ${playlistName}`)
  } else if (ok > 0) {
    toast.err(`Added ${ok} of ${trackIds.length} tracks to ${playlistName}`)
  } else {
    toast.err(`Could not add "${payload.title}" to ${playlistName}`)
  }
}

async function dropPlaylistOntoPlaylist(payload: DragPlaylistPayload, playlistId: number, playlistName: string) {
  const { toast } = useToast()
  const playlists = usePlaylists()
  const { $heya } = useNuxtApp()

  let trackIds: number[] = []
  try {
    const detail = await $heya('/api/me/playlists/{id}', {
      path: { id: payload.playlistId },
    }) as unknown as { tracks?: Array<{ track_id: number; available?: boolean }> }
    trackIds = (detail.tracks ?? [])
      .filter((t) => t.available !== false)
      .map((t) => t.track_id)
  } catch {
    toast.err(`Could not read "${payload.title}"`)
    return
  }

  if (!trackIds.length) {
    toast.err(`"${payload.title}" has no tracks to add`)
    return
  }

  let ok = 0
  for (const id of trackIds) {
    try {
      await playlists.addTrack(playlistId, id)
      ok++
    } catch { /* count as failure, keep going */ }
  }

  if (ok === trackIds.length) {
    toast.ok(`Added ${ok} ${ok === 1 ? 'track' : 'tracks'} to ${playlistName}`)
  } else if (ok > 0) {
    toast.err(`Added ${ok} of ${trackIds.length} tracks to ${playlistName}`)
  } else {
    toast.err(`Could not add "${payload.title}" to ${playlistName}`)
  }
}

// Mirrors useMusicActions.ts's fetchAlbumTracks filtering (files?.length > 0)
// but only needs ids here, not full Track objects.
async function fetchAlbumTrackIds(artistSlug: string, albumSlug: string): Promise<number[]> {
  const { $heya } = useNuxtApp()
  try {
    const detail = await $heya('/api/music/artists/{artist_slug}/albums/{album_slug}', {
      path: { artist_slug: artistSlug, album_slug: albumSlug },
    }) as unknown as { tracks: Array<{ id: number; files?: unknown[] }> }
    return (detail.tracks ?? [])
      .filter((t) => (t.files?.length ?? 0) > 0)
      .map((t) => t.id)
  } catch {
    return []
  }
}
