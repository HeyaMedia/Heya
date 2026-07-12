/**
 * Builds context-menu item arrays for music entities (track / album /
 * artist). Single source of truth for "what can I do with this thing"
 * across the app — right-click on any music row uses the same actions,
 * so the muscle memory is consistent.
 *
 * Usage:
 *   const actions = useMusicActions()
 *   <AppContextMenu :items="actions.forTrack(rowToTrack)">
 *     <li>...</li>
 *   </AppContextMenu>
 *
 * The composable handles: playback, queue ops, radio kickoff, playlist
 * pickers, rating sub-menus, and navigation. Async ops (load album tracks
 * before queueing) live inside the action closures so the consumer just
 * passes the row.
 */

import type { ContextMenuItem } from '~~/shared/types'
import type { Track } from '~/composables/usePlayer'
import { musicAlbumDetailQuery } from '~/queries/music'

export interface TrackEntity {
  id: number
  title: string
  artist: string
  album: string
  duration: number
  album_id?: number
  artist_id?: number
  artist_slug?: string
  album_slug?: string
  /** When false, the track has no file on disk — omit play/queue actions. */
  available?: boolean
}

export interface AlbumEntity {
  id: number
  title: string
  artist_slug: string
  album_slug: string
  artist_id?: number
  artist_name?: string
  /** When false, the album's files are gone — omit play/queue actions. */
  available?: boolean
}

export interface ArtistEntity {
  id: number
  name: string
  slug: string
  media_item_id?: number
  /** When false, the artist's files are gone — omit play/queue actions. */
  available?: boolean
}

