<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { useQuery } from '@pinia/colada'
import { managerHistoryQuery, managerRunDetailQuery } from '~/queries/manager'
import type { ManagerDecisionView, ManagerHistoryPage } from '~/queries/manager'
import { librariesQuery } from '~/queries/catalog'

const { $heya } = useNuxtApp()

const verdictFilter = ref<string[]>([])
const libraryFilter = ref<number>(0)

const VERDICTS = [
  { value: 'would_grab', label: 'Would grab' },
  { value: 'already_satisfied', label: 'Satisfied' },
  { value: 'no_acceptable_candidate', label: 'Nothing acceptable' },
  { value: 'comparison_uncertain', label: 'Uncertain' },
  { value: 'configuration_error', label: 'Config error' },
]

const VERDICT_META: Record<string, { label: string, state: 'ok' | 'warn' | 'error' | 'idle' }> = {
  would_grab: { label: 'would grab', state: 'ok' },
  already_satisfied: { label: 'satisfied', state: 'idle' },
  no_acceptable_candidate: { label: 'nothing acceptable', state: 'error' },
  comparison_uncertain: { label: 'uncertain', state: 'warn' },
  configuration_error: { label: 'config error', state: 'error' },
}

const librariesData = useQuery(librariesQuery)
const libraries = computed(() => librariesData.data.value ?? [])

const { data, asyncStatus, error, refetch } = useQuery(() => managerHistoryQuery({
  verdicts: verdictFilter.value,
  library: libraryFilter.value || undefined,
}))

// Older pages accumulate client-side; filter changes reset the pile via the
// query key swap (extra keeps only pages loaded under the current key).
const extra = ref<ManagerDecisionView[]>([])
const extraCursor = ref<{ before: string, beforeId: number } | null>(null)
const loadingMore = ref(false)
watch(data, () => {
  extra.value = []
  extraCursor.value = null
})

const decisions = computed<ManagerDecisionView[]>(() => [
  ...(data.value?.decisions ?? []),
  ...extra.value,
])

const nextCursor = computed(() => {
  if (extraCursor.value) return extraCursor.value
  if (data.value?.next_before) return { before: data.value.next_before, beforeId: data.value.next_id ?? 0 }
  return null
})

async function loadMore() {
  const cursor = nextCursor.value
  if (!cursor || loadingMore.value) return
  loadingMore.value = true
  try {
    const page = await $heya('/api/manager/history', {
      query: {
        verdicts: verdictFilter.value.length ? verdictFilter.value.join(',') : undefined,
        library: libraryFilter.value || undefined,
        before: cursor.before,
        before_id: cursor.beforeId,
      },
    }) as ManagerHistoryPage
    extra.value.push(...(page.decisions ?? []))
    extraCursor.value = page.next_before ? { before: page.next_before, beforeId: page.next_id ?? 0 } : null
    if (!page.next_before) extraCursor.value = null
  } finally {
    loadingMore.value = false
  }
}

// Row expansion loads the run's full candidate record lazily.
const expandedRun = ref<number | null>(null)
const runDetail = useQuery(() => managerRunDetailQuery(expandedRun.value ?? 0))

function toggleRun(runId: number) {
  expandedRun.value = expandedRun.value === runId ? null : runId
}

