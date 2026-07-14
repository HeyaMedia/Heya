<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { generateLocalImage, imageGenerationCatalogQuery, imageGenerationStatusQuery } from '~/queries/intelligence'

const { $heya } = useNuxtApp()
const { flash } = useFlash()
const statusData = useQuery(imageGenerationStatusQuery())
const catalogData = useQuery(imageGenerationCatalogQuery())
const status = computed(() => statusData.data.value ?? null)
const fallbackModel = {
  id: 'z-image-turbo-q4', label: 'Z-Image Turbo Q4 — recommended', license: 'Apache 2.0',
  ram_hint: '16 GB recommended (CPU supported)', default_width: 768, default_height: 768,
  default_steps: 8, default_cfg: 1, artifacts: [],
}
const models = computed(() => catalogData.data.value?.models?.length ? catalogData.data.value.models : [fallbackModel])
const selectedModel = ref('z-image-turbo-q4')
const selectedDevice = ref('auto')
const downloading = ref(false)
const prompt = ref('A cinematic retro-futurist media library floating in deep space, orange and teal light, detailed digital illustration, no text')
const generating = ref(false)
const generated = ref<{ url: string, duration_ms: number } | null>(null)
const testError = ref('')
const apiError = computed(() => statusData.error.value || catalogData.error.value)
const dlActive = computed(() => status.value?.download_state === 'downloading')
const ready = computed(() => !!status.value?.runtime_present && !!status.value?.model_present)
const dlPercent = computed(() => {
  const p = status.value?.progress
  return p?.bytes_total ? Math.min(100, Math.round(p.bytes_done / p.bytes_total * 100)) : 0
})

async function refresh() {
  try {
    await statusData.refetch()
    downloading.value = status.value?.download_state === 'downloading'
  } catch { /* keep the last status during transient polling failures */ }
}

async function fetchArtifacts() {
  downloading.value = true
  try {
    await $heya('/api/ai/images/fetch', { method: 'POST', body: { model: selectedModel.value, backend: 'auto' } as any })
    flash.value = { kind: 'ok', text: 'Image runtime download started.' }
    void refresh()
  } catch (e: any) {
    downloading.value = false
    flash.value = { kind: 'err', text: e?.data?.detail ?? e?.message ?? 'Download failed to start.' }
  }
}

async function generateTest() {
  if (!prompt.value.trim() || generating.value) return
  generating.value = true
  generated.value = null
  testError.value = ''
  try {
    generated.value = await generateLocalImage({ prompt: prompt.value, model_id: selectedModel.value, backend: 'auto', device: selectedDevice.value })
  } catch (e: any) {
    const detail = String(e?.data?.detail ?? e?.message ?? 'Image generation failed.')
    testError.value = /OutOfDeviceMemory|Device memory allocation .* failed/i.test(detail)
      ? 'The selected GPU ran out of memory. Choose Automatic (best fit) or another compute device and try again.'
      : detail
  } finally { generating.value = false }
}

let timer: ReturnType<typeof setInterval> | null = null
onMounted(async () => {
  await Promise.allSettled([statusData.refetch(), catalogData.refetch()])
  timer = setInterval(() => void refresh(), dlActive.value || downloading.value ? 1500 : 5000)
})
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
</script>

