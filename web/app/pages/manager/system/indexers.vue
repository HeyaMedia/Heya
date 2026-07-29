<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { managerIndexersQuery, type ManagerIndexerHistoryDay, type ManagerIndexerHistoryView, type ManagerIndexerInput, type ManagerIndexerStatsView, type ManagerIndexerView, type ManagerTestResult } from '~/queries/manager'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

const indexersData = useQuery(managerIndexersQuery())
const indexers = computed(() => indexersData.data.value ?? [])
const loading = computed(() => indexersData.isLoading.value)

// Another admin (or the CLI) mutating indexers shows up live.
useLiveRefresh([{
  events: ['manager.changed'],
  keys: [['manager', 'indexers']],
  filter: event => (event.payload as { area?: string } | undefined)?.area === 'indexers',
}])

const prowlarrApps = computed(() => indexers.value.filter(ix => ix.kind === 'prowlarr'))
const manualIndexers = computed(() => indexers.value.filter(ix => ix.kind !== 'prowlarr'))

// ── Live stats (Prowlarr owns the history; we fetch, never persist) ──────

const statsByApp = ref<Record<number, ManagerIndexerStatsView[]>>({})
const statsError = ref('')

const historyByApp = ref<Record<number, ManagerIndexerHistoryView>>({})

async function loadStats() {
  for (const app of prowlarrApps.value) {
    if (app.last_test_at && !app.last_test_ok) continue
    try {
      statsByApp.value[app.id] = await $heya(`/api/manager/indexers/${app.id}/stats`) as ManagerIndexerStatsView[]
      statsError.value = ''
    } catch (e: any) {
      statsError.value = e?.data?.detail ?? e?.message ?? 'Stats unavailable.'
    }
    try {
      historyByApp.value[app.id] = await $heya(`/api/manager/indexers/${app.id}/history`) as ManagerIndexerHistoryView
    } catch (e: any) {
      statsError.value = e?.data?.detail ?? e?.message ?? 'History unavailable.'
    }
  }
}

watch(prowlarrApps, () => { loadStats() }, { immediate: true })

// Chart feeds: overall per app, and per synced child for the stats dialog.
function appChartDays(appID: number): { date: string, ok: number, bad: number }[] {
  return (historyByApp.value[appID]?.days ?? []).map(day => ({ date: day.date, ok: day.queries, bad: day.failed }))
}
function appGrabDays(appID: number): { date: string, ok: number }[] {
  return (historyByApp.value[appID]?.days ?? []).map(day => ({ date: day.date, ok: day.grabs }))
}
function appSources(appID: number): { name: string, count: number }[] {
  const sources = historyByApp.value[appID]?.by_source ?? {}
  return Object.entries(sources)
    .map(([name, count]) => ({ name, count: count as number }))
    .sort((a, b) => b.count - a.count)
}

const dialogHistory = computed<ManagerIndexerHistoryDay[]>(() => {
  const child = statsDialogChild.value
  if (!child || child.parent_id == null) return []
  return historyByApp.value[child.parent_id]?.by_indexer?.[child.id] ?? []
})
const dialogQueryDays = computed(() => dialogHistory.value.map(day => ({ date: day.date, ok: day.queries, bad: day.failed })))
const dialogGrabDays = computed(() => dialogHistory.value.map(day => ({ date: day.date, ok: day.grabs })))
const dialogHasActivity = computed(() => dialogHistory.value.some(day => day.queries + day.failed + day.grabs > 0))

// Stats rows keyed by the Heya child-row id, for the children table.
const statsByChildID = computed(() => {
  const map = new Map<number, ManagerIndexerStatsView>()
  for (const rows of Object.values(statsByApp.value)) {
    for (const row of rows) {
      if (row.id) map.set(row.id, row)
    }
  }
  return map
})

function failCount(stat?: ManagerIndexerStatsView): number {
  if (!stat) return 0
  return (stat.failed_queries ?? 0) + (stat.failed_rss ?? 0) + (stat.failed_grabs ?? 0)
}

