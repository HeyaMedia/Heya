<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">Dashboard</h2>
      <p class="page-desc">Overview of your media server</p>
    </div>

    <!-- Server health: per-component status from /api/health/ready -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="pulse" :size="14" />
        Server
      </h3>
      <div class="server-grid">
        <div class="server-card">
          <div class="server-card-label">Version</div>
          <div class="server-card-value">{{ health?.version ?? '—' }}</div>
        </div>
        <div class="server-card">
          <div class="server-card-label">Status</div>
          <div class="server-card-value">
            <span class="dot-led" :class="ready?.status === 'ok' ? 'good' : 'bad'" />
            {{ ready?.status === 'ok' ? 'Healthy' : (ready?.status ?? 'Unknown') }}
          </div>
        </div>
        <div class="server-card">
          <div class="server-card-label">Database</div>
          <div class="server-card-value">
            <span class="dot-led" :class="health?.database === 'connected' ? 'good' : 'bad'" />
            {{ health?.database === 'connected' ? 'Connected' : (health?.database ?? 'Unknown') }}
          </div>
        </div>
      </div>
      <div v-if="ready?.components?.length" class="health-grid" style="margin-top: 12px">
        <div v-for="c in ready.components" :key="c.name" class="health-card">
          <div class="health-indicator" :class="c.ok ? 'good' : 'bad'" />
          <div class="health-info">
            <div class="health-label">{{ formatComponent(c.name) }}</div>
            <div class="health-status">{{ c.ok ? 'Online' : (c.message || 'Down') }}</div>
          </div>
        </div>
      </div>
    </section>

    <!-- Library counts -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="folder" :size="14" />
        Media Library
      </h3>
      <div class="stat-grid">
        <div v-for="s in mediaStats" :key="s.label" class="stat-card">
          <div class="stat-icon" :style="{ background: s.bg, color: s.color }">
            <Icon :name="s.icon" :size="18" />
          </div>
          <div class="stat-body">
            <div class="stat-value">{{ s.value }}</div>
            <div class="stat-label">{{ s.label }}</div>
          </div>
        </div>
      </div>
    </section>

    <!-- Job queue: enrich-queue priority bands + currently running -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="layers" :size="14" />
        Metadata Queue
        <NuxtLink to="/settings/tasks" class="section-link">Open tasks &rarr;</NuxtLink>
      </h3>
      <div class="queue-row">
        <div class="queue-card">
          <div class="queue-card-label">Pending</div>
          <div class="queue-card-value">{{ metaQueue?.pending ?? 0 }}</div>
        </div>
        <div class="queue-card">
          <div class="queue-card-label">P1 watcher / view</div>
          <div class="queue-card-value">{{ metaQueue?.pending_by_priority?.['1'] ?? 0 }}</div>
        </div>
        <div class="queue-card">
          <div class="queue-card-label">P2 movies / TV</div>
          <div class="queue-card-value">{{ metaQueue?.pending_by_priority?.['2'] ?? 0 }}</div>
        </div>
        <div class="queue-card">
          <div class="queue-card-label">P3 music / books</div>
          <div class="queue-card-value">{{ metaQueue?.pending_by_priority?.['3'] ?? 0 }}</div>
        </div>
        <div class="queue-card">
          <div class="queue-card-label">Last 5 min</div>
          <div class="queue-card-value">{{ metaQueue?.recent?.completed_5min ?? 0 }}</div>
          <div class="queue-card-sub" v-if="metaQueue?.recent?.avg_duration_sec">
            avg {{ metaQueue.recent.avg_duration_sec.toFixed(1) }}s
          </div>
        </div>
      </div>
      <div v-if="metaQueue?.running" class="running-card">
        <div class="running-pulse" />
        <div class="running-info">
          <div class="running-label">Currently enriching · P{{ metaQueue.running.priority }}</div>
          <div class="running-title">
            <span v-if="metaQueue.running.item_title">{{ metaQueue.running.item_title }}</span>
            <span v-else>job #{{ metaQueue.running.job_id }} · {{ metaQueue.running.kind }}</span>
            <span v-if="metaQueue.running.media_type" class="running-type">· {{ metaQueue.running.media_type }}</span>
          </div>
        </div>
        <div class="running-elapsed">{{ runningElapsed }}</div>
      </div>
      <div v-if="jobSummary.length" class="job-summary-grid">
        <div v-for="row in jobSummary" :key="row.state" class="job-summary-pill" :class="`pill-${row.state}`">
          <span class="pill-value">{{ row.count }}</span>
          <span class="pill-label">{{ row.state }}</span>
        </div>
      </div>
    </section>

    <!-- Transcoder -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="film" :size="14" />
        Transcoder
      </h3>
      <div v-if="!transcode?.available" class="empty-row">
        <Icon name="info" :size="14" />
        ffmpeg not available
      </div>
      <div v-else class="server-grid">
        <div class="server-card">
          <div class="server-card-label">Hardware</div>
          <div class="server-card-value">{{ transcode?.hw_accel_label || transcode?.hw_accel || 'Software' }}</div>
        </div>
        <div class="server-card">
          <div class="server-card-label">H.264 encoder</div>
          <div class="server-card-value mono">{{ transcode?.encoder_h264 || '—' }}</div>
        </div>
        <div class="server-card">
          <div class="server-card-label">HEVC encoder</div>
          <div class="server-card-value mono">{{ transcode?.encoder_hevc || '—' }}</div>
        </div>
        <div class="server-card">
          <div class="server-card-label">Cache size</div>
          <div class="server-card-value">
            {{ formatMB(transcode?.cache_size_mb) }}
            <span class="server-card-sub" v-if="transcode?.cache_max_gb">/ {{ transcode.cache_max_gb }} GB</span>
          </div>
          <div class="server-card-sub" v-if="transcode?.cache_items != null">{{ transcode.cache_items }} cached items</div>
        </div>
      </div>
    </section>

    <!-- Tailscale (only if enabled) -->
    <section v-if="tailscale?.enabled" class="section">
      <h3 class="section-heading">
        <Icon name="globe" :size="14" />
        Tailscale
        <NuxtLink to="/settings/tailscale" class="section-link">Configure &rarr;</NuxtLink>
      </h3>
      <div class="server-grid">
        <div class="server-card">
          <div class="server-card-label">Hostname</div>
          <div class="server-card-value mono">{{ tailscale.status?.hostname || tailscale.config?.hostname || '—' }}</div>
          <div class="server-card-sub" v-if="tailscale.status?.magic_dns">{{ tailscale.status.magic_dns }}</div>
        </div>
        <div class="server-card">
          <div class="server-card-label">Backend</div>
          <div class="server-card-value">
            <span class="dot-led" :class="tailscale.status?.running ? 'good' : 'bad'" />
            {{ tailscale.status?.backend_state || (tailscale.status?.running ? 'Running' : 'Stopped') }}
          </div>
        </div>
        <div class="server-card">
          <div class="server-card-label">Node IPv4</div>
          <div class="server-card-value mono">{{ tailscale.status?.ipv4 || '—' }}</div>
          <div class="server-card-sub mono" v-if="tailscale.status?.ipv6">{{ tailscale.status.ipv6 }}</div>
        </div>
        <div class="server-card">
          <div class="server-card-label">HTTPS</div>
          <div class="server-card-value">
            <span class="dot-led" :class="tailscale.status?.https_active ? 'good' : 'idle'" />
            {{ tailscale.status?.https_active ? 'Active' : (tailscale.config?.https ? 'Enabled' : 'Off') }}
          </div>
        </div>
        <div class="server-card">
          <div class="server-card-label">Funnel</div>
          <div class="server-card-value">
            <span class="dot-led" :class="tailscale.status?.funnel_active ? 'good' : 'idle'" />
            {{ tailscale.status?.funnel_active ? 'Public' : (tailscale.config?.funnel ? 'Enabled' : 'Off') }}
          </div>
          <div class="server-card-sub" v-if="tailscale.status?.funnel_url">{{ tailscale.status.funnel_url }}</div>
        </div>
        <div class="server-card" v-if="tailscale.status?.last_error">
          <div class="server-card-label">Last error</div>
          <div class="server-card-value bad-text">{{ tailscale.status.last_error }}</div>
        </div>
      </div>
    </section>

    <!-- Sonic analysis -->
    <section v-if="sonic" class="section">
      <h3 class="section-heading">
        <Icon name="music" :size="14" />
        Sonic Analysis
        <NuxtLink to="/settings/sonic-analysis" class="section-link">Configure &rarr;</NuxtLink>
      </h3>
      <div class="server-grid">
        <div class="server-card">
          <div class="server-card-label">Analyzer</div>
          <div class="server-card-value">{{ sonic.analyzer?.state || 'Idle' }}</div>
          <div class="server-card-sub mono" v-if="sonic.analyzer_version">v{{ sonic.analyzer_version }}</div>
        </div>
        <div class="server-card">
          <div class="server-card-label">Models</div>
          <div class="server-card-value">
            <template v-if="sonic.fetcher">
              {{ (sonic.fetcher.total_count ?? 0) - (sonic.fetcher.missing_count ?? 0) }} / {{ sonic.fetcher.total_count ?? 0 }}
            </template>
            <template v-else>—</template>
          </div>
          <div class="server-card-sub" v-if="sonic.fetcher?.total_size">{{ formatBytes(sonic.fetcher.total_size) }}</div>
        </div>
        <div class="server-card">
          <div class="server-card-label">Fetcher</div>
          <div class="server-card-value">{{ sonic.fetcher?.state || '—' }}</div>
          <div class="server-card-sub" v-if="sonic.fetcher?.progress">
            {{ sonic.fetcher.progress.files_done }}/{{ sonic.fetcher.progress.files_total }} ·
            {{ formatBytes(sonic.fetcher.progress.bytes_done) }}/{{ formatBytes(sonic.fetcher.progress.bytes_total) }}
          </div>
        </div>
        <div class="server-card">
          <div class="server-card-label">Accelerators</div>
          <div class="accel-list" v-if="sonic.accelerators?.length">
            <span
              v-for="a in sonic.accelerators"
              :key="a.name"
              class="accel-chip"
              :class="{ off: !a.available }"
              :title="a.available ? `${a.label} available` : (a.reason || `${a.label} unavailable`)"
            >
              <span class="accel-dot" :class="a.available ? 'good' : 'idle'" />
              {{ a.label || a.name }}
            </span>
          </div>
          <div v-else class="server-card-value mono">cpu</div>
        </div>
      </div>
      <div v-if="sonic.fetcher?.last_error" class="empty-row bad-text" style="margin-top: 10px">
        <Icon name="warning" :size="14" />
        {{ sonic.fetcher.last_error }}
      </div>
    </section>

    <!-- Scheduled tasks summary: which are enabled, last run, items pending -->
    <section v-if="tasks.length" class="section">
      <h3 class="section-heading">
        <Icon name="clock" :size="14" />
        Scheduled Tasks
        <NuxtLink to="/settings/tasks" class="section-link">Manage &rarr;</NuxtLink>
      </h3>
      <div class="task-list">
        <div v-for="t in tasks" :key="t.id" class="task-row" :class="{ disabled: !t.enabled, busy: t.state === 'running' }">
          <div class="task-led" :class="taskLedClass(t)" />
          <div class="task-main">
            <div class="task-name">{{ t.display_name }}</div>
            <div class="task-meta">
              <span v-if="!t.enabled">disabled</span>
              <span v-else-if="t.state === 'running'">running · {{ progressPercent(t) }}</span>
              <span v-else-if="t.last_run_at">last {{ timeAgo(t.last_run_at) }}{{ t.last_run_result ? ` · ${t.last_run_result}` : '' }}</span>
              <span v-else>not yet run</span>
              <span v-if="t.stats?.pending" class="task-pending">· {{ t.stats.pending }} pending</span>
              <span v-if="t.stats?.failed" class="task-failed">· {{ t.stats.failed }} failed</span>
            </div>
          </div>
          <button
            v-if="t.enabled && t.state !== 'running'"
            class="task-run-btn"
            :disabled="taskTriggering === t.id"
            @click="triggerTask(t.id)"
          >
            <Icon name="play" :size="12" />
            Run
          </button>
        </div>
      </div>
    </section>

    <!-- Recent Activity (moved from frontpage) -->
    <section class="section">
      <ActivityFeed />
    </section>

    <!-- Missing media: items whose files vanished -->
    <section v-if="missingItems.length" class="section">
      <h3 class="section-heading">
        <Icon name="warning" :size="14" />
        Missing Media
      </h3>
      <div class="missing-header">
        <div class="missing-summary">
          <Icon name="warning" :size="14" />
          <span>{{ missingItems.length }} item{{ missingItems.length > 1 ? 's' : '' }} no longer found on disk</span>
        </div>
        <button class="btn btn-secondary" :disabled="cleaning" @click="cleanupMissing">
          <Icon name="trash" :size="14" />
          {{ cleaning ? 'Cleaning…' : 'Clean up all' }}
        </button>
      </div>
      <div class="missing-scroll">
        <div v-for="item in missingItems" :key="item.id" class="missing-tile">
          <div class="missing-poster">
            <img v-if="item.poster_path && !item.poster_path.startsWith('http')" :src="`/api/media/${item.id}/image/poster`" />
            <div v-else class="missing-poster-empty">
              <Icon :name="item.media_type === 'movie' ? 'film' : item.media_type === 'tv' ? 'tv' : 'music'" :size="16" />
            </div>
            <div class="missing-badge">Missing</div>
          </div>
          <div class="missing-meta">
            <div class="missing-tile-title">{{ item.title }}</div>
            <div class="missing-tile-sub">{{ item.year }} · {{ item.media_type }}</div>
          </div>
        </div>
      </div>
    </section>

  </div>
