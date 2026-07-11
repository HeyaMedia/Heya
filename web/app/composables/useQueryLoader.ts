import { useQueryCache, type UseQueryOptions } from '@pinia/colada'

/** Imperative reads (Play Album, drag/drop, etc.) share the exact same cache
 * entry as their destination page instead of issuing an unrelated request. */
export function useQueryLoader() {
  const queryCache = useQueryCache()

  return async function loadQuery<T>(options: UseQueryOptions<T>): Promise<T> {
    const entry = queryCache.ensure(options)
    const state = await queryCache.refresh(entry)
    if (state.status === 'error') throw state.error
    return state.data as T
  }
}
