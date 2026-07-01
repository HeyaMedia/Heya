<template>
  <!-- Dev-only in-app overview of the vue-query cache. Toggled from the
       dev-only button in AppTopBar (left of search) via shared useState, plus
       ⌘⇧Q. Reads the live $queryClient. Async-imported from app.vue behind
       import.meta.dev, so neither this nor its deps ship in the prod binary. -->
  <div v-if="open" class="qcp-panel">
    <header class="qcp-head">
      <div class="qcp-title"><Icon name="database" :size="13" /> Query Cache</div>
      <div class="qcp-summary">
        <span v-if="counts.fresh" class="qcp-chip fresh">{{ counts.fresh }} fresh</span>
        <span v-if="counts.stale" class="qcp-chip stale">{{ counts.stale }} stale</span>
        <span v-if="counts.fetching" class="qcp-chip fetching">{{ counts.fetching }} fetching</span>
        <span v-if="counts.error" class="qcp-chip error">{{ counts.error }} error</span>
        <span v-if="counts.inactive" class="qcp-chip inactive">{{ counts.inactive }} idle</span>
      </div>
      <button class="qcp-x" title="Close" @click="open = false"><Icon name="close" :size="13" /></button>
    </header>

    <div class="qcp-toolbar">
      <label class="qcp-search">
        <Icon name="filter" :size="12" />
        <input v-model="filter" placeholder="Filter keys…" spellcheck="false" />
      </label>
      <button class="qcp-tool" :title="`Sort by ${sortBy === 'updated' ? 'key' : 'most recent'}`" @click="sortBy = sortBy === 'updated' ? 'key' : 'updated'">
        <Icon name="sort" :size="12" /> {{ sortBy === 'updated' ? 'recent' : 'key' }}
      </button>
      <button class="qcp-tool" title="Invalidate all queries" @click="invalidateAll">
        <Icon name="refresh" :size="12" /> all
      </button>
      <button class="qcp-tool danger" title="Clear the whole cache" @click="clearAll">
        <Icon name="trash" :size="12" />
      </button>
    </div>

    <div class="qcp-list scroll">
      <div v-if="!rows.length" class="qcp-empty">
        {{ filter ? 'No queries match that filter.' : 'No queries in the cache yet.' }}
      </div>
      <div v-for="row in rows" :key="row.hash" class="qcp-row" :class="{ open: expanded === row.hash }">
        <div class="qcp-row-main" @click="toggleExpand(row.hash)">
          <span class="qcp-dot" :class="row.dotClass" />
          <span class="qcp-key" :title="row.keyLabel">{{ row.keyLabel }}</span>
          <span class="qcp-info">
            <Icon v-if="row.fetchStatus === 'fetching'" name="loading" :size="11" class="qcp-spin" />
            <span class="qcp-obs" :title="`${row.observers} observer(s)`">◉{{ row.observers }}</span>
            <span class="qcp-age" title="Last updated">{{ fmtAge(row.updatedAt) }}</span>
          </span>
          <span class="qcp-actions" @click.stop>
            <button title="Refetch" @click="refetch(row.query)"><Icon name="refresh" :size="12" /></button>
            <button title="Invalidate" @click="invalidate(row.query)"><Icon name="lightning" :size="12" /></button>
            <button class="danger" title="Remove" @click="remove(row.query)"><Icon name="trash" :size="12" /></button>
          </span>
        </div>
        <pre v-if="expanded === row.hash" class="qcp-data">{{ preview(row.query) }}</pre>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Query } from '@tanstack/vue-query'
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'

// $queryClient is provided by plugins/vue-query.client.ts and typed on NuxtApp.
// Read via useNuxtApp() (not a plugin param) so the augmented type resolves —
// same reason cache-invalidation.client.ts does.
const { $queryClient: queryClient } = useNuxtApp()
const cache = queryClient.getQueryCache()

// Shared with the dev toggle button in AppTopBar via the same useState key.
const open = useState('dev_query_panel', () => false)
const filter = ref('')
const sortBy = ref<'updated' | 'key'>('updated')
const expanded = ref<string | null>(null)

// Reactive triggers: `tick` bumps on cache events (coalesced to one frame) so
// the derived lists recompute; `nowTs` ticks once a second while open so the
// relative ages stay live without recomputing on every cache event.
const tick = ref(0)
const nowTs = ref(Date.now())

let raf = 0
let unsub: (() => void) | undefined
let clock: ReturnType<typeof setInterval> | undefined

