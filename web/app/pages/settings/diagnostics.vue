<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { adminDiagnosticsQuery } from '~/queries/admin'
import type { QueryStatement } from '~~/shared/api/types.gen'

const { $heya } = useNuxtApp()
const diagnosticsData = useQuery(adminDiagnosticsQuery())
const diag = computed(() => diagnosticsData.data.value ?? null)
const loading = computed(() => diagnosticsData.isLoading.value)
const refreshing = ref(false)
const bundling = ref(false)
const { flash } = useFlash()

const hostCPUHistory = ref<number[]>([])
const serveCPUHistory = ref<number[]>([])
const workerCPUHistory = ref<number[]>([])
const requestHistory = ref<number[]>([])
const queryHistory = ref<number[]>([])
const heapHistory = ref<number[]>([])
const HISTORY = 48

let timer: ReturnType<typeof setInterval> | null = null

function appendSample(target: number[], value: number) {
  target.push(value)
  if (target.length > HISTORY) target.shift()
}

watch(() => diagnosticsData.data.value, (value) => {
  if (!value) return
  appendSample(hostCPUHistory.value, value.system.host_cpu_percent)
  appendSample(serveCPUHistory.value, value.system.cpu_percent)
  appendSample(workerCPUHistory.value, value.worker.cpu_percent)
  appendSample(requestHistory.value, value.http.p95_latency_ms)
  appendSample(queryHistory.value, value.queries.p95_ms)
  appendSample(heapHistory.value, value.system.heap_inuse_bytes)
}, { immediate: true })

async function refresh() {
  refreshing.value = true
  try {
    await diagnosticsData.refetch()
  } catch (error: any) {
    flash.value = { kind: 'err', text: error?.message ?? 'Failed to refresh diagnostics.' }
  } finally {
    refreshing.value = false
  }
}

async function downloadSupportBundle() {
  bundling.value = true
  try {
    const report = await $heya('/api/admin/doctor')
    const blob = new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `heya-doctor-${new Date().toISOString().slice(0, 10)}.json`
    document.body.appendChild(anchor)
    anchor.click()
    document.body.removeChild(anchor)
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    flash.value = { kind: 'ok', text: 'Support bundle downloaded.' }
  } catch (error: any) {
    flash.value = { kind: 'err', text: error?.message ?? 'Failed to build support bundle.' }
  } finally {
    bundling.value = false
  }
}

const dbPoolPct = computed(() => {
  const db = diag.value?.database
  if (!db?.max_connections) return 0
  return db.acquired_connections / db.max_connections * 100
})

const dbTone = computed<'good' | 'warn' | 'bad'>(() => {
  if (dbPoolPct.value >= 90) return 'bad'
  if (dbPoolPct.value >= 70) return 'warn'
  return 'good'
})

const errorLogs5m = computed(() => {
  const levels = diag.value?.logs.last_5_minutes
  if (!levels) return 0
  return (levels.error ?? 0) + (levels.fatal ?? 0) + (levels.panic ?? 0)
})

const warningLogs5m = computed(() => diag.value?.logs.last_5_minutes.warn ?? 0)
const workerTelemetryAvailable = computed(() => !!diag.value?.worker_online && (diag.value?.worker.num_cpu ?? 0) > 0)

const expandedQuery = ref('')
const queryRows = computed<QueryStatement[]>(() => [...(diag.value?.queries.top_statements ?? [])]
  .sort((a, b) => {
    if (a.recent_errors !== b.recent_errors) return b.recent_errors - a.recent_errors
    if (a.recent_p95_ms !== b.recent_p95_ms) return b.recent_p95_ms - a.recent_p95_ms
    return b.total_duration_ms - a.total_duration_ms
  }))

