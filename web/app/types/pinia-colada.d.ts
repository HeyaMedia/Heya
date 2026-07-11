import '@pinia/colada'
import '@pinia/colada-plugin-auto-refetch'
import '@pinia/colada-plugin-retry'

declare module '@pinia/colada' {
  interface TypesConfig {
    queryMeta: {
      /** How eagerly navigation affordances may warm this query. */
      prefetch?: 'none' | 'intent' | 'visible' | 'immediate'
      /** Future IndexedDB/native-cache policy; persistence is not enabled yet. */
      persistence?: 'none' | 'session' | 'device' | 'offline-essential'
      /** Prevent future persistence plugins from storing sensitive results. */
      sensitivity?: 'normal' | 'private' | 'secret'
    }
  }
}

export {}
