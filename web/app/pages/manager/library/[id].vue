<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { librariesQuery } from '~/queries/catalog'
import { MOCK_CATALOG, type MockCatalogItem } from '~/utils/managerMock'

const route = useRoute()
const { data: libraries } = useQuery(librariesQuery())

const library = computed(() => {
  const id = Number(route.params.id)
  return (libraries.value ?? []).find(l => l.id === id) ?? null
})

// The real page will be a managed lens over this library's media_items
// (monitored flags, profile, missing counts). Until then: mock rows keyed by
// the library's media type, copied into local state so the toggles feel live.
const items = ref<MockCatalogItem[]>([])
watch(() => library.value?.media_type, type => {
  items.value = (MOCK_CATALOG[type ?? 'movie'] ?? []).map(i => ({ ...i }))
}, { immediate: true })

const view = ref<'all' | 'monitored' | 'missing' | 'unmet'>('all')
const VIEWS = [
  { key: 'all' as const, label: 'All' },
  { key: 'monitored' as const, label: 'Monitored' },
  { key: 'missing' as const, label: 'Missing' },
  { key: 'unmet' as const, label: 'Cutoff unmet' },
]

const rows = computed(() => {
  switch (view.value) {
    case 'monitored': return items.value.filter(i => i.monitored)
    case 'missing': return items.value.filter(i => i.status === 'missing')
    case 'unmet': return items.value.filter(i => i.status === 'unmet')
    default: return items.value
  }
})

const monitoredCount = computed(() => items.value.filter(i => i.monitored).length)
const missingCount = computed(() => items.value.filter(i => i.status === 'missing').length)
const unmetCount = computed(() => items.value.filter(i => i.status === 'unmet').length)

const STATUS_STATE: Record<MockCatalogItem['status'], 'ok' | 'warn' | 'error'> = {
  ok: 'ok',
  unmet: 'warn',
  missing: 'error',
}
</script>

<template>
  <div>
    <SettingsContextHero
      :title="library?.name ?? 'Library'"
      :icon="managerLibraryIcon(library?.media_type ?? '')"
      eyebrow="Manager · Preview — mock data"
      description="The managed lens over this library: what's monitored, what's missing, and what sits below its quality cutoff. Same catalog as the server — a different room, not a second database."
    />

    <div class="tiles">
      <MetricTile label="Items" :value="items.length" icon="folder" tone="neutral" :sub="`in ${library?.name ?? 'library'}`" />
      <MetricTile label="Monitored" :value="monitoredCount" icon="eye" tone="good" sub="watched for releases" />
      <MetricTile label="Missing" :value="missingCount" icon="target" :tone="missingCount ? 'warn' : 'good'" sub="no file on disk" />
      <MetricTile label="Cutoff unmet" :value="unmetCount" icon="sort" tone="neutral" sub="upgrade candidates" />
    </div>

    <div class="lib-toolbar">
      <div class="lib-views" role="group" aria-label="Filter items">
        <button
          v-for="v in VIEWS"
          :key="v.key"
          type="button"
          class="lib-view-chip"
          :class="{ active: view === v.key }"
          @click="view = v.key"
        >{{ v.label }}</button>
      </div>
      <button type="button" class="lib-search-all">
        <Icon name="search" :size="14" />
        <span>Search all missing</span>
      </button>
    </div>

    <div class="lib-table">
      <div class="lib-head">
        <span>Title</span>
        <span>Monitored</span>
        <span>Profile</span>
        <span>Status</span>
        <span>Size</span>
      </div>
      <div v-for="row in rows" :key="row.id" class="lib-row">
        <div class="lib-item">
          <div class="lib-title">{{ row.title }}</div>
          <div class="lib-sub">{{ row.detail }}</div>
        </div>
        <AppSwitch v-model="row.monitored" :aria-label="`Monitor ${row.title}`" />
        <span class="lib-profile">{{ row.profile }}</span>
        <StatusBadge :state="STATUS_STATE[row.status]">{{ row.statusLabel }}</StatusBadge>
        <span class="lib-size">{{ row.size }}</span>
      </div>
    </div>

    <div v-if="rows.length === 0" class="lib-empty">
      <Icon name="check" :size="14" /> Nothing matches this view.
    </div>
  </div>
</template>

<style scoped>
.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 10px;
  margin-bottom: 24px;
}
@media (max-width: 720px) {
  .tiles { grid-template-columns: repeat(2, 1fr); }
}

.lib-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  flex-wrap: wrap;
  margin-bottom: 18px;
}
.lib-views { display: flex; gap: 6px; flex-wrap: wrap; }
.lib-view-chip {
  display: inline-flex;
  align-items: center;
  height: 30px;
  padding: 0 12px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.lib-view-chip:hover { background: rgb(var(--ink) / 0.09); color: var(--fg-0); }
.lib-view-chip.active {
  color: var(--gold-bright);
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 45%, transparent);
}

.lib-search-all {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 32px;
  padding: 0 14px;
  border-radius: var(--r-sm);
  background: var(--gold-soft);
  border: 1px solid color-mix(in srgb, var(--gold) 40%, transparent);
  color: var(--gold-bright);
  font-size: 12.5px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.12s;
}
.lib-search-all:hover { background: color-mix(in srgb, var(--gold) 18%, transparent); }

.lib-table { display: flex; flex-direction: column; gap: 6px; }
.lib-head,
.lib-row {
  display: grid;
  grid-template-columns: minmax(220px, 2.4fr) 90px 120px minmax(160px, 1.2fr) 90px;
  gap: 14px;
  align-items: center;
}
.lib-head {
  padding: 0 14px 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.lib-row {
  padding: 12px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.lib-item { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.lib-title { font-size: 13.5px; font-weight: 500; color: var(--fg-0); }
.lib-sub { font-size: 12px; color: var(--fg-2); }
.lib-profile { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-1); }
.lib-size { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-2); text-align: right; }

.lib-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
  margin-top: 6px;
}

@media (max-width: 960px) {
  .lib-head { display: none; }
  .lib-row {
    grid-template-columns: minmax(0, 1fr) auto;
    row-gap: 8px;
  }
  .lib-profile, .lib-size { display: none; }
  .lib-row > :nth-child(4) { grid-column: 1; justify-self: start; }
}
</style>
