<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { MOCK_HISTORY, MOCK_LIBRARY_OPTIONS } from '~/utils/managerMock'

const filter = ref<string[]>([])

const rows = computed(() => filter.value.length === 0
  ? MOCK_HISTORY
  : MOCK_HISTORY.filter(h => filter.value.includes(h.library)))

const EVENT_META: Record<string, { icon: string, cls: string }> = {
  grabbed: { icon: 'cloud-download', cls: 'ev-grab' },
  imported: { icon: 'check', cls: 'ev-ok' },
  upgraded: { icon: 'sparkle', cls: 'ev-ok' },
  failed: { icon: 'warning', cls: 'ev-bad' },
  renamed: { icon: 'pencil', cls: 'ev-dim' },
  deleted: { icon: 'trash', cls: 'ev-dim' },
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="History"
      icon="clock"
      eyebrow="Manager · Preview — mock data"
      description="The full audit trail: every grab, import, upgrade, rename and failure, across all libraries."
    />

    <div class="h-toolbar">
      <ManagerLibraryFilter v-model="filter" :options="MOCK_LIBRARY_OPTIONS" />
    </div>

    <ul class="h-feed">
      <li v-for="ev in rows" :key="ev.id" class="h-row">
        <span class="h-icon" :class="EVENT_META[ev.event]?.cls"><Icon :name="EVENT_META[ev.event]?.icon ?? 'info'" :size="14" /></span>
        <div class="h-body">
          <div class="h-title">
            <span class="h-event">{{ ev.event }}</span>
            {{ ev.title }}
            <ManagerLibraryChip :library="ev.library" />
          </div>
          <div class="h-detail">{{ ev.detail }}</div>
        </div>
        <div class="h-meta">
          <span class="h-time">{{ ev.at }}</span>
          <span class="h-via">{{ ev.via }}</span>
        </div>
      </li>
    </ul>

    <div v-if="rows.length === 0" class="h-empty">
      <Icon name="info" :size="14" /> No history for the selected libraries.
    </div>
  </div>
</template>

<style scoped>
.h-toolbar { margin-bottom: 18px; }

.h-feed { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.h-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.h-icon {
  width: 30px; height: 30px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
  color: var(--fg-2);
}
.h-icon.ev-ok { color: var(--good); }
.h-icon.ev-bad { color: var(--bad); }
.h-icon.ev-grab { color: var(--gold); }
.h-icon.ev-dim { color: var(--fg-3); }

.h-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.h-title {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 13.5px; font-weight: 500; color: var(--fg-0);
}
.h-event {
  font-family: var(--font-mono);
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.h-detail {
  font-size: 12px; color: var(--fg-2);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.h-meta {
  display: flex; flex-direction: column; align-items: flex-end; gap: 2px;
  flex-shrink: 0;
  padding-top: 2px;
}
.h-time { font-family: var(--font-mono); font-size: 11px; color: var(--fg-3); }
.h-via { font-family: var(--font-mono); font-size: 10px; color: var(--fg-3); text-transform: uppercase; letter-spacing: 0.05em; }

.h-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
  margin-top: 6px;
}

@media (max-width: 720px) {
  .h-detail { white-space: normal; }
}
</style>
