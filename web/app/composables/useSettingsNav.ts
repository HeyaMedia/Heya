// Information architecture for /settings. This is the single source of truth
// for the desktop rail, mobile sheet, page titles, and personal quick links.

export type SettingsNavItem = {
  to: string
  label: string
  icon: string
  tabs?: SettingsNavTab[]
  aliases?: string[]
  applicationOnly?: boolean
}

export type SettingsNavTab = {
  to: string
  label: string
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
    label: 'Personal',
    items: [
      { to: '/settings/profile', label: 'Profile', icon: 'user' },
      { to: '/settings/playback', label: 'Playback', icon: 'eq' },
      { to: '/settings/services', label: 'Music services', icon: 'music' },
      { to: '/settings/appearance', label: 'Appearance', icon: 'brightness' },
      { to: '/settings/device', label: 'This device', icon: 'cpu' },
      { to: '/settings/application', label: 'Application', icon: 'settings', applicationOnly: true },
      { to: '/settings/sessions', label: 'My sessions', icon: 'eye' },
      { to: '/settings/tokens', label: 'API tokens', icon: 'key' },
    ],
  },
  {
    id: 'overview',
    label: 'Overview',
    adminOnly: true,
    items: [
      {
        to: '/settings/dashboard', label: 'Dashboard', icon: 'pulse',
        aliases: ['/settings/activity'],
      },
    ],
  },
  {
    id: 'media',
    label: 'Media',
    adminOnly: true,
    items: [
      {
        to: '/settings/libraries', label: 'Libraries', icon: 'folder',
        tabs: [
          { to: '/settings/libraries', label: 'Libraries' },
          { to: '/settings/watchers', label: 'Watchers' },
        ],
      },
      {
        to: '/settings/metadata', label: 'Metadata', icon: 'refresh',
        tabs: [
          { to: '/settings/metadata', label: 'Policies' },
          { to: '/settings/providers', label: 'Providers' },
          { to: '/settings/metadata-editor', label: 'Editor' },
        ],
      },
      { to: '/settings/transcoding', label: 'Transcoding', icon: 'film' },
      {
        to: '/settings/intelligence', label: 'Intelligence', icon: 'sparkle',
        tabs: [
          { to: '/settings/intelligence', label: 'Overview' },
          { to: '/settings/recommendations', label: 'Recommendations' },
          { to: '/settings/sonic', label: 'Sonic analysis' },
          { to: '/settings/ai', label: 'AI providers' },
          { to: '/settings/images', label: 'Image generation' },
        ],
      },
    ],
  },
  {
    id: 'server',
    label: 'Server',
    adminOnly: true,
    items: [
      {
        to: '/settings/jobs', label: 'Jobs & automation', icon: 'list',
        tabs: [
          { to: '/settings/jobs', label: 'Job queue' },
          { to: '/settings/tasks', label: 'Scheduled tasks' },
        ],
      },
      { to: '/settings/storage', label: 'Storage', icon: 'hard-drives' },
      { to: '/settings/network', label: 'Network', icon: 'network' },
      {
        to: '/settings/users', label: 'Users & access', icon: 'users',
        tabs: [
          { to: '/settings/users', label: 'Users' },
          { to: '/settings/all-sessions', label: 'All sessions' },
        ],
      },
      { to: '/settings/cast', label: 'Casting', icon: 'cast' },
      {
        to: '/settings/jellyfin', label: 'Client APIs', icon: 'link',
        tabs: [
          { to: '/settings/jellyfin', label: 'Jellyfin' },
          { to: '/settings/subsonic', label: 'Subsonic' },
        ],
      },
    ],
  },
  {
    id: 'advanced',
    label: 'Advanced',
    adminOnly: true,
    items: [
      { to: '/settings/configuration', label: 'Configuration', icon: 'settings' },
      {
        to: '/settings/diagnostics', label: 'Diagnostics', icon: 'pulse',
        tabs: [
          { to: '/settings/diagnostics', label: 'Dashboard' },
          { to: '/settings/runtime', label: 'Runtime' },
          { to: '/settings/workers', label: 'Workers' },
          { to: '/settings/traffic', label: 'API & WS' },
          { to: '/settings/logs', label: 'Logs' },
          { to: '/settings/database', label: 'Database' },
        ],
      },
      { to: '/settings/about', label: 'About Heya', icon: 'info' },
    ],
  },
]

export function useSettingsNav() {
  const { user } = useAuth()
  const { applicationAvailable } = useApplicationBridge()
  const isAdmin = computed(() => user.value?.is_admin === true)

  const groups = computed(() => ALL_GROUPS
    .filter(group => !group.adminOnly || isAdmin.value)
    .map(group => ({
      ...group,
      items: group.items.filter(item => !item.applicationOnly || applicationAvailable.value),
    }))
    .filter(group => group.items.length > 0))

  // Every tab route resolves both to its own display label and to the stable
  // sidebar destination that owns it.
  const itemByPath = computed(() => {
    const map = new Map<string, { group: SettingsNavGroup, item: SettingsNavItem }>()
    for (const group of groups.value) {
      for (const item of group.items) {
        map.set(item.to, { group, item })
        for (const alias of item.aliases ?? []) map.set(alias, { group, item })
        for (const tab of item.tabs ?? []) {
          map.set(tab.to, { group, item: { ...item, to: tab.to, label: tab.label } })
        }
      }
    }
    return map
  })

  const sectionByPath = computed(() => {
    const map = new Map<string, SettingsNavItem>()
    for (const group of groups.value) {
      for (const item of group.items) {
        map.set(item.to, item)
        for (const alias of item.aliases ?? []) map.set(alias, item)
        for (const tab of item.tabs ?? []) map.set(tab.to, item)
      }
    }
    return map
  })

  return { groups, isAdmin, itemByPath, sectionByPath }
}
