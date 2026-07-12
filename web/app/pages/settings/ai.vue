<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

const { $heya } = useNuxtApp()
const { isLocked, lockTooltip, ensure: ensureSources } = useConfigSources()
const { flash } = useFlash()
import { aiCatalogQuery, aiSettingsQuery, aiStatusQuery } from '~/queries/intelligence'
import type { AIChatResponse, AISettings as AISettingsView } from '~/queries/intelligence'

const statusData = useQuery(aiStatusQuery())
const settingsData = useQuery(aiSettingsQuery())
const catalogData = useQuery(aiCatalogQuery())
const status = computed(() => statusData.data.value ?? null)
const settings = ref<AISettingsView | null>(null)
const providers = computed(() => catalogData.data.value?.providers ?? [])
const localModels = computed(() => catalogData.data.value?.local_models ?? [])
const providerModels = ref<string[]>([])

const saving = ref(false)
const downloading = ref(false)
const loadingModels = ref(false)
const apiKeyDraft = ref('')
const claudeTokenDraft = ref('')

const isOff = computed(() => (settings.value?.mode ?? 'off') === 'off')
const isLocal = computed(() => settings.value?.mode === 'local')
const isExternal = computed(() => settings.value?.mode === 'external')
const isAgent = computed(() => settings.value?.mode === 'claude' || settings.value?.mode === 'codex')
const isClaude = computed(() => settings.value?.mode === 'claude')
const isCodex = computed(() => settings.value?.mode === 'codex')
const agentModel = computed({
  get: () => isClaude.value ? (settings.value?.claude_model ?? '') : (settings.value?.codex_model ?? ''),
  set: (value: string) => {
    if (!settings.value) return
    if (isClaude.value) settings.value.claude_model = value
    else settings.value.codex_model = value
  },
})
const agentModelField = computed(() => isClaude.value ? 'ai.claude_model' : 'ai.codex_model')
const agentModelOptions = computed(() => [...new Set([agentModel.value, ...providerModels.value].filter(Boolean))])
const isCustomProvider = computed(() => settings.value?.provider === 'custom')
const selectedProvider = computed(() => providers.value.find(p => p.id === settings.value?.provider))
const selectedLocalModel = computed(() => localModels.value.find(m => m.id === settings.value?.local_model))

const dl = computed(() => status.value?.local)
const dlActive = computed(() => dl.value?.download_state === 'downloading')
const dlPercent = computed(() => {
  const p = dl.value?.download_progress
  if (!p || !p.bytes_total) return 0
  return Math.min(100, Math.round((p.bytes_done / p.bytes_total) * 100))
})
const artifactsReady = computed(() => !!dl.value?.server_present && !!dl.value?.model_present)

// --- test console ---
const testSystem = ref('')
const testPrompt = ref('')
const testing = ref(false)
const testResult = ref<AIChatResponse | null>(null)
const testError = ref('')

async function loadStatus() {
  try {
    await statusData.refetch()
    downloading.value = status.value?.local.download_state === 'downloading'
  } catch { /* transient poll failure — keep last snapshot */ }
}
async function loadSettings() {
  await settingsData.refetch()
  if (settingsData.data.value) settings.value = structuredClone(settingsData.data.value)
}
async function loadCatalog() {
  await catalogData.refetch()
}
watch(() => settingsData.data.value, value => {
  if (value) settings.value = structuredClone(value)
}, { immediate: true })

async function save() {
  if (!settings.value || saving.value) return
  saving.value = true
  flash.value = null
  try {
    const body = {
      mode: settings.value.mode,
      provider: settings.value.provider,
      api_key: apiKeyDraft.value, // empty = keep stored key
      model: settings.value.model,
      base_url: settings.value.base_url,
      local_model: settings.value.local_model,
      local_backend: settings.value.local_backend,
      context_size: Number(settings.value.context_size) || 0,
      claude_model: settings.value.claude_model,
      codex_model: settings.value.codex_model,
      claude_token: claudeTokenDraft.value, // empty = keep stored token
    }
    settings.value = await $heya('/api/ai/settings', { method: 'PUT', body: body as any }) as AISettingsView
    apiKeyDraft.value = ''
    claudeTokenDraft.value = ''
    flash.value = { kind: 'ok', text: 'AI settings saved.' }
    loadStatus()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Save failed.' }
  } finally {
    saving.value = false
  }
}

