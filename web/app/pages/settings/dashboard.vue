<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import type { components } from '#open-fetch-schemas/heya'
type Stats          = components['schemas']['DashboardStats']
type Health         = components['schemas']['HealthBody']
type Ready          = components['schemas']['ReadyBody']
type Transcode      = components['schemas']['TranscodeStatusBody']
type Tailscale      = components['schemas']['TailscaleStatusBody']
type QueueStatus    = components['schemas']['MetadataQueueStatus']
type SummaryRow     = components['schemas']['JobSummaryRow']
type TaskResponse   = components['schemas']['TaskResponse']
type MissingItem    = components['schemas']['MissingMediaItem']
// Sonic status is an open-shape object on the API; treat as any here.
type SonicLike = {
  analyzer_version?: string
  accelerators?: { name: string; label: string; available: boolean; reason?: string }[]
  analyzer?: { state?: string }
  fetcher?: {
    state?: string
    missing_count?: number
    total_count?: number
    total_size?: number
    last_error?: string
    progress?: { files_done?: number; files_total?: number; bytes_done?: number; bytes_total?: number }
  }
}

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()
const { on } = useEventBus()

const stats         = ref<Stats | null>(null)
const health        = ref<Health | null>(null)
const ready         = ref<Ready | null>(null)
const queueStatus   = ref<QueueStatus | null>(null)
const summary       = ref<SummaryRow[]>([])
const transcode     = ref<Transcode | null>(null)
const tailscale     = ref<Tailscale | null>(null)
const sonic         = ref<SonicLike | null>(null)
const tasks         = ref<TaskResponse[]>([])
const missing       = ref<MissingItem[]>([])

const cleaning = ref(false)
const now = ref(Date.now())
let nowTimer: ReturnType<typeof setInterval> | null = null
let queuePoll: ReturnType<typeof setInterval> | null = null

async function loadAll() {
  const [s, h, r, mq, jSum, tc, ts, sa, tk, mi] = await Promise.allSettled([
    $heya('/api/stats'),
    $heya('/api/health'),
    $heya('/api/health/ready'),
    $heya('/api/jobs/queue/metadata'),
    $heya('/api/jobs/summary'),
    $heya('/api/transcode/status'),
    $heya('/api/tailscale/status'),
    $heya('/api/admin/sonicanalysis/status'),
    $heya('/api/tasks'),
    $heya('/api/media/missing'),
  ])
  if (s.status === 'fulfilled')   stats.value = s.value as Stats
  if (h.status === 'fulfilled')   health.value = h.value as Health
  if (r.status === 'fulfilled')   ready.value = r.value as Ready
  if (mq.status === 'fulfilled')  queueStatus.value = mq.value as QueueStatus
  if (jSum.status === 'fulfilled') summary.value = ((jSum.value as SummaryRow[]) ?? []).filter(row => row.state !== 'completed' || row.count > 0)
  if (tc.status === 'fulfilled')  transcode.value = tc.value as Transcode
  if (ts.status === 'fulfilled')  tailscale.value = ts.value as Tailscale
  if (sa.status === 'fulfilled')  sonic.value = sa.value as SonicLike
  if (tk.status === 'fulfilled')  tasks.value = (tk.value as TaskResponse[]) ?? []
  if (mi.status === 'fulfilled')  missing.value = (mi.value as MissingItem[]) ?? []
}

async function refetchQueue() {
  try {
    const [sum, q] = await Promise.all([
      $heya('/api/jobs/summary'),
      $heya('/api/jobs/queue/metadata'),
    ])
    summary.value = (sum as SummaryRow[] ?? []).filter(row => row.state !== 'completed' || row.count > 0)
    queueStatus.value = q
  } catch {}
}

async function refetchStats() {
  try { stats.value = await $heya('/api/stats') } catch {}
}

let statsDebounce: ReturnType<typeof setTimeout> | null = null
function debouncedRefetchStats() {
  if (statsDebounce) clearTimeout(statsDebounce)
  statsDebounce = setTimeout(refetchStats, 2000)
}

