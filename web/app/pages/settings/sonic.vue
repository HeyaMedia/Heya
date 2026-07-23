<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

const { $heya } = useNuxtApp()
const { isLocked, lockTooltip, ensure: ensureSources } = useConfigSources()
import { sonicSettingsQuery, sonicStatusQuery } from '~/queries/intelligence'
import type { SonicManifestFile as ManifestFile, SonicSettings } from '~/queries/intelligence'

const statusData = useQuery(sonicStatusQuery())
const settingsData = useQuery(sonicSettingsQuery())
const status = computed(() => statusData.data.value ?? null)
const settings = ref<SonicSettings | null>(null)
const saving = ref(false)
const fetching = ref(false)
const enableSaving = ref(false)
const restartRequired = ref(false)
const restartingWorker = ref(false)
const { flash } = useFlash()
const { confirm } = useConfirm()
const manifestOpen = ref(false)
const pipelineWorkers = computed(() => (settings.value?.preprocess_ahead ?? 0) + (settings.value?.gpu_workers ?? 0))

const fetcherState = computed(() => status.value?.fetcher?.state ?? 'unknown')
const missingCount = computed(() => status.value?.fetcher?.missing_count ?? 0)
const totalCount   = computed(() => status.value?.fetcher?.total_count ?? 0)
const presentCount = computed(() => totalCount.value - missingCount.value)
const totalSizeMB  = computed(() => (status.value?.fetcher?.total_size ?? 0) / 1024 / 1024)
const presentSizeMB = computed(() => {
  let sum = 0
  for (const f of status.value?.fetcher?.manifest ?? []) if (f.present) sum += f.actual_size
  return sum / 1024 / 1024
})
const coverage = computed(() => status.value?.coverage ?? { analyzed: 0, pending: 0 })
const coverageSub = computed(() => {
  const parts = [`${coverage.value.pending} pending`]
  const cleanup = coverage.value.clap_cleanup_pending ?? 0
  if (cleanup > 0) parts.push(`${cleanup} CLAP cleanup`)
  return parts.join(' · ')
})
const holder = computed(() => status.value?.holder)
const pipelineMismatch = computed(() => {
  if (!settings.value || !holder.value || holder.value.source !== 'worker') return false
  return holder.value.preprocess_ahead !== settings.value.preprocess_ahead ||
    holder.value.gpu_workers !== settings.value.gpu_workers
})
const needsWorkerRestart = computed(() => restartRequired.value || pipelineMismatch.value)
const fetchProgress = computed(() => status.value?.fetcher?.progress)
const lastError = computed(() => status.value?.fetcher?.last_error)

const fetcherTone = computed<'good' | 'warn' | 'bad'>(() => {
  if (fetcherState.value === 'failed') return 'bad'
  if (fetcherState.value === 'fetching' || missingCount.value > 0) return 'warn'
  return 'good'
})
const fetcherLabel = computed(() => {
  switch (fetcherState.value) {
    case 'idle':     return missingCount.value === 0 ? 'all present' : 'download pending'
    case 'checking': return 'verifying'
    case 'fetching': return 'downloading'
    case 'ready':    return 'all present'
    case 'failed':   return 'failed'
    default:         return fetcherState.value
  }
})

const holderTone = computed<'good' | 'warn' | 'neutral'>(() => {
  switch (holder.value?.state) {
    case 'ready':     return 'good'
    case 'loading':
    case 'unloading': return 'warn'
    default:          return 'neutral'
  }
})
const holderLabel = computed(() => {
  switch (holder.value?.state) {
    case 'ready':     return 'warm'
    case 'loading':   return 'loading'
    case 'unloading': return 'unloading'
    case 'unloaded':  return 'cold'
    default:          return holder.value?.state ?? 'unknown'
  }
})

const availableAccelerators = computed(() => (status.value?.accelerators ?? []).filter(a => a.available))
const hiddenAccelerators    = computed(() => (status.value?.accelerators ?? []).filter(a => !a.available))

