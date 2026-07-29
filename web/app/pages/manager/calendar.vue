<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { MOCK_CALENDAR, MOCK_LIBRARY_OPTIONS } from '~/utils/managerMock'

const filter = ref<string[]>([])

const days = computed(() => {
  if (filter.value.length === 0) return MOCK_CALENDAR
  return MOCK_CALENDAR
    .map(day => ({ ...day, entries: day.entries.filter(e => filter.value.includes(e.library)) }))
    .filter(day => day.entries.length > 0)
})

const STATUS_META: Record<string, { label: string, state: 'ok' | 'warn' | 'error' | 'idle' }> = {
  downloaded: { label: 'downloaded', state: 'ok' },
  airing: { label: 'airs today', state: 'warn' },
  missing: { label: 'missing', state: 'error' },
  announced: { label: 'upcoming', state: 'idle' },
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Calendar"
      icon="calendar"
      eyebrow="Manager · Preview — mock data"
      description="Upcoming and recent releases across every monitored library — episodes airing, albums dropping, movies going digital."
    />

    <div class="cal-toolbar">
      <ManagerLibraryFilter v-model="filter" :options="MOCK_LIBRARY_OPTIONS" />
    </div>

    <div v-for="day in days" :key="day.label" class="cal-day">
      <div class="cal-day-head">
        <span class="cal-day-label">{{ day.label }}</span>
        <span class="cal-day-date">{{ day.date }}</span>
        <span class="cal-day-rule" />
      </div>
      <ul class="cal-list">
        <li v-for="entry in day.entries" :key="entry.id" class="cal-row">
          <span class="cal-time">{{ entry.time }}</span>
          <div class="cal-body">
            <div class="cal-title">{{ entry.title }} <ManagerLibraryChip :library="entry.library" /></div>
            <div class="cal-sub">{{ entry.subtitle }}</div>
          </div>
          <StatusBadge :state="STATUS_META[entry.status]!.state">{{ STATUS_META[entry.status]!.label }}</StatusBadge>
        </li>
      </ul>
    </div>

    <div v-if="days.length === 0" class="cal-empty">
      <Icon name="info" :size="14" /> Nothing scheduled for the selected libraries.
    </div>
  </div>
</template>

<style scoped>
.cal-toolbar { margin-bottom: 22px; }

.cal-day { margin-bottom: 22px; }
.cal-day-head {
  display: flex;
  align-items: baseline;
  gap: 10px;
  margin-bottom: 10px;
}
.cal-day-label {
  font-family: var(--font-display);
  font-variation-settings: "wdth" 100;
  font-size: 17px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--fg-0);
}
.cal-day-date {
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.cal-day-rule { flex: 1; border-top: 1px solid var(--hair); align-self: center; }

.cal-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.cal-row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 11px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.cal-time {
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--fg-3);
  width: 42px;
  flex-shrink: 0;
  text-align: right;
}
.cal-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.cal-title {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 13.5px; font-weight: 500; color: var(--fg-0);
}
.cal-sub { font-size: 12px; color: var(--fg-2); }

.cal-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}

@media (max-width: 720px) {
  .cal-row { flex-wrap: wrap; }
  .cal-time { width: auto; text-align: left; }
}
</style>
