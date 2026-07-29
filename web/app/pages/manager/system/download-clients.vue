<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { managerDownloadClientsQuery, type ManagerDownloadClientInput, type ManagerDownloadClientView, type ManagerTestResult } from '~/queries/manager'

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

      <div v-for="client in clients" :key="client.id" class="dc-card">
        <div class="dc-icon" :class="{ err: client.last_test_at && !client.last_test_ok }"><Icon name="download" :size="16" /></div>
        <div class="dc-body">
          <div class="dc-name">
            {{ client.name }}
            <StatusBadge :state="testStateOf(client).state">{{ testStateOf(client).label }}</StatusBadge>
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
    </SettingsSection>

    <AppDialog v-model:open="dialogOpen" :title="editingID != null ? 'Edit download client' : 'Add download client'" size="sm">
      <div class="mgr-form">
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
  align-items: flex-start;
  gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  margin-bottom: 8px;
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
  .dc-card { flex-wrap: wrap; }
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
