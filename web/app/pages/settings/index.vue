<template>
  <div>
    <div class="page-header">
      <h2 class="page-title">Dashboard</h2>
      <p class="page-desc">Overview of your media server</p>
    </div>

    <section class="section">
      <h3 class="section-heading">
        <Icon name="folder" :size="14" />
        Media Library
      </h3>
      <div class="stat-grid">
        <div v-for="s in mediaStats" :key="s.label" class="stat-card">
          <div class="stat-icon" :style="{ background: s.bg, color: s.color }">
            <Icon :name="s.icon" :size="18" />
          </div>
          <div class="stat-body">
            <div class="stat-value">{{ s.value }}</div>
            <div class="stat-label">{{ s.label }}</div>
          </div>
        </div>
      </div>
    </section>

    <section v-if="missingItems.length" class="section">
      <h3 class="section-heading">
        <Icon name="warning" :size="14" />
        Missing Media
      </h3>
      <div class="missing-header">
        <div class="missing-summary">
          <Icon name="warning" :size="14" />
          <span>{{ missingItems.length }} item{{ missingItems.length > 1 ? 's' : '' }} no longer found on disk</span>
        </div>
        <button class="btn btn-secondary" :disabled="cleaning" @click="cleanupMissing">
          <Icon name="trash" :size="14" />
          {{ cleaning ? 'Cleaning…' : 'Clean up all' }}
        </button>
      </div>
      <div class="missing-scroll">
        <div v-for="item in missingItems" :key="item.id" class="missing-tile">
          <div class="missing-poster">
            <img v-if="item.poster_path && !item.poster_path.startsWith('http')" :src="`/api/media/${item.id}/image/poster`" />
            <div v-else class="missing-poster-empty">
              <Icon :name="item.media_type === 'movie' ? 'film' : item.media_type === 'tv' ? 'tv' : 'music'" :size="16" />
            </div>
            <div class="missing-badge">Missing</div>
          </div>
          <div class="missing-meta">
            <div class="missing-tile-title">{{ item.title }}</div>
            <div class="missing-tile-sub">{{ item.year }} · {{ item.media_type }}</div>
          </div>
        </div>
      </div>
    </section>

    <section class="section">
      <h3 class="section-heading">
        <Icon name="pulse" :size="14" />
        System Health
      </h3>
      <div class="health-grid">
        <div class="health-card">
          <div class="health-indicator" :class="health?.status === 'ok' ? 'good' : 'bad'" />
          <div class="health-info">
            <div class="health-label">Server</div>
            <div class="health-status">{{ health?.status === 'ok' ? 'Online' : 'Offline' }}</div>
          </div>
        </div>
        <div class="health-card">
          <div class="health-indicator" :class="health?.database === 'connected' ? 'good' : 'bad'" />
          <div class="health-info">
            <div class="health-label">Database</div>
            <div class="health-status">{{ health?.database === 'connected' ? 'Connected' : (health?.database ?? 'Unknown') }}</div>
          </div>
        </div>
        <div class="health-card">
          <div class="health-indicator" :class="queueStatus.running > 0 ? 'active' : 'idle'" />
          <div class="health-info">
            <div class="health-label">Job Queue</div>
            <div class="health-status">
              {{ queueStatus.running }} running
              <span v-if="queueStatus.pending > 0" class="queue-pending">
                / {{ queueStatus.pending }} pending
              </span>
            </div>
          </div>
        </div>
      </div>
    </section>

  </div>
</template>

<script setup lang="ts">
import type { HealthResponse } from '~~/shared/types'

interface DashboardStats {
  libraries: number
  media_counts: Record<string, number>
  total_media: number
  total_people: number
  total_files: number
  missing_count: number
  queue_pending: number
  queue_running: number
}

interface MissingItem {
  id: number
  title: string
  year: string
  media_type: string
  poster_path: string
  slug: string
}

const stats = ref<DashboardStats | null>(null)
const health = ref<HealthResponse | null>(null)
const missingItems = ref<MissingItem[]>([])
const cleaning = ref(false)

async function cleanupMissing() {
  if (!confirm(`Delete ${missingItems.value.length} missing items and all their metadata? This cannot be undone.`)) return
  cleaning.value = true
  try {
    const result = await apiFetch<{ deleted: number }>('/api/media/missing', { method: 'DELETE' })
    missingItems.value = []
    if (stats.value) {
      stats.value.missing_count = 0
      stats.value.total_media -= result.deleted
    }
  } catch {}
  cleaning.value = false
}

const mediaStats = computed(() => [
  {
    label: 'Libraries',
    value: stats.value?.libraries ?? '–',
    icon: 'folder',
    bg: 'var(--gold-soft)',
    color: 'var(--gold)',
  },
  {
    label: 'Movies',
    value: stats.value?.media_counts?.movie ?? 0,
    icon: 'film',
    bg: 'rgba(230, 185, 74, 0.12)',
    color: 'var(--gold)',
  },
  {
    label: 'TV Shows',
    value: stats.value?.media_counts?.tv ?? 0,
    icon: 'tv',
    bg: 'rgba(140, 160, 255, 0.12)',
    color: 'rgb(140, 160, 255)',
  },
  {
    label: 'Music',
    value: stats.value?.media_counts?.music ?? 0,
    icon: 'music',
    bg: 'rgba(200, 140, 255, 0.12)',
    color: 'rgb(200, 140, 255)',
  },
  {
    label: 'Books',
    value: stats.value?.media_counts?.book ?? 0,
    icon: 'book',
    bg: 'rgba(140, 220, 180, 0.12)',
    color: 'rgb(140, 220, 180)',
  },
  {
    label: 'People',
    value: stats.value?.total_people ?? 0,
    icon: 'users',
    bg: 'rgba(255, 255, 255, 0.04)',
    color: 'var(--fg-2)',
  },
  {
    label: 'Files',
    value: stats.value?.total_files ?? 0,
    icon: 'hard-drives',
    bg: 'rgba(255, 255, 255, 0.04)',
    color: 'var(--fg-2)',
  },
])

