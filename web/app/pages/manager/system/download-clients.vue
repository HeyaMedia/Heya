<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { managerDownloadClientsQuery, type ManagerClientActivityView, type ManagerDownloadClientInput, type ManagerDownloadClientView, type ManagerTestResult } from '~/queries/manager'
import { fmtBytes, timeAgoShort } from '~/composables/useFormat'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()

const clientsData = useQuery(managerDownloadClientsQuery())
const clients = computed(() => clientsData.data.value ?? [])
const loading = computed(() => clientsData.isLoading.value)

useLiveRefresh([{
  events: ['manager.changed'],
  keys: [['manager', 'download-clients']],
  filter: event => (event.payload as { area?: string } | undefined)?.area === 'download_clients',
}])

// ── Live activity (queue, history, transfer totals) ──────────────────────
// Polled straight off the client every 10s while the tab is visible —
// nothing is persisted server-side, so Colada caching buys little here.

const activityByClient = ref<Record<number, ManagerClientActivityView>>({})
const activityError = ref('')
let activityTimer: ReturnType<typeof setInterval> | null = null

async function loadActivity() {
  for (const client of clients.value) {
    if (!client.enabled || (client.last_test_at && !client.last_test_ok)) continue
    try {
      activityByClient.value[client.id] = await $heya(`/api/manager/download-clients/${client.id}/activity`) as ManagerClientActivityView
      activityError.value = ''
    } catch (e: any) {
      activityError.value = e?.data?.detail ?? e?.message ?? 'Activity unavailable.'
    }
  }
}

watch(clients, () => { loadActivity() }, { immediate: true })
onMounted(() => {
  activityTimer = setInterval(() => {
    if (document.visibilityState === 'hidden') return
    loadActivity()
  }, 10_000)
})
onBeforeUnmount(() => {
  if (activityTimer) clearInterval(activityTimer)
})

function clientState(activity?: ManagerClientActivityView): { label: string, state: 'ok' | 'warn' | 'idle' } {
  if (!activity) return { label: 'no data', state: 'idle' }
  if (activity.paused) return { label: 'paused', state: 'warn' }
  const active = activity.queue?.length ?? 0
  if (active > 0) return { label: `downloading ${active}`, state: 'ok' }
  return { label: 'idle', state: 'idle' }
}

function speedText(activity: ManagerClientActivityView): string {
  const kbps = activity.speed_kbps
  if (kbps >= 1024) return `${(kbps / 1024).toFixed(1)} MB/s`
  return `${kbps.toFixed(0)} KB/s`
}

function historyAge(completedAt: number): string {
  if (!completedAt) return ''
  return timeAgoShort(new Date(completedAt * 1000).toISOString())
}

const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)
const busyID = ref<number | null>(null)

// ── Add / edit dialog ────────────────────────────────────────────────────

const dialogOpen = ref(false)
const editingID = ref<number | null>(null)
const form = reactive({
  name: '',
  kind: 'sabnzbd',
  base_url: '',
  api_key: '',
  category: 'heya',
  map_remote: '',
  map_local: '',
  hasStoredKey: false,
})
const savingForm = ref(false)
const formError = ref('')

// One entry today; the selector is the pattern the torrent clients slot into.
const CLIENT_KIND_OPTIONS = [
  { value: 'sabnzbd', label: 'SABnzbd', meta: 'usenet' },
]

function openAdd() {
  editingID.value = null
  form.name = 'SABnzbd'
  form.kind = 'sabnzbd'
  form.base_url = ''
  form.api_key = ''
  form.category = 'heya'
  form.map_remote = ''
  form.map_local = ''
  form.hasStoredKey = false
  formError.value = ''
  dialogOpen.value = true
}

function openEdit(client: ManagerDownloadClientView) {
  editingID.value = client.id
  form.name = client.name
  form.kind = client.kind
  form.base_url = client.base_url
  form.api_key = ''
  form.category = client.category
  form.map_remote = client.path_mappings?.[0]?.remote ?? ''
  form.map_local = client.path_mappings?.[0]?.local ?? ''
  form.hasStoredKey = client.api_key_set
  formError.value = ''
  dialogOpen.value = true
}