onMounted(() => {
  unsub = cache.subscribe(() => {
    if (raf) return
    raf = requestAnimationFrame(() => {
      raf = 0
      tick.value++
    })
  })
  clock = setInterval(() => {
    if (open.value) nowTs.value = Date.now()
  }, 1000)
  window.addEventListener('keydown', onKey)
})

onBeforeUnmount(() => {
  unsub?.()
  if (raf) cancelAnimationFrame(raf)
  if (clock) clearInterval(clock)
  window.removeEventListener('keydown', onKey)
})

function onKey(e: KeyboardEvent) {
  // ⌘⇧Q / Ctrl+Shift+Q toggles the panel. Modifier combo → safe while typing.
  if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.code === 'KeyQ') {
    e.preventDefault()
    open.value = !open.value
  }
}

interface Row {
  hash: string
  query: Query
  keyLabel: string
  status: 'pending' | 'success' | 'error'
  fetchStatus: 'fetching' | 'paused' | 'idle'
  isStale: boolean
  isActive: boolean
  observers: number
  updatedAt: number
  dotClass: string
}

const allRows = computed<Row[]>(() => {
  void tick.value // subscribe this computed to cache events
  return cache.getAll().map((q) => {
    const status = q.state.status
    const fetchStatus = q.state.fetchStatus
    const isStale = q.isStale()
    const isActive = q.isActive()
    const dotClass =
      status === 'error' ? 'd-error'
      : fetchStatus === 'fetching' ? 'd-fetching'
      : !isActive ? 'd-inactive'
      : isStale ? 'd-stale'
      : 'd-fresh'
    return {
      hash: q.queryHash,
      query: q,
      keyLabel: safeKey(q.queryKey),
      status,
      fetchStatus,
      isStale,
      isActive,
      observers: q.getObserversCount(),
      updatedAt: q.state.dataUpdatedAt,
      dotClass,
    }
  })
})

const rows = computed<Row[]>(() => {
  const f = filter.value.trim().toLowerCase()
  const list = f ? allRows.value.filter((r) => r.keyLabel.toLowerCase().includes(f)) : allRows.value
  return [...list].sort((a, b) =>
    sortBy.value === 'key' ? a.keyLabel.localeCompare(b.keyLabel) : b.updatedAt - a.updatedAt,
  )
})

const counts = computed(() => {
  const c = { fresh: 0, stale: 0, fetching: 0, error: 0, inactive: 0 }
  for (const r of allRows.value) {
    if (r.status === 'error') c.error++
    if (r.fetchStatus === 'fetching') c.fetching++
    if (!r.isActive) c.inactive++
    else if (r.isStale) c.stale++
    else c.fresh++
  }
  return c
})

function safeKey(key: unknown): string {
  try {
    return JSON.stringify(key)
  } catch {
    return String(key)
  }
}

function fmtAge(ts: number): string {
  if (!ts) return '—'
  const s = Math.max(0, Math.round((nowTs.value - ts) / 1000))
  if (s < 1) return 'now'
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h`
  return `${Math.floor(h / 24)}d`
}

function preview(q: Query): string {
  const parts: string[] = []
  if (q.state.error) parts.push('// error\n' + safeJson(q.state.error))
  parts.push(safeJson(q.state.data))
  return parts.join('\n\n')
}

function safeJson(v: unknown): string {
  if (v instanceof Error) return `${v.name}: ${v.message}`
  if (v === undefined) return 'undefined'
  try {
    const s = JSON.stringify(v, jsonReplacer(), 2)
    if (s == null) return String(v)
    return s.length > 6000 ? s.slice(0, 6000) + '\n… (truncated)' : s
  } catch (e) {
    return `// unserialisable: ${(e as Error).message}`
  }
}

function jsonReplacer() {
  const seen = new WeakSet<object>()
  return (_k: string, val: unknown) => {
    if (typeof val === 'object' && val !== null) {
      if (seen.has(val)) return '[Circular]'
      seen.add(val)
    }
    return val
  }
}

function toggleExpand(hash: string) {
  expanded.value = expanded.value === hash ? null : hash
}

// Per-query actions use exact:true so they don't fan out to sibling/child keys.
function invalidate(q: Query) {
  queryClient.invalidateQueries({ queryKey: q.queryKey, exact: true })
}
function refetch(q: Query) {
  queryClient.refetchQueries({ queryKey: q.queryKey, exact: true })
}
function remove(q: Query) {
  queryClient.removeQueries({ queryKey: q.queryKey, exact: true })
  tick.value++
}
function invalidateAll() {
  queryClient.invalidateQueries()
}
function clearAll() {
  queryClient.clear()
  tick.value++
}
</script>

