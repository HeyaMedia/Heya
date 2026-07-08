<script setup lang="ts">
import { timeAgo as timeAgoBase } from '~/composables/useFormat'
definePageMeta({ layout: 'settings', middleware: 'admin' })

import type { components } from '#open-fetch-schemas/heya'
type JobRow = components['schemas']['JobRow']
type SummaryRow = components['schemas']['JobSummaryRow']
type WorkerSetting = {
  kind: string
  label: string
  value: number
  default: number
  source: 'default' | 'env' | 'db'
  env_var?: string
  locked?: boolean
}
type WorkerSettingsResponse = {
  workers: WorkerSetting[]
  restart_required: boolean
}
type WorkerPresetID = 'rpi' | 'default' | 'speedy' | 'turbo'
type WorkerPreset = {
  id: WorkerPresetID
  label: string
  title: string
}

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

type KindRow = components['schemas']['JobKindSummaryRow']

const jobs = ref<JobRow[]>([])
const total = ref(0)
const summary = ref<SummaryRow[]>([])
const kindSummary = ref<KindRow[]>([])
const filter = ref<string>('')
const kindFilter = ref<string>('')
const offset = ref(0)
const limit = 50
const expanded = ref<number | null>(null)
const loading = ref(true)
const busy = ref<'' | 'rescue' | 'completed' | 'all' | 'kind'>('')
const { flash } = useFlash()
const workerSettingsOpen = ref(false)
const workerSettings = ref<WorkerSettingsResponse | null>(null)
const workerDraft = ref<Record<string, number>>({})
const workerSaving = ref(false)
const workerSettingsError = ref('')
const tick = ref(0)
setInterval(() => { tick.value++ }, 1000)

const WORKER_PRESETS: WorkerPreset[] = [
  { id: 'rpi', label: 'RPI', title: 'Low-power profile: one worker per queue.' },
  { id: 'default', label: 'Default', title: 'Restore Heya defaults.' },
  { id: 'speedy', label: 'Speedy', title: 'A modest bump for scanner and I/O queues.' },
  { id: 'turbo', label: 'Turbo', title: 'A larger bump while keeping CPU-heavy queues restrained.' },
]

const SPEEDY_WORKERS: Record<string, number> = {
  process_scan: 6,
  fetch_metadata: 6,
  apply_metadata: 5,
  ffprobe: 2,
  scan_keyframes: 2,
  detect_local_assets: 2,
  enrich_media_item: 2,
  person_fetch: 8,
  ratings_fetch: 5,
  fetch_artwork: 5,
  download_image: 6,
  save_images: 2,
  save_nfo: 2,
  save_music_nfo: 2,
  force_refresh_metadata: 2,
  force_refresh_images: 2,
  scan_media_segments_file: 8,
  scan_track_fingerprint: 2,
  scan_track_loudness: 2,
}

const TURBO_WORKERS: Record<string, number> = {
  process_scan: 8,
  fetch_metadata: 8,
  apply_metadata: 6,
  ffprobe: 3,
  scan_keyframes: 3,
  detect_local_assets: 3,
  enrich_media_item: 3,
  person_fetch: 12,
  ratings_fetch: 8,
  fetch_artwork: 8,
  download_image: 10,
  save_images: 3,
  save_nfo: 2,
  save_music_nfo: 2,
  force_refresh_metadata: 3,
  force_refresh_images: 3,
  scan_media_segments_file: 10,
  scan_track_fingerprint: 2,
  scan_track_loudness: 2,
  scan_album_loudness: 2,
  detect_segments_movie: 2,
  detect_segments_season: 2,
  trickplay: 2,
  thumbnails: 2,
  scan_library_disk: 2,
}

async function fetchJobs() {
  try {
    const query: Record<string, any> = { limit, offset: offset.value }
    if (filter.value) query.state = filter.value
    if (kindFilter.value) query.kind = kindFilter.value
    const res = await $heya('/api/jobs', { query })
    jobs.value = res.jobs ?? []
    total.value = res.total
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load jobs.' }
  }
}

async function fetchSummary() {
  try {
    summary.value = await $heya('/api/jobs/summary')
  } catch {}
}

async function fetchKinds() {
  try {
    kindSummary.value = await $heya('/api/jobs/kinds')
  } catch {}
}

