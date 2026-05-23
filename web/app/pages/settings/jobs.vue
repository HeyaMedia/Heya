<template>
  <div>
    <div class="page-header">
      <div>
        <h2 class="page-title">Job Queue</h2>
        <p class="page-desc">Monitor, retry, and manage queued background jobs</p>
      </div>
      <div class="header-actions">
        <button class="btn btn-secondary btn-sm" @click="rescueStuck" :disabled="rescuing">
          <Icon name="lightning" :size="13" />
          Rescue stuck
        </button>
        <button class="btn btn-secondary btn-sm" @click="clearCompleted" :disabled="clearing">
          <Icon name="trash" :size="13" />
          Clear completed
        </button>
        <button class="btn btn-sm btn-danger" @click="clearAll" :disabled="clearingAll">
          <Icon name="trash" :size="13" />
          Clear queue
        </button>
        <button class="btn btn-secondary btn-sm" @click="refresh">
          <Icon name="refresh" :size="13" />
          Refresh
        </button>
      </div>
    </div>

    <!-- Summary pills -->
    <div v-if="jobSummary.length" class="job-pills">
      <button
        v-for="s in jobSummary"
        :key="s.state"
        class="job-pill"
        :class="[s.state, { active: jobFilter === s.state }]"
        @click="jobFilter = jobFilter === s.state ? '' : s.state"
      >
        <span class="job-pill-count">{{ s.count }}</span>
        <span class="job-pill-label">{{ s.state }}</span>
      </button>
      <button v-if="jobFilter" class="job-pill clear-filter" @click="jobFilter = ''">
        <Icon name="close" :size="10" />
        Clear filter
      </button>
    </div>

    <!-- Job list -->
    <div v-if="jobs.length" class="job-table">
      <div class="job-table-head">
        <span class="col-state">State</span>
        <span class="col-kind">Kind</span>
        <span class="col-queue">Queue</span>
        <span class="col-attempt">Attempt</span>
        <span class="col-time">Created</span>
        <span class="col-actions">Actions</span>
      </div>
      <div v-for="j in jobs" :key="j.id" class="job-row" :class="{ expanded: expandedJob === j.id }" @click="expandedJob = expandedJob === j.id ? null : j.id">
        <span class="col-state">
          <span class="state-dot" :class="j.state" />
          {{ j.state }}
        </span>
        <span class="col-kind job-kind">{{ j.kind }}</span>
        <span class="col-queue job-queue">{{ j.queue }}</span>
        <span class="col-attempt">{{ j.attempt }}/{{ j.max_attempts }}</span>
        <span class="col-time job-time">{{ timeAgo(j.created_at) }}</span>
        <span class="col-actions" @click.stop>
          <button
            v-if="['discarded', 'cancelled', 'retryable'].includes(j.state)"
            class="action-btn-sm"
            @click="retryJob(j.id)"
            title="Retry"
          >
            <Icon name="refresh" :size="12" />
          </button>
          <button
            v-if="['available', 'retryable', 'scheduled'].includes(j.state)"
            class="action-btn-sm danger"
            @click="cancelJob(j.id)"
            title="Cancel"
          >
            <Icon name="close" :size="12" />
          </button>
        </span>

        <!-- Expanded details -->
        <div v-if="expandedJob === j.id" class="job-detail" @click.stop>
          <div class="detail-grid">
            <span class="detail-key">ID</span>
            <span class="detail-val mono">{{ j.id }}</span>
            <span class="detail-key">Created</span>
            <span class="detail-val">{{ formatDate(j.created_at) }}</span>
            <span v-if="j.attempted_at" class="detail-key">Last attempt</span>
            <span v-if="j.attempted_at" class="detail-val">{{ formatDate(j.attempted_at) }}</span>
            <span v-if="j.finalized_at" class="detail-key">Finalized</span>
            <span v-if="j.finalized_at" class="detail-val">{{ formatDate(j.finalized_at) }}</span>
          </div>
          <div v-if="j.args && j.args !== '{}'" class="detail-args">
            <span class="detail-key">Args</span>
            <pre class="args-json">{{ formatArgs(j.args) }}</pre>
          </div>
          <div v-if="j.errors" class="detail-errors">
            <span class="detail-key">Errors</span>
            <pre class="error-text">{{ j.errors }}</pre>
          </div>
        </div>
      </div>
    </div>
    <div v-else class="empty-hint">
      <Icon name="check" :size="14" />
      {{ jobFilter ? 'No jobs matching filter' : 'Job queue is empty' }}
    </div>

    <div v-if="jobTotal > jobs.length" class="pagination">
      <button class="btn btn-secondary btn-sm" :disabled="jobOffset === 0" @click="jobOffset -= 50; fetchJobs()">Previous</button>
      <span class="page-info">{{ jobOffset + 1 }}–{{ Math.min(jobOffset + 50, jobTotal) }} of {{ jobTotal }}</span>
      <button class="btn btn-secondary btn-sm" :disabled="jobOffset + 50 >= jobTotal" @click="jobOffset += 50; fetchJobs()">Next</button>
    </div>
  </div>
