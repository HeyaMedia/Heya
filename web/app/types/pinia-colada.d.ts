import '@pinia/colada'
import '@pinia/colada-plugin-auto-refetch'
import '@pinia/colada-plugin-retry'

declare module '@pinia/colada' {
  interface TypesConfig {
    queryMeta: {
      /** How eagerly navigation affordances may warm this query. */
      prefetch?: 'none' | 'intent' | 'visible' | 'immediate'
      /** IndexedDB/native-cache retention policy for successful query data. */
      persistence?: 'none' | 'session' | 'device' | 'offline-essential'
      /** Prevent the persistence layer from storing sensitive results. */
      sensitivity?: 'normal' | 'private' | 'secret'
    }
  }
}

export {}
