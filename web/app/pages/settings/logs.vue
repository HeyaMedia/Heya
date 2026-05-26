<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import type { components } from '#open-fetch-schemas/heya'
import type { LogPayload } from '~/composables/useEventBus'

type Entry = components['schemas']['Entry']

const { $heya } = useNuxtApp()
const { on, connected: wsConnected } = useEventBus()

const LEVELS = ['trace', 'debug', 'info', 'warn', 'error'] as const
type Level = (typeof LEVELS)[number]

const MAX_BUFFER = 5000

const logs = ref<Entry[]>([])
const loading = ref(true)
const paused = ref(false)
const autoScroll = ref(true)
const droppedWhilePaused = ref(0)
const flash = ref<{ kind: 'ok' | 'err', text: string } | null>(null)

const search = ref('')
const enabledLevels = ref<Set<Level>>(new Set(LEVELS))

// Tail length defaults to "last 500" — much faster to render than the full
// 5k ring and still gives plenty of recent context for the eye-test case.
const tailWindow = ref(500)
const TAIL_OPTIONS = [200, 500, 1000, 2000]

const listRef = ref<HTMLElement | null>(null)

async function backfill() {
  loading.value = true
  try {
    const recent = await $heya('/api/logs', { query: { n: tailWindow.value } }) as Entry[]
    // Backend returns newest-first; we render oldest-first so the live tail
    // appends naturally at the bottom.
    logs.value = (recent ?? []).slice().reverse()
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load logs.' }
  } finally {
    loading.value = false
  }
}

// Live tail via the existing WebSocket event bus — `log` events are bridged
// from the ring buffer in cmd/serve.go.
const unsubLog = on('log', (event) => {
  if (paused.value) {
    droppedWhilePaused.value++
    return
  }
  const p = event.payload as LogPayload
  logs.value.push({
    time: event.ts,
    level: p.level,
    message: p.message,
    fields: p.fields,
  } as Entry)
  if (logs.value.length > MAX_BUFFER) {
    logs.value.splice(0, logs.value.length - MAX_BUFFER)
  }
  if (autoScroll.value) scheduleScroll()
})

onUnmounted(() => { unsubLog?.() })

function togglePause() {
  paused.value = !paused.value
  if (!paused.value) {
    droppedWhilePaused.value = 0
  }
}

function clearLogs() {
  logs.value = []
  droppedWhilePaused.value = 0
}

async function reloadBackfill() {
  await backfill()
  scheduleScroll()
}

function exportJSON() {
  const data = filteredLogs.value
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `heya-logs-${new Date().toISOString().replace(/[:.]/g, '-')}.json`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  setTimeout(() => URL.revokeObjectURL(url), 1000)
  flash.value = { kind: 'ok', text: `Exported ${data.length} entries.` }
}

function toggleLevel(lvl: Level) {
  const next = new Set(enabledLevels.value)
  if (next.has(lvl)) next.delete(lvl)
  else next.add(lvl)
  enabledLevels.value = next
}

function onlyLevel(lvl: Level) {
  enabledLevels.value = new Set([lvl])
}

function allLevels() {
  enabledLevels.value = new Set(LEVELS)
}

const filteredLogs = computed(() => {
  const q = search.value.trim().toLowerCase()
  let out = logs.value
  if (enabledLevels.value.size !== LEVELS.length) {
    out = out.filter(e => enabledLevels.value.has(e.level as Level))
  }
  if (q) {
    out = out.filter(e => {
      if (e.message?.toLowerCase().includes(q)) return true
      if (e.fields) {
        for (const v of Object.values(e.fields)) {
          if (String(v).toLowerCase().includes(q)) return true
        }
      }
      return false
    })
  }
  // Cap render to tail window — keeps rendering fast even with a 5k buffer.
  return out.slice(-tailWindow.value)
})

const levelCounts = computed(() => {
  const counts: Record<string, number> = {}
  for (const lvl of LEVELS) counts[lvl] = 0
  for (const e of logs.value) {
    const cur = counts[e.level]
    if (cur !== undefined) counts[e.level] = cur + 1
  }
  return counts
})

function countFor(lvl: Level): number {
  return levelCounts.value[lvl] ?? 0
}

const totalShown = computed(() => filteredLogs.value.length)
const totalBuffered = computed(() => logs.value.length)