async function fetchWorkerSettings() {
  try {
    workerSettingsError.value = ''
    const raw = await $heya('/api/jobs/worker-settings' as any) as any
    const res = normalizeWorkerSettings(raw)
    if (!Array.isArray(res.workers)) {
      throw new Error('Worker settings endpoint returned an unexpected response. Restart the Go backend so /api/jobs/worker-settings is active.')
    }
    workerSettings.value = res
    workerDraft.value = Object.fromEntries((res.workers ?? []).map(w => [w.kind, w.value]))
  } catch (e: any) {
    workerSettings.value = { workers: [], restart_required: true }
    workerSettingsError.value = e?.message ?? 'Failed to load worker settings.'
    flash.value = { kind: 'err', text: workerSettingsError.value }
  }
}

function normalizeWorkerSettings(raw: any): WorkerSettingsResponse {
  if (typeof raw === 'string') {
    return { workers: undefined as any, restart_required: true }
  }
  const body = raw?.body ?? raw
  return {
    workers: body?.workers ?? body?.Workers,
    restart_required: body?.restart_required ?? body?.restartRequired ?? body?.RestartRequired ?? true,
  }
}

async function openWorkerSettings() {
  workerSettingsOpen.value = true
  await fetchWorkerSettings()
}

function presetWorkerValue(preset: WorkerPresetID, worker: WorkerSetting): number {
  if (preset === 'rpi') return 1
  if (preset === 'default') return worker.default
  const table = preset === 'speedy' ? SPEEDY_WORKERS : TURBO_WORKERS
  return table[worker.kind] ?? worker.default
}

function applyWorkerPreset(preset: WorkerPresetID) {
  if (!workerSettings.value) return
  const next = { ...workerDraft.value }
  for (const worker of workerSettings.value.workers) {
    if (worker.locked) continue
    next[worker.kind] = Math.min(64, Math.max(1, presetWorkerValue(preset, worker)))
  }
  workerDraft.value = next
}

async function saveWorkerSettings(close?: () => void) {
  if (!workerSettings.value) return
  workerSaving.value = true
  try {
    const changed: Record<string, number> = {}
    for (const w of workerSettings.value.workers ?? []) {
      const value = Number(workerDraft.value[w.kind] ?? w.value)
      if (value !== w.value) changed[w.kind] = value
    }
    await $heya('/api/jobs/worker-settings' as any, {
      method: 'PUT',
      body: { workers: changed },
    } as any)
    flash.value = { kind: 'ok', text: 'Worker settings saved. Restart Heya for queue worker counts to change.' }
    await fetchWorkerSettings()
    close?.()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to save worker settings.' }
  } finally {
    workerSaving.value = false
  }
}

// First load shows the spinner; subsequent refreshes silently update so
// the table doesn't flash empty every time WS fires a queue event.
async function refresh() {
  await Promise.all([fetchJobs(), fetchSummary(), fetchKinds()])
}

async function retryJob(id: number) {
  try {
    await $heya('/api/jobs/{id}/retry', { method: 'POST', path: { id } })
    flash.value = { kind: 'ok', text: `Job #${id} requeued.` }
    refresh()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Retry failed.' }
  }
}

async function cancelJob(id: number) {
  try {
    await $heya('/api/jobs/{id}/cancel', { method: 'POST', path: { id } })
    flash.value = { kind: 'ok', text: `Job #${id} cancelled.` }
    refresh()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Cancel failed.' }
  }
}

async function rescueStuck() {
  busy.value = 'rescue'
  try {
    await $heya('/api/jobs/rescue', { method: 'POST' })
    flash.value = { kind: 'ok', text: 'Stuck jobs requeued.' }
    refresh()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Rescue failed.' }
  } finally {
    busy.value = ''
  }
}

async function clearCompleted() {
  busy.value = 'completed'
  try {
    await $heya('/api/jobs/completed', { method: 'DELETE' })
    flash.value = { kind: 'ok', text: 'Cleared completed jobs.' }
    refresh()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Clear failed.' }
  } finally {
    busy.value = ''
  }
}

