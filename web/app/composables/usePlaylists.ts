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
        const resp = await apiFetch<{ items: UserPlaylistRow[] }>('/api/me/playlists')
        playlists.value = resp.items ?? []
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

async function create(name: string, description = '', coverPath = '') {
  const created = await apiFetch<UserPlaylistRow>('/api/me/playlists', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, description, cover_path: coverPath }),
  })
  // Server returns the bare playlist row without aggregate counts — fold in
  // sensible defaults so the sidebar can render immediately.
  playlists.value = [
    { ...created, track_count: 0, auto_cover: '' } as UserPlaylistRow,
    ...playlists.value,
  ]
  return created
}

async function remove(id: number) {
  await apiFetch(`/api/me/playlists/${id}`, { method: 'DELETE' })
  playlists.value = playlists.value.filter((p) => p.id !== id)
}

async function addTrack(playlistId: number, trackId: number) {
  await apiFetch(`/api/me/playlists/${playlistId}/tracks/${trackId}`, { method: 'POST' })
  // Bump track_count locally so the sidebar counter stays in sync until next load.
  playlists.value = playlists.value.map((p) =>
    p.id === playlistId ? { ...p, track_count: p.track_count + 1 } : p,
  )
}

async function removeTrack(playlistId: number, trackId: number) {
  await apiFetch(`/api/me/playlists/${playlistId}/tracks/${trackId}`, { method: 'DELETE' })
  playlists.value = playlists.value.map((p) =>
    p.id === playlistId ? { ...p, track_count: Math.max(0, p.track_count - 1) } : p,
  )
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