<template>
  <div>
    <SettingsContextHero
      title="Image generation"
      icon="image"
      eyebrow="Media intelligence · Generative artwork"
      description="Generate artwork locally with Z-Image Turbo through a managed stable-diffusion.cpp runtime. CPU-only servers are supported."
    />

    <SettingsFlash :flash="flash" />

    <SettingsSection
      title="Local runtime"
      icon="cpu"
      :description="`stable-diffusion.cpp ${status?.build ?? '…'} · ${status?.backend ?? 'auto'} backend`"
    >
      <div v-if="apiError" class="api-error">
        <StatusBadge state="error">Backend unavailable</StatusBadge>
        <span>The image-generation API could not be reached. Restart or redeploy the Heya backend so it includes the new image routes.</span>
      </div>

      <SettingsField label="Model" description="The Qwen3 text encoder is reused from the local LLM installation when present." v-slot="{ fieldId }">
        <select :id="fieldId" v-model="selectedModel" class="sv2-select">
          <option v-for="model in models" :key="model.id" :value="model.id">{{ model.label }}</option>
        </select>
        <p v-if="models[0]" class="field-note">
          {{ models[0].license }} · {{ models[0].ram_hint }} · {{ models[0].default_steps }} steps at {{ models[0].default_width }}×{{ models[0].default_height }}
        </p>
      </SettingsField>

      <SettingsField
        label="Compute device"
        description="Automatic checks free memory for every available device at generation time, places model components where they fit, and falls back to CPU when needed."
        v-slot="{ fieldId }"
      >
        <select :id="fieldId" v-model="selectedDevice" class="sv2-select" :disabled="!ready">
          <option value="auto">Automatic (best fit)</option>
          <option v-for="device in status?.devices ?? []" :key="device.name" :value="device.name">
            {{ device.description }} ({{ device.name }})
          </option>
        </select>
        <p v-if="status?.device_error" class="dl-error">{{ status.device_error }}</p>
      </SettingsField>

      <div class="image-artifacts">
        <div v-for="artifact in status?.artifacts" :key="artifact.role" class="image-artifact-row">
          <span class="artifact-role">{{ artifact.role }}</span>
          <span class="artifact-name">{{ artifact.name }}</span>
          <span class="artifact-size">{{ (artifact.size / 1024 / 1024 / 1024).toFixed(2) }} GiB</span>
          <StatusBadge :state="artifact.present ? 'ok' : 'idle'">
            {{ artifact.shared ? 'Shared from LLM' : artifact.present ? 'Installed' : 'Missing' }}
          </StatusBadge>
        </div>
      </div>

      <div class="artifact-card" :class="{ ok: ready }">
        <div class="artifact-info">
          <StatusBadge :state="ready ? 'ok' : dlActive ? 'warn' : 'idle'">
            {{ ready ? 'Ready' : dlActive ? 'Downloading' : 'Not downloaded' }}
          </StatusBadge>
          <span class="artifact-text">{{ ((status?.download_bytes ?? 0) / 1024 / 1024 / 1024).toFixed(2) }} GiB additional</span>
        </div>
        <button v-if="!ready" class="sv2-btn primary" :disabled="!!apiError || !status || dlActive || downloading" @click="fetchArtifacts">
          <Icon name="cloud" :size="13" />
          {{ dlActive || downloading ? 'Downloading…' : status ? `Fetch ${(status.download_bytes / 1024 / 1024 / 1024).toFixed(2)} GiB` : 'Status unavailable' }}
        </button>
      </div>

      <div v-if="dlActive && status?.progress" class="fetch-progress">
        <div class="prog-track"><div class="prog-fill" :style="{ width: dlPercent + '%' }" /></div>
        <div class="prog-meta">
          <span>{{ dlPercent }}%</span><span class="dim">·</span>
          <span>{{ ((status.progress.bytes_done ?? 0) / 1024 / 1024).toFixed(0) }} / {{ ((status.progress.bytes_total ?? 0) / 1024 / 1024).toFixed(0) }} MB</span>
          <span v-if="status.progress.current_file" class="dim ellipsis">· {{ status.progress.current_file }}</span>
        </div>
      </div>
      <p v-if="status?.download_error" class="dl-error">{{ status.download_error }}</p>
    </SettingsSection>

    <SettingsSection title="Test generation" icon="sparkle" description="Run one request through the same frontend API available to artwork features.">
      <div class="image-test-card">
        <textarea v-model="prompt" class="sv2-input test-textarea" rows="3" placeholder="Describe an image…" />
        <button class="sv2-btn primary" :disabled="!ready || generating || !prompt.trim()" @click="generateTest">
          <Icon :name="generating ? 'spinner' : 'sparkle'" :size="13" />
          {{ generating ? 'Generating…' : 'Generate test image' }}
        </button>
        <div v-if="testError" class="generation-error" role="alert" aria-live="assertive">
          <Icon name="warning" :size="13" aria-hidden="true" />
          <span>{{ testError }}</span>
        </div>
        <p v-if="!ready" class="field-note">Fetch the artifacts before generating. CPU-only generation may take several minutes.</p>
        <div v-if="generated" class="generated-preview">
          <LoadingImage :src="generated.url" alt="Generated Z-Image test result" />
          <span>{{ (generated.duration_ms / 1000).toFixed(1) }} seconds</span>
        </div>
      </div>
    </SettingsSection>
  </div>
