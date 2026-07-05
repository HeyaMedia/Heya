<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import type { components } from '#open-fetch-schemas/heya'
type Sys      = components['schemas']['AdminSystemBody']
type LogLevel = components['schemas']['AdminLogLevelBody']

const { $heya } = useNuxtApp()

const sys = ref<Sys | null>(null)
const logLevel = ref<LogLevel | null>(null)
const loading = ref(true)
const setting = ref(false)
const bundling = ref(false)
const { flash } = useFlash()

// History of last ~60 samples for sparklines. We pull /api/admin/system every
// 2s; a 60-sample window = the last ~2 minutes of runtime activity.
const heapHistory = ref<number[]>([])
const goroutineHistory = ref<number[]>([])
const HISTORY = 60

const tick = ref(0)
let sysTimer: ReturnType<typeof setInterval> | null = null
let tickTimer: ReturnType<typeof setInterval> | null = null

async function loadSystem() {
  try {
    const s = await $heya('/api/admin/system')
    sys.value = s
    heapHistory.value.push(s.heap_inuse_bytes)
    if (heapHistory.value.length > HISTORY) heapHistory.value.shift()
    goroutineHistory.value.push(s.goroutines)
    if (goroutineHistory.value.length > HISTORY) goroutineHistory.value.shift()
  } catch {}
}

async function loadLogLevel() {
  try {
    logLevel.value = await $heya('/api/admin/log-level')
  } catch {}
}

async function setLogLevel(level: string) {
  setting.value = true
  try {
    logLevel.value = await $heya('/api/admin/log-level', {
      method: 'PUT',
      body: { level } as any,
    })
    flash.value = { kind: 'ok', text: `Log level set to ${level}.` }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to set level.' }
  } finally {
    setting.value = false
  }
}

function fmtNs(ns?: number) {
  if (ns == null || ns === 0) return '—'
  if (ns < 1_000) return `${ns} ns`
  if (ns < 1_000_000) return `${(ns / 1000).toFixed(1)} µs`
  return `${(ns / 1_000_000).toFixed(2)} ms`
}
function fmtUptime(sec?: number) {
  // Read tick to keep this re-evaluating on the per-second tick.
  void tick.value
  if (sec == null) return '—'
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  const parts = []
  if (d > 0) parts.push(`${d}d`)
  if (d > 0 || h > 0) parts.push(`${h}h`)
  if (d > 0 || h > 0 || m > 0) parts.push(`${m}m`)
  parts.push(`${s}s`)
  return parts.join(' ')
}
function fmtNumber(n?: number) {
  if (n == null) return '—'
  return n.toLocaleString()
}

// Full read-only diagnostic bundle — app/config/db/libraries/tools/queue/
// storage/logs, secrets redacted server-side (see internal/service/doctor.go)
// so this is safe to paste into a bug report or Discord message. Same
// blob-download pattern as the logs page's Export button.
async function downloadSupportBundle() {
  bundling.value = true
  try {
    const report = await $heya('/api/admin/doctor')
    const blob = new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `heya-doctor-${new Date().toISOString().slice(0, 10)}.json`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    flash.value = { kind: 'ok', text: 'Support bundle downloaded.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to build support bundle.' }
  } finally {
    bundling.value = false
  }
}

// pprof endpoints — admin-only binary profiles. We open them in a new tab
// rather than fetching ourselves; the browser handles the download.
const PPROF = [
  { name: 'CPU profile (30s)', endpoint: '/api/debug/pprof/profile', icon: 'cpu' },
  { name: 'Heap snapshot',      endpoint: '/api/debug/pprof/heap',    icon: 'hard-drives' },
  { name: 'Goroutines',         endpoint: '/api/debug/pprof/goroutine', icon: 'pulse' },
  { name: 'Allocations',        endpoint: '/api/debug/pprof/allocs',  icon: 'layers' },
  { name: 'Mutex contention',   endpoint: '/api/debug/pprof/mutex',   icon: 'lock' },
  { name: 'Block profile',      endpoint: '/api/debug/pprof/block',   icon: 'warning' },
]

