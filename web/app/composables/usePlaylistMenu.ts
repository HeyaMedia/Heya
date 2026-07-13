import type { ContextMenuItem } from '~~/shared/types'

// The one right-click menu for a playlist, shared by every surface that
// shows one: the home shelf, My Music, the /music/playlists grid, and the
// sidebar. Surfaces pass whatever partial row they render with; pin state,
// tags, and sync services resolve from the shared ['me','playlists'] cache
// at menu-build time, so labels reflect current state without each surface
// threading full rows through. Mutations go through usePlaylists, whose
// cache invalidation converges all surfaces afterwards.
export function usePlaylistMenu() {
  const actions = useMusicActions()
  const playlistsApi = usePlaylists()
  const { toast } = useToast()

  function menuFor(p: { id: number; name: string; slug?: string; track_count?: number }): ContextMenuItem[] {
    const row = playlistsApi.playlists.value.find(r => r.id === p.id)
    const pinned = row?.pinned ?? false
    const sidebarPinned = row?.sidebar_pinned ?? false
    const name = row?.name ?? p.name

    const items = actions.forPlaylist(p)
    items.push(
      { label: '', separator: true },
      {
        label: pinned ? 'Unpin' : 'Pin to top',
        icon: 'pin',
        action: async () => {
          try {
            await playlistsApi.setPin(p.id, 'page', !pinned)
          } catch { toast.err('Pin failed') }
        },
      },
      {
        label: sidebarPinned ? 'Unpin from sidebar' : 'Pin to sidebar',
        icon: 'pin',
        action: async () => {
          try {
            await playlistsApi.setPin(p.id, 'sidebar', !sidebarPinned)
          } catch { toast.err('Pin failed') }
        },
      },
      { label: '', separator: true },
      {
        label: 'Rename…',
        icon: 'pencil',
        action: async () => {
          const next = prompt('Playlist name', name)?.trim()
          if (!next || next === name) return
          try {
            await playlistsApi.update(p.id, { name: next })
            toast.ok('Renamed')
          } catch (e: any) {
            toast.err(e?.data?.detail || 'Rename failed')
          }
        },
      },
      {
        label: 'Edit tags…',
        icon: 'bookmark',
        action: async () => {
          const raw = prompt('Tags (comma separated)', (row?.tags ?? []).join(', '))
          if (raw === null) return
          const tags = raw.split(',').map(t => t.trim()).filter(Boolean)
          try {
            await playlistsApi.update(p.id, { tags })
            toast.ok(tags.length ? 'Tags updated' : 'Tags cleared')
          } catch (e: any) {
            toast.err(e?.data?.detail || 'Tag update failed')
          }
        },
      },
      { label: '', separator: true },
      {
        label: 'Delete Playlist',
        icon: 'trash',
        action: async () => {
          const syncNote = row?.sync_services?.length
            ? `\n\nThis playlist syncs with ${row.sync_services.join(', ')} — deleting it stops the sync (the copy on the service stays).`
            : ''
          if (!confirm(`Delete “${name}”?${syncNote}`)) return
          try {
            await playlistsApi.remove(p.id)
            toast.ok('Playlist deleted')
          } catch (e: any) {
            toast.err(e?.data?.detail || 'Delete failed')
          }
        },
      },
    )
    return items
  }

  return { menuFor }
}
