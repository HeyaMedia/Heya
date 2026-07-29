<script setup lang="ts">
// Month grid — the scanning view. Weeks start Monday; every day cell lists
// ALL of its events with their full fields (title, detail line, state dot,
// tag), and the whole month is ONE css grid so `grid-auto-rows: 1fr` sizes
// every week row to the tallest one — an Outlook-style fused table, not
// islands of cards. Clicking a day selects it (the consumer typically shows
// that day's agenda below). Presentation-only, same generic entry contract
// as CalendarAgenda.
import { parseCalendarDate, toCalendarDate, type CalendarEntry } from '~/components/calendar/calendarEntry'

const props = defineProps<{
  /** Which month to render, as YYYY-MM. */
  month: string
  entries: CalendarEntry[]
  /** Opt-in day selection (v-model:selected-date + gold highlight). Off by
      default — most consumers act on individual events via @select. */
  selectable?: boolean
}>()

const emit = defineEmits<{ select: [entry: CalendarEntry] }>()

const selectedDate = defineModel<string | null>('selectedDate', { default: null })

type DayCell = {
  date: string
  dayOfMonth: number
  inMonth: boolean
  weekend: boolean
  isToday: boolean
  entries: CalendarEntry[]
}

const byDate = computed(() => {
  const map = new Map<string, CalendarEntry[]>()
  for (const entry of props.entries) {
    const list = map.get(entry.date)
    if (list) list.push(entry)
    else map.set(entry.date, [entry])
  }
  return map
})

const weekdayFormat = new Intl.DateTimeFormat(undefined, { weekday: 'short' })
const weekdays = computed(() => {
  // A known Monday (2024-01-01) anchors the label order.
  const monday = new Date(2024, 0, 1)
  return Array.from({ length: 7 }, (_, i) =>
    weekdayFormat.format(new Date(monday.getFullYear(), monday.getMonth(), monday.getDate() + i)))
})

const cells = computed<DayCell[]>(() => {
  const [y, m] = props.month.split('-').map(Number)
  const first = new Date(y!, (m ?? 1) - 1, 1)
  // Walk back to Monday (getDay(): 0 = Sunday).
  const start = new Date(first)
  start.setDate(first.getDate() - ((first.getDay() + 6) % 7))
  const today = toCalendarDate(new Date())

  const out: DayCell[] = []
  const cursor = new Date(start)
  do {
    for (let i = 0; i < 7; i++) {
      const date = toCalendarDate(cursor)
      out.push({
        date,
        dayOfMonth: cursor.getDate(),
        inMonth: cursor.getMonth() === (m ?? 1) - 1,
        weekend: i >= 5,
        isToday: date === today,
        entries: byDate.value.get(date) ?? [],
      })
      cursor.setDate(cursor.getDate() + 1)
    }
  } while (cursor.getMonth() === (m ?? 1) - 1 && out.length < 6 * 7)
  return out
})

function toggleDay(cell: DayCell) {
  if (!props.selectable) return
  selectedDate.value = selectedDate.value === cell.date ? null : cell.date
}
</script>

<template>
  <div class="cmg">
    <div class="cmg-weekdays">
      <span v-for="day in weekdays" :key="day" class="cmg-weekday">{{ day }}</span>
    </div>
    <div class="cmg-grid">
      <component
        :is="selectable ? 'button' : 'div'"
        v-for="cell in cells"
        :key="cell.date"
        :type="selectable ? 'button' : undefined"
        class="cmg-cell"
        :class="{
          outside: !cell.inMonth,
          weekend: cell.weekend,
          today: cell.isToday,
          selected: selectable && selectedDate === cell.date,
          selectable,
        }"
        :aria-label="selectable ? `${cell.date}, ${cell.entries.length} releases` : undefined"
        :aria-pressed="selectable ? selectedDate === cell.date : undefined"
        @click="toggleDay(cell)"
      >
        <span class="cmg-daynum">{{ cell.dayOfMonth }}</span>
        <button
          v-for="entry in cell.entries"
          :key="entry.id"
          type="button"
          class="cmg-chip"
          :aria-label="`${entry.title}${entry.subtitle ? ' — ' + entry.subtitle : ''}`"
          @click.stop="emit('select', entry)"
        >
          <span class="cmg-chip-head">
            <span v-if="entry.badge" class="cmg-dot" :class="`state-${entry.badge.state}`" />
            <Icon v-if="entry.icon" :name="entry.icon" :size="10" class="cmg-chip-icon" />
            <span class="cmg-chip-title">{{ entry.title }}</span>
            <span v-if="entry.tags?.length" class="cmg-chip-tag" :class="`tone-${entry.tags[0]!.tone}`">{{ entry.tags[0]!.label }}</span>
          </span>
          <span v-if="entry.subtitle" class="cmg-chip-sub">{{ entry.subtitle }}</span>
        </button>
      </component>
    </div>
  </div>
