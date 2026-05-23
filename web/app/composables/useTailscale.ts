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
  funnel: boolean
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
      const res = await apiFetch<TailscaleResponse>('/api/tailscale/status')
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
    await apiFetch<{ status: string }>('/api/tailscale/config', {
      method: 'PUT',
      body: JSON.stringify(merged),
      headers: { 'Content-Type': 'application/json' },
    })
    await refresh()
  }

  async function setFunnel(on: boolean) {
    await apiFetch<{ funnel: boolean }>('/api/tailscale/funnel', {
      method: 'POST',
      body: JSON.stringify({ enabled: on }),
      headers: { 'Content-Type': 'application/json' },
    })
    await refresh()
  }

  async function logout() {
    await apiFetch<{ status: string }>('/api/tailscale/logout', { method: 'POST' })
    await refresh()
  }

  function subscribeToEvents() {
    const bus = useEventBus()
    return bus.on('tailscale.status', (ev) => {
      status.value = ev.payload as TailscaleStatus
    })
  }

  return { enabled, status, cfg, message, loading, refresh, saveConfig, setFunnel, logout, subscribeToEvents }
}
