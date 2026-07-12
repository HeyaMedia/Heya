<script setup lang="ts">
import { timeAgo as timeAgoBase } from '~/composables/useFormat'
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { adminTasksQuery, metadataQueueQuery } from '~/queries/admin'
import type { TaskResponse } from '~/queries/admin'

const { $heya } = useNuxtApp()
const { taskProgress: liveTaskProgress } = useEventBus()

const tasksData = useQuery(adminTasksQuery())
const queueData = useQuery(metadataQueueQuery())
const tasks = computed(() => tasksData.data.value ?? [])
const queueStatus = computed(() => queueData.data.value ?? null)
const itemsModalTask = ref<string | null>(null)
const expandedTaskId = ref<string | null>(null)
const { flash } = useFlash()
const tick = ref(0)

let queuePoll: ReturnType<typeof setInterval> | null = null
let tasksPoll: ReturnType<typeof setInterval> | null = null
let tickPoll: ReturnType<typeof setInterval> | null = null

async function fetchTasks() {
  try {
    await tasksData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load tasks.' }
  }
}

async function fetchQueueStatus() {
  try { await queueData.refetch() } catch {}
}

async function runTask(id: string) {
  try {
    await $heya('/api/tasks/{id}/run', { method: 'POST', path: { id: id as any } })
    flash.value = { kind: 'ok', text: `Kicked off ${id}.` }
    fetchTasks()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to start task.' }
  }
}

async function cancelTask(id: string) {
  try {
    await $heya('/api/tasks/{id}/cancel', { method: 'POST', path: { id: id as any } })
    flash.value = { kind: 'ok', text: `Cancelled ${id}.` }
    fetchTasks()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to cancel task.' }
  }
}

function configBody(t: TaskResponse, override: Partial<TaskResponse> = {}) {
  return {
    enabled:             override.enabled ?? t.enabled,
    interval_hours:      override.interval_hours ?? t.interval_hours,
    daily_start_time:    override.daily_start_time ?? t.daily_start_time,
    daily_end_time:      override.daily_end_time ?? t.daily_end_time,
    max_runtime_minutes: override.max_runtime_minutes ?? t.max_runtime_minutes,
  } as any
}

async function toggleEnabled(t: TaskResponse) {
  try {
    await $heya('/api/tasks/{id}', {
      method: 'PUT',
      path: { id: t.id as any },
      body: configBody(t, { enabled: !t.enabled }),
    })
    fetchTasks()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Toggle failed.' }
  }
}

async function updateField(t: TaskResponse, patch: Partial<TaskResponse>) {
  try {
    await $heya('/api/tasks/{id}', {
      method: 'PUT',
      path: { id: t.id as any },
      body: configBody(t, patch),
    })
    fetchTasks()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Update failed.' }
  }
}

function taskIcon(id: string): string {
  switch (id) {
    case 'generate_trickplay':  return 'film'
    case 'generate_thumbnails': return 'image'
    case 'scan_libraries':      return 'folder'
    case 'refresh_stale_items': return 'refresh'
    case 'scan_music_loudness': return 'pulse'
    case 'scan_music_fingerprint': return 'fingerprint'
    case 'scan_media_segments': return 'scissors'
    case 'detect_media_segments': return 'wand'
    case 'analyze_music_facets': return 'eq'
    case 'cleanup_scanner_artifacts': return 'database'
    default:                    return 'timer'
  }
}

function taskBadge(t: TaskResponse): { state: 'ok' | 'warn' | 'idle', label: string } {
  if (t.state === 'running') return { state: 'ok',  label: 'Running' }
  if (t.enabled)             return { state: 'warn', label: 'Scheduled' }
  return { state: 'idle', label: 'Disabled' }
}

function resultBadge(result: string): { state: 'ok' | 'warn' | 'error' | 'idle', label: string } {
  if (result === 'completed') return { state: 'ok',    label: 'done' }
  if (result === 'partial')   return { state: 'warn',  label: 'partial' }
  if (result === 'stopped')   return { state: 'idle',  label: 'stopped' }
  if (result === 'error')     return { state: 'error', label: 'error' }
  return { state: 'idle', label: result || 'never' }
}

function timeAgo(dateStr?: string | null) {
  // Bind to the per-second tick so cells re-evaluate in place without
  // remounting the DOM element (the old `:key="tick"` pattern jittered
  // the table layout each refresh).
  void tick.value
  return timeAgoBase(dateStr)
}

