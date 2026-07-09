import type { MediaItem, ContextMenuItem, UserList } from '~~/shared/types'

// Composes the standard "card right-click" menu shared between the Movies
// and TV grid pages. The menu UI itself is now reka-driven (AppContextMenu);
// this helper just builds the items array so the same set of actions stays
// consistent across both grids.
export interface CardContextOpts {
  watchedSet?: Set<number>
  favoritedSet: Set<number>
  userLists: UserList[]
  onToggleWatched?: (id: number, watched: boolean, item: MediaItem) => void
  onToggleFavorite: (id: number, favorited: boolean) => void
  onAddToList: (listId: number, mediaId: number) => void
}

export function useCardContextItems() {
  function buildItems(item: MediaItem, opts: CardContextOpts): ContextMenuItem[] {
    const isWatched = opts.watchedSet?.has(item.id) ?? false
    const isFavorited = opts.favoritedSet.has(item.id)

    const listSubmenu: ContextMenuItem[] = opts.userLists
      .filter(l => l.list_type === 'manual')
      .map(l => ({
        label: l.name,
        icon: 'bookmark',
        action: () => opts.onAddToList(l.id, item.id),
      }))

    const items: ContextMenuItem[] = [
      {
        label: 'View Details',
        icon: 'info',
        action: () => navigateTo(mediaUrl(item)),
      },
      { separator: true, label: '' },
      {
        label: isFavorited ? 'Remove from Loved' : 'Add to Loved',
        icon: isFavorited ? 'heartfill' : 'heart',
        action: () => opts.onToggleFavorite(item.id, !isFavorited),
      },
    ]

    if (opts.watchedSet && opts.onToggleWatched) {
      items.splice(2, 0, {
        label: isWatched ? 'Mark Unwatched' : 'Mark Watched',
        icon: 'eye',
        action: () => opts.onToggleWatched?.(item.id, !isWatched, item),
      })
    }

    if (listSubmenu.length > 0) {
      items.push({ separator: true, label: '' })
      items.push({ label: 'Add to List', icon: 'plus', submenu: listSubmenu })
    }

    return items
  }

  return { buildItems }
}
