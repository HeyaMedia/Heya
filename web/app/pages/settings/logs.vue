<template>
  <div>
    <div class="page-header">
      <div>
        <h2 class="page-title">Server Logs</h2>
        <p class="page-desc">Real-time log stream with filtering</p>
      </div>
      <div class="header-actions">
        <div class="log-filter-group">
          <button
            v-for="lvl in logLevels"
            :key="lvl"
            class="log-filter-btn"
            :class="[lvl, { active: logLevel === lvl }]"
            @click="logLevel = logLevel === lvl ? '' : lvl"
          >{{ lvl }}</button>
        </div>
      </div>
    </div>

    <div class="log-panel scroll" ref="logPanelRef">
      <div v-if="!filteredLogs.length" class="log-empty">
        <Icon name="info" :size="14" />
        {{ logLevel ? `No ${logLevel} logs` : 'No logs yet — waiting for events...' }}
      </div>
      <div
        v-for="(entry, i) in filteredLogs"
        :key="i"
        class="log-entry"
        :class="entry.level"
      >
        <span class="log-time">{{ formatLogTime(entry.time) }}</span>
        <span class="log-level" :class="entry.level">{{ entry.level }}</span>
        <span class="log-msg">{{ entry.message }}</span>
        <span v-if="hasFields(entry)" class="log-fields">
          <span v-for="(v, k) in entry.fields" :key="String(k)" class="log-field">
            {{ k }}=<span class="log-field-val">{{ v }}</span>
          </span>
        </span>
      </div>
    </div>

    <div class="log-footer">
      <span class="log-count">{{ filteredLogs.length }} entries</span>
      <span class="live-indicator" :class="{ connected: wsConnected }">
        <span class="live-pulse" />
        {{ wsConnected ? 'Live' : 'Disconnected' }}
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { LogPayload } from '~/composables/useEventBus'

interface LogEntry {
  time: string
  level: string
  message: string
  fields?: Record<string, any>
}

const logs = ref<LogEntry[]>([])
const logLevel = ref('')
const logPanelRef = ref<HTMLElement>()

const { on, connected: wsConnected } = useEventBus()

const logLevels = ['debug', 'info', 'warn', 'error']

const filteredLogs = computed(() => {
  if (!logLevel.value) return logs.value
  return logs.value.filter(e => e.level === logLevel.value)
})

function hasFields(entry: LogEntry) {
  return entry.fields && Object.keys(entry.fields).length > 0
}

function formatLogTime(t: string) {
  try {
    return new Date(t).toLocaleTimeString('en-GB', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return '' }
}

async function fetchLogs() {
  try {
    const entries = await apiFetch<LogEntry[]>('/api/logs?n=500')
    logs.value = entries.reverse()
  } catch {}
}

let unsub: (() => void) | null = null

onMounted(() => {
  fetchLogs()
  unsub = on('log', (event) => {
    const p = event.payload as LogPayload
    logs.value.unshift({ time: event.ts, level: p.level, message: p.message, fields: p.fields })
    if (logs.value.length > 2000) logs.value.length = 2000
  })
})

onUnmounted(() => unsub?.())
</script>

<style scoped>
.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 20px; }
.page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.page-desc { font-size: 13px; color: var(--fg-3); margin: 6px 0 0; }
.header-actions { display: flex; align-items: center; gap: 8px; }

/* Level filters */
.log-filter-group { display: flex; gap: 2px; }
.log-filter-btn {
  font-size: 10px; font-weight: 600; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em;
  padding: 6px 10px; border-radius: var(--r-xs);
  border: 1px solid var(--border); background: transparent;
  color: var(--fg-3); cursor: pointer; transition: all 0.12s ease;
}
.log-filter-btn:hover { border-color: var(--border-strong); color: var(--fg-1); }
.log-filter-btn.active.debug { color: var(--fg-2); border-color: var(--fg-3); background: rgba(255, 255, 255, 0.04); }
.log-filter-btn.active.info { color: rgb(140, 160, 255); border-color: rgba(140, 160, 255, 0.3); background: rgba(140, 160, 255, 0.08); }
.log-filter-btn.active.warn { color: var(--gold); border-color: rgba(230, 185, 74, 0.3); background: var(--gold-soft); }
.log-filter-btn.active.error { color: var(--bad); border-color: rgba(217, 107, 107, 0.3); background: rgba(217, 107, 107, 0.08); }

/* Log panel */
.log-panel {
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-md) var(--r-md) 0 0;
  padding: 4px 0;
  height: calc(100vh - 240px);
  min-height: 300px;
  overflow-y: auto;
  font-family: var(--font-mono);
  font-size: 11px;
  line-height: 1.7;
}

.log-empty {
  display: flex; align-items: center; gap: 8px;
  padding: 20px 14px; color: var(--fg-3); font-size: 12px;
  font-family: var(--font-sans);
}

.log-entry { display: flex; gap: 8px; padding: 1px 14px; align-items: baseline; }
.log-entry:hover { background: rgba(255, 255, 255, 0.02); }
.log-entry.error { background: rgba(217, 107, 107, 0.04); }
.log-entry.warn { background: rgba(230, 185, 74, 0.03); }

.log-time { color: var(--fg-4); flex-shrink: 0; width: 70px; }
.log-level {
  font-weight: 700; text-transform: uppercase; width: 42px; flex-shrink: 0;
  letter-spacing: 0.04em; font-size: 10px;
}
.log-level.debug { color: var(--fg-3); }
.log-level.info { color: rgb(140, 160, 255); }
.log-level.warn { color: var(--gold); }
.log-level.error { color: var(--bad); }

.log-msg { color: var(--fg-1); flex-shrink: 0; }
.log-fields { color: var(--fg-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.log-field { margin-left: 8px; }
.log-field-val { color: var(--fg-2); }

/* Footer */
.log-footer {
  display: flex; align-items: center; justify-content: space-between;
  padding: 8px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border); border-top: none;
  border-radius: 0 0 var(--r-md) var(--r-md);
  font-size: 11px; font-family: var(--font-mono); color: var(--fg-3);
}

.log-count { letter-spacing: 0.04em; }

.live-indicator {
  display: inline-flex; align-items: center; gap: 6px;
  font-weight: 600; text-transform: uppercase;
  font-size: 10px; letter-spacing: 0.06em;
  color: var(--fg-3);
}

.live-indicator.connected { color: var(--good); }

.live-pulse {
  width: 6px; height: 6px; border-radius: 50%;
  background: var(--fg-4);
}

.live-indicator.connected .live-pulse {
  background: var(--good);
  animation: pulse-ring 1.5s ease-in-out infinite;
}

@keyframes pulse-ring {
  0% { box-shadow: 0 0 0 0 rgba(111, 191, 124, 0.5); }
  70% { box-shadow: 0 0 0 6px rgba(111, 191, 124, 0); }
  100% { box-shadow: 0 0 0 0 rgba(111, 191, 124, 0); }
}
</style>