async function cleanupMissing() {
  const ok = await confirm({
    title: 'Clean up missing items?',
    message: `Delete ${missing.value.length} missing items and all their metadata. The files have already vanished from disk — this just removes the database rows.`,
    destructive: true,
    confirmLabel: 'Delete rows',
  })
  if (!ok) return
  cleaning.value = true
  try {
    await $heya('/api/media/missing', { method: 'DELETE' })
    // Cleanup removes tracks + albums + media_items; refetch rather than
    // guess at the deltas (res.deleted spans all three).
    missing.value = []
    await refetchStats()
  } catch {} finally { cleaning.value = false }
}

const runningElapsed = computed(() => {
  const r = queueStatus.value?.running
  if (!r?.started_at) return ''
  const ms = now.value - new Date(r.started_at).getTime()
  if (ms < 0 || Number.isNaN(ms)) return ''
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  return `${m}m ${s % 60}s`
})

const subsystemTone = computed<'good' | 'warn' | 'bad'>(() => {
  if (!ready.value) return 'warn'
  return ready.value.status === 'ok' ? 'good' : 'bad'
})

function fmtBytes(b?: number) {
  if (b == null || b === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0; let n = b
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(n < 10 && i > 0 ? 1 : 0)} ${units[i]}`
}
function fmtMB(mb?: number) {
  if (mb == null) return '—'
  if (mb < 1024) return `${mb} MB`
  return `${(mb / 1024).toFixed(1)} GB`
}
function fmtNumber(n: number | undefined) {
  if (n == null) return '—'
  return n.toLocaleString()
}

function componentIcon(name: string): string {
  switch (name) {
    case 'database':   return 'database'
    case 'watcher':    return 'eye'
    case 'scheduler':  return 'timer'
    case 'transcoder': return 'film'
    case 'tailscale':  return 'network'
    default:           return 'pulse'
  }
}

const enabledTasks = computed(() => tasks.value.filter(t => t.enabled || t.state === 'running'))

function taskBadge(t: TaskResponse): { state: 'ok' | 'warn' | 'error' | 'idle', label: string } {
  if (t.state === 'running') return { state: 'ok', label: 'running' }
  if (!t.enabled) return { state: 'idle', label: 'disabled' }
  if (t.last_run_result === 'error') return { state: 'error', label: 'error' }
  if ((t.stats?.failed ?? 0) > 0) return { state: 'warn', label: 'partial' }
  return { state: 'ok', label: 'scheduled' }
}

function timeAgo(ts?: string | null) {
  if (!ts) return 'never'
  const sec = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`
  return `${Math.floor(sec / 86400)}d ago`
}

const buildKv = computed(() => [
  { key: 'Version',    value: health.value?.version ?? '—', mono: true, copy: true },
  { key: 'Database',   value: health.value?.database ?? '—' },
  { key: 'Status',     value: ready.value?.status ?? health.value?.status ?? '—' },
  { key: 'Components', value: ready.value?.components?.length ?? 0 },
])

// Hoist event-bus subscriptions + cleanup to top-level setup. Calling
// lifecycle hooks after an `await` inside an async `onMounted` callback
// loses the active component instance and triggers "onUnmounted is called
// when there is no active component instance to be associated with".
const unsubs = [
  on('media.added',    debouncedRefetchStats),
  on('media.removed',  debouncedRefetchStats),
  on('scan.completed', debouncedRefetchStats),
]

onUnmounted(() => {
  unsubs.forEach(fn => fn())
  if (nowTimer) clearInterval(nowTimer)
  if (queuePoll) clearInterval(queuePoll)
  if (statsDebounce) clearTimeout(statsDebounce)
})