</template>

<script setup lang="ts">
interface JobRow {
  id: number
  state: string
  kind: string
  queue: string
  args: string
  attempt: number
  max_attempts: number
  created_at: string
  attempted_at?: string
  finalized_at?: string
  errors?: string
}

interface JobSummary { state: string; count: number }

const jobs = ref<JobRow[]>([])
const jobTotal = ref(0)
const jobSummary = ref<JobSummary[]>([])
const jobFilter = ref('')
const jobOffset = ref(0)
const clearing = ref(false)
const clearingAll = ref(false)
const rescuing = ref(false)
const expandedJob = ref<number | null>(null)

function timeAgo(dateStr: string) {
  const sec = Math.floor((Date.now() - new Date(dateStr).getTime()) / 1000)
  if (sec < 60) return `${sec}s ago`
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`
  return `${Math.floor(sec / 86400)}d ago`
}

function formatDate(d: string) {
  return new Date(d).toLocaleString('en-GB', { dateStyle: 'medium', timeStyle: 'medium' })
}

function formatArgs(raw: string) {
  try { return JSON.stringify(JSON.parse(raw), null, 2) } catch { return raw }
}

async function fetchJobs() {
  try {
    const params = new URLSearchParams({ limit: '50', offset: String(jobOffset.value) })
    if (jobFilter.value) params.set('state', jobFilter.value)
    const res = await apiFetch<{ jobs: JobRow[], total: number }>(`/api/jobs?${params}`)
    jobs.value = res.jobs
    jobTotal.value = res.total
  } catch {}
}

async function fetchJobSummary() {
  try { jobSummary.value = await apiFetch<JobSummary[]>('/api/jobs/summary') } catch {}
}

async function retryJob(id: number) {
  try { await apiFetch(`/api/jobs/${id}/retry`, { method: 'POST' }); refresh() } catch {}
}

async function cancelJob(id: number) {
  try { await apiFetch(`/api/jobs/${id}/cancel`, { method: 'POST' }); refresh() } catch {}
}

async function rescueStuck() {
  rescuing.value = true
  try { await apiFetch('/api/jobs/rescue', { method: 'POST' }); refresh() } catch {}
  rescuing.value = false
}

async function clearCompleted() {
  clearing.value = true
  try { await apiFetch('/api/jobs/completed', { method: 'DELETE' }); refresh() } catch {}
  clearing.value = false
}

async function clearAll() {
  if (!confirm('Delete ALL jobs including pending and running? This cannot be undone.')) return
  clearingAll.value = true
  try { await apiFetch('/api/jobs', { method: 'DELETE' }); refresh() } catch {}
  clearingAll.value = false
}

function refresh() { fetchJobs(); fetchJobSummary() }

let refreshTimer: ReturnType<typeof setTimeout> | null = null
function debouncedRefresh() {
  if (refreshTimer) clearTimeout(refreshTimer)
  refreshTimer = setTimeout(refresh, 500)
}

watch(jobFilter, () => { jobOffset.value = 0; fetchJobs() })

const { on } = useEventBus()

onMounted(() => {
  refresh()

  const unsubs = [
    on('queue.status', debouncedRefresh),
    on('scan.started', debouncedRefresh),
    on('scan.completed', debouncedRefresh),
  ]

  onUnmounted(() => {
    unsubs.forEach(fn => fn())
    if (refreshTimer) clearTimeout(refreshTimer)
  })
})
</script>

<style scoped>
.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 24px; }
.page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.page-desc { font-size: 13px; color: var(--fg-3); margin: 6px 0 0; }
.header-actions { display: flex; gap: 6px; }
.btn-sm { height: 34px; padding: 0 14px; font-size: 12px; }

/* Summary pills */
.job-pills { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 14px; }
.job-pill {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 5px 12px; border-radius: 100px;
  font-size: 11px; font-family: var(--font-mono);
  background: var(--bg-3); border: 1px solid var(--border);
  color: var(--fg-2); cursor: pointer; transition: all 0.12s ease;
  text-transform: capitalize;
}
.job-pill:hover { border-color: var(--border-strong); color: var(--fg-1); }
.job-pill.active { border-color: var(--gold); color: var(--gold); background: var(--gold-soft); }
.job-pill-count { font-weight: 700; }
.job-pill-label { font-weight: 500; }
.job-pill.running .job-pill-count { color: var(--good); }
.job-pill.available .job-pill-count { color: var(--gold); }
.job-pill.discarded .job-pill-count, .job-pill.cancelled .job-pill-count { color: var(--bad); }
.job-pill.completed .job-pill-count { color: var(--fg-3); }
.job-pill.clear-filter { font-size: 10px; gap: 4px; color: var(--fg-3); text-transform: none; }

/* Job table */
.job-table { background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md); overflow: hidden; }
.job-table-head, .job-row {
  display: grid;
  grid-template-columns: 100px 1fr 80px 70px 90px 70px;
  gap: 8px; padding: 8px 16px; font-size: 12px; align-items: center;
}
.job-table-head {
  font-size: 10px; font-weight: 600; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em;
  color: var(--fg-3); border-bottom: 1px solid var(--border); padding: 10px 16px;
}
.job-row { border-bottom: 1px solid var(--border); color: var(--fg-1); cursor: pointer; }
.job-row:last-child { border-bottom: none; }
.job-row:hover { background: rgba(255, 255, 255, 0.02); }
.job-row.expanded { background: rgba(255, 255, 255, 0.02); }

.state-dot { display: inline-block; width: 6px; height: 6px; border-radius: 50%; margin-right: 6px; }
.state-dot.running { background: var(--good); }
.state-dot.available, .state-dot.retryable, .state-dot.scheduled { background: var(--gold); }
.state-dot.completed { background: var(--fg-4); }
.state-dot.discarded, .state-dot.cancelled { background: var(--bad); }

.col-state { text-transform: capitalize; font-weight: 500; white-space: nowrap; }
.job-kind { font-family: var(--font-mono); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.job-queue { font-family: var(--font-mono); font-size: 11px; color: var(--fg-3); }
.col-attempt { font-family: var(--font-mono); font-size: 11px; color: var(--fg-2); }
.job-time { font-family: var(--font-mono); font-size: 11px; color: var(--fg-3); }

.action-btn-sm {
  width: 26px; height: 26px; border-radius: var(--r-xs);
  display: inline-flex; align-items: center; justify-content: center;
  color: var(--fg-3); border: 1px solid transparent; transition: all 0.12s ease;
}
.action-btn-sm:hover { color: var(--fg-0); background: rgba(255, 255, 255, 0.06); border-color: var(--border); }
.action-btn-sm.danger:hover { color: var(--bad); background: rgba(217, 107, 107, 0.08); border-color: rgba(217, 107, 107, 0.2); }

/* Expanded detail */
.job-detail {
  grid-column: 1 / -1;
  padding: 12px 0 8px;
  border-top: 1px solid var(--border);
  margin-top: 6px;
}

.detail-grid {
  display: grid; grid-template-columns: 100px 1fr; gap: 4px 12px;
  font-size: 12px; margin-bottom: 8px;
}
.detail-key { color: var(--fg-3); font-family: var(--font-mono); font-size: 10px; text-transform: uppercase; letter-spacing: 0.06em; padding-top: 2px; }
.detail-val { color: var(--fg-1); }
.detail-val.mono { font-family: var(--font-mono); }

.detail-args, .detail-errors { margin-top: 8px; }
.args-json {
  font-family: var(--font-mono); font-size: 11px; color: var(--fg-2);
  background: var(--bg-0); border: 1px solid var(--border); border-radius: var(--r-sm);
  padding: 8px 12px; margin: 4px 0 0; overflow-x: auto; white-space: pre;
}
.error-text {
  font-family: var(--font-mono); font-size: 11px; color: var(--bad);
  background: rgba(217, 107, 107, 0.06); border: 1px solid rgba(217, 107, 107, 0.15);
  border-radius: var(--r-sm); padding: 8px 12px; margin: 4px 0 0;
  overflow-x: auto; white-space: pre-wrap;
}

.pagination { display: flex; align-items: center; justify-content: center; gap: 12px; margin-top: 12px; }
.page-info { font-size: 11px; color: var(--fg-3); font-family: var(--font-mono); }

.empty-hint {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 13px;
  padding: 14px 16px; background: var(--bg-2);
  border: 1px dashed var(--border); border-radius: var(--r-md);
}
</style>
