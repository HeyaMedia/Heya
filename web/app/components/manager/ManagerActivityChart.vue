<script setup lang="ts">
// Daily-count bar chart for manager activity (queries/grabs per day).
// One measure per chart (no dual axes); failures stack on top of the same
// unit in the reserved status red. Bars are plain divs so the house tokens
// apply directly and the chart re-themes for free.
const props = withDefaults(defineProps<{
  days: { date: string, ok: number, bad?: number }[]
  title: string
  unit: string
  height?: number
}>(), { height: 88 })

const max = computed(() =>
  Math.max(1, ...props.days.map(day => day.ok + (day.bad ?? 0))))

const total = computed(() =>
  props.days.reduce((sum, day) => sum + day.ok + (day.bad ?? 0), 0))

const anyFailures = computed(() => props.days.some(day => (day.bad ?? 0) > 0))

const hovered = ref<number | null>(null)

function fmtDate(date: string): string {
  const parsed = new Date(date + 'T00:00:00')
  return parsed.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
}
</script>

<template>
  <figure class="mac">
    <figcaption class="mac-head">
      <span class="mac-title">{{ title }}</span>
      <span class="mac-legend">
        <span class="mac-total">{{ total.toLocaleString() }} {{ unit }}</span>
        <span v-if="anyFailures" class="mac-legend-item bad"><span class="mac-swatch bad" /> failed</span>
      </span>
    </figcaption>
    <div class="mac-plot" :style="{ height: `${height}px` }" role="img" :aria-label="`${title}: ${total} ${unit} across ${days.length} days`">
      <div
        v-for="(day, index) in days"
        :key="day.date"
        class="mac-col"
        @mouseenter="hovered = index"
        @mouseleave="hovered = null"
      >
        <div class="mac-bar">
          <div v-if="day.bad" class="mac-seg bad" :style="{ height: `${(day.bad / max) * 100}%` }" />
          <div v-if="day.ok" class="mac-seg ok" :style="{ height: `${(day.ok / max) * 100}%` }" />
        </div>
        <div v-if="hovered === index" class="mac-tip surface">
          <div class="mac-tip-date">{{ fmtDate(day.date) }}</div>
          <div class="mac-tip-row"><span class="mac-swatch ok" /> {{ day.ok.toLocaleString() }} {{ unit }}</div>
          <div v-if="day.bad" class="mac-tip-row"><span class="mac-swatch bad" /> {{ day.bad.toLocaleString() }} failed</div>
        </div>
      </div>
    </div>
    <div class="mac-axis">
      <span>{{ fmtDate(days[0]?.date ?? '') }}</span>
      <span>{{ fmtDate(days[days.length - 1]?.date ?? '') }}</span>
    </div>
  </figure>
</template>

<style scoped>
.mac { margin: 0; display: flex; flex-direction: column; gap: 6px; min-width: 0; }

.mac-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}
.mac-title {
  font-family: var(--font-mono);
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.mac-legend { display: flex; align-items: baseline; gap: 10px; }
.mac-total { font-size: 12px; font-weight: 600; color: var(--fg-1); }
.mac-legend-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 10px;
  color: var(--fg-2);
}
.mac-swatch {
  width: 8px; height: 8px;
  border-radius: 2px;
  display: inline-block;
}
.mac-swatch.ok { background: var(--gold); }
.mac-swatch.bad { background: var(--bad); }

.mac-plot {
  display: flex;
  align-items: flex-end;
  gap: 2px;
  border-bottom: 1px solid var(--hair);
  padding-bottom: 1px;
}
.mac-col {
  position: relative;
  flex: 1;
  min-width: 3px;
  max-width: 42px;
  height: 100%;
  display: flex;
  align-items: flex-end;
  cursor: default;
}
.mac-bar {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  gap: 1px;
}
.mac-seg {
  width: 100%;
  min-height: 2px;
  transition: filter 0.1s;
}
.mac-seg.ok { background: var(--gold); border-radius: 3px 3px 0 0; }
.mac-seg.bad { background: var(--bad); border-radius: 3px 3px 0 0; }
.mac-col:hover .mac-seg { filter: brightness(1.15); }

.mac-tip {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 50%;
  transform: translateX(-50%);
  padding: 7px 10px;
  border-radius: var(--r-sm);
  white-space: nowrap;
  z-index: 10;
  pointer-events: none;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.mac-tip-date {
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  color: var(--fg-2);
  letter-spacing: 0.04em;
}
.mac-tip-row {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11.5px;
  color: var(--fg-0);
}

.mac-axis {
  display: flex;
  justify-content: space-between;
  font-family: var(--font-mono);
  font-size: 9px;
  color: var(--fg-3);
  letter-spacing: 0.04em;
}
</style>