// ── Per-indexer detail dialog ────────────────────────────────────────────

const statsDialogOpen = ref(false)
const statsDialogChild = ref<ManagerIndexerView | null>(null)
const statsDialogStat = computed(() =>
  statsDialogChild.value ? statsByChildID.value.get(statsDialogChild.value.id) : undefined)

function openStats(child: ManagerIndexerView) {
  statsDialogChild.value = child
  statsDialogOpen.value = true
}

const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)
const busyID = ref<number | null>(null)

// ── Add / edit dialog ────────────────────────────────────────────────────

const dialogOpen = ref(false)
const editingID = ref<number | null>(null)
const form = reactive({
  name: '',
  kind: 'prowlarr',
  base_url: '',
  api_key: '',
  protocol: 'usenet',
  hasStoredKey: false,
})
const savingForm = ref(false)
const formError = ref('')

const KIND_OPTIONS = [
  { value: 'prowlarr', label: 'Prowlarr (app connection)' },
  { value: 'torznab', label: 'Torznab endpoint' },
  { value: 'newznab', label: 'Newznab endpoint' },
]
const PROTOCOL_OPTIONS = [
  { value: 'usenet', label: 'Usenet' },
  { value: 'torrent', label: 'Torrent' },
]

function openAdd() {
  editingID.value = null
  form.name = ''
  form.kind = 'prowlarr'
  form.base_url = ''
  form.api_key = ''
  form.protocol = 'usenet'
  form.hasStoredKey = false
  formError.value = ''
  dialogOpen.value = true
}

function openEdit(ix: ManagerIndexerView) {
  editingID.value = ix.id
  form.name = ix.name
  form.kind = ix.kind
  form.base_url = ix.base_url
  form.api_key = ''
  form.protocol = ix.protocol || 'usenet'
  form.hasStoredKey = ix.api_key_set
  formError.value = ''
  dialogOpen.value = true
}

async function saveForm() {
  savingForm.value = true
  formError.value = ''
  const body: ManagerIndexerInput = {
    name: form.name,
    kind: form.kind,
    base_url: form.base_url,
    api_key: form.api_key,
    protocol: form.kind === 'prowlarr' ? '' : form.protocol,
    categories: [],
  }
  try {
    let saved: ManagerIndexerView
    if (editingID.value != null) {
      saved = await $heya(`/api/manager/indexers/${editingID.value}`, { method: 'PUT', body })
    } else {
      saved = await $heya('/api/manager/indexers', { method: 'POST', body })
    }
    dialogOpen.value = false
    await indexersData.refetch()
    await testIndexer(saved)
  } catch (e: any) {
    formError.value = e?.data?.detail ?? e?.message ?? 'Save failed.'
  } finally {
    savingForm.value = false
  }
}

// ── Row actions ──────────────────────────────────────────────────────────

async function testIndexer(ix: ManagerIndexerView) {
  busyID.value = ix.id
  flash.value = null
  try {
    const result = await $heya(`/api/manager/indexers/${ix.id}/test`, { method: 'POST' }) as ManagerTestResult
    flash.value = result.ok
      ? { kind: 'ok', text: `${ix.name}: ${result.detail}` }
      : { kind: 'err', text: `${ix.name}: ${result.error}` }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Test failed.' }
  } finally {
    busyID.value = null
    await indexersData.refetch()
    await loadStats()
  }
}

async function removeIndexer(ix: ManagerIndexerView) {
  const label = ix.kind === 'prowlarr' ? `${ix.name} and its ${ix.children?.length ?? 0} synced indexers` : ix.name
  const ok = await confirm({
    title: 'Remove indexer',
    message: `Remove ${label}? Searching stops using it immediately.`,
    destructive: true,
  })
  if (!ok) return
  try {
    await $heya(`/api/manager/indexers/${ix.id}`, { method: 'DELETE' })
    await indexersData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Delete failed.' }
  }
}