</template>

<script setup lang="ts">
import type { HealthResponse } from '~~/shared/types'

interface DashboardStats {
  libraries: number
  media_counts: Record<string, number>
  total_media: number
  total_people: number
  total_files: number
  missing_count: number
  queue_pending: number
  queue_running: number
}

interface MissingItem {
  id: number
  title: string
  year: string
  media_type: string
  poster_path: string
  slug: string
}

interface HealthComponent { name: string; ok: boolean; message?: string }
interface ReadyResponse { status: string; components: HealthComponent[] }

interface JobSummaryRow { state: string; count: number }

interface MetadataQueueRunning {
  job_id: number
  kind: string
  priority: number
  item_id?: number
  item_title?: string
  media_type?: string
  source?: string
  started_at: string
}
interface MetadataQueueStatus {
  pending: number
  pending_by_priority: Record<string, number>
  running?: MetadataQueueRunning
  recent: { completed_5min: number; avg_duration_sec: number }
}

interface TranscodeStatus {
  available: boolean
  hw_accel: string
  hw_accel_label: string
  encoder_h264: string
  encoder_hevc: string
  cache_dir: string
  cache_max_gb: number
  cache_size_mb: number
  cache_items: number
  config_mode: string
}

interface TailscaleStatusBlock {
  enabled?: boolean
  running?: boolean
  hostname?: string
  backend_state?: string
  magic_dns?: string
  ipv4?: string
  ipv6?: string
  cert_domain?: string
  https?: boolean
  https_active?: boolean
  https_url?: string
  funnel?: boolean
  funnel_active?: boolean
  funnel_url?: string
  login_url?: string
  last_error?: string
}
interface TailscaleStatus {
  enabled: boolean
  config?: { enabled: boolean; hostname: string; https: boolean; funnel: boolean }
  status?: TailscaleStatusBlock
  message?: string
}

