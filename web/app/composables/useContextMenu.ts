import type { EnrichedMediaItem, ContextMenuItem, UserList } from '~~/shared/types'

const menuState = reactive({
  visible: false,
  x: 0,
  y: 0,
  items: [] as ContextMenuItem[],
})

export function useContextMenu() {
  function showMenu(
    event: MouseEvent,
    item: EnrichedMediaItem,
    opts: {
      watchedSet: Set<number>
      favoritedSet: Set<number>
      userLists: UserList[]
      onToggleWatched: (id: number, watched: boolean) => void
      onToggleFavorite: (id: number, favorited: boolean) => void
      onAddToList: (listId: number, mediaId: number) => void
    },
  ) {
    const isWatched = opts.watchedSet.has(item.id)
    const isFavorited = opts.favoritedSet.has(item.id)

    const listSubmenu: ContextMenuItem[] = opts.userLists
      .filter(l => l.list_type === 'manual')
      .map(l => ({
        label: l.name,
        icon: 'bookmark',
        action: () => opts.onAddToList(l.id, item.id),
      }))

    menuState.items = [
      {
        label: 'View Details',
        icon: 'info',
        action: () => navigateTo(mediaUrl(item)),
      },
      { separator: true, label: '' },
      {
        label: isWatched ? 'Mark Unwatched' : 'Mark Watched',
        icon: isWatched ? 'eye' : 'eye',
        action: () => opts.onToggleWatched(item.id, !isWatched),
      },
      {
        label: isFavorited ? 'Remove from Loved' : 'Add to Loved',
        icon: isFavorited ? 'heartfill' : 'heart',
        action: () => opts.onToggleFavorite(item.id, !isFavorited),
      },
      ...(listSubmenu.length > 0
        ? [{ separator: true, label: '' }, { label: 'Add to List', icon: 'plus', submenu: listSubmenu }]
        : []),
    ]

    menuState.x = event.clientX
    menuState.y = event.clientY
    menuState.visible = true
  }

  function closeMenu() {
    menuState.visible = false
  }

  return { menuState, showMenu, closeMenu }
}
