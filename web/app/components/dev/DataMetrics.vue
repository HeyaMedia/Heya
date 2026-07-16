<template>
  <details
    class="dm-panel"
    :style="{ bottom: playbarVisible ? 'calc(var(--playbar-h, 88px) + 14px)' : '14px' }"
  >
    <summary>Data · {{ metrics.cacheEntries }} cached</summary>
    <div class="dm-grid">
      <span>Last navigation</span><b>{{ metrics.lastNavigationMs }} ms</b>
      <span>Average</span><b>{{ metrics.averageNavigationMs }} ms</b>
      <span>Warm / cold</span><b>{{ metrics.warmNavigations }} / {{ metrics.coldNavigations }}</b>
      <span>Prefetch used</span><b>{{ metrics.prefetchUsed }}/{{ metrics.prefetchAttempts }} · {{ metrics.prefetchUseRate }}%</b>
      <span>Prefetch wasted</span><b>{{ metrics.prefetchWasted }}</b>
      <span>Memory estimate</span><b>{{ formatBytes(metrics.cacheBytes) }}</b>
      <span>Disk cache</span><b>{{ metrics.persistedEntries }} · {{ formatBytes(metrics.persistedBytes) }}</b>
      <span>Hydrated</span><b>{{ metrics.hydratedEntries }}</b>
    </div>
    <div class="dm-path">{{ metrics.lastPath || 'No navigation yet' }}</div>
  </details>
</template>

<script setup lang="ts">
import { useQueryCache } from '@pinia/colada'

const metrics = useDataMetricsStore()
const queryCache = useQueryCache()
const route = useRoute()
const { isPhone } = useViewport()
const { currentTrack } = usePlayerBindings()

// Mirror DesktopPlayerHost's visibility condition. Keeping this in Vue avoids
// relying on WebKit's :has() handling for positioning a global dev overlay.
const isMusic = computed(() => route.path === '/music' || route.path.startsWith('/music/'))
const playbarVisible = computed(() => !isPhone.value && (!!currentTrack.value || isMusic.value))

function sampleCache() {
  const entries = queryCache.getEntries()
  let bytes = 0
  for (const entry of entries) {
    if (entry.state.value.status !== 'success') continue
    try { bytes += new Blob([JSON.stringify(entry.state.value.data)]).size } catch { /* estimate only */ }
  }
  metrics.setCacheStats(entries.length, bytes)
}

const timer = setInterval(sampleCache, 1000)
sampleCache()
onScopeDispose(() => clearInterval(timer))

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}
</script>

<style scoped>
.dm-panel {
  position: fixed; right: 14px; bottom: 14px; z-index: 10050;
  width: 260px; padding: 9px 11px; border-radius: 9px;
  color: #ddd; background: rgba(12, 12, 18, 0.94);
  border: 1px solid rgba(255, 255, 255, 0.13);
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.45);
  font: 11px/1.45 var(--font-mono, monospace);
  backdrop-filter: blur(14px);
  transition: bottom 180ms ease;
}
.dm-panel summary { cursor: pointer; color: var(--gold, #d6b56d); user-select: none; }
.dm-grid { display: grid; grid-template-columns: 1fr auto; gap: 4px 12px; margin-top: 9px; }
.dm-grid span { color: #999; }
.dm-grid b { color: #eee; font-weight: 500; text-align: right; }
.dm-path { margin-top: 8px; color: #777; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
