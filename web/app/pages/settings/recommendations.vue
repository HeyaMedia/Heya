<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

const { $heya } = useNuxtApp()

type Accel = { name: string; label: string; available: boolean; reason?: string }
type Progress = { current_file?: string; bytes_done?: number; bytes_total?: number; files_done?: number; files_total?: number }
type MLStatus = {
  enabled: boolean
  accelerator: string
  env_locks?: { enabled?: string; accelerator?: string }
  embedded?: number
  total?: number
  embedded_episodes?: number
  total_episodes?: number
  model?: string
  dimensions?: number
  accelerators?: Accel[]
  fetcher?: { state: string; all_present?: boolean; missing_count?: number; progress?: Progress; last_error?: string }
}
interface MLSettings { enabled: boolean; accelerator: string }

const status = ref<MLStatus | null>(null)
const settings = ref<MLSettings | null>(null)
const saving = ref(false)
const enableSaving = ref(false)
const busy = ref(false)
const { flash } = useFlash()

const enabledLocked = computed(() => !!status.value?.env_locks?.enabled)
const accelLocked = computed(() => !!status.value?.env_locks?.accelerator)
const fetcher = computed(() => status.value?.fetcher)
const embedded = computed(() => status.value?.embedded ?? 0)
const total = computed(() => status.value?.total ?? 0)
const epEmbedded = computed(() => status.value?.embedded_episodes ?? 0)
const epTotal = computed(() => status.value?.total_episodes ?? 0)
const missing = computed(() => fetcher.value?.missing_count ?? 0)
const progress = computed(() => fetcher.value?.progress)
const availableAccelerators = computed(() => (status.value?.accelerators ?? []).filter(a => a.available))
const modelReady = computed(() => !!fetcher.value?.all_present && embedded.value > 0)

const fetcherLabel = computed(() => {
  switch (fetcher.value?.state) {
    case 'idle': return missing.value === 0 ? 'all present' : 'download pending'
    case 'checking': return 'verifying'
    case 'fetching': return 'downloading'
    case 'ready': return 'all present'
    case 'failed': return 'failed'
    default: return fetcher.value?.state ?? 'unknown'
  }
})

async function loadStatus() {
  try { status.value = await $heya('/api/admin/recommendations-ml/status') as MLStatus }
  catch (e: any) { flash.value = { kind: 'err', text: e?.message ?? 'Failed to load status.' } }
}
async function loadSettings() {
  try { settings.value = await $heya('/api/admin/recommendations-ml/settings') as MLSettings }
  catch { settings.value = { enabled: false, accelerator: 'auto' } }
}
async function toggleEnabled() {
  if (!settings.value || enableSaving.value) return
  enableSaving.value = true
  const next = { ...settings.value, enabled: !settings.value.enabled }
  try {
    await $heya('/api/admin/recommendations-ml/settings', { method: 'PUT', body: next as any })
    settings.value = next
    flash.value = { kind: 'ok', text: next.enabled ? 'Enabled — downloading the model and embedding your library in the background.' : 'Disabled.' }
    loadStatus()
  } catch (e: any) { flash.value = { kind: 'err', text: e?.data?.error ?? 'Toggle failed.' } }
  finally { enableSaving.value = false }
}
async function save() {
  if (!settings.value) return
  saving.value = true; flash.value = null
  try {
    await $heya('/api/admin/recommendations-ml/settings', { method: 'PUT', body: settings.value as any })
    flash.value = { kind: 'ok', text: 'Saved.' }
    loadStatus()
  } catch (e: any) { flash.value = { kind: 'err', text: e?.data?.error ?? 'Save failed.' } }
  finally { saving.value = false }
}
async function reFetch() {
  busy.value = true
  try { await $heya('/api/admin/recommendations-ml/fetch', { method: 'POST', body: {} as any }); flash.value = { kind: 'ok', text: 'Model download + embedding kicked off.' } }
  catch (e: any) { flash.value = { kind: 'err', text: e?.message ?? 'Failed.' } }
  finally { busy.value = false }
}
async function reEmbed() {
  busy.value = true
  try { await $heya('/api/admin/recommendations-ml/backfill', { method: 'POST', body: {} as any }); flash.value = { kind: 'ok', text: 'Re-embedding the catalog…' } }
  catch (e: any) { flash.value = { kind: 'err', text: e?.message ?? 'Failed.' } }
  finally { busy.value = false }
}

