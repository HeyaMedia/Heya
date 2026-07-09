// Information architecture for /settings. Single source of truth — the
// sidebar reads this and the admin middleware uses the `adminOnly` flag to
// gate routes. Add/move/rename items here, no other file touched.

export type SettingsNavItem = {
  to: string
  label: string
  icon: string
}

export type SettingsNavGroup = {
  id: string
  label: string
  adminOnly?: boolean
  items: SettingsNavItem[]
}

const ALL_GROUPS: SettingsNavGroup[] = [
  {
    id: 'you',
    label: 'You',
    items: [
      { to: '/settings/profile',    label: 'Profile',     icon: 'user' },
      { to: '/settings/playback',   label: 'Playback',    icon: 'eq' },
      { to: '/settings/device',     label: 'Device',      icon: 'cpu' },
      { to: '/settings/appearance', label: 'Appearance',  icon: 'brightness' },
      { to: '/settings/sessions',   label: 'My sessions', icon: 'eye' },
      { to: '/settings/lists',      label: 'My lists',    icon: 'bookmark' },
      { to: '/settings/tokens',     label: 'API tokens',  icon: 'key' },
    ],
  },
  {
    id: 'overview',
    label: 'Overview',
    adminOnly: true,
    items: [
      { to: '/settings/dashboard', label: 'Dashboard', icon: 'pulse' },
    ],
  },
  {
    id: 'activity',
    label: 'Activity',
    adminOnly: true,
    items: [
      { to: '/settings/activity', label: 'Now Playing', icon: 'cast' },
      { to: '/settings/jobs',     label: 'Jobs',        icon: 'list' },
      { to: '/settings/tasks',    label: 'Tasks',       icon: 'timer' },
      { to: '/settings/watchers', label: 'Watchers',    icon: 'eye' },
      { to: '/settings/logs',     label: 'Logs',        icon: 'clipboard' },
    ],
  },
  {
    id: 'content',
    label: 'Content',
    adminOnly: true,
    items: [
      { to: '/settings/libraries',   label: 'Libraries',      icon: 'folder' },
      { to: '/settings/providers',   label: 'Providers',      icon: 'database' },
      { to: '/settings/metadata',    label: 'Metadata',       icon: 'refresh' },
      { to: '/settings/transcoding', label: 'Transcoding',    icon: 'film' },
      { to: '/settings/sonic',       label: 'Sonic analysis', icon: 'eq' },
      { to: '/settings/recommendations', label: 'Recommendations', icon: 'sparkle' },
    ],
  },
  {
    id: 'access',
    label: 'Access',
    adminOnly: true,
    items: [
      { to: '/settings/users',        label: 'Users',         icon: 'users' },
      { to: '/settings/all-sessions', label: 'All sessions',  icon: 'eye' },
    ],
  },
  {
    id: 'system',
    label: 'System',
    adminOnly: true,
    items: [
      { to: '/settings/configuration', label: 'Configuration', icon: 'settings' },
      { to: '/settings/network',       label: 'Network',       icon: 'network' },
      { to: '/settings/jellyfin',      label: 'Jellyfin API',  icon: 'cast' },
      { to: '/settings/storage',       label: 'Storage',       icon: 'hard-drives' },
      { to: '/settings/database',      label: 'Database',      icon: 'database' },
      { to: '/settings/diagnostics',   label: 'Diagnostics',   icon: 'cpu' },
      { to: '/settings/about',         label: 'About',         icon: 'info' },
    ],
  },
]

export function useSettingsNav() {
  const { user } = useAuth()
  const isAdmin = computed(() => user.value?.is_admin === true)

  const groups = computed(() =>
    ALL_GROUPS.filter(g => !g.adminOnly || isAdmin.value),
  )

  // Flat lookup for breadcrumb / page-title resolution
  const itemByPath = computed(() => {
    const map = new Map<string, { group: SettingsNavGroup, item: SettingsNavItem }>()
    for (const g of groups.value) for (const it of g.items) map.set(it.to, { group: g, item: it })
    return map
  })

  return { groups, isAdmin, itemByPath }
}
