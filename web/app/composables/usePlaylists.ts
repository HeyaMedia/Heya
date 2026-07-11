import { useQuery, useQueryCache } from '@pinia/colada'
import {
  userPlaylistsQuery,
  type UserPlaylistRow,
  type UserPlaylistsResponse,
} from '~/queries/music'

export type { UserPlaylistRow } from '~/queries/music'

export interface SidebarPlaylist {
  id: number
  name: string
  count: number
  cover_path: string
}

// Server-owned playlist state lives in Colada. Every caller observes the same
// cache entry, while mutations update it optimistically and invalidate the
// destination/shelf queries that contain related playlist data.
export function usePlaylists() {
  const queryCache = useQueryCache()
  const listQuery = useQuery(userPlaylistsQuery())
  const playlists = computed<UserPlaylistRow[]>(() => listQuery.data.value?.items ?? [])
  const loaded = computed(() => listQuery.status.value !== 'pending')

  async function ensureLoaded() {
    if (listQuery.data.value !== undefined || import.meta.server) return
    try { await listQuery.refetch() } catch { /* sidebar remains empty */ }
  }

  function updateList(updater: (rows: UserPlaylistRow[]) => UserPlaylistRow[]) {
    queryCache.setQueryData<UserPlaylistsResponse>(['me', 'playlists'], (current) => ({
      items: updater(current?.items ?? []),
    }))
  }

  function invalidatePlaylistCaches(playlistId?: number) {
    if (import.meta.server) return
    if (playlistId != null) {
      queryCache.invalidateQueries({ key: ['music', 'playlist', playlistId] })
    }
    queryCache.invalidateQueries({ key: ['music', 'home', 'recent-playlists'] })
  }

  async function create(name: string, description = '', coverPath = '') {
    const { $heya } = useNuxtApp()
    const created = await $heya('/api/me/playlists', {
      method: 'POST',
      body: { name, description, cover_path: coverPath },
    }) as UserPlaylistRow
    const row = { ...created, track_count: 0, auto_cover: '' } as UserPlaylistRow
    updateList(rows => [row, ...rows])
    invalidatePlaylistCaches()
    return created
  }

  async function remove(id: number) {
    const { $heya } = useNuxtApp()
    await $heya('/api/me/playlists/{id}', { method: 'DELETE', path: { id } })
    updateList(rows => rows.filter(p => p.id !== id))
    invalidatePlaylistCaches(id)
  }

  async function addTrack(playlistId: number, trackId: number) {
    const { $heya } = useNuxtApp()
    await $heya('/api/me/playlists/{id}/tracks/{track_id}', {
      method: 'POST',
      path: { id: playlistId, track_id: trackId },
    })
    updateList(rows => rows.map(p =>
      p.id === playlistId ? { ...p, track_count: p.track_count + 1 } : p,
    ))
    invalidatePlaylistCaches(playlistId)
  }

  async function removeTrack(playlistId: number, trackId: number) {
    const { $heya } = useNuxtApp()
    await $heya('/api/me/playlists/{id}/tracks/{track_id}', {
      method: 'DELETE',
      path: { id: playlistId, track_id: trackId },
    })
    updateList(rows => rows.map(p =>
      p.id === playlistId ? { ...p, track_count: Math.max(0, p.track_count - 1) } : p,
    ))
    invalidatePlaylistCaches(playlistId)
  }

  const sidebarRows = computed<SidebarPlaylist[]>(() =>
    playlists.value.map(p => ({
      id: p.id,
      name: p.name,
      count: p.track_count,
      cover_path: p.cover_path || p.auto_cover,
    })),
  )

  return {
    playlists,
    sidebarRows,
    loaded,
    ensureLoaded,
    create,
    remove,
    addTrack,
    removeTrack,
  }
}