async function toggleChild(child: ManagerIndexerView, enabled: boolean) {
  try {
    await $heya(`/api/manager/indexers/${child.id}`, {
      method: 'PUT',
      body: { name: child.name, kind: child.kind, base_url: child.base_url, api_key: '', protocol: child.protocol, categories: child.categories ?? [], enabled },
    })
    await indexersData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Update failed.' }
    await indexersData.refetch()
  }
}

function testStateOf(ix: ManagerIndexerView): { state: 'ok' | 'warn' | 'error' | 'idle', label: string } {
  if (!ix.last_test_at) return { state: 'idle', label: 'untested' }
  if (ix.last_test_ok) return { state: 'ok', label: 'connected' }
  return { state: 'error', label: ix.last_test_error || 'failed' }
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Indexers"
      icon="search"
      eyebrow="Manager · System"
      description="Release sources. Connect Prowlarr once and its indexers sync automatically, or add Torznab/Newznab endpoints directly."
      tone="connected"
    />

    <div v-if="flash" class="mgr-flash" :class="flash.kind">
      <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" /> {{ flash.text }}
    </div>

    <div v-if="loading && !indexers.length" class="mgr-loading">
      <Icon name="spinner" :size="16" /> Loading…
    </div>

    <template v-else>
      <SettingsSection
        title="Prowlarr"
        icon="link"
        description="One connection, every indexer. Heya syncs the indexer list on every successful test and searches through Prowlarr's per-indexer Torznab endpoints."
      >
        <template #actions>
          <button type="button" :class="prowlarrApps.length ? 'mgr-btn' : 'mgr-btn-gold'" @click="openAdd">
            <Icon name="plus" :size="14" /> {{ prowlarrApps.length ? 'Add connection' : 'Connect Prowlarr' }}
          </button>
        </template>

        <div v-if="!prowlarrApps.length" class="mgr-empty">
          <Icon name="info" :size="14" /> No Prowlarr connection yet.
        </div>

        <div v-for="app in prowlarrApps" :key="app.id" class="px-card">
          <div class="px-icon" :class="{ err: app.last_test_at && !app.last_test_ok }"><Icon name="link" :size="16" /></div>
          <div class="px-body">
            <div class="px-name">
              {{ app.name }}
              <StatusBadge :state="testStateOf(app).state">{{ testStateOf(app).label }}</StatusBadge>
            </div>
            <div class="px-host">{{ app.base_url }} · {{ app.children?.length ?? 0 }} indexers synced</div>
          </div>
          <div class="mgr-row-actions">
            <AppTooltip label="Test & sync">
              <button type="button" class="mgr-btn-icon" :disabled="busyID === app.id" @click="testIndexer(app)">
                <Icon :name="busyID === app.id ? 'spinner' : 'refresh'" :size="14" />
              </button>
            </AppTooltip>
            <AppTooltip label="Edit">
              <button type="button" class="mgr-btn-icon" @click="openEdit(app)"><Icon name="pencil" :size="14" /></button>
            </AppTooltip>
            <AppTooltip label="Remove">
              <button type="button" class="mgr-btn-icon danger" @click="removeIndexer(app)"><Icon name="trash" :size="14" /></button>
            </AppTooltip>
          </div>
        </div>

        <div v-if="statsError" class="mgr-flash err">
          <Icon name="warning" :size="13" /> {{ statsError }}
        </div>

        <div v-for="app in prowlarrApps" :key="`activity-${app.id}`">
          <div v-if="appChartDays(app.id).length" class="ix-activity">
            <ManagerActivityChart :days="appChartDays(app.id)" title="Searches / day" unit="searches" />
            <ManagerActivityChart :days="appGrabDays(app.id)" title="Grabs / day" unit="grabs" />
          </div>
          <div v-if="appSources(app.id).length" class="ix-sources">
            <span class="ix-sources-label">searched by</span>
            <span v-for="source in appSources(app.id)" :key="source.name" class="ix-source-chip">
              {{ source.name }} <strong>{{ source.count.toLocaleString() }}</strong>
            </span>
          </div>
        </div>

        <div v-for="app in prowlarrApps" :key="`children-${app.id}`" class="ix-table">
          <div class="ix-head">
            <span>Indexer</span>
            <span>Protocol</span>
            <span>Priority</span>
            <span class="num">Grabs</span>
            <span class="num">Queries</span>
            <span class="num">Fails</span>
            <span class="num">Avg</span>
            <span>Enabled</span>
          </div>
          <div
            v-for="child in app.children ?? []"
            :key="child.id"
            class="ix-row clickable"
            :class="{ dim: !child.enabled }"
            role="button"
            tabindex="0"
            :aria-label="`${child.name} stats`"
            @click="openStats(child)"
            @keydown.enter="openStats(child)"
            @keydown.space.prevent="openStats(child)"
          >
            <span class="ix-name">
              {{ child.name }}
              <StatusBadge v-if="statsByChildID.get(child.id)?.disabled_till" state="warn">backoff</StatusBadge>
            </span>
            <span class="ix-proto" :class="child.protocol">{{ child.protocol }}</span>
            <span class="ix-prio">{{ child.priority }}</span>
            <span class="ix-stat">{{ statsByChildID.get(child.id)?.grabs ?? '—' }}</span>
            <span class="ix-stat">{{ statsByChildID.get(child.id) ? (statsByChildID.get(child.id)!.queries + statsByChildID.get(child.id)!.rss_queries).toLocaleString() : '—' }}</span>
            <span class="ix-stat" :class="{ bad: failCount(statsByChildID.get(child.id)) > 0 }">{{ statsByChildID.get(child.id) ? failCount(statsByChildID.get(child.id)) : '—' }}</span>
            <span class="ix-stat">{{ statsByChildID.get(child.id) ? `${statsByChildID.get(child.id)!.avg_response_ms} ms` : '—' }}</span>
            <span @click.stop>
              <AppSwitch :model-value="child.enabled" :aria-label="`Enable ${child.name}`" @update:model-value="(v: boolean) => toggleChild(child, v)" />
            </span>
          </div>
        </div>
      </SettingsSection>

      <SettingsSection
        title="Direct endpoints"
        icon="search"
        description="Hand-added Torznab/Newznab endpoints — for indexers you don't route through Prowlarr."
      >
        <template #actions>
          <button type="button" class="mgr-btn" @click="openAdd"><Icon name="plus" :size="14" /> Add endpoint</button>
        </template>
        <div v-if="!manualIndexers.length" class="mgr-empty">
          <Icon name="info" :size="14" /> None — everything goes through Prowlarr.
        </div>
        <div v-else class="ix-table">
          <div class="ix-head">
            <span>Indexer</span>
            <span>Protocol</span>
            <span>Priority</span>
            <span>Health</span>
          </div>
          <div v-for="ix in manualIndexers" :key="ix.id" class="ix-row" :class="{ dim: !ix.enabled }">
            <span class="ix-name">{{ ix.name }}</span>
            <span class="ix-proto" :class="ix.protocol">{{ ix.protocol }}</span>
            <span class="ix-prio">{{ ix.priority }}</span>
            <div class="mgr-row-actions">
              <StatusBadge :state="testStateOf(ix).state">{{ testStateOf(ix).label }}</StatusBadge>
              <AppTooltip label="Test">
                <button type="button" class="mgr-btn-icon" :disabled="busyID === ix.id" @click="testIndexer(ix)">
                  <Icon :name="busyID === ix.id ? 'spinner' : 'refresh'" :size="14" />
                </button>
              </AppTooltip>
              <AppTooltip label="Edit">
                <button type="button" class="mgr-btn-icon" @click="openEdit(ix)"><Icon name="pencil" :size="14" /></button>
              </AppTooltip>
              <AppTooltip label="Remove">
                <button type="button" class="mgr-btn-icon danger" @click="removeIndexer(ix)"><Icon name="trash" :size="14" /></button>
              </AppTooltip>
            </div>
          </div>
        </div>
      </SettingsSection>
    </template>

    <AppDialog v-model="statsDialogOpen" :title="statsDialogChild?.name ?? 'Indexer'" size="sm">
      <div v-if="statsDialogChild" class="ixs-detail">
        <div class="ixs-url">{{ statsDialogChild.base_url }}</div>
        <div v-if="statsDialogStat" class="ixs-grid">
          <div class="ixs-cell"><span class="ixs-label">Grabs</span><span class="ixs-value">{{ statsDialogStat.grabs.toLocaleString() }}</span></div>
          <div class="ixs-cell"><span class="ixs-label">Searches</span><span class="ixs-value">{{ statsDialogStat.queries.toLocaleString() }}</span></div>
          <div class="ixs-cell"><span class="ixs-label">RSS queries</span><span class="ixs-value">{{ statsDialogStat.rss_queries.toLocaleString() }}</span></div>
          <div class="ixs-cell"><span class="ixs-label">Avg response</span><span class="ixs-value">{{ statsDialogStat.avg_response_ms }} ms</span></div>
          <div class="ixs-cell"><span class="ixs-label">Failed searches</span><span class="ixs-value" :class="{ bad: statsDialogStat.failed_queries > 0 }">{{ statsDialogStat.failed_queries }}</span></div>
          <div class="ixs-cell"><span class="ixs-label">Failed RSS</span><span class="ixs-value" :class="{ bad: statsDialogStat.failed_rss > 0 }">{{ statsDialogStat.failed_rss }}</span></div>
          <div class="ixs-cell"><span class="ixs-label">Failed grabs</span><span class="ixs-value" :class="{ bad: statsDialogStat.failed_grabs > 0 }">{{ statsDialogStat.failed_grabs }}</span></div>
          <div class="ixs-cell"><span class="ixs-label">Health</span><span class="ixs-value">{{ statsDialogStat.disabled_till ? `backoff until ${statsDialogStat.disabled_till}` : 'healthy' }}</span></div>
        </div>
        <div v-else class="ixs-none">No stats yet — Prowlarr reports counters once an indexer has been queried.</div>
        <div v-if="dialogHasActivity" class="ixs-charts">
          <ManagerActivityChart :days="dialogQueryDays" title="Searches / day" unit="searches" :height="64" />
          <ManagerActivityChart :days="dialogGrabDays" title="Grabs / day" unit="grabs" :height="64" />
        </div>
        <p v-if="statsDialogStat?.recent_failure" class="mgr-form-error">
          <Icon name="warning" :size="12" /> Last failure: {{ statsDialogStat.recent_failure }}
        </p>
      </div>
    </AppDialog>

    <AppDialog v-model="dialogOpen" :title="editingID != null ? 'Edit indexer' : 'Add indexer'" size="sm">
      <div class="mgr-form">
        <label class="mgr-field">
          <span>Name</span>
          <input v-model="form.name" class="mgr-input" placeholder="Prowlarr">
        </label>
        <label v-if="editingID == null" class="mgr-field">
          <span>Kind</span>
          <AppSelect v-model="form.kind" :options="KIND_OPTIONS" />
        </label>
        <label class="mgr-field">
          <span>URL</span>
          <input v-model="form.base_url" class="mgr-input mono" :placeholder="form.kind === 'prowlarr' ? 'https://prowlarr.example.ts.net' : 'https://indexer.example/api'">
        </label>
        <label class="mgr-field">
          <span>API key</span>
          <input v-model="form.api_key" class="mgr-input mono" type="password" :placeholder="form.hasStoredKey ? '•••••••• (unchanged)' : ''">
        </label>
        <label v-if="form.kind !== 'prowlarr'" class="mgr-field">
          <span>Protocol</span>
          <AppSelect v-model="form.protocol" :options="PROTOCOL_OPTIONS" />
        </label>
        <p v-if="formError" class="mgr-form-error"><Icon name="warning" :size="12" /> {{ formError }}</p>
      </div>
      <template #footer>
        <button type="button" class="mgr-btn" @click="dialogOpen = false">Cancel</button>
        <button type="button" class="mgr-btn-gold" :disabled="savingForm" @click="saveForm">
          <Icon v-if="savingForm" name="spinner" :size="13" />
          {{ editingID != null ? 'Save' : 'Add & test' }}
        </button>
      </template>
    </AppDialog>
  </div>
</template>

<style scoped>
.px-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  margin-bottom: 10px;
}
.px-icon {
  width: 36px; height: 36px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  display: flex; align-items: center; justify-content: center;
  color: var(--good);
  flex-shrink: 0;
}
.px-icon.err { color: var(--bad); }
.px-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.px-name {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 14px; font-weight: 500; color: var(--fg-0);
}
.px-host {
  font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-2);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