function querySignal(row: QueryStatement): 'error' | 'slow' | 'hot' | 'impact' | 'idle' {
  if (row.recent_errors > 0 || row.last_error_code) return 'error'
  if (row.recent_p95_ms >= 250 || row.max_ms >= 1000) return 'slow'
  if (row.recent_calls >= 30) return 'hot'
  if (row.total_duration_ms >= 1000) return 'impact'
  return 'idle'
}
function querySignalLabel(row: QueryStatement) {
  const signal = querySignal(row)
  if (signal === 'error') return row.last_error_code ? `SQLSTATE ${row.last_error_code}` : 'errors'
  if (signal === 'slow') return 'slow'
  if (signal === 'hot') return 'high volume'
  if (signal === 'impact') return 'cumulative'
  return 'normal'
}

function statusState(status?: string): 'ok' | 'warn' | 'error' | 'idle' {
  if (status === 'healthy') return 'ok'
  if (status === 'watching') return 'warn'
  if (status === 'degraded') return 'error'
  return 'idle'
}

function findingIcon(tone: string) {
  return tone === 'good' ? 'check' : tone === 'bad' ? 'warning' : 'pulse'
}

function fmtMs(value?: number) {
  if (value == null || !Number.isFinite(value)) return '—'
  if (value < 0.01) return '<0.01 ms'
  if (value < 10) return `${value.toFixed(2)} ms`
  if (value < 100) return `${value.toFixed(1)} ms`
  return `${Math.round(value).toLocaleString()} ms`
}

function fmtPct(value?: number) {
  if (value == null || !Number.isFinite(value)) return '—'
  if (value === 0) return '0%'
  return `${value < 10 ? value.toFixed(1) : value.toFixed(0)}%`
}

function fmtRate(value?: number) {
  if (value == null || !Number.isFinite(value)) return '—'
  return `${value < 10 ? value.toFixed(2) : value.toFixed(1)}/s`
}

function hostMetricLabel(metric?: string) {
  return metric === 'load_average_1m' ? '1m load normalized by CPU count' : 'whole-host CPU utilization'
}

function fmtUptime(seconds?: number) {
  if (seconds == null) return '—'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
}

function fmtAge(value?: string) {
  if (!value) return 'waiting for first sample'
  const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000))
  return seconds < 2 ? 'updated just now' : `updated ${seconds}s ago`
}