async function saveForm() {
  savingForm.value = true
  formError.value = ''
  const body: ManagerDownloadClientInput = {
    name: form.name,
    kind: form.kind,
    base_url: form.base_url,
    api_key: form.api_key,
    username: '',
    password: '',
    category: form.category,
    path_mappings: form.map_remote && form.map_local
      ? [{ remote: form.map_remote, local: form.map_local }]
      : [],
  }
  try {
    let saved: ManagerDownloadClientView
    if (editingID.value != null) {
      saved = await $heya(`/api/manager/download-clients/${editingID.value}`, { method: 'PUT', body })
    } else {
      saved = await $heya('/api/manager/download-clients', { method: 'POST', body })
    }
    dialogOpen.value = false
    await clientsData.refetch()
    await testClient(saved)
  } catch (e: any) {
    formError.value = e?.data?.detail ?? e?.message ?? 'Save failed.'
  } finally {
    savingForm.value = false
  }
}

// ── Row actions ──────────────────────────────────────────────────────────

async function testClient(client: ManagerDownloadClientView) {
  busyID.value = client.id
  flash.value = null
  try {
    const result = await $heya(`/api/manager/download-clients/${client.id}/test`, { method: 'POST' }) as ManagerTestResult
    flash.value = result.ok
      ? { kind: 'ok', text: `${client.name}: ${result.detail}` }
      : { kind: 'err', text: `${client.name}: ${result.error}` }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Test failed.' }
  } finally {
    busyID.value = null
    await clientsData.refetch()
  }
}

async function removeClient(client: ManagerDownloadClientView) {
  const ok = await confirm({
    title: 'Remove download client',
    message: `Remove ${client.name}? Grabs can no longer be sent to it.`,
    destructive: true,
  })
  if (!ok) return
  try {
    await $heya(`/api/manager/download-clients/${client.id}`, { method: 'DELETE' })
    await clientsData.refetch()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Delete failed.' }
  }
}

