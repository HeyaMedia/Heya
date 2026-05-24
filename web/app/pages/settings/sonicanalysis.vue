<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">Sonic Analysis</h2>
      <p class="page-desc">
        ML/DSP music analysis: embeddings for similarity, BPM, key, loudness,
        mood, genre, waveform. Runs as a scheduled task — configure the time
        window on the Tasks page.
      </p>
    </div>

    <!-- Master enable / disable -->
    <section class="section enable-section">
      <div class="enable-card" :class="{ 'enable-on': settings?.enabled }">
        <div class="enable-info">
          <div class="enable-title">
            <span class="dot-status" :class="settings?.enabled ? 'dot-good' : 'dot-muted'" />
            Sonic Analysis is <strong>{{ settings?.enabled ? 'enabled' : 'disabled' }}</strong>
          </div>
          <div class="enable-sub">
            <template v-if="settings?.enabled">
              Models {{ missingCount === 0 ? 'are present' : 'will download in the background' }}.
              The scheduled task processes tracks during its time window.
            </template>
            <template v-else>
              Enable to download the analysis models (~720 MB) and unlock similarity,
              audio-vibe search, BPM, key, loudness, and waveform features. Off costs
              you nothing — disk, RAM, and boot time stay clean.
            </template>
          </div>
        </div>
        <button
          class="enable-toggle"
          :class="{ on: settings?.enabled }"
          :disabled="!settings || enableSaving || isLocked('sonic_analysis.enabled')"
          @click="toggleEnabled"
          :title="isLocked('sonic_analysis.enabled') ? lockTooltip('sonic_analysis.enabled') : (settings?.enabled ? 'Disable sonic analysis' : 'Enable sonic analysis')"
        >
          <span class="enable-toggle-knob" />
        </button>
      </div>
    </section>

    <!-- Everything below is gated on enabled -->
    <template v-if="settings?.enabled">

    <!-- Models -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="hard-drives" :size="14" />
        Models
      </h3>

      <!-- Summary card -->
      <div class="status-card" :class="modelsStatusClass">
        <div class="status-body">
          <div class="status-row">
            <span class="status-label">Fetcher</span>
            <span class="status-val">
              <span class="dot-status" :class="dotStatusClass" />
              {{ fetcherStateLabel }}
            </span>
          </div>
          <div v-if="fetcherProgress && fetcherState === 'fetching'" class="status-row">
            <span class="status-label">Progress</span>
            <span class="status-val mono">
              {{ fetcherProgress.files_done }}/{{ fetcherProgress.files_total }} files
              · {{ (fetcherProgress.bytes_done / 1024 / 1024).toFixed(1) }} /
              {{ (fetcherProgress.bytes_total / 1024 / 1024).toFixed(1) }} MB
              <span v-if="fetcherProgress.current_file"> · {{ fetcherProgress.current_file }}</span>
            </span>
          </div>
          <div v-if="fetcherLastError" class="status-row">
            <span class="status-label">Last error</span>
            <span class="status-val status-bad mono">{{ fetcherLastError }}</span>
          </div>
          <div class="status-row">
            <span class="status-label">On disk</span>
            <span class="status-val mono">
              {{ presentCount }} / {{ totalCount }} files
              <span v-if="totalSizeMB > 0" class="status-subtle"> ({{ presentSizeMB.toFixed(0) }} / {{ totalSizeMB.toFixed(0) }} MB)</span>
            </span>
          </div>
          <div class="status-row">
            <span class="status-label">Analyzer</span>
            <span class="status-val">{{ status?.analyzer?.state ?? 'unknown' }}</span>
          </div>
          <div class="status-row">
            <span class="status-label">Text search</span>
            <span class="status-val">{{ status?.text_searcher?.ready ? 'ready (warm)' : 'idle (cold)' }}</span>
          </div>
          <div class="status-row">
            <span class="status-label">Hardware</span>
            <span class="status-val">
              <span
                v-for="accel in (status?.accelerators ?? []).filter(a => a.name !== 'auto')"
                :key="accel.name"
                class="hw-chip"
                :class="{ 'hw-chip-good': accel.available, 'hw-chip-muted': !accel.available }"
                :title="accel.reason || (accel.available ? 'available' : 'unavailable')"
              >
                <span class="dot-status" :class="accel.available ? 'dot-good' : 'dot-muted'" />
                {{ accel.label }}
              </span>
            </span>
          </div>
          <div class="status-row">
            <span class="status-label">Analyzer version</span>
            <span class="status-val mono">v{{ status?.analyzer_version ?? '?' }} <span class="status-subtle">(code-level)</span></span>
          </div>
        </div>
      </div>

      <!-- Actions row: contextual to fetcher state -->
      <div class="form-actions">
        <button
          v-if="missingCount > 0"
          class="btn btn-primary"
          :disabled="fetching"
          @click="triggerFetch"
        >
          <Icon name="cloud" :size="14" />
          {{ fetching ? 'Fetching…' : `Download ${missingCount} missing file${missingCount === 1 ? '' : 's'}` }}
        </button>
        <button
          v-else-if="!fetching"
          class="btn btn-secondary"
          @click="triggerFetch"
          title="Re-verifies file presence and re-fetches anything that looks wrong"
        >
          <Icon name="refresh" :size="14" />
          Re-verify
        </button>
        <span v-if="missingCount === 0 && !fetching" class="form-hint">
          All models present. Upstream files don't carry a version stamp — re-verify if a download looks corrupt.
        </span>
        <span v-else-if="missingCount > 0 && !fetching" class="form-hint">
          ~{{ totalSizeMB.toFixed(0) }} MB total. Already-present files are skipped.
        </span>
      </div>

      <!-- Per-file detail (collapsed by default once ready) -->
      <div class="manifest">
        <button class="manifest-toggle" type="button" @click="manifestOpen = !manifestOpen">
          <Icon name="chevright" :size="12" :style="manifestOpen ? { transform: 'rotate(90deg)' } : undefined" />
          <span>{{ manifestOpen ? 'Hide file list' : 'Show file list' }}</span>
        </button>
        <div v-if="manifestOpen" class="manifest-groups">
          <div v-for="group in manifestGroups" :key="group.key" class="manifest-group">
            <div class="manifest-group-label">{{ group.label }}</div>
            <div v-for="file in group.files" :key="file.name" class="manifest-row">
              <span class="manifest-state">
                <span class="dot-status" :class="file.present ? 'dot-good' : 'dot-bad'" />
              </span>
              <span class="manifest-name mono">{{ file.name }}</span>
              <span class="manifest-size mono">
                <template v-if="file.present">
                  {{ (file.actual_size / 1024 / 1024).toFixed(1) }} MB
                </template>
                <template v-else>
                  missing
                  <span class="status-subtle">(~{{ (file.expected_size / 1024 / 1024).toFixed(1) }} MB)</span>
                </template>
              </span>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- Settings form -->
    <section class="section">
      <h3 class="section-heading">
        <Icon name="settings" :size="14" />
        Pipeline settings
      </h3>
      <div v-if="settings" class="form-grid">
        <div class="form-field">
          <label class="form-label">Accelerator</label>
          <select
            v-model="settings.accelerator"
            class="form-input"
            :disabled="isLocked('sonic_analysis.accelerator')"
            :title="lockTooltip('sonic_analysis.accelerator')"
          >
            <option v-for="opt in availableAccelerators" :key="opt.name" :value="opt.name">{{ opt.label }}</option>
          </select>
          <span class="form-hint">
            <template v-if="hiddenAccelerators.length">
              CPU + {{ availableAccelerators.filter(a => a.name !== 'auto' && a.name !== 'cpu').map(a => a.label).join(' / ') || 'no GPU EPs' }}
              available. Unavailable on this host:
              <span class="mono">{{ hiddenAccelerators.map(a => a.label + ' (' + (a.reason || 'not present') + ')').join(', ') }}</span>.
            </template>
            <template v-else>
              Only CPU is available on this host.
            </template>
            <br />
            Dynamic-batch models (classifier heads, base EffNet) always run on CPU when the primary picks CoreML
            — the EP otherwise recompiles per call and ends up ~8× slower.
          </span>
        </div>
      </div>

      <div class="form-actions">
        <button class="btn btn-primary" :disabled="saving || !settings" @click="save">
          <Icon name="check" :size="14" />
          {{ saving ? 'Saving…' : 'Save' }}
        </button>
        <span v-if="saved" class="save-confirmation">{{ saved }}</span>
      </div>
    </section>

    <section class="section">
      <h3 class="section-heading">
        <Icon name="pulse" :size="14" />
        Library coverage
      </h3>
      <div class="status-card">
        <div class="status-body">
          <div class="status-row">
            <span class="status-label">Tracks analyzed</span>
            <span class="status-val mono">{{ coverage.analyzed }}</span>
          </div>
          <div class="status-row">
            <span class="status-label">Tracks pending</span>
            <span class="status-val mono">{{ coverage.pending }}</span>
          </div>
        </div>
      </div>
      <div class="form-actions">
        <NuxtLink class="btn btn-secondary" to="/settings/tasks">
          <Icon name="timer" :size="14" />
          Configure schedule
        </NuxtLink>
        <span class="form-hint">The Tasks page owns the daily time window + max runtime.</span>
      </div>
    </section>
    </template>
  </div>
