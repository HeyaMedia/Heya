import { defineQueryOptions } from '@pinia/colada'
import type { MediaItem } from '~~/shared/types'

export interface UserListDetail {
  list: {
    id: number
    name: string
    description: string
  }
  items: MediaItem[]
}

export const userListDetailQuery = defineQueryOptions((id: number) => ({
  key: ['me', 'lists', id],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/me/lists/{id}', { path: { id } }) as UserListDetail
  },
  staleTime: 1000 * 60,
  meta: {
    prefetch: 'intent',
    persistence: 'offline-essential',
    sensitivity: 'private',
  },
}))