interface SonicProgress {
  current_file?: string
  bytes_done?: number
  bytes_total?: number
  files_done?: number
  files_total?: number
  started_at?: string
}
interface SonicFetcher {
  state?: string
  all_present?: boolean
  missing_count?: number
  total_count?: number
  total_size?: number
  progress?: SonicProgress
  last_error?: string
}
interface AcceleratorAvailability {
  name: string
  label: string
  available: boolean
  reason?: string
}
interface SonicStatus {
  analyzer_version?: string
  accelerators?: AcceleratorAvailability[]
  fetcher?: SonicFetcher
  analyzer?: { state?: string }
  text_searcher?: { ready?: boolean }
}

interface ScheduledTaskStats {
  pending?: number
  running?: number
  done?: number
  failed?: number
}
interface ScheduledTask {
  id: string
  display_name: string
  description: string
  category: string
  enabled: boolean
  interval_hours: number
  state: string
  last_run_at: string | null
  last_run_result: string
  last_run_duration_sec: number
  last_run_items_processed: number
  last_run_items_total: number
  next_run_at: string | null
  progress: { state?: string; completed?: number; total?: number; current_item?: string } | null
  stats?: ScheduledTaskStats
}

const stats = ref<DashboardStats | null>(null)
const health = ref<HealthResponse | null>(null)
const ready = ref<ReadyResponse | null>(null)
const missingItems = ref<MissingItem[]>([])
const jobSummary = ref<JobSummaryRow[]>([])
const metaQueue = ref<MetadataQueueStatus | null>(null)
const transcode = ref<TranscodeStatus | null>(null)
const tailscale = ref<TailscaleStatus | null>(null)
const sonic = ref<SonicStatus | null>(null)
const tasks = ref<ScheduledTask[]>([])
const cleaning = ref(false)
const taskTriggering = ref<string | null>(null)
const now = ref(Date.now())