let scrollPending = false
function scheduleScroll() {
  if (scrollPending) return
  scrollPending = true
  requestAnimationFrame(() => {
    scrollPending = false
    const el = listRef.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

function formatTime(t: string) {
  try {
    const d = new Date(t)
    const h = String(d.getHours()).padStart(2, '0')
    const m = String(d.getMinutes()).padStart(2, '0')
    const s = String(d.getSeconds()).padStart(2, '0')
    const ms = String(d.getMilliseconds()).padStart(3, '0')
    return `${h}:${m}:${s}.${ms}`
  } catch { return '' }
}

function fieldsToString(fields?: Record<string, any>): string {
  if (!fields) return ''
  return Object.entries(fields)
    .map(([k, v]) => `${k}=${typeof v === 'object' ? JSON.stringify(v) : String(v)}`)
    .join(' · ')
}

function hasFields(e: Entry): boolean {
  return !!e.fields && Object.keys(e.fields).length > 0
}

const expanded = ref<Set<number>>(new Set())
function toggleExpand(idx: number) {
  const next = new Set(expanded.value)
  if (next.has(idx)) next.delete(idx)
  else next.add(idx)
  expanded.value = next
}

onMounted(async () => {
  await backfill()
  scheduleScroll()
})

watch(tailWindow, () => backfill())

// Polling /api/logs while offline doesn't make sense — the WS bus is the
// only source of new entries; a periodic GET would just give us the same
// last-N every time. Skip the polling fallback; keep the reconnect catchup
// so a fresh backfill arrives the moment WS recovers.
useLiveFallback(backfill, { pollWhileOffline: 0, immediate: false })
</script>

<template>
  <div class="logs-page">
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Logs</h2>
      <p class="sv2-page-desc">
        Live tail of the in-process ring buffer. Filter by level, search across
        messages and fields, pause to inspect, or export the current view as
        JSON. Adjust the runtime log level on
        <NuxtLink to="/settings/diagnostics" class="inline-link">Diagnostics</NuxtLink>.
      </p>
    </header>

    <div class="tiles">
      <MetricTile label="Buffered" :value="totalBuffered" icon="clipboard"
        :sub="`cap ${MAX_BUFFER}`" />
      <MetricTile
        v-for="lvl in LEVELS" :key="lvl"
        :label="lvl"
        :value="countFor(lvl)"
        :icon="lvl === 'error' ? 'warning' : (lvl === 'warn' ? 'pulse' : lvl === 'info' ? 'info' : lvl === 'debug' ? 'wrench' : 'eq')"
        :tone="lvl === 'error' && countFor(lvl) > 0 ? 'bad' : (lvl === 'warn' && countFor(lvl) > 0 ? 'warn' : 'neutral')"
      />
    </div>

    <div class="toolbar">
      <div class="tb-left">
        <div class="lvl-row">
          <button
            v-for="lvl in LEVELS"
            :key="lvl"
            class="lvl-chip"
            :class="[lvl, { active: enabledLevels.has(lvl) }]"
            :title="`Toggle ${lvl} — double-click to isolate`"
            @click="toggleLevel(lvl)"
            @dblclick="onlyLevel(lvl)"
          >
            <span class="lvl-dot" />
            {{ lvl }}
          </button>
          <button class="lvl-all" :disabled="enabledLevels.size === LEVELS.length" @click="allLevels">all</button>
        </div>
        <input v-model="search" class="search-input" placeholder="search message + fields…" spellcheck="false" />
      </div>

      <div class="tb-right">
        <select v-model="tailWindow" class="tail-select" title="Render tail window">
          <option v-for="n in TAIL_OPTIONS" :key="n" :value="n">last {{ n }}</option>
        </select>
        <label class="check-row" :title="autoScroll ? 'Auto-scroll is on' : 'Auto-scroll is off'">
          <input v-model="autoScroll" type="checkbox" />
          <span>auto-scroll</span>
        </label>
        <button class="sv2-btn ghost" @click="reloadBackfill" title="Re-fetch backfill from /api/logs">
          <Icon name="refresh" :size="12" /> Reload
        </button>
        <button class="sv2-btn ghost" :disabled="logs.length === 0" @click="clearLogs" title="Clear the in-memory buffer">
          <Icon name="trash" :size="12" /> Clear
        </button>
        <button class="sv2-btn" :class="paused ? 'warn' : 'ghost'" @click="togglePause">
          <Icon :name="paused ? 'play' : 'pause'" :size="12" />
          {{ paused ? `Paused · +${droppedWhilePaused}` : 'Pause' }}
        </button>
        <button class="sv2-btn primary" :disabled="filteredLogs.length === 0" @click="exportJSON">
          <Icon name="download" :size="12" /> Export
        </button>
      </div>
    </div>

    <div class="status-bar">
      <span class="sb-count">
        Showing <strong>{{ totalShown }}</strong> of <strong>{{ totalBuffered }}</strong> buffered
        <template v-if="search || enabledLevels.size !== LEVELS.length"> · filtered</template>
      </span>
      <LiveDot :connected="wsConnected" :label="wsConnected ? 'Live · WS' : 'WS offline'" />
    </div>

    <div ref="listRef" class="log-list" :class="{ paused }">
      <div v-if="loading" class="log-state">
        <Icon name="spinner" :size="14" /> Loading backfill…
      </div>
      <div v-else-if="filteredLogs.length === 0" class="log-state">
        <Icon name="info" :size="14" />
        {{ search || enabledLevels.size !== LEVELS.length ? 'No entries match the filter.' : 'No logs yet — waiting for events.' }}
      </div>
      <div v-else>
        <div
          v-for="(e, i) in filteredLogs"
          :key="`${e.time}-${i}`"
          class="log-row"
          :class="[e.level, { expanded: expanded.has(i), 'has-fields': hasFields(e) }]"
          @click="hasFields(e) && toggleExpand(i)"
        >
          <span class="lr-time">{{ formatTime(e.time) }}</span>
          <span class="lr-level" :class="e.level">{{ e.level }}</span>
          <span class="lr-msg">{{ e.message }}</span>
          <span v-if="hasFields(e) && !expanded.has(i)" class="lr-fields">
            {{ fieldsToString(e.fields) }}
          </span>
          <div v-if="expanded.has(i) && hasFields(e)" class="lr-fields-expanded">
            <div v-for="(v, k) in e.fields" :key="String(k)" class="lr-field">
              <span class="lr-field-k">{{ k }}</span>
              <span class="lr-field-v">{{ typeof v === 'object' ? JSON.stringify(v, null, 2) : String(v) }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="flash" class="sv2-flash" :class="flash.kind">
      <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" />
      {{ flash.text }}
    </div>
  </div>
</template>

<style scoped>
.logs-page {
  display: flex;
  flex-direction: column;
  /* Fill the scrollable main area; the log list itself owns the overflow. */
  min-height: calc(100vh - 64px);
}

.sv2-page-head { margin-bottom: 16px; }
.sv2-page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.sv2-page-desc { margin: 6px 0 0; font-size: 13px; color: var(--fg-3); line-height: 1.55; }
.inline-link { color: var(--gold); text-decoration: none; }
.inline-link:hover { text-decoration: underline; }

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 8px;
  margin-bottom: 14px;
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md) var(--r-md) 0 0;
  border-bottom: 0;
  flex-wrap: wrap;
}
.tb-left, .tb-right { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }

.lvl-row { display: flex; gap: 4px; }
.lvl-chip {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 4px 10px;
  border-radius: 999px;
  font-family: var(--font-mono);
  font-size: 10.5px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.06em;
  background: rgba(255,255,255,0.02);
  border: 1px solid var(--border);
  color: var(--fg-4);
  cursor: pointer;
  transition: border-color 0.12s, color 0.12s, background 0.12s;
}
.lvl-chip:hover { border-color: var(--border-strong); color: var(--fg-2); }
.lvl-chip .lvl-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--fg-4); }
.lvl-chip.active        { background: rgba(255,255,255,0.05); color: var(--fg-1); }
.lvl-chip.trace.active  { color: var(--fg-3); border-color: var(--fg-4); }
.lvl-chip.trace.active  .lvl-dot { background: var(--fg-3); }
.lvl-chip.debug.active  { color: var(--fg-2); border-color: rgba(255,255,255,0.20); }
.lvl-chip.debug.active  .lvl-dot { background: var(--fg-2); }
.lvl-chip.info.active   { color: rgb(140, 160, 255); border-color: rgba(140, 160, 255, 0.40); background: rgba(140, 160, 255, 0.08); }
.lvl-chip.info.active   .lvl-dot { background: rgb(140, 160, 255); }
.lvl-chip.warn.active   { color: var(--gold); border-color: rgba(230, 185, 74, 0.40); background: var(--gold-soft); }
.lvl-chip.warn.active   .lvl-dot { background: var(--gold); }
.lvl-chip.error.active  { color: var(--bad); border-color: rgba(217, 107, 107, 0.40); background: rgba(217, 107, 107, 0.10); }
.lvl-chip.error.active  .lvl-dot { background: var(--bad); }