const manifestGroups = computed(() => {
  const manifest = status.value?.fetcher?.manifest ?? []
  const groups: { key: string; label: string; files: ManifestFile[] }[] = [
    { key: 'discogs',     label: 'Discogs specialised heads (track / artist / release)', files: [] },
    { key: 'effnet_base', label: 'Base EffNet (genre + 1280-dim)', files: [] },
    { key: 'head',        label: 'Classifier heads (mood / danceability / voice)', files: [] },
    { key: 'clap',        label: 'CLAP HTSAT (audio + text encoders)', files: [] },
    { key: 'clap_aux',    label: 'CLAP tokenizer files', files: [] },
  ]
  const byKey = new Map(groups.map(g => [g.key, g]))
  const fallback = byKey.get('head')!
  for (const f of manifest) (byKey.get(f.category) ?? fallback).files.push(f)
  return groups.filter(g => g.files.length > 0)
})

const nowTick = ref(Date.now())
let nowHandle: ReturnType<typeof setInterval> | null = null
function fmtDuration(sec: number): string {
  if (sec < 60) return `${sec}s`
  if (sec < 3600) { const m = Math.floor(sec / 60), s = sec % 60; return s > 0 ? `${m}m ${s}s` : `${m}m` }
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60)
  return `${h}h ${m}m`
}
function relTime(ts?: string): string {
  if (!ts) return ''
  return fmtDuration(Math.abs(Math.round((nowTick.value - new Date(ts).getTime()) / 1000)))
}
function relTimeFuture(ts?: string): string {
  if (!ts) return ''
  const sec = Math.round((new Date(ts).getTime() - nowTick.value) / 1000)
  if (sec <= 0) return 'imminent'
  return fmtDuration(sec)
}

async function loadStatus() {
  try {
    await statusData.refetch()
    fetching.value = status.value?.fetcher?.state === 'fetching'
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load sonic status.' }
  }
}
async function loadSettings() {
  try {
    await settingsData.refetch()
    if (settingsData.data.value) settings.value = structuredClone(settingsData.data.value)
  } catch {
    settings.value = { enabled: false, accelerator: 'auto', preprocess_ahead: 4, gpu_workers: 1 }
  }
}

watch(() => settingsData.data.value, value => {
  if (value) settings.value = structuredClone(value)
}, { immediate: true })

async function toggleEnabled() {
  if (!settings.value || enableSaving.value) return
  enableSaving.value = true
  const next = { ...settings.value, enabled: !settings.value.enabled }
  try {
    await $heya('/api/admin/sonicanalysis/settings', { method: 'PUT', body: next as any })
    settings.value = next
    flash.value = { kind: 'ok', text: settings.value.enabled ? 'Sonic analysis enabled.' : 'Sonic analysis disabled.' }
    loadStatus()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.error ?? 'Toggle failed.' }
  } finally {
    enableSaving.value = false
  }
}

async function save() {
  if (!settings.value) return
  saving.value = true
  flash.value = null
  try {
    const res = await $heya('/api/admin/sonicanalysis/settings', {
      method: 'PUT',
      body: settings.value as any,
    }) as { status: string; applied: boolean; restart_required: boolean }
    restartRequired.value = res.restart_required
    flash.value = {
      kind: res.restart_required || !res.applied ? 'warn' : 'ok',
      text: res.restart_required
        ? 'Saved — restart the worker to apply the new pipeline concurrency.'
        : res.applied ? 'Saved and applied.' : 'Saved — analyzer is busy; will apply at next idle.',
    }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.error ?? 'Save failed.' }
  } finally {
    saving.value = false
  }
}

async function restartWorker() {
  const approved = await confirm({
    title: 'Restart the worker?',
    message: 'Active jobs will stop gracefully and resume after the process supervisor brings the worker back.',
    destructive: true,
    confirmLabel: 'Restart worker',
  })
  if (!approved) return
  restartingWorker.value = true
  try {
    await $heya('/api/admin/processes/restart', {
      method: 'POST',
      body: { target: 'worker' } as any,
    })
    restartRequired.value = false
    flash.value = { kind: 'ok', text: 'Worker restart requested. It should return within a few seconds.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Worker restart failed.' }
  } finally {
    restartingWorker.value = false
  }
}

async function triggerFetch() {
  fetching.value = true
  try {
    await $heya('/api/admin/sonicanalysis/fetch', { method: 'POST', body: {} as any })
    flash.value = { kind: 'ok', text: 'Fetcher kicked off.' }
  } catch (e: any) {
    fetching.value = false
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to start fetch.' }
  }
}