let nowTimer: ReturnType<typeof setInterval> | null = null
let queuePoll: ReturnType<typeof setInterval> | null = null
let tasksPoll: ReturnType<typeof setInterval> | null = null
let statsTimer: ReturnType<typeof setTimeout> | null = null

async function cleanupMissing() {
  if (!confirm(`Delete ${missingItems.value.length} missing items and all their metadata? This cannot be undone.`)) return
  cleaning.value = true
  try {
    const { $heya } = useNuxtApp()
    const result = await $heya('/api/media/missing', { method: 'DELETE' }) as { deleted: number }
    missingItems.value = []
    if (stats.value) {
      stats.value.missing_count = 0
      stats.value.total_media -= result.deleted
    }
  } catch {}
  cleaning.value = false
}

async function triggerTask(id: string) {
  taskTriggering.value = id
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/tasks/{id}/run', { method: 'POST', path: { id: id as any } })
    await refetchTasks()
  } catch {}
  taskTriggering.value = null
}

const mediaStats = computed(() => [
  { label: 'Libraries', value: stats.value?.libraries ?? '–', icon: 'folder', bg: 'var(--gold-soft)', color: 'var(--gold)' },
  { label: 'Movies', value: stats.value?.media_counts?.movie ?? 0, icon: 'film', bg: 'rgba(230, 185, 74, 0.12)', color: 'var(--gold)' },
  { label: 'TV Shows', value: stats.value?.media_counts?.tv ?? 0, icon: 'tv', bg: 'rgba(140, 160, 255, 0.12)', color: 'rgb(140, 160, 255)' },
  { label: 'Music', value: stats.value?.media_counts?.music ?? 0, icon: 'music', bg: 'rgba(200, 140, 255, 0.12)', color: 'rgb(200, 140, 255)' },
  { label: 'Books', value: stats.value?.media_counts?.book ?? 0, icon: 'book', bg: 'rgba(140, 220, 180, 0.12)', color: 'rgb(140, 220, 180)' },
  { label: 'People', value: stats.value?.total_people ?? 0, icon: 'users', bg: 'rgba(255, 255, 255, 0.04)', color: 'var(--fg-2)' },
  { label: 'Files', value: stats.value?.total_files ?? 0, icon: 'hard-drives', bg: 'rgba(255, 255, 255, 0.04)', color: 'var(--fg-2)' },
])

