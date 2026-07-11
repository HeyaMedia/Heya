import type { UseQueryReturn } from '@pinia/colada'

/**
 * Block a Nuxt page boundary only for a cold query. Cached data renders
 * immediately while Colada's mount/reconnect policies revalidate it in the
 * background, which keeps back/forward navigation instant.
 */
export async function waitForQuery(query: Pick<UseQueryReturn<any, any, any>, 'data' | 'refresh'>) {
  if (query.data.value === undefined) await query.refresh()
}
