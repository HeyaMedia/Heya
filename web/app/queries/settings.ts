import { defineQueryOptions } from '@pinia/colada'
import type { components } from '#open-fetch-schemas/heya'

export type AuthSession = components['schemas']['AuthSessionView']
export type ApiToken = components['schemas']['ApiTokenView']
export type AdminSession = components['schemas']['AdminSessionView']
export type AdminUser = components['schemas']['AdminUserView']
export type AdminStorage = components['schemas']['AdminStorageBody']
export type AdminListeners = components['schemas']['AdminListenersBody']
export type WatcherStatus = components['schemas']['WatcherStatusBody']
export type JellyfinConfig = components['schemas']['JellyfinConfigBody']
export type SubsonicConfig = components['schemas']['SubsonicConfigBody']
export type SubsonicCredential = components['schemas']['SubsonicCredentialBody']
export type Library = components['schemas']['LibraryView']
export type LibrarySettings = components['schemas']['LibrarySettings']
export type UserSettings = components['schemas']['UserSettings']
export type UserListView = components['schemas']['UserListView']
export type LogEntry = components['schemas']['Entry']
export type JobListResult = components['schemas']['JobListResult']
export type JobKindSummary = components['schemas']['JobKindSummaryRow']
export type MusicServiceImportState = { status?: string, imported?: number, matched?: number, unmatched?: number, scanned?: number, error?: string }
export type MusicService = {
  service: 'listenbrainz' | 'lastfm'
  username: string
  token_set: boolean
  scrobble_enabled: boolean
  import_state: MusicServiceImportState
}

export type PlaylistServiceCatalog = {
  service: 'listenbrainz' | 'lastfm'
  capabilities: { available: boolean, read: boolean, write: boolean, reason?: string }
  playlists: Array<{
    external_id: string
    name: string
    description?: string
    url?: string
    updated_at?: string
    track_count: number
    local_playlist_id?: number
    sync_mode?: 'two_way' | 'pull_only'
  }>
  collections: Array<{
    key: string
    name: string
    description?: string
    auto_sync: boolean
    playlists: Array<{
      external_id: string
      name: string
      description?: string
      url?: string
      updated_at?: string
      track_count: number
      local_playlist_id?: number
      sync_mode?: 'two_way' | 'pull_only'
    }>
  }>
}

export type ConfigSourceEntry = { source: string, env_var?: string }
export type ConfigSources = Record<string, ConfigSourceEntry>
export type OpenSubtitlesSettings = { api_key?: string, username?: string, password?: string }

const privateSettings = {
  prefetch: 'none',
  persistence: 'none',
  sensitivity: 'secret',
} as const

export const mySessionsQuery = defineQueryOptions(() => ({
  key: ['me', 'auth-sessions'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/me/auth-sessions') as AuthSession[] | null) ?? []
  },
  staleTime: 1000 * 15,
  meta: privateSettings,
}))

export const myApiTokensQuery = defineQueryOptions(() => ({
  key: ['me', 'api-tokens'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/me/api-tokens') as ApiToken[] | null) ?? []
  },
  staleTime: 1000 * 15,
  meta: privateSettings,
}))

export const userPlaybackSettingsQuery = defineQueryOptions(() => ({
  key: ['me', 'settings'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/me/settings') as UserSettings
  },
  staleTime: 1000 * 30,
  meta: privateSettings,
}))

export const musicServicesQuery = defineQueryOptions(() => ({
  key: ['me', 'music-services'],
  query: async () => {
    const { $heya } = useNuxtApp()
    const response = await $heya('/api/me/music-services') as { services?: MusicService[] }
    return response.services ?? []
  },
  staleTime: 1000 * 15,
  meta: privateSettings,
}))

export const managedUserListsQuery = defineQueryOptions(() => ({
  key: ['me', 'lists'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/me/lists') as UserListView[] | null) ?? []
  },
  staleTime: 1000 * 30,
  meta: privateSettings,
}))

export const adminUsersQuery = defineQueryOptions(() => ({
  key: ['admin', 'users'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/admin/users') as AdminUser[] | null) ?? []
  },
  staleTime: 1000 * 15,
  meta: privateSettings,
}))

