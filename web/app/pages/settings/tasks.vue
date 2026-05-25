<template>
  <div>
    <div class="page-header">
      <div>
        <h2 class="page-title">Scheduled Tasks</h2>
        <p class="page-desc">Configure automated tasks and recurring schedules</p>
      </div>
    </div>

    <!-- Metadata Queue panel — live view of the unified enrich queue -->
    <section v-if="queueStatus" class="section">
      <h3 class="section-heading">
        <Icon name="refresh" :size="14" />
        Metadata queue
        <span class="queue-subtle">unified enrich · MaxWorkers=1</span>
      </h3>

      <div class="queue-panel">
        <div class="queue-stats">
          <div class="queue-stat">
            <span class="queue-stat-num">{{ queueStatus.pending }}</span>
            <span class="queue-stat-label">pending</span>
          </div>
          <div class="queue-priority-bands">
            <div class="queue-band" :class="{ active: (queueStatus.pending_by_priority['1'] ?? 0) > 0 }">
              <span class="band-label">P1 · watcher / view</span>
              <span class="band-count">{{ queueStatus.pending_by_priority['1'] ?? 0 }}</span>
            </div>
            <div class="queue-band" :class="{ active: (queueStatus.pending_by_priority['2'] ?? 0) > 0 }">
              <span class="band-label">P2 · movies + tv</span>
              <span class="band-count">{{ queueStatus.pending_by_priority['2'] ?? 0 }}</span>
            </div>
            <div class="queue-band" :class="{ active: (queueStatus.pending_by_priority['3'] ?? 0) > 0 }">
              <span class="band-label">P3 · music + books</span>
              <span class="band-count">{{ queueStatus.pending_by_priority['3'] ?? 0 }}</span>
            </div>
          </div>
        </div>

        <div v-if="queueStatus.running" class="queue-current">
          <div class="queue-current-spinner">
            <Icon name="loader" :size="14" />
          </div>
          <div class="queue-current-info">
            <div class="queue-current-title">{{ queueStatus.running.item_title || queueStatus.running.kind }}</div>
            <div class="queue-current-meta">
              <span v-if="queueStatus.running.media_type">{{ queueStatus.running.media_type }}</span>
              <span v-if="queueStatus.running.source">· {{ queueStatus.running.source }}</span>
              <span>· P{{ queueStatus.running.priority }}</span>
              <span v-if="queueStatus.running.started_at">· {{ timeAgo(queueStatus.running.started_at) }}</span>
            </div>
          </div>
        </div>
        <div v-else class="queue-idle">
          <Icon name="check" :size="14" />
          Queue idle
        </div>

        <div class="queue-throughput">
          <span class="throughput-num">{{ queueStatus.recent.completed_5min }}</span>
          <span class="throughput-label">completed · last 5 min</span>
          <span v-if="queueStatus.recent.avg_duration_sec > 0" class="throughput-avg">
            avg {{ queueStatus.recent.avg_duration_sec.toFixed(1) }}s
          </span>
        </div>
      </div>
    </section>

    <!-- Scheduled Tasks -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="timer" :size="14" />
        Tasks
      </h3>

      <div v-if="tasks.length" class="task-list">
        <div v-for="t in tasks" :key="t.id" class="task-card">
          <div class="task-header">
            <div class="task-icon" :class="t.state === 'running' ? 'running' : 'idle'">
              <Icon :name="taskIcon(t.id)" :size="16" />
            </div>
            <div class="task-info">
              <div class="task-name">
                {{ t.display_name }}
                <span v-if="t.state === 'running'" class="state-badge running">Running</span>
                <span v-else-if="t.enabled" class="state-badge scheduled">Scheduled</span>
                <span v-else class="state-badge disabled">Disabled</span>
              </div>
              <div class="task-desc">{{ t.description }}</div>
            </div>
            <div class="task-actions">
              <button
                class="btn btn-secondary btn-sm"
                @click="itemsModalTask = t.id"
              >
                <Icon name="list" :size="12" />
                View items
              </button>
              <button
                class="btn btn-secondary btn-sm"
                :disabled="false"
                @click="t.state === 'running' ? cancelTask(t.id) : runTask(t.id)"
              >
                <Icon :name="t.state === 'running' ? 'close' : 'play'" :size="12" />
                {{ t.state === 'running' ? 'Cancel' : 'Run Now' }}
              </button>
            </div>
          </div>

          <!-- Stats -->
          <div v-if="t.stats && t.stats.total > 0" class="task-stats">
            <div class="stats-bar-track">
              <!-- Stacked: complete (green) → failed (red) → pending (rest, gold) -->
              <div class="stats-bar-fill stats-bar-complete" :style="{ width: (t.stats.complete / t.stats.total * 100) + '%' }" />
              <div v-if="(t.stats.failed ?? 0) > 0" class="stats-bar-fill stats-bar-failed" :style="{ width: ((t.stats.failed ?? 0) / t.stats.total * 100) + '%' }" />
            </div>
            <div class="stats-label">
              <span class="stats-complete">{{ t.stats.complete }}</span>
              <span class="stats-text">complete</span>
              <template v-if="(t.stats.failed ?? 0) > 0">
                <span class="stats-sep">·</span>
                <span class="stats-failed">{{ t.stats.failed }}</span>
                <span class="stats-text">failed</span>
              </template>
              <template v-if="t.stats.pending > 0">
                <span class="stats-sep">·</span>
                <span class="stats-pending">{{ t.stats.pending }}</span>
                <span class="stats-text">pending</span>
              </template>
              <span class="stats-sep">/</span>
              <span class="stats-total">{{ t.stats.total }}</span>
            </div>
          </div>
          <div v-else-if="t.stats && t.stats.total === 0" class="task-stats">
            <div class="stats-label"><span class="stats-text">No eligible items found</span></div>
          </div>

          <!-- Progress bar -->
          <div v-if="t.state === 'running' && t.progress" class="task-progress-section">
            <div class="progress-bar-track">
              <div
                class="progress-bar-fill"
                :class="{ indeterminate: t.id === 'scan_libraries' }"
                :style="{ width: t.id === 'scan_libraries' ? '100%' : progressPct(t.progress) + '%' }"
              />
            </div>
            <div class="progress-stats">
              <span v-if="t.progress.current_item" class="progress-current">{{ t.progress.current_item }}</span>
              <span class="progress-count">
                {{ t.id === 'scan_libraries' ? `${t.progress.completed} files discovered` : `${t.progress.completed} / ${t.progress.total}` }}
              </span>
              <span v-if="t.id !== 'scan_libraries' && t.progress.total > 0" class="progress-pct">{{ progressPct(t.progress) }}%</span>
            </div>
          </div>

          <!-- Last run + schedule config -->
          <div class="task-details">
            <div v-if="t.last_run_at" class="task-last-run">
              <span class="detail-label">Last run</span>
              <span class="detail-val">
                {{ timeAgo(t.last_run_at) }}
                <span class="result-badge" :class="t.last_run_result">{{ t.last_run_result || 'never' }}</span>
                <span v-if="t.last_run_items_total > 0" class="detail-sub">
                  {{ t.last_run_items_processed }}/{{ t.last_run_items_total }} items · {{ formatDuration(t.last_run_duration_sec) }}
                  <template v-if="t.last_run_result === 'partial'"> · some failed</template>
                </span>
              </span>
            </div>
            <div v-if="t.next_run_at && t.enabled" class="task-next-run">
              <span class="detail-label">Next run</span>
              <span class="detail-val">{{ formatDate(t.next_run_at) }}</span>
            </div>

            <div class="task-schedule">
              <div class="schedule-row">
                <label class="toggle-row">
                  <span class="toggle-label">Enable schedule</span>
                  <button class="toggle-switch" :class="{ on: t.enabled }" @click="toggleEnabled(t)">
                    <span class="toggle-knob" />
                  </button>
                </label>
              </div>
              <div v-if="t.enabled" class="schedule-config">
                <div class="config-field">
                  <label class="field-label">Time window</label>
                  <div class="time-inputs">
                    <input
                      type="time"
                      :value="t.daily_start_time"
                      @change="updateField(t, 'daily_start_time', ($event.target as HTMLInputElement).value)"
                      class="time-input"
                    />
                    <span class="time-sep">to</span>
                    <input
                      type="time"
                      :value="t.daily_end_time"
                      @change="updateField(t, 'daily_end_time', ($event.target as HTMLInputElement).value)"
                      class="time-input"
                    />
                  </div>
                </div>
                <div class="config-field">
                  <label class="field-label">Max runtime</label>
                  <div class="runtime-select">
                    <select
                      :value="t.max_runtime_minutes"
                      @change="updateField(t, 'max_runtime_minutes', Number(($event.target as HTMLSelectElement).value))"
                      class="select-input"
                    >
                      <option :value="30">30 min</option>
                      <option :value="60">1 hour</option>
                      <option :value="120">2 hours</option>
                      <option :value="240">4 hours</option>
                      <option :value="480">8 hours</option>
                    </select>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
      <div v-else class="empty-hint">
        <Icon name="info" :size="14" />
        Loading scheduled tasks...
      </div>
    </section>

    <TaskItemsModal
      v-if="itemsModalTask"
      :task-id="itemsModalTask"
      :task-name="tasks.find(t => t.id === itemsModalTask)?.display_name ?? ''"
      @close="itemsModalTask = null"
    />
  </div>
