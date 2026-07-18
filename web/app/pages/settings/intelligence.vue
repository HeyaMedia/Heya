<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import {
  aiStatusQuery,
  imageGenerationStatusQuery,
  recommendationsStatusQuery,
  sonicSettingsQuery,
  sonicStatusQuery,
} from '~/queries/intelligence'

const sonicData = useQuery(sonicStatusQuery())
const sonicSettingsData = useQuery(sonicSettingsQuery())
const recsData = useQuery(recommendationsStatusQuery())
const aiData = useQuery(aiStatusQuery())
const imagesData = useQuery(imageGenerationStatusQuery())

const sonic = computed(() => sonicData.data.value ?? null)
const sonicEnabled = computed(() => sonicSettingsData.data.value?.enabled ?? false)
const recs = computed(() => recsData.data.value ?? null)
const ai = computed(() => aiData.data.value ?? null)
const images = computed(() => imagesData.data.value ?? null)

// Live per-item progress rides the shared WS bus (task.progress); the HTTP
// status snapshot carries the same fields as a fallback for cold loads.
const { taskProgress } = useEventBus()
const sonicLive = computed(() => taskProgress.value['analyze_music_facets'])
const embedLive = computed(() => taskProgress.value['embed_recommendations'])

const sonicCurrentItem = computed(() => sonicLive.value?.current_item || sonic.value?.current_item || '')
const sonicCurrentStage = computed(() => sonicLive.value?.current_stage || sonic.value?.current_stage || '')
const sonicPending = computed(() => sonicLive.value?.pending ?? sonic.value?.coverage?.pending ?? 0)
const sonicActive = computed(() =>
  (sonicLive.value?.running ?? 0) > 0 || (sonic.value?.holder?.refs ?? 0) > 0)

// --- Sonic tiles ------------------------------------------------------
const holder = computed(() => sonic.value?.holder)
const modelLabel = computed(() => {
  switch (holder.value?.state) {
    case 'ready':     return 'Warm'
    case 'loading':   return 'Loading'
    case 'unloading': return 'Unloading'
    case 'unloaded':  return 'Cold'
    default:          return holder.value?.state ?? '—'
  }
})
const modelTone = computed<'good' | 'warn' | 'neutral'>(() => {
  switch (holder.value?.state) {
    case 'ready':     return 'good'
    case 'loading':
    case 'unloading': return 'warn'
    default:          return 'neutral'
  }
})
const modelSub = computed(() => {
  const parts: string[] = []
  if (holder.value?.accelerator) parts.push(holder.value.accelerator)
  if (holder.value?.source === 'worker') parts.push('reported by worker')
  else if (holder.value?.source === 'local') parts.push('local process')
  return parts.join(' · ')
})

const coverage = computed(() => sonic.value?.coverage ?? { analyzed: 0, pending: 0 })
const coveragePct = computed(() => {
  const total = coverage.value.analyzed + coverage.value.pending
  if (!total) return null
  return Math.floor((coverage.value.analyzed / total) * 1000) / 10
})

const throughput = computed(() => sonic.value?.throughput)
const throughputSpark = computed(() => (throughput.value?.buckets ?? []).map(b => b.count))

const sonicFetcher = computed(() => sonic.value?.fetcher)
const sonicModelsValue = computed(() => {
  const f = sonicFetcher.value
  if (!f) return '—'
  if (f.state === 'fetching') return 'Downloading'
  if (f.state === 'checking') return 'Verifying'
  if ((f.missing_count ?? 0) > 0) return `${f.missing_count} missing`
  return 'Ready'
})
const sonicModelsTone = computed<'good' | 'warn' | 'bad'>(() => {
  const f = sonicFetcher.value
  if (!f) return 'warn'
  if (f.state === 'failed') return 'bad'
  if (f.state === 'fetching' || (f.missing_count ?? 0) > 0) return 'warn'
  return 'good'
})

// --- Recommendations tiles --------------------------------------------
function embedPair(embedded?: number, total?: number) {
  const e = embedded ?? 0
  const t = total ?? 0
  return { value: t ? `${Math.floor((e / t) * 100)}%` : '—', sub: `${e.toLocaleString()} of ${t.toLocaleString()} embedded` }
}
const recsVideo = computed(() => embedPair(recs.value?.embedded, recs.value?.total))
const recsEpisodes = computed(() => embedPair(recs.value?.embedded_episodes, recs.value?.total_episodes))
const recsMusic = computed(() => embedPair(recs.value?.embedded_music, recs.value?.total_music))

