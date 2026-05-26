<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import type { components } from '#open-fetch-schemas/heya'
type WatcherStatus = components['schemas']['WatcherStatusBody']
type Library      = components['schemas']['LibraryView']

const { $heya } = useNuxtApp()

const status = ref<WatcherStatus | null>(null)
const libraries = ref<Library[]>([])
const loading = ref(true)
const lastFetched = ref<Date | null>(null)
const error = ref<string | null>(null)
let timer: ReturnType<typeof setInterval> | null = null

async function load() {
  error.value = null
  try {
    const [s, ls] = await Promise.all([
      $heya('/api/watchers'),
      $heya('/api/libraries'),
    ])
    status.value = s
    libraries.value = ls ?? []
    lastFetched.value = new Date()
  } catch (e: any) {
    error.value = e?.message ?? 'Failed to fetch watcher status.'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  load()
  timer = setInterval(load, 5000)
})
onBeforeUnmount(() => { if (timer) clearInterval(timer) })

const libraryById = computed(() => {
  const m = new Map<number, Library>()
  for (const l of libraries.value) m.set(l.id, l)
  return m
})

const rows = computed(() =>
  (status.value?.watchers ?? []).map(w => ({
    library_id: w.library_id,
    path: w.path,
    library: libraryById.value.get(w.library_id),
  })),
)

const librariesWithoutWatcher = computed(() => {
  if (!status.value) return []
  const watched = new Set((status.value.watchers ?? []).map(w => w.library_id))
  return libraries.value.filter(l => !watched.has(l.id))
})

const coverageTone = computed<'good' | 'warn' | 'bad'>(() => {
  const total = libraries.value.length
  if (total === 0) return 'warn'
  const watched = status.value?.watchers?.length ?? 0
  if (watched === total) return 'good'
  if (watched === 0) return 'bad'
  return 'warn'
})

function iconForKind(kind: string): string {
  switch (kind) {
    case 'movie': return 'film'
    case 'tv':    return 'tv'
    case 'music': return 'music'
    case 'book':  return 'book'
    default:      return 'folder'
  }
}

const tickKey = ref(0)
setInterval(() => { tickKey.value++ }, 1000)

function lastFetchedText(): string {
  // Read tick to depend on the per-second tick without remounting the cell.
  void tickKey.value
  if (!lastFetched.value) return '—'
  const s = Math.floor((Date.now() - lastFetched.value.getTime()) / 1000)
  if (s < 5) return 'just now'
  return `${s}s ago`
}
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Filesystem watchers</h2>
      <p class="sv2-page-desc">
        One fsnotify watcher per library path. New files trigger an immediate
        scan; without a watcher you depend on the periodic rescan task.
      </p>
    </header>

    <div v-if="loading && !status" class="loading-state">
      <Icon name="spinner" :size="16" /> Loading…
    </div>

    <template v-else>
      <div class="tiles">
        <MetricTile
          label="Watchers active"
          :value="status?.count ?? 0"
          icon="eye"
          :tone="coverageTone === 'good' ? 'good' : coverageTone === 'warn' ? 'warn' : 'bad'"
          :sub="`of ${libraries.length} ${libraries.length === 1 ? 'library' : 'libraries'}`"
        />
        <MetricTile
          label="Unwatched libraries"
          :value="librariesWithoutWatcher.length"
          icon="warning"
          :tone="librariesWithoutWatcher.length === 0 ? 'good' : 'warn'"
          :sub="librariesWithoutWatcher.length === 0 ? 'full coverage' : 'falling back to periodic scan'"
        />
        <MetricTile
          label="Refreshed"
          :value="lastFetchedText()"
          icon="timer"
          tone="neutral"
          sub="polling every 5s"
        />
      </div>

      <SettingsSection title="Active watchers" icon="eye"
        description="Each row is a live fsnotify subscription. The path shown is the watched root — recursive notifications cover its entire subtree.">
        <template #actions>
          <LiveDot connected :label="`Polling · ${lastFetchedText()}`" />
        </template>

        <div v-if="error" class="sv2-flash err">
          <Icon name="warning" :size="13" /> {{ error }}
        </div>

        <div v-if="rows.length === 0" class="empty-state">
          <Icon name="info" :size="14" />
          No watchers are currently active. Libraries fall back to the periodic rescan task.
        </div>

        <div v-else class="watcher-grid">
          <div v-for="r in rows" :key="`${r.library_id}-${r.path}`" class="watcher-card">
            <div class="watcher-icon"><Icon :name="iconForKind(r.library?.media_type ?? '')" :size="16" /></div>
            <div class="watcher-body">
              <div class="watcher-name">
                {{ r.library?.name ?? `Library #${r.library_id}` }}
                <StatusBadge state="ok">watching</StatusBadge>
              </div>
              <div class="watcher-path">{{ r.path }}</div>
              <div class="watcher-meta">
                <span>{{ r.library?.media_type ?? 'unknown type' }}</span>
                <span v-if="r.library?.paths?.length && r.library.paths.length > 1">
                  · {{ r.library.paths.length }} library paths
                </span>
              </div>
            </div>
          </div>
        </div>
      </SettingsSection>

      <SettingsSection
        v-if="librariesWithoutWatcher.length"
        title="Libraries without a watcher"
        icon="warning"
        description="Likely the watcher failed to start (path missing, EMFILE, permission). These libraries only update during the periodic rescan."
      >
        <div class="watcher-grid">
          <div v-for="l in librariesWithoutWatcher" :key="l.id" class="watcher-card dim">
            <div class="watcher-icon dim"><Icon :name="iconForKind(l.media_type)" :size="16" /></div>
            <div class="watcher-body">
              <div class="watcher-name">
                {{ l.name }}
                <StatusBadge state="warn">no watcher</StatusBadge>
              </div>
              <div class="watcher-path">{{ (l.paths ?? []).join(', ') || '—' }}</div>
              <div class="watcher-meta">
                <span>{{ l.media_type }}</span>
                <span>· library #{{ l.id }}</span>
              </div>
            </div>
          </div>
        </div>
      </SettingsSection>
    </template>
  </div>
</template>

<style scoped>
.sv2-page-head { margin-bottom: 28px; }
.sv2-page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.sv2-page-desc { margin: 6px 0 0; font-size: 13px; color: var(--fg-3); line-height: 1.55; }

.loading-state, .empty-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 10px;
  margin-bottom: 28px;
}

.watcher-grid { display: flex; flex-direction: column; gap: 8px; }
.watcher-card {
  display: flex;
  align-items: flex-start;
  gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.watcher-card.dim { background: rgba(255,255,255,0.012); border-style: dashed; }
.watcher-icon {
  width: 36px; height: 36px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  display: flex; align-items: center; justify-content: center;
  color: var(--good);
  flex-shrink: 0;
}
.watcher-icon.dim { color: var(--fg-3); }
.watcher-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.watcher-name {
  display: flex; align-items: center; gap: 8px;
  font-size: 14px; font-weight: 500; color: var(--fg-0);
}
.watcher-path {
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--fg-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.watcher-meta {
  font-size: 11.5px; color: var(--fg-3);
  display: flex; flex-wrap: wrap; gap: 6px;
}

.sv2-flash {
  margin-bottom: 12px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  font-size: 12px;
  display: flex; align-items: center; gap: 8px;
}
.sv2-flash.err { background: rgba(217, 107, 107, 0.10); border: 1px solid rgba(217, 107, 107, 0.30); color: var(--bad); }
</style>