async function clearAll() {
  const ok = await confirm({
    title: 'Delete every job?',
    message: 'Every job in the queue will be removed — pending, running, completed, all of it. This cannot be undone.',
    destructive: true,
    confirmLabel: 'Delete all',
  })
  if (!ok) return
  busy.value = 'all'
  try {
    await $heya('/api/jobs', { method: 'DELETE' })
    flash.value = { kind: 'ok', text: 'Queue wiped.' }
    refresh()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Wipe failed.' }
  } finally {
    busy.value = ''
  }
}

async function flushKind() {
  const k = kindFilter.value
  if (!k) return
  const scope = filter.value ? `${filter.value} ` : ''
  const ok = await confirm({
    title: `Flush ${k} jobs?`,
    message: `All ${total.value} ${scope}${k} job(s) will be permanently deleted. This cannot be undone.`,
    destructive: true,
    confirmLabel: 'Flush',
  })
  if (!ok) return
  busy.value = 'kind'
  try {
    // `kind` is required by the endpoint; keep it statically present so the
    // typed client is satisfied. `state` is the optional enum narrow.
    const query: { kind: string, state?: any } = { kind: k }
    if (filter.value) query.state = filter.value
    await $heya('/api/jobs/by-kind', { method: 'DELETE', query })
    flash.value = { kind: 'ok', text: `Flushed ${scope}${k} jobs.` }
    refresh()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Flush failed.' }
  } finally {
    busy.value = ''
  }
}

watch(filter, () => { offset.value = 0; fetchJobs() })
watch(kindFilter, () => { offset.value = 0; fetchJobs() })

function summaryCount(state: string): number {
  return summary.value.find(s => s.state === state)?.count ?? 0
}

const tileTones = computed(() => ({
  running:    summaryCount('running')    > 0 ? 'good' : 'neutral',
  available:  summaryCount('available')  > 0 ? 'warn' : 'neutral',
  retryable:  summaryCount('retryable')  > 0 ? 'warn' : 'neutral',
  discarded:  summaryCount('discarded')  > 0 ? 'bad'  : 'neutral',
  cancelled:  summaryCount('cancelled')  > 0 ? 'bad'  : 'neutral',
  completed:  'neutral',
} as const))

// Bound to `tick` so it re-evaluates each second without remounting the cell.
function timeAgo(iso?: string | null): string {
  void tick.value
  return timeAgoBase(iso)
}

function formatArgs(raw: string) {
  try { return JSON.stringify(JSON.parse(raw), null, 2) } catch { return raw }
}

const WORKER_GROUPS: Array<{ title: string, kinds: string[] }> = [
  { title: 'Scanner', kinds: ['kickoff_library_scan', 'process_scan', 'fetch_metadata', 'apply_metadata', 'ffprobe', 'scan_keyframes', 'detect_local_assets'] },
  { title: 'Metadata', kinds: ['enrich_media_item', 'person_fetch', 'ratings_fetch', 'fetch_artwork', 'force_refresh_metadata', 'force_refresh_images'] },
  { title: 'Files', kinds: ['download_image', 'save_images', 'save_nfo', 'save_music_nfo', 'soft_delete', 'scan_library_disk'] },
  { title: 'Analysis', kinds: ['scan_track_fingerprint', 'scan_track_loudness', 'scan_album_loudness', 'scan_media_segments_file', 'detect_segments_season', 'detect_segments_movie', 'trickplay', 'thumbnails', 'sonic_analysis', 'transcode', 'artist_centroid', 'album_centroid'] },
  { title: 'Kickoffs', kinds: ['kickoff_refresh_stale', 'kickoff_music_loudness', 'kickoff_music_fingerprint', 'kickoff_media_segments', 'kickoff_detect_segments', 'kickoff_trickplay', 'kickoff_thumbnails', 'kickoff_sonic_analysis', 'debounce_sweep', 'default'] },
]

const workerByKind = computed(() => {
  const map = new Map<string, WorkerSetting>()
  for (const w of workerSettings.value?.workers ?? []) map.set(w.kind, w)
  return map
})

const workerGroups = computed(() => WORKER_GROUPS
  .map(group => ({
    ...group,
    workers: group.kinds.map(kind => workerByKind.value.get(kind)).filter(Boolean) as WorkerSetting[],
  }))
  .filter(group => group.workers.length > 0)
)