</template>

<script setup lang="ts">
definePageMeta({ layout: 'default' })

const { isLocked, lockTooltip, ensure: ensureSources } = useConfigSources()

interface SonicSettings {
  enabled: boolean
  accelerator: string
}

interface AcceleratorAvailability {
  name: string
  label: string
  available: boolean
  reason?: string
}

interface FetcherProgress {
  current_file: string
  bytes_done: number
  bytes_total: number
  files_done: number
  files_total: number
  started_at: string
}

interface ManifestFile {
  name: string
  present: boolean
  expected_size: number
  actual_size: number
  category: string
}

interface SonicStatus {
  fetcher?: {
    state: string
    all_present: boolean
    missing_count: number
    total_count: number
    total_size: number
    manifest: ManifestFile[]
    progress?: FetcherProgress
    last_error?: string
  }
  analyzer?: { state: string }
  text_searcher?: { ready: boolean }
  accelerators?: AcceleratorAvailability[]
  analyzer_version?: number
}

const settings = ref<SonicSettings | null>(null)
const status = ref<SonicStatus | null>(null)
const coverage = ref<{ analyzed: number; pending: number }>({ analyzed: 0, pending: 0 })
const saving = ref(false)
const saved = ref<string | null>(null)
const fetching = ref(false)

const fetcherState = computed(() => status.value?.fetcher?.state ?? 'unknown')
const fetcherProgress = computed(() => status.value?.fetcher?.progress)
const fetcherLastError = computed(() => status.value?.fetcher?.last_error)
const missingCount = computed(() => status.value?.fetcher?.missing_count ?? 0)
const totalCount = computed(() => status.value?.fetcher?.total_count ?? 0)
const presentCount = computed(() => totalCount.value - missingCount.value)
const totalSizeMB = computed(() => (status.value?.fetcher?.total_size ?? 0) / 1024 / 1024)
const presentSizeMB = computed(() => {
  const manifest = status.value?.fetcher?.manifest ?? []
  let sum = 0
  for (const f of manifest) if (f.present) sum += f.actual_size
  return sum / 1024 / 1024
})

