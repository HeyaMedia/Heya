<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import { adminDatabaseQuery } from '~/queries/admin'

const databaseData = useQuery(adminDatabaseQuery())
const db = computed(() => databaseData.data.value ?? null)
const loading = computed(() => databaseData.isLoading.value)
const tick = ref(0)
let timer: ReturnType<typeof setInterval> | null = null
let tickTimer: ReturnType<typeof setInterval> | null = null

async function load() {
  try { await databaseData.refetch() } catch {}
}

const totalDbSize = computed(() => {
  // tick is read to force re-evaluation when the timer fires.
  void tick.value
  if (!db.value?.top_tables) return 1
  return db.value.top_tables.reduce((s, t) => s + t.size_bytes, 1)
})

function tablePct(size: number): number {
  const max = db.value?.top_tables?.[0]?.size_bytes ?? 1
  return Math.round((size / Math.max(max, 1)) * 100)
}

const poolUsedPct = computed(() => {
  if (!db.value || db.value.max_connections === 0) return 0
  return Math.round((db.value.total_connections / db.value.max_connections) * 100)
})
const poolTone = computed<'good' | 'warn' | 'bad'>(() => {
  if (poolUsedPct.value >= 90) return 'bad'
  if (poolUsedPct.value >= 70) return 'warn'
  return 'good'
})

onMounted(() => {
  load()
  timer = setInterval(load, 5000)
  tickTimer = setInterval(() => { tick.value++ }, 1000)
})
onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
  if (tickTimer) clearInterval(tickTimer)
})
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Database</h2>
      <p class="sv2-page-desc">
        Postgres state — version, on-disk size, the largest tables, and the
        pgxpool's connection accounting. Polls every 5 seconds.
      </p>
    </header>

    <div v-if="loading && !db" class="loading-state">
      <Icon name="spinner" :size="16" /> Probing database…
    </div>

    <template v-else-if="db">
      <div v-if="db.error" class="empty-state err">
        <Icon name="warning" :size="14" /> {{ db.error }}
      </div>

      <div class="tiles">
        <MetricTile label="Database size" :value="fmtBytes(db.size_bytes)" icon="hard-drives" />
        <MetricTile label="Postgres" :value="db.version || '—'" icon="database" />
        <MetricTile label="Connections"
          :value="`${db.total_connections} / ${db.max_connections}`"
          icon="cpu"
          :tone="poolTone"
          :sub="`${poolUsedPct}% of cap`" />
        <MetricTile label="Idle"
          :value="db.idle_connections"
          icon="pulse"
          :sub="`${db.acquired_connections} in use`" />
      </div>

      <SettingsSection title="Pool stats" icon="cpu"
        description="In-memory counters from the pgxpool. Acquire count climbs forever; canceled / empty acquires should stay near zero — non-trivial growth means the pool is under-sized.">
        <KVTable :rows="[
          { key: 'Database',           value: db.database_name, mono: true, copy: true },
          { key: 'Version',            value: db.version, mono: true },
          { key: 'On-disk size',       value: `${fmtBytes(db.size_bytes)} (${db.size_bytes} bytes)` },
          { key: 'Total connections',  value: `${db.total_connections} of ${db.max_connections}` },
          { key: 'Acquired',           value: db.acquired_connections },
          { key: 'Idle',               value: db.idle_connections },
          { key: 'Total acquires',     value: db.acquire_count.toLocaleString() },
          { key: 'Cumulative wait',    value: `${db.acquire_duration_ms.toLocaleString()} ms` },
          { key: 'Canceled acquires',  value: db.canceled_acquire_count.toLocaleString() },
          { key: 'Empty acquires',     value: db.empty_acquire_count.toLocaleString() },
        ]" />

        <div class="pool-bar">
          <div class="pool-segment used" :style="{ width: (db.acquired_connections / db.max_connections * 100) + '%' }" />
          <div class="pool-segment idle" :style="{ width: (db.idle_connections / db.max_connections * 100) + '%' }" />
        </div>
        <div class="pool-legend">
          <span class="legend used"><span class="dot" />acquired</span>
          <span class="legend idle"><span class="dot" />idle</span>
          <span class="legend free"><span class="dot" />headroom</span>
        </div>
      </SettingsSection>

      <SettingsSection title="Largest tables" icon="database"
        description="Top 10 user tables by pg_total_relation_size (heap + indexes + toast). Catalog tables are excluded.">
        <div v-if="!db.top_tables || db.top_tables.length === 0" class="empty-state">
          <Icon name="info" :size="14" /> No table stats yet.
        </div>
        <div v-else class="tbl-list">
          <div v-for="t in db.top_tables" :key="t.name" class="tbl-row">
            <span class="tbl-name mono">{{ t.name }}</span>
            <div class="tbl-bar"><div class="tbl-fill" :style="{ width: tablePct(t.size_bytes) + '%' }" /></div>
            <span class="tbl-size mono">{{ fmtBytes(t.size_bytes) }}</span>
          </div>
        </div>
      </SettingsSection>
    </template>
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
.empty-state.err { color: var(--bad); background: color-mix(in srgb, var(--bad) 6%, transparent); border-color: color-mix(in srgb, var(--bad) 25%, transparent); margin-bottom: 16px; }

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 8px;
  margin-bottom: 28px;
}

.pool-bar {
  margin-top: 14px;
  height: 8px;
  border-radius: 4px;
  background: var(--bg-0);
  overflow: hidden;
  display: flex;
}
.pool-segment { height: 100%; transition: width 0.4s ease; }
.pool-segment.used { background: var(--gold); }
.pool-segment.idle { background: color-mix(in srgb, var(--good) 60%, transparent); }
.pool-legend {
  display: flex; gap: 14px;
  margin-top: 6px;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
}
.legend { display: inline-flex; align-items: center; gap: 5px; }
.legend .dot { width: 8px; height: 8px; border-radius: 50%; }
.legend.used .dot { background: var(--gold); }
.legend.idle .dot { background: color-mix(in srgb, var(--good) 60%, transparent); }
.legend.free .dot { background: var(--bg-0); border: 1px solid var(--border); }

.tbl-list { display: flex; flex-direction: column; gap: 4px; }
.tbl-row {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(0, 2fr) 90px;
  align-items: center;
  gap: 14px;
  padding: 8px 12px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  font-size: 12px;
}
.tbl-name { color: var(--fg-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tbl-bar { height: 6px; background: var(--bg-0); border-radius: 3px; overflow: hidden; }
.tbl-fill { height: 100%; background: var(--gold); transition: width 0.4s ease; }
.tbl-size { color: var(--fg-2); font-size: 11.5px; text-align: right; }

.mono { font-family: var(--font-mono); }

/* Phone: name + bar + size can't share one row at 390px — put the bar on
   its own line under the name/size header line. */
@media (max-width: 720px) {
  .tbl-row {
    grid-template-columns: 1fr auto;
    grid-template-areas: "name size" "bar bar";
    row-gap: 6px;
  }
  .tbl-name { grid-area: name; }
  .tbl-size { grid-area: size; }
  .tbl-bar { grid-area: bar; }

  /* minmax(180px) only fits 1 column at 390px — force 2. */
  .tiles { grid-template-columns: repeat(2, 1fr); }
}
</style>
