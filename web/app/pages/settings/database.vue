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
  return Math.round((db.value.acquired_connections / db.value.max_connections) * 100)
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
    <SettingsContextHero
      title="Database"
      icon="database"
      eyebrow="Advanced · PostgreSQL"
      description="Follow database size, large tables, PostgreSQL version, and pgxpool connection pressure with a lightweight live refresh."
    />

    <div v-if="loading && !db" class="loading-state">
      <Icon name="spinner" :size="16" /> Probing database…
    </div>

    <template v-else-if="db">
      <div v-if="db.error" class="empty-state err">
        <Icon name="warning" :size="14" /> {{ db.error }}
      </div>

      <div class="tiles">
        <MetricTile label="Database size" :value="fmtBytes(db.size_bytes)" icon="hard-drives" />
        <MetricTile label="Connections"
          :value="`${db.acquired_connections} / ${db.max_connections}`"
          icon="cpu"
          :tone="poolTone"
          :sub="`${db.total_connections} open · ${poolUsedPct}% active`" />
        <MetricTile label="Idle"
          :value="db.idle_connections"
          icon="pulse"
          :sub="`${db.acquired_connections} in use`" />
        <MetricTile label="Buffer cache hit"
          :value="`${db.buffer_cache_hit_ratio.toFixed(1)}%`"
          icon="lightning"
          :tone="db.buffer_cache_hit_ratio >= 95 ? 'good' : 'warn'"
          :sub="`${db.blocks_hit.toLocaleString()} hits · ${db.blocks_read.toLocaleString()} reads`" />
        <MetricTile label="Active / waiting"
          :value="`${db.active_queries} / ${db.waiting_queries}`"
          icon="timer"
          :tone="db.waiting_queries > 0 ? 'warn' : 'good'"
          :sub="`longest ${db.longest_query_ms < 10 ? db.longest_query_ms.toFixed(2) : db.longest_query_ms.toFixed(0)} ms`" />
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

      <SettingsSection title="Workload health" icon="pulse"
        description="PostgreSQL lifetime counters for this database. Ratios and tuple activity help distinguish cache pressure, scan-heavy workloads, and write churn.">
        <KVTable :rows="[
          { key: 'PostgreSQL',           value: db.version || '—', mono: true },
          { key: 'Transactions committed', value: db.transactions_committed.toLocaleString() },
          { key: 'Transactions rolled back', value: db.transactions_rolled_back.toLocaleString() },
          { key: 'Buffer cache hit ratio', value: `${db.buffer_cache_hit_ratio.toFixed(2)}%` },
          { key: 'Index scan ratio',      value: `${db.index_scan_ratio.toFixed(2)}%` },
          { key: 'Rows returned / fetched', value: `${db.rows_returned.toLocaleString()} / ${db.rows_fetched.toLocaleString()}` },
          { key: 'Rows inserted / updated / deleted', value: `${db.rows_inserted.toLocaleString()} / ${db.rows_updated.toLocaleString()} / ${db.rows_deleted.toLocaleString()}` },
          { key: 'Dead tuples',           value: db.dead_tuples.toLocaleString() },
          { key: 'Temporary bytes',       value: fmtBytes(db.temp_bytes) },
          { key: 'Deadlocks',             value: db.deadlocks.toLocaleString() },
        ]" />
      </SettingsSection>

      <SettingsSection title="Expensive statements" icon="timer"
        description="Database-wide pg_stat_statements totals, including both API and worker processes. Query text is normalized and sanitized before display.">
        <div v-if="db.query_stats_error" class="pg-stats-setup">
          <div class="setup-heading">
            <Icon name="warning" :size="15" />
            <div>
              <strong>Statement history needs PostgreSQL configuration</strong>
              <span>{{ db.query_stats_error }}</span>
            </div>
          </div>
          <p>The bundled Compose and all-in-one images now preload the module automatically. Recreate or restart PostgreSQL, then restart Heya so it can install the extension.</p>
          <div class="setup-commands">
            <code>shared_preload_libraries = 'pg_stat_statements'</code>
            <code>CREATE EXTENSION IF NOT EXISTS pg_stat_statements;</code>
          </div>
          <small>External PostgreSQL users must apply both settings with a role allowed to create extensions.</small>
        </div>
        <div v-else-if="!db.query_stats_available" class="empty-state">
          <Icon name="info" :size="14" /> pg_stat_statements is not enabled; the Diagnostics dashboard still shows API-process query timings.
        </div>
        <div v-else-if="!(db.top_queries ?? []).length" class="empty-state"><Icon name="info" :size="14" /> No statement stats yet.</div>
        <div v-else class="query-table" role="table" aria-label="Expensive PostgreSQL statements">
          <div class="query-row query-head" role="row"><span>Statement</span><span>Calls</span><span>Avg</span><span>Max</span><span>Total</span></div>
          <div v-for="q in db.top_queries ?? []" :key="q.statement" class="query-row" role="row">
            <code :title="q.statement">{{ q.statement }}</code>
            <span>{{ q.calls.toLocaleString() }}</span>
            <span>{{ q.average_ms.toFixed(2) }} ms</span>
            <span>{{ q.max_ms.toFixed(2) }} ms</span>
            <span>{{ q.total_duration_ms.toFixed(1) }} ms</span>
          </div>
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

.pg-stats-setup {
  padding: 13px 15px;
  border: 1px solid color-mix(in srgb, var(--gold) 35%, var(--border));
  border-radius: var(--r-md);
  background: color-mix(in srgb, var(--gold) 5%, var(--bg-2));
}
.setup-heading { display: flex; align-items: flex-start; gap: 9px; color: var(--gold); }
.setup-heading svg { flex: none; margin-top: 2px; }
.setup-heading strong { display: block; color: var(--fg-1); font-size: 12.5px; }
.setup-heading span { display: block; margin-top: 2px; color: var(--fg-3); font-family: var(--font-mono); font-size: 10.5px; }
.pg-stats-setup p { margin: 10px 0; color: var(--fg-2); font-size: 11.5px; line-height: 1.5; }
.setup-commands { display: grid; gap: 5px; }
.setup-commands code { padding: 7px 9px; overflow-x: auto; border: 1px solid var(--border); border-radius: var(--r-sm); background: var(--bg-0); color: var(--fg-1); font-family: var(--font-mono); font-size: 10.5px; white-space: nowrap; }
.pg-stats-setup small { display: block; margin-top: 8px; color: var(--fg-4); font-size: 10.5px; }

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

.query-table { overflow: hidden; border: 1px solid var(--border); border-radius: var(--r-md); }
.query-row {
  display: grid;
  grid-template-columns: minmax(300px, 1fr) 65px 90px 90px 100px;
  gap: 10px;
  align-items: center;
  min-height: 38px;
  padding: 6px 11px;
  border-bottom: 1px solid var(--hair);
  color: var(--fg-2);
  font-family: var(--font-mono);
  font-size: 10.5px;
}
.query-row:last-child { border-bottom: 0; }
.query-head { min-height: 31px; background: var(--bg-2); color: var(--fg-3); font-size: 9px; letter-spacing: .08em; text-transform: uppercase; }
.query-row code { overflow: hidden; color: var(--fg-1); font-family: inherit; text-overflow: ellipsis; white-space: nowrap; }
.query-row span:not(:first-child) { text-align: right; font-variant-numeric: tabular-nums; }

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
  .query-table { overflow-x: auto; }
  .query-row { min-width: 780px; }
}
</style>