const groupedWorkerKinds = computed(() => new Set(WORKER_GROUPS.flatMap(group => group.kinds)))
const ungroupedWorkers = computed(() =>
  (workerSettings.value?.workers ?? []).filter(w => !groupedWorkerKinds.value.has(w.kind)),
)
const displayWorkerGroups = computed(() => {
  const groups = [...workerGroups.value]
  if (ungroupedWorkers.value.length > 0) {
    groups.push({ title: 'Other', kinds: [], workers: ungroupedWorkers.value })
  }
  return groups
})

const startIdx = computed(() => total.value === 0 ? 0 : offset.value + 1)
const endIdx   = computed(() => Math.min(offset.value + limit, total.value))

let debounce: ReturnType<typeof setTimeout> | null = null
function debouncedRefresh() {
  if (debounce) clearTimeout(debounce)
  debounce = setTimeout(refresh, 400)
}

// Hoist the subscription out of onMounted: lifecycle hooks must register
// during synchronous setup, not inside an async-or-not onMounted body.
const { on } = useEventBus()
const unsubs = [
  on('queue.status',   debouncedRefresh),
  on('scan.started',   debouncedRefresh),
  on('scan.completed', debouncedRefresh),
]
onUnmounted(() => {
  unsubs.forEach(fn => fn())
  if (debounce) clearTimeout(debounce)
})

// Polling fallback for WS drops + reconnect catchup. immediate=false because
// onMounted below already does the first fetch and toggles `loading`.
const { connected: wsConnected } = useLiveFallback(refresh, {
  pollWhileOffline: 5000,
  immediate: false,
})

