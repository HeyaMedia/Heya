<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

const { $heya } = useNuxtApp()

type SourceEntry = { source: string, env_var?: string }
type SourcesMap = Record<string, SourceEntry>

const sources = ref<SourcesMap>({})
const loading = ref(true)
const filterText = ref('')
const filterSource = ref<'' | 'env' | 'db' | 'default'>('')

async function load() {
  loading.value = true
  try {
    sources.value = await $heya('/api/config/sources') as SourcesMap
  } catch {} finally {
    loading.value = false
  }
}

const grouped = computed(() => {
  const groups: Record<string, { key: string, entry: SourceEntry }[]> = {}
  for (const [key, entry] of Object.entries(sources.value)) {
    if (filterSource.value && entry.source !== filterSource.value) continue
    const needle = filterText.value.trim().toLowerCase()
    if (needle) {
      const hay = (key + ' ' + (entry.env_var ?? '')).toLowerCase()
      if (!hay.includes(needle)) continue
    }
    const dot = key.indexOf('.')
    const group = dot > 0 ? key.slice(0, dot) : 'misc'
    if (!groups[group]) groups[group] = []
    groups[group].push({ key, entry })
  }
  for (const g of Object.values(groups)) g.sort((a, b) => a.key.localeCompare(b.key))
  return Object.entries(groups).sort(([a], [b]) => a.localeCompare(b))
})

const counts = computed(() => {
  let env = 0, db = 0, def = 0
  for (const e of Object.values(sources.value)) {
    if (e.source === 'env') env++
    else if (e.source === 'db') db++
    else def++
  }
  return { env, db, def, total: Object.keys(sources.value).length }
})

function groupIcon(group: string): string {
  switch (group) {
    case 'infra':       return 'cpu'
    case 'transcoder':  return 'film'
    case 'tailscale':   return 'network'
    case 'sonic_analysis': return 'eq'
    case 'library':     return 'folder'
    default:            return 'settings'
  }
}

function badgeState(source: string): 'ok' | 'warn' | 'idle' {
  if (source === 'env') return 'warn'
  if (source === 'db') return 'ok'
  return 'idle'
}

async function copyKey(key: string) {
  try { await navigator.clipboard.writeText(key) } catch {}
}

onMounted(load)
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Configuration</h2>
      <p class="sv2-page-desc">
        Every operational knob and where its current value came from.
        <strong>env</strong> beats the UI; <strong>db</strong> is what the
        Settings UI wrote; <strong>default</strong> is the built-in fallback.
        Set the matching env var to lock a knob.
      </p>
    </header>

    <div class="tiles">
      <MetricTile label="Tracked knobs" :value="counts.total" icon="settings" />
      <MetricTile label="From env" :value="counts.env" icon="key" tone="warn" sub="locks the UI input" />
      <MetricTile label="From DB" :value="counts.db" icon="database" tone="good" sub="UI-editable" />
      <MetricTile label="Default" :value="counts.def" icon="info" sub="not touched" />
    </div>

    <SettingsSection title="Provenance browser" icon="settings"
      description="Search by key or env var. Filter by source to find unset defaults or live env locks.">
      <template #actions>
        <input
          v-model="filterText"
          class="filter-input"
          placeholder="filter by key or env var…"
          spellcheck="false"
        />
        <select v-model="filterSource" class="filter-select">
          <option value="">All sources</option>
          <option value="env">env</option>
          <option value="db">db</option>
          <option value="default">default</option>
        </select>
      </template>

      <div v-if="loading" class="loading-state"><Icon name="spinner" :size="14" /> Loading…</div>

      <div v-else-if="grouped.length === 0" class="empty-state">
        <Icon name="info" :size="14" />
        {{ Object.keys(sources).length === 0 ? 'No config sources reported.' : 'Nothing matches the filter.' }}
      </div>

      <div v-else class="cfg-groups">
        <div v-for="[group, entries] in grouped" :key="group" class="cfg-group">
          <div class="cfg-group-head">
            <Icon :name="groupIcon(group)" :size="13" class="cfg-group-icon" />
            <span class="cfg-group-name">{{ group }}</span>
            <span class="cfg-group-count mono">{{ entries.length }}</span>
          </div>
          <div class="cfg-table">
            <div v-for="row in entries" :key="row.key" class="cfg-row">
              <button class="cfg-key mono" :title="'Copy ' + row.key" @click="copyKey(row.key)">
                {{ row.key }}
                <Icon name="clipboard" :size="11" class="cfg-key-copy" />
              </button>
              <StatusBadge :state="badgeState(row.entry.source)">{{ row.entry.source }}</StatusBadge>
              <span class="cfg-env mono">{{ row.entry.env_var || '—' }}</span>
            </div>
          </div>
        </div>
      </div>
    </SettingsSection>
  </div>
</template>

<style scoped>
.sv2-page-desc strong { color: var(--gold); font-weight: 600; }

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 8px;
  margin-bottom: 28px;
}

.filter-input {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 12px;
  font-family: var(--font-mono);
  padding: 6px 10px;
  width: 240px;
  outline: none;
}
.filter-input:focus { border-color: var(--gold); }
.filter-select {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-1);
  font-size: 12px;
  padding: 6px 10px;
  cursor: pointer;
  outline: none;
}
.filter-select:focus { border-color: var(--gold); }

.loading-state {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.cfg-groups { display: flex; flex-direction: column; gap: 18px; }
.cfg-group {
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
}
.cfg-group-head {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 14px;
  background: var(--bg-1);
  border-bottom: 1px solid var(--border);
}
.cfg-group-icon { color: var(--fg-3); }
.cfg-group-name {
  font-family: var(--font-mono);
  font-size: 10.5px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.08em;
  color: var(--fg-1);
}
.cfg-group-count {
  margin-left: auto;
  color: var(--fg-3);
  font-size: 11px;
}

.cfg-table { display: flex; flex-direction: column; }
.cfg-row {
  display: grid;
  grid-template-columns: minmax(0, 2fr) 80px minmax(0, 1.5fr);
  align-items: center;
  gap: 12px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--border);
  font-size: 12px;
}
.cfg-row:last-child { border-bottom: 0; }
.cfg-row:hover { background: rgb(var(--ink) / 0.02); }
.cfg-row:hover .cfg-key-copy { opacity: 1; }

.cfg-key {
  display: inline-flex; align-items: center; gap: 6px;
  color: var(--fg-1);
  text-align: left;
  font-size: 11.5px;
  cursor: pointer;
  background: transparent;
  border: 0;
  padding: 0;
}
.cfg-key:hover { color: var(--gold); }
.cfg-key-copy { color: var(--fg-4); opacity: 0; transition: opacity 0.12s; }
.cfg-env { color: var(--fg-3); font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.mono { font-family: var(--font-mono); }

/* Phone: the "2fr 80px 1.5fr" key/source/env grid is unreadably cramped at
   390px — key gets its own line, source badge + env value share the next. */
@media (max-width: 720px) {
  .filter-input { width: 100%; }

  .cfg-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 10px;
    padding: 10px 14px;
  }
  .cfg-key { flex: 1 1 100%; white-space: normal; word-break: break-word; }
  .cfg-env { flex: 1 1 auto; min-width: 0; }

  /* minmax(180px) only fits 1 column at 390px — force 2. */
  .tiles { grid-template-columns: repeat(2, 1fr); }
}
</style>
