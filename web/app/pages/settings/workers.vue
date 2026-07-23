<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { adminWorkersQuery } from '~/queries/admin'

const workersData = useQuery(adminWorkersQuery())
const data = computed(() => workersData.data.value ?? null)
const status = computed(() => data.value?.status ?? null)
const loading = computed(() => workersData.isLoading.value && !data.value)
const activeJobs = computed(() => data.value?.active_jobs ?? [])
const recentJobs = computed(() => data.value?.recent_jobs ?? [])
const telemetryAvailable = computed(() => (status.value?.num_cpu ?? 0) > 0)
const restarting = ref<'server' | 'worker' | 'all' | null>(null)
const controlFlash = ref<{ kind: 'ok' | 'warn' | 'err'; text: string } | null>(null)
const { $heya } = useNuxtApp()
const { confirm } = useConfirm()
let timer: ReturnType<typeof setInterval> | null = null

const queueCounts = computed(() => Object.fromEntries((data.value?.queue_summary ?? []).map(row => [row.state, row.count])))
const pending = computed(() => (queueCounts.value.available ?? 0) + (queueCounts.value.scheduled ?? 0) + (queueCounts.value.retryable ?? 0))

function fmtPct(value?: number) { return value == null ? '—' : `${value.toFixed(1)}%` }
function fmtUptime(started?: string) {
  if (!started) return '—'
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(started).getTime()) / 1000))
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return days ? `${days}d ${hours}h` : hours ? `${hours}h ${minutes}m` : `${minutes}m`
}
function age(value?: string) {
  if (!value) return 'never'
  const seconds = Math.max(0, Math.round((Date.now() - new Date(value).getTime()) / 1000))
  return seconds < 2 ? 'just now' : `${seconds}s ago`
}
function jobDuration(job: any) {
  const start = job.attempted_at || job.created_at
  const end = job.finalized_at || new Date().toISOString()
  const seconds = Math.max(0, Math.round((new Date(end).getTime() - new Date(start).getTime()) / 1000))
  return seconds < 60 ? `${seconds}s` : `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}
function jobTone(state: string): 'ok' | 'warn' | 'error' | 'idle' {
  if (state === 'completed') return 'ok'
  if (state === 'running' || state === 'available' || state === 'scheduled') return 'idle'
  if (state === 'retryable') return 'warn'
  return 'error'
}

async function restartProcess(target: 'server' | 'worker' | 'all') {
  const label = target === 'all' ? 'server and worker' : target
  const approved = await confirm({
    title: `Restart ${label}?`,
    message: target === 'server'
      ? 'The web connection will briefly drop while the process supervisor brings the server back.'
      : target === 'worker'
        ? 'Active jobs will stop gracefully and resume after the process supervisor brings the worker back.'
        : 'The web connection will briefly drop, and active jobs will resume after both supervised processes return.',
    destructive: true,
    confirmLabel: `Restart ${label}`,
  })
  if (!approved) return

  restarting.value = target
  controlFlash.value = null
  try {
    await $heya('/api/admin/processes/restart', {
      method: 'POST',
      body: { target } as any,
    })
    controlFlash.value = {
      kind: 'ok',
      text: `Restart requested for ${label}. The external process supervisor will bring ${target === 'all' ? 'them' : 'it'} back.`,
    }
  } catch (e: any) {
    controlFlash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Restart request failed.' }
  } finally {
    restarting.value = null
  }
}

onMounted(() => { timer = setInterval(() => { void workersData.refetch() }, 3000) })
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <div>
    <SettingsContextHero
      title="Workers"
      icon="wrench"
      eyebrow="Advanced · Background runtime"
      description="Inspect the dedicated worker process, current River jobs, queue pressure, recent outcomes, and filesystem watcher ownership."
      :tone="data?.online ? 'connected' : 'accent'"
    >
      <div class="context-fact"><strong>{{ data?.online ? 'online' : 'offline' }}</strong><span>worker state</span></div>
      <div class="context-fact"><strong>{{ age(status?.heartbeat_at) }}</strong><span>heartbeat</span></div>
      <div class="context-fact"><strong>{{ activeJobs.length }}</strong><span>active jobs</span></div>
    </SettingsContextHero>

    <div v-if="loading" class="loading-state"><Icon name="spinner" :size="15" /> Reading worker heartbeat…</div>
    <template v-else-if="data && status">
      <div v-if="data.error" class="error-state"><Icon name="warning" :size="14" /> {{ data.error }}</div>
      <div v-else-if="data.online && !telemetryAvailable" class="notice-state"><Icon name="info" :size="14" /> The worker is online but still publishing the legacy heartbeat. Restart it after this upgrade to expose CPU, memory, and log-source telemetry.</div>
      <div v-else-if="!data.online" class="error-state"><Icon name="warning" :size="14" /> The worker heartbeat is missing or stale.</div>
      <SettingsFlash :flash="controlFlash" />
      <div class="tiles">
        <MetricTile label="Worker CPU" :value="telemetryAvailable ? fmtPct(status.cpu_percent) : 'Waiting'" icon="cpu" :tone="telemetryAvailable && status.cpu_percent >= 100 ? 'warn' : telemetryAvailable ? 'good' : 'neutral'" sub="one logical core = 100%" />
        <MetricTile label="Heap in use" :value="telemetryAvailable ? fmtBytes(status.heap_inuse_bytes) : 'Waiting'" icon="hard-drives" :sub="telemetryAvailable ? `${status.goroutines} goroutines` : 'extended heartbeat pending'" />
        <MetricTile label="Uptime" :value="fmtUptime(status.started_at)" icon="timer" />
        <MetricTile label="Running" :value="queueCounts.running ?? 0" icon="play" :tone="(queueCounts.running ?? 0) > 0 ? 'good' : 'neutral'" />
        <MetricTile label="Pending" :value="pending" icon="list" :tone="pending > 1000 ? 'warn' : 'neutral'" />
        <MetricTile label="Retryable" :value="queueCounts.retryable ?? 0" icon="refresh" :tone="(queueCounts.retryable ?? 0) > 0 ? 'warn' : 'good'" />
      </div>

      <SettingsSection title="Active work" icon="pulse" description="Jobs currently owned by the dedicated River worker.">
        <div v-if="!activeJobs.length" class="empty-state">Worker is idle — no jobs are currently running.</div>
        <div v-else class="active-grid">
          <article v-for="job in activeJobs" :key="job.id" class="active-job">
            <div><StatusBadge state="idle">running</StatusBadge><code>#{{ job.id }}</code></div>
            <strong>{{ job.kind }}</strong><span>{{ job.queue }} · attempt {{ job.attempt }}/{{ job.max_attempts }} · {{ jobDuration(job) }}</span>
          </article>
        </div>
      </SettingsSection>

      <div class="detail-grid">
        <SettingsSection title="Process" icon="cpu">
          <KVTable :rows="[
            { key: 'Hostname', value: status.hostname || (telemetryAvailable ? '—' : 'awaiting restart'), mono: true },
            { key: 'PID', value: telemetryAvailable ? (status.pid || '—') : 'awaiting restart', mono: true },
            { key: 'Started', value: status.started_at, mono: true },
            { key: 'Heartbeat', value: status.heartbeat_at, mono: true },
            { key: 'Worker CPU', value: telemetryAvailable ? `${fmtPct(status.cpu_percent)} (one core = 100%)` : 'awaiting restart' },
            { key: 'System load at sample', value: telemetryAvailable && status.host_cpu_available ? `${fmtPct(status.host_cpu_percent)} (${status.host_cpu_metric})` : telemetryAvailable ? 'unavailable' : 'awaiting restart' },
            { key: 'Heap allocated', value: telemetryAvailable ? fmtBytes(status.heap_alloc_bytes) : 'awaiting restart' },
            { key: 'Process sys', value: telemetryAvailable ? fmtBytes(status.sys_bytes) : 'awaiting restart' },
            { key: 'GOMAXPROCS / CPUs', value: telemetryAvailable ? `${status.gomaxprocs} / ${status.num_cpu}` : 'awaiting restart' },
            { key: 'Log level', value: status.log_level || (telemetryAvailable ? '—' : 'awaiting restart'), mono: true },
          ]" />
        </SettingsSection>
        <SettingsSection title="Queue states" icon="list">
          <KVTable :rows="(data.queue_summary ?? []).map(row => ({ key: row.state, value: row.count.toLocaleString() }))" />
        </SettingsSection>
      </div>

      <SettingsSection title="Filesystem watchers" icon="eye" description="Libraries currently owned by this worker's watcher runtime. Paths are redacted before they enter the heartbeat.">
        <div v-if="!(status.watchers ?? []).length" class="empty-state">No filesystem watchers are active.</div>
        <div v-else class="watcher-list">
          <div v-for="watcher in status.watchers ?? []" :key="watcher.library_id" class="watcher-row">
            <span>Library {{ watcher.library_id }}</span><code>{{ watcher.path }}</code>
          </div>
        </div>
      </SettingsSection>

      <SettingsSection
        title="Process controls"
        icon="power"
        description="Request a graceful shutdown; Kubernetes, Docker Compose, or the all-in-one supervisor is responsible for starting the process again."
      >
        <div class="process-controls">
          <button class="sv2-btn danger" :disabled="restarting !== null" @click="restartProcess('worker')">
            <Icon v-if="restarting === 'worker'" name="spinner" :size="13" />
            <Icon v-else name="refresh" :size="13" />
            Restart worker
          </button>
          <button class="sv2-btn danger" :disabled="restarting !== null" @click="restartProcess('server')">
            <Icon v-if="restarting === 'server'" name="spinner" :size="13" />
            <Icon v-else name="refresh" :size="13" />
            Restart server
          </button>
          <button class="sv2-btn danger" :disabled="restarting !== null" @click="restartProcess('all')">
            <Icon v-if="restarting === 'all'" name="spinner" :size="13" />
            <Icon v-else name="power" :size="13" />
            Restart both
          </button>
        </div>
      </SettingsSection>

      <SettingsSection title="Recent job activity" icon="clipboard" description="Newest worker-owned jobs, including completed, retryable, cancelled, and discarded outcomes.">
        <div v-if="!recentJobs.length" class="empty-state">No recent job activity is available.</div>
        <div v-else class="job-table">
          <div class="job-row head"><span>Job</span><span>State</span><span>Queue</span><span>Attempt</span><span>Duration</span></div>
          <div v-for="job in recentJobs" :key="job.id" class="job-row">
            <div><strong>{{ job.kind }}</strong><code>#{{ job.id }}</code></div>
            <StatusBadge :state="jobTone(job.state)">{{ job.state }}</StatusBadge>
            <code>{{ job.queue }}</code><span>{{ job.attempt }}/{{ job.max_attempts }}</span><span>{{ jobDuration(job) }}</span>
          </div>
        </div>
      </SettingsSection>
    </template>
  </div>
</template>

<style scoped>
.loading-state, .empty-state, .error-state, .notice-state { display: flex; align-items: center; gap: 8px; padding: 14px; border: 1px solid var(--border); border-radius: var(--r-md); color: var(--fg-3); background: var(--bg-2); }
.error-state { margin-bottom: 12px; color: var(--bad); border-color: color-mix(in srgb, var(--bad) 35%, var(--border)); }
.notice-state { margin-bottom: 12px; color: var(--gold); border-color: color-mix(in srgb, var(--gold) 35%, var(--border)); }
.tiles { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; margin-bottom: 20px; }
.active-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
.active-job { padding: 12px; border: 1px solid var(--border); border-radius: var(--r-md); background: var(--bg-2); }
.active-job > div { display: flex; align-items: center; justify-content: space-between; }
.active-job strong { display: block; margin-top: 10px; color: var(--fg-1); font-size: 12.5px; }
.active-job span, .active-job code { color: var(--fg-3); font-family: var(--font-mono); font-size: 10px; }
.detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.watcher-list { overflow: hidden; border: 1px solid var(--border); border-radius: var(--r-md); }
.watcher-row { display: grid; grid-template-columns: 110px minmax(0, 1fr); gap: 12px; padding: 8px 11px; border-bottom: 1px solid var(--hair); color: var(--fg-3); font-size: 11px; }
.watcher-row:last-child { border-bottom: 0; }
.watcher-row code { overflow: hidden; color: var(--fg-1); font-family: var(--font-mono); font-size: 10.5px; text-overflow: ellipsis; white-space: nowrap; }
.process-controls { display: flex; flex-wrap: wrap; gap: 8px; }
.job-table { overflow-x: auto; border: 1px solid var(--border); border-radius: var(--r-md); }
.job-row { display: grid; grid-template-columns: minmax(220px, 1fr) 100px minmax(130px, .6fr) 70px 85px; gap: 10px; min-width: 690px; min-height: 42px; padding: 7px 11px; align-items: center; border-bottom: 1px solid var(--hair); font-size: 11px; }
.job-row:last-child { border-bottom: 0; }
.job-row.head { min-height: 31px; background: var(--bg-2); color: var(--fg-3); font-family: var(--font-mono); font-size: 9px; text-transform: uppercase; letter-spacing: .07em; }
.job-row > div strong { display: block; color: var(--fg-1); }
.job-row code { color: var(--fg-3); font-family: var(--font-mono); font-size: 10px; }
.job-row > span:last-child, .job-row > span:nth-last-child(2) { font-family: var(--font-mono); color: var(--fg-2); }
@media (max-width: 900px) { .tiles { grid-template-columns: repeat(2, minmax(0, 1fr)); } .detail-grid { grid-template-columns: 1fr; } }
@media (max-width: 620px) { .tiles, .active-grid { grid-template-columns: 1fr; } }
</style>
