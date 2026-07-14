import type {
  RemoteConfigView,
  RemoteStatus,
  RemoteStatusBody,
} from '~~/shared/api/types.gen'

export type { RemoteConfigView, RemoteStatus }

/** Fields accepted by PUT /api/remote/config. dns_token is write-only:
 *  omit (or send '') to keep the stored token. */
export interface RemoteConfigPatch {
  enabled?: boolean
  port?: number
  acme_email?: string
  dns_provider?: string
  dns_token?: string
  domain?: string
  subdomain?: string
}

export function useRemoteAccess() {
  const available = useState<boolean>('remote_available', () => true)
  const cfg = useState<RemoteConfigView | null>('remote_config', () => null)
  const status = useState<RemoteStatus | null>('remote_status', () => null)
  const message = useState<string>('remote_message', () => '')
  const loading = useState<boolean>('remote_loading', () => false)

  function apply(res: RemoteStatusBody) {
    available.value = res.available
    cfg.value = res.config ?? null
    status.value = res.status ?? null
    message.value = res.message ?? ''
  }

  async function refresh() {
    loading.value = true
    try {
      const { $heya } = useNuxtApp()
      apply(await $heya('/api/remote/status') as RemoteStatusBody)
    } finally {
      loading.value = false
    }
  }

  async function saveConfig(patch: RemoteConfigPatch) {
    const merged = {
      enabled: cfg.value?.enabled ?? false,
      port: cfg.value?.port ?? 0,
      acme_email: cfg.value?.acme_email ?? '',
      dns_provider: cfg.value?.dns_provider ?? '',
      domain: cfg.value?.domain ?? '',
      subdomain: cfg.value?.subdomain ?? '',
      ...patch,
    }
    const { $heya } = useNuxtApp()
    await $heya('/api/remote/config', { method: 'PUT', body: merged as any })
    await refresh()
  }

  /** Synchronous outside-in re-check; the response carries fresh status. */
  async function recheck() {
    const { $heya } = useNuxtApp()
    apply(await $heya('/api/remote/check', { method: 'POST' }) as RemoteStatusBody)
  }

  function subscribeToEvents() {
    const bus = useEventBus()
    return bus.on('remote.status', (ev) => {
      status.value = ev.payload as RemoteStatus
    })
  }

  return { available, cfg, status, message, loading, refresh, saveConfig, recheck, subscribeToEvents }
}
