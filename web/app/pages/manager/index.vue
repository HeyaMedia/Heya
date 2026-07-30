<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { useQuery, defineQueryOptions } from '@pinia/colada'
import type { ManagerActivityPage, ManagerActivityRun, ManagerQueueView, ManagerWantedPage } from '~~/shared/api/types.gen'

const activityQuery = defineQueryOptions((page: number) => ({
  key: ['manager', 'activity', page],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/manager/activity', { query: { page, per_page: 30 } }) as ManagerActivityPage
  },
  placeholderData: (prev: ManagerActivityPage | undefined) => prev,
  staleTime: 1000 * 15,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

const quickQueueQuery = defineQueryOptions(() => ({
  key: ['manager', 'queue'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/manager/queue') as ManagerQueueView
  },
  staleTime: 1000 * 10,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

const quickWantedQuery = defineQueryOptions(() => ({
  key: ['manager', 'wanted', 'missing', 0, 1],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/manager/wanted', { query: { tab: 'missing', per_page: 1 } }) as ManagerWantedPage
  },
  staleTime: 1000 * 30,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

const page = ref(1)
const { data, asyncStatus, error, refetch } = useQuery(() => activityQuery(page.value))
const queueData = useQuery(quickQueueQuery)
const wantedData = useQuery(quickWantedQuery)

const activeDownloads = computed(() => (queueData.data.value?.items ?? []).filter(i => !i.history).length)
const lastRSS = computed(() => {
  const rss = (data.value?.runs ?? []).find(r => r.kind === 'rss')
  if (!rss) return 'never'
  const mins = Math.round((Date.now() - new Date(rss.started_at).getTime()) / 60000)
  return mins < 1 ? 'just now' : mins < 60 ? `${mins}m ago` : `${Math.round(mins / 60)}h ago`
})
const totalPages = computed(() => Math.max(1, Math.ceil((data.value?.total ?? 0) / 30)))

const KIND_META: Record<string, { icon: string, label: string }> = {
  rss: { icon: 'refresh', label: 'RSS sweep' },
  interactive: { icon: 'search', label: 'Interactive search' },
  search: { icon: 'search', label: 'Search' },
  add: { icon: 'plus', label: 'Item added' },
  add_search: { icon: 'search', label: 'Post-add search' },
  reevaluate: { icon: 'refresh', label: 'Re-evaluation' },
  queue_verdict: { icon: 'cloud-download', label: 'Queue verdicts' },
}

function runSummary(run: ManagerActivityRun): string {
  const stats = run.stats as Record<string, unknown> | undefined
  if (!stats) return ''
  if (run.kind === 'rss') {
    return `${stats.fetched ?? 0} fetched · ${stats.fresh ?? 0} fresh · ${stats.matched ?? 0} matched · ${stats.would_grabs ?? 0} would grab`
  }
  const scope = run.scope as Record<string, unknown> | undefined
  const target = scope?.title ? `${scope.title}${scope.scope ? ' ' + scope.scope : ''}` : ''
  return [target, stats.verdict ? managerVerdictLabel(String(stats.verdict)) : ''].filter(Boolean).join(' — ')
}

function fmtWhen(iso: string): string {
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Activity"
      icon="pulse"
      eyebrow="Manager · Dry run"
      description="Acquisition at a glance: what's downloading, what's still wanted, and every run the pipeline made — RSS sweeps, searches, and their outcomes."
    />

    <div class="tiles">
      <MetricTile label="Active downloads" :value="activeDownloads" icon="cloud-download" tone="good" sub="across enabled clients" />
      <MetricTile label="Missing" :value="wantedData.data.value?.total ?? 0" icon="target" tone="warn" sub="monitored, no file yet" />
      <MetricTile label="Runs recorded" :value="data?.total ?? 0" icon="pulse" tone="neutral" sub="decision ledger entries" />
      <MetricTile label="Last RSS sweep" :value="lastRSS" icon="refresh" tone="good" sub="dry run — nothing downloads" />
    </div>

    <div v-if="error" class="a-empty">
      <Icon name="warning" :size="14" /> Couldn't load activity.
      <button type="button" class="mgr-btn" @click="refetch()">Retry</button>
    </div>
    <div v-else-if="asyncStatus === 'loading' && !data" class="a-empty">
      <span class="mgr-spin" /> Loading runs…
    </div>

    <template v-else-if="data">
      <ul class="a-feed">
        <li v-for="run in data.runs ?? []" :key="run.id" class="a-row">
          <span class="a-icon"><Icon :name="KIND_META[run.kind]?.icon ?? 'info'" :size="14" /></span>
          <div class="a-body">
            <div class="a-title">
              {{ KIND_META[run.kind]?.label ?? run.kind }}
              <StatusBadge :state="run.status === 'completed' ? (run.partial ? 'warn' : 'ok') : run.status === 'running' ? 'idle' : 'error'">
                {{ run.status }}{{ run.partial ? ' (partial)' : '' }}{{ run.truncated ? ' (truncated)' : '' }}
              </StatusBadge>
            </div>
            <div class="a-detail">{{ runSummary(run) }}</div>
            <div v-if="run.indexers?.length" class="a-indexers">
              <span v-for="idx in run.indexers" :key="`${idx.indexer}-${idx.domain}`" class="a-idx mono" :class="{ bad: idx.status === 'failed' }">
                {{ idx.indexer }}/{{ idx.domain }}: {{ idx.status === 'ok' ? idx.fetched : idx.status }}
              </span>
            </div>
          </div>
          <div class="a-meta">
            <span class="a-time">{{ fmtWhen(run.started_at) }}</span>
            <span class="a-via mono">{{ run.source }} · run #{{ run.id }}</span>
          </div>
        </li>
      </ul>

      <div v-if="(data.runs ?? []).length === 0" class="a-empty">
        <Icon name="info" :size="14" /> No runs recorded yet — trigger a search from a manager page, or run an RSS sweep.
      </div>

      <div v-if="totalPages > 1" class="a-pager">
        <button type="button" class="mgr-btn" :disabled="page <= 1" @click="page--">Newer</button>
        <span class="a-page mono">{{ page }} / {{ totalPages }}</span>
        <button type="button" class="mgr-btn" :disabled="page >= totalPages" @click="page++">Older</button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: 10px;
  margin-bottom: 22px;
}

.a-feed { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.a-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.a-icon {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px; height: 30px;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.05);
  color: var(--fg-2);
}
.a-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.a-title { display: flex; align-items: center; gap: 8px; font-size: 13.5px; font-weight: 500; color: var(--fg-0); }
.a-detail { font-size: 12px; color: var(--fg-2); }
.a-indexers { display: flex; gap: 10px; flex-wrap: wrap; margin-top: 2px; }
.a-idx { font-size: 10.5px; color: var(--fg-3); }
.a-idx.bad { color: var(--bad); }
.a-meta { display: flex; flex-direction: column; align-items: flex-end; gap: 2px; flex-shrink: 0; }
.a-time { font-size: 11.5px; color: var(--fg-2); }
.a-via { font-size: 10.5px; color: var(--fg-3); }

.a-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}
.a-pager {
  display: flex; align-items: center; justify-content: center; gap: 14px;
  margin-top: 14px;
}
.a-page { font-size: 12px; color: var(--fg-2); }
.mono { font-family: var(--font-mono); }
</style>
