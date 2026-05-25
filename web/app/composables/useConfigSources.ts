// useConfigSources fetches the per-field provenance map exposed by
// /api/config/sources once per session and caches it in useState. Every
// settings input that may be env-locked queries `isLocked(key)` and
// `lockTooltip(key)` to drive its `disabled` / tooltip props — no
// per-field plumbing on the Vue side.

export type ConfigSource = 'env' | 'db' | 'default'

export interface ConfigSourceEntry {
  source: ConfigSource
  env_var?: string
}

export type ConfigSourcesMap = Record<string, ConfigSourceEntry>

export function useConfigSources() {
  const sources = useState<ConfigSourcesMap | null>('config_sources', () => null)
  const loading = useState<boolean>('config_sources_loading', () => false)
  const loaded = useState<boolean>('config_sources_loaded', () => false)

  async function refresh() {
    loading.value = true
    try {
      const { $heya } = useNuxtApp()
      sources.value = await $heya('/api/config/sources') as ConfigSourcesMap
      loaded.value = true
    } catch {
      sources.value = sources.value ?? {}
      loaded.value = true
    } finally {
      loading.value = false
    }
  }

  // Ensure resolves once per session — multiple components calling it
  // in parallel still produce a single network request.
  async function ensure() {
    if (loaded.value) return
    if (!loading.value) {
      await refresh()
      return
    }
    await new Promise<void>((resolve) => {
      const stop = watch(loading, (v) => {
        if (!v) {
          stop()
          resolve()
        }
      })
    })
  }

  function isLocked(key: string): boolean {
    return sources.value?.[key]?.source === 'env'
  }

  function lockTooltip(key: string): string {
    const entry = sources.value?.[key]
    if (entry?.source !== 'env') return ''
    return entry.env_var
      ? `Locked by environment variable ${entry.env_var}`
      : 'Locked by environment variable'
  }

  return { sources, loading, loaded, refresh, ensure, isLocked, lockTooltip }
}