async function setMode(mode: string) {
  if (!settings.value || settings.value.mode === mode) return
  settings.value.mode = mode
  providerModels.value = []
  await save()
  if (mode === 'claude' || mode === 'codex') await fetchProviderModels()
}

async function startDownload() {
  downloading.value = true
  try {
    await $heya('/api/ai/local/download', { method: 'POST', body: {} as any })
    flash.value = { kind: 'ok', text: 'Download started.' }
  } catch (e: any) {
    downloading.value = false
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Download failed to start.' }
  }
}

async function stopRuntime() {
  try {
    await $heya('/api/ai/local/stop', { method: 'POST', body: {} as any })
    flash.value = { kind: 'ok', text: 'Local runtime stopped.' }
    loadStatus()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Stop failed.' }
  }
}

async function fetchProviderModels() {
  loadingModels.value = true
  providerModels.value = []
  flash.value = null
  try {
    const res = await $heya('/api/ai/models') as { models: string[] }
    providerModels.value = res.models
    if (!res.models.length) flash.value = { kind: 'warn', text: 'Provider returned no models.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Could not list models — check key and provider.' }
  } finally {
    loadingModels.value = false
  }
}

async function runTest() {
  if (!testPrompt.value.trim() || testing.value) return
  testing.value = true
  testResult.value = null
  testError.value = ''
  try {
    testResult.value = await $heya('/api/ai/chat', {
      method: 'POST',
      body: { prompt: testPrompt.value, system: testSystem.value || undefined } as any,
    }) as AIChatResponse
  } catch (e: any) {
    testError.value = e?.data?.detail ?? e?.message ?? 'Request failed.'
  } finally {
    testing.value = false
  }
}

let pollTimer: ReturnType<typeof setInterval> | null = null
onMounted(async () => {
  await Promise.all([loadSettings(), loadCatalog(), loadStatus(), ensureSources()])
  if (isAgent.value) void fetchProviderModels()
  let last = 0
  pollTimer = setInterval(() => {
    const interval = dlActive.value || downloading.value ? 1500 : 5000
    const now = Date.now()
    if (now - last >= interval) { last = now; loadStatus() }
  }, 1000)
})
onBeforeUnmount(() => { if (pollTimer) clearInterval(pollTimer) })
</script>