onMounted(async () => {
  await refresh()
  loading.value = false
})
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Jobs</h2>
      <p class="sv2-page-desc">
        River background queue — every scan, ffprobe, image fetch and analyse
        runs through here. Auto-refreshes on queue events.
      </p>
    </header>

    <div class="tiles">
      <MetricTile label="Running"    :value="summaryCount('running')"    icon="pulse"   :tone="tileTones.running" />
      <MetricTile label="Available"  :value="summaryCount('available')"  icon="list"    :tone="tileTones.available" />
      <MetricTile label="Retryable"  :value="summaryCount('retryable')"  icon="refresh" :tone="tileTones.retryable" />
      <MetricTile label="Discarded"  :value="summaryCount('discarded')"  icon="warning" :tone="tileTones.discarded" />
      <MetricTile label="Cancelled"  :value="summaryCount('cancelled')"  icon="close"   :tone="tileTones.cancelled" />
      <MetricTile label="Completed"  :value="summaryCount('completed')"  icon="check"   :tone="tileTones.completed" />
    </div>

    <SettingsSection title="Queue" icon="list">
      <template #actions>
        <LiveDot :connected="wsConnected" :label="wsConnected ? 'Live' : 'Polling · WS offline'" />
        <button class="sv2-btn ghost" @click="openWorkerSettings">
          <Icon name="settings" :size="12" />
          Workers
        </button>
        <button class="sv2-btn ghost" :disabled="busy === 'rescue'" @click="rescueStuck">
          <Icon name="lightning" :size="12" />
          {{ busy === 'rescue' ? 'Rescuing…' : 'Rescue stuck' }}
        </button>
        <button class="sv2-btn ghost" :disabled="busy === 'completed'" @click="clearCompleted">
          <Icon name="trash" :size="12" />
          {{ busy === 'completed' ? 'Clearing…' : 'Clear completed' }}
        </button>
        <button class="sv2-btn danger" :disabled="busy === 'all'" @click="clearAll">
          <Icon name="trash" :size="12" />
          {{ busy === 'all' ? 'Wiping…' : 'Wipe queue' }}
        </button>
        <button class="sv2-btn danger" :disabled="!kindFilter || busy === 'kind'" @click="flushKind">
          <Icon name="trash" :size="12" />
          {{ busy === 'kind' ? 'Flushing…' : kindFilter ? `Flush ${kindFilter}` : 'Flush kind' }}
        </button>
        <button class="sv2-btn ghost" @click="refresh">
          <Icon name="refresh" :size="12" />
          Refresh
        </button>
      </template>

      <div class="filter-row">
        <span class="filter-group-label"><Icon name="list" :size="11" /> Filter · State</span>
        <button
          v-for="s in summary"
          :key="s.state"
          class="filter-pill"
          :class="[s.state, { active: filter === s.state }]"
          @click="filter = filter === s.state ? '' : s.state"
        >
          <span class="filter-count">{{ s.count }}</span>
          <span class="filter-label">{{ s.state }}</span>
        </button>
        <button v-if="filter" class="filter-pill clear" @click="filter = ''">
          <Icon name="close" :size="10" /> Clear
        </button>
      </div>

      <div v-if="kindSummary.length" class="filter-row kinds">
        <span class="filter-group-label"><Icon name="list" :size="11" /> Filter · Kind</span>
        <button
          v-for="k in kindSummary"
          :key="k.kind"
          class="filter-pill kind"
          :class="{ active: kindFilter === k.kind }"
          @click="kindFilter = kindFilter === k.kind ? '' : k.kind"
        >
          <span class="filter-count">{{ k.count }}</span>
          <span class="filter-label">{{ k.kind }}</span>
        </button>
        <button v-if="kindFilter" class="filter-pill clear" @click="kindFilter = ''">
          <Icon name="close" :size="10" /> Clear
        </button>
      </div>

      <div v-if="loading" class="empty-state"><Icon name="spinner" :size="14" /> Loading…</div>
      <div v-else-if="jobs.length === 0" class="empty-state">
        <Icon name="check" :size="14" />
        {{ filter || kindFilter ? `No ${[filter, kindFilter].filter(Boolean).join(' ')} jobs.` : 'Queue is empty.' }}
      </div>

      <div v-else class="job-table">
        <div class="thead">
          <span class="col-state">State</span>
          <span class="col-kind">Kind</span>
          <span class="col-queue">Queue</span>
          <span class="col-attempt">Attempt</span>
          <span class="col-time">Created</span>
          <span class="col-actions" />
        </div>
        <div
          v-for="j in jobs"
          :key="j.id"
          class="job-row"
          :class="{ expanded: expanded === j.id }"
          @click="expanded = expanded === j.id ? null : j.id"
        >
          <span class="col-state">
            <span class="state-dot" :class="j.state" />
            {{ j.state }}
          </span>
          <span class="col-kind mono">{{ j.kind }}</span>
          <span class="col-queue mono dim">{{ j.queue }}</span>
          <span class="col-attempt mono">{{ j.attempt }}/{{ j.max_attempts }}</span>
          <span class="col-time mono dim">{{ timeAgo(j.created_at) }}</span>
          <span class="col-actions" @click.stop>
            <button
              v-if="['discarded', 'cancelled', 'retryable'].includes(j.state)"
              class="row-btn"
              :title="`Retry job #${j.id}`"
              @click="retryJob(j.id)"
            >
              <Icon name="refresh" :size="11" />
            </button>
            <button
              v-if="['available', 'retryable', 'scheduled'].includes(j.state)"
              class="row-btn danger"
              :title="`Cancel job #${j.id}`"
              @click="cancelJob(j.id)"
            >
              <Icon name="close" :size="11" />
            </button>
          </span>

          <div v-if="expanded === j.id" class="detail" @click.stop>
            <div class="detail-grid">
              <span class="dkey">ID</span>
              <span class="dval mono">{{ j.id }}</span>
              <span class="dkey">Created</span>
              <span class="dval">{{ formatDateTime(j.created_at) }}</span>
              <template v-if="j.attempted_at">
                <span class="dkey">Last attempt</span>
                <span class="dval">{{ formatDateTime(j.attempted_at) }}</span>
              </template>
              <template v-if="j.finalized_at">
                <span class="dkey">Finalized</span>
                <span class="dval">{{ formatDateTime(j.finalized_at) }}</span>
              </template>
            </div>
            <div v-if="j.args && j.args !== '{}'" class="detail-block">
              <span class="dkey">Args</span>
              <pre class="json-block">{{ formatArgs(j.args) }}</pre>
            </div>
            <div v-if="j.errors" class="detail-block">
              <span class="dkey">Errors</span>
              <pre class="err-block">{{ j.errors }}</pre>
            </div>
          </div>
        </div>
      </div>

      <div v-if="total > limit" class="pager">
        <button class="sv2-btn ghost" :disabled="offset === 0" @click="offset -= limit; fetchJobs()">Previous</button>
        <span class="page-info">{{ startIdx }}–{{ endIdx }} of {{ total }}</span>
        <button class="sv2-btn ghost" :disabled="offset + limit >= total" @click="offset += limit; fetchJobs()">Next</button>
      </div>
    </SettingsSection>

    <AppDialog
      v-model="workerSettingsOpen"
      title="Worker settings"
      description="Queue concurrency is loaded when workers start. Saved changes apply after restarting Heya."
      size="xl"
    >
      <div v-if="!workerSettings" class="worker-loading">
        <Icon name="spinner" :size="14" /> Loading worker settings
      </div>
      <div v-else-if="workerSettingsError" class="empty-state compact warn">
        <Icon name="warning" :size="14" /> {{ workerSettingsError }}
      </div>
      <div v-else-if="displayWorkerGroups.length === 0" class="empty-state compact">
        <Icon name="info" :size="14" /> No worker queues returned by the API.
      </div>
      <div v-else class="worker-settings">
        <div class="worker-toolbar">
          <div class="worker-summary">
            <span>{{ workerSettings.workers.length }} queues</span>
            <span>Changes apply after restart</span>
          </div>
          <div class="worker-presets" aria-label="Worker profiles">
            <button
              v-for="preset in WORKER_PRESETS"
              :key="preset.id"
              class="worker-preset"
              type="button"
              :title="preset.title"
              @click="applyWorkerPreset(preset.id)"
            >
              {{ preset.label }}
            </button>
          </div>
        </div>

        <section v-for="group in displayWorkerGroups" :key="group.title" class="worker-group">
          <header class="worker-group-head">
            <span class="worker-group-title">{{ group.title }}</span>
            <span class="worker-group-count">{{ group.workers.length }}</span>
          </header>
          <div class="worker-list">
            <label v-for="w in group.workers" :key="w.kind" class="worker-row" :class="{ locked: w.locked }">
              <span class="worker-main">
                <span class="worker-label">{{ w.label }}</span>
                <span class="worker-kind mono">{{ w.kind }}</span>
              </span>
              <span class="worker-meta">
                <span v-if="w.source === 'env'" class="worker-source" :title="`Locked by ${w.env_var}`">env</span>
                <span v-else-if="w.source === 'db'" class="worker-source db">db</span>
                <span class="worker-default mono">default {{ w.default }}</span>
              </span>
              <input
                v-model.number="workerDraft[w.kind]"
                class="worker-input"
                type="number"
                min="1"
                max="64"
                :disabled="w.locked"
              />
            </label>
          </div>
        </section>
      </div>

      <template #footer="{ close }">
        <button class="sv2-btn ghost" @click="close()">Cancel</button>
        <button class="sv2-btn primary" :disabled="workerSaving || !workerSettings" @click="saveWorkerSettings(close)">
          <Icon :name="workerSaving ? 'spinner' : 'check'" :size="12" />
          {{ workerSaving ? 'Saving…' : 'Save worker settings' }}
        </button>
      </template>
    </AppDialog>

    <SettingsFlash :flash="flash" />
  </div>
