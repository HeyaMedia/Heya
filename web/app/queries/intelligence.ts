import { defineQueryOptions } from '@pinia/colada'

const privateRuntime = {
  prefetch: 'none',
  persistence: 'none',
  sensitivity: 'secret',
} as const

export type AcceleratorAvailability = { name: string, label: string, available: boolean, reason?: string }
export type FetchProgress = { current_file?: string, bytes_done?: number, bytes_total?: number, files_done?: number, files_total?: number, started_at?: string }

export type RecommendationsStatus = {
  enabled: boolean
  accelerator: string
  env_locks?: { enabled?: string, accelerator?: string }
  embedded?: number
  total?: number
  embedded_episodes?: number
  total_episodes?: number
  model?: string
  dimensions?: number
  accelerators?: AcceleratorAvailability[]
  fetcher?: { state: string, all_present?: boolean, missing_count?: number, progress?: FetchProgress, last_error?: string }
}
export type RecommendationsSettings = { enabled: boolean, accelerator: string }

export const recommendationsStatusQuery = defineQueryOptions(() => ({
  key: ['admin', 'intelligence', 'recommendations', 'status'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/admin/recommendations-ml/status') as RecommendationsStatus
  },
  staleTime: 1000 * 3,
  meta: privateRuntime,
}))

export const recommendationsSettingsQuery = defineQueryOptions(() => ({
  key: ['admin', 'intelligence', 'recommendations', 'settings'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/admin/recommendations-ml/settings') as RecommendationsSettings
  },
  staleTime: 1000 * 30,
  meta: privateRuntime,
}))

export type SonicManifestFile = { name: string, present: boolean, expected_size: number, actual_size: number, category: string }
export type SonicHolder = { state: string, accelerator?: string, refs?: number, idle_timeout_sec?: number, total_borrows?: number, loaded_at?: string, idle_unload_at?: string, last_borrow_at?: string }
export type SonicStatus = {
  fetcher?: {
    state: string
    all_present?: boolean
    missing_count?: number
    total_count?: number
    total_size?: number
    manifest?: SonicManifestFile[]
    progress?: FetchProgress
    last_error?: string
  }
  analyzer?: { state?: string }
  holder?: SonicHolder
  text_searcher?: { ready?: boolean }
  accelerators?: AcceleratorAvailability[]
  analyzer_version?: number
  coverage?: { analyzed: number, pending: number }
}
export type SonicSettings = { enabled: boolean, accelerator: string }

export const sonicStatusQuery = defineQueryOptions(() => ({
  key: ['admin', 'intelligence', 'sonic', 'status'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/admin/sonicanalysis/status') as SonicStatus
  },
  staleTime: 1000 * 3,
  meta: privateRuntime,
}))

export const sonicSettingsQuery = defineQueryOptions(() => ({
  key: ['admin', 'intelligence', 'sonic', 'settings'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/admin/sonicanalysis/settings') as SonicSettings
  },
  staleTime: 1000 * 30,
  meta: privateRuntime,
}))

export type AIProvider = { id: string, label: string, base_url: string, needs_key: boolean }
export type AILocalModel = { id: string, label: string, size: number, ram_hint: string, notes?: string }
export type AISettings = {
  mode: string
  provider: string
  api_key_set: boolean
  api_key_hint?: string
  model: string
  base_url: string
  local_model: string
  local_backend: string
  context_size: number
  claude_model: string
  codex_model: string
  claude_token_set: boolean
  claude_token_hint?: string
}
export type AIDownloadProgress = { current_file?: string, bytes_done: number, bytes_total: number, started_at?: string }
export type AIStatus = {
  mode: string
  ready: boolean
  detail?: string
  provider?: string
  model?: string
  local_model?: string
  context_size?: number
  local: {
    build: string
    server_present: boolean
    model_present: boolean
    running: boolean
    running_model?: string
    download_state: string
    download_progress?: AIDownloadProgress
    download_error?: string
  }
  agent: { provider?: string, binary_present: boolean, authenticated: boolean, setup_hint?: string }
}
export type AIChatResponse = { content: string, model?: string, mode: string, prompt_tokens: number, completion_tokens: number, duration_ms: number }
export type AICatalog = { providers: AIProvider[], local_models: AILocalModel[] }

export const aiStatusQuery = defineQueryOptions(() => ({
  key: ['admin', 'intelligence', 'ai', 'status'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/ai/status') as AIStatus
  },
  staleTime: 1000 * 3,
  meta: privateRuntime,
}))

export const aiSettingsQuery = defineQueryOptions(() => ({
  key: ['admin', 'intelligence', 'ai', 'settings'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/ai/settings') as AISettings
  },
  staleTime: 1000 * 30,
  meta: privateRuntime,
}))

export const aiCatalogQuery = defineQueryOptions(() => ({
  key: ['admin', 'intelligence', 'ai', 'catalog'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/ai/catalog') as AICatalog
  },
  staleTime: 1000 * 60 * 5,
  meta: privateRuntime,
}))

export type ImageArtifactStatus = { role: string, name: string, size: number, present: boolean, shared: boolean }
export type ImageComputeDevice = { name: string, description: string }
export type ImageModel = {
  id: string
  label: string
  license: string
  ram_hint: string
  default_width: number
  default_height: number
  default_steps: number
  default_cfg: number
  artifacts: Array<{ role: string, name: string, size: number }>
}
export type ImageGenerationStatus = {
  build: string
  backend: string
  model: string
  runtime_present: boolean
  model_present: boolean
  download_state: string
  progress?: AIDownloadProgress
  download_error?: string
  devices: ImageComputeDevice[]
  device_error?: string
  artifacts: ImageArtifactStatus[]
  download_bytes: number
}
export type ImageGenerateRequest = { prompt: string, negative_prompt?: string, width?: number, height?: number, steps?: number, cfg?: number, seed?: number, model_id?: string, backend?: string, device?: string }
export type ImageGenerateResult = { url: string, model: string, seed: number, duration_ms: number }

export const imageGenerationStatusQuery = defineQueryOptions(() => ({
  key: ['admin', 'intelligence', 'images', 'status'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/ai/images/status') as ImageGenerationStatus
  },
  staleTime: 1000 * 3,
  meta: privateRuntime,
}))

export const imageGenerationCatalogQuery = defineQueryOptions(() => ({
  key: ['admin', 'intelligence', 'images', 'catalog'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/ai/images/catalog') as { models: ImageModel[] }
  },
  staleTime: 1000 * 60 * 5,
  meta: privateRuntime,
}))

// Shared frontend entry point for playlist/collection artwork and settings QA.
export async function generateLocalImage(request: ImageGenerateRequest): Promise<ImageGenerateResult> {
  const { $heya } = useNuxtApp()
  return await $heya('/api/ai/images/generate', { method: 'POST', body: request as any }) as ImageGenerateResult
}