function formatDate(d?: string | null) {
  if (!d) return ''
  return new Date(d).toLocaleString('en-GB', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit', hour12: false })
}

function formatDuration(sec: number): string {
  if (sec <= 0) return '—'
  if (sec < 60) return `${sec}s`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ${sec % 60}s`
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return `${h}h ${m}m`
}

function shortTime(t?: string | null): string {
  return (t ?? '').slice(0, 5) || '00:00'
}

function scheduleSummary(t: TaskResponse): string {
  if (!t.enabled) return 'Manual only'
  return `${t.interval_hours}h · ${shortTime(t.daily_start_time)}-${shortTime(t.daily_end_time)} · ${formatDuration((t.max_runtime_minutes ?? 0) * 60)} cap`
}

const intervalOptions = [
  { value: 1, label: '1 hour' },
  { value: 6, label: '6 hours' },
  { value: 12, label: '12 hours' },
  { value: 24, label: '24 hours' },
  { value: 48, label: '2 days' },
  { value: 168, label: '7 days' },
]

const WORKER_LABELS: Record<string, string> = {
  analyze_track_facets:   'Analyzing',
  refresh_artist_centroids: 'Artist centroid',
  refresh_album_centroids: 'Album centroid',
  scan_track_fingerprint:'Fingerprinting',
  scan_track_loudness:    'Loudness',
  scan_album_loudness:    'Album loudness',
  trickplay_file:         'Trickplay',
  thumbnail_extra:        'Thumbnail',
  ffprobe:                'Probing',
  scan_keyframes:         'Keyframes',
  detect_local_assets:    'Local assets',
  enrich_media_item:      'Enriching',
  kickoff_library_scan:   'Scanning',
  process_scan:   'Matching',
  fetch_metadata: 'Fetching metadata',
  apply_metadata:     'Applying',
  kickoff_refresh_stale:  'Refresh',
  kickoff_music_loudness: 'Loudness',
  kickoff_trickplay:      'Trickplay',
  kickoff_thumbnails:     'Thumbnails',
  kickoff_sonic_analysis: 'Sonic',
}
function workerLabel(kind?: string): string {
  if (!kind) return ''
  return WORKER_LABELS[kind] ?? kind
}

function bandCount(p: string): number {
  return queueStatus.value?.pending_by_priority?.[p] ?? 0
}

onMounted(() => {
  queuePoll = setInterval(fetchQueueStatus, 2000)
  tasksPoll = setInterval(fetchTasks, 5000)
  tickPoll = setInterval(() => { tick.value++ }, 1000)
})
onBeforeUnmount(() => {
  if (queuePoll) clearInterval(queuePoll)
  if (tasksPoll) clearInterval(tasksPoll)
  if (tickPoll) clearInterval(tickPoll)
})
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Scheduled tasks</h2>
      <p class="sv2-page-desc">
        Time-windowed automation. Each task can be enabled, scheduled within
        a daily window, capped to a max runtime, and triggered manually.
      </p>
    </header>

    <SettingsSection title="Metadata queue" icon="refresh"
      description="The unified enrich queue, fed by every scan and the refresh task. Polls every 2 seconds.">
      <template #actions>
        <LiveDot connected label="Polling" />
      </template>

      <div class="queue-panel">
        <div class="queue-left">
          <div class="queue-pending">
            <span class="qp-num">{{ queueStatus?.pending ?? 0 }}</span>
            <span class="qp-label">pending</span>
          </div>
          <div class="queue-bands">
            <div class="qb" :class="{ active: bandCount('1') > 0 }">
              <span class="qb-label">P1 · watcher / view</span>
              <span class="qb-count">{{ bandCount('1') }}</span>
            </div>
            <div class="qb" :class="{ active: bandCount('2') > 0 }">
              <span class="qb-label">P2 · movies + TV</span>
              <span class="qb-count">{{ bandCount('2') }}</span>
            </div>
            <div class="qb" :class="{ active: bandCount('3') > 0 }">
              <span class="qb-label">P3 · music + books</span>
              <span class="qb-count">{{ bandCount('3') }}</span>
            </div>
          </div>
        </div>

        <div class="queue-current" v-if="queueStatus?.running">
          <div class="qc-spin"><Icon name="spinner" :size="14" /></div>
          <div class="qc-info">
            <div class="qc-title">{{ queueStatus.running.item_title || queueStatus.running.kind }}</div>
            <div class="qc-meta">
              <span v-if="queueStatus.running.media_type">{{ queueStatus.running.media_type }}</span>
              <span v-if="queueStatus.running.source">· {{ queueStatus.running.source }}</span>
              <span>· P{{ queueStatus.running.priority }}</span>
              <span>· {{ timeAgo(queueStatus.running.started_at) }}</span>
            </div>
          </div>
        </div>
        <div v-else class="queue-idle">
          <StatusBadge state="idle">idle</StatusBadge>
        </div>

        <div class="queue-thru">
          <span class="qt-num">{{ queueStatus?.recent.completed_5min ?? 0 }}</span>
          <span class="qt-label">completed / 5 min</span>
          <span v-if="(queueStatus?.recent.avg_duration_sec ?? 0) > 0" class="qt-avg">
            avg {{ queueStatus!.recent.avg_duration_sec.toFixed(1) }}s
          </span>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="Tasks" icon="timer"
      description="Cadence, run window, runtime cap, and manual control for each scheduled task.">
      <div v-if="tasks.length === 0" class="empty-state">
        <Icon name="info" :size="14" /> No scheduled tasks found.
      </div>
      <div v-else class="task-table" role="table" aria-label="Scheduled tasks">
        <div class="task-table-head" role="row">
          <span role="columnheader">Task</span>
          <span role="columnheader">Status</span>
          <span role="columnheader">Coverage</span>
          <span role="columnheader">Last / next</span>
          <span role="columnheader" />
        </div>

        <div
          v-for="t in tasks"
          :key="t.id"
          class="task-row"
          role="row"
          :class="{ running: t.state === 'running', expanded: expandedTaskId === t.id }"
        >
          <div class="task-main" role="cell">
            <div class="task-icon" :class="t.state === 'running' ? 'on' : ''">
              <Icon :name="taskIcon(t.id)" :size="15" />
            </div>
            <div class="task-title-wrap">
              <div class="task-title">{{ t.display_name }}</div>
              <div class="task-desc" :title="t.description">{{ t.description }}</div>
              <div class="task-sched-summary" :title="scheduleSummary(t)">{{ scheduleSummary(t) }}</div>
              <div v-if="t.state === 'running' && liveTaskProgress[t.id]?.current_item" class="task-current" :title="liveTaskProgress[t.id]?.current_item">
                {{ workerLabel(liveTaskProgress[t.id]?.item_kind) }}: {{ liveTaskProgress[t.id]?.current_item }}
              </div>
            </div>
          </div>

          <div class="task-status" role="cell">
            <StatusBadge :state="taskBadge(t).state">{{ taskBadge(t).label }}</StatusBadge>
            <span v-if="t.state === 'running' && (t.runtime || liveTaskProgress[t.id])" class="runtime-counts">
              <template v-if="(liveTaskProgress[t.id]?.running ?? t.runtime?.running ?? 0) > 0">
                {{ liveTaskProgress[t.id]?.running ?? t.runtime?.running }} run
              </template>
              <template v-if="(liveTaskProgress[t.id]?.running ?? t.runtime?.running ?? 0) > 0 && (liveTaskProgress[t.id]?.pending ?? t.runtime?.pending ?? 0) > 0"> · </template>
              <template v-if="(liveTaskProgress[t.id]?.pending ?? t.runtime?.pending ?? 0) > 0">
                {{ liveTaskProgress[t.id]?.pending ?? t.runtime?.pending }} pend
              </template>
            </span>
          </div>

          <div class="task-coverage" role="cell">
            <template v-if="t.stats && t.stats.total > 0">
              <div class="stats-track">
                <div class="stats-seg complete" :style="{ width: (t.stats.complete / t.stats.total * 100) + '%' }" />
                <div v-if="(t.stats.failed ?? 0) > 0" class="stats-seg failed" :style="{ width: ((t.stats.failed ?? 0) / t.stats.total * 100) + '%' }" />
              </div>
              <div class="stats-label">
                <span class="ok">{{ t.stats.complete }}</span>
                <span class="dim">/{{ t.stats.total }}</span>
                <template v-if="t.stats.pending > 0">
                  · <span class="pending">{{ t.stats.pending }}</span>
                </template>
                <template v-if="(t.stats.failed ?? 0) > 0">
                  · <span class="bad">{{ t.stats.failed }}</span>
                </template>
              </div>
            </template>
            <span v-else class="stats-label dim">No eligible items</span>
          </div>

          <div class="task-times" role="cell">
            <div class="time-line">
              <span class="time-key">Last</span>
              <span v-if="t.last_run_at" class="time-val">
                {{ timeAgo(t.last_run_at) }}
                <StatusBadge :state="resultBadge(t.last_run_result).state">{{ resultBadge(t.last_run_result).label }}</StatusBadge>
              </span>
              <span v-else class="time-val dim">never</span>
            </div>
            <div class="time-line">
              <span class="time-key">Next</span>
              <span v-if="t.next_run_at && t.enabled" class="time-val mono">{{ formatDate(t.next_run_at) }}</span>
              <span v-else class="time-val dim">disabled</span>
            </div>
            <div v-if="t.last_run_items_total > 0" class="time-sub">
              {{ t.last_run_items_processed }}/{{ t.last_run_items_total }} · {{ formatDuration(t.last_run_duration_sec) }}
            </div>
          </div>

          <div v-if="expandedTaskId === t.id" class="task-schedule" role="cell">
            <span class="schedule-label">Schedule</span>
            <div class="schedule-enable" @click="toggleEnabled(t)">
              <AppSwitch
                :model-value="t.enabled"
                size="sm"
                aria-label="Enable schedule"
                @click.stop
                @update:model-value="toggleEnabled(t)"
              />
            </div>
            <select
              class="cfg-control compact"
              :value="t.interval_hours"
              :disabled="!t.enabled"
              aria-label="Run every"
              @change="updateField(t, { interval_hours: Number(($event.target as HTMLSelectElement).value) })"
            >
              <option v-for="opt in intervalOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
            </select>
            <div class="cfg-time">
              <input
                class="cfg-control time"
                type="time"
                :value="t.daily_start_time"
                :disabled="!t.enabled"
                aria-label="Window start"
                @change="updateField(t, { daily_start_time: ($event.target as HTMLInputElement).value })"
              />
              <span class="cfg-sep">-</span>
              <input
                class="cfg-control time"
                type="time"
                :value="t.daily_end_time"
                :disabled="!t.enabled"
                aria-label="Window end"
                @change="updateField(t, { daily_end_time: ($event.target as HTMLInputElement).value })"
              />
            </div>
            <select
              class="cfg-control compact"
              :value="t.max_runtime_minutes"
              :disabled="!t.enabled"
              aria-label="Max runtime"
              @change="updateField(t, { max_runtime_minutes: Number(($event.target as HTMLSelectElement).value) })"
            >
              <option :value="30">30m cap</option>
              <option :value="60">1h cap</option>
              <option :value="120">2h cap</option>
              <option :value="240">4h cap</option>
              <option :value="480">8h cap</option>
            </select>
          </div>

          <div class="task-actions" role="cell">
            <button class="icon-btn" title="View items" aria-label="View items" @click="itemsModalTask = t.id">
              <Icon name="list" :size="13" />
            </button>
            <button
              class="icon-btn"
              :class="{ active: expandedTaskId === t.id }"
              title="Configure schedule"
              aria-label="Configure schedule"
              @click="expandedTaskId = expandedTaskId === t.id ? null : t.id"
            >
              <Icon name="settings" :size="13" />
            </button>
            <button
              class="icon-btn"
              :class="t.state === 'running' ? 'danger' : 'primary'"
              :title="t.state === 'running' ? 'Cancel task' : 'Run now'"
              :aria-label="t.state === 'running' ? 'Cancel task' : 'Run now'"
              @click="t.state === 'running' ? cancelTask(t.id) : runTask(t.id)"
            >
              <Icon :name="t.state === 'running' ? 'close' : 'play'" :size="13" />
            </button>
          </div>
        </div>
      </div>
    </SettingsSection>

    <SettingsFlash :flash="flash" />

    <TaskItemsModal
      v-if="itemsModalTask"
      :task-id="itemsModalTask"
      :task-name="tasks.find(t => t.id === itemsModalTask)?.display_name ?? ''"
      @close="itemsModalTask = null"
    />
  </div>
</template>

<style scoped>
/* queue panel */
.queue-panel {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr) auto;
  gap: 18px;
  align-items: center;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 14px 18px;
}
.queue-left { display: flex; align-items: center; gap: 18px; }
.queue-pending { display: flex; flex-direction: column; align-items: flex-start; }
.qp-num {
  font-size: 28px; font-weight: 600; line-height: 1;
  font-family: var(--font-mono); color: var(--fg-0);
  font-variant-numeric: tabular-nums;
}
.qp-label {
  font-size: 10px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em; color: var(--fg-4);
  margin-top: 4px;
}

.queue-bands {
  display: flex; flex-direction: column; gap: 4px;
  min-width: 160px;
}
.qb {
  display: flex; align-items: center; justify-content: space-between;
  padding: 2px 8px; border-radius: var(--r-xs);
  font-family: var(--font-mono); font-size: 11px;
  color: var(--fg-3); background: var(--bg-1);
}
.qb.active { color: var(--fg-0); background: var(--bg-3); }
.qb-count { font-weight: 600; }

.queue-current {
  display: flex; align-items: center; gap: 10px;
  padding: 8px 12px; background: var(--bg-1);
  border: 1px solid var(--border); border-radius: var(--r-sm);
  min-width: 0;
}
.qc-spin {
  width: 24px; height: 24px;
  display: flex; align-items: center; justify-content: center;
  color: var(--gold);
  animation: qc-spin 1.2s linear infinite;
}
@keyframes qc-spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }
.qc-info { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.qc-title {
  font-size: 13px; color: var(--fg-0); font-weight: 500;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.qc-meta {
  font-size: 11px; font-family: var(--font-mono); color: var(--fg-4);
  display: flex; gap: 4px; flex-wrap: wrap;
}

.queue-idle { display: flex; align-items: center; justify-content: center; padding: 4px; }

.queue-thru { display: flex; flex-direction: column; align-items: flex-end; gap: 2px; min-width: 130px; }
.qt-num {
  font-size: 18px; font-weight: 600; line-height: 1;
  font-family: var(--font-mono); color: var(--fg-1);
}
.qt-label { font-size: 10px; font-family: var(--font-mono); text-transform: uppercase; letter-spacing: 0.06em; color: var(--fg-4); }
.qt-avg { font-size: 11px; font-family: var(--font-mono); color: var(--fg-3); margin-top: 2px; }

/* task matrix */
.task-table {
  overflow: hidden;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.task-table-head,
.task-row {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) 96px minmax(90px, 0.3fr) minmax(145px, 0.48fr) 96px;
  column-gap: 8px;
  align-items: center;
  min-width: 0;
}
.task-table-head {
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  color: var(--fg-4);
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}
.task-row {
  grid-template-areas:
    "main status coverage times actions";
  min-height: 64px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
}
.task-row.expanded {
  grid-template-areas:
    "main status coverage times actions"
    "schedule schedule schedule schedule actions";
  row-gap: 7px;
  min-height: 94px;
}
.task-row:last-child { border-bottom: none; }
.task-row.running {
  background: color-mix(in srgb, var(--gold) 3.5%, transparent);
}
.task-main {
  grid-area: main;
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.task-icon {
  width: 32px;
  height: 32px;
  border-radius: var(--r-sm);
  background: var(--bg-1);
  color: var(--fg-3);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.task-icon.on {
  background: color-mix(in srgb, var(--good) 12%, transparent);
  color: var(--good);
}
.task-title-wrap { min-width: 0; }
.task-title {
  color: var(--fg-0);
  font-size: 13px;
  font-weight: 600;
  line-height: 1.25;
}
.task-desc,
.task-current {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.task-desc {
  margin-top: 2px;
  color: var(--fg-3);
  font-size: 11.5px;
}
.task-current {
  margin-top: 4px;
  color: var(--gold);
  font-family: var(--font-mono);
  font-size: 10.5px;
}
.task-status {
  grid-area: status;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 5px;
}
.runtime-counts {
  color: var(--fg-3);
  font-family: var(--font-mono);
  font-size: 10.5px;
}
.task-sched-summary {
  max-width: 100%;
  overflow: hidden;
  color: var(--fg-4);
  font-family: var(--font-mono);
  font-size: 10px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.task-coverage {
  grid-area: coverage;
  min-width: 0;
}
.stats-track {
  height: 4px;
  border-radius: 2px;
  background: var(--bg-0);
  overflow: hidden;
  display: flex;
}
.stats-seg { height: 100%; transition: width 0.3s ease; }
.stats-seg.complete { background: var(--good); }
.stats-seg.failed { background: var(--bad); }
.stats-label {
  color: var(--fg-3);
  font-family: var(--font-mono);
  font-size: 10.5px;
  margin-top: 5px;
  white-space: nowrap;
}
.stats-label .ok { color: var(--good); font-weight: 600; }
.stats-label .bad { color: var(--bad); font-weight: 600; }
.stats-label .pending { color: var(--gold); font-weight: 600; }
.stats-label .dim { color: var(--fg-4); }
.task-times {
  grid-area: times;
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}
.time-line {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.time-key {
  width: 28px;
  flex-shrink: 0;
  color: var(--fg-4);
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.time-val {
  min-width: 0;
  color: var(--fg-2);
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11.5px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.time-val.mono,
.time-sub {
  font-family: var(--font-mono);
}
.time-sub {
  color: var(--fg-4);
  font-size: 10.5px;
  padding-left: 34px;
}
.dim { color: var(--fg-4); }
.task-schedule {
  grid-area: schedule;
  display: grid;
  grid-template-columns: 68px 34px 94px minmax(126px, 156px) 84px;
  gap: 6px;
  align-items: center;
  min-width: 0;
}
.schedule-label {
  color: var(--fg-4);
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.schedule-enable {
  display: flex;
  align-items: center;
  cursor: pointer;
}
.cfg-time {
  display: grid;
  grid-template-columns: 1fr 8px 1fr;
  gap: 5px;
  align-items: center;
  min-width: 0;
}
.cfg-sep {
  color: var(--fg-4);
  font-family: var(--font-mono);
  font-size: 10px;
  text-align: center;
}
.cfg-control {
  width: 100%;
  min-width: 0;
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-xs);
  color: var(--fg-1);
  font-family: var(--font-mono);
  font-size: 10.5px;
  height: 27px;
  outline: none;
  padding: 0 7px;
}
.cfg-control:focus { border-color: var(--gold); }
.cfg-control:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.cfg-control.compact { cursor: pointer; }
.cfg-control.time { padding: 0 5px; }
.task-actions {
  grid-area: actions;
  display: flex;
  justify-content: flex-end;
  gap: 6px;
}
.icon-btn {
  width: 28px;
  height: 28px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-1);
  color: var(--fg-2);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: border-color 0.12s, color 0.12s, background 0.12s;
}
.icon-btn:hover {
  border-color: color-mix(in srgb, var(--gold) 35%, transparent);
  color: var(--fg-0);
}
.icon-btn.active {
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 30%, transparent);
  color: var(--gold);
}
.icon-btn.primary {
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 30%, transparent);
  color: var(--gold);
}
.icon-btn.danger {
  background: color-mix(in srgb, var(--bad) 10%, transparent);
  border-color: color-mix(in srgb, var(--bad) 25%, transparent);
  color: var(--bad);
}

@media (max-width: 900px) {
  .task-table {
    display: flex;
    flex-direction: column;
    gap: 10px;
    overflow: visible;
    background: transparent;
    border: none;
    border-radius: 0;
  }
  .task-table-head {
    display: none;
  }
  .task-row {
    grid-template-columns: minmax(0, 1fr) 100px;
    grid-template-areas:
      "main actions"
      "status status"
      "coverage coverage"
      "times times";
    row-gap: 7px;
    min-height: 106px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--r-md);
  }
  .task-row:last-child { border: 1px solid var(--border); }
  .task-row.expanded {
    grid-template-areas:
      "main actions"
      "status status"
      "coverage coverage"
      "times times"
      "schedule schedule";
    min-height: 142px;
  }
  .task-status {
    flex-direction: row;
    align-items: center;
  }
  .task-actions {
    align-self: center;
  }
  .task-schedule {
    grid-template-columns: 68px 34px minmax(0, 1fr) minmax(0, 1.1fr);
  }
  .task-schedule .cfg-control.compact:last-child {
    grid-column: 3 / 5;
  }
}

@media (max-width: 720px) {
  .queue-panel {
    grid-template-columns: 1fr;
    gap: 12px;
  }
  .queue-left { flex-wrap: wrap; }
  .queue-thru { align-items: flex-start; }

  .task-table {
    gap: 12px;
  }
  .task-row {
    grid-template-areas:
      "main actions"
      "status status"
      "coverage coverage"
      "times times";
    grid-template-columns: minmax(0, 1fr) 100px;
    min-width: 0;
    gap: 10px;
    padding: 12px;
  }
  .task-row.expanded {
    grid-template-areas:
      "main actions"
      "status status"
      "coverage coverage"
      "times times"
      "schedule schedule";
  }
  .task-status,
  .task-coverage,
  .task-times,
  .task-schedule {
    padding-left: 42px;
  }
  .task-status {
    flex-direction: row;
    align-items: center;
  }
  .task-schedule {
    grid-template-columns: 68px 34px minmax(0, 1fr) minmax(0, 1.2fr);
  }
  .task-schedule .cfg-control.compact:last-child {
    grid-column: 3 / 5;
  }
  .task-actions {
    justify-content: flex-end;
  }
}
</style>