onMounted(() => {
  timer = setInterval(() => { void diagnosticsData.refetch() }, 5000)
})
onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="diagnostics-page">
    <SettingsContextHero
      title="Diagnostics"
      icon="pulse"
      eyebrow="Advanced · Operational overview"
      description="One live view of host and process load, request latency, actionable query cost, database pressure, worker health, and recent logs."
      :tone="diag?.status === 'healthy' ? 'connected' : 'accent'"
    >
      <div class="context-fact">
        <strong>{{ diag?.status ?? 'probing' }}</strong>
        <span>system state</span>
      </div>
      <div class="context-fact">
        <strong>{{ fmtUptime(diag?.system.uptime_seconds) }}</strong>
        <span>uptime</span>
      </div>
      <div class="context-fact">
        <strong>{{ diag?.system.hostname ?? '—' }}</strong>
        <span>host</span>
      </div>
    </SettingsContextHero>

    <div class="dashboard-toolbar">
      <div class="toolbar-state">
        <StatusBadge :state="statusState(diag?.status)">{{ diag?.status ?? 'probing' }}</StatusBadge>
        <span>{{ fmtAge(diag?.generated_at) }}</span>
      </div>
      <div class="toolbar-actions">
        <button class="sv2-btn ghost" :disabled="refreshing" @click="refresh">
          <Icon :name="refreshing ? 'spinner' : 'refresh'" :size="12" />
          {{ refreshing ? 'Refreshing…' : 'Refresh' }}
        </button>
        <button class="sv2-btn primary" :disabled="bundling" @click="downloadSupportBundle">
          <Icon :name="bundling ? 'spinner' : 'download'" :size="12" />
          {{ bundling ? 'Building…' : 'Support bundle' }}
        </button>
      </div>
    </div>

    <div v-if="loading && !diag" class="loading-state">
      <Icon name="spinner" :size="16" /> Gathering diagnostics…
    </div>

    <template v-else-if="diag">
      <div class="signal-tiles">
        <MetricTile
          label="Request p95"
          :value="diag.http_available ? fmtMs(diag.http.p95_latency_ms) : 'Unavailable'"
          icon="timer"
          :tone="!diag.http_available ? 'neutral' : diag.http.p95_latency_ms >= 2000 ? 'bad' : diag.http.p95_latency_ms >= 750 ? 'warn' : 'good'"
          :sub="diag.http_available ? `${fmtRate(diag.http.requests_per_second)} · ${diag.http.requests_in_flight.toFixed(0)} in flight` : 'Ingress metrics not active'"
          :sparkline="requestHistory"
        />
        <MetricTile
          label="Query p95 · 1m"
          :value="fmtMs(diag.queries.p95_ms)"
          icon="database"
          :tone="diag.queries.p95_ms >= 1000 ? 'bad' : diag.queries.p95_ms >= 250 ? 'warn' : 'good'"
          :sub="`${fmtRate(diag.queries.queries_per_second)} · ${diag.queries.in_flight} in flight`"
          :sparkline="queryHistory"
        />
        <MetricTile
          label="System load"
          :value="diag.system.host_cpu_available ? fmtPct(diag.system.host_cpu_percent) : 'Unavailable'"
          icon="cpu"
          :tone="diag.system.host_cpu_percent >= 90 ? 'bad' : diag.system.host_cpu_percent >= 70 ? 'warn' : 'good'"
          :sub="`${hostMetricLabel(diag.system.host_cpu_metric)} · ${diag.system.num_cpu} CPUs`"
          :sparkline="hostCPUHistory"
        />
        <MetricTile
          label="Serve CPU"
          :value="fmtPct(diag.system.cpu_percent)"
          icon="pulse"
          sub="one logical core = 100%"
          :sparkline="serveCPUHistory"
        />
        <MetricTile
          label="Worker CPU"
          :value="workerTelemetryAvailable ? fmtPct(diag.worker.cpu_percent) : diag.worker_online ? 'Waiting' : 'Offline'"
          icon="wrench"
          :tone="workerTelemetryAvailable ? 'good' : 'warn'"
          :sub="workerTelemetryAvailable ? `${diag.worker.goroutines} goroutines · one core = 100%` : diag.worker_online ? 'restart worker to publish extended telemetry' : 'worker heartbeat missing or stale'"
          :sparkline="workerCPUHistory"
        />
        <MetricTile
          label="Heap in use"
          :value="fmtBytes(diag.system.heap_inuse_bytes)"
          icon="hard-drives"
          :sub="`${diag.system.goroutines.toLocaleString()} goroutines`"
          :sparkline="heapHistory"
        />
        <MetricTile
          label="Database pool"
          :value="`${diag.database.acquired_connections} / ${diag.database.max_connections}`"
          icon="database"
          :tone="dbTone"
          :sub="`${diag.database.total_connections} open · ${diag.database.waiting_queries} waiting`"
        />
        <MetricTile
          label="Errors · 5m"
          :value="errorLogs5m"
          icon="warning"
          :tone="errorLogs5m > 0 ? 'bad' : warningLogs5m > 0 ? 'warn' : 'good'"
          :sub="`${warningLogs5m} warnings · ${diag.logs.buffered} buffered`"
        />
      </div>

      <section class="health-panel" :class="`state-${diag.status}`">
        <div class="health-heading">
          <div class="health-icon"><Icon :name="diag.status === 'healthy' ? 'shield-check' : 'warning'" :size="19" /></div>
          <div>
            <span>Operational read</span>
            <strong>{{ diag.status === 'healthy' ? 'No immediate pressure detected' : diag.status === 'watching' ? 'A few signals need watching' : 'One or more signals are degraded' }}</strong>
          </div>
        </div>
        <div class="finding-list">
          <div v-for="finding in diag.findings ?? []" :key="`${finding.section}-${finding.title}`" class="finding" :class="`tone-${finding.tone}`">
            <Icon :name="findingIcon(finding.tone)" :size="13" />
            <div><strong>{{ finding.title }}</strong><span>{{ finding.detail }}</span></div>
          </div>
        </div>
      </section>

      <div class="overview-grid">
        <DashboardSummaryCard title="Compute" icon="cpu" :value="diag.system.host_cpu_available ? fmtPct(diag.system.host_cpu_percent) : '—'" value-label="system load">
          <div class="summary-row"><span>Serve / worker CPU</span><strong>{{ fmtPct(diag.system.cpu_percent) }} / {{ workerTelemetryAvailable ? fmtPct(diag.worker.cpu_percent) : diag.worker_online ? 'waiting' : 'offline' }}</strong></div>
          <div class="summary-row"><span>Heap / process sys</span><strong>{{ fmtBytes(diag.system.heap_inuse_bytes) }} / {{ fmtBytes(diag.system.sys_bytes) }}</strong></div>
          <div class="summary-row"><span>Goroutines</span><strong>{{ diag.system.goroutines.toLocaleString() }}</strong></div>
          <div class="summary-row"><span>Worker heartbeat</span><strong :class="diag.worker_online ? 'good' : 'warn'">{{ diag.worker_online ? fmtAge(diag.worker.heartbeat_at) : 'offline' }}</strong></div>
          <template #footer><NuxtLink class="summary-link" to="/settings/runtime">Open runtime details <Icon name="chevright" :size="11" /></NuxtLink></template>
        </DashboardSummaryCard>

        <DashboardSummaryCard title="Traffic" icon="pulse" :value="diag.http_available ? fmtRate(diag.http.requests_per_second) : '—'" value-label="requests">
          <div class="summary-row"><span>p50 / p95 latency</span><strong>{{ fmtMs(diag.http.p50_latency_ms) }} / {{ fmtMs(diag.http.p95_latency_ms) }}</strong></div>
          <div class="summary-row"><span>In flight</span><strong>{{ diag.http.requests_in_flight.toFixed(0) }}</strong></div>
          <div class="summary-row"><span>5xx lifetime</span><strong :class="diag.http.errors_total > 0 ? 'warn' : 'good'">{{ diag.http.errors_total.toLocaleString() }}</strong></div>
          <div class="summary-row"><span>Transferred</span><strong>{{ fmtBytes(diag.http.bytes_received) }} in · {{ fmtBytes(diag.http.bytes_sent) }} out</strong></div>
          <template #footer><NuxtLink class="summary-link" to="/settings/traffic">Open API &amp; WebSocket details <Icon name="chevright" :size="11" /></NuxtLink></template>
        </DashboardSummaryCard>

        <DashboardSummaryCard title="Database" icon="database" :value="fmtPct(dbPoolPct)" value-label="pool used" :tone="dbTone">
          <div class="summary-row"><span>Buffer cache hit</span><strong :class="diag.database.buffer_cache_hit_ratio >= 95 ? 'good' : 'warn'">{{ fmtPct(diag.database.buffer_cache_hit_ratio) }}</strong></div>
          <div class="summary-row"><span>Active / waiting</span><strong>{{ diag.database.active_queries }} / {{ diag.database.waiting_queries }}</strong></div>
          <div class="summary-row"><span>Longest active query</span><strong>{{ fmtMs(diag.database.longest_query_ms) }}</strong></div>
          <div class="summary-row"><span>Dead tuples / deadlocks</span><strong>{{ diag.database.dead_tuples.toLocaleString() }} / {{ diag.database.deadlocks.toLocaleString() }}</strong></div>
          <template #footer><NuxtLink class="summary-link" to="/settings/database">Open database details <Icon name="chevright" :size="11" /></NuxtLink></template>
        </DashboardSummaryCard>
      </div>

      <SettingsSection
        title="Query performance"
        icon="timer"
        description="Use recent p95 to find slow request-path queries, recent calls to find hot loops, cumulative time to find expensive repetition, and SQLSTATE to identify failures. Click a row for its sanitized statement and full evidence."
      >
        <template #actions>
          <NuxtLink class="link-arrow" to="/settings/database">Database-wide statements <Icon name="chevright" :size="11" /></NuxtLink>
        </template>
        <div class="section-metrics">
          <div><span>API rate · 1m</span><strong>{{ fmtRate(diag.queries.queries_per_second) }}</strong></div>
          <div><span>API average · 1m</span><strong>{{ fmtMs(diag.queries.average_ms) }}</strong></div>
          <div><span>API p95 · 1m</span><strong>{{ fmtMs(diag.queries.p95_ms) }}</strong></div>
          <div><span>API errors · 1m</span><strong :class="diag.queries.recent_errors > 0 ? 'bad' : 'good'">{{ diag.queries.recent_errors }}</strong></div>
        </div>
        <div v-if="queryRows.length === 0" class="empty-state"><Icon name="info" :size="14" /> Waiting for query samples.</div>
        <div v-else class="query-table" role="table" aria-label="Most expensive database queries">
          <div class="query-row query-head" role="row">
            <span role="columnheader">Signal</span><span role="columnheader">Statement</span><span role="columnheader">Calls · 1m</span><span role="columnheader">p95 · 1m</span><span role="columnheader">Total time</span><span role="columnheader">Errors</span>
          </div>
          <div v-for="row in queryRows.slice(0, 10)" :key="row.statement" class="query-entry">
            <button class="query-row" role="row" :class="`signal-${querySignal(row)}`" @click="expandedQuery = expandedQuery === row.statement ? '' : row.statement">
              <span role="cell" class="query-signal">{{ querySignalLabel(row) }}</span>
              <code role="cell" :title="row.statement">{{ row.statement }}</code>
              <span role="cell">{{ row.recent_calls.toLocaleString() }}</span>
              <span role="cell">{{ fmtMs(row.recent_p95_ms) }}</span>
              <span role="cell">{{ fmtMs(row.total_duration_ms) }}</span>
              <span role="cell" :class="row.errors > 0 ? 'bad' : 'good'">{{ row.errors.toLocaleString() }}</span>
            </button>
            <div v-if="expandedQuery === row.statement" class="query-detail">
              <code>{{ row.statement }}</code>
              <div><span>Lifetime calls</span><strong>{{ row.calls.toLocaleString() }}</strong></div>
              <div><span>Lifetime avg / max</span><strong>{{ fmtMs(row.average_ms) }} / {{ fmtMs(row.max_ms) }}</strong></div>
              <div><span>Recent avg / p95</span><strong>{{ fmtMs(row.recent_average_ms) }} / {{ fmtMs(row.recent_p95_ms) }}</strong></div>
              <div><span>Last seen</span><strong>{{ fmtAge(row.last_seen_at) }}</strong></div>
              <div v-if="row.last_error_code"><span>Last error</span><strong class="bad">SQLSTATE {{ row.last_error_code }} · {{ fmtAge(row.last_error_at) }}</strong></div>
            </div>
          </div>
        </div>
      </SettingsSection>

      <SettingsSection title="Recent warnings + errors" icon="clipboard" description="The latest high-signal entries from the serve ring and worker relay. Use Logs for full search, process filtering, field inspection, pause, and export.">
        <template #actions>
          <NuxtLink class="link-arrow" to="/settings/logs">Open logs <Icon name="chevright" :size="11" /></NuxtLink>
        </template>
        <div class="log-summary">
          <div v-for="level in ['trace', 'debug', 'info', 'warn', 'error']" :key="level" class="log-count" :class="level">
            <span>{{ level }}</span><strong>{{ (diag.logs.last_5_minutes[level] ?? 0).toLocaleString() }}</strong><small>last 5m</small>
          </div>
        </div>
        <div v-if="!(diag.logs.recent ?? []).length" class="empty-state"><Icon name="check" :size="14" /> No warning or error entries are buffered.</div>
        <div v-else class="recent-logs">
          <div v-for="entry in [...(diag.logs.recent ?? [])].reverse()" :key="`${entry.time}-${entry.message}`" class="recent-log" :class="entry.level">
            <span class="log-time">{{ new Date(entry.time).toLocaleTimeString() }}</span>
            <span class="log-source">{{ entry.source || 'serve' }}</span>
            <span class="log-level">{{ entry.level }}</span>
            <span class="log-message">{{ entry.message }}</span>
          </div>
        </div>
      </SettingsSection>
    </template>

    <SettingsFlash :flash="flash" />
  </div>