<template>
  <div>
    <SettingsContextHero
      title="AI providers"
      icon="wand"
      eyebrow="Media intelligence · Language models"
      description="Choose a private local model, an external API, or an authenticated subscription for smart collections, playlists, and recommendations."
    />

    <SettingsSection
      title="Mode"
      icon="power"
      :lockedBy="isLocked('ai.mode') ? lockTooltip('ai.mode') : undefined"
    >
      <div class="mode-row">
        <button
          v-for="m in ['off', 'local', 'external', 'claude', 'codex']" :key="m"
          class="mode-btn" :class="{ active: settings?.mode === m }"
          :aria-pressed="settings?.mode === m"
          :disabled="saving || isLocked('ai.mode')"
          @click="setMode(m)"
        >
          <span class="mode-name">{{ m === 'off' ? 'Off' : m === 'local' ? 'Local model' : m === 'external' ? 'External provider' : m === 'claude' ? 'Claude subscription' : 'Codex subscription' }}</span>
          <span class="mode-desc">
            {{ m === 'off' ? 'Disabled entirely' : m === 'local' ? 'Private, runs on this machine' : m === 'external' ? 'Bring your own API key' : 'Use your existing account' }}
          </span>
        </button>
      </div>
      <div v-if="status && !isOff" class="mode-status">
        <StatusBadge :state="status.ready ? 'ok' : 'warn'">{{ status.ready ? 'Ready' : 'Not ready' }}</StatusBadge>
        <span v-if="!status.ready && status.detail" class="mode-detail">{{ status.detail }}</span>
        <span v-else-if="status.ready && isLocal" class="mode-detail">
          {{ dl?.running ? `llama-server warm (${dl?.running_model})` : 'llama-server cold — starts on first request' }}
        </span>
        <span v-else-if="status.ready && isAgent" class="mode-detail">
          {{ status.agent.provider }} · {{ status.model }} · no Heya tools
        </span>
      </div>
    </SettingsSection>

    <SettingsSection
      v-if="isAgent"
      :title="isClaude ? 'Claude Agent' : 'Codex'"
      icon="sparkle"
      description="Heya launches the official native CLI with an isolated working directory, minimal environment, structured output, and no Heya tools enabled."
    >
      <div class="artifact-card" :class="{ ok: status?.agent.authenticated && status?.agent.binary_present }">
        <div class="artifact-info">
          <StatusBadge :state="status?.agent.binary_present ? 'ok' : 'error'">
            CLI {{ status?.agent.binary_present ? 'installed' : 'missing' }}
          </StatusBadge>
          <StatusBadge :state="status?.agent.authenticated ? 'ok' : 'warn'">
            {{ status?.agent.authenticated ? 'Authenticated' : 'Login required' }}
          </StatusBadge>
        </div>
      </div>

      <SettingsField
        v-if="isClaude"
        label="Subscription token"
        description="Run `claude setup-token` on a trusted machine, then paste its output here. The token is stored server-side and never echoed back."
        :lockedBy="isLocked('ai.claude_token') ? lockTooltip('ai.claude_token') : undefined"
        v-slot="{ fieldId }"
      >
        <input
          :id="fieldId"
          v-model="claudeTokenDraft" type="password" class="sv2-input" autocomplete="off"
          :placeholder="settings?.claude_token_set ? `token set (${settings.claude_token_hint}) — enter to replace` : 'paste Claude setup token'"
          :disabled="saving || isLocked('ai.claude_token')"
          @blur="claudeTokenDraft && save()"
        >
      </SettingsField>

      <SettingsField
        v-if="isCodex"
        label="ChatGPT login"
        description="Codex device login is stored in Heya's persistent data volume. Run this once inside the Heya container:"
      >
        <code class="setup-command">codex -c cli_auth_credentials_store=&quot;file&quot; login --device-auth</code>
      </SettingsField>

      <SettingsField label="Model" :lockedBy="isLocked(agentModelField) ? lockTooltip(agentModelField) : undefined" v-slot="{ fieldId }">
        <div class="model-row">
          <select
            :id="fieldId"
            v-model="agentModel" class="sv2-select"
            :disabled="saving || isLocked(agentModelField)"
            @change="save"
          >
            <option v-for="m in agentModelOptions" :key="m" :value="m">{{ m }}</option>
          </select>
          <button class="sv2-btn ghost" :disabled="loadingModels" @click="fetchProviderModels">
            <Icon :name="loadingModels ? 'spinner' : 'refresh'" :size="13" />
            {{ loadingModels ? 'Loading…' : providerModels.length ? `${providerModels.length} models` : 'Fetch models' }}
          </button>
        </div>
        <p v-if="isClaude" class="field-note">Aliases automatically follow the current model in each Claude tier; exact model IDs also work.</p>
      </SettingsField>
    </SettingsSection>

    <SettingsSection
      v-if="isLocal"
      title="Local runtime"
      icon="cpu"
      :description="`Managed llama.cpp (${dl?.build ?? '…'}) serving a curated GGUF. Downloads once, runs on demand, unloads after 10 idle minutes.`"
    >
      <template #actions>
        <button v-if="dl?.running" class="sv2-btn ghost" @click="stopRuntime">
          <Icon name="power" :size="13" /> Stop runtime
        </button>
      </template>

      <SettingsField label="Model" :lockedBy="isLocked('ai.local_model') ? lockTooltip('ai.local_model') : undefined" v-slot="{ fieldId }">
        <select :id="fieldId" v-model="settings!.local_model" class="sv2-select" :disabled="saving || isLocked('ai.local_model')" @change="save">
          <option v-for="m in localModels" :key="m.id" :value="m.id">{{ m.label }}</option>
        </select>
        <p v-if="selectedLocalModel" class="field-note">
          {{ (selectedLocalModel.size / 1024 / 1024 / 1024).toFixed(1) }} GB download · {{ selectedLocalModel.ram_hint }} RAM{{ selectedLocalModel.notes ? ` · ${selectedLocalModel.notes}` : '' }}
        </p>
      </SettingsField>

      <SettingsField
        label="Context window"
        description="Tokens of context per request. Bigger costs RAM (KV cache) — 16384 is plenty for Heya's own features."
        :lockedBy="isLocked('ai.context_size') ? lockTooltip('ai.context_size') : undefined"
        v-slot="{ fieldId }"
      >
        <select :id="fieldId" v-model.number="settings!.context_size" class="sv2-select" :disabled="saving || isLocked('ai.context_size')" @change="save">
          <option v-for="c in [4096, 8192, 16384, 32768, 65536]" :key="c" :value="c">{{ c.toLocaleString() }}</option>
        </select>
      </SettingsField>

      <div class="artifact-card" :class="{ ok: artifactsReady }">
        <div class="artifact-info">
          <StatusBadge :state="artifactsReady ? 'ok' : dlActive ? 'warn' : 'idle'">
            {{ artifactsReady ? 'Installed' : dlActive ? 'Downloading' : 'Not downloaded' }}
          </StatusBadge>
          <span class="artifact-text">
            llama-server {{ dl?.server_present ? '✓' : '✗' }} · model {{ dl?.model_present ? '✓' : '✗' }}
          </span>
        </div>
        <button
          v-if="!artifactsReady"
          class="sv2-btn primary"
          :disabled="dlActive || downloading"
          @click="startDownload"
        >
          <Icon name="cloud" :size="13" />
          {{ dlActive || downloading ? 'Downloading…' : `Download (~${((selectedLocalModel?.size ?? 0) / 1024 / 1024 / 1024).toFixed(1)} GB)` }}
        </button>
      </div>

      <div v-if="dlActive && dl?.download_progress" class="fetch-progress">
        <div class="prog-track"><div class="prog-fill" :style="{ width: dlPercent + '%' }" /></div>
        <div class="prog-meta">
          <span>{{ dlPercent }}%</span>
          <span class="dim">·</span>
          <span>{{ ((dl.download_progress.bytes_done ?? 0) / 1024 / 1024).toFixed(0) }} / {{ ((dl.download_progress.bytes_total ?? 0) / 1024 / 1024).toFixed(0) }} MB</span>
          <span v-if="dl.download_progress.current_file" class="dim ellipsis">· {{ dl.download_progress.current_file }}</span>
        </div>
      </div>
      <p v-if="dl?.download_error" class="dl-error">{{ dl.download_error }}</p>
    </SettingsSection>

    <SettingsSection
      v-if="isExternal"
      title="External provider"
      icon="cloud"
      description="Any OpenAI-compatible API. The key is stored server-side and never echoed back."
    >
      <SettingsField label="Provider" :lockedBy="isLocked('ai.provider') ? lockTooltip('ai.provider') : undefined" v-slot="{ fieldId }">
        <select :id="fieldId" v-model="settings!.provider" class="sv2-select" :disabled="saving || isLocked('ai.provider')" @change="providerModels = []; save()">
          <option v-for="p in providers" :key="p.id" :value="p.id">{{ p.label }}</option>
        </select>
      </SettingsField>

      <SettingsField v-if="isCustomProvider" label="Base URL" description="OpenAI-compatible API root, e.g. http://my-box:8000/v1" :lockedBy="isLocked('ai.base_url') ? lockTooltip('ai.base_url') : undefined" v-slot="{ fieldId }">
        <input :id="fieldId" v-model="settings!.base_url" type="text" class="sv2-input" placeholder="https://…/v1" autocomplete="off" :disabled="saving || isLocked('ai.base_url')" @blur="save">
      </SettingsField>

      <SettingsField
        v-if="selectedProvider?.needs_key || isCustomProvider"
        label="API key"
        :lockedBy="isLocked('ai.api_key') ? lockTooltip('ai.api_key') : undefined"
        v-slot="{ fieldId }"
      >
        <input
          :id="fieldId"
          v-model="apiKeyDraft" type="password" class="sv2-input" autocomplete="off"
          :placeholder="settings?.api_key_set ? `key set (${settings.api_key_hint}) — enter to replace` : 'paste your API key'"
          :disabled="saving || isLocked('ai.api_key')"
          @blur="apiKeyDraft && save()"
        >
      </SettingsField>

      <SettingsField label="Model" :lockedBy="isLocked('ai.model') ? lockTooltip('ai.model') : undefined" v-slot="{ fieldId }">
        <div class="model-row">
          <input
            :id="fieldId"
            v-model="settings!.model" type="text" class="sv2-input" list="ai-provider-models"
            placeholder="e.g. anthropic/claude-sonnet-5" autocomplete="off"
            :disabled="saving || isLocked('ai.model')"
            @blur="save"
          >
          <datalist id="ai-provider-models">
            <option v-for="m in providerModels" :key="m" :value="m" />
          </datalist>
          <button class="sv2-btn ghost" :disabled="loadingModels" @click="fetchProviderModels">
            <Icon :name="loadingModels ? 'spinner' : 'refresh'" :size="13" />
            {{ loadingModels ? 'Loading…' : providerModels.length ? `${providerModels.length} models` : 'Fetch models' }}
          </button>
        </div>
      </SettingsField>
    </SettingsSection>

    <SettingsSection
      v-if="!isOff"
      title="Test console"
      icon="pulse"
      description="Round-trip a prompt through the active configuration. Optional context becomes the system prompt — use it to check the model actually honors instructions."
    >
      <SettingsField label="Context (optional)" v-slot="{ fieldId }">
        <textarea
          :id="fieldId"
          v-model="testSystem" class="sv2-input test-textarea" rows="2"
          placeholder="e.g. You are Heya's media assistant. The user's favorite film is Blade Runner (1982)."
        />
      </SettingsField>
      <SettingsField label="Prompt" v-slot="{ fieldId }">
        <div class="model-row">
          <input
            :id="fieldId"
            v-model="testPrompt" type="text" class="sv2-input"
            placeholder='e.g. "Say hello world" or "What is my favorite film?"'
            @keydown.enter="runTest"
          >
          <button class="sv2-btn primary" :disabled="testing || !testPrompt.trim()" @click="runTest">
            <Icon :name="testing ? 'spinner' : 'pulse'" :size="13" />
            {{ testing ? (isLocal && !dl?.running ? 'Starting model…' : 'Thinking…') : 'Send' }}
          </button>
        </div>
      </SettingsField>

      <div v-if="testResult" class="test-card ok">
        <p class="test-reply">{{ testResult.content }}</p>
        <div class="test-meta">
          <span>{{ testResult.mode }}</span>
          <span class="dim">·</span>
          <span>{{ testResult.model || 'model n/a' }}</span>
          <span class="dim">·</span>
          <span>{{ testResult.prompt_tokens }}+{{ testResult.completion_tokens }} tokens</span>
          <span class="dim">·</span>
          <span>{{ (testResult.duration_ms / 1000).toFixed(1) }}s</span>
        </div>
      </div>
      <div v-else-if="testError" class="test-card err">
        <StatusBadge state="error">Failed</StatusBadge>
        <span class="test-err-text">{{ testError }}</span>
      </div>
    </SettingsSection>

    <SettingsFlash :flash="flash" />
  </div>
