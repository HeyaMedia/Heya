<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { useQuery } from '@pinia/colada'
import { defineQueryOptions } from '@pinia/colada'
import { librariesQuery } from '~/queries/catalog'
import { managerLibraryIcon } from '~/composables/useManagerNav'
import type { ManagerWantedPage, ManagerWantedRow } from '~~/shared/api/types.gen'

const { $heya } = useNuxtApp()

const tab = ref<'missing' | 'cutoff' | 'problems'>('missing')
const libraryFilter = ref<number>(0)
const page = ref(1)

const librariesData = useQuery(librariesQuery)
const libraries = computed(() => librariesData.data.value ?? [])

const wantedQuery = defineQueryOptions((p: { tab: string, library: number, page: number }) => ({
  key: ['manager', 'wanted', p.tab, p.library, p.page],
  query: async () => {
    const { $heya: heya } = useNuxtApp()
    return await heya('/api/manager/wanted', {
      query: {
        tab: p.tab,
        libraries: p.library ? String(p.library) : undefined,
        page: p.page,
        per_page: 50,
      },
    }) as ManagerWantedPage
  },
  placeholderData: (prev: ManagerWantedPage | undefined) => prev,
  staleTime: 1000 * 30,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

const { data, asyncStatus, error, refetch } = useQuery(() => wantedQuery({
  tab: tab.value, library: libraryFilter.value, page: page.value,
}))
watch([tab, libraryFilter], () => { page.value = 1 })

const rows = computed(() => data.value?.rows ?? [])
const totalPages = computed(() => Math.max(1, Math.ceil((data.value?.total ?? 0) / 50)))

// Rows grouped per library — the wanted list reads as "what each library
// still owes", with a per-group search-all action.
const groups = computed(() => {
  const byLib = new Map<number, ManagerWantedRow[]>()
  for (const row of rows.value) {
    if (!byLib.has(row.library_id)) byLib.set(row.library_id, [])
    byLib.get(row.library_id)!.push(row)
  }
  return [...byLib.entries()].map(([id, groupRows]) => {
    const lib = libraries.value.find(l => l.id === id)
    return {
      id,
      name: lib?.name ?? `Library ${id}`,
      mediaType: lib?.media_type ?? '',
      rows: groupRows,
    }
  }).sort((a, b) => a.name.localeCompare(b.name))
})

function unitLabel(row: ManagerWantedRow): string {
  if (row.kind === 'album') return row.album_title ?? ''
  if (row.season != null && row.episode != null) {
    const ep = `S${String(row.season).padStart(2, '0')}E${String(row.episode).padStart(2, '0')}`
    return row.episode_name ? `${ep} · ${row.episode_name}` : ep
  }
  return ''
}

function whyLabel(row: ManagerWantedRow): string {
  return row.shortfall || row.problem || ''
}

function seenVia(row: ManagerWantedRow): string {
  switch (row.last_decision_kind) {
    case 'rss': return 'via RSS'
    case 'interactive': return 'searched'
    case 'search': return 'searched'
    case 'add_search': return 'post-add search'
    case 'queue_verdict': return 'queue'
    default: return row.last_decision_kind ?? ''
  }
}

// Music release search isn't wired yet (music targets are the recorded
// deferred slice) — albums list for visibility, search stays off.
const searchable = (row: ManagerWantedRow) => row.kind !== 'album'

// Per-row shadow search → run the interactive pipeline and jump to History.
const searching = ref<number | null>(null)
const flash = ref('')
async function searchRow(row: ManagerWantedRow, index: number) {
  searching.value = index
  flash.value = ''
  try {
    const query: Record<string, unknown> = {}
    if (row.episode_id) query.episode_id = row.episode_id
    else if (row.season != null) query.season = row.season
    const run = await $heya(`/api/manager/media/${row.media_item_id}/search`, {
      method: 'POST', query,
    }) as { run_id: number, verdict: string, chosen_title?: string }
    flash.value = run.verdict === 'would_grab'
      ? `Run #${run.run_id}: would grab ${run.chosen_title}`
      : `Run #${run.run_id}: ${run.verdict.replaceAll('_', ' ')}`
    refetch()
  } catch (e: any) {
    flash.value = e?.data?.detail ?? 'Search failed'
  } finally {
    searching.value = null
  }
}

// Search-all for one library group: dedupe to sensible scopes (movies →
// item, episodes → one season search per item+season) and run them
// sequentially — polite to the indexers, and every run lands in History.
const searchingAll = ref<number | null>(null)
const searchAllProgress = ref('')
async function searchAllGroup(group: { id: number, name: string, rows: ManagerWantedRow[] }) {
  if (searching.value !== null || searchingAll.value !== null) return
  const scopes: { itemID: number, season?: number, label: string }[] = []
  const seen = new Set<string>()
  for (const row of group.rows) {
    if (!searchable(row)) continue
    const isEpisode = row.kind === 'episode' && row.season != null
    const key = isEpisode ? `${row.media_item_id}:s${row.season}` : `m${row.media_item_id}`
    if (seen.has(key)) continue
    seen.add(key)
    scopes.push({
      itemID: row.media_item_id,
      season: isEpisode ? row.season! : undefined,
      label: isEpisode ? `${row.title} S${String(row.season).padStart(2, '0')}` : row.title,
    })
  }
  if (!scopes.length) return
  searchingAll.value = group.id
  flash.value = ''
  let grabs = 0
  try {
    for (const [i, scope] of scopes.entries()) {
      searchAllProgress.value = `Searching ${i + 1}/${scopes.length} — ${scope.label}`
      try {
        const run = await $heya(`/api/manager/media/${scope.itemID}/search`, {
          method: 'POST',
          query: scope.season != null ? { season: scope.season } : {},
        }) as { verdict: string }
        if (run.verdict === 'would_grab') grabs++
      } catch {
        // Per-target failures are recorded in the run ledger — keep sweeping.
      }
    }
    flash.value = `Searched ${scopes.length} ${scopes.length === 1 ? 'target' : 'targets'} in ${group.name} — ${grabs} would grab. Details in History.`
    refetch()
  } finally {
    searchingAll.value = null
    searchAllProgress.value = ''
  }
}

const TABS = [
  { value: 'missing' as const, label: 'Missing' },
  { value: 'cutoff' as const, label: 'Cutoff unmet' },
  { value: 'problems' as const, label: 'Problems' },
]
</script>

<template>
  <div>
    <SettingsContextHero
      title="Wanted"
      icon="target"
      eyebrow="Manager · Dry run"
      description="Everything the pipeline still owes you, grouped by library — missing entirely, on disk below the quality cutoff, or misconfigured so it can't act. Each row shows how it was last seen (RSS or search) and what the verdict was."
    />

    <div class="w-toolbar">
      <div class="w-tabs" role="tablist" aria-label="Wanted category">
        <button
          v-for="t in TABS" :key="t.value"
          type="button" role="tab" class="w-tab"
          :aria-selected="tab === t.value" :class="{ active: tab === t.value }"
          @click="tab = t.value"
        >{{ t.label }}<span v-if="tab === t.value && data" class="w-count">{{ data.total }}</span></button>
      </div>
      <select v-model.number="libraryFilter" class="w-select" aria-label="Library filter">
        <option :value="0">All libraries</option>
        <option v-for="lib in libraries" :key="lib.id" :value="lib.id">{{ lib.name }}</option>
      </select>
    </div>

    <div v-if="searchAllProgress" class="mgr-flash ok"><span class="mgr-spin" /> {{ searchAllProgress }}</div>
    <div v-else-if="flash" class="mgr-flash ok">{{ flash }}</div>

    <div v-if="error" class="w-empty">
      <Icon name="warning" :size="14" /> Couldn't load wanted items.
      <button type="button" class="mgr-btn" @click="refetch()">Retry</button>
    </div>
    <div v-else-if="asyncStatus === 'loading' && !data" class="w-empty">
      <span class="mgr-spin" /> Computing wanted…
    </div>

    <template v-else>
      <section v-for="group in groups" :key="group.id" class="w-group">
        <div class="w-group-head">
          <Icon :name="managerLibraryIcon(group.mediaType)" :size="15" class="w-group-icon" />
          <span class="w-group-name">{{ group.name }}</span>
          <span class="w-group-count mono">{{ group.rows.length }}<template v-if="totalPages > 1"> on this page</template></span>
          <AppTooltip v-if="group.mediaType === 'music'" label="Albums are listed for visibility — music release search lands with music targets">
            <span class="w-group-note mono">list only</span>
          </AppTooltip>
          <button
            v-else-if="tab !== 'problems'"
            type="button" class="mgr-btn w-group-search"
            :disabled="searching !== null || searchingAll !== null"
            @click="searchAllGroup(group)"
          >
            <span v-if="searchingAll === group.id" class="mgr-spin" />
            <Icon v-else name="search" :size="13" />
            Search all in {{ group.name }}
          </button>
        </div>

        <div class="w-table">
          <div class="w-head">
            <span>Item</span>
            <span>Profile</span>
            <span>{{ tab === 'cutoff' ? 'Current' : 'Released' }}</span>
            <span>{{ tab === 'problems' ? 'Why' : tab === 'cutoff' ? 'Why' : 'Last seen' }}</span>
            <span class="w-col-actions" aria-hidden="true" />
          </div>
          <div v-for="(row, i) in group.rows" :key="`${row.media_item_id}-${row.episode_id ?? 0}-${row.discography_id ?? 0}`" class="w-row">
            <div class="w-item">
              <div class="w-title">
                <NuxtLink :to="`/manager/library/${row.library_id}/${row.media_item_id}`" class="w-link">
                  {{ row.title }}<template v-if="row.year"> ({{ row.year }})</template>
                </NuxtLink>
              </div>
              <div v-if="unitLabel(row)" class="w-sub mono">{{ unitLabel(row) }}</div>
            </div>
            <span class="w-profile mono">{{ row.profile_name || '—' }}</span>
            <span v-if="tab === 'cutoff'" class="w-since">
              <span v-if="row.current_quality" class="mgr-quality">{{ row.current_quality }}</span>
              <span class="w-score mono">score <b>{{ row.current_score }}</b></span>
            </span>
            <span v-else class="w-since mono">{{ row.air_date || '—' }}</span>
            <span v-if="tab === 'missing'" class="w-search">
              <template v-if="row.last_decision_at">
                <StatusBadge :state="managerVerdictState(row.last_decision_verdict ?? '')">{{ managerVerdictLabel(row.last_decision_verdict ?? '') }}</StatusBadge>
                <span class="w-decided mono">{{ seenVia(row) }} · {{ new Date(row.last_decision_at).toLocaleDateString() }}</span>
              </template>
              <template v-else><span class="w-never">not seen yet — no RSS match, never searched</span></template>
            </span>
            <span v-else class="w-search w-why" :title="whyLabel(row)">{{ whyLabel(row) }}</span>
            <div class="w-actions">
              <AppTooltip :label="searchable(row) ? 'Shadow search now (dry run)' : 'Music release search lands with music targets'">
                <button
                  type="button" class="w-btn"
                  :disabled="searching !== null || searchingAll !== null || tab === 'problems' || !searchable(row)"
                  @click="searchRow(row, i)"
                >
                  <span v-if="searching === i" class="mgr-spin" />
                  <Icon v-else name="search" :size="14" />
                </button>
              </AppTooltip>
            </div>
          </div>
        </div>
      </section>

      <div v-if="rows.length === 0" class="w-empty">
        <Icon name="check" :size="14" />
        <template v-if="tab === 'missing'">Nothing missing for the selected libraries.</template>
        <template v-else-if="tab === 'cutoff'">Nothing below cutoff for the selected libraries.</template>
        <template v-else>No configuration problems.</template>
      </div>

      <div v-if="totalPages > 1" class="w-pager">
        <button type="button" class="mgr-btn" :disabled="page <= 1" @click="page--">Previous</button>
        <span class="w-page mono">{{ page }} / {{ totalPages }}</span>
        <button type="button" class="mgr-btn" :disabled="page >= totalPages" @click="page++">Next</button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.w-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  flex-wrap: wrap;
  margin-bottom: 18px;
}

.w-tabs { display: flex; gap: 6px; }
.w-tab {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 32px;
  padding: 0 14px;
  border-radius: 999px;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  font-size: 12.5px;
  font-weight: 600;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.w-tab:hover { color: var(--fg-0); background: rgb(var(--ink) / 0.09); }
.w-tab.active {
  color: var(--gold-bright);
  background: var(--gold-soft);
  border-color: color-mix(in srgb, var(--gold) 45%, transparent);
}
.w-count {
  font-family: var(--font-mono);
  font-size: 10.5px;
  padding: 1px 7px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--gold) 22%, transparent);
}
.w-select {
  height: 32px;
  padding: 0 10px;
  border-radius: var(--r-sm);
  background: var(--bg-2);
  border: 1px solid var(--border);
  color: var(--fg-1);
  font-size: 12.5px;
}

/* ── Library groups ──────────────────────────────────────────────────── */
.w-group { margin-bottom: 22px; }
.w-group-head {
  display: flex;
  align-items: center;
  gap: 9px;
  margin-bottom: 10px;
}
.w-group-icon { color: var(--fg-2); }
.w-group-name {
  font-family: var(--font-display);
  font-variation-settings: "wdth" 100;
  font-size: 16px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--fg-0);
}
.w-group-count {
  font-size: 10.5px;
  color: var(--fg-3);
  padding: 1px 8px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: rgb(var(--ink) / 0.05);
}
.w-group-note {
  font-size: 10px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--fg-3);
  border-bottom: 1px dotted var(--fg-4);
  cursor: help;
}
.w-group-search { margin-left: auto; }