</template>

<script setup lang="ts">
import type { TaskProgressPayload } from '~/composables/useEventBus'

interface TaskStatsPayload {
  complete: number
  pending: number
  failed?: number
  total: number
}

interface ScheduledTask {
  id: string
  display_name: string
  description: string
  category: string
  enabled: boolean
  interval_hours: number
  daily_start_time: string
  daily_end_time: string
  max_runtime_minutes: number
  last_run_at: string | null
  last_run_result: string
  last_run_duration_sec: number
  last_run_items_processed: number
  last_run_items_total: number
  next_run_at: string | null
  state: string
  progress: TaskProgressPayload | null
  stats: TaskStatsPayload | null
}

interface MetadataQueueRunning {
  job_id: number
  kind: string
  priority: number
  item_id?: number
  item_title?: string
  media_type?: string
  source?: string
  started_at?: string
}

interface MetadataQueueStatus {
  pending: number
  pending_by_priority: Record<string, number>
  running?: MetadataQueueRunning
  recent: { completed_5min: number, avg_duration_sec: number }
}

const tasks = ref<ScheduledTask[]>([])
const itemsModalTask = ref<string | null>(null)
const queueStatus = ref<MetadataQueueStatus | null>(null)
let queuePoll: ReturnType<typeof setInterval> | null = null

