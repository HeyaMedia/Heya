export interface TailscaleStatus {
  enabled: boolean
  running: boolean
  hostname: string
  backend_state: string
  magic_dns?: string
  ipv4?: string
  ipv6?: string
  cert_domain?: string
  https: boolean
  https_active: boolean
  https_url?: string
  funnel: boolean
  funnel_active: boolean
  funnel_url?: string
  login_url?: string
  last_error?: string
  updated_at: string
}

export interface TailscaleConfig {
  enabled: boolean
  hostname: string
  state_dir?: string
  https: boolean
  funnel: boolean
}

export interface TailscaleResponse {
  enabled: boolean
  config?: TailscaleConfig
  status?: TailscaleStatus
  message?: string
}

export function useTailscale() {
  const enabled = useState<boolean>('ts_enabled', () => false)
  const status = useState<TailscaleStatus | null>('ts_status', () => null)
  const cfg = useState<TailscaleConfig | null>('ts_config', () => null)
  const message = useState<string>('ts_message', () => '')
  const loading = useState<boolean>('ts_loading', () => false)

  async function refresh() {
    loading.value = true
    try {
      const { $heya } = useNuxtApp()
      const res = await $heya('/api/tailscale/status') as TailscaleResponse
      enabled.value = res.enabled
      status.value = res.status ?? null
      cfg.value = res.config ?? null
      message.value = res.message ?? ''
    } finally {
      loading.value = false
    }
  }

  async function saveConfig(patch: Partial<TailscaleConfig>) {
    const merged: TailscaleConfig = {
      enabled: cfg.value?.enabled ?? false,
      hostname: cfg.value?.hostname ?? 'heya',
      https: cfg.value?.https ?? true,
      funnel: cfg.value?.funnel ?? false,
      ...patch,
    }
    const { $heya } = useNuxtApp()
    await $heya('/api/tailscale/config', {
      method: 'PUT',
      body: merged as any,
    })
    await refresh()
  }

  async function setFunnel(on: boolean) {
    const baseline = Date.parse(status.value?.updated_at ?? '') || Date.now()
    const { $heya } = useNuxtApp()
    await $heya('/api/tailscale/funnel', {
      method: 'POST',
      body: { enabled: on } as any,
    })

    // The server applies this in the background because the request itself
    // may be travelling over the listener that Caddy needs to replace. Wait
    // for a newer status snapshot so callers get the real listener result,
    // not merely the persisted preference returned by the POST.
    const deadline = Date.now() + 20_000
    while (Date.now() < deadline) {
      await refresh()
      const current = status.value
      const updated = Date.parse(current?.updated_at ?? '')
      if (current && Number.isFinite(updated) && updated > baseline) {
        if (on && current.funnel_active) return
        if (!on && !current.funnel && !current.funnel_active) return
        if (current.last_error) throw new Error(current.last_error)
      }
      await new Promise(resolve => setTimeout(resolve, 400))
    }

    throw new Error(status.value?.last_error || 'Tailscale did not finish applying the Funnel change in time.')
  }

  async function logout() {
    const { $heya } = useNuxtApp()
    await $heya('/api/tailscale/logout', { method: 'POST' })
    await refresh()
  }

  async function fetchRaw() {
    const { $heya } = useNuxtApp()
    return await $heya('/api/tailscale/raw') as Record<string, unknown>
  }

  function subscribeToEvents() {
    const bus = useEventBus()
    return bus.on('tailscale.status', (ev) => {
      status.value = ev.payload as TailscaleStatus
    })
  }

  return { enabled, status, cfg, message, loading, refresh, saveConfig, setFunnel, logout, fetchRaw, subscribeToEvents }
}