</template>

<style scoped>
.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 8px;
  margin-bottom: 28px;
}

.filter-row {
  display: flex; flex-wrap: wrap; gap: 6px;
  margin-bottom: 14px;
}
/* Leading label that marks each pill row as a filter control. */
.filter-group-label {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--fg-3);
  margin-right: 4px;
}
/* Kind pills sit in their own row just below the state pills. Snake_case
   kind names must not be capitalised the way state labels are. */
.filter-row.kinds { margin-top: -4px; }
.filter-pill.kind { text-transform: none; }
.filter-pill {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 5px 12px; border-radius: 999px;
  font-size: 11px; font-family: var(--font-mono);
  background: var(--bg-2); border: 1px solid var(--border);
  color: var(--fg-2); cursor: pointer;
  text-transform: capitalize;
  transition: border-color 0.12s, color 0.12s, background 0.12s;
}
.filter-pill:hover { border-color: var(--border-strong); color: var(--fg-1); }
.filter-pill.active { border-color: var(--gold); color: var(--gold); background: var(--gold-soft); }
.filter-count { font-weight: 700; }
.filter-pill.running   .filter-count { color: var(--good); }
.filter-pill.available .filter-count { color: var(--gold); }
.filter-pill.discarded .filter-count,
.filter-pill.cancelled .filter-count { color: var(--bad); }
.filter-pill.completed .filter-count { color: var(--fg-3); }
.filter-pill.clear { font-size: 10px; gap: 4px; color: var(--fg-3); text-transform: none; }

