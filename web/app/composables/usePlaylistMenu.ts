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
  const { promptText } = usePrompt()
  const { confirm } = useConfirm()

  // `surface` keeps the two pin scopes from bleeding into each other: the
  // sidebar's menu shows a single "Pin" that only touches sidebar_pinned,
  // the playlists page's only touches pinned, and neutral surfaces (home
  // shelf, My Music) spell both out. A plain "Pin" in the sidebar acting on
  // the page scope is exactly the confusion this avoids.
  function menuFor(
    p: { id: number; name: string; slug?: string; track_count?: number },
    opts: { surface?: 'sidebar' | 'page' } = {},
  ): ContextMenuItem[] {
    const row = playlistsApi.playlists.value.find(r => r.id === p.id)
    const pinned = row?.pinned ?? false
    const sidebarPinned = row?.sidebar_pinned ?? false
    const name = row?.name ?? p.name

    const togglePin = (scope: 'page' | 'sidebar', next: boolean): ContextMenuItem['action'] =>
      async () => {
        try {
          await playlistsApi.setPin(p.id, scope, next)
        } catch { toast.err('Pin failed') }
      }

    const pinItems: ContextMenuItem[] =
      opts.surface === 'sidebar'
        ? [{ label: sidebarPinned ? 'Unpin' : 'Pin', icon: 'pin', action: togglePin('sidebar', !sidebarPinned) }]
        : opts.surface === 'page'
          ? [{ label: pinned ? 'Unpin' : 'Pin to top', icon: 'pin', action: togglePin('page', !pinned) }]
          : [
              { label: pinned ? 'Unpin from Playlists page' : 'Pin on Playlists page', icon: 'pin', action: togglePin('page', !pinned) },
              { label: sidebarPinned ? 'Unpin from sidebar' : 'Pin in sidebar', icon: 'pin', action: togglePin('sidebar', !sidebarPinned) },
            ]

    const items = actions.forPlaylist(p)
    items.push(
      { label: '', separator: true },
      ...pinItems,
      { label: '', separator: true },
      {
        label: 'Rename…',
        icon: 'pencil',
        action: async () => {
          const next = await promptText({
            title: 'Rename playlist',
            label: 'Playlist name',
            initial: name,
            confirmLabel: 'Rename',
          })
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
          const raw = await promptText({
            title: 'Edit tags',
            label: 'Tags (comma separated)',
            initial: (row?.tags ?? []).join(', '),
            placeholder: 'chill, focus, workout',
            allowEmpty: true,
            confirmLabel: 'Save tags',
          })
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
            ? `This playlist syncs with ${row.sync_services.join(', ')} — deleting it stops the sync (the copy on the service stays).`
            : 'This can’t be undone.'
          const ok = await confirm({
            title: `Delete “${name}”?`,
            message: syncNote,
            confirmLabel: 'Delete',
            destructive: true,
          })
          if (!ok) return
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