let pollTimer: ReturnType<typeof setInterval> | null = null
onMounted(async () => {
  await Promise.all([loadSettings(), loadStatus(), ensureSources()])
  let last = 0
  pollTimer = setInterval(() => {
    const fast = fetching.value || status.value?.fetcher?.state === 'fetching'
    const interval = fast ? 2000 : 5000
    const now = Date.now()
    if (now - last >= interval) { last = now; loadStatus() }
  }, 1000)
  nowHandle = setInterval(() => { nowTick.value = Date.now() }, 1000)
})
onBeforeUnmount(() => {
  if (pollTimer) clearInterval(pollTimer)
  if (nowHandle) clearInterval(nowHandle)
})
</script>

<template>
  <div>
    <SettingsContextHero
      title="Sonic analysis"
      icon="eq"
      eyebrow="Media intelligence · Audio understanding"
      description="Analyze the sound itself to unlock similarity, vibe search, BPM, key, mood, danceability, loudness, and waveforms."
    />

    <SettingsSection
      title="Master switch"
      icon="power"
      :lockedBy="isLocked('sonic_analysis.enabled') ? lockTooltip('sonic_analysis.enabled') : undefined"
    >
      <div class="enable-card" :class="{ on: settings?.enabled }">
        <div class="enable-info">
          <div class="enable-row">
            <StatusBadge :state="settings?.enabled ? 'ok' : 'idle'">
              {{ settings?.enabled ? 'Enabled' : 'Disabled' }}
            </StatusBadge>
            <span class="enable-label">Sonic analysis</span>
          </div>
          <p class="enable-sub">
            <template v-if="settings?.enabled">
              Models {{ missingCount === 0 ? 'are present' : 'will download in the background' }}.
              The scheduled task processes tracks during its time window.
            </template>
            <template v-else>
              Enable to download the analysis models (~720 MB) and unlock
              similarity, audio-vibe search, BPM, key, loudness, and waveform
              features. Off costs nothing — disk, RAM, and boot time stay clean.
            </template>
          </p>
        </div>
        <button
          class="enable-toggle"
          :class="{ on: settings?.enabled }"
          role="switch"
          :aria-checked="!!settings?.enabled"
          aria-label="Enable sonic analysis"
          :disabled="!settings || enableSaving || isLocked('sonic_analysis.enabled')"
          :title="isLocked('sonic_analysis.enabled') ? lockTooltip('sonic_analysis.enabled') : (settings?.enabled ? 'Disable' : 'Enable')"
          @click="toggleEnabled"
        >
          <span class="enable-knob" />
        </button>
      </div>
    </SettingsSection>

    <template v-if="settings?.enabled">
      <div class="tiles">
        <MetricTile
          label="Models on disk"
          :value="`${presentCount} / ${totalCount}`"
          :tone="fetcherTone === 'good' ? 'good' : fetcherTone === 'warn' ? 'warn' : 'bad'"
          icon="database"
          :sub="totalSizeMB > 0 ? `${presentSizeMB.toFixed(0)} / ${totalSizeMB.toFixed(0)} MB` : ''"
        />
        <MetricTile
          label="Fetcher"
          :value="fetcherLabel"
          :tone="fetcherTone === 'good' ? 'good' : fetcherTone === 'warn' ? 'warn' : 'bad'"
          icon="cloud"
        />
        <MetricTile
          label="Analyzer"
          :value="holderLabel"
          :tone="holderTone === 'good' ? 'good' : holderTone === 'warn' ? 'warn' : 'neutral'"
          icon="eq"
          :sub="holder?.refs ? `${holder.refs} in use` : ''"
        />
        <MetricTile
          label="Coverage"
          :value="`${coverage.analyzed}`"
          icon="check"
          :sub="coverageSub"
          :tone="coverage.pending === 0 ? 'good' : 'warn'"
        />
      </div>

      <SettingsSection title="Models" icon="hard-drives"
        description="Heya's analyzer ships without weights — they're downloaded on first enable. Upstream files don't carry a version stamp; re-verify if a download looks corrupt.">
        <template #actions>
          <button
            v-if="missingCount > 0"
            class="sv2-btn primary"
            :disabled="fetching"
            @click="triggerFetch"
          >
            <Icon name="cloud" :size="13" />
            {{ fetching ? 'Fetching…' : `Download ${missingCount} missing` }}
          </button>
          <button v-else class="sv2-btn ghost" :disabled="fetching" @click="triggerFetch">
            <Icon name="refresh" :size="13" />
            Re-verify
          </button>
        </template>

        <KVTable :rows="[
          { key: 'Fetcher state',  value: fetcherLabel },
          { key: 'On disk',        value: `${presentCount} / ${totalCount} files (${presentSizeMB.toFixed(0)} / ${totalSizeMB.toFixed(0)} MB)`, mono: true },
          { key: 'Analyzer version', value: `v${status?.analyzer_version ?? '?'} (code-level)`, mono: true },
          { key: 'Text searcher', value: status?.text_searcher?.ready ? 'ready (warm)' : 'idle (cold)' },
          { key: 'Last error', value: lastError ?? '' },
        ]" />

        <div v-if="fetchProgress && fetcherState === 'fetching'" class="fetch-progress">
          <div class="prog-track">
            <div
              class="prog-fill"
              :style="{ width: `${Math.min(100, Math.round((fetchProgress.bytes_done ?? 0) / (fetchProgress.bytes_total || 1) * 100))}%` }"
            />
          </div>
          <div class="prog-meta">
            <span>{{ fetchProgress.files_done }}/{{ fetchProgress.files_total }} files</span>
            <span class="dim">·</span>
            <span>{{ ((fetchProgress.bytes_done ?? 0) / 1024 / 1024).toFixed(1) }} / {{ ((fetchProgress.bytes_total ?? 0) / 1024 / 1024).toFixed(1) }} MB</span>
            <span v-if="fetchProgress.current_file" class="dim ellipsis">· {{ fetchProgress.current_file }}</span>
          </div>
        </div>

        <button class="manifest-toggle" type="button" @click="manifestOpen = !manifestOpen">
          <Icon name="chevright" :size="12" :style="manifestOpen ? { transform: 'rotate(90deg)' } : undefined" />
          {{ manifestOpen ? 'Hide file list' : 'Show file list' }}
        </button>
        <div v-if="manifestOpen" class="manifest">
          <div v-for="g in manifestGroups" :key="g.key" class="manifest-group">
            <div class="manifest-label">{{ g.label }}</div>
            <div v-for="f in g.files" :key="f.name" class="manifest-row">
              <StatusBadge :state="f.present ? 'ok' : 'error'">{{ f.present ? 'ok' : 'missing' }}</StatusBadge>
              <span class="manifest-name mono">{{ f.name }}</span>
              <span class="manifest-size mono">
                <template v-if="f.present">{{ (f.actual_size / 1024 / 1024).toFixed(1) }} MB</template>
                <template v-else>~{{ (f.expected_size / 1024 / 1024).toFixed(1) }} MB</template>
              </span>
            </div>
          </div>
        </div>
      </SettingsSection>

      <SettingsSection title="Runtime holder" icon="pulse"
        description="The analyze_track_facets worker borrows from a singleton holder that keeps the model resident for 5 min after the last lease releases — subsequent tracks don't pay the ~10s cold-load.">
        <KVTable :rows="[
          { key: 'State',           value: holderLabel },
          { key: 'Accelerator',     value: holder?.accelerator ?? '—', mono: true },
          { key: 'CPU prep lanes',  value: holder?.preprocess_ahead ?? '—' },
          { key: 'GPU lanes',       value: holder?.gpu_workers ?? '—' },
          { key: 'Queue workers',   value: holder?.pipeline_workers ?? '—' },
          { key: 'Active leases',   value: holder?.refs ?? 0 },
          { key: 'Loaded',          value: holder?.loaded_at ? `${relTime(holder.loaded_at)} ago` : '' },
          { key: 'Idle unload',     value: holder?.idle_unload_at ? `in ${relTimeFuture(holder.idle_unload_at)}` : '' },
          { key: 'Tracks analyzed', value: (holder?.total_borrows ?? 0) > 0 ? `${holder?.total_borrows} this session` : '' },
        ]" />
      </SettingsSection>

      <SettingsSection title="Pipeline settings" icon="settings">
        <SettingsField
          label="Accelerator"
          description="Auto detects the best inference EP at boot. Dynamic-batch models (classifier heads, base EffNet) always run on CPU when the primary picks CoreML — the EP otherwise recompiles per call and ends up ~8× slower."
          :lockedBy="isLocked('sonic_analysis.accelerator') ? lockTooltip('sonic_analysis.accelerator') : undefined"
          v-slot="{ fieldId }"
        >
          <select
            v-if="settings"
            :id="fieldId"
            v-model="settings.accelerator"
            class="sv2-select"
            :disabled="isLocked('sonic_analysis.accelerator')"
          >
            <option v-for="o in availableAccelerators" :key="o.name" :value="o.name">{{ o.label }}</option>
          </select>

          <div v-if="hiddenAccelerators.length" class="accel-hint">
            Unavailable on this host:
            <span v-for="(a, i) in hiddenAccelerators" :key="a.name">
              <span class="mono">{{ a.label }}</span>
              <span class="dim">{{ a.reason ? ` (${a.reason})` : '' }}</span>{{ i < hiddenAccelerators.length - 1 ? ', ' : '' }}
            </span>
          </div>
        </SettingsField>

        <div class="pipeline-grid">
          <SettingsField
            label="CPU preparation ahead"
            description="Tracks allowed to decode, resample, build spectrograms, detect BPM/key, and wait ready for inference. More keeps the GPU fed but uses more CPU and RAM."
            v-slot="{ fieldId }"
          >
            <input
              v-if="settings"
              :id="fieldId"
              v-model.number="settings.preprocess_ahead"
              class="sv2-number"
              type="number"
              min="1"
              max="32"
              step="1"
            >
          </SettingsField>

          <SettingsField
            label="GPU inference lanes"
            description="Tracks traversing the shared model bundle concurrently. Start at 1; try 2 if the GPU still has idle gaps. Models are shared rather than duplicated in VRAM."
            v-slot="{ fieldId }"
          >
            <input
              v-if="settings"
              :id="fieldId"
              v-model.number="settings.gpu_workers"
              class="sv2-number"
              type="number"
              min="1"
              max="8"
              step="1"
            >
          </SettingsField>
        </div>

        <div class="pipeline-summary">
          <Icon name="info" :size="13" />
          River will start {{ pipelineWorkers }} sonic workers: {{ settings?.preprocess_ahead }} CPU-prep slots + {{ settings?.gpu_workers }} GPU slots. Concurrency changes apply after a worker restart.
        </div>

        <div class="save-bar">
          <button
            v-if="needsWorkerRestart"
            class="sv2-btn danger"
            :disabled="restartingWorker"
            @click="restartWorker"
          >
            <Icon v-if="restartingWorker" name="spinner" :size="13" />
            <Icon v-else name="refresh" :size="13" />
            {{ restartingWorker ? 'Restarting…' : 'Restart worker' }}
          </button>
          <span class="save-spacer" />
          <button class="sv2-btn primary" :disabled="saving" @click="save">
            <Icon v-if="saving" name="spinner" :size="13" />
            {{ saving ? 'Saving…' : 'Save settings' }}
          </button>
        </div>
      </SettingsSection>

      <SettingsSection title="Library coverage" icon="music">
        <template #actions>
          <NuxtLink to="/settings/tasks" class="link-arrow">
            Configure schedule <Icon name="chevright" :size="11" />
          </NuxtLink>
        </template>

        <div class="cov-row">
          <MetricTile label="Analyzed" :value="coverage.analyzed" icon="check" tone="good" />
          <MetricTile label="Pending"  :value="coverage.pending"  icon="timer"
            :tone="coverage.pending > 0 ? 'warn' : 'neutral'" />
        </div>
        <p class="cov-note">
          The Tasks page owns the daily time window and max runtime for the
          analyzer task. {{ (coverage.clap_cleanup_pending ?? 0) > 0
            ? `${coverage.clap_cleanup_pending} existing tracks will receive their extra CLAP windows after full analysis catches up.`
            : '' }}
        </p>
      </SettingsSection>
    </template>

    <SettingsFlash :flash="flash" />
  </div>