</template>

<style scoped>
.diagnostics-page { padding-bottom: 20px; }
.dashboard-toolbar {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  margin: -4px 0 14px;
}
.toolbar-state, .toolbar-actions { display: flex; align-items: center; gap: 8px; }
.toolbar-state > span:last-child { color: var(--fg-3); font-family: var(--font-mono); font-size: 10.5px; }

.loading-state, .empty-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.good { color: var(--good); }
.warn { color: var(--gold); }
.bad { color: var(--bad); }

.signal-tiles {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
  margin-bottom: 12px;
}

.health-panel {
  display: grid;
  grid-template-columns: minmax(220px, 0.75fr) minmax(0, 1.6fr);
  gap: 18px;
  margin-bottom: 12px;
  padding: 15px 17px;
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  background: linear-gradient(145deg, var(--bg-1), var(--bg-2));
}
.health-panel.state-healthy { border-color: color-mix(in srgb, var(--good) 27%, var(--border)); }
.health-panel.state-watching { border-color: color-mix(in srgb, var(--gold) 34%, var(--border)); }
.health-panel.state-degraded { border-color: color-mix(in srgb, var(--bad) 38%, var(--border)); }
.health-heading { display: flex; align-items: center; gap: 11px; }
.health-icon {
  width: 38px; height: 38px; display: grid; place-items: center; flex: none;
  border-radius: 12px; background: var(--gold-soft); color: var(--gold);
}
.state-healthy .health-icon { background: color-mix(in srgb, var(--good) 11%, transparent); color: var(--good); }
.state-degraded .health-icon { background: color-mix(in srgb, var(--bad) 11%, transparent); color: var(--bad); }
.health-heading span { display: block; margin-bottom: 2px; color: var(--fg-3); font-family: var(--font-mono); font-size: 9px; letter-spacing: .12em; text-transform: uppercase; }
.health-heading strong { display: block; color: var(--fg-0); font-size: 13px; }
.finding-list { display: grid; gap: 5px; }
.finding { display: grid; grid-template-columns: 16px minmax(0, 1fr); gap: 6px; color: var(--fg-3); }
.finding > svg { margin-top: 2px; }
.finding.tone-good > svg { color: var(--good); }
.finding.tone-warn > svg { color: var(--gold); }
.finding.tone-bad > svg { color: var(--bad); }
.finding strong { display: inline; margin-right: 6px; color: var(--fg-1); font-size: 11.5px; }
.finding span { color: var(--fg-3); font-size: 11.5px; line-height: 1.45; }