.w-table { display: flex; flex-direction: column; gap: 6px; }
.w-head,
.w-row {
  display: grid;
  grid-template-columns: minmax(220px, 2.2fr) 110px minmax(120px, 0.9fr) minmax(200px, 1.6fr) 50px;
  gap: 14px;
  align-items: center;
}
.w-head {
  padding: 0 14px 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.w-row {
  padding: 12px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}

.w-item { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.w-title { font-size: 13.5px; font-weight: 500; color: var(--fg-0); }
.w-link { color: inherit; text-decoration: none; }
.w-link:hover { color: var(--gold-bright); }
.w-sub { font-size: 11.5px; color: var(--fg-2); }

.w-profile { font-size: 11.5px; color: var(--fg-1); }
.w-since { display: flex; align-items: center; gap: 7px; font-size: 11.5px; color: var(--fg-2); }
.w-score { font-size: 10.5px; color: var(--fg-3); }
.w-score b { color: var(--fg-0); font-weight: 700; }
.w-search { display: flex; align-items: center; gap: 7px; font-size: 11.5px; color: var(--fg-3); min-width: 0; }
.w-decided { flex-shrink: 0; }
.w-never { font-style: italic; }
.w-why { color: var(--gold-bright); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: block; }

.w-actions { display: flex; gap: 4px; justify-content: flex-end; }
.w-btn {
  width: 30px; height: 30px;
  display: flex; align-items: center; justify-content: center;
  border-radius: var(--r-sm);
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
}
.w-btn:hover:not(:disabled) { background: rgb(var(--ink) / 0.1); color: var(--fg-0); }
.w-btn:disabled { opacity: 0.5; cursor: default; }

.w-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
  margin-top: 6px;
}

.w-pager {
  display: flex; align-items: center; justify-content: center; gap: 14px;
  margin-top: 14px;
}
.w-page { font-size: 12px; color: var(--fg-2); }
.mono { font-family: var(--font-mono); }

@media (max-width: 960px) {
  .w-head { display: none; }
  .w-row { grid-template-columns: minmax(0, 1fr) auto; row-gap: 8px; }
  .w-profile { display: none; }
  .w-since, .w-search { grid-column: 1 / -1; }
  .w-actions { grid-row: 1; grid-column: 2; }
  .w-group-search { margin-left: 0; }
  .w-group-head { flex-wrap: wrap; }
}
</style>