const manifestOpen = ref(false)

// State label is more readable than the raw enum.
const fetcherStateLabel = computed(() => {
  switch (fetcherState.value) {
    case 'idle': return missingCount.value === 0 ? 'all models present' : 'idle (download pending)'
    case 'checking': return 'verifying files'
    case 'fetching': return 'downloading'
    case 'ready': return 'all models present'
    case 'failed': return 'failed'
    default: return fetcherState.value
  }
})

const modelsStatusClass = computed(() => {
  if (fetcherState.value === 'failed') return 'status-error'
  if (missingCount.value === 0) return 'status-ok'
  return ''
})
const dotStatusClass = computed(() => {
  if (fetcherState.value === 'failed') return 'dot-bad'
  if (fetcherState.value === 'fetching') return 'dot-busy'
  if (missingCount.value === 0) return 'dot-good'
  return 'dot-warn'
})

// Group the manifest rows so the UI doesn't show 18 flat lines.
const manifestGroups = computed(() => {
  const manifest = status.value?.fetcher?.manifest ?? []
  const groups: { key: string; label: string; files: ManifestFile[] }[] = [
    { key: 'discogs', label: 'Discogs specialized heads (track / artist / release)', files: [] },
    { key: 'effnet_base', label: 'Base EffNet (genre + 1280-dim)', files: [] },
    { key: 'head', label: 'Classifier heads (mood / danceability / voice)', files: [] },
    { key: 'clap', label: 'CLAP HTSAT (audio + text encoders)', files: [] },
    { key: 'clap_aux', label: 'CLAP tokenizer files', files: [] },
  ]
  const byKey = new Map(groups.map(g => [g.key, g]))
  const fallback = byKey.get('head')!
  for (const f of manifest) {
    const target = byKey.get(f.category) ?? fallback
    target.files.push(f)
  }
  return groups.filter(g => g.files.length > 0)
})