export const adminSessionsQuery = defineQueryOptions(() => ({
  key: ['admin', 'sessions'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/admin/sessions') as AdminSession[] | null) ?? []
  },
  staleTime: 1000 * 15,
  meta: privateSettings,
}))

export const adminStorageQuery = defineQueryOptions(() => ({
  key: ['admin', 'storage'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/admin/storage') as AdminStorage
  },
  staleTime: 1000 * 15,
  meta: privateSettings,
}))

export const adminListenersQuery = defineQueryOptions(() => ({
  key: ['admin', 'listeners'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/admin/listeners') as AdminListeners
  },
  staleTime: 1000 * 15,
  meta: privateSettings,
}))

export const watcherStatusQuery = defineQueryOptions(() => ({
  key: ['admin', 'watchers'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/watchers') as WatcherStatus
  },
  staleTime: 1000 * 5,
  meta: privateSettings,
}))

export const configSourcesQuery = defineQueryOptions(() => ({
  key: ['admin', 'config-sources'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/config/sources') as ConfigSources
  },
  staleTime: 1000 * 30,
  meta: privateSettings,
}))

export const jellyfinConfigQuery = defineQueryOptions(() => ({
  key: ['admin', 'client-api', 'jellyfin'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/jellyfin/config') as JellyfinConfig
  },
  staleTime: 1000 * 30,
  meta: privateSettings,
}))

export const subsonicConfigQuery = defineQueryOptions(() => ({
  key: ['admin', 'client-api', 'subsonic'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/subsonic/config') as SubsonicConfig
  },
  staleTime: 1000 * 30,
  meta: privateSettings,
}))

export const subsonicCredentialQuery = defineQueryOptions(() => ({
  key: ['me', 'subsonic-credential'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/me/subsonic-credential') as SubsonicCredential
  },
  staleTime: 1000 * 30,
  meta: privateSettings,
}))

export const metadataPoliciesQuery = defineQueryOptions(() => ({
  key: ['admin', 'metadata', 'policies'],
  query: async () => {
    const { $heya } = useNuxtApp()
    const libraries = (await $heya('/api/libraries') as Library[] | null) ?? []
    const pairs = await Promise.all(libraries.map(async (library) => {
      try {
        const response = await $heya('/api/libraries/{id}/settings', { path: { id: library.id } })
        return [library.id, response.settings as LibrarySettings] as const
      } catch {
        return [library.id, null] as const
      }
    }))
    return {
      libraries,
      settings: Object.fromEntries(pairs.filter((pair): pair is readonly [number, LibrarySettings] => pair[1] != null)),
    }
  },
  staleTime: 1000 * 30,
  meta: privateSettings,
}))

export const openSubtitlesSettingsQuery = defineQueryOptions(() => ({
  key: ['admin', 'metadata', 'opensubtitles'],
  query: async () => {
    const { $heya } = useNuxtApp()
    const response = await $heya('/api/system-settings/{key}', { path: { key: 'opensubtitles' } }) as { value?: OpenSubtitlesSettings }
    return response.value ?? {}
  },
  staleTime: 1000 * 30,
  meta: privateSettings,
}))

export const adminLogsQuery = defineQueryOptions((limit: number) => ({
  key: ['admin', 'logs', limit],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/logs', { query: { n: limit } }) as LogEntry[] | null) ?? []
  },
  staleTime: 1000 * 5,
  meta: privateSettings,
}))

export const adminJobsQuery = defineQueryOptions((target: { state: string, kind: string, offset: number, limit: number }) => ({
  key: ['admin', 'jobs', target.state || 'all', target.kind || 'all', target.offset, target.limit],
  query: async () => {
    const { $heya } = useNuxtApp()
    const query: Record<string, string | number> = { limit: target.limit, offset: target.offset }
    if (target.state) query.state = target.state
    if (target.kind) query.kind = target.kind
    return await $heya('/api/jobs', { query }) as JobListResult
  },
  staleTime: 1000 * 3,
  meta: privateSettings,
}))

export const adminJobKindsQuery = defineQueryOptions(() => ({
  key: ['admin', 'jobs', 'kinds'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return (await $heya('/api/jobs/kinds') as JobKindSummary[] | null) ?? []
  },
  staleTime: 1000 * 15,
  meta: privateSettings,
}))
