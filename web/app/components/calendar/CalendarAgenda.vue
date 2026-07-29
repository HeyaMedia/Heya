<script setup lang="ts">
// Day-grouped agenda list — the calendar's readable core. Presentation-only:
// entries arrive sorted by date, already mapped into the generic contract.
import { parseCalendarDate, toCalendarDate, type CalendarEntry } from '~/components/calendar/calendarEntry'

const props = defineProps<{
  entries: CalendarEntry[]
  emptyText?: string
}>()

type DayGroup = { date: string, label: string, sub: string, isToday: boolean, entries: CalendarEntry[] }

const dayFormat = new Intl.DateTimeFormat(undefined, { weekday: 'long' })
const subFormat = new Intl.DateTimeFormat(undefined, { day: '2-digit', month: 'short', year: 'numeric' })

const days = computed<DayGroup[]>(() => {
  // Date-part arithmetic, not ±24h: DST days are 23/25 hours long and
  // millisecond math would mislabel Tomorrow/Yesterday around transitions.
  const now = new Date()
  const today = toCalendarDate(now)
  const tomorrow = toCalendarDate(new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1))
  const yesterday = toCalendarDate(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1))
  const groups: DayGroup[] = []
  for (const entry of props.entries) {
    const last = groups[groups.length - 1]
    if (last && last.date === entry.date) {
      last.entries.push(entry)
      continue
    }
    const parsed = parseCalendarDate(entry.date)
    const label = entry.date === today
      ? 'Today'
      : entry.date === tomorrow
        ? 'Tomorrow'
        : entry.date === yesterday
          ? 'Yesterday'
          : dayFormat.format(parsed)
    groups.push({
      date: entry.date,
      label,
      sub: subFormat.format(parsed),
      isToday: entry.date === today,
      entries: [entry],
    })
  }
  return groups
})
</script>

<template>
  <div class="cal-agenda">
    <div v-for="day in days" :key="day.date" class="cal-day">
      <div class="cal-day-head">
        <span class="cal-day-label" :class="{ today: day.isToday }">{{ day.label }}</span>
        <span class="cal-day-date">{{ day.sub }}</span>
        <span class="cal-day-rule" />
      </div>
      <ul class="cal-list">
        <li v-for="entry in day.entries" :key="entry.id">
          <component
            :is="entry.to ? resolveComponent('NuxtLink') : 'div'"
            :to="entry.to"
            class="cal-row"
            :class="{ linked: !!entry.to }"
          >
            <span v-if="entry.icon" class="cal-kind"><Icon :name="entry.icon" :size="15" /></span>
            <div class="cal-body">
              <div class="cal-title">
                {{ entry.title }}
                <span v-if="entry.library" class="cal-lib">{{ entry.library.label }}</span>
                <span v-for="tag in entry.tags" :key="tag.label" class="cal-tag" :class="`tone-${tag.tone}`">{{ tag.label }}</span>
              </div>
              <div v-if="entry.subtitle" class="cal-sub">{{ entry.subtitle }}</div>
            </div>
            <StatusBadge v-if="entry.badge" :state="entry.badge.state">{{ entry.badge.label }}</StatusBadge>
          </component>
        </li>
      </ul>
    </div>

    <div v-if="days.length === 0" class="cal-empty">
      <Icon name="info" :size="14" /> {{ emptyText ?? 'No dated releases in this range.' }}
    </div>
  </div>
</template>

<style scoped>
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
.cal-day-label.today { color: var(--gold-bright); }
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
  gap: 12px;
  padding: 11px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  color: inherit;
  text-decoration: none;
}
.cal-row.linked { transition: background 0.12s, border-color 0.12s; }
.cal-row.linked:hover { background: var(--bg-3); border-color: color-mix(in srgb, var(--gold) 30%, var(--border)); }

.cal-kind {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.05);
  color: var(--fg-2);
}

.cal-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.cal-title {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: 13.5px; font-weight: 500; color: var(--fg-0);
}
.cal-lib {
  display: inline-flex;
  padding: 1px 7px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  white-space: nowrap;
}
.cal-sub { font-size: 12px; color: var(--fg-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.cal-tag {
  display: inline-flex;
  padding: 1px 7px;
  border-radius: 999px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  white-space: nowrap;
}
.cal-tag.tone-gold { color: var(--gold-bright); background: var(--gold-soft); border: 1px solid color-mix(in srgb, var(--gold) 40%, transparent); }
.cal-tag.tone-good { color: var(--good); background: color-mix(in srgb, var(--good) 10%, transparent); border: 1px solid color-mix(in srgb, var(--good) 30%, transparent); }
.cal-tag.tone-bad { color: var(--bad); background: color-mix(in srgb, var(--bad) 10%, transparent); border: 1px solid color-mix(in srgb, var(--bad) 30%, transparent); }
.cal-tag.tone-neutral { color: var(--fg-2); background: rgb(var(--ink) / 0.05); border: 1px solid var(--border); }

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
}
</style>