function fmtWhen(iso: string): string {
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

function targetLabel(d: ManagerDecisionView): string {
  let label = d.target_title
  if (d.target_year) label += ` (${d.target_year})`
  if (d.target_kind === 'episode' && d.season_number != null && d.episode_number != null) {
    label += ` · S${String(d.season_number).padStart(2, '0')}E${String(d.episode_number).padStart(2, '0')}`
  }
  return label
}

function fmtSize(bytes?: number): string {
  if (!bytes || bytes <= 0) return '—'
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`
  return `${(bytes / 1024 ** 2).toFixed(0)} MB`
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="History"
      icon="clock"
      eyebrow="Manager · Dry run"
      description="The decision ledger: every evaluation the pipeline ran, what it would have grabbed, and why. Nothing is downloaded — this is the shadow record."
    />

    <div class="h-toolbar">
      <div class="h-verdicts" role="group" aria-label="Verdict filter">
        <button
          v-for="v in VERDICTS" :key="v.value" type="button" class="h-chip"
          :class="{ active: verdictFilter.includes(v.value) }"
          :aria-pressed="verdictFilter.includes(v.value)"
          @click="verdictFilter = verdictFilter.includes(v.value) ? verdictFilter.filter(x => x !== v.value) : [...verdictFilter, v.value]"
        >{{ v.label }}</button>
      </div>
      <select v-model.number="libraryFilter" class="h-select" aria-label="Library filter">
        <option :value="0">All libraries</option>
        <option v-for="lib in libraries" :key="lib.id" :value="lib.id">{{ lib.name }}</option>
      </select>
    </div>

    <div v-if="error" class="h-empty">
      <Icon name="warning" :size="14" /> Couldn't load history.
      <button type="button" class="mgr-btn" @click="refetch()">Retry</button>
    </div>

    <div v-else-if="asyncStatus === 'loading' && !data" class="h-empty">
      <span class="mgr-loading" /> Loading the ledger…
    </div>

    <template v-else>
      <ul class="h-feed">
        <li v-for="d in decisions" :key="d.id">
          <button type="button" class="h-row" :aria-expanded="expandedRun === d.run_id" @click="toggleRun(d.run_id)">
            <StatusBadge :state="VERDICT_META[d.verdict]?.state ?? 'idle'">{{ VERDICT_META[d.verdict]?.label ?? d.verdict }}</StatusBadge>
            <div class="h-body">
              <div class="h-title">
                {{ targetLabel(d) }}
                <span class="h-domain mono">{{ d.domain }}</span>
              </div>
              <div class="h-detail">
                <template v-if="d.chosen_title">would have grabbed <span class="mono">{{ d.chosen_title }}</span></template>
                <template v-else>{{ VERDICT_META[d.verdict]?.label ?? d.verdict }}<template v-if="d.profile_name"> under {{ d.profile_name }}</template></template>
              </div>
            </div>
            <div class="h-meta">
              <span class="h-time">{{ fmtWhen(d.decided_at) }}</span>
              <span class="h-via mono">{{ d.run_kind }} · run #{{ d.run_id }}</span>
            </div>
            <Icon :name="expandedRun === d.run_id ? 'chevdown' : 'chevright'" :size="14" class="h-chev" />
          </button>

          <div v-if="expandedRun === d.run_id" class="h-expand">
            <div v-if="runDetail.asyncStatus.value === 'loading' && !runDetail.data.value" class="h-expand-loading">
              <span class="mgr-loading" /> Loading run…
            </div>
            <template v-else-if="runDetail.data.value">
              <div class="h-indexers">
                <span v-for="idx in runDetail.data.value.indexers ?? []" :key="idx.indexer" class="h-idx mono">
                  {{ idx.indexer }}: {{ idx.status === 'ok' ? `${idx.fetched} results` : idx.status }}
                </span>
              </div>
              <div class="h-tablewrap">
                <table class="h-table">
                  <thead>
                    <tr><th>Release</th><th>Indexer</th><th>Quality</th><th class="num">Score</th><th class="num">Size</th><th>Verdict</th></tr>
                  </thead>
                  <tbody>
                    <tr v-for="cand in runDetail.data.value.candidates ?? []" :key="cand.id" :class="{ chosen: cand.per_target?.some(t => t.chosen) }">
                      <td class="h-rel mono" :title="cand.title">{{ cand.title }}</td>
                      <td>{{ cand.indexer }}</td>
                      <td class="mono">{{ cand.quality || '—' }}</td>
                      <td class="num mono">{{ cand.format_score }}</td>
                      <td class="num mono">{{ fmtSize(cand.size_bytes) }}</td>
                      <td class="h-why">
                        <template v-if="cand.per_target?.some(t => t.chosen)"><span class="h-star">★ chosen</span></template>
                        <template v-else-if="cand.per_target?.[0]?.verdict === 'acceptable'">#{{ cand.per_target[0]?.selection_rank }}</template>
                        <template v-else-if="cand.per_target?.[0]?.rejections?.length">{{ cand.per_target[0].rejections[0]!.code }}</template>
                        <template v-else-if="cand.rejections?.length">{{ cand.rejections[0]!.code }}</template>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </template>
          </div>
        </li>
      </ul>

      <div v-if="decisions.length === 0" class="h-empty">
        <Icon name="info" :size="14" /> No decisions recorded yet — run a search from a movie's manager page.
      </div>

      <div v-if="nextCursor" class="h-more">
        <button type="button" class="mgr-btn" :disabled="loadingMore" @click="loadMore">
          {{ loadingMore ? 'Loading…' : 'Load older decisions' }}
        </button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.h-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  flex-wrap: wrap;
  margin-bottom: 18px;
}
.h-verdicts { display: flex; gap: 6px; flex-wrap: wrap; }
.h-chip {
  height: 30px;
  padding: 0 12px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.h-chip:hover { color: var(--fg-0); }
.h-chip.active {
  color: var(--gold-bright);
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 45%, transparent);
}
.h-select {
  height: 30px;
  padding: 0 10px;
  border-radius: var(--r-sm);
  background: var(--bg-2);
  border: 1px solid var(--border);
  color: var(--fg-1);
  font-size: 12.5px;
}

.h-feed { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.h-row {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 11px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  transition: background 0.12s, border-color 0.12s;
}
.h-row:hover { background: var(--bg-3); border-color: color-mix(in srgb, var(--gold) 30%, var(--border)); }
.h-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.h-title { display: flex; align-items: center; gap: 8px; font-size: 13.5px; font-weight: 500; color: var(--fg-0); }
.h-domain { font-size: 10px; text-transform: uppercase; letter-spacing: 0.08em; color: var(--fg-3); }
.h-detail { font-size: 12px; color: var(--fg-2); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.h-meta { display: flex; flex-direction: column; align-items: flex-end; gap: 2px; flex-shrink: 0; }
.h-time { font-size: 11.5px; color: var(--fg-2); }
.h-via { font-size: 10.5px; color: var(--fg-3); }
.h-chev { color: var(--fg-3); flex-shrink: 0; }

.h-expand {
  margin: 4px 0 8px;
  padding: 12px 14px;
  background: rgb(var(--shade) / 0.16);
  border: 1px solid var(--hair);
  border-radius: var(--r-md);
}
.h-expand-loading { display: flex; align-items: center; gap: 8px; color: var(--fg-3); font-size: 12px; }
.h-indexers { display: flex; gap: 12px; flex-wrap: wrap; margin-bottom: 10px; }
.h-idx { font-size: 11px; color: var(--fg-2); }
.h-tablewrap { overflow-x: auto; max-height: 420px; overflow-y: auto; }
.h-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.h-table th {
  position: sticky; top: 0;
  background: var(--bg-2);
  text-align: left;
  padding: 6px 8px;
  font-family: var(--font-mono);
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--fg-3);
  border-bottom: 1px solid var(--border);
}
.h-table td { padding: 5px 8px; border-bottom: 1px solid var(--hair); }
.h-table th.num, .h-table td.num { text-align: right; }
.h-table tr.chosen td { background: var(--gold-soft); }
.h-rel { max-width: 480px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.h-why { font-family: var(--font-mono); font-size: 10.5px; color: var(--fg-2); white-space: nowrap; }
.h-star { color: var(--gold-bright); }

.h-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}
.h-more { display: flex; justify-content: center; margin-top: 14px; }
.mono { font-family: var(--font-mono); }
</style>