function taskIcon(id: string): string {
  if (id === 'generate_trickplay') return 'film'
  if (id === 'generate_thumbnails') return 'image'
  if (id === 'scan_libraries') return 'folder'
  if (id === 'refresh_stale_items') return 'refresh'
  return 'timer'
}

function progressPct(p: TaskProgressPayload): number {
  if (!p || p.total === 0) return 0
  return Math.round((p.completed / p.total) * 100)
}

function timeAgo(dateStr: string | null) {
  if (!dateStr) return 'never'
  const sec = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`
  return `${Math.floor(sec / 86400)}d ago`
}

function formatDate(d: string | null) {
  if (!d) return ''
  return new Date(d).toLocaleString('en-GB', { dateStyle: 'medium', timeStyle: 'short' })
}

function formatDuration(sec: number): string {
  if (sec < 60) return `${sec}s`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ${sec % 60}s`
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return `${h}h ${m}m`
}

async function fetchTasks() {
  try {
    const { $heya } = useNuxtApp()
    tasks.value = await $heya('/api/tasks') as ScheduledTask[]
  } catch {}
}

async function fetchQueueStatus() {
  try {
    const { $heya } = useNuxtApp()
    queueStatus.value = await $heya('/api/jobs/queue/metadata') as MetadataQueueStatus
  } catch {}
}