onMounted(async () => {
  await Promise.all([loadSystem(), loadLogLevel()])
  loading.value = false
  sysTimer = setInterval(loadSystem, 2000)
  tickTimer = setInterval(() => { tick.value++ }, 1000)
})
onBeforeUnmount(() => {
  if (sysTimer) clearInterval(sysTimer)
  if (tickTimer) clearInterval(tickTimer)
})
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Diagnostics</h2>
      <p class="sv2-page-desc">
        Process runtime — uptime, memory, goroutines, GC. Adjust the log
        level on the fly, or grab a pprof profile.
      </p>
    </header>

    <SettingsSection title="Support bundle" icon="clipboard"
      description="Everything a maintainer needs in one file — app version, config (with provenance, secrets redacted), database health, library path checks, ffmpeg/ffprobe, queue counts, and storage. Read-only; safe to paste into a bug report.">
      <button class="sv2-btn primary" :disabled="bundling" @click="downloadSupportBundle">
        <Icon :name="bundling ? 'spinner' : 'download'" :size="12" />
        {{ bundling ? 'Building…' : 'Download support bundle' }}
      </button>
    </SettingsSection>

    <div v-if="loading && !sys" class="loading-state">
      <Icon name="spinner" :size="16" /> Probing runtime…
    </div>

    <template v-else-if="sys">
      <div class="tiles">
        <MetricTile label="Uptime" :value="fmtUptime(sys.uptime_seconds)" icon="timer" />
        <MetricTile label="Goroutines" :value="sys.goroutines" icon="pulse"
          :sparkline="goroutineHistory" />
        <MetricTile label="Heap in use" :value="fmtBytes(sys.heap_inuse_bytes)" icon="hard-drives"
          :sparkline="heapHistory" />
        <MetricTile label="GC pause (last)" :value="fmtNs(sys.gc_pause_last_ns)" icon="lightning"
          :sub="`${sys.num_gc} cycles total`" />
        <MetricTile label="WS subscribers" :value="sys.ws_subscribers" icon="eye"
          :tone="sys.ws_subscribers > 0 ? 'good' : 'neutral'" />
        <MetricTile label="CPU" :value="`${sys.gomaxprocs} / ${sys.num_cpu}`" icon="cpu"
          sub="GOMAXPROCS / available" />
      </div>

      <SettingsSection title="Runtime" icon="cpu">
        <KVTable :rows="[
          { key: 'Hostname',     value: sys.hostname, mono: true, copy: true },
          { key: 'PID',          value: sys.pid, mono: true },
          { key: 'Started',      value: sys.started_at, mono: true },
          { key: 'Uptime',       value: fmtUptime(sys.uptime_seconds), mono: true },
          { key: 'Go version',   value: sys.go_version, mono: true },
          { key: 'OS / arch',    value: `${sys.goos} / ${sys.goarch}`, mono: true },
          { key: 'NumCPU',       value: sys.num_cpu },
          { key: 'GOMAXPROCS',   value: sys.gomaxprocs },
          { key: 'Goroutines',   value: fmtNumber(sys.goroutines) },
          { key: 'cgo calls',    value: fmtNumber(sys.num_cgo_call) },
          { key: 'WS subscribers', value: sys.ws_subscribers },
        ]" />
      </SettingsSection>

      <SettingsSection title="Memory + GC" icon="hard-drives"
        description="Snapshots from runtime.ReadMemStats(). 'In use' is what's actively allocated; 'sys' is what's been requested from the OS (steady-state higher).">
        <KVTable :rows="[
          { key: 'Heap in use',    value: `${fmtBytes(sys.heap_inuse_bytes)} (${sys.heap_inuse_bytes.toLocaleString()} bytes)` },
          { key: 'Heap allocated', value: fmtBytes(sys.heap_alloc_bytes) },
          { key: 'OS sys bytes',   value: fmtBytes(sys.sys_bytes) },
          { key: 'Stack in use',   value: fmtBytes(sys.stack_bytes) },
          { key: 'GC cycles',      value: fmtNumber(sys.num_gc) },
          { key: 'Last GC pause',  value: fmtNs(sys.gc_pause_last_ns) },
        ]" />
      </SettingsSection>

      <SettingsSection v-if="sys.build && Object.keys(sys.build).length" title="Build" icon="info">
        <KVTable :rows="Object.entries(sys.build).map(([k, v]) => ({ key: k, value: String(v), mono: true, copy: true }))" />
      </SettingsSection>

      <SettingsSection title="Log level" icon="clipboard"
        :description="`Active global zerolog level. Boot value (HEYA_LOG_LEVEL) was ${logLevel?.boot_level ?? '—'}; this picker is in-memory only and resets on restart.`">
        <div v-if="!logLevel" class="loading-state"><Icon name="spinner" :size="14" /> Loading…</div>
        <div v-else class="level-row">
          <div class="level-buttons">
            <button
              v-for="lvl in logLevel.available"
              :key="lvl"
              class="level-btn"
              :class="{ active: logLevel.level === lvl, disabled: setting }"
              :disabled="setting || logLevel.level === lvl"
              @click="setLogLevel(lvl)"
            >{{ lvl }}</button>
          </div>
          <div class="level-hint">
            Current: <code>{{ logLevel.level }}</code>
            <span v-if="logLevel.level !== logLevel.boot_level"> · diverges from boot value</span>
          </div>
        </div>
      </SettingsSection>

      <SettingsSection title="Profiles" icon="lightning"
        description="Live pprof endpoints — opens in a new tab so the browser handles the download. CPU profile blocks for 30 seconds while it samples.">
        <div class="prof-grid">
          <a v-for="p in PPROF" :key="p.endpoint" :href="p.endpoint" target="_blank" rel="noopener" class="prof-card">
            <div class="prof-icon"><Icon :name="p.icon" :size="16" /></div>
            <div class="prof-info">
              <div class="prof-name">{{ p.name }}</div>
              <div class="prof-path mono">{{ p.endpoint }}</div>
            </div>
            <Icon name="chevright" :size="14" class="prof-chev" />
          </a>
        </div>
      </SettingsSection>
    </template>

    <SettingsFlash :flash="flash" />
  </div>