onMounted(async () => {
  await loadAll()
  nowTimer = setInterval(() => { now.value = Date.now() }, 1000)
  queuePoll = setInterval(refetchQueue, 5000)
})
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Dashboard</h2>
      <p class="sv2-page-desc">
        Live snapshot of the server — health, library counts, what's in the
        queue, transcoder + sonic + tailscale at a glance.
      </p>
    </header>

    <section class="tiles tiles-wide">
      <MetricTile label="Libraries" :value="fmtNumber(stats?.libraries)"    icon="folder" />
      <MetricTile label="Movies"    :value="fmtNumber(stats?.media_counts?.movie ?? 0)" icon="film"  />
      <MetricTile label="TV Shows"  :value="fmtNumber(stats?.media_counts?.tv ?? 0)"    icon="tv"    />
      <MetricTile label="Music"     :value="fmtNumber(stats?.media_counts?.music ?? 0)" icon="music" />
      <MetricTile label="Books"     :value="fmtNumber(stats?.media_counts?.book ?? 0)"  icon="book"  />
      <MetricTile label="People"    :value="fmtNumber(stats?.total_people)" icon="users" />
      <MetricTile label="Files"     :value="fmtNumber(stats?.total_files)"  icon="hard-drives" />
      <MetricTile
        label="Missing"
        :value="stats?.missing_count ?? 0"
        icon="warning"
        :tone="(stats?.missing_count ?? 0) > 0 ? 'warn' : 'good'"
        :sub="(stats?.missing_count ?? 0) > 0 ? 'see below' : 'none'"
      />
    </section>

    <SettingsSection title="Server" icon="pulse"
      description="Build info and per-component readiness. Use About for the full breakdown.">
      <template #actions>
        <StatusBadge :state="subsystemTone === 'good' ? 'ok' : subsystemTone === 'warn' ? 'warn' : 'error'">
          {{ subsystemTone === 'good' ? 'All systems' : subsystemTone === 'warn' ? 'Loading' : 'Degraded' }}
        </StatusBadge>
      </template>

      <div class="two-col">
        <KVTable :rows="buildKv" />
        <div v-if="ready" class="comp-list">
          <div v-for="c in ready.components" :key="c.name" class="comp-row">
            <div class="comp-name">
              <Icon :name="componentIcon(c.name)" :size="13" />
              <span>{{ c.name }}</span>
            </div>
            <div class="comp-msg">{{ c.message || (c.ok ? 'healthy' : 'check failed') }}</div>
            <StatusBadge :state="c.ok ? 'ok' : 'error'">{{ c.ok ? 'ok' : 'down' }}</StatusBadge>
          </div>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="Metadata queue" icon="layers">
      <template #actions>
        <NuxtLink to="/settings/tasks" class="link-arrow">
          Open tasks <Icon name="chevright" :size="11" />
        </NuxtLink>
      </template>

      <div class="queue-row">
        <MetricTile label="Pending"  :value="queueStatus?.pending ?? 0"
          :tone="(queueStatus?.pending ?? 0) > 0 ? 'warn' : 'neutral'" icon="list" />
        <MetricTile label="P1 watch/view"  :value="queueStatus?.pending_by_priority?.['1'] ?? 0" icon="lightning" />
        <MetricTile label="P2 movies/TV"   :value="queueStatus?.pending_by_priority?.['2'] ?? 0" icon="film" />
        <MetricTile label="P3 music/books" :value="queueStatus?.pending_by_priority?.['3'] ?? 0" icon="music" />
        <MetricTile
          label="Last 5 min"
          :value="queueStatus?.recent.completed_5min ?? 0"
          icon="check"
          :sub="(queueStatus?.recent.avg_duration_sec ?? 0) > 0 ? `avg ${queueStatus!.recent.avg_duration_sec.toFixed(1)}s` : ''"
        />
      </div>

      <div v-if="queueStatus?.running" class="running-card">
        <div class="running-pulse" />
        <div class="running-info">
          <div class="running-label">Currently enriching · P{{ queueStatus.running.priority }}</div>
          <div class="running-title">
            <span v-if="queueStatus.running.item_title">{{ queueStatus.running.item_title }}</span>
            <span v-else>job #{{ queueStatus.running.job_id }} · {{ queueStatus.running.kind }}</span>
            <span v-if="queueStatus.running.media_type" class="running-type">· {{ queueStatus.running.media_type }}</span>
          </div>
        </div>
        <div class="running-elapsed">{{ runningElapsed }}</div>
      </div>

      <div v-if="summary.length" class="pill-row">
        <span v-for="row in summary" :key="row.state" class="state-pill" :class="row.state">
          <span class="pill-val">{{ row.count }}</span>
          <span class="pill-lbl">{{ row.state }}</span>
        </span>
      </div>
    </SettingsSection>

    <SettingsSection title="Transcoder" icon="film">
      <template #actions>
        <NuxtLink to="/settings/transcoding" class="link-arrow">
          Configure <Icon name="chevright" :size="11" />
        </NuxtLink>
      </template>

      <div v-if="!transcode?.available" class="empty-state">
        <Icon name="info" :size="14" /> ffmpeg not available
      </div>
      <div v-else class="queue-row">
        <MetricTile label="Hardware" :value="transcode.hw_accel_label || transcode.hw_accel || 'Software'" icon="cpu" />
        <MetricTile label="Active jobs" :value="transcode.active_jobs" icon="pulse"
          :tone="transcode.active_jobs > 0 ? 'good' : 'neutral'" />
        <MetricTile label="H.264 encoder" :value="transcode.encoder_h264 || '—'" icon="film" />
        <MetricTile label="HEVC encoder"  :value="transcode.encoder_hevc || '—'" icon="film" />
        <MetricTile
          label="Cache"
          :value="fmtMB(transcode.cache_size_mb)"
          icon="hard-drives"
          :sub="`${transcode.cache_items} items · cap ${transcode.cache_max_gb} GB`"
        />
      </div>
    </SettingsSection>

    <SettingsSection v-if="sonic" title="Sonic analysis" icon="music">
      <template #actions>
        <NuxtLink to="/settings/sonic" class="link-arrow">
          Configure <Icon name="chevright" :size="11" />
        </NuxtLink>
      </template>

      <div class="queue-row">
        <MetricTile
          label="Analyzer"
          :value="sonic.analyzer?.state || 'idle'"
          icon="eq"
          :sub="sonic.analyzer_version ? `v${sonic.analyzer_version}` : ''"
        />
        <MetricTile
          label="Models"
          :value="`${(sonic.fetcher?.total_count ?? 0) - (sonic.fetcher?.missing_count ?? 0)} / ${sonic.fetcher?.total_count ?? 0}`"
          icon="database"
          :sub="sonic.fetcher?.total_size ? fmtBytes(sonic.fetcher.total_size) : ''"
        />
        <MetricTile
          label="Fetcher"
          :value="sonic.fetcher?.state ?? '—'"
          icon="refresh"
          :sub="sonic.fetcher?.progress
            ? `${sonic.fetcher.progress.files_done}/${sonic.fetcher.progress.files_total}`
            : ''"
          :tone="sonic.fetcher?.last_error ? 'bad' : 'neutral'"
        />
      </div>

      <div v-if="sonic.accelerators?.length" class="accel-row">
        <span
          v-for="a in sonic.accelerators"
          :key="a.name"
          class="accel-chip"
          :class="{ off: !a.available }"
          :title="a.available ? `${a.label} available` : (a.reason || `${a.label} unavailable`)"
        >
          <span class="accel-dot" :class="{ on: a.available }" />
          {{ a.label || a.name }}
        </span>
      </div>

      <div v-if="sonic.fetcher?.last_error" class="empty-state err">
        <Icon name="warning" :size="14" />
        {{ sonic.fetcher.last_error }}
      </div>
    </SettingsSection>

    <SettingsSection v-if="tailscale?.enabled" title="Tailscale" icon="network">
      <template #actions>
        <NuxtLink to="/settings/network" class="link-arrow">
          Configure <Icon name="chevright" :size="11" />
        </NuxtLink>
      </template>

      <KVTable :rows="[
        { key: 'Hostname', value: tailscale.status?.hostname ?? tailscale.config?.hostname ?? '—', mono: true, copy: true },
        { key: 'MagicDNS', value: tailscale.status?.magic_dns ?? '', mono: true },
        { key: 'Backend',  value: tailscale.status?.backend_state ?? (tailscale.status?.running ? 'Running' : 'Stopped') },
        { key: 'IPv4',     value: tailscale.status?.ipv4 ?? '—', mono: true, copy: true },
        { key: 'IPv6',     value: tailscale.status?.ipv6 ?? '', mono: true, copy: true },
        { key: 'HTTPS',    value: tailscale.status?.https_active ? 'Active' : (tailscale.config?.https ? 'Enabled' : 'Off') },
        { key: 'Funnel',   value: tailscale.status?.funnel_active ? 'Public' : (tailscale.config?.funnel ? 'Enabled' : 'Off') },
        { key: 'Funnel URL', value: tailscale.status?.funnel_url ?? '', mono: true, copy: true },
        { key: 'Last error', value: tailscale.status?.last_error ?? '' },
      ]" />
    </SettingsSection>

    <SettingsSection v-if="enabledTasks.length" title="Scheduled tasks" icon="timer">
      <template #actions>
        <NuxtLink to="/settings/tasks" class="link-arrow">
          Manage <Icon name="chevright" :size="11" />
        </NuxtLink>
      </template>

      <div class="task-list">
        <div v-for="t in enabledTasks" :key="t.id" class="task-row">
          <StatusBadge :state="taskBadge(t).state">{{ taskBadge(t).label }}</StatusBadge>
          <div class="task-name">{{ t.display_name }}</div>
          <div class="task-meta">
            <span v-if="t.state === 'running'">running</span>
            <span v-else-if="t.last_run_at">last {{ timeAgo(t.last_run_at) }}</span>
            <span v-else>not yet run</span>
            <span v-if="t.stats?.pending" class="pending">· {{ t.stats.pending }} pending</span>
            <span v-if="t.stats?.failed"  class="failed">· {{ t.stats.failed }} failed</span>
          </div>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection v-if="missing.length" title="Missing media" icon="warning"
      :description="`${missing.length} item${missing.length === 1 ? '' : 's'} no longer found on disk. Cleaning removes the DB rows; the files are already gone.`">
      <template #actions>
        <button class="sv2-btn danger" :disabled="cleaning" @click="cleanupMissing">
          <Icon name="trash" :size="12" />
          {{ cleaning ? 'Cleaning…' : 'Clean up all' }}
        </button>
      </template>

      <div class="missing-scroll">
        <div v-for="item in missing" :key="`${item.media_type}-${item.id}`" class="missing-tile">
          <div class="missing-poster">
            <NuxtImg
              v-if="item.poster_path && !item.poster_path.startsWith('http')"
              :src="`/api/media/${item.id}/image/poster`"
              :width="200"
              :quality="80"
              loading="lazy"
            />
            <div v-else class="missing-empty">
              <Icon :name="item.media_type === 'movie' ? 'film' : item.media_type === 'tv' ? 'tv' : 'music'" :size="16" />
            </div>
            <div class="missing-badge">Missing</div>
          </div>
          <div class="missing-meta">
            <div class="missing-title">{{ item.title }}</div>
            <div class="missing-sub">{{ item.year }} · {{ item.media_type }}</div>
          </div>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="Recent activity" icon="clipboard"
      description="Live event feed from the WebSocket bus — scans, enrichments, additions.">
      <div class="activity-wrap">
        <LazyActivityFeed />
      </div>
    </SettingsSection>
  </div>