async function runTask(id: string) {
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/tasks/{id}/run', { method: 'POST', path: { id: id as any } })
    fetchTasks()
  } catch {}
}

async function cancelTask(id: string) {
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/tasks/{id}/cancel', { method: 'POST', path: { id: id as any } })
    fetchTasks()
  } catch {}
}

async function toggleEnabled(t: ScheduledTask) {
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/tasks/{id}', {
      method: 'PUT',
      path: { id: t.id as any },
      body: {
        enabled: !t.enabled,
        interval_hours: t.interval_hours,
        daily_start_time: t.daily_start_time,
        daily_end_time: t.daily_end_time,
        max_runtime_minutes: t.max_runtime_minutes,
      } as any,
    })
    fetchTasks()
  } catch {}
}

async function updateField(t: ScheduledTask, field: string, value: any) {
  const body: any = {
    enabled: t.enabled,
    interval_hours: t.interval_hours,
    daily_start_time: t.daily_start_time,
    daily_end_time: t.daily_end_time,
    max_runtime_minutes: t.max_runtime_minutes,
  }
  body[field] = value
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/tasks/{id}', { method: 'PUT', path: { id: t.id as any }, body: body as any })
    fetchTasks()
  } catch {}
}

const { taskProgress: liveTaskProgress } = useEventBus()

watch(liveTaskProgress, () => {
  for (const t of tasks.value) {
    const live = liveTaskProgress.value[t.id]
    if (live) {
      t.state = live.state
      t.progress = live
    } else if (t.state === 'running') {
      t.state = 'idle'
      t.progress = null
      fetchTasks()
    }
  }
}, { deep: true })

onMounted(() => {
  fetchTasks()
  fetchQueueStatus()
  queuePoll = setInterval(fetchQueueStatus, 2000)
})

onBeforeUnmount(() => {
  if (queuePoll) clearInterval(queuePoll)
})
</script>

<style scoped>
.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 24px; }
.page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.page-desc { font-size: 13px; color: var(--fg-3); margin: 6px 0 0; }

.section { margin-bottom: 36px; }
.section-heading {
  display: flex; align-items: center; gap: 8px;
  font-size: 11px; font-weight: 600; color: var(--fg-3);
  font-family: var(--font-mono); text-transform: uppercase;
  letter-spacing: 0.1em; margin: 0 0 14px; padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}
.section-desc { font-size: 12px; color: var(--fg-3); margin: -8px 0 14px; }

/* Scheduled Tasks */
.task-list { display: flex; flex-direction: column; gap: 12px; }
.task-card {
  background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md);
  padding: 18px 20px;
}
.task-header { display: flex; align-items: flex-start; gap: 14px; }
.task-icon {
  width: 38px; height: 38px; border-radius: var(--r-sm);
  display: flex; align-items: center; justify-content: center; flex-shrink: 0;
}
.task-icon.idle { background: var(--bg-3); color: var(--fg-3); }
.task-icon.running { background: rgba(100, 200, 140, 0.12); color: var(--good); }
.task-info { flex: 1; min-width: 0; }
.task-name { font-size: 14px; font-weight: 500; display: flex; align-items: center; gap: 8px; }
.task-desc { font-size: 12px; color: var(--fg-3); margin-top: 2px; }
.task-actions { flex-shrink: 0; }
.btn-sm { height: 34px; padding: 0 14px; font-size: 12px; }

