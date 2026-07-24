import type {
  AdvertisementStatus,
  DiscoveryConfigView,
  DiscoveryStatusBody,
} from '~~/shared/api/types.gen'

export type { AdvertisementStatus, DiscoveryConfigView }

/** Fields accepted by PUT /api/discovery/config. Port/host/addresses/
 *  interfaces are env-only escape hatches and deliberately not settable here. */
export interface DiscoveryConfigPatch {
  enabled?: boolean
  name?: string
}

export function useDiscovery() {
  const available = useState<boolean>('discovery_available', () => true)
  const cfg = useState<DiscoveryConfigView | null>('discovery_config', () => null)
  const status = useState<AdvertisementStatus | null>('discovery_status', () => null)
  const message = useState<string>('discovery_message', () => '')
  const loading = useState<boolean>('discovery_loading', () => false)

  function apply(res: DiscoveryStatusBody) {
    available.value = res.available
    cfg.value = res.config ?? null
    status.value = res.status ?? null
    message.value = res.message ?? ''
  }

  async function refresh() {
    loading.value = true
    try {
      const { $heya } = useNuxtApp()
      apply(await $heya('/api/discovery/status') as DiscoveryStatusBody)
    } finally {
      loading.value = false
    }
  }

  /** The PUT response already carries fresh status — no follow-up GET. */
  async function saveConfig(patch: DiscoveryConfigPatch) {
    const merged = {
      enabled: cfg.value?.enabled ?? false,
      name: cfg.value?.name ?? '',
      ...patch,
    }
    const { $heya } = useNuxtApp()
    apply(await $heya('/api/discovery/config', { method: 'PUT', body: merged as any }) as DiscoveryStatusBody)
  }

  function subscribeToEvents() {
    const bus = useEventBus()
    return bus.on('discovery.status', (ev) => {
      status.value = ev.payload as AdvertisementStatus
    })
  }

  return { available, cfg, status, message, loading, refresh, saveConfig, subscribeToEvents }
}