</template>

<style scoped>
.field-note { margin: 6px 0 0; font-size: 11.5px; color: var(--fg-3); }
.api-error { display: flex; align-items: center; gap: 10px; margin-bottom: 14px; padding: 12px 14px; border: 1px solid color-mix(in srgb, var(--bad) 35%, transparent); border-radius: var(--r-md); background: color-mix(in srgb, var(--bad) 5%, transparent); color: var(--fg-2); font-size: 12px; }
.sv2-select, .sv2-input { width: 100%; max-width: 560px; padding: 9px 12px; background: var(--bg-0); border: 1px solid var(--border); border-radius: var(--r-md); color: var(--fg-0); font-size: 13px; outline: none; }
.image-artifacts { margin-top: 14px; border: 1px solid var(--border); border-radius: var(--r-md); overflow: hidden; }
.image-artifact-row { display: grid; grid-template-columns: 80px minmax(0, 1fr) 72px auto; align-items: center; gap: 10px; padding: 10px 12px; background: var(--bg-2); border-bottom: 1px solid var(--border); }
.image-artifact-row:last-child { border-bottom: 0; }
.artifact-role { font-size: 11px; text-transform: uppercase; letter-spacing: .06em; color: var(--fg-3); }
.artifact-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 11px var(--font-mono); color: var(--fg-1); }
.artifact-size { text-align: right; font: 11px var(--font-mono); color: var(--fg-3); }
.artifact-card { display: flex; align-items: center; justify-content: space-between; gap: 14px; margin-top: 14px; padding: 14px 16px; background: var(--bg-2); border: 1px solid var(--border); border-radius: var(--r-md); }
.artifact-card.ok { border-color: color-mix(in srgb, var(--good) 30%, transparent); }
.artifact-info { display: flex; align-items: center; gap: 10px; min-width: 0; }
.artifact-text { font: 12px var(--font-mono); color: var(--fg-2); }
.fetch-progress { margin-top: 14px; }
.prog-track { height: 6px; border-radius: 3px; background: var(--bg-0); overflow: hidden; }
.prog-fill { height: 100%; background: var(--gold); transition: width .3s ease; }
.prog-meta { display: flex; gap: 6px; align-items: center; margin-top: 6px; font: 11px var(--font-mono); color: var(--fg-2); }
.dim { color: var(--fg-4); }
.ellipsis { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
.dl-error { margin: 10px 0 0; font-size: 12px; color: var(--bad, #e5484d); }
.image-test-card { display: grid; gap: 10px; padding: 14px 16px; border: 1px solid var(--border); border-radius: var(--r-md); background: var(--bg-2); }
.test-textarea { resize: vertical; min-height: 72px; max-width: none; font-family: inherit; line-height: 1.5; }
.image-test-card .sv2-btn { justify-self: start; }
.generation-error { display: flex; align-items: flex-start; gap: 8px; padding: 10px 12px; border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent); border-radius: var(--r-sm); background: color-mix(in srgb, var(--bad) 8%, transparent); color: var(--bad); font-size: 12px; line-height: 1.45; }
.generated-preview { display: grid; gap: 8px; color: var(--fg-3); font: 11px var(--font-mono); }
.generated-preview img { width: min(100%, 512px); aspect-ratio: 1; object-fit: contain; border-radius: var(--r-md); border: 1px solid var(--border); background: var(--bg-0); }
@media (max-width: 620px) { .image-artifact-row { grid-template-columns: 64px minmax(0, 1fr); } .artifact-card { align-items: flex-start; flex-direction: column; } }
</style>