async function loadSettings() {
  try {
    settings.value = await apiFetch<SonicSettings>('/api/admin/sonicanalysis/settings')
  } catch {
    settings.value = {
      enabled: false,
      accelerator: 'auto',
    }
  }
}

const enableSaving = ref(false)
async function toggleEnabled() {
  if (!settings.value || enableSaving.value) return
  enableSaving.value = true
  const next = { ...settings.value, enabled: !settings.value.enabled }
  try {
    await apiFetch('/api/admin/sonicanalysis/settings', {
      method: 'PUT',
      body: next as any,
      headers: { 'Content-Type': 'application/json' },
    } as any)
    settings.value = next
    // Refresh status to pick up the new fetcher state (the backend
    // kicks off a fetch on enable transitions).
    loadStatus()
  } catch (e: any) {
    saved.value = e?.data?.error ?? 'Toggle failed'
  } finally {
    enableSaving.value = false
  }
}

// Filter accelerator options to those actually available on this host.
// We always include "auto" (synthesized server-side) and "cpu", and
// add any GPU EPs the runtime can actually attach.
const availableAccelerators = computed(() => {
  const all = status.value?.accelerators ?? []
  return all.filter(a => a.available)
})
const hiddenAccelerators = computed(() => {
  const all = status.value?.accelerators ?? []
  return all.filter(a => !a.available)
})

async function loadStatus() {
  try {
    status.value = await apiFetch<SonicStatus>('/api/admin/sonicanalysis/status')
    if (status.value?.fetcher?.state === 'fetching') {
      fetching.value = true
    } else if (status.value?.fetcher?.state === 'ready' && fetching.value) {
      fetching.value = false
    }
  } catch {
    status.value = null
  }
}

async function loadCoverage() {
  // No dedicated endpoint yet — derive from a tiny SQL probe via the
  // status endpoint's stats block. Re-using the status hub: skipped
  // for now; the dashboard already counts tracks elsewhere.
}

async function save() {
  if (!settings.value) return
  saving.value = true
  saved.value = null
  try {
    const res = await apiFetch<{ status: string; applied: boolean }>(
      '/api/admin/sonicanalysis/settings',
      {
        method: 'PUT',
        body: settings.value as any,
        headers: { 'Content-Type': 'application/json' },
      } as any,
    )
    saved.value = res.applied
      ? 'Saved & applied'
      : 'Saved (will apply at next idle — analyzer is busy)'
    setTimeout(() => { saved.value = null }, 4000)
  } catch (e: any) {
    saved.value = e?.data?.error ?? 'Save failed'
  } finally {
    saving.value = false
  }
}

async function triggerFetch() {
  fetching.value = true
  try {
    await apiFetch('/api/admin/sonicanalysis/fetch', {
      method: 'POST',
      body: {},
      headers: { 'Content-Type': 'application/json' },
    } as any)
  } catch {
    fetching.value = false
  }
}

let pollTimer: ReturnType<typeof setInterval> | null = null
onMounted(async () => {
  await Promise.all([loadSettings(), loadStatus(), loadCoverage(), ensureSources()])
  // Poll status while the page is mounted. We always re-fetch on a
  // slow cadence (5 s) so manual file moves on disk show up; we
  // tighten to 2 s while an active fetch is running.
  let lastFetch = 0
  pollTimer = setInterval(() => {
    const fast = fetching.value || status.value?.fetcher?.state === 'fetching'
    const interval = fast ? 2000 : 5000
    const now = Date.now()
    if (now - lastFetch >= interval) {
      lastFetch = now
      loadStatus()
    }
  }, 1000)
})
onBeforeUnmount(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<style scoped>
.page-header { margin-bottom: 32px; }
.page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.page-desc { font-size: 13px; color: var(--fg-3); margin: 6px 0 0; }

.section { margin-bottom: 36px; }

.enable-section { margin-bottom: 28px; }
.enable-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 20px 22px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  transition: border-color 0.2s ease;
}
.enable-card.enable-on { border-color: var(--good); }
.enable-info { min-width: 0; flex: 1; }
.enable-title {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 15px;
  color: var(--fg-0);
}
.enable-title strong { font-weight: 600; }
.enable-sub {
  font-size: 12px;
  color: var(--fg-3);
  margin-top: 6px;
  max-width: 560px;
  line-height: 1.5;
}
.enable-toggle {
  width: 48px;
  height: 26px;
  border-radius: 100px;
  background: rgba(255,255,255,0.08);
  border: 0;
  position: relative;
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.2s ease;
}
.enable-toggle.on { background: var(--good); }
.enable-toggle:disabled { opacity: 0.5; cursor: default; }
.enable-toggle-knob {
  position: absolute;
  top: 3px;
  left: 3px;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: #fff;
  transition: transform 0.2s ease;
  box-shadow: 0 1px 3px rgba(0,0,0,0.4);
}
.enable-toggle.on .enable-toggle-knob { transform: translateX(22px); }
.section-heading {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-3);
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin: 0 0 14px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 16px;
}
@media (max-width: 720px) {
  .form-grid { grid-template-columns: 1fr; }
}