.job-table {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
}
.thead, .job-row {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr) 90px 70px 100px 64px;
  gap: 8px;
  align-items: center;
  padding: 8px 14px;
  font-size: 12px;
}
.thead {
  background: var(--bg-1);
  font-size: 10px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em;
  color: var(--fg-3);
  border-bottom: 1px solid var(--border);
  padding: 9px 14px;
}
.job-row { border-bottom: 1px solid var(--border); cursor: pointer; color: var(--fg-1); }
.job-row:last-child { border-bottom: 0; }
.job-row:hover, .job-row.expanded { background: rgba(255,255,255,0.02); }

.col-state { text-transform: capitalize; font-weight: 500; display: flex; align-items: center; }
.state-dot { display: inline-block; width: 6px; height: 6px; border-radius: 50%; margin-right: 7px; }
.state-dot.running { background: var(--good); }
.state-dot.available, .state-dot.retryable, .state-dot.scheduled { background: var(--gold); }
.state-dot.completed { background: var(--fg-4); }
.state-dot.discarded, .state-dot.cancelled { background: var(--bad); }

.mono { font-family: var(--font-mono); font-size: 11px; }
.dim  { color: var(--fg-3); }
.col-kind { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.col-actions { display: flex; gap: 4px; justify-content: flex-end; }
.row-btn {
  width: 24px; height: 24px;
  border-radius: var(--r-xs);
  display: inline-flex; align-items: center; justify-content: center;
  color: var(--fg-3); border: 1px solid transparent;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.row-btn:hover { color: var(--fg-0); background: rgba(255,255,255,0.05); border-color: var(--border); }
.row-btn.danger:hover { color: var(--bad); background: rgba(217,107,107,0.08); border-color: rgba(217,107,107,0.25); }

.detail {
  grid-column: 1 / -1;
  padding: 12px 0 8px;
  border-top: 1px solid var(--border);
  margin-top: 6px;
  cursor: default;
}
.detail-grid {
  display: grid; grid-template-columns: 110px 1fr; gap: 4px 14px;
  font-size: 12px; margin-bottom: 10px;
}
.dkey { color: var(--fg-3); font-family: var(--font-mono); font-size: 10px; text-transform: uppercase; letter-spacing: 0.06em; padding-top: 2px; }
.dval { color: var(--fg-1); }
.dval.mono { font-family: var(--font-mono); }

.detail-block { margin-top: 8px; }
.json-block {
  font-family: var(--font-mono); font-size: 11px; color: var(--fg-2);
  background: var(--bg-0); border: 1px solid var(--border);
  border-radius: var(--r-sm);
  padding: 8px 12px; margin: 4px 0 0;
  overflow-x: auto; white-space: pre;
}
.err-block {
  font-family: var(--font-mono); font-size: 11px; color: var(--bad);
  background: rgba(217,107,107,0.06); border: 1px solid rgba(217,107,107,0.15);
  border-radius: var(--r-sm);
  padding: 8px 12px; margin: 4px 0 0;
  overflow-x: auto; white-space: pre-wrap;
}

.pager {
  display: flex; align-items: center; justify-content: center;
  gap: 12px; margin-top: 14px;
}
.page-info { font-size: 11px; color: var(--fg-3); font-family: var(--font-mono); }

.sv2-btn {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 6px 12px;
  border-radius: var(--r-sm);
  font-size: 11.5px;
  font-weight: 500;
  cursor: pointer;
  transition: border-color 0.12s, color 0.12s, background 0.12s;
}

.worker-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--fg-2);
  font-size: 13px;
}

.worker-settings {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.worker-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.worker-summary {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  color: var(--fg-3);
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  font-family: var(--font-mono);
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}

.worker-presets {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 3px;
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}

.worker-preset {
  height: 28px;
  padding: 0 10px;
  color: var(--fg-2);
  border: 1px solid transparent;
  border-radius: var(--r-xs);
  font-size: 11px;
  font-weight: 600;
  cursor: pointer;
  transition: color 0.12s, background 0.12s, border-color 0.12s;
}

.worker-preset:hover {
  color: var(--fg-0);
  background: var(--bg-2);
  border-color: var(--border);
}

.worker-preset:nth-child(2) {
  color: var(--gold);
}

.worker-group {
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
}

.worker-group-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 11px;
  background: var(--bg-1);
  border-bottom: 1px solid var(--border);
}

