import type { PiniaColadaOptions } from '@pinia/colada'
import { PiniaColadaAutoRefetch } from '@pinia/colada-plugin-auto-refetch'
import { PiniaColadaRetry } from '@pinia/colada-plugin-retry'

// Heya is a long-running client application. Keep recently visited media in
// memory so back/forward navigation is instant, but treat it as a disposable
// cache: live events and mutations still invalidate the affected query trees.
// The serializable query metadata added in app/types/pinia-colada.d.ts is the
// seam for selectively persisting safe data to IndexedDB in a later pass.
export default {
  queryOptions: {
    staleTime: 1000 * 60,
    gcTime: 1000 * 60 * 30,
    refetchOnWindowFocus: true,
    refetchOnReconnect: true,
  },
  plugins: [
    PiniaColadaAutoRefetch(),
    PiniaColadaRetry({ retry: 1 }),
  ],
} satisfies PiniaColadaOptions
