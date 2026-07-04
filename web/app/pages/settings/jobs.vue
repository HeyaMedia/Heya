<script setup lang="ts">
import { timeAgo as timeAgoBase } from '~/composables/useFormat'
definePageMeta({ layout: 'settings', middleware: 'admin' })

import type { components } from '#open-fetch-schemas/heya'
type JobRow = components['schemas']['JobRow']
type SummaryRow = components['schemas']['JobSummaryRow']

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
const tick = ref(0)
setInterval(() => { tick.value++ }, 1000)

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

</style>