</template>

<style scoped>
.sv2-page-head { margin-bottom: 28px; }
.sv2-page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.sv2-page-desc { margin: 6px 0 0; font-size: 13px; color: var(--fg-3); line-height: 1.55; }

.tiles {
  display: grid;
  gap: 8px;
  margin-bottom: 28px;
}
.tiles-wide {
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
}

.two-col {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
  align-items: start;
}
@media (max-width: 900px) {
  .two-col { grid-template-columns: 1fr; }
}

.comp-list {
  display: flex; flex-direction: column;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
  overflow: hidden;
}
.comp-row {
  display: grid;
  grid-template-columns: 130px 1fr auto;
  align-items: center;
  gap: 12px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--border);
  font-size: 12px;
}
.comp-row:last-child { border-bottom: 0; }
.comp-name { display: flex; align-items: center; gap: 7px; color: var(--fg-1); font-weight: 500; }
.comp-msg  { color: var(--fg-3); font-family: var(--font-mono); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.queue-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 8px;
}

.running-card {
  display: flex; align-items: center; gap: 14px;
  background: rgba(230, 185, 74, 0.06);
  border: 1px solid rgba(230, 185, 74, 0.25);
  border-radius: var(--r-md);
  padding: 12px 16px;
  margin-top: 10px;
}
.running-pulse {
  width: 10px; height: 10px;
  border-radius: 50%; background: var(--gold);
  flex-shrink: 0;
  animation: dash-pulse 1.6s infinite;
}
@keyframes dash-pulse {
  0%   { box-shadow: 0 0 0 0   rgba(230, 185, 74, 0.6); }
  70%  { box-shadow: 0 0 0 12px rgba(230, 185, 74, 0); }
  100% { box-shadow: 0 0 0 0   rgba(230, 185, 74, 0); }
}
.running-info { flex: 1; min-width: 0; }
.running-label {
  font-size: 10px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em;
  color: var(--fg-3);
}
.running-title {
  font-size: 13.5px; font-weight: 500; color: var(--fg-0);
  margin-top: 4px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.running-type { color: var(--fg-3); font-weight: 400; margin-left: 4px; }
.running-elapsed { font-family: var(--font-mono); font-size: 13px; color: var(--gold); font-variant-numeric: tabular-nums; }

.pill-row {
  display: flex; flex-wrap: wrap; gap: 6px;
  margin-top: 10px;
}
.state-pill {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 4px 10px; border-radius: 999px;
  font-family: var(--font-mono); font-size: 11px;
  background: var(--bg-2); border: 1px solid var(--border);
  text-transform: capitalize;
}
.pill-val { font-weight: 700; color: var(--fg-0); }
.pill-lbl { color: var(--fg-3); }
.state-pill.running   { border-color: rgba(111,191,124,0.3); }
.state-pill.available { border-color: rgba(230,185,74,0.3); }
.state-pill.retryable { border-color: rgba(230,185,74,0.3); }
.state-pill.discarded { border-color: rgba(217,107,107,0.3); }

.empty-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.empty-state.err { color: var(--bad); background: rgba(217,107,107,0.06); border-color: rgba(217,107,107,0.25); margin-top: 10px; }

.accel-row { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 10px; }
.accel-chip {
  display: inline-flex; align-items: center; gap: 6px;
  font-family: var(--font-mono); font-size: 11px;
  padding: 3px 10px; border-radius: 999px;
  background: var(--bg-2); border: 1px solid var(--border);
  color: var(--fg-1);
}
.accel-chip.off { color: var(--fg-3); opacity: 0.6; }
.accel-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--fg-4); }
.accel-dot.on { background: var(--good); }

