<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import AdminNowPlaying from './activity.vue'
import {
  dashboardStatsQuery,
  jobSummaryQuery,
  metadataQueueQuery,
  serverHealthQuery,
  serverReadinessQuery,
} from '~/queries/admin'

const { on } = useEventBus()
const { sessions } = useActiveSessions()

const statsData = useQuery(dashboardStatsQuery())
const healthData = useQuery(serverHealthQuery())
const readinessData = useQuery(serverReadinessQuery())
const queueData = useQuery(metadataQueueQuery())
const summaryData = useQuery(jobSummaryQuery())

const stats = computed(() => statsData.data.value ?? null)
const health = computed(() => healthData.data.value ?? null)
const ready = computed(() => readinessData.data.value ?? null)
const queueStatus = computed(() => queueData.data.value ?? null)

const activeStreams = computed(() => sessions.value.filter(session => !session.paused).length)
const transcodingStreams = computed(() => sessions.value.filter(session => session.playback_action === 'transcode').length)
const directStreams = computed(() => sessions.value.filter(session => session.playback_action === 'direct_play' || session.playback_action === 'remux').length)
const viewerCount = computed(() => new Set(sessions.value.map(session => session.user_id)).size)

function summaryCount(state: string) {
  return (summaryData.data.value ?? [])
    .filter(row => row.state === state)
    .reduce((total, row) => total + row.count, 0)
}

const totalQueued = computed(() =>
  summaryCount('running')
  + summaryCount('available')
  + summaryCount('retryable')
  + summaryCount('scheduled'),
)
const discardedJobs = computed(() => summaryCount('discarded'))
const healthyComponents = computed(() => ready.value?.components?.filter(component => component.ok).length ?? 0)
const unhealthyComponents = computed(() => (ready.value?.components?.length ?? 0) - healthyComponents.value)

const subsystemTone = computed<'good' | 'warn' | 'bad'>(() => {
  if (!ready.value) return 'warn'
  return ready.value.status === 'ok' ? 'good' : 'bad'
})
const healthLabel = computed(() => {
  if (!ready.value) return 'Checking'
  return ready.value.status === 'ok' ? 'Healthy' : 'Degraded'
})

const now = ref(Date.now())
let nowTimer: ReturnType<typeof setInterval> | null = null
let livePoll: ReturnType<typeof setInterval> | null = null
let statsDebounce: ReturnType<typeof setTimeout> | null = null