</template>

<style scoped>
.enable-card {
  display: flex; align-items: center; justify-content: space-between;
  gap: 18px;
  padding: 18px 20px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  transition: border-color 0.2s ease, background 0.2s ease;
}
.enable-card.on {
  border-color: color-mix(in srgb, var(--good) 30%, transparent);
  background: color-mix(in srgb, var(--good) 4%, transparent);
}
.enable-info { min-width: 0; flex: 1; }
.enable-row { display: flex; align-items: center; gap: 10px; }
.enable-label { font-size: 14px; font-weight: 500; color: var(--fg-0); }
.enable-sub {
  margin: 6px 0 0;
  font-size: 12px; color: var(--fg-3);
  max-width: 560px; line-height: 1.5;
}
.enable-toggle {
  width: 48px; height: 26px;
  border-radius: 100px;
  background: rgb(var(--ink) / 0.08);
  border: 0;
  position: relative; cursor: pointer;
  flex-shrink: 0;
  transition: background 0.2s ease;
}
.enable-toggle.on { background: var(--good); }
.enable-toggle:disabled { opacity: 0.5; cursor: not-allowed; }
.enable-knob {
  position: absolute; top: 3px; left: 3px;
  width: 20px; height: 20px;
  border-radius: 50%; background: #fff;
  transition: transform 0.2s ease;
  box-shadow: 0 1px 3px rgb(var(--shade) / 0.4);
}
.enable-toggle.on .enable-knob { transform: translateX(22px); }

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 8px;
  margin-bottom: 28px;
  margin-top: 12px;
}

