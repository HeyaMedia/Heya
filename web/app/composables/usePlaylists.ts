import { useQuery, useQueryCache } from '@pinia/colada'
import {
  userPlaylistsQuery,
  type UserPlaylistRow,
  type UserPlaylistsResponse,
} from '~/queries/music'

export type { UserPlaylistRow } from '~/queries/music'

export interface SidebarPlaylist {
  id: number
  slug: string
  name: string
  count: number
  cover_path: string
  pinned: boolean
}

export type SidebarPlaylistSort = 'custom' | 'name' | 'updated' | 'created'

// Module-level singleton so every usePlaylists() caller (sidebar, phone nav
// sheet, playlists page) observes the same sort choice. Client-only pref —
// SSR renders the default and hydration corrects it before first paint of
// the sidebar list.
const SIDEBAR_SORT_KEY = 'heya:sidebar-playlist-sort'
const sidebarSort = ref<SidebarPlaylistSort>('custom')
if (import.meta.client) {
  const stored = localStorage.getItem(SIDEBAR_SORT_KEY)
  if (stored === 'custom' || stored === 'name' || stored === 'updated' || stored === 'created') {
    sidebarSort.value = stored
  }
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

  function invalidatePlaylistCaches(playlistId?: number, slug?: string) {
    if (import.meta.server) return
    // Detail entries are keyed by String(ref) — a playlist can sit in the
    // cache under its slug (canonical URL visits) and/or its id (internal
    // callers); invalidate whichever we know about.
    if (playlistId != null) {
      queryCache.invalidateQueries({ key: ['music', 'playlist', String(playlistId)] })
    }
    if (slug) {
      queryCache.invalidateQueries({ key: ['music', 'playlist', slug] })
    }
    queryCache.invalidateQueries({ key: ['music', 'home', 'recent-playlists'] })
  }

  async function create(name: string, description = '', coverPath = '') {
    const { $heya } = useNuxtApp()
    const created = await $heya('/api/me/playlists', {
      method: 'POST',
      body: { name, description, cover_path: coverPath },
    }) as UserPlaylistRow
    const row = { ...created, track_count: 0, auto_artist_slug: '', auto_album_slug: '' } as UserPlaylistRow
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

  // Rename / re-describe. Renaming REGENERATES the slug server-side (URLs
  // track names during dev — no legacy-slug shims), so the caller gets the
  // fresh row back to re-route with.
  async function update(id: number, patch: { name: string; description: string }) {
    const { $heya } = useNuxtApp()
    // cover_path is required by the PUT body and stores the custom cover's
    // disk path — pass the current value through so a rename never clears
    // an uploaded cover.
    const current = playlists.value.find(p => p.id === id)
    const updated = await $heya('/api/me/playlists/{id}', {
      method: 'PUT',
      path: { id },
      body: { name: patch.name, description: patch.description, cover_path: current?.cover_path ?? '' },
    }) as unknown as UserPlaylistRow
    updateList(rows => rows.map(p => (p.id === id ? { ...p, ...updated } : p)))
    invalidatePlaylistCaches(id, updated.slug)
    return updated
  }

  // Custom cover upload. Multipart stays on raw $fetch — $heya/openapi-fetch
  // insist on JSON bodies (same reasoning as MetadataEditorImages.vue).
  async function setCover(id: number, file: File) {
    const { token } = useAuth()
    const form = new FormData()
    form.append('file', file)
    await $fetch(`/api/me/playlists/${id}/cover`, {
      method: 'POST',
      body: form,
      headers: token.value ? { Authorization: `Bearer ${token.value}` } : {},
    })
    updateList(rows => rows.map(p => (p.id === id ? { ...p, has_cover: true } : p)))
    invalidatePlaylistCaches(id)
  }

  async function clearCover(id: number) {
    const { $heya } = useNuxtApp()
    await $heya('/api/me/playlists/{id}/cover' as never, { method: 'DELETE', path: { id } } as never)
    updateList(rows => rows.map(p => (p.id === id ? { ...p, has_cover: false } : p)))
    invalidatePlaylistCaches(id)
  }

  // Toggle one of the two independent pin scopes. Optimistic — a pin is
  // cheap to re-toggle if the request fails, so no rollback plumbing.
  async function setPin(id: number, scope: 'page' | 'sidebar', pinned: boolean) {
    const { $heya } = useNuxtApp()
    updateList(rows => rows.map(p => (p.id === id
      ? { ...p, ...(scope === 'page' ? { pinned } : { sidebar_pinned: pinned }) }
      : p)))
    await $heya('/api/me/playlists/{id}/pin', {
      method: 'PUT',
      path: { id },
      body: { scope, pinned },
    })
  }

  // Persist a manual sidebar drag order. `ids` is the full list, top to
  // bottom, as rendered (pinned block first) — positions are just 1..n.
  async function reorderSidebar(ids: number[]) {
    const { $heya } = useNuxtApp()
    const pos = new Map(ids.map((id, i) => [id, i + 1]))
    updateList(rows => rows.map(p => (pos.has(p.id) ? { ...p, sidebar_position: pos.get(p.id)! } : p)))
    await $heya('/api/me/playlists/sidebar-order', {
      method: 'PUT',
      body: { ids },
    })
  }

  function setSidebarSort(sort: SidebarPlaylistSort) {
    sidebarSort.value = sort
    if (import.meta.client) localStorage.setItem(SIDEBAR_SORT_KEY, sort)
  }

  const sidebarRows = computed<SidebarPlaylist[]>(() => {
    const bySort = (a: UserPlaylistRow, b: UserPlaylistRow): number => {
      switch (sidebarSort.value) {
        case 'name':
          return a.name.localeCompare(b.name)
        case 'updated':
          return b.updated_at.localeCompare(a.updated_at)
        case 'created':
          return b.created_at.localeCompare(a.created_at)
        default: {
          // Manual order; never-dragged rows (position 0) sink below
          // positioned ones, newest-first among themselves.
          const ap = a.sidebar_position > 0 ? a.sidebar_position : Infinity
          const bp = b.sidebar_position > 0 ? b.sidebar_position : Infinity
          return ap !== bp ? ap - bp : b.created_at.localeCompare(a.created_at)
        }
      }
    }
    return [...playlists.value]
      .sort((a, b) => Number(b.sidebar_pinned) - Number(a.sidebar_pinned) || bySort(a, b))
      .map(p => ({
        id: p.id,
        slug: p.slug,
        name: p.name,
        count: p.track_count,
        // Resolved, renderable URL (custom-cover endpoint or first album's
        // canonical cover) — raw cover_path is a disk path, never usable.
        cover_path: playlistCoverSrc(p) ?? '',
        pinned: p.sidebar_pinned,
      }))
  })

  return {
    playlists,
    sidebarRows,
    sidebarSort: readonly(sidebarSort),
    setSidebarSort,
    loaded,
    ensureLoaded,
    create,
    remove,
    update,
    addTrack,
    removeTrack,
    setCover,
    clearCover,
    setPin,
    reorderSidebar,
  }
}