export function useMusicActions() {
  const { play, queue, addToQueue, playNext } = usePlayerBindings()
  const { $heya } = useNuxtApp()
  const playlists = usePlaylists()
  const radio = useRadio()
  const trackRatings = useRatings('track')
  const albumRatings = useRatings('album')
  const artistRatings = useRatings('artist')
  const loadQuery = useQueryLoader()

  if (import.meta.client) playlists.ensureLoaded()

  function trackEntityToPlayable(t: TrackEntity): Track {
    return {
      id: t.id,
      title: t.title,
      artist: t.artist,
      album: t.album,
      duration: t.duration,
      stream_url: `/api/music/tracks/${t.id}/stream`,
      album_id: t.album_id,
      artist_id: t.artist_id,
      artist_slug: t.artist_slug,
      album_slug: t.album_slug,
      poster: t.artist_slug && t.album_slug ? (useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? undefined) : undefined,
      source: 'context',
      available: t.available,
    }
  }

  // --- Playlist picker submenu (shared across track/album) ---
  function playlistSubmenu(onPick: (playlistId: number) => Promise<void> | void): ContextMenuItem[] {
    const rows = playlists.playlists.value
    const items: ContextMenuItem[] = []
    if (!rows.length) {
      items.push({ label: 'No playlists yet', disabled: true })
    } else {
      for (const p of rows) {
        items.push({
          label: p.name,
          icon: 'list',
          action: () => onPick(p.id),
        })
      }
    }
    items.push({ label: '', separator: true })
    items.push({
      label: 'New playlist…',
      icon: 'plus',
      action: async () => {
        const name = prompt('New playlist name')
        if (!name) return
        const created = await playlists.create(name, '')
        await onPick(created.id)
        navigateTo(`/music/playlist/${created.slug || created.id}`)
      },
    })
    return items
  }

  // --- Reaction submenu — Heart / Thumbs Up / Thumbs Down / Clear.
  // Values are sentinels on the stored 1–10 rating scale (heart=10, up=7,
  // down=1); the active check reads BANDS so ratings arriving from Subsonic
  // clients (1–5 stars ×2) mark the matching reaction. Tapping the active
  // reaction clears — same contract as ReactionControl.
  function ratingSubmenu(currentValue: number, onPick: (rating: number) => Promise<void>): ContextMenuItem[] {
    const heart = currentValue >= 9
    const up = currentValue >= 6 && currentValue <= 8
    const down = currentValue >= 1 && currentValue <= 3
    const opts: { label: string; icon: string; active: boolean; v: number }[] = [
      { label: 'Love', icon: 'heart', active: heart, v: heart ? 0 : 10 },
      { label: 'Like', icon: 'thumbsup', active: up, v: up ? 0 : 7 },
      { label: 'Not for me', icon: 'thumbsdown', active: down, v: down ? 0 : 1 },
    ]
    return opts.map((o) => ({
      label: (o.active ? '• ' : '') + o.label,
      icon: o.icon,
      action: () => onPick(o.v),
    }))
  }

  function forTrack(track: TrackEntity): ContextMenuItem[] {
    const playable = trackEntityToPlayable(track)
    const items: ContextMenuItem[] = []
    // Playback/queue actions only make sense when the track still has a file.
    if (track.available !== false) {
      items.push(
        {
          label: 'Play Now',
          icon: 'play',
          action: async () => { queue.value = [playable]; await play(playable) },
        },
        {
          label: 'Play Next',
          icon: 'chevright',
          action: () => playNext(playable),
        },
        {
          label: 'Add to Queue',
          icon: 'plus',
          action: () => addToQueue(playable),
        },
        { label: '', separator: true },
        {
          label: 'Start Radio',
          icon: 'radio',
          action: () => radio.startRadio({ kind: 'track', track_id: track.id }, playable),
        },
        {
          label: 'Add to Playlist',
          icon: 'list',
          submenu: playlistSubmenu(async (plId) => { await playlists.addTrack(plId, track.id) }),
        },
      )
    }
    items.push(
      {
        label: 'React',
        icon: 'heart',
        submenu: ratingSubmenu(trackRatings.get(track.id), async (v) => { await trackRatings.set(track.id, v) }),
      },
      { label: '', separator: true },
    )
    if (track.artist_slug) {
      items.push({
        label: 'Go to Artist',
        icon: 'user',
        action: () => navigateTo(`/music/artist/${track.artist_slug}`),
      })
    }
    if (track.artist_slug && track.album_slug) {
      items.push({
        label: 'Go to Album',
        icon: 'music',
        action: () => navigateTo(`/music/artist/${track.artist_slug}/${track.album_slug}`),
      })
    }
    return items
  }

  // --- Album: needs to fetch its tracklist before queueing. Wrap the
  // async fetch inside each action so the menu opens instantly and the
  // network round-trip happens on click. ---
  async function fetchAlbumTracks(album: AlbumEntity): Promise<Track[]> {
    try {
      const detail = await loadQuery(musicAlbumDetailQuery({
        artistSlug: album.artist_slug,
        albumSlug: album.album_slug,
      }))
      // Only queue tracks that still have a file on disk (detail.tracks[].files
      // is server-filtered to live files; empty means removed).
      return (detail.tracks ?? [])
        .filter((t) => (t.files?.length ?? 0) > 0)
        .map((t) => ({
          id: t.id,
          title: t.title,
          artist: album.artist_name || detail.artist?.name || '',
          album: album.title,
          duration: t.duration,
          stream_url: `/api/music/tracks/${t.id}/stream`,
          album_id: album.id,
          artist_id: album.artist_id,
          artist_slug: album.artist_slug,
          album_slug: album.album_slug,
          poster: useAlbumCoverUrl(album.artist_slug, album.album_slug) ?? undefined,
          source: 'album',
          available: true,
        }))
    } catch {
      return []
    }
  }

  function forAlbum(album: AlbumEntity): ContextMenuItem[] {
    const items: ContextMenuItem[] = []
    // Playback/queue actions only make sense when the album still has files.
    if (album.available !== false) {
      items.push(
        {
          label: 'Play',
          icon: 'play',
          action: async () => {
            const ts = await fetchAlbumTracks(album)
            if (!ts.length) return
            queue.value = ts
            await play(ts[0]!)
          },
        },
        {
          label: 'Play Next',
          icon: 'chevright',
          action: async () => { const ts = await fetchAlbumTracks(album); if (ts.length) await playNext(ts) },
        },
        {
          label: 'Add to Queue',
          icon: 'plus',
          action: async () => { const ts = await fetchAlbumTracks(album); if (ts.length) await addToQueue(ts) },
        },
        { label: '', separator: true },
        {
          label: 'Add All to Playlist',
          icon: 'list',
          submenu: playlistSubmenu(async (plId) => {
            const ts = await fetchAlbumTracks(album)
            for (const t of ts) await playlists.addTrack(plId, t.id)
          }),
        },
      )
    }
    items.push(
      {
        label: 'React to Album',
        icon: 'heart',
        submenu: ratingSubmenu(albumRatings.get(album.id), async (v) => { await albumRatings.set(album.id, v) }),
      },
      { label: '', separator: true },
      {
        label: 'Go to Artist',
        icon: 'user',
        action: () => navigateTo(`/music/artist/${album.artist_slug}`),
      },
      {
        label: 'Open Album',
        icon: 'music',
        action: () => navigateTo(`/music/artist/${album.artist_slug}/${album.album_slug}`),
      },
    )
    return items
  }

  function forArtist(artist: ArtistEntity): ContextMenuItem[] {
    const items: ContextMenuItem[] = []
    // Playback/radio actions only make sense when the artist still has files.
    if (artist.available !== false) {
      items.push(
      {
        label: 'Play Top Tracks',
        icon: 'play',
        action: async () => {
          try {
            const r = await $heya('/api/music/artists/{slug}/tracks', {
              path: { slug: artist.slug },
              query: { limit: 50 },
            }) as unknown as { items?: Array<{ track_id?: number; id?: number; track_title?: string; title?: string; duration: number; album_title?: string; album_slug?: string; artist_slug?: string; available?: boolean }> }
            // Drop tracks whose file was removed from disk.
            const items = (r.items ?? []).filter((t) => t.available !== false)
            const ts: Track[] = items.map((t) => ({
              id: t.track_id ?? t.id ?? 0,
              title: t.track_title ?? t.title ?? '',
              artist: artist.name,
              album: t.album_title ?? '',
              duration: t.duration,
              stream_url: `/api/music/tracks/${t.track_id ?? t.id}/stream`,
              artist_id: artist.id,
              artist_slug: artist.slug,
              album_slug: t.album_slug,
              poster: t.album_slug ? (useAlbumCoverUrl(artist.slug, t.album_slug) ?? undefined) : undefined,
              source: 'artist',
              available: t.available,
            }))
            if (!ts.length) return
            queue.value = ts
            await play(ts[0]!)
          } catch { /* no-op */ }
        },
      },
      {
        label: 'Start Artist Radio',
        icon: 'radio',
        action: () => radio.startRadio({ kind: 'artist', artist_slug: artist.slug }),
      },
      { label: '', separator: true },
      )
    }
    items.push(
      {
        label: 'React to Artist',
        icon: 'heart',
        submenu: ratingSubmenu(artistRatings.get(artist.id), async (v) => { await artistRatings.set(artist.id, v) }),
      },
      { label: '', separator: true },
      {
        label: 'Go to Artist',
        icon: 'user',
        action: () => navigateTo(`/music/artist/${artist.slug}`),
      },
    )
    return items
  }

  // --- Mix: a curated, pre-loaded track list. The "Save as Playlist" item
  // lets the user freeze the (otherwise ephemeral) mix into a stable list.
  // ---
  function forMix(mix: { name: string; tracks: TrackEntity[]; seed_artist_slug?: string }): ContextMenuItem[] {
    const playables = mix.tracks.map(trackEntityToPlayable)
    return [
      {
        label: 'Play Mix',
        icon: 'play',
        action: async () => {
          if (!playables.length) return
          queue.value = playables
          await play(playables[0]!)
        },
      },
      {
        label: 'Play Next',
        icon: 'chevright',
        action: () => playNext(playables),
      },
      {
        label: 'Add to Queue',
        icon: 'plus',
        action: () => addToQueue(playables),
      },
      { label: '', separator: true },
      {
        label: 'Save as Playlist',
        icon: 'list',
        action: async () => {
          const name = prompt('Playlist name', mix.name)
          if (!name) return
          try {
            const created = await playlists.create(name, `Built from mix: ${mix.name}`)
            for (const t of mix.tracks) await playlists.addTrack(created.id, t.id)
            navigateTo(`/music/playlist/${created.slug || created.id}`)
          } catch { /* swallow */ }
        },
      },
      { label: '', separator: true },
      ...(mix.seed_artist_slug ? [{
        label: 'Go to Seed Artist',
        icon: 'user',
        action: () => navigateTo(`/music/artist/${mix.seed_artist_slug}`),
      }] : []),
    ]
  }

  // --- Playlist tile context menu (user playlists, not the system "Loved
  // Songs" pseudo-playlist). ---
  function forPlaylist(p: { id: number; name: string; track_count?: number; slug?: string }): ContextMenuItem[] {
    return [
      {
        label: 'Play',
        icon: 'play',
        action: async () => {
          try {
            // The playlist-detail endpoint returns ListPlaylistTracksRow, whose
            // JSON keys are track_id/track_title (not id/title). Build from the
            // correct fields and drop tracks whose file was removed from disk.
            const r = await $heya('/api/me/playlists/{id}', { path: { id: String(p.id) } }) as unknown as { tracks?: Array<{ track_id: number; track_title: string; artist_name?: string; album_title?: string; duration: number; album_id?: number; artist_id?: number; artist_slug?: string; album_slug?: string; available?: boolean }> }
            const list = (r.tracks ?? []).filter((t) => t.available !== false)
            if (!list.length) return
            const ts: Track[] = list.map((t) => ({
              id: t.track_id, title: t.track_title,
              artist: t.artist_name ?? '', album: t.album_title ?? '',
              duration: t.duration,
              stream_url: `/api/music/tracks/${t.track_id}/stream`,
              album_id: t.album_id, artist_id: t.artist_id,
              artist_slug: t.artist_slug, album_slug: t.album_slug,
              poster: t.artist_slug && t.album_slug ? (useAlbumCoverUrl(t.artist_slug, t.album_slug) ?? undefined) : undefined,
              source: 'playlist',
              available: t.available,
            }))
            queue.value = ts
            await play(ts[0]!)
          } catch { /* swallow */ }
        },
        disabled: (p.track_count ?? 1) === 0,
      },
      { label: '', separator: true },
      {
        label: 'Open Playlist',
        icon: 'list',
        action: () => navigateTo(`/music/playlist/${p.slug || p.id}`),
      },
      {
        label: 'Delete',
        icon: 'trash',
        action: async () => {
          if (!confirm(`Delete playlist "${p.name}"?`)) return
          try { await playlists.remove(p.id) } catch { /* swallow */ }
        },
      },
    ]
  }

  return { forTrack, forAlbum, forArtist, forMix, forPlaylist, playlistSubmenu }
}
