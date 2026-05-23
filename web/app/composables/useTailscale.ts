export interface TailscaleStatus {
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

export interface TailscaleResponse {
  enabled: boolean
  status?: TailscaleStatus
  message?: string
}

export function useTailscale() {
  const enabled = useState<boolean>('ts_enabled', () => false)
  const status = useState<TailscaleStatus | null>('ts_status', () => null)
  const message = useState<string>('ts_message', () => '')
  const loading = useState<boolean>('ts_loading', () => false)

  async function refresh() {
    loading.value = true
    try {
      const res = await apiFetch<TailscaleResponse>('/api/tailscale/status')
      enabled.value = res.enabled
      status.value = res.status ?? null
      message.value = res.message ?? ''
    } finally {
      loading.value = false
    }
  }

  async function setFunnel(on: boolean) {
    return apiFetch<{ funnel: boolean, note: string }>('/api/tailscale/funnel', {
      method: 'POST',
      body: JSON.stringify({ enabled: on }),
      headers: { 'Content-Type': 'application/json' },
    })
  }

  async function logout() {
    return apiFetch<{ status: string }>('/api/tailscale/logout', { method: 'POST' })
  }

  function subscribeToEvents() {
    const bus = useEventBus()
    return bus.on('tailscale.status', (ev) => {
      status.value = ev.payload as TailscaleStatus
      enabled.value = true
    })
  }

  return { enabled, status, message, loading, refresh, setFunnel, logout, subscribeToEvents }
}