const runningElapsed = computed(() => {
  const r = metaQueue.value?.running
  if (!r?.started_at) return ''
  const ms = now.value - new Date(r.started_at).getTime()
  if (ms < 0 || Number.isNaN(ms)) return ''
  const s = Math.floor(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  return `${m}m ${s % 60}s`
})

function formatComponent(name: string) {
  return name.charAt(0).toUpperCase() + name.slice(1)
}

function formatMB(mb: number | undefined) {
  if (mb == null) return '—'
  if (mb < 1024) return `${mb} MB`
  return `${(mb / 1024).toFixed(1)} GB`
}

function formatBytes(b: number | undefined) {
  if (b == null || b === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let n = b
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(n < 10 && i > 0 ? 1 : 0)} ${units[i]}`
}

function timeAgo(ts: string) {
  const diff = Date.now() - new Date(ts).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

function taskLedClass(t: ScheduledTask) {
  if (!t.enabled) return 'idle'
  if (t.state === 'running') return 'active'
  if (t.last_run_result === 'error') return 'bad'
  if (t.stats?.failed && t.stats.failed > 0) return 'warn'
  return 'good'
}

function progressPercent(t: ScheduledTask) {
  const p = t.progress
  if (!p || !p.total) return ''
  const pct = Math.round(((p.completed ?? 0) * 100) / p.total)
  return `${pct}%`
}

const { on } = useEventBus()

async function refetchStats() {
  try {
    const { $heya } = useNuxtApp()
    stats.value = await $heya('/api/stats') as DashboardStats
  } catch {}
}

async function refetchQueue() {
  try {
    const { $heya } = useNuxtApp()
    const [summary, meta] = await Promise.all([
      $heya('/api/jobs/summary') as Promise<JobSummaryRow[]>,
      $heya('/api/jobs/queue/metadata') as Promise<MetadataQueueStatus>,
    ])
    jobSummary.value = (summary || []).filter(r => r.state !== 'completed' || r.count > 0)
    metaQueue.value = meta
  } catch {}
}

async function refetchTasks() {
  try {
    const { $heya } = useNuxtApp()
    tasks.value = await $heya('/api/tasks') as ScheduledTask[]
  } catch {}
}

function debouncedRefetchStats() {
  if (statsTimer) clearTimeout(statsTimer)
  statsTimer = setTimeout(refetchStats, 2000)
}

onMounted(async () => {
  const { $heya } = useNuxtApp()
  const [s, h, r, m, jobSum, mq, tc, ts, sa, tk] = await Promise.allSettled([
    $heya('/api/stats') as Promise<DashboardStats>,
    $heya('/api/health') as Promise<HealthResponse>,
    $heya('/api/health/ready') as Promise<ReadyResponse>,
    $heya('/api/media/missing') as Promise<MissingItem[]>,
    $heya('/api/jobs/summary') as Promise<JobSummaryRow[]>,
    $heya('/api/jobs/queue/metadata') as Promise<MetadataQueueStatus>,
    $heya('/api/transcode/status') as Promise<TranscodeStatus>,
    $heya('/api/tailscale/status') as Promise<TailscaleStatus>,
    $heya('/api/admin/sonicanalysis/status') as Promise<SonicStatus>,
    $heya('/api/tasks') as Promise<ScheduledTask[]>,
  ])
  if (s.status === 'fulfilled') stats.value = s.value
  if (h.status === 'fulfilled') health.value = h.value
  if (r.status === 'fulfilled') ready.value = r.value
  if (m.status === 'fulfilled') missingItems.value = m.value ?? []
  if (jobSum.status === 'fulfilled') jobSummary.value = (jobSum.value || []).filter(row => row.state !== 'completed' || row.count > 0)
  if (mq.status === 'fulfilled') metaQueue.value = mq.value
  if (tc.status === 'fulfilled') transcode.value = tc.value
  if (ts.status === 'fulfilled') tailscale.value = ts.value
  if (sa.status === 'fulfilled') sonic.value = sa.value
  if (tk.status === 'fulfilled') tasks.value = tk.value ?? []

  nowTimer = setInterval(() => { now.value = Date.now() }, 1000)
  queuePoll = setInterval(refetchQueue, 5000)
  tasksPoll = setInterval(refetchTasks, 10000)

  const unsubs = [
    on('media.added', debouncedRefetchStats),
    on('media.removed', debouncedRefetchStats),
    on('scan.completed', debouncedRefetchStats),
    on('stats.updated', (event) => {
      const p = event.payload as DashboardStats
      if (stats.value) {
        stats.value.libraries = p.libraries
        stats.value.media_counts = p.media_counts
        stats.value.total_media = p.total_media
        stats.value.total_people = p.total_people
        stats.value.total_files = p.total_files
        stats.value.queue_pending = p.queue_pending
        stats.value.queue_running = p.queue_running
      } else {
        stats.value = { ...p, missing_count: 0 } as DashboardStats
      }
    }),
  ]

  onUnmounted(() => {
    unsubs.forEach(fn => fn())
    if (statsTimer) clearTimeout(statsTimer)
    if (nowTimer) clearInterval(nowTimer)
    if (queuePoll) clearInterval(queuePoll)
    if (tasksPoll) clearInterval(tasksPoll)
  })
})
</script>

<style scoped>
.page-header { margin-bottom: 32px; }
.page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.page-desc { font-size: 13px; color: var(--fg-3); margin: 6px 0 0; }

.section { margin-bottom: 36px; }
.section-heading {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-3);
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin: 0 0 14px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}
.section-link {
  margin-left: auto;
  color: var(--fg-3);
  font-weight: 500;
  font-size: 10px;
  text-decoration: none;
}
.section-link:hover { color: var(--gold); }

.server-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 10px;
}
.server-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 14px 16px;
}
.server-card-label {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  margin-bottom: 6px;
}
.server-card-value {
  font-size: 15px;
  font-weight: 600;
  color: var(--fg-0);
  display: flex;
  align-items: center;
  gap: 8px;
}
.server-card-value.mono { font-family: var(--font-mono); font-size: 13px; }
.server-card-sub {
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  margin-top: 4px;
}
.bad-text { color: var(--bad); }

.dot-led {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  display: inline-block;
  flex-shrink: 0;
}
.dot-led.good { background: var(--good); box-shadow: 0 0 6px rgba(111, 191, 124, 0.5); }
.dot-led.bad { background: var(--bad); box-shadow: 0 0 6px rgba(217, 107, 107, 0.5); }
.dot-led.active { background: var(--gold); box-shadow: 0 0 6px rgba(230, 185, 74, 0.5); }
.dot-led.warn { background: var(--gold); }
.dot-led.idle { background: var(--fg-4); }

.empty-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  font-size: 13px;
  color: var(--fg-3);
}

.accel-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.accel-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  font-family: var(--font-mono);
  padding: 3px 8px;
  border-radius: 100px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
  color: var(--fg-1);
}
.accel-chip.off { color: var(--fg-3); opacity: 0.6; }
.accel-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
}
.accel-dot.good { background: var(--good); }
.accel-dot.idle { background: var(--fg-4); }

.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 10px;
}
.stat-card {
  display: flex;
  align-items: center;
  gap: 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 16px 18px;
  transition: border-color 0.15s ease;
}
.stat-card:hover { border-color: var(--border-strong); }
.stat-icon {
  width: 40px;
  height: 40px;
  border-radius: var(--r-md);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.stat-body { min-width: 0; }
.stat-value { font-size: 22px; font-weight: 700; line-height: 1; }
.stat-label {
  font-size: 10px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin-top: 4px;
}

.health-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 8px;
}
.health-card {
  display: flex;
  align-items: center;
  gap: 10px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 10px 14px;
}
.health-indicator {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.health-indicator.good { background: var(--good); box-shadow: 0 0 6px rgba(111, 191, 124, 0.4); }
.health-indicator.bad { background: var(--bad); box-shadow: 0 0 6px rgba(217, 107, 107, 0.4); }
.health-indicator.active { background: var(--gold); box-shadow: 0 0 6px rgba(230, 185, 74, 0.4); }
.health-indicator.idle { background: var(--fg-4); }
.health-info { flex: 1; display: flex; align-items: center; justify-content: space-between; min-width: 0; }
.health-label { font-size: 13px; font-weight: 500; color: var(--fg-1); }
.health-status { font-size: 11px; color: var(--fg-3); font-family: var(--font-mono); }

.queue-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: 8px;
  margin-bottom: 10px;
}
.queue-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 12px 14px;
}
.queue-card-label {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-3);
}
.queue-card-value {
  font-size: 22px;
  font-weight: 700;
  margin-top: 4px;
  font-variant-numeric: tabular-nums;
}
.queue-card-sub { font-size: 11px; color: var(--fg-3); font-family: var(--font-mono); margin-top: 2px; }

.running-card {
  display: flex;
  align-items: center;
  gap: 14px;
  background: rgba(230, 185, 74, 0.06);
  border: 1px solid rgba(230, 185, 74, 0.25);
  border-radius: var(--r-md);
  padding: 12px 16px;
  margin-bottom: 10px;
}
.running-pulse {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--gold);
  box-shadow: 0 0 0 0 rgba(230, 185, 74, 0.7);
  animation: pulse 1.6s infinite;
  flex-shrink: 0;
}
@keyframes pulse {
  0% { box-shadow: 0 0 0 0 rgba(230, 185, 74, 0.6); }
  70% { box-shadow: 0 0 0 12px rgba(230, 185, 74, 0); }
  100% { box-shadow: 0 0 0 0 rgba(230, 185, 74, 0); }
}
.running-info { flex: 1; min-width: 0; }
.running-label { font-size: 10px; font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.08em; color: var(--fg-3); }
.running-title { font-size: 14px; font-weight: 500; margin-top: 4px; color: var(--fg-0); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.running-type { color: var(--fg-3); font-weight: 400; margin-left: 4px; }
.running-elapsed { font-family: var(--font-mono); font-size: 13px; color: var(--gold); font-variant-numeric: tabular-nums; }

.job-summary-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.job-summary-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  font-family: var(--font-mono);
  padding: 4px 10px;
  border-radius: 100px;
  background: var(--bg-2);
  border: 1px solid var(--border);
}
.pill-value { font-weight: 700; color: var(--fg-0); }
.pill-label { color: var(--fg-3); text-transform: uppercase; letter-spacing: 0.06em; }
.pill-available { border-color: rgba(230, 185, 74, 0.3); }
.pill-retryable { border-color: rgba(217, 107, 107, 0.3); }

.task-list { display: flex; flex-direction: column; gap: 2px; background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md); overflow: hidden; }
.task-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  transition: background 0.12s;
}
.task-row + .task-row { border-top: 1px solid var(--border); }
.task-row.disabled { opacity: 0.5; }
.task-row.busy { background: rgba(230, 185, 74, 0.04); }
.task-led { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.task-led.good { background: var(--good); }
.task-led.bad { background: var(--bad); }
.task-led.warn { background: var(--gold); }
.task-led.active { background: var(--gold); box-shadow: 0 0 6px rgba(230, 185, 74, 0.5); }
.task-led.idle { background: var(--fg-4); }
.task-main { flex: 1; min-width: 0; }
.task-name { font-size: 13px; font-weight: 500; color: var(--fg-0); }
.task-meta { font-size: 11px; color: var(--fg-3); font-family: var(--font-mono); margin-top: 2px; display: flex; gap: 4px; flex-wrap: wrap; }
.task-pending { color: var(--gold); }
.task-failed { color: var(--bad); }
.task-run-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  font-family: var(--font-mono);
  padding: 4px 10px;
  border-radius: var(--r-sm);
  background: rgba(255,255,255,0.06);
  border: 1px solid var(--border);
  color: var(--fg-1);
  cursor: pointer;
}
.task-run-btn:hover:not(:disabled) { background: rgba(255,255,255,0.12); color: var(--fg-0); }
.task-run-btn:disabled { opacity: 0.4; cursor: default; }

.missing-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.missing-summary {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--bad);
  font-weight: 500;
}
.missing-scroll {
  display: flex;
  gap: 10px;
  overflow-x: auto;
  overflow-y: hidden;
  padding-bottom: 4px;
  scrollbar-width: none;
}
.missing-scroll::-webkit-scrollbar { display: none; }
.missing-tile { width: 120px; flex-shrink: 0; opacity: 0.7; }
.missing-poster { position: relative; border-radius: var(--r-md); overflow: hidden; aspect-ratio: 2/3; background: var(--bg-3); }
.missing-poster img { width: 100%; height: 100%; object-fit: cover; filter: grayscale(0.6); }
.missing-poster-empty { width: 100%; height: 100%; display: flex; align-items: center; justify-content: center; color: var(--fg-3); }
.missing-badge {
  position: absolute;
  top: 6px;
  right: 6px;
  font-size: 8px;
  font-weight: 700;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  padding: 2px 6px;
  border-radius: 100px;
  background: rgba(217, 107, 107, 0.85);
  color: #fff;
}
.missing-meta { margin-top: 6px; }
.missing-tile-title {
  font-size: 11px;
  font-weight: 500;
  color: var(--fg-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.missing-tile-sub {
  font-size: 10px;
  color: var(--fg-3);
  font-family: var(--font-mono);
}
</style>