let pollTimer: ReturnType<typeof setInterval> | null = null
onMounted(async () => {
  await Promise.all([loadSettings(), loadStatus()])
  pollTimer = setInterval(loadStatus, 3000)
})
onBeforeUnmount(() => { if (pollTimer) clearInterval(pollTimer) })
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Recommendations</h2>
      <p class="sv2-page-desc">
        Optional embedding engine (BGE-large-en). Off by default — turning it on
        downloads a ~340 MB model, embeds your library, and unlocks
        natural-language search plus deeper "For You" discovery. The non-ML
        recommender keeps working regardless.
      </p>
    </header>

    <SettingsSection title="Master switch" icon="power"
      :lockedBy="enabledLocked ? `Locked by ${status?.env_locks?.enabled}` : undefined">
      <div class="enable-card" :class="{ on: settings?.enabled }">
        <div class="enable-info">
          <div class="enable-row">
            <StatusBadge :state="settings?.enabled ? 'ok' : 'idle'">{{ settings?.enabled ? 'Enabled' : 'Disabled' }}</StatusBadge>
            <span class="enable-label">Embedding engine</span>
          </div>
          <p class="enable-sub">
            <template v-if="settings?.enabled">Model {{ fetcher?.all_present ? 'downloaded' : 'downloading' }}; {{ embedded }} / {{ total }} items embedded.</template>
            <template v-else>Enable to download the model (~340 MB) and embed your catalog. Off costs nothing — no disk, RAM, or boot cost.</template>
          </p>
        </div>
        <button class="enable-toggle" :class="{ on: settings?.enabled }"
          :disabled="!settings || enableSaving || enabledLocked" @click="toggleEnabled">
          <span class="enable-knob" />
        </button>
      </div>
    </SettingsSection>

    <template v-if="settings?.enabled">
      <div class="tiles">
        <MetricTile label="Model" :value="fetcherLabel"
          :tone="fetcher?.state === 'failed' ? 'bad' : fetcher?.all_present ? 'good' : 'warn'" icon="cloud"
          :sub="status?.model" />
        <MetricTile label="Embedded" :value="`${embedded} / ${total}`"
          :tone="embedded >= total && total > 0 ? 'good' : 'warn'" icon="sparkle"
          :sub="`${status?.dimensions ?? 1024}-dim`" />
        <MetricTile label="Episodes" :value="`${epEmbedded} / ${epTotal}`"
          :tone="epEmbedded >= epTotal && epTotal > 0 ? 'good' : 'warn'" icon="film"
          sub="overview embeddings" />
        <MetricTile label="Semantic search" :value="modelReady ? 'ready' : 'not ready'"
          :tone="modelReady ? 'good' : 'neutral'" icon="check" />
      </div>

      <SettingsSection title="Model & embeddings" icon="hard-drives"
        description="The model downloads on enable, then every title's metadata is embedded. Re-embed after a big library change.">
        <template #actions>
          <button v-if="missing > 0 || fetcher?.state === 'failed'" class="sv2-btn primary" :disabled="busy" @click="reFetch">
            <Icon name="cloud" :size="13" /> Download model
          </button>
          <button class="sv2-btn ghost" :disabled="busy" @click="reEmbed">
            <Icon name="refresh" :size="13" /> Re-embed
          </button>
        </template>
        <KVTable :rows="[
          { key: 'Model', value: status?.model ?? 'BGE-large-en-v1.5', mono: true },
          { key: 'Download', value: fetcherLabel },
          { key: 'Embedded', value: `${embedded} / ${total} items`, mono: true },
          { key: 'Episodes', value: `${epEmbedded} / ${epTotal} episodes`, mono: true },
          { key: 'Last error', value: fetcher?.last_error ?? '' },
        ]" />
        <div v-if="progress && fetcher?.state === 'fetching'" class="fetch-progress">
          <div class="prog-track"><div class="prog-fill" :style="{ width: `${Math.min(100, Math.round((progress.bytes_done ?? 0) / (progress.bytes_total || 1) * 100))}%` }" /></div>
          <div class="prog-meta">
            <span>{{ progress.files_done }}/{{ progress.files_total }} files</span>
            <span class="dim">·</span>
            <span>{{ ((progress.bytes_done ?? 0) / 1024 / 1024).toFixed(0) }} / {{ ((progress.bytes_total ?? 0) / 1024 / 1024).toFixed(0) }} MB</span>
          </div>
        </div>
      </SettingsSection>

      <SettingsSection title="Pipeline settings" icon="settings">
        <SettingsField label="Accelerator" description="Inference execution provider. Auto picks the best available at boot."
          :lockedBy="accelLocked ? `Locked by ${status?.env_locks?.accelerator}` : undefined">
          <select v-if="settings" v-model="settings.accelerator" class="sv2-select" :disabled="accelLocked">
            <option v-for="o in availableAccelerators" :key="o.name" :value="o.name">{{ o.label }}</option>
          </select>
        </SettingsField>
        <div class="save-bar">
          <span class="save-spacer" />
          <button class="sv2-btn primary" :disabled="saving" @click="save">{{ saving ? 'Saving…' : 'Save settings' }}</button>
        </div>
      </SettingsSection>
    </template>

    <SettingsFlash :flash="flash" />
  </div>