</template>

<style scoped>
/* Outlook-style fused table: one continuous bordered grid, weeks as rows,
   hairline separators instead of per-day card islands. */
.cmg {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
  overflow: hidden;
}

.cmg-weekdays {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
}
.cmg-weekday {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--fg-3);
  padding: 8px 10px 7px;
  text-align: right;
}
.cmg-weekday + .cmg-weekday { border-left: 1px solid var(--hair); }

/* One grid for the whole month: 1fr auto-rows give every week row the
   height of the tallest, Outlook-style. */
.cmg-grid {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  grid-auto-rows: 1fr;
}

.cmg-cell {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 3px;
  min-height: 104px;
  padding: 6px;
  text-align: left;
  background: none;
  border: 0;
  border-top: 1px solid var(--hair);
  transition: background 0.12s;
  min-width: 0;
}
.cmg-cell.selectable { cursor: pointer; }
.cmg-cell:not(:nth-child(7n + 1)) { border-left: 1px solid var(--hair); }
.cmg-cell.selectable:hover { background: rgb(var(--ink) / 0.04); }
.cmg-cell.weekend { background: rgb(var(--shade) / 0.14); }
.cmg-cell.weekend:hover { background: rgb(var(--ink) / 0.04); }
.cmg-cell.outside { background: rgb(var(--shade) / 0.28); }
.cmg-cell.outside .cmg-daynum,
.cmg-cell.outside .cmg-chip { opacity: 0.5; }
.cmg-cell.selected,
.cmg-cell.selected.weekend { background: var(--gold-soft); }

/* Outlook grammar: the day number sits top-right; today wears a gold pill. */
.cmg-daynum {
  align-self: flex-end;
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-2);
  min-width: 20px;
  line-height: 20px;
  text-align: center;
  border-radius: 999px;
}
.cmg-cell.today .cmg-daynum {
  background: var(--gold);
  color: var(--accent-ink, var(--bg-0));
}

.cmg-chip {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 1px;
  padding: 3px 6px;
  border: 0;
  border-radius: 4px;
  background: rgb(var(--ink) / 0.06);
  min-width: 0;
  text-align: left;
  cursor: pointer;
  transition: background 0.12s;
}
.cmg-chip:hover { background: rgb(var(--ink) / 0.12); }
.cmg-chip-head {
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 0;
  font-size: 10.5px;
  color: var(--fg-1);
}
.cmg-chip-icon { flex-shrink: 0; color: var(--fg-3); }
.cmg-chip-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}
.cmg-chip-sub {
  font-size: 9.5px;
  color: var(--fg-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding-left: 13px;
}

.cmg-chip-tag {
  flex-shrink: 0;
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: 8.5px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.cmg-chip-tag.tone-gold { color: var(--gold-bright); }
.cmg-chip-tag.tone-good { color: var(--good); }
.cmg-chip-tag.tone-bad { color: var(--bad); }
.cmg-chip-tag.tone-neutral { color: var(--fg-3); }

.cmg-dot { width: 5px; height: 5px; border-radius: 50%; flex-shrink: 0; }
.cmg-dot.state-ok { background: var(--good); }
.cmg-dot.state-warn { background: var(--gold-bright); }
.cmg-dot.state-error { background: var(--bad); }
.cmg-dot.state-idle { background: var(--fg-3); }
</style>
