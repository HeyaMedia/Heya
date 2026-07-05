// Reactive cache of the user's playlists, hydrated once per session.
// The sidebar and the playlist pages all read from the same source.

export interface UserPlaylistRow {
  id: number
  user_id: number
  name: string
  description: string
  cover_path: string
  created_at: string
  updated_at: string
  track_count: number
  auto_cover: string
}

const playlists = ref<UserPlaylistRow[]>([])
const loaded = ref(false)
let inflight: Promise<void> | null = null

async function loadAll() {
  if (loaded.value || import.meta.server) return
  if (!inflight) {
    inflight = (async () => {
      try {
        const { $heya } = useNuxtApp()
        const resp = await $heya('/api/me/playlists')
        playlists.value = (resp.items as UserPlaylistRow[] | undefined) ?? []
      } catch {
        playlists.value = []
      } finally {
        loaded.value = true
        inflight = null
      }
    })()
  }
  await inflight
}

// Every mutation below also has to invalidate the vue-query caches that
// mirror playlist state elsewhere in the app — the playlist detail page
// (`['music', 'playlist', id]`), the music-home recent-playlists rail
// (`['music', 'home', 'recent-playlists']`), and the /music/my "My
// Playlists" shelf (`['me', 'playlists']`). Those pages don't read from the
// `playlists` ref above, so without this a create/add/remove/delete here
// (this ref updates synchronously) leaves those other views stale until a
// hard reload — the exact bug this helper exists to close.
//
// `useNuxtApp().$queryClient` (not `useQueryClient()`) on purpose: these
// functions run from event-handler closures (context-menu actions, modal
// submit handlers) with no active component instance on the call stack, the
// same reason plugins/cache-invalidation.client.ts reads the client off
// `nuxtApp` instead of injecting it.
function invalidatePlaylistCaches(playlistId?: number) {
  if (import.meta.server) return
  const { $queryClient } = useNuxtApp()
  if (playlistId != null) {
    $queryClient.invalidateQueries({ queryKey: ['music', 'playlist', playlistId] })
  }
  $queryClient.invalidateQueries({ queryKey: ['music', 'home', 'recent-playlists'] })
  $queryClient.invalidateQueries({ queryKey: ['me', 'playlists'] })
}

async function create(name: string, description = '', coverPath = '') {
  const { $heya } = useNuxtApp()
  const created = await $heya('/api/me/playlists', {
    method: 'POST',
    body: { name, description, cover_path: coverPath },
  }) as UserPlaylistRow
  // Server returns the bare playlist row without aggregate counts — fold in
  // sensible defaults so the sidebar can render immediately.
  playlists.value = [
    { ...created, track_count: 0, auto_cover: '' } as UserPlaylistRow,
    ...playlists.value,
  ]
  invalidatePlaylistCaches()
  return created
}

async function remove(id: number) {
  const { $heya } = useNuxtApp()
  await $heya('/api/me/playlists/{id}', { method: 'DELETE', path: { id } })
  playlists.value = playlists.value.filter((p) => p.id !== id)
  invalidatePlaylistCaches(id)
}

async function addTrack(playlistId: number, trackId: number) {
  const { $heya } = useNuxtApp()
  await $heya('/api/me/playlists/{id}/tracks/{track_id}', {
    method: 'POST',
    path: { id: playlistId, track_id: trackId },
  })
  // Bump track_count locally so the sidebar counter stays in sync until next load.
  playlists.value = playlists.value.map((p) =>
    p.id === playlistId ? { ...p, track_count: p.track_count + 1 } : p,
  )
  invalidatePlaylistCaches(playlistId)
}

async function removeTrack(playlistId: number, trackId: number) {
  const { $heya } = useNuxtApp()
  await $heya('/api/me/playlists/{id}/tracks/{track_id}', {
    method: 'DELETE',
    path: { id: playlistId, track_id: trackId },
  })
  playlists.value = playlists.value.map((p) =>
    p.id === playlistId ? { ...p, track_count: Math.max(0, p.track_count - 1) } : p,
  )
  invalidatePlaylistCaches(playlistId)
}

// Sidebar view: simplified rows with `cover` synthesizing from user-set
// cover_path falling back to the first-track auto cover.
export interface SidebarPlaylist {
  id: number
  name: string
  count: number
  cover_path: string
}

export function usePlaylists() {
  const sidebarRows = computed<SidebarPlaylist[]>(() =>
    playlists.value.map((p) => ({
      id: p.id,
      name: p.name,
      count: p.track_count,
      cover_path: p.cover_path || p.auto_cover,
    })),
  )

  return {
    playlists: readonly(playlists),
    sidebarRows,
    loaded: readonly(loaded),
    ensureLoaded: loadAll,
    create,
    remove,
    addTrack,
    removeTrack,
  }
}
