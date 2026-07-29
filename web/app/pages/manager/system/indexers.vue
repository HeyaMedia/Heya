<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { managerIndexersQuery, type ManagerIndexerInput, type ManagerIndexerView, type ManagerTestResult } from '~/queries/manager'

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
          <button v-if="!prowlarrApps.length" type="button" class="mgr-btn-gold" @click="openAdd">
            <Icon name="plus" :size="14" /> Connect Prowlarr
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

        <div v-for="app in prowlarrApps" :key="`children-${app.id}`" class="ix-table">
          <div class="ix-head">
            <span>Indexer</span>
            <span>Protocol</span>
            <span>Priority</span>
            <span>Enabled</span>
          </div>
          <div v-for="child in app.children ?? []" :key="child.id" class="ix-row" :class="{ dim: !child.enabled }">
            <span class="ix-name">{{ child.name }}</span>
            <span class="ix-proto" :class="child.protocol">{{ child.protocol }}</span>
            <span class="ix-prio">{{ child.priority }}</span>
            <AppSwitch :model-value="child.enabled" :aria-label="`Enable ${child.name}`" @update:model-value="(v: boolean) => toggleChild(child, v)" />
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

    <AppDialog v-model:open="dialogOpen" :title="editingID != null ? 'Edit indexer' : 'Add indexer'" size="sm">
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

.ix-table { display: flex; flex-direction: column; gap: 6px; }
.ix-head,
.ix-row {
  display: grid;
  grid-template-columns: minmax(140px, 1.6fr) 90px 70px minmax(120px, auto);
  gap: 14px;
  align-items: center;
}
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

@media (max-width: 960px) {
  .ix-head { display: none; }
  .ix-row { grid-template-columns: minmax(0, 1fr) auto; row-gap: 6px; }
  .ix-prio { display: none; }
}
</style>

<!-- Shared .mgr-form/.mgr-input dialog styles live unscoped in
     layouts/manager.vue (portaled content + per-page chunking). -->