.task-list { display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: var(--r-md); overflow: hidden; background: var(--bg-2); }
.task-row {
  display: grid;
  grid-template-columns: 110px 1fr auto;
  align-items: center;
  gap: 12px;
  padding: 9px 14px;
  border-bottom: 1px solid var(--border);
  font-size: 12.5px;
}
.task-row:last-child { border-bottom: 0; }
.task-name { font-weight: 500; color: var(--fg-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.task-meta { font-family: var(--font-mono); font-size: 11px; color: var(--fg-3); display: flex; flex-wrap: wrap; gap: 4px; justify-content: flex-end; }
.task-meta .pending { color: var(--gold); }
.task-meta .failed  { color: var(--bad); }

.missing-scroll {
  display: flex; gap: 10px;
  overflow-x: auto; overflow-y: hidden;
  padding-bottom: 4px;
  scrollbar-width: none;
}
.missing-scroll::-webkit-scrollbar { display: none; }
.missing-tile { width: 120px; flex-shrink: 0; opacity: 0.75; }
.missing-poster {
  position: relative;
  border-radius: var(--r-md);
  overflow: hidden;
  aspect-ratio: 2/3;
  background: var(--bg-3);
}
.missing-poster img { width: 100%; height: 100%; object-fit: cover; filter: grayscale(0.6); }
.missing-empty { width: 100%; height: 100%; display: flex; align-items: center; justify-content: center; color: var(--fg-3); }
.missing-badge {
  position: absolute; top: 6px; right: 6px;
  font-size: 8px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em;
  padding: 2px 6px; border-radius: 999px;
  background: rgba(217, 107, 107, 0.85); color: #fff;
}
.missing-meta { margin-top: 6px; }
.missing-title { font-size: 11px; font-weight: 500; color: var(--fg-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.missing-sub { font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); }

.activity-wrap {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 8px;
}

.link-arrow {
  display: inline-flex; align-items: center; gap: 2px;
  font-size: 11px;
  color: var(--fg-3);
  text-decoration: none;
}
.link-arrow:hover { color: var(--gold); }

.sv2-btn {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 6px 12px;
  border-radius: var(--r-sm);
  font-size: 11.5px; font-weight: 500;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.sv2-btn.danger {
  border: 1px solid rgba(217,107,107,0.30);
  background: rgba(217,107,107,0.06);
  color: var(--bad);
}
.sv2-btn.danger:hover:not(:disabled) { background: rgba(217,107,107,0.12); }
.sv2-btn:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