.overview-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 16px;
}
.summary-link {
  display: flex; align-items: center; justify-content: space-between;
  color: var(--fg-2); font-size: 11.5px; text-decoration: none;
}
.summary-link:hover { color: var(--gold); }

.section-metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 7px; margin-bottom: 12px; }
.section-metrics > div { padding: 10px 12px; border: 1px solid var(--border); border-radius: var(--r-sm); background: var(--bg-2); }
.section-metrics span { display: block; margin-bottom: 3px; color: var(--fg-3); font-size: 10px; }
.section-metrics strong { color: var(--fg-0); font-family: var(--font-mono); font-size: 14px; font-variant-numeric: tabular-nums; }
.section-metrics strong.good { color: var(--good); }
.section-metrics strong.bad { color: var(--bad); }

.query-table { overflow: hidden; border: 1px solid var(--border); border-radius: var(--r-md); }
.query-entry { border-bottom: 1px solid var(--hair); }
.query-entry:last-child { border-bottom: 0; }
.query-row {
  display: grid; grid-template-columns: 92px minmax(280px, 1fr) 78px 84px 90px 58px;
  gap: 10px; align-items: center; min-height: 37px; padding: 6px 11px;
  width: 100%; border: 0; background: transparent; color: var(--fg-2);
  font-family: var(--font-mono); font-size: 10.5px; text-align: left;
}
.query-row:not(.query-head):hover { background: rgb(var(--ink) / .025); }
.query-head { min-height: 31px; background: var(--bg-2); color: var(--fg-3); font-size: 9px; letter-spacing: .08em; text-transform: uppercase; }
.query-row code { overflow: hidden; color: var(--fg-1); font-family: inherit; text-overflow: ellipsis; white-space: nowrap; }
.query-row span:nth-child(n+3) { text-align: right; font-variant-numeric: tabular-nums; }
.query-signal { display: inline-flex; width: fit-content; padding: 2px 6px; border: 1px solid var(--border); border-radius: 999px; color: var(--fg-3); font-size: 8.5px; text-transform: uppercase; }
.signal-error .query-signal { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 40%, var(--border)); }
.signal-slow .query-signal, .signal-impact .query-signal { color: var(--gold); border-color: color-mix(in srgb, var(--gold) 40%, var(--border)); }
.signal-hot .query-signal { color: rgb(140, 160, 255); border-color: rgba(140, 160, 255, .4); }
.query-detail { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 8px 14px; padding: 10px 12px 12px 114px; background: var(--bg-2); }
.query-detail > code { grid-column: 1 / -1; overflow-wrap: anywhere; color: var(--fg-1); font-family: var(--font-mono); font-size: 10.5px; line-height: 1.45; }
.query-detail span { display: block; color: var(--fg-4); font-size: 9px; text-transform: uppercase; letter-spacing: .05em; }
.query-detail strong { color: var(--fg-2); font-family: var(--font-mono); font-size: 10.5px; }