// --- AI provider tiles --------------------------------------------------
const aiModeLabel = computed(() => {
  switch (ai.value?.mode) {
    case 'off':      return 'Off'
    case 'local':    return 'Local'
    case 'external': return 'External'
    case 'claude':   return 'Claude'
    case 'codex':    return 'Codex'
    default:         return ai.value?.mode ?? '—'
  }
})
const aiModelValue = computed(() =>
  ai.value?.local.running_model || ai.value?.model || ai.value?.local_model || '—')
const aiRuntimeValue = computed(() => {
  const local = ai.value?.local
  if (!local) return '—'
  if (local.download_state === 'fetching') return 'Downloading'
  if (local.running) return 'Running'
  if (local.server_present) return 'Installed'
  return 'Not installed'
})

// --- Image generation tiles ---------------------------------------------
const imageRuntimeValue = computed(() => {
  if (!images.value) return '—'
  if (images.value.download_state === 'fetching') return 'Downloading'
  if (!images.value.runtime_present) return 'Not installed'
  return 'Ready'
})
const imageProgressSub = computed(() => {
  const p = images.value?.progress
  if (!p || !p.bytes_total) return undefined
  return `${Math.floor((p.bytes_done / p.bytes_total) * 100)}% of ${fmtBytes(p.bytes_total)}`
})

function fmtBytes(n?: number) {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let u = 0
  while (v >= 1024 && u < units.length - 1) { v /= 1024; u++ }
  return `${v >= 10 ? Math.round(v) : Math.round(v * 10) / 10} ${units[u]}`
}