function testStateOf(client: ManagerDownloadClientView): { state: 'ok' | 'warn' | 'error' | 'idle', label: string } {
  if (!client.last_test_at) return { state: 'idle', label: 'untested' }
  if (client.last_test_ok) return { state: 'ok', label: 'connected' }
  return { state: 'error', label: client.last_test_error || 'failed' }
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Download clients"
      icon="download"
      eyebrow="Manager · System"
      description="Where grabbed releases go. Completed downloads are picked up by the import pipeline, renamed into the library, and handed to the scanner."
      tone="connected"
    />

    <div v-if="flash" class="mgr-flash" :class="flash.kind">
      <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" /> {{ flash.text }}
    </div>

    <SettingsSection
      title="Connected clients"
      icon="download"
      description="SABnzbd first — torrent clients arrive with the torrent pipeline. Path mappings translate the client's filesystem view into this machine's (e.g. /storage → /Volumes/Storage in local dev)."
    >
      <template #actions>
        <button type="button" class="mgr-btn-gold" @click="openAdd"><Icon name="plus" :size="14" /> Add client</button>
      </template>

      <div v-if="loading && !clients.length" class="mgr-loading">
        <Icon name="spinner" :size="16" /> Loading…
      </div>
      <div v-else-if="!clients.length" class="mgr-empty">
        <Icon name="info" :size="14" /> No download clients configured yet.
      </div>

      <div v-if="activityError" class="mgr-flash err">
        <Icon name="warning" :size="13" /> {{ activityError }}
      </div>

      <div v-for="client in clients" :key="client.id" class="dc-card">
        <div class="dc-card-head">
          <div class="dc-icon" :class="{ err: client.last_test_at && !client.last_test_ok }"><Icon name="download" :size="16" /></div>
          <div class="dc-body">
            <div class="dc-name">
              {{ client.name }}
              <StatusBadge :state="testStateOf(client).state">{{ testStateOf(client).label }}</StatusBadge>
              <StatusBadge v-if="activityByClient[client.id]" :state="clientState(activityByClient[client.id]).state === 'idle' ? 'idle' : clientState(activityByClient[client.id]).state === 'warn' ? 'warn' : 'ok'">
                {{ clientState(activityByClient[client.id]).label }}
              </StatusBadge>
            </div>
            <div class="dc-host">{{ client.base_url }}</div>
            <div class="dc-meta">
              <span class="dc-proto" :class="client.protocol">{{ client.protocol }}</span>
              <span>· category <code>{{ client.category }}</code></span>
              <span v-for="mapping in client.path_mappings" :key="mapping.remote">
                · maps <code>{{ mapping.remote }}</code> → <code>{{ mapping.local }}</code>
              </span>
            </div>
          </div>
          <div class="mgr-row-actions">
            <AppTooltip label="Test connection">
              <button type="button" class="mgr-btn-icon" :disabled="busyID === client.id" @click="testClient(client)">
                <Icon :name="busyID === client.id ? 'spinner' : 'refresh'" :size="14" />
              </button>
            </AppTooltip>
            <AppTooltip label="Edit">
              <button type="button" class="mgr-btn-icon" @click="openEdit(client)"><Icon name="pencil" :size="14" /></button>
            </AppTooltip>
            <AppTooltip label="Remove">
              <button type="button" class="mgr-btn-icon danger" @click="removeClient(client)"><Icon name="trash" :size="14" /></button>
            </AppTooltip>
          </div>
        </div>

        <template v-if="activityByClient[client.id]">
          <div class="dc-activity">
            <div class="dc-stats">
              <div class="dc-stat">
                <span class="dc-stat-label">Speed</span>
                <span class="dc-stat-value">{{ speedText(activityByClient[client.id]!) }}</span>
              </div>
              <div class="dc-stat">
                <span class="dc-stat-label">Today</span>
                <span class="dc-stat-value">{{ fmtBytes(activityByClient[client.id]!.downloaded_day) }}</span>
              </div>
              <div class="dc-stat">
                <span class="dc-stat-label">This week</span>
                <span class="dc-stat-value">{{ fmtBytes(activityByClient[client.id]!.downloaded_week) }}</span>
              </div>
              <div class="dc-stat">
                <span class="dc-stat-label">This month</span>
                <span class="dc-stat-value">{{ fmtBytes(activityByClient[client.id]!.downloaded_month) }}</span>
              </div>
              <div class="dc-stat">
                <span class="dc-stat-label">All time</span>
                <span class="dc-stat-value">{{ fmtBytes(activityByClient[client.id]!.downloaded_total) }}</span>
              </div>
              <div class="dc-stat">
                <span class="dc-stat-label">Disk free</span>
                <span class="dc-stat-value">{{ fmtBytes(activityByClient[client.id]!.disk_free_gb * 1024 ** 3) }}</span>
              </div>
            </div>

            <div v-if="activityByClient[client.id]?.queue?.length" class="dc-queue">
              <div class="dc-sub-label">Queue</div>
              <div v-for="item in activityByClient[client.id]?.queue ?? []" :key="item.name" class="dc-queue-row">
                <div class="dc-queue-main">
                  <span class="dc-queue-name">{{ item.name }}</span>
                  <span class="dc-queue-meta">{{ item.category }} · {{ item.status }} · {{ item.time_left }}</span>
                </div>
                <div class="dc-queue-bar">
                  <div class="dc-queue-fill" :style="{ width: `${item.percentage}%` }" />
                </div>
                <span class="dc-queue-pct">{{ item.percentage }}%</span>
              </div>
            </div>

            <div v-if="activityByClient[client.id]?.history?.length" class="dc-history">
              <div class="dc-sub-label">Recent</div>
              <div v-for="item in (activityByClient[client.id]?.history ?? []).slice(0, 5)" :key="item.name + item.completed_at" class="dc-history-row">
                <Icon :name="item.status === 'Completed' ? 'check' : item.status === 'Failed' ? 'warning' : 'clock'" :size="12" class="dc-history-icon" :class="{ ok: item.status === 'Completed', bad: item.status === 'Failed' }" />
                <span class="dc-history-name">{{ item.name }}</span>
                <span class="dc-history-meta">{{ item.category }} · {{ fmtBytes(item.bytes) }} · {{ historyAge(item.completed_at) }}</span>
              </div>
            </div>

            <div v-if="activityByClient[client.id]?.warnings?.length" class="dc-warnings">
              <div class="dc-sub-label">Warnings</div>
              <div v-for="warning in activityByClient[client.id]?.warnings ?? []" :key="warning.at + warning.text.slice(0, 24)" class="dc-warning-row">
                <Icon name="warning" :size="12" class="dc-warning-icon" />
                <span class="dc-warning-text">{{ warning.text }}</span>
                <span class="dc-warning-age">{{ historyAge(warning.at) }}</span>
              </div>
            </div>

            <div v-if="activityByClient[client.id]?.categories?.length" class="dc-categories">
              <div class="dc-sub-label">Categories</div>
              <div class="dc-cat-table">
                <div class="dc-cat-head">
                  <span>Name</span>
                  <span>Client path</span>
                  <span>Heya sees</span>
                </div>
                <div v-for="cat in activityByClient[client.id]?.categories ?? []" :key="cat.name" class="dc-cat-row">
                  <span class="dc-cat-name">{{ cat.name }}</span>
                  <span class="dc-cat-path">{{ cat.remote_dir }}</span>
                  <span class="dc-cat-path" :class="{ unmapped: !cat.local_dir }">{{ cat.local_dir || 'no mapping' }}</span>
                </div>
              </div>
            </div>
          </div>
        </template>
      </div>
    </SettingsSection>

    <AppDialog v-model="dialogOpen" :title="editingID != null ? 'Edit download client' : 'Add download client'" size="sm">
      <div class="mgr-form">
        <label v-if="editingID == null" class="mgr-field">
          <span>Client</span>
          <AppSelect v-model="form.kind" :options="CLIENT_KIND_OPTIONS" />
        </label>
        <label class="mgr-field">
          <span>Name</span>
          <input v-model="form.name" class="mgr-input" placeholder="SABnzbd">
        </label>
        <label class="mgr-field">
          <span>URL</span>
          <input v-model="form.base_url" class="mgr-input mono" placeholder="https://sabnzbd.example.ts.net">
        </label>
        <label class="mgr-field">
          <span>API key</span>
          <input v-model="form.api_key" class="mgr-input mono" type="password" :placeholder="form.hasStoredKey ? '•••••••• (unchanged)' : ''">
        </label>
        <label class="mgr-field">
          <span>Category</span>
          <input v-model="form.category" class="mgr-input mono" placeholder="heya">
        </label>
        <div class="mgr-field">
          <span>Path mapping <em class="mgr-field-opt">(optional — client path → local path)</em></span>
          <div class="dc-map-row">
            <input v-model="form.map_remote" class="mgr-input mono" placeholder="/storage">
            <Icon name="arrow-right" :size="13" class="dc-map-arrow" />
            <input v-model="form.map_local" class="mgr-input mono" placeholder="/Volumes/Storage">
          </div>
        </div>
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
.dc-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  margin-bottom: 8px;
}
.dc-card-head {
  display: flex;
  align-items: flex-start;
  gap: 14px;
}