</template>

<style scoped>
.loading-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 8px;
  margin-bottom: 28px;
}

.level-row { display: flex; flex-direction: column; gap: 10px; }
.level-buttons { display: flex; flex-wrap: wrap; gap: 4px; }
.level-btn {
  padding: 6px 14px;
  border-radius: var(--r-sm);
  background: var(--bg-2);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: 11.5px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  cursor: pointer;
  transition: border-color 0.12s, color 0.12s, background 0.12s;
}
.level-btn:hover:not(:disabled) { border-color: var(--border-strong); color: var(--fg-0); }
.level-btn.active {
  border-color: var(--gold);
  background: var(--gold-soft);
  color: var(--gold);
}
.level-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.level-hint {
  font-size: 12px;
  color: var(--fg-3);
}
.level-hint code {
  font-family: var(--font-mono);
  color: var(--fg-1);
  padding: 1px 6px;
  background: var(--bg-2);
  border-radius: var(--r-xs);
}

.prof-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 8px;
}
.prof-card {
  display: flex; align-items: center; gap: 12px;
  padding: 12px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  text-decoration: none;
  color: inherit;
  transition: border-color 0.12s, background 0.12s;
}
.prof-card:hover {
  border-color: var(--gold);
  background: var(--gold-soft);
}
.prof-icon {
  width: 32px; height: 32px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  color: var(--gold);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.prof-info { flex: 1; min-width: 0; }
.prof-name { font-size: 13px; font-weight: 500; color: var(--fg-0); }
.prof-path { font-size: 11px; color: var(--fg-3); margin-top: 2px; }
.prof-chev { color: var(--fg-3); }

.mono { font-family: var(--font-mono); }

/* Phone: minmax(180px) only fits 1 column at 390px (358px content width) —
   force 2 so the tile row actually reflows per the responsive plan instead
   of stacking to full-width singles. */
@media (max-width: 720px) {
  .tiles { grid-template-columns: repeat(2, 1fr); }
}
</style>
