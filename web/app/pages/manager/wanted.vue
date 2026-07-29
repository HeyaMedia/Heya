<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { MOCK_LIBRARY_OPTIONS, MOCK_WANTED } from '~/utils/managerMock'

const filter = ref<string[]>([])
const tab = ref<'missing' | 'cutoff'>('missing')

const rows = computed(() => MOCK_WANTED
  .filter(w => w.reason === tab.value)
  .filter(w => filter.value.length === 0 || filter.value.includes(w.library)))

const missingCount = MOCK_WANTED.filter(w => w.reason === 'missing').length
const cutoffCount = MOCK_WANTED.filter(w => w.reason === 'cutoff').length
</script>

<template>
  <div>
    <SettingsContextHero
      title="Wanted"
      icon="target"
      eyebrow="Manager · Preview — mock data"
      description="Monitored items the pipeline still owes you — missing entirely, or on disk below the quality cutoff."
    />

    <div class="w-toolbar">
      <div class="w-tabs" role="tablist" aria-label="Wanted category">
        <button
          type="button" role="tab" class="w-tab"
          :aria-selected="tab === 'missing'" :class="{ active: tab === 'missing' }"
          @click="tab = 'missing'"
        >Missing <span class="w-count">{{ missingCount }}</span></button>
        <button
          type="button" role="tab" class="w-tab"
          :aria-selected="tab === 'cutoff'" :class="{ active: tab === 'cutoff' }"
          @click="tab = 'cutoff'"
        >Cutoff unmet <span class="w-count">{{ cutoffCount }}</span></button>
      </div>
      <ManagerLibraryFilter v-model="filter" :options="MOCK_LIBRARY_OPTIONS" />
    </div>

    <div class="w-table">
      <div class="w-head">
        <span>Item</span>
        <span>Profile</span>
        <span>Status</span>
        <span>Last search</span>
        <span class="w-col-actions" aria-hidden="true" />
      </div>
      <div v-for="row in rows" :key="row.id" class="w-row">
        <div class="w-item">
          <div class="w-title">{{ row.title }} <ManagerLibraryChip :library="row.library" /></div>
          <div class="w-sub">{{ row.subtitle }}</div>
        </div>
        <span class="w-profile">{{ row.profile }}</span>
        <span class="w-since">{{ row.since }}</span>
        <span class="w-search">{{ row.lastSearch }}</span>
        <div class="w-actions">
          <AppTooltip label="Search now">
            <button type="button" class="w-btn"><Icon name="search" :size="14" /></button>
          </AppTooltip>
          <AppTooltip label="Interactive search">
            <button type="button" class="w-btn"><Icon name="list" :size="14" /></button>
          </AppTooltip>
        </div>
      </div>
    </div>

    <div v-if="rows.length === 0" class="w-empty">
      <Icon name="check" :size="14" /> Nothing {{ tab === 'missing' ? 'missing' : 'below cutoff' }} for the selected libraries.
    </div>
  </div>
</template>

<style scoped>
.w-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  flex-wrap: wrap;
  margin-bottom: 18px;
}

.w-tabs { display: flex; gap: 6px; }
.w-tab {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 32px;
  padding: 0 14px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-size: 12.5px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.w-tab:hover { color: var(--fg-0); background: rgb(var(--ink) / 0.09); }
.w-tab.active {
  color: var(--gold-bright);
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 45%, transparent);
}
.w-count {
  font-family: var(--font-mono);
  font-size: 10.5px;
  padding: 1px 7px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.08);
}
.w-tab.active .w-count { background: color-mix(in srgb, var(--gold) 22%, transparent); }

.w-table { display: flex; flex-direction: column; gap: 6px; }
.w-head,
.w-row {
  display: grid;
  grid-template-columns: minmax(220px, 2.4fr) 120px minmax(140px, 1fr) 100px 76px;
  gap: 14px;
  align-items: center;
}
.w-head {
  padding: 0 14px 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.w-row {
  padding: 12px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.w-item { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.w-title {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 13.5px; font-weight: 500; color: var(--fg-0);
}
.w-sub { font-size: 12px; color: var(--fg-2); }

.w-profile { font-family: var(--font-mono); font-size: 11.5px; color: var(--fg-1); }
.w-since { font-size: 12px; color: var(--fg-2); }
.w-search { font-family: var(--font-mono); font-size: 11px; color: var(--fg-3); }

.w-actions { display: flex; gap: 4px; justify-content: flex-end; }
.w-btn {
  width: 30px; height: 30px;
  display: flex; align-items: center; justify-content: center;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.w-btn:hover { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }

.w-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
  margin-top: 6px;
}

@media (max-width: 960px) {
  .w-head { display: none; }
  .w-row {
    grid-template-columns: minmax(0, 1fr) auto;
    row-gap: 8px;
  }
  .w-profile, .w-search { display: none; }
  .w-since { grid-column: 1; }
  .w-actions { grid-row: 1; grid-column: 2; }
}
</style>