const { on, queueStatus } = useEventBus()

async function refetchStats() {
  try { stats.value = await apiFetch<DashboardStats>('/api/stats') } catch {}
}

let statsTimer: ReturnType<typeof setTimeout> | null = null
function debouncedRefetchStats() {
  if (statsTimer) clearTimeout(statsTimer)
  statsTimer = setTimeout(refetchStats, 2000)
}

onMounted(async () => {
  const [s, h, m] = await Promise.allSettled([
    apiFetch<DashboardStats>('/api/stats'),
    $fetch<HealthResponse>('/api/health'),
    apiFetch<MissingItem[]>('/api/media/missing'),
  ])
  if (s.status === 'fulfilled') stats.value = s.value
  if (h.status === 'fulfilled') health.value = h.value
  if (m.status === 'fulfilled') missingItems.value = m.value ?? []

  const unsubs = [
    on('media.added', debouncedRefetchStats),
    on('media.removed', debouncedRefetchStats),
    on('scan.completed', debouncedRefetchStats),
    on('stats.updated', (event) => {
      const p = event.payload as DashboardStats
      if (stats.value) {
        stats.value.libraries = p.libraries
        stats.value.media_counts = p.media_counts
        stats.value.total_media = p.total_media
        stats.value.total_people = p.total_people
        stats.value.total_files = p.total_files
        stats.value.queue_pending = p.queue_pending
        stats.value.queue_running = p.queue_running
      } else {
        stats.value = { ...p, missing_count: 0 } as DashboardStats
      }
    }),
  ]

  onUnmounted(() => {
    unsubs.forEach(fn => fn())
    if (statsTimer) clearTimeout(statsTimer)
  })
})
</script>

<style scoped>
.page-header { margin-bottom: 32px; }
.page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.page-desc { font-size: 13px; color: var(--fg-3); margin: 6px 0 0; }

.section { margin-bottom: 36px; }
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

.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
  gap: 10px;
}

.stat-card {
  display: flex;
  align-items: center;
  gap: 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 16px 18px;
  transition: border-color 0.15s ease;
}

.stat-card:hover { border-color: var(--border-strong); }

.stat-icon {
  width: 40px;
  height: 40px;
  border-radius: var(--r-md);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.stat-body { min-width: 0; }
.stat-value { font-size: 22px; font-weight: 700; line-height: 1; }
.stat-label {
  font-size: 10px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin-top: 4px;
}

.health-grid { display: flex; flex-direction: column; gap: 2px; }

.health-card {
  display: flex;
  align-items: center;
  gap: 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  padding: 14px 18px;
}

.health-card:first-child { border-radius: var(--r-md) var(--r-md) 0 0; }
.health-card:last-child { border-radius: 0 0 var(--r-md) var(--r-md); }
.health-card:only-child { border-radius: var(--r-md); }

.health-indicator {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.health-indicator.good { background: var(--good); box-shadow: 0 0 8px rgba(111, 191, 124, 0.4); }
.health-indicator.bad { background: var(--bad); box-shadow: 0 0 8px rgba(217, 107, 107, 0.4); }
.health-indicator.active { background: var(--gold); box-shadow: 0 0 8px rgba(230, 185, 74, 0.4); }
.health-indicator.idle { background: var(--fg-4); }

.health-info { flex: 1; display: flex; align-items: center; justify-content: space-between; }
.health-label { font-size: 13px; font-weight: 500; color: var(--fg-1); }
.health-status { font-size: 12px; color: var(--fg-2); font-family: var(--font-mono); }
.queue-pending { color: var(--gold); }

.missing-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.missing-summary {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--bad);
  font-weight: 500;
}
.missing-scroll {
  display: flex;
  gap: 10px;
  overflow-x: auto;
  overflow-y: hidden;
  padding-bottom: 4px;
  scrollbar-width: none;
}
.missing-scroll::-webkit-scrollbar { display: none; }
.missing-tile {
  width: 120px;
  flex-shrink: 0;
  opacity: 0.7;
}
.missing-poster {
  position: relative;
  border-radius: var(--r-md);
  overflow: hidden;
  aspect-ratio: 2/3;
  background: var(--bg-3);
}
.missing-poster img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  filter: grayscale(0.6);
}
.missing-poster-empty {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
}
.missing-badge {
  position: absolute;
  top: 6px;
  right: 6px;
  font-size: 8px;
  font-weight: 700;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  padding: 2px 6px;
  border-radius: 100px;
  background: rgba(217, 107, 107, 0.85);
  color: #fff;
}
.missing-meta { margin-top: 6px; }
.missing-tile-title {
  font-size: 11px;
  font-weight: 500;
  color: var(--fg-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.missing-tile-sub {
  font-size: 10px;
  color: var(--fg-3);
  font-family: var(--font-mono);
}
</style>
