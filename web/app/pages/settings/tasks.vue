<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import type { components } from '#open-fetch-schemas/heya'
type TaskResponse = components['schemas']['TaskResponse']
type QueueStatus  = components['schemas']['MetadataQueueStatus']

const { $heya } = useNuxtApp()
const { taskProgress: liveTaskProgress } = useEventBus()

const tasks = ref<TaskResponse[]>([])
const queueStatus = ref<QueueStatus | null>(null)
const itemsModalTask = ref<string | null>(null)
const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)
const tick = ref(0)

let queuePoll: ReturnType<typeof setInterval> | null = null
let tasksPoll: ReturnType<typeof setInterval> | null = null
let tickPoll: ReturnType<typeof setInterval> | null = null

async function fetchTasks() {
  try {
    tasks.value = await $heya('/api/tasks') as unknown as TaskResponse[]
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load tasks.' }
  }
}

async function fetchQueueStatus() {
  try {
    queueStatus.value = await $heya('/api/jobs/queue/metadata')
  } catch {}
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
    case 'sonic_analysis':      return 'eq'
    case 'music_loudness':      return 'pulse'
    default:                    return 'timer'
  }
}

function taskBadge(t: TaskResponse): { state: 'ok' | 'warn' | 'idle', label: string } {
  if (t.state === 'running') return { state: 'ok',  label: 'Running' }
  if (t.enabled)             return { state: 'warn', label: 'Scheduled' }
  return { state: 'idle', label: 'Disabled' }
}

function resultBadge(result: string): { state: 'ok' | 'warn' | 'error' | 'idle', label: string } {
  if (result === 'completed') return { state: 'ok',    label: 'completed' }
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
  if (!dateStr) return 'never'
  const sec = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`
  return `${Math.floor(sec / 86400)}d ago`
}

function formatDate(d?: string | null) {
  if (!d) return ''
  return new Date(d).toLocaleString('en-GB', { dateStyle: 'medium', timeStyle: 'short' })
}

function formatDuration(sec: number): string {
  if (sec <= 0) return '—'
  if (sec < 60) return `${sec}s`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ${sec % 60}s`
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return `${h}h ${m}m`
}

