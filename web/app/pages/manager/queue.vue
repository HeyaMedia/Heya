<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { MOCK_LIBRARY_OPTIONS, MOCK_QUEUE } from '~/utils/managerMock'

const filter = ref<string[]>([])

const rows = computed(() => filter.value.length === 0
  ? MOCK_QUEUE
  : MOCK_QUEUE.filter(q => filter.value.includes(q.library)))

const STATUS_META: Record<string, { label: string, state: 'ok' | 'warn' | 'error' | 'idle' }> = {
  downloading: { label: 'downloading', state: 'ok' },
  importing: { label: 'importing', state: 'ok' },
  seeding: { label: 'seeding', state: 'idle' },
  paused: { label: 'paused', state: 'idle' },
  stalled: { label: 'stalled', state: 'error' },
}

function pct(row: { sizeGb: number, doneGb: number }) {
  if (row.sizeGb <= 0) return 0
  return Math.min(100, Math.round((row.doneGb / row.sizeGb) * 100))
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Queue"
      icon="cloud-download"
      eyebrow="Manager · Preview — mock data"
      description="Everything the download clients are working on, from grab to import. One queue across all libraries and both protocols."
    />

    <div class="q-toolbar">
      <ManagerLibraryFilter v-model="filter" :options="MOCK_LIBRARY_OPTIONS" />
    </div>

    <div class="q-table">
      <div class="q-head">
        <span>Release</span>
        <span>Quality</span>
        <span class="q-col-progress">Progress</span>
        <span>Speed / ETA</span>
        <span>Status</span>
        <span class="q-col-actions" aria-hidden="true" />
      </div>
      <div v-for="row in rows" :key="row.id" class="q-row">
        <div class="q-release">
          <div class="q-title">{{ row.title }} <ManagerLibraryChip :library="row.library" /></div>
          <div class="q-sub">
            {{ row.subtitle }}
            <span class="q-via">· {{ row.client }} · {{ row.indexer }}</span>
          </div>
        </div>
        <span class="q-quality">{{ row.quality }}</span>
        <div class="q-progress">
          <div class="q-bar" :class="{ stalled: row.status === 'stalled', paused: row.status === 'paused' }">
            <div class="q-bar-fill" :style="{ width: `${pct(row)}%` }" />
          </div>
          <span class="q-progress-text">{{ row.doneGb }} / {{ row.sizeGb }} GB</span>
        </div>
        <div class="q-speed">
          <span>{{ row.speed }}</span>
          <span class="q-eta">{{ row.eta }}</span>
        </div>
        <StatusBadge :state="STATUS_META[row.status]!.state">{{ STATUS_META[row.status]!.label }}</StatusBadge>
        <div class="q-actions">
          <AppTooltip :label="row.status === 'paused' ? 'Resume' : 'Pause'">
            <button type="button" class="q-btn"><Icon :name="row.status === 'paused' ? 'play' : 'pause'" :size="14" /></button>
          </AppTooltip>
          <AppTooltip label="Remove & blocklist">
            <button type="button" class="q-btn danger"><Icon name="trash" :size="14" /></button>
          </AppTooltip>
        </div>
      </div>
    </div>

    <div v-if="rows.length === 0" class="q-empty">
      <Icon name="info" :size="14" /> The queue is empty for the selected libraries.
    </div>
  </div>
</template>

<style scoped>
.q-toolbar { margin-bottom: 18px; }

.q-table { display: flex; flex-direction: column; gap: 6px; }
.q-head,
.q-row {
  display: grid;
  grid-template-columns: minmax(220px, 2.2fr) 130px minmax(160px, 1.4fr) 110px 120px 76px;
  gap: 14px;
  align-items: center;
}
.q-head {
  padding: 0 14px 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.q-row {
  padding: 12px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.q-release { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.q-title {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 13.5px; font-weight: 500; color: var(--fg-0);
}
.q-sub {
  font-size: 12px; color: var(--fg-2);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.q-via { color: var(--fg-3); }

.q-quality {
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--fg-1);
}

.q-progress { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.q-bar {
  height: 5px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.08);
  overflow: hidden;
}
.q-bar-fill {
  height: 100%;
  border-radius: 999px;
  background: var(--gold);
  transition: width 0.3s ease;
}
.q-bar.stalled .q-bar-fill { background: var(--bad); }
.q-bar.paused .q-bar-fill { background: var(--fg-3); }
.q-progress-text {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--fg-3);
}

.q-speed {
  display: flex; flex-direction: column; gap: 2px;
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--fg-1);
}
.q-eta { color: var(--fg-3); font-size: 10.5px; }

.q-actions { display: flex; gap: 4px; justify-content: flex-end; }
.q-btn {
  width: 30px; height: 30px;
  display: flex; align-items: center; justify-content: center;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.q-btn:hover { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }
.q-btn.danger:hover { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 40%, transparent); }

.q-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
  margin-top: 6px;
}

/* Tablet/phone: collapse the grid into a two-line card. */
@media (max-width: 960px) {
  .q-head { display: none; }
  .q-row {
    grid-template-columns: minmax(0, 1fr) auto;
    row-gap: 10px;
  }
  .q-release { grid-column: 1; }
  .q-quality { display: none; }
  .q-progress { grid-column: 1 / -1; order: 3; }
  .q-speed { grid-row: 1; grid-column: 2; align-items: flex-end; }
  .q-actions { order: 4; grid-column: 2; }
  .q-row > :nth-child(5) { order: 4; grid-column: 1; justify-self: start; }
}
</style>