.form-field { display: flex; flex-direction: column; gap: 6px; }
.form-label {
  font-size: 12px;
  font-weight: 500;
  color: var(--fg-1);
}
.form-check {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}
.form-input {
  padding: 10px 12px;
  background: rgba(255,255,255,0.04);
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--fg-0);
  font-size: 13px;
  outline: none;
}
.form-input:focus { border-color: var(--gold); }
.form-hint { font-size: 11px; color: var(--fg-3); }

.form-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 16px;
  flex-wrap: wrap;
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border-radius: 6px;
  border: 0;
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  text-decoration: none;
}
.btn-primary { background: var(--gold); color: var(--bg-0); }
.btn-primary:disabled { opacity: 0.5; cursor: default; }
.btn-secondary {
  background: rgba(255,255,255,0.04);
  border: 1px solid var(--border);
  color: var(--fg-1);
}
.btn-secondary:disabled { opacity: 0.5; cursor: default; }
.save-confirmation { font-size: 12px; color: var(--good); }

.status-card {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 14px 18px;
  margin-bottom: 12px;
}
.status-card.status-ok { border-color: var(--good); }
.status-card.status-error { border-color: var(--bad); }
.status-body { display: grid; gap: 8px; }
.status-row { display: flex; justify-content: space-between; gap: 12px; font-size: 13px; }
.status-label { color: var(--fg-3); }
.status-val { color: var(--fg-0); display: inline-flex; align-items: center; gap: 6px; }
.status-val.status-bad { color: var(--bad); }
.status-subtle { color: var(--fg-3); }
.mono { font-family: var(--font-mono); font-size: 12px; }

.dot-status {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  display: inline-block;
  flex-shrink: 0;
}
.dot-good { background: var(--good); box-shadow: 0 0 6px rgba(111, 191, 124, 0.4); }
.dot-bad { background: var(--bad); box-shadow: 0 0 6px rgba(217, 107, 107, 0.4); }
.dot-warn { background: var(--gold); }
.dot-muted { background: var(--fg-4); }
.dot-busy {
  background: var(--gold);
  animation: pulse 1.2s ease-in-out infinite;
}

.hw-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 8px;
  border-radius: 100px;
  background: rgba(255,255,255,0.04);
  border: 1px solid var(--border);
  font-size: 11px;
  font-family: var(--font-mono);
  margin-right: 4px;
}
.hw-chip-good { color: var(--fg-1); border-color: rgba(111, 191, 124, 0.3); }
.hw-chip-muted { color: var(--fg-4); }
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

.manifest { margin-top: 8px; }
.manifest-toggle {
  background: transparent;
  border: 0;
  color: var(--fg-3);
  font-size: 12px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  padding: 6px 0;
}
.manifest-toggle:hover { color: var(--fg-1); }
.manifest-groups {
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  border-top: 1px solid var(--border);
  padding-top: 12px;
}
.manifest-group { display: flex; flex-direction: column; gap: 4px; }
.manifest-group-label {
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin-bottom: 4px;
}
.manifest-row {
  display: grid;
  grid-template-columns: 16px 1fr auto;
  align-items: center;
  gap: 10px;
  padding: 4px 0;
  font-size: 12px;
}
.manifest-state { display: flex; align-items: center; justify-content: center; }
.manifest-name { color: var(--fg-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.manifest-size { color: var(--fg-3); }
</style>