.worker-group-title {
  color: var(--fg-3);
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.worker-group-count {
  min-width: 22px;
  height: 18px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-2);
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: 999px;
  font-family: var(--font-mono);
  font-size: 10px;
}

.worker-list {
  display: flex;
  flex-direction: column;
}

.worker-row {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) minmax(130px, auto) 72px;
  gap: 12px;
  align-items: center;
  min-height: 42px;
  padding: 7px 10px 7px 11px;
  border-bottom: 1px solid var(--border);
}

.worker-row:last-child {
  border-bottom: 0;
}

.worker-row:hover {
  background: rgba(255, 255, 255, 0.025);
}

.worker-row.locked {
  opacity: 0.66;
}

.worker-main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.worker-label {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--fg-0);
  font-size: 12px;
  font-weight: 500;
}

.worker-kind {
  color: var(--fg-4);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.worker-meta {
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  min-width: 0;
  color: var(--fg-4);
}

.worker-source {
  display: inline-flex;
  align-items: center;
  height: 18px;
  padding: 0 6px;
  color: var(--gold);
  background: var(--gold-soft);
  border: 1px solid rgba(230, 185, 74, 0.26);
  border-radius: 999px;
  font-family: var(--font-mono);
  font-size: 9px;
  text-transform: uppercase;
}

.worker-source.db {
  color: var(--good);
  background: rgba(111, 191, 124, 0.09);
  border-color: rgba(111, 191, 124, 0.22);
}

.worker-input {
  width: 72px;
  height: 30px;
  padding: 0 7px;
  color: var(--fg-0);
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-xs);
  font-family: var(--font-mono);
  font-size: 12px;
}

.worker-default {
  color: var(--fg-4);
  font-size: 10px;
  white-space: nowrap;
}

/* Phone: the 6-column grid (state/kind/queue/attempt/time/actions) can't
   fit 390px. Pure-CSS regrid to a 3-row card via grid-template-areas — no
   markup change, same spans just reassigned areas: primary line is kind +
   status, secondary line is the queue/attempt/time meta, actions trail on
   their own row. A plain flex-wrap reflow was tried first, but forcing a
   line break per row (flex-basis:100%) also forces that item to *fill*
   the line, bumping later siblings to their own line each — grid areas
   don't have that problem, and an empty actions area (nothing to
   retry/cancel) collapses to just the row-gap instead of a dead 44px line.
   The header row is meaningless once columns aren't aligned, so hide it. */
@media (max-width: 720px) {
  .thead { display: none; }
  .job-row {
    display: grid;
    grid-template-columns: 1fr auto auto;
    grid-template-areas:
      "kind    kind    state"
      "queue   attempt time"
      "actions actions actions";
    column-gap: 10px;
    row-gap: 4px;
    align-items: center;
    padding: 12px 14px;
    min-height: 44px;
  }
  .col-kind {
    grid-area: kind;
    min-width: 0;
    font-size: 13px;
    font-weight: 600;
    color: var(--fg-0);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .col-state { grid-area: state; justify-self: end; }
  .col-queue { grid-area: queue; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .col-attempt { grid-area: attempt; justify-self: end; }
  .col-time { grid-area: time; justify-self: end; }
  .col-actions { grid-area: actions; justify-content: flex-end; }
  .detail { grid-column: 1 / -1; }
  .row-btn { width: 44px; height: 44px; }
  .detail-grid { grid-template-columns: 100px 1fr; }

  .worker-summary {
    align-items: flex-start;
    gap: 4px;
    flex-wrap: wrap;
  }
  .worker-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .worker-presets {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
  .worker-preset {
    padding: 0 6px;
  }
  .worker-row {
    grid-template-columns: minmax(0, 1fr) 72px;
    grid-template-areas:
      "main input"
      "meta input";
    gap: 3px 10px;
    padding: 10px 11px;
  }
  .worker-main { grid-area: main; }
  .worker-meta {
    grid-area: meta;
    justify-content: flex-start;
  }
  .worker-input {
    grid-area: input;
    justify-self: end;
  }
}
</style>