.lvl-all {
  font-family: var(--font-mono);
  font-size: 10px; text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--fg-3);
  padding: 4px 8px;
  border-radius: var(--r-xs);
  border: 0;
  background: transparent;
  cursor: pointer;
}
.lvl-all:hover:not(:disabled) { color: var(--gold); }
.lvl-all:disabled { opacity: 0.4; cursor: not-allowed; }

.search-input {
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 12px;
  font-family: var(--font-mono);
  padding: 6px 10px;
  width: 240px;
  outline: none;
  transition: border-color 0.12s;
}
.search-input:focus { border-color: var(--gold); }

.tail-select {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-1);
  font-family: var(--font-mono);
  font-size: 11px;
  padding: 5px 8px;
  cursor: pointer;
}
.check-row {
  display: inline-flex; align-items: center; gap: 6px;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-2);
  cursor: pointer;
  padding: 4px 6px;
}
.check-row input { accent-color: var(--gold); }

.status-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 6px 14px;
  background: var(--bg-1);
  border-left: 1px solid var(--border);
  border-right: 1px solid var(--border);
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
}
.sb-count strong { color: var(--fg-1); font-weight: 700; }

.log-list {
  flex: 1;
  min-height: 360px;
  max-height: calc(100vh - 360px);
  overflow-y: auto;
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: 0 0 var(--r-md) var(--r-md);
  font-family: var(--font-mono);
  font-size: 11.5px;
  line-height: 1.55;
  padding: 6px 0;
}
.log-list.paused { box-shadow: inset 0 0 0 2px rgba(230, 185, 74, 0.30); }