.ix-activity {
  display: grid;
  grid-template-columns: 2fr 1fr;
  gap: 20px;
  padding: 14px 16px 10px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  margin-bottom: 10px;
}
.ix-sources {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 12px;
}
.ix-sources-label {
  font-family: var(--font-mono);
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--fg-3);
  margin-right: 2px;
}
.ix-source-chip {
  display: inline-flex;
  align-items: baseline;
  gap: 5px;
  padding: 3px 9px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  font-size: 11px;
  color: var(--fg-2);
}
.ix-source-chip strong { font-family: var(--font-mono); font-size: 10.5px; color: var(--fg-0); font-weight: 600; }

@media (max-width: 720px) {
  .ix-activity { grid-template-columns: 1fr; }
}

.ix-table { display: flex; flex-direction: column; gap: 6px; }
.ix-head,
.ix-row {
  display: grid;
  grid-template-columns: minmax(150px, 1.6fr) 74px 56px 62px 76px 52px 64px minmax(52px, auto);
  gap: 12px;
  align-items: center;
}
.ix-head .num { text-align: right; }
.ix-row.clickable { cursor: pointer; transition: background 0.12s, border-color 0.12s; }
.ix-row.clickable:hover { background: rgb(var(--ink) / 0.04); border-color: var(--border-strong); }
.ix-row.clickable:focus-visible { outline: 2px solid var(--gold); outline-offset: 1px; }
.ix-stat {
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--fg-2);
  text-align: right;
  white-space: nowrap;
}
.ix-stat.bad { color: var(--bad); }
.ix-head {
  padding: 0 14px 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.ix-row {
  padding: 10px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.ix-row.dim .ix-name { color: var(--fg-3); }
.ix-name { font-size: 13px; font-weight: 500; color: var(--fg-0); }
.ix-proto {
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
}
.ix-proto.torrent { color: var(--gold); }
.ix-proto.usenet { color: var(--good); }
.ix-prio { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-2); }

.mgr-row-actions { display: flex; gap: 4px; align-items: center; justify-content: flex-end; flex-wrap: wrap; }

.mgr-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}

@media (max-width: 960px) {
  .ix-head { display: none; }
  .ix-row { grid-template-columns: minmax(0, 1fr) auto; row-gap: 6px; }
  .ix-prio, .ix-stat { display: none; }
}
</style>

<!-- Stats dialog content is portaled — unscoped. -->
<style>
.ixs-detail { display: flex; flex-direction: column; gap: 14px; }
.ixs-url {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ixs-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 8px;
}
.ixs-cell {
  display: flex;
  flex-direction: column;
  gap: 3px;
  padding: 10px 12px;
  background: rgb(var(--ink) / 0.04);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}
.ixs-label {
  font-family: var(--font-mono);
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.ixs-value { font-size: 15px; font-weight: 600; color: var(--fg-0); }
.ixs-value.bad { color: var(--bad); }
.ixs-none { font-size: 12.5px; color: var(--fg-3); }
.ixs-charts {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 12px;
  background: rgb(var(--ink) / 0.03);
  border: 1px solid var(--hair);
  border-radius: var(--r-sm);
}
</style>

<!-- Shared .mgr-form/.mgr-input dialog styles live unscoped in
     layouts/manager.vue (portaled content + per-page chunking). -->