const runningElapsed = computed(() => {
  const running = queueStatus.value?.running
  if (!running?.started_at) return ''
  const elapsed = now.value - new Date(running.started_at).getTime()
  if (elapsed < 0 || Number.isNaN(elapsed)) return ''
  const seconds = Math.floor(elapsed / 1000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
})

function fmtNumber(value?: number) {
  return value == null ? '—' : value.toLocaleString()
}

async function refreshLiveOverview() {
  await Promise.allSettled([
    queueData.refetch(),
    summaryData.refetch(),
    readinessData.refetch(),
  ])
}

function debouncedRefetchStats() {
  if (statsDebounce) clearTimeout(statsDebounce)
  statsDebounce = setTimeout(() => { void statsData.refetch() }, 1500)
}

const unsubs = [
  on('media.added', debouncedRefetchStats),
  on('media.removed', debouncedRefetchStats),
  on('scan.completed', debouncedRefetchStats),
]

// A hidden tab has nobody reading the dashboard — skip the ticks instead of
// polling into the void, then catch up once on return so nothing looks stale.
function onVisibilityReturn() {
  if (document.visibilityState !== 'visible') return
  now.value = Date.now()
  void refreshLiveOverview()
}

onMounted(() => {
  nowTimer = setInterval(() => {
    if (document.hidden) return
    now.value = Date.now()
  }, 1000)
  livePoll = setInterval(() => {
    if (document.hidden) return
    void refreshLiveOverview()
  }, 5000)
  document.addEventListener('visibilitychange', onVisibilityReturn)
})

onUnmounted(() => {
  unsubs.forEach(unsubscribe => unsubscribe())
  if (nowTimer) clearInterval(nowTimer)
  if (livePoll) clearInterval(livePoll)
  if (statsDebounce) clearTimeout(statsDebounce)
  document.removeEventListener('visibilitychange', onVisibilityReturn)
})
</script>

<template>
  <div>
    <header class="sv2-page-head dashboard-head">
      <div>
        <h2 class="sv2-page-title">Dashboard</h2>
        <p class="sv2-page-desc">
          What is happening right now—streams, system readiness, your library, and the work queue.
        </p>
      </div>
      <StatusBadge :state="subsystemTone === 'good' ? 'ok' : subsystemTone === 'warn' ? 'warn' : 'error'">
        {{ subsystemTone === 'good' ? 'All systems operational' : subsystemTone === 'warn' ? 'Checking systems' : 'Needs attention' }}
      </StatusBadge>
    </header>

    <div class="dashboard-overview-grid">
      <DashboardSummaryCard
        title="Streams"
        icon="cast"
        :value="sessions.length"
        value-label="total"
        :tone="sessions.length ? 'good' : 'neutral'"
      >
        <div class="summary-row"><span>Active</span><strong>{{ activeStreams }}</strong></div>
        <div class="summary-row"><span>Transcoding</span><strong :class="{ warn: transcodingStreams }">{{ transcodingStreams }}</strong></div>
        <div class="summary-row"><span>Direct / remux</span><strong>{{ directStreams }}</strong></div>
        <div class="summary-row"><span>Viewers</span><strong>{{ viewerCount }}</strong></div>
      </DashboardSummaryCard>

      <DashboardSummaryCard
        title="Library"
        icon="folder"
        :value="fmtNumber(stats?.total_media)"
        value-label="items"
        :alert="(stats?.missing_count ?? 0) > 0 ? stats?.missing_count : ''"
        :alert-label="(stats?.missing_count ?? 0) > 0 ? 'missing' : ''"
        :tone="(stats?.missing_count ?? 0) > 0 ? 'warn' : 'neutral'"
      >
        <div class="summary-row"><span>Movies</span><strong>{{ fmtNumber(stats?.media_counts?.movie ?? 0) }}</strong></div>
        <div class="summary-row"><span>TV shows</span><strong>{{ fmtNumber(stats?.media_counts?.tv ?? 0) }}</strong></div>
        <div class="summary-row"><span>Artists / music</span><strong>{{ fmtNumber(stats?.media_counts?.music ?? 0) }}</strong></div>
        <div class="summary-row"><span>Books</span><strong>{{ fmtNumber(stats?.media_counts?.book ?? 0) }}</strong></div>
      </DashboardSummaryCard>

      <DashboardSummaryCard
        title="Queue"
        icon="layers"
        :value="totalQueued"
        value-label="queued"
        :alert="discardedJobs > 0 ? discardedJobs : ''"
        :alert-label="discardedJobs > 0 ? 'discarded' : ''"
        :tone="discardedJobs > 0 ? 'bad' : totalQueued > 0 ? 'warn' : 'good'"
      >
        <div class="summary-row"><span>Metadata pending</span><strong>{{ queueStatus?.pending ?? 0 }}</strong></div>
        <div class="summary-row"><span>Running</span><strong :class="{ good: summaryCount('running') }">{{ summaryCount('running') }}</strong></div>
        <div class="summary-row"><span>Ready</span><strong>{{ summaryCount('available') }}</strong></div>
        <div class="summary-row"><span>Retryable</span><strong :class="{ warn: summaryCount('retryable') }">{{ summaryCount('retryable') }}</strong></div>
        <div class="summary-row"><span>Completed · 5 min</span><strong>{{ queueStatus?.recent.completed_5min ?? 0 }}</strong></div>
        <template v-if="queueStatus?.running" #footer>
          <div class="queue-current">
            <span class="queue-pulse" />
            <div class="queue-current-text">
              <strong>{{ queueStatus.running.item_title || `Job #${queueStatus.running.job_id}` }}</strong>
              <span>priority {{ queueStatus.running.priority }} · {{ runningElapsed }}</span>
            </div>
          </div>
        </template>
      </DashboardSummaryCard>

      <DashboardSummaryCard
        title="Health"
        icon="pulse"
        :value="healthLabel"
        :alert="unhealthyComponents > 0 ? unhealthyComponents : ''"
        :alert-label="unhealthyComponents > 0 ? 'unhealthy' : ''"
        :tone="subsystemTone"
      >
        <div class="summary-row"><span>Server</span><strong :class="subsystemTone">{{ ready?.status ?? health?.status ?? 'checking' }}</strong></div>
        <div class="summary-row"><span>Database</span><strong>{{ health?.database ?? '—' }}</strong></div>
        <div class="summary-row"><span>Components</span><strong>{{ healthyComponents }} / {{ ready?.components?.length ?? 0 }}</strong></div>
        <div class="summary-row"><span>Version</span><strong>{{ health?.version ?? '—' }}</strong></div>
      </DashboardSummaryCard>
    </div>

    <AdminNowPlaying embedded :show-summary="false" />
  </div>
</template>

<style scoped>
.dashboard-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
}

.dashboard-overview-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 22px;
}

.queue-current {
  display: flex;
  align-items: center;
  gap: 9px;
  min-width: 0;
}
.queue-pulse {
  width: 8px;
  height: 8px;
  flex-shrink: 0;
  border-radius: 50%;
  background: var(--gold);
  animation: queue-pulse 1.6s infinite;
}
@keyframes queue-pulse {
  0% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--gold) 60%, transparent); }
  70% { box-shadow: 0 0 0 9px transparent; }
  100% { box-shadow: 0 0 0 0 transparent; }
}
.queue-current-text {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.queue-current-text strong {
  overflow: hidden;
  color: var(--fg-1);
  font-size: 11px;
  font-weight: 580;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.queue-current-text span { color: var(--fg-3); font-family: var(--font-mono); font-size: 9.5px; }

@media (max-width: 1180px) {
  .dashboard-overview-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 720px) {
  .dashboard-head { flex-direction: column; }
}

@media (max-width: 620px) {
  .dashboard-overview-grid { grid-template-columns: 1fr; }
}
</style>