.log-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3);
  padding: 16px 14px;
  font-family: var(--font-sans); font-size: 12.5px;
}

.log-row {
  display: grid;
  grid-template-columns: 90px 50px 1fr;
  gap: 8px;
  padding: 2px 14px;
  align-items: baseline;
}
.log-row.has-fields { cursor: pointer; }
.log-row:hover { background: rgba(255,255,255,0.02); }
.log-row.warn  { background: rgba(230, 185, 74, 0.04); }
.log-row.error { background: rgba(217, 107, 107, 0.06); }
.log-row.expanded { background: rgba(255,255,255,0.04); }

.lr-time   { color: var(--fg-4); white-space: nowrap; }
.lr-level  { font-weight: 700; text-transform: uppercase; font-size: 10px; letter-spacing: 0.04em; }
.lr-level.trace { color: var(--fg-4); }
.lr-level.debug { color: var(--fg-3); }
.lr-level.info  { color: rgb(140, 160, 255); }
.lr-level.warn  { color: var(--gold); }
.lr-level.error { color: var(--bad); }
.lr-msg { color: var(--fg-1); word-break: break-word; }
.lr-fields { color: var(--fg-4); margin-left: 8px; }

.lr-fields-expanded {
  grid-column: 3 / -1;
  margin-top: 4px;
  display: grid;
  grid-template-columns: minmax(120px, max-content) 1fr;
  gap: 2px 12px;
  padding: 6px 10px;
  border-left: 2px solid var(--gold);
  background: rgba(255,255,255,0.02);
}
.lr-field-k { color: var(--gold); font-size: 11px; }
.lr-field-v { color: var(--fg-1); font-size: 11px; white-space: pre-wrap; word-break: break-word; }

.sv2-btn {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 6px 12px;
  border-radius: var(--r-sm);
  font-size: 11.5px; font-weight: 500;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.sv2-btn.ghost { border: 1px solid var(--border); background: var(--bg-2); color: var(--fg-2); }
.sv2-btn.ghost:hover:not(:disabled) { border-color: var(--border-strong); color: var(--fg-0); }
.sv2-btn.warn  { border: 1px solid rgba(230, 185, 74, 0.40); background: var(--gold-soft); color: var(--gold); }
.sv2-btn.warn:hover:not(:disabled) { background: rgba(230, 185, 74, 0.18); }
.sv2-btn.primary { background: var(--gold); color: #1a1408; }
.sv2-btn.primary:hover:not(:disabled) { background: var(--gold-deep); }
.sv2-btn:disabled { opacity: 0.5; cursor: not-allowed; }

.sv2-flash {
  margin-top: 12px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  font-size: 12px;
  display: flex; align-items: center; gap: 8px;
}
.sv2-flash.ok  { background: rgba(111,191,124,0.10); border: 1px solid rgba(111,191,124,0.25); color: var(--good); }
.sv2-flash.err { background: rgba(217,107,107,0.10); border: 1px solid rgba(217,107,107,0.30); color: var(--bad); }
</style>
