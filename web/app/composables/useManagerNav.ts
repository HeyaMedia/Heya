// Information architecture for /manager — the acquisition/ops surface.
// Single source of truth for the desktop rail, the phone nav sheet, and page
// titles, mirroring useSettingsNav. The Libraries group is dynamic: it lists
// the user's actual libraries (so Anime is naturally separate from TV, and a
// second Movies library would just appear), keyed by library id.
import { librariesQuery } from '~/queries/catalog'

export type ManagerNavItem = {
  to: string
  label: string
  icon: string
}

export type ManagerNavGroup = {
  id: string
  label: string | null
  items: ManagerNavItem[]
}

export function managerLibraryIcon(mediaType: string): string {
  switch (mediaType) {
    case 'movie': return 'film'
    case 'tv':
    case 'anime': return 'tv'
    case 'music': return 'music'
    case 'book': return 'book'
    default: return 'folder'
  }
}

export function useManagerNav() {
  const { data: libraries } = useQuery(librariesQuery())

  const groups = computed<ManagerNavGroup[]>(() => [
    {
      id: 'ops',
      label: null,
      items: [
        { to: '/manager', label: 'Activity', icon: 'pulse' },
        { to: '/manager/add', label: 'Add new', icon: 'plus' },
        { to: '/manager/calendar', label: 'Calendar', icon: 'calendar' },
        { to: '/manager/queue', label: 'Queue', icon: 'cloud-download' },
        { to: '/manager/wanted', label: 'Wanted', icon: 'target' },
        { to: '/manager/history', label: 'History', icon: 'clock' },
      ],
    },
    {
      id: 'libraries',
      label: 'Libraries',
      items: (libraries.value ?? []).map(l => ({
        to: `/manager/library/${l.id}`,
        label: l.name,
        icon: managerLibraryIcon(l.media_type),
      })),
    },
    {
      id: 'system',
      label: 'System',
      items: [
        { to: '/manager/system/indexers', label: 'Indexers', icon: 'search' },
        { to: '/manager/system/download-clients', label: 'Download clients', icon: 'download' },
        { to: '/manager/system/quality-profiles', label: 'Quality profiles', icon: 'eq' },
        { to: '/manager/system/custom-formats', label: 'Custom formats', icon: 'hash' },
        { to: '/manager/system/lists', label: 'Import lists', icon: 'list' },
      ],
    },
  ])

  const titleByPath = computed(() => {
    const map = new Map<string, string>()
    for (const group of groups.value) {
      for (const item of group.items) map.set(item.to, item.label)
    }
    return map
  })

  return { groups, titleByPath }
}
