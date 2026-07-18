<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { adminNetworkStatusQuery } from '~/queries/settings'

const networkData = useQuery(adminNetworkStatusQuery())
const network = computed(() => networkData.data.value ?? null)
const http = computed(() => network.value?.ingress.http ?? null)
const loading = computed(() => networkData.isLoading.value && !network.value)
const rateHistory = ref<number[]>([])
const latencyHistory = ref<number[]>([])
let timer: ReturnType<typeof setInterval> | null = null

watch(() => networkData.data.value, (value) => {
  if (!value) return
  rateHistory.value.push(value.ingress.http.requests_per_second)
  latencyHistory.value.push(value.ingress.http.p95_latency_ms)
  if (rateHistory.value.length > 60) rateHistory.value.shift()
  if (latencyHistory.value.length > 60) latencyHistory.value.shift()
}, { immediate: true })

function fmtRate(value?: number) {
  if (value == null) return '—'
  return `${value < 10 ? value.toFixed(2) : value.toFixed(1)}/s`
}
function fmtMs(value?: number) {
  if (value == null) return '—'
  if (value < 1) return `${value.toFixed(2)} ms`
  if (value < 100) return `${value.toFixed(1)} ms`
  return `${Math.round(value).toLocaleString()} ms`
}

onMounted(() => { timer = setInterval(() => { void networkData.refetch() }, 3000) })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <div>
    <SettingsContextHero
      title="API & WebSocket"
      icon="network"
      eyebrow="Advanced · Transport diagnostics"
      description="Inspect API throughput, latency, failures, transfer volume, HTTP protocol usage, ingress paths, and live WebSocket clients."
    >
      <div class="context-fact"><strong>{{ fmtRate(http?.requests_per_second) }}</strong><span>request rate</span></div>
      <div class="context-fact"><strong>{{ network?.general.ws_subscribers ?? 0 }}</strong><span>WS clients</span></div>
    </SettingsContextHero>

    <div v-if="loading" class="loading-state"><Icon name="spinner" :size="15" /> Gathering transport metrics…</div>
    <template v-else-if="network && http">
      <div class="tiles">
        <MetricTile label="Requests" :value="fmtRate(http.requests_per_second)" icon="pulse" :sparkline="rateHistory" />
        <MetricTile label="p50 latency" :value="fmtMs(http.p50_latency_ms)" icon="timer" />
        <MetricTile label="p95 latency" :value="fmtMs(http.p95_latency_ms)" icon="timer"
          :tone="http.p95_latency_ms >= 2000 ? 'bad' : http.p95_latency_ms >= 750 ? 'warn' : 'good'" :sparkline="latencyHistory" />
        <MetricTile label="In flight" :value="http.requests_in_flight.toFixed(0)" icon="layers" />
        <MetricTile label="5xx lifetime" :value="http.errors_total" icon="warning" :tone="http.errors_total > 0 ? 'warn' : 'good'" />
        <MetricTile label="WebSockets" :value="network.general.ws_subscribers" icon="eye"
          :sub="`${network.general.ws_admin_subscribers} admin · ${network.general.internal_subscribers} internal relays`" />
      </div>

      <SettingsSection title="Ingress request paths" icon="network" description="Caddy metrics split by the listener that accepted the request.">
        <div v-if="!(network.ingress.by_ingress ?? []).length" class="empty-state">No ingress request samples yet.</div>
        <div v-else class="metric-table">
          <div class="metric-row head"><span>Ingress</span><span>Rate</span><span>In flight</span><span>p95</span><span>5xx</span><span>Sent</span></div>
          <div v-for="row in network.ingress.by_ingress" :key="row.name" class="metric-row">
            <code>{{ row.name }}</code><span>{{ fmtRate(row.requests_per_second) }}</span><span>{{ row.requests_in_flight.toFixed(0) }}</span>
            <span>{{ fmtMs(row.p95_latency_ms) }}</span><span>{{ row.errors_total.toLocaleString() }}</span><span>{{ fmtBytes(row.bytes_sent) }}</span>
          </div>
        </div>
      </SettingsSection>

      <div class="detail-grid">
        <SettingsSection title="HTTP protocols" icon="link">
          <KVTable :rows="[
            { key: 'HTTP/1.1 requests', value: http.protocols.http1.toLocaleString() },
            { key: 'HTTP/2 requests', value: http.protocols.http2.toLocaleString() },
            { key: 'HTTP/3 requests', value: http.protocols.http3.toLocaleString() },
            { key: 'Requests lifetime', value: http.requests_total.toLocaleString() },
            { key: 'Bytes received', value: fmtBytes(http.bytes_received) },
            { key: 'Bytes sent', value: fmtBytes(http.bytes_sent) },
          ]" />
        </SettingsSection>
        <SettingsSection title="WebSocket hub" icon="eye" description="Browser connections are separated from trusted in-process event consumers.">
          <KVTable :rows="[
            { key: 'Browser clients', value: network.general.ws_subscribers },
            { key: 'Admin clients', value: network.general.ws_admin_subscribers },
            { key: 'Internal consumers', value: network.general.internal_subscribers },
            { key: 'Ingress running', value: network.ingress.running ? 'yes' : 'no' },
            { key: 'Ingress generation', value: network.ingress.generation },
          ]" />
        </SettingsSection>
      </div>
      <p class="network-link">Listener topology, certificates, interfaces, Tailscale, and remote access remain under <NuxtLink to="/settings/network">Network settings</NuxtLink>.</p>
    </template>
  </div>
</template>

<style scoped>
.loading-state, .empty-state { padding: 14px; border: 1px solid var(--border); border-radius: var(--r-md); color: var(--fg-3); background: var(--bg-2); }
.loading-state { display: flex; align-items: center; gap: 8px; }
.tiles { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; margin-bottom: 20px; }
.metric-table { overflow-x: auto; border: 1px solid var(--border); border-radius: var(--r-md); }
.metric-row { display: grid; grid-template-columns: minmax(150px, 1fr) repeat(5, minmax(75px, .5fr)); gap: 10px; min-width: 700px; padding: 8px 11px; border-bottom: 1px solid var(--hair); align-items: center; font-family: var(--font-mono); font-size: 10.5px; }
.metric-row:last-child { border-bottom: 0; }
.metric-row.head { background: var(--bg-2); color: var(--fg-3); font-size: 9px; text-transform: uppercase; letter-spacing: .07em; }
.metric-row code { color: var(--fg-1); }
.metric-row span:not(:first-child) { text-align: right; }
.detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.network-link { margin-top: 4px; color: var(--fg-3); font-size: 11.5px; }
.network-link a { color: var(--gold); text-decoration: none; }
@media (max-width: 900px) { .tiles { grid-template-columns: repeat(2, minmax(0, 1fr)); } .detail-grid { grid-template-columns: 1fr; } }
@media (max-width: 540px) { .tiles { grid-template-columns: 1fr; } }
</style>