const WORKER_LABELS: Record<string, string> = {
  analyze_track_facets:   'Analyzing',
  scan_track_loudness:    'Loudness',
  scan_album_loudness:    'Album loudness',
  trickplay_file:         'Trickplay',
  thumbnail_extra:        'Thumbnail',
  process_file:           'Processing',
  ffprobe:                'Probing',
  detect_local_assets:    'Local assets',
  metadata_match:         'Matching',
  enrich_media_item:      'Enriching',
  kickoff_library_scan:   'Scanning',
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

const queueTone = computed<'good' | 'warn' | 'neutral'>(() => {
  if (!queueStatus.value) return 'neutral'
  if (queueStatus.value.running) return 'good'
  return queueStatus.value.pending > 0 ? 'warn' : 'neutral'
})

function bandCount(p: string): number {
  return queueStatus.value?.pending_by_priority?.[p] ?? 0
}

onMounted(() => {
  fetchTasks()
  fetchQueueStatus()
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
          <div class="qc-spin"><Icon name="loader" :size="14" /></div>
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
      description="Last run, next run, schedule window, and a manual trigger for each.">
      <div v-if="tasks.length === 0" class="empty-state">
        <Icon name="info" :size="14" /> No scheduled tasks found.
      </div>
      <div v-else class="task-list">
        <div v-for="t in tasks" :key="t.id" class="task-card">
          <div class="t-head">
            <div class="t-icon" :class="t.state === 'running' ? 'on' : ''">
              <Icon :name="taskIcon(t.id)" :size="16" />
            </div>
            <div class="t-info">
              <div class="t-name">
                {{ t.display_name }}
                <StatusBadge :state="taskBadge(t).state">{{ taskBadge(t).label }}</StatusBadge>
              </div>
              <div class="t-desc">{{ t.description }}</div>
            </div>
            <div class="t-actions">
              <button class="sv2-btn ghost" @click="itemsModalTask = t.id">
                <Icon name="list" :size="12" />
                View items
              </button>
              <button
                class="sv2-btn"
                :class="t.state === 'running' ? 'danger' : 'primary'"
                @click="t.state === 'running' ? cancelTask(t.id) : runTask(t.id)"
              >
                <Icon :name="t.state === 'running' ? 'close' : 'play'" :size="12" />
                {{ t.state === 'running' ? 'Cancel' : 'Run now' }}
              </button>
            </div>
          </div>

          <div v-if="t.stats && t.stats.total > 0" class="t-stats">
            <div class="stats-track">
              <div class="stats-seg complete" :style="{ width: (t.stats.complete / t.stats.total * 100) + '%' }" />
              <div v-if="(t.stats.failed ?? 0) > 0" class="stats-seg failed" :style="{ width: ((t.stats.failed ?? 0) / t.stats.total * 100) + '%' }" />
            </div>
            <div class="stats-label">
              <span class="ok">{{ t.stats.complete }}</span> complete
              <template v-if="(t.stats.failed ?? 0) > 0">
                · <span class="bad">{{ t.stats.failed }}</span> failed
              </template>
              <template v-if="t.stats.pending > 0">
                · <span class="pending">{{ t.stats.pending }}</span> pending
              </template>
              <span class="dim"> / {{ t.stats.total }}</span>
            </div>
          </div>
          <div v-else-if="t.stats && t.stats.total === 0" class="t-stats">
            <div class="stats-label dim">No eligible items.</div>
          </div>

          <div v-if="t.state === 'running' && (t.runtime || liveTaskProgress[t.id])" class="t-live">
            <div class="live-track">
              <div class="live-fill" />
            </div>
            <div class="live-meta">
              <span v-if="liveTaskProgress[t.id]?.current_item" class="live-current">
                {{ workerLabel(liveTaskProgress[t.id]?.item_kind) }}: {{ liveTaskProgress[t.id]?.current_item }}
              </span>
              <span class="live-count">
                <template v-if="(liveTaskProgress[t.id]?.running ?? t.runtime?.running ?? 0) > 0">
                  {{ liveTaskProgress[t.id]?.running ?? t.runtime?.running }} running
                </template>
                <template v-if="(liveTaskProgress[t.id]?.running ?? t.runtime?.running ?? 0) > 0 && (liveTaskProgress[t.id]?.pending ?? t.runtime?.pending ?? 0) > 0"> · </template>
                <template v-if="(liveTaskProgress[t.id]?.pending ?? t.runtime?.pending ?? 0) > 0">
                  {{ liveTaskProgress[t.id]?.pending ?? t.runtime?.pending }} pending
                </template>
              </span>
            </div>
          </div>

          <div class="t-detail">
            <div v-if="t.last_run_at" class="d-row">
              <span class="d-key">Last run</span>
              <span class="d-val">
                {{ timeAgo(t.last_run_at) }}
                <StatusBadge :state="resultBadge(t.last_run_result).state">{{ resultBadge(t.last_run_result).label }}</StatusBadge>
                <span v-if="t.last_run_items_total > 0" class="d-sub">
                  {{ t.last_run_items_processed }}/{{ t.last_run_items_total }} items · {{ formatDuration(t.last_run_duration_sec) }}
                </span>
              </span>
            </div>
            <div v-if="t.next_run_at && t.enabled" class="d-row">
              <span class="d-key">Next run</span>
              <span class="d-val mono">{{ formatDate(t.next_run_at) }}</span>
            </div>

            <div class="d-schedule">
              <label class="toggle-row" @click="toggleEnabled(t)">
                <span class="toggle-label">Enable schedule</span>
                <button class="toggle-sw" :class="{ on: t.enabled }">
                  <span class="toggle-knob" />
                </button>
              </label>
              <div v-if="t.enabled" class="d-config">
                <div class="cfg-field">
                  <label class="cfg-label">Time window</label>
                  <div class="cfg-time">
                    <input
                      type="time"
                      :value="t.daily_start_time"
                      @change="updateField(t, { daily_start_time: ($event.target as HTMLInputElement).value })"
                    />
                    <span class="cfg-sep">to</span>
                    <input
                      type="time"
                      :value="t.daily_end_time"
                      @change="updateField(t, { daily_end_time: ($event.target as HTMLInputElement).value })"
                    />
                  </div>
                </div>
                <div class="cfg-field">
                  <label class="cfg-label">Max runtime</label>
                  <select
                    :value="t.max_runtime_minutes"
                    @change="updateField(t, { max_runtime_minutes: Number(($event.target as HTMLSelectElement).value) })"
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
    </SettingsSection>

    <div v-if="flash" class="sv2-flash" :class="flash.kind">
      <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" />
      {{ flash.text }}
    </div>

    <TaskItemsModal
      v-if="itemsModalTask"
      :task-id="itemsModalTask"
      :task-name="tasks.find(t => t.id === itemsModalTask)?.display_name ?? ''"
      @close="itemsModalTask = null"
    />
  </div>
</template>

<style scoped>
.sv2-page-head { margin-bottom: 28px; }
.sv2-page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.sv2-page-desc { margin: 6px 0 0; font-size: 13px; color: var(--fg-3); line-height: 1.55; }

.empty-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2); border: 1px solid var(--border);
  border-radius: var(--r-md);
}

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

/* task cards */
.task-list { display: flex; flex-direction: column; gap: 10px; }
.task-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 16px 18px;
}
.t-head { display: flex; align-items: flex-start; gap: 14px; }
.t-icon {
  width: 38px; height: 38px;
  border-radius: var(--r-sm);
  background: var(--bg-1);
  color: var(--fg-3);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.t-icon.on { background: rgba(111, 191, 124, 0.12); color: var(--good); }
.t-info { flex: 1; min-width: 0; }
.t-name {
  display: flex; align-items: center; gap: 8px;
  font-size: 14px; font-weight: 500; color: var(--fg-0);
}
.t-desc { font-size: 12px; color: var(--fg-3); margin-top: 2px; line-height: 1.4; }
.t-actions { display: flex; gap: 6px; flex-shrink: 0; }

/* stats bar */
.t-stats { margin-top: 12px; }
.stats-track {
  height: 4px; border-radius: 2px;
  background: var(--bg-0); overflow: hidden;
  display: flex;
}
.stats-seg { height: 100%; transition: width 0.3s ease; }
.stats-seg.complete { background: var(--good); }
.stats-seg.failed { background: var(--bad); }
.stats-label {
  font-size: 11px; font-family: var(--font-mono);
  color: var(--fg-3); margin-top: 6px;
}
.stats-label .ok { color: var(--good); font-weight: 600; }
.stats-label .bad { color: var(--bad); font-weight: 600; }
.stats-label .pending { color: var(--gold); font-weight: 600; }
.stats-label .dim { color: var(--fg-4); }

/* live progress */
.t-live { margin-top: 12px; }
.live-track {
  height: 6px; border-radius: 3px;
  background: var(--bg-0); overflow: hidden;
}
.live-fill {
  height: 100%;
  background: linear-gradient(90deg, transparent 0%, var(--gold) 50%, transparent 100%);
  background-size: 200% 100%;
  animation: live-indeterminate 1.5s ease-in-out infinite;
}
@keyframes live-indeterminate { 0% { background-position: -100% 0; } 100% { background-position: 200% 0; } }
.live-meta {
  display: flex; align-items: center; gap: 12px; margin-top: 6px;
  font-family: var(--font-mono); font-size: 11px; color: var(--fg-3);
}
.live-current { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.live-count { font-weight: 600; color: var(--fg-2); }

/* detail / schedule */
.t-detail { margin-top: 14px; padding-top: 12px; border-top: 1px solid var(--border); }
.d-row { display: flex; align-items: baseline; gap: 8px; margin-bottom: 6px; font-size: 12px; }
.d-key {
  width: 80px; flex-shrink: 0;
  font-family: var(--font-mono);
  font-size: 10px; font-weight: 600;
  text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--fg-4);
}
.d-val { color: var(--fg-1); display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.d-val.mono { font-family: var(--font-mono); font-size: 11.5px; }
.d-sub { color: var(--fg-3); font-family: var(--font-mono); font-size: 11px; }

.d-schedule { margin-top: 10px; }
.toggle-row {
  display: flex; align-items: center; justify-content: space-between;
  cursor: pointer; user-select: none;
}
.toggle-label { font-size: 12.5px; color: var(--fg-1); }
.toggle-sw {
  width: 36px; height: 20px; border-radius: 10px;
  background: var(--bg-3); border: 1px solid var(--border);
  position: relative; cursor: pointer;
  transition: background 0.15s, border-color 0.15s;
}
.toggle-sw.on { background: var(--gold); border-color: var(--gold); }
.toggle-knob {
  width: 16px; height: 16px; border-radius: 50%;
  background: white; position: absolute; top: 1px; left: 1px;
  transition: transform 0.15s ease;
  box-shadow: 0 1px 2px rgba(0,0,0,0.2);
}
.toggle-sw.on .toggle-knob { transform: translateX(16px); }

.d-config { display: flex; gap: 24px; margin-top: 10px; flex-wrap: wrap; }
.cfg-field { display: flex; flex-direction: column; gap: 4px; }
.cfg-label {
  font-family: var(--font-mono);
  font-size: 10px; font-weight: 600;
  text-transform: uppercase; letter-spacing: 0.06em; color: var(--fg-4);
}
.cfg-time { display: flex; align-items: center; gap: 6px; }
.cfg-sep { font-size: 11px; color: var(--fg-3); }
.cfg-field input, .cfg-field select {
  background: var(--bg-0); border: 1px solid var(--border); border-radius: var(--r-xs);
  padding: 5px 9px; font-size: 12px; font-family: var(--font-mono); color: var(--fg-1);
  outline: none;
}
.cfg-field input:focus, .cfg-field select:focus { border-color: var(--gold); }
.cfg-field select { cursor: pointer; }

/* buttons */
.sv2-btn {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 6px 12px;
  border-radius: var(--r-sm);
  font-size: 11.5px;
  font-weight: 500;
  cursor: pointer;
  transition: border-color 0.12s, color 0.12s, background 0.12s;
}
.sv2-btn.ghost { border: 1px solid var(--border); background: var(--bg-1); color: var(--fg-2); }
.sv2-btn.ghost:hover { border-color: var(--border-strong); color: var(--fg-0); }
.sv2-btn.primary { background: var(--gold); color: #1a1408; }
.sv2-btn.primary:hover { background: var(--gold-deep); }
.sv2-btn.danger {
  border: 1px solid rgba(217,107,107,0.30);
  background: rgba(217,107,107,0.06);
  color: var(--bad);
}
.sv2-btn.danger:hover { background: rgba(217,107,107,0.12); }

.sv2-flash {
  margin-top: 16px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  font-size: 12px;
  display: flex; align-items: center; gap: 8px;
}
.sv2-flash.ok { background: rgba(111,191,124,0.10); border: 1px solid rgba(111,191,124,0.25); color: var(--good); }
.sv2-flash.err { background: rgba(217,107,107,0.10); border: 1px solid rgba(217,107,107,0.30); color: var(--bad); }
</style>