.state-badge {
  font-size: 10px; font-weight: 600; font-family: var(--font-mono);
  padding: 2px 8px; border-radius: 100px; text-transform: uppercase; letter-spacing: 0.04em;
}
.state-badge.running { background: rgba(100, 200, 140, 0.12); color: var(--good); }
.state-badge.scheduled { background: var(--gold-soft); color: var(--gold); }
.state-badge.disabled { background: var(--bg-3); color: var(--fg-4); }

/* Stats bar */
.task-stats { margin-top: 12px; }
.stats-bar-track {
  height: 4px; border-radius: 2px; background: var(--bg-0); overflow: hidden;
  display: flex;
}
.stats-bar-fill { height: 100%; transition: width 0.3s ease; }
.stats-bar-complete { background: var(--good); }
.stats-bar-failed { background: var(--bad, #d6594a); }
.stats-pending { font-weight: 600; color: var(--gold); }
.stats-failed { font-weight: 600; color: var(--bad, #d6594a); }
.stats-label {
  display: flex; align-items: center; gap: 3px; margin-top: 5px;
  font-size: 11px; font-family: var(--font-mono); color: var(--fg-3);
}
.stats-complete { font-weight: 600; color: var(--good); }
.stats-sep { color: var(--fg-4); }
.stats-total { font-weight: 500; color: var(--fg-2); }
.stats-text { margin-left: 2px; }

/* Progress */
.task-progress-section { margin-top: 14px; }
.progress-bar-track { height: 6px; border-radius: 3px; background: var(--bg-0); overflow: hidden; }
.progress-bar-fill { height: 100%; border-radius: 3px; background: var(--gold); transition: width 0.3s ease; }
.progress-bar-fill.indeterminate {
  background: linear-gradient(90deg, transparent 0%, var(--gold) 50%, transparent 100%);
  background-size: 200% 100%;
  animation: indeterminate 1.5s ease-in-out infinite;
}
@keyframes indeterminate { 0% { background-position: -100% 0; } 100% { background-position: 200% 0; } }
.progress-stats {
  display: flex; align-items: center; gap: 12px; margin-top: 6px; font-size: 11px;
  font-family: var(--font-mono); color: var(--fg-3);
}
.progress-count { font-weight: 600; color: var(--fg-2); }
.progress-current { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--fg-3); }
.progress-pct { font-weight: 700; color: var(--gold); }

/* Task details */
.task-details { margin-top: 14px; padding-top: 14px; border-top: 1px solid var(--border); }
.task-last-run, .task-next-run {
  display: flex; align-items: center; gap: 8px; font-size: 12px; margin-bottom: 6px;
}
.detail-label {
  font-size: 10px; font-weight: 600; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em; color: var(--fg-4); width: 70px; flex-shrink: 0;
}
.detail-val { color: var(--fg-2); display: flex; align-items: center; gap: 6px; }
.detail-sub { color: var(--fg-3); font-family: var(--font-mono); font-size: 11px; }

.result-badge {
  font-size: 9px; font-weight: 600; font-family: var(--font-mono);
  padding: 1px 6px; border-radius: 100px; text-transform: uppercase;
}
.result-badge.completed { background: rgba(100, 200, 140, 0.12); color: var(--good); }
.result-badge.partial { background: var(--gold-soft); color: var(--gold); }
.result-badge.stopped { background: rgba(140, 160, 255, 0.1); color: rgb(140, 160, 255); }
.result-badge.error { background: rgba(217, 107, 107, 0.1); color: var(--bad); }

/* Schedule config */
.task-schedule { margin-top: 10px; }
.toggle-row {
  display: flex; align-items: center; justify-content: space-between; cursor: pointer; user-select: none;
}
.toggle-label { font-size: 12px; color: var(--fg-2); }
.toggle-switch {
  width: 36px; height: 20px; border-radius: 10px;
  background: var(--bg-3); border: 1px solid var(--border);
  position: relative; transition: all 0.15s ease; cursor: pointer;
}
.toggle-switch.on { background: var(--gold); border-color: var(--gold); }
.toggle-knob {
  width: 16px; height: 16px; border-radius: 50%;
  background: white; position: absolute; top: 1px; left: 1px;
  transition: transform 0.15s ease; box-shadow: 0 1px 2px rgba(0,0,0,0.2);
}
.toggle-switch.on .toggle-knob { transform: translateX(16px); }

.schedule-config { display: flex; gap: 20px; margin-top: 10px; flex-wrap: wrap; }
.config-field { display: flex; flex-direction: column; gap: 4px; }
.field-label {
  font-size: 10px; font-weight: 600; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em; color: var(--fg-4);
}
.time-inputs { display: flex; align-items: center; gap: 6px; }
.time-sep { font-size: 11px; color: var(--fg-3); }
.time-input, .select-input {
  background: var(--bg-0); border: 1px solid var(--border); border-radius: var(--r-xs);
  padding: 4px 8px; font-size: 12px; font-family: var(--font-mono); color: var(--fg-1); outline: none;
}
.time-input:focus, .select-input:focus { border-color: var(--gold); }
.select-input { cursor: pointer; }

.empty-hint {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 13px;
  padding: 14px 16px; background: var(--bg-2);
  border: 1px dashed var(--border); border-radius: var(--r-md);
}

/* Metadata queue panel */
.queue-subtle {
  font-size: 11px; font-weight: 400; color: var(--fg-4);
  font-family: var(--font-mono); margin-left: 8px;
}
.queue-panel {
  display: grid;
  grid-template-columns: 1fr 1.4fr auto;
  gap: 18px;
  align-items: center;
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 14px 18px;
}
.queue-stats { display: flex; align-items: center; gap: 18px; }
.queue-stat { display: flex; flex-direction: column; align-items: flex-start; }
.queue-stat-num {
  font-size: 28px; font-weight: 600; line-height: 1;
  font-family: var(--font-mono); color: var(--fg-1);
}
.queue-stat-label {
  font-size: 10px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em; color: var(--fg-4);
  margin-top: 4px;
}
.queue-priority-bands { display: flex; flex-direction: column; gap: 4px; min-width: 140px; }
.queue-band {
  display: flex; align-items: center; justify-content: space-between;
  font-family: var(--font-mono); font-size: 11px;
  color: var(--fg-3);
  padding: 2px 6px; border-radius: var(--r-xs);
  background: var(--bg-2);
}
.queue-band.active { color: var(--fg-1); background: var(--bg-3); }
.band-label { letter-spacing: 0.02em; }
.band-count { font-weight: 600; }

.queue-current {
  display: flex; align-items: center; gap: 10px;
  padding: 8px 12px; background: var(--bg-2);
  border: 1px solid var(--border); border-radius: var(--r-sm);
}
.queue-current-spinner {
  width: 24px; height: 24px; display: flex; align-items: center; justify-content: center;
  color: var(--gold);
  animation: queue-spin 1.2s linear infinite;
}
@keyframes queue-spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
.queue-current-info { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.queue-current-title {
  font-size: 13px; color: var(--fg-1); font-weight: 500;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.queue-current-meta {
  font-size: 11px; font-family: var(--font-mono); color: var(--fg-4);
  display: flex; gap: 4px;
}

.queue-idle {
  display: flex; align-items: center; gap: 8px;
  font-size: 12px; color: var(--fg-3);
  padding: 8px 12px;
}

.queue-throughput {
  display: flex; flex-direction: column; align-items: flex-end; gap: 2px;
  min-width: 110px;
}
.throughput-num {
  font-size: 18px; font-weight: 600; line-height: 1;
  font-family: var(--font-mono); color: var(--fg-1);
}
.throughput-label {
  font-size: 10px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em; color: var(--fg-4);
}
.throughput-avg {
  font-size: 11px; font-family: var(--font-mono); color: var(--fg-3);
  margin-top: 2px;
}
</style>