<style scoped>
/* Docks below the navbar on the right, under its toggle (AppTopBar, left of
   search). Above everything (navbar is z-index 50). */
.qcp-panel {
  position: fixed;
  z-index: 99998;
  top: calc(var(--topbar-h) + 6px);
  right: 12px;
  width: min(780px, calc(100vw - 24px));
  height: min(64vh, 580px);
  display: flex;
  flex-direction: column;
  background: var(--bg-2);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-3);
  overflow: hidden;
  font-family: var(--font-sans);
}

.qcp-head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-3);
}
.qcp-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  font-weight: 600;
  color: var(--fg-0);
  letter-spacing: 0.02em;
}
.qcp-summary {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  flex: 1;
}
.qcp-chip {
  font-family: var(--font-mono);
  font-size: 10px;
  padding: 2px 6px;
  border-radius: 999px;
  border: 1px solid var(--border);
  color: var(--fg-2);
}
.qcp-chip.fresh { color: #6fd08c; border-color: rgba(111, 208, 140, 0.35); }
.qcp-chip.stale { color: var(--gold-bright); border-color: var(--gold-soft); }
.qcp-chip.fetching { color: #7cc0ff; border-color: rgba(124, 192, 255, 0.35); }
.qcp-chip.error { color: #ff7b72; border-color: rgba(255, 123, 114, 0.4); }
.qcp-chip.inactive { color: var(--fg-3); }
.qcp-x {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px; height: 24px;
  border-radius: var(--r-sm);
  border: 0;
  background: transparent;
  color: var(--fg-2);
  cursor: pointer;
}
.qcp-x:hover { color: var(--fg-0); background: var(--bg-4); }

.qcp-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
}
.qcp-search {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  height: 28px;
  padding: 0 10px;
  border-radius: var(--r-sm);
  background: var(--bg-1);
  border: 1px solid var(--border);
  color: var(--fg-3);
}
.qcp-search input {
  flex: 1;
  background: transparent;
  border: 0;
  outline: none;
  color: var(--fg-0);
  font-size: 12px;
  font-family: var(--font-mono);
}
.qcp-tool {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 9px;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: 11px;
  cursor: pointer;
  transition: color 0.12s, border-color 0.12s;
}
.qcp-tool:hover { color: var(--fg-0); border-color: var(--border-strong); }
.qcp-tool.danger:hover { color: #ff7b72; border-color: rgba(255, 123, 114, 0.4); }

.qcp-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 6px;
}
.qcp-empty {
  padding: 28px 12px;
  text-align: center;
  color: var(--fg-3);
  font-size: 12px;
}

.qcp-row { border-radius: var(--r-sm); }
.qcp-row.open { background: var(--bg-1); }
.qcp-row-main {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: var(--r-sm);
  cursor: pointer;
}
.qcp-row-main:hover { background: var(--bg-3); }

.qcp-dot {
  flex: 0 0 auto;
  width: 7px; height: 7px;
  border-radius: 50%;
  background: var(--fg-3);
}
.qcp-dot.d-fresh { background: #6fd08c; }
.qcp-dot.d-stale { background: var(--gold); }
.qcp-dot.d-fetching { background: #7cc0ff; animation: qcp-pulse 1s ease-in-out infinite; }
.qcp-dot.d-error { background: #ff7b72; }
.qcp-dot.d-inactive { background: var(--fg-4); }

.qcp-key {
  flex: 1;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--fg-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.qcp-info {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--fg-3);
}
.qcp-obs { color: var(--fg-3); }
.qcp-age { color: var(--fg-2); min-width: 30px; text-align: right; }

.qcp-actions {
  display: inline-flex;
  gap: 2px;
  opacity: 0;
  transition: opacity 0.12s;
}
.qcp-row-main:hover .qcp-actions { opacity: 1; }
.qcp-actions button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px; height: 24px;
  border-radius: var(--r-sm);
  border: 0;
  background: transparent;
  color: var(--fg-2);
  cursor: pointer;
}
.qcp-actions button:hover { color: var(--gold-bright); background: var(--bg-4); }
.qcp-actions button.danger:hover { color: #ff7b72; }

.qcp-data {
  margin: 0;
  padding: 8px 10px 10px 24px;
  font-family: var(--font-mono);
  font-size: 11px;
  line-height: 1.5;
  color: var(--fg-2);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 240px;
  overflow: auto;
}

.qcp-spin { animation: qcp-spin 0.8s linear infinite; color: #7cc0ff; }
@keyframes qcp-spin { to { transform: rotate(360deg); } }
@keyframes qcp-pulse { 50% { opacity: 0.35; } }
</style>