// Background poll keeps the tiles moving while the page is open. The
// loading guards below stop skeleton flicker on refetches (network.vue
// pattern).
let pollTimer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  pollTimer = setInterval(() => {
    sonicData.refetch()
    recsData.refetch()
    aiData.refetch()
    imagesData.refetch()
  }, 5000)
})
onBeforeUnmount(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<template>
  <div>
    <SettingsContextHero
      icon="sparkle"
      eyebrow="Media intelligence"
      title="Intelligence"
      description="Live overview of the models working through your library — sonic analysis, recommendation embeddings, AI providers, and image generation."
    />

    <SettingsSection title="Sonic analysis" icon="eq">
      <template #actions>
        <LiveDot :connected="sonicActive" :label="sonicActive ? 'Analyzing' : (sonicEnabled ? 'Idle' : 'Disabled')" />
        <NuxtLink to="/settings/sonic" class="cfg-link">Configure</NuxtLink>
      </template>
      <div class="tiles">
        <MetricTile label="Model" :value="modelLabel" icon="cpu" :tone="modelTone" :sub="modelSub" />
        <MetricTile
          label="Coverage"
          :value="coveragePct != null ? `${coveragePct}%` : '—'"
          icon="chart-bar"
          :tone="coveragePct === 100 ? 'good' : 'neutral'"
          :sub="`${coverage.analyzed.toLocaleString()} analyzed · ${coverage.pending.toLocaleString()} pending`"
        />
        <MetricTile
          label="Analyzed (24h)"
          :value="(throughput?.last_24h ?? 0).toLocaleString()"
          icon="pulse"
          :sparkline="throughputSpark"
          :sub="`${throughput?.last_hour ?? 0} this hour`"
        />
        <MetricTile label="Model files" :value="sonicModelsValue" icon="cloud-arrow-down" :tone="sonicModelsTone"
                    :sub="sonicFetcher?.last_error ? 'last fetch failed' : undefined" />
      </div>
      <div v-if="sonicCurrentItem" class="live-row">
        <span class="live-row-pulse" aria-hidden="true" />
        <div class="live-row-copy">
          <span class="live-row-item">{{ sonicCurrentItem }}</span>
          <span v-if="sonicCurrentStage" class="live-row-stage">{{ sonicCurrentStage }}</span>
        </div>
        <span v-if="sonicPending > 0" class="live-row-count">{{ sonicPending.toLocaleString() }} queued</span>
      </div>
      <p v-else-if="!sonicEnabled" class="section-hint">
        Sonic analysis is disabled — enable it in the Sonic analysis tab to start building coverage.
      </p>
    </SettingsSection>

    <SettingsSection title="Recommendations" icon="sparkle">
      <template #actions>
        <StatusBadge :state="recs?.enabled ? 'ok' : 'idle'">{{ recs?.enabled ? 'Enabled' : 'Disabled' }}</StatusBadge>
        <NuxtLink to="/settings/recommendations" class="cfg-link">Configure</NuxtLink>
      </template>
      <div class="tiles">
        <MetricTile label="Movies & TV" :value="recsVideo.value" icon="film" :sub="recsVideo.sub" />
        <MetricTile label="Episodes" :value="recsEpisodes.value" icon="tv" :sub="recsEpisodes.sub" />
        <MetricTile label="Music" :value="recsMusic.value" icon="music" :sub="recsMusic.sub" />
        <MetricTile
          label="Embedding model"
          :value="recs?.model ?? '—'"
          icon="cpu"
          :sub="recs?.dimensions ? `${recs.dimensions} dims · ${recs?.fetcher?.state ?? 'unknown'}` : undefined"
        />
      </div>
      <div v-if="embedLive?.current_item" class="live-row">
        <span class="live-row-pulse" aria-hidden="true" />
        <div class="live-row-copy">
          <span class="live-row-item">{{ embedLive.current_item }}</span>
        </div>
      </div>
    </SettingsSection>

    <SettingsSection title="AI providers" icon="cpu">
      <template #actions>
        <StatusBadge :state="ai?.ready ? 'ok' : (ai?.mode === 'off' ? 'idle' : 'warn')">
          {{ ai?.ready ? 'Ready' : (ai?.mode === 'off' ? 'Off' : 'Not ready') }}
        </StatusBadge>
        <NuxtLink to="/settings/ai" class="cfg-link">Configure</NuxtLink>
      </template>
      <div class="tiles">
        <MetricTile label="Mode" :value="aiModeLabel" icon="lightning" :tone="ai?.mode === 'off' ? 'neutral' : 'good'" />
        <MetricTile label="Model" :value="aiModelValue" icon="cpu" :sub="ai?.detail" />
        <MetricTile
          label="Local runtime"
          :value="aiRuntimeValue"
          icon="hard-drives"
          :tone="ai?.local.running ? 'good' : 'neutral'"
          :sub="ai?.local.build ? `llama-server ${ai.local.build}` : undefined"
        />
        <MetricTile
          label="Context"
          :value="ai?.context_size ? ai.context_size.toLocaleString() : '—'"
          icon="text-aa"
          sub="tokens"
        />
      </div>
    </SettingsSection>

    <SettingsSection title="Image generation" icon="image">
      <template #actions>
        <StatusBadge :state="images?.runtime_present && images?.model_present ? 'ok' : 'idle'">
          {{ images?.runtime_present && images?.model_present ? 'Ready' : 'Not set up' }}
        </StatusBadge>
        <NuxtLink to="/settings/images" class="cfg-link">Configure</NuxtLink>
      </template>
      <div class="tiles">
        <MetricTile label="Runtime" :value="imageRuntimeValue" icon="wand"
                    :tone="images?.runtime_present ? 'good' : 'neutral'"
                    :sub="images?.backend ? `backend ${images.backend}` : undefined" />
        <MetricTile label="Model" :value="images?.model ?? '—'" icon="image"
                    :tone="images?.model_present ? 'good' : 'neutral'"
                    :sub="images?.model_present ? 'downloaded' : 'not downloaded'" />
        <MetricTile label="Download" :value="images?.download_state ?? '—'" icon="cloud-arrow-down"
                    :sub="imageProgressSub" />
        <MetricTile label="Compute devices" :value="images?.devices?.length ?? 0" icon="cpu"
                    :sub="images?.devices?.[0]?.name" />
      </div>
    </SettingsSection>
  </div>
</template>

<style scoped>
.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 8px;
}
@media (max-width: 720px) {
  .tiles { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

.cfg-link {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: var(--fg-2);
  text-decoration: none;
  padding: 4px 10px;
  border: 1px solid var(--border-strong);
  border-radius: 999px;
  transition: color 0.15s ease, border-color 0.15s ease;
}
.cfg-link:hover {
  color: var(--gold-bright);
  border-color: color-mix(in srgb, var(--gold) 55%, transparent);
}

/* "Working on X right now" strip under the tiles. */
.live-row {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 8px;
  padding: 10px 14px;
  border: 1px solid color-mix(in srgb, var(--gold) 26%, transparent);
  border-radius: var(--r-md);
  background: var(--gold-soft);
}
.live-row-pulse {
  width: 8px;
  height: 8px;
  flex: none;
  border-radius: 50%;
  background: var(--gold-bright);
  animation: live-pulse 1.6s ease-in-out infinite;
}
@keyframes live-pulse {
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.45; transform: scale(0.8); }
}
.live-row-copy {
  display: flex;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
  flex: 1;
}
.live-row-item {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg-0);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.live-row-stage {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-2);
}
.live-row-count {
  flex: none;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-2);
}

.section-hint {
  margin: 8px 0 0;
  font-size: 12.5px;
  color: var(--fg-2);
}

@media (prefers-reduced-motion: reduce) {
  .live-row-pulse { animation: none; }
}
</style>