</template>

<style scoped>
.enable-card { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 18px 20px; background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md); }
.enable-card.on { border-color: rgba(111, 191, 124, 0.3); background: rgba(111, 191, 124, 0.04); }
.enable-info { min-width: 0; flex: 1; }
.enable-row { display: flex; align-items: center; gap: 10px; }
.enable-label { font-size: 14px; font-weight: 500; color: var(--fg-0); }
.enable-sub { margin: 6px 0 0; font-size: 12px; color: var(--fg-3); max-width: 560px; line-height: 1.5; }
.enable-toggle { width: 48px; height: 26px; border-radius: 100px; background: rgba(255,255,255,0.08); border: 0; position: relative; cursor: pointer; flex-shrink: 0; transition: background 0.2s ease; }
.enable-toggle.on { background: var(--good); }
.enable-toggle:disabled { opacity: 0.5; cursor: not-allowed; }
.enable-knob { position: absolute; top: 3px; left: 3px; width: 20px; height: 20px; border-radius: 50%; background: #fff; transition: transform 0.2s ease; box-shadow: 0 1px 3px rgba(0,0,0,0.4); }
.enable-toggle.on .enable-knob { transform: translateX(22px); }
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 8px; margin: 12px 0 28px; }
.fetch-progress { margin-top: 14px; }
.prog-track { height: 6px; border-radius: 3px; background: var(--bg-0); overflow: hidden; }
.prog-fill { height: 100%; background: var(--gold); transition: width 0.3s ease; }
.prog-meta { display: flex; gap: 6px; align-items: center; font-family: var(--font-mono); font-size: 11px; color: var(--fg-2); margin-top: 6px; }
.prog-meta .dim { color: var(--fg-4); }
.sv2-select { background: var(--bg-0); border: 1px solid var(--border); border-radius: var(--r-sm); color: var(--fg-0); font-size: 13px; padding: 8px 12px; min-width: 240px; cursor: pointer; outline: none; }
.sv2-select:focus { border-color: var(--gold); }
.sv2-select:disabled { opacity: 0.5; cursor: not-allowed; }
.save-bar { display: flex; align-items: center; gap: 12px; padding: 16px 0 0; }
.save-spacer { flex: 1; }
@media (max-width: 720px) { .sv2-select { min-width: 0; width: 100%; } .tiles { grid-template-columns: repeat(2, 1fr); } }
</style>