/* ── Live activity panel ── */
.dc-activity {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--hair);
}
.dc-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(110px, 1fr));
  gap: 8px;
}
.dc-stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 8px 11px;
  background: rgb(var(--ink) / 0.04);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}
.dc-stat-label {
  font-family: var(--font-mono);
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.dc-stat-value { font-size: 13.5px; font-weight: 600; color: var(--fg-0); white-space: nowrap; }

.dc-sub-label {
  font-family: var(--font-mono);
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--fg-3);
  margin-bottom: 6px;
}

.dc-queue-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 140px 42px;
  gap: 12px;
  align-items: center;
  padding: 7px 0;
}
.dc-queue-main { min-width: 0; display: flex; flex-direction: column; gap: 1px; }
.dc-queue-name {
  font-size: 12.5px; font-weight: 500; color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.dc-queue-meta { font-size: 11px; color: var(--fg-3); }
.dc-queue-bar {
  height: 5px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.08);
  overflow: hidden;
}
.dc-queue-fill {
  height: 100%;
  border-radius: 999px;
  background: var(--gold);
  transition: width 0.5s ease;
}
.dc-queue-pct {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-2);
  text-align: right;
}

.dc-history-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  min-width: 0;
}
.dc-history-icon { color: var(--fg-3); flex-shrink: 0; }
.dc-history-icon.ok { color: var(--good); }
.dc-history-icon.bad { color: var(--bad); }
.dc-history-name {
  font-size: 12px; color: var(--fg-1);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.dc-history-meta { font-size: 11px; color: var(--fg-3); flex-shrink: 0; margin-left: auto; white-space: nowrap; }

.dc-warning-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 4px 0;
  min-width: 0;
}
.dc-warning-icon { color: var(--gold); flex-shrink: 0; align-self: center; }
.dc-warning-text {
  font-size: 11.5px; color: var(--fg-2);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.dc-warning-age { font-family: var(--font-mono); font-size: 10px; color: var(--fg-3); margin-left: auto; flex-shrink: 0; }

.dc-cat-table { display: flex; flex-direction: column; }
.dc-cat-head,
.dc-cat-row {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr) minmax(0, 1fr);
  gap: 14px;
  align-items: baseline;
  padding: 4px 0;
}
.dc-cat-head {
  font-family: var(--font-mono);
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--fg-3);
  border-bottom: 1px solid var(--hair);
  padding-bottom: 6px;
  margin-bottom: 2px;
}
.dc-cat-name { font-size: 12px; font-weight: 500; color: var(--fg-0); }
.dc-cat-path {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.dc-cat-path.unmapped { color: var(--fg-3); font-style: italic; }

@media (max-width: 720px) {
  .dc-cat-head, .dc-cat-row { grid-template-columns: 90px minmax(0, 1fr); }
  .dc-cat-head span:last-child, .dc-cat-row span:last-child { display: none; }
}
.dc-icon {
  width: 36px; height: 36px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  display: flex; align-items: center; justify-content: center;
  color: var(--good);
  flex-shrink: 0;
}
.dc-icon.err { color: var(--bad); }
.dc-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.dc-name {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 14px; font-weight: 500; color: var(--fg-0);
}
.dc-host { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-2); }
.dc-meta {
  font-size: 11.5px; color: var(--fg-3);
  display: flex; flex-wrap: wrap; gap: 6px;
  align-items: baseline;
}
.dc-meta code {
  font-family: var(--font-mono);
  background: rgb(var(--ink) / 0.06);
  padding: 1px 5px;
  border-radius: 4px;
}
.dc-proto {
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
}
.dc-proto.torrent { color: var(--gold); }
.dc-proto.usenet { color: var(--good); }