.fetch-progress { margin-top: 14px; }
.prog-track {
  height: 6px; border-radius: 3px;
  background: var(--bg-0); overflow: hidden;
}
.prog-fill {
  height: 100%;
  background: var(--gold);
  transition: width 0.3s ease;
}
.prog-meta {
  display: flex; gap: 6px; align-items: center;
  font-family: var(--font-mono); font-size: 11px;
  color: var(--fg-2);
  margin-top: 6px;
}
.prog-meta .dim { color: var(--fg-4); }
.prog-meta .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; flex: 1; }

.manifest-toggle {
  background: transparent;
  border: 0;
  color: var(--fg-3);
  font-size: 11.5px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  padding: 10px 0 4px;
  margin-top: 4px;
}
.manifest-toggle:hover { color: var(--fg-1); }
.manifest {
  display: flex; flex-direction: column;
  gap: 14px;
  border-top: 1px solid var(--border);
  padding-top: 12px;
}
.manifest-group { display: flex; flex-direction: column; gap: 4px; }
.manifest-label {
  font-size: 10px; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--fg-3); margin-bottom: 4px;
}
.manifest-row {
  display: grid;
  grid-template-columns: 70px 1fr auto;
  align-items: center;
  gap: 10px;
  padding: 4px 0;
  font-size: 11.5px;
}
.manifest-name { color: var(--fg-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.manifest-size { color: var(--fg-3); }
.mono { font-family: var(--font-mono); font-size: 11px; }
.dim { color: var(--fg-4); }

.sv2-select {
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  padding: 8px 12px;
  min-width: 240px;
  cursor: pointer;
  outline: none;
  transition: border-color 0.12s;
}
.sv2-select:focus { border-color: var(--gold); }
.sv2-select:disabled { opacity: 0.5; cursor: not-allowed; }
.sv2-number {
  width: 92px;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  outline: none;
  background: var(--bg-0);
  color: var(--fg-0);
  font-family: var(--font-mono);
  font-size: 13px;
}
.sv2-number:focus { border-color: var(--gold); }

.pipeline-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.pipeline-summary {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-top: 12px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--fg-3);
  font-size: 11px;
  line-height: 1.45;
}

.accel-hint {
  margin-top: 6px;
  font-size: 11px;
  color: var(--fg-3);
  line-height: 1.5;
}

.save-bar {
  display: flex; align-items: center; gap: 12px;
  padding: 16px 0 0;
}
.save-spacer { flex: 1; }

.cov-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 8px;
}
.cov-note { margin: 10px 0 0; font-size: 12px; color: var(--fg-3); line-height: 1.5; }

.link-arrow {
  display: inline-flex; align-items: center; gap: 2px;
  font-size: 11px; color: var(--fg-3); text-decoration: none;
}
.link-arrow:hover { color: var(--gold); }

@media (max-width: 720px) {
  .sv2-select { min-width: 0; width: 100%; }
  .enable-card { flex-wrap: wrap; }
  .pipeline-grid { grid-template-columns: 1fr; }

  /* minmax(180px) only fits 1 column at 390px — force 2. */
  .tiles, .cov-row { grid-template-columns: repeat(2, 1fr); }
}
</style>