.log-summary { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 6px; margin-bottom: 10px; }
.log-count { padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--r-sm); background: var(--bg-2); }
.log-count span { display: block; color: var(--fg-3); font-family: var(--font-mono); font-size: 9px; font-weight: 650; text-transform: uppercase; }
.log-count strong { display: block; color: var(--fg-1); font-size: 16px; }
.log-count small { color: var(--fg-4); font-size: 9px; }
.log-count.warn strong { color: var(--gold); }
.log-count.error strong { color: var(--bad); }
.recent-logs { overflow: hidden; border: 1px solid var(--border); border-radius: var(--r-md); background: var(--bg-0); }
.recent-log {
  display: grid; grid-template-columns: 82px 54px 48px minmax(0, 1fr); gap: 8px;
  padding: 5px 10px; border-bottom: 1px solid var(--hair); font-family: var(--font-mono); font-size: 10.5px;
}
.recent-log:last-child { border-bottom: 0; }
.recent-log.warn { background: color-mix(in srgb, var(--gold) 4%, transparent); }
.recent-log.error, .recent-log.fatal, .recent-log.panic { background: color-mix(in srgb, var(--bad) 6%, transparent); }
.log-time { color: var(--fg-4); }
.log-source { color: var(--fg-3); font-size: 8.5px; font-weight: 700; text-transform: uppercase; }
.log-level { color: var(--gold); font-size: 9px; font-weight: 700; text-transform: uppercase; }
.error .log-level, .fatal .log-level, .panic .log-level { color: var(--bad); }
.log-message { overflow: hidden; color: var(--fg-1); text-overflow: ellipsis; white-space: nowrap; }

@media (max-width: 1100px) {
  .signal-tiles { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .overview-grid { grid-template-columns: 1fr; }
}
@media (max-width: 760px) {
  .health-panel { grid-template-columns: 1fr; }
  .query-table { overflow-x: auto; }
  .query-row { min-width: 820px; }
  .query-detail { min-width: 820px; padding-left: 114px; }
  .section-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 560px) {
  .dashboard-toolbar { align-items: flex-start; }
  .toolbar-state { flex-direction: column; align-items: flex-start; }
  .toolbar-actions { flex-wrap: wrap; justify-content: flex-end; }
  .signal-tiles { grid-template-columns: 1fr; }
  .log-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .recent-log { grid-template-columns: 70px 48px 42px minmax(0, 1fr); }
}
</style>