.mgr-row-actions { display: flex; gap: 4px; align-items: center; flex-shrink: 0; }

.mgr-btn,
.mgr-btn-gold {
  display: inline-flex; align-items: center; gap: 7px;
  height: 32px; padding: 0 14px;
  border-radius: var(--r-sm);
  font-size: 12.5px; font-weight: 600;
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.mgr-btn {
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-1);
}
.mgr-btn:hover { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }
.mgr-btn-gold {
  background: var(--gold-soft);
  border: 1px solid color-mix(in srgb, var(--gold) 40%, transparent);
  color: var(--gold-bright);
}
.mgr-btn-gold:hover { background: color-mix(in srgb, var(--gold) 18%, transparent); }
.mgr-btn-gold:disabled { opacity: 0.6; pointer-events: none; }

.mgr-btn-icon {
  width: 30px; height: 30px;
  display: flex; align-items: center; justify-content: center;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.mgr-btn-icon:hover { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }
.mgr-btn-icon.danger:hover { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 40%, transparent); }
.mgr-btn-icon:disabled { opacity: 0.5; pointer-events: none; }

.mgr-flash {
  display: flex; align-items: center; gap: 8px;
  margin-bottom: 14px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  font-size: 12.5px;
}
.mgr-flash.ok { background: color-mix(in srgb, var(--good) 10%, transparent); border: 1px solid color-mix(in srgb, var(--good) 30%, transparent); color: var(--good); }
.mgr-flash.err { background: color-mix(in srgb, var(--bad) 10%, transparent); border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent); color: var(--bad); }

.mgr-loading,
.mgr-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}

@media (max-width: 720px) {
  .dc-card-head { flex-wrap: wrap; }
  .dc-queue-row { grid-template-columns: minmax(0, 1fr) 60px; }
  .dc-queue-pct { display: none; }
}
</style>

<!-- Portaled dialog content — unscoped on purpose. Base .mgr-form/.mgr-input
     styles live in layouts/manager.vue; only the mapping-row layout is added here. -->
<style>
.mgr-field-opt { font-style: normal; font-weight: 400; color: var(--fg-3); }
.dc-map-row {
  display: grid;
  grid-template-columns: 1fr auto 1fr;
  gap: 8px;
  align-items: center;
}
.dc-map-arrow { color: var(--fg-3); }
</style>