</template>

<style scoped>
.mode-row {
  display: grid;
  grid-template-columns: repeat(5, minmax(140px, 1fr));
  gap: 8px;
  overflow-x: auto;
  padding-bottom: 2px;
}
.setup-command {
  display: block;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--fg-1);
  font-size: 12px;
  user-select: all;
}
.mode-btn {
  display: flex; flex-direction: column; align-items: flex-start; gap: 4px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  cursor: pointer;
  text-align: left;
  transition: border-color 0.15s ease, background 0.15s ease;
}
.mode-btn:hover:not(:disabled) { border-color: var(--fg-4); }
.mode-btn.active {
  border-color: color-mix(in srgb, var(--good) 40%, transparent);
  background: color-mix(in srgb, var(--good) 5%, transparent);
}
.mode-btn:disabled { opacity: 0.55; cursor: not-allowed; }
.mode-name { font-size: 13.5px; font-weight: 500; color: var(--fg-0); }
.mode-desc { font-size: 11.5px; color: var(--fg-3); }
.mode-status {
  display: flex; align-items: center; gap: 10px;
  margin-top: 12px;
}
.mode-detail { font-size: 12px; color: var(--fg-3); }

.field-note { margin: 6px 0 0; font-size: 11.5px; color: var(--fg-3); }

.artifact-card {
  display: flex; align-items: center; justify-content: space-between; gap: 14px;
  margin-top: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.artifact-card.ok { border-color: color-mix(in srgb, var(--good) 30%, transparent); }
.artifact-info { display: flex; align-items: center; gap: 10px; min-width: 0; }
.artifact-text { font-size: 12px; color: var(--fg-2); font-family: var(--font-mono); }
.dl-error { margin: 10px 0 0; font-size: 12px; color: var(--bad, #e5484d); }

.image-artifacts { margin-top: 14px; border: 1px solid var(--border); border-radius: var(--r-md); overflow: hidden; }
.image-artifact-row { display: grid; grid-template-columns: 80px minmax(0, 1fr) 72px auto; align-items: center; gap: 10px; padding: 10px 12px; background: var(--bg-2); border-bottom: 1px solid var(--border); }
.image-artifact-row:last-child { border-bottom: 0; }
.artifact-role { font-size: 11px; text-transform: uppercase; letter-spacing: .06em; color: var(--fg-3); }
.artifact-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 11px var(--font-mono); color: var(--fg-1); }
.artifact-size { text-align: right; font: 11px var(--font-mono); color: var(--fg-3); }
.image-test-card { display: grid; gap: 10px; margin-top: 16px; padding: 14px 16px; border: 1px solid var(--border); border-radius: var(--r-md); background: var(--bg-2); }
.image-test-card .sv2-btn { justify-self: start; }
.generated-preview { display: grid; gap: 8px; color: var(--fg-3); font: 11px var(--font-mono); }
.generated-preview img { width: min(100%, 512px); aspect-ratio: 1; object-fit: contain; border-radius: var(--r-md); border: 1px solid var(--border); background: var(--bg-0); }

.fetch-progress { margin-top: 14px; }
.prog-track { height: 6px; border-radius: 3px; background: var(--bg-0); overflow: hidden; }
.prog-fill { height: 100%; background: var(--gold); transition: width 0.3s ease; }
.prog-meta {
  display: flex; gap: 6px; align-items: center;
  font-family: var(--font-mono); font-size: 11px;
  color: var(--fg-2);
  margin-top: 6px;
}
.prog-meta .dim { color: var(--fg-4); }
.prog-meta .ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; flex: 1; }

.model-row { display: flex; gap: 8px; align-items: center; }
.model-row .sv2-input { flex: 1; }
.model-row .sv2-btn { flex-shrink: 0; white-space: nowrap; }

/* .sv2-input is a per-page convention (defined in each settings page's scoped
   block, only .sv2-btn is global) — without this the inputs render as square
   browser defaults. */
.sv2-input {
  width: 100%;
  max-width: 460px;
  padding: 9px 12px;
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  color: var(--fg-0);
  font-size: 13px;
  outline: none;
  transition: border-color 0.12s, background 0.12s;
}
.sv2-input::placeholder { color: var(--fg-4); }
.sv2-input:focus { border-color: var(--gold); background: var(--bg-1); }
.sv2-input:disabled { opacity: 0.5; cursor: not-allowed; }

.test-textarea {
  resize: vertical;
  min-height: 44px;
  max-width: none;
  width: 100%;
  font-family: inherit;
  line-height: 1.5;
}

.model-row .sv2-input,
.test-console-row .sv2-input { max-width: none; }

.test-card {
  margin-top: 14px;
  padding: 14px 16px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: var(--bg-2);
}
.test-card.ok { border-color: color-mix(in srgb, var(--good) 30%, transparent); }
.test-card.err {
  border-color: color-mix(in srgb, var(--bad) 35%, transparent);
  display: flex; align-items: center; gap: 10px;
}
.test-reply {
  margin: 0;
  font-size: 13px; line-height: 1.55; color: var(--fg-0);
  white-space: pre-wrap;
}
.test-meta {
  display: flex; gap: 6px; align-items: center;
  margin-top: 10px;
  font-family: var(--font-mono); font-size: 11px; color: var(--fg-2);
}
.test-meta .dim { color: var(--fg-4); }
.test-err-text { font-size: 12px; color: var(--fg-1); }

.sv2-select {
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  color: var(--fg-0);
  font-size: 13px;
  padding: 9px 12px;
  min-width: 260px;
  max-width: 460px;
  cursor: pointer;
  outline: none;
  transition: border-color 0.12s, background 0.12s;
}
.sv2-select:hover:not(:disabled) { border-color: var(--fg-4); }
.sv2-select:focus { border-color: var(--gold); background: var(--bg-1); }
.sv2-select:disabled { opacity: 0.5; cursor: not-allowed; }

@media (max-width: 900px) {
  .mode-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    overflow: visible;
  }
  .artifact-card { align-items: flex-start; flex-direction: column; }
  .artifact-info { flex-wrap: wrap; }
}

@media (max-width: 520px) {
  .mode-row { grid-template-columns: 1fr; }
  .mode-btn { padding: 12px 13px; }
  .mode-status, .test-meta { align-items: flex-start; flex-wrap: wrap; }
  .model-row { align-items: stretch; flex-direction: column; }
  .model-row .sv2-btn { justify-content: center; }
  .sv2-input, .sv2-select { width: 100%; min-width: 0; max-width: none; }
  .image-artifact-row { grid-template-columns: 64px minmax(0, 1fr); }
  .image-artifact-row .artifact-size { text-align: left; }
}
</style>
