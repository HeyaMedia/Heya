<script setup lang="ts">
// Interactive shadow search: run the full decision pipeline for one item
// and show every candidate with its verdict — what would be grabbed, what
// was rejected, and exactly why. Dry-run: nothing is ever sent to a client.
import type { ManagerSearchRunView, ManagerSearchCandidateView } from '~/queries/manager'

const props = defineProps<{
  mediaItemId: number
  title: string
}>()

const open = defineModel<boolean>({ default: false })

const { $heya } = useNuxtApp()
const loading = ref(false)
const error = ref('')
const run = ref<ManagerSearchRunView | null>(null)

watch(open, (isOpen) => {
  if (isOpen && !run.value && !loading.value) search()
  if (!isOpen) error.value = ''
})

async function search() {
  loading.value = true
  error.value = ''
  run.value = null
  try {
    run.value = await $heya(`/api/manager/media/${props.mediaItemId}/search`, { method: 'POST' }) as ManagerSearchRunView
  } catch (e: any) {
    error.value = e?.data?.detail ?? e?.message ?? 'Search failed'
  } finally {
    loading.value = false
  }
}

function rankLabel(cand: ManagerSearchCandidateView): string {
  if (cand.chosen) return '★'
  if (cand.acceptable && cand.selection_rank) return `#${cand.selection_rank}`
  return ''
}

function whyNot(cand: ManagerSearchCandidateView): string {
  return (cand.rejections ?? []).map(r => r.message).join(' · ')
}

function fmtSize(bytes: number): string {
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(0)} MB`
  return bytes > 0 ? `${bytes} B` : '—'
}

function fmtAge(date?: string): string {
  if (!date) return '—'
  const hours = (Date.now() - new Date(date).getTime()) / 36e5
  if (hours < 1) return '<1h'
  if (hours < 24) return `${Math.round(hours)}h`
  return `${Math.round(hours / 24)}d`
}

const VERDICT_META: Record<string, { label: string, state: 'ok' | 'warn' | 'error' | 'idle' }> = {
  would_grab: { label: 'would grab', state: 'ok' },
  already_satisfied: { label: 'already satisfied', state: 'idle' },
  no_acceptable_candidate: { label: 'no acceptable candidate', state: 'error' },
  comparison_uncertain: { label: 'comparison uncertain', state: 'warn' },
  configuration_error: { label: 'configuration error', state: 'error' },
}
</script>

<template>
  <AppDialog v-model="open" :title="`Search · ${title}`" size="xl">
    <div v-if="loading" class="msm-loading">
      <span class="mgr-loading" /> Querying indexers and evaluating candidates…
    </div>

    <div v-else-if="error" class="msm-error">
      <Icon name="warning" :size="14" /> {{ error }}
      <button type="button" class="mgr-btn" @click="search">Retry</button>
    </div>

    <template v-else-if="run">
      <div class="msm-summary">
        <StatusBadge :state="VERDICT_META[run.verdict]?.state ?? 'idle'">
          {{ VERDICT_META[run.verdict]?.label ?? run.verdict }}
        </StatusBadge>
        <span v-if="run.chosen_title" class="msm-chosen mono">{{ run.chosen_title }}</span>
        <span class="msm-meta">
          {{ run.profile || 'no profile' }} ·
          <template v-for="(idx, i) in run.indexers ?? []" :key="idx.indexer">
            <template v-if="i > 0"> · </template>{{ idx.indexer }} {{ idx.status === 'ok' ? idx.fetched : idx.status }}
          </template>
        </span>
        <button type="button" class="mgr-btn msm-again" :disabled="loading" @click="search">
          <Icon name="arrows-clockwise" :size="13" /> Search again
        </button>
      </div>

      <div class="msm-tablewrap">
        <table class="msm-table">
          <thead>
            <tr>
              <th class="msm-rank" aria-label="Rank" />
              <th>Release</th>
              <th>Indexer</th>
              <th>Quality</th>
              <th class="num">Score</th>
              <th class="num">Size</th>
              <th class="num">Age</th>
              <th>Verdict</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="cand in run.candidates ?? []" :key="`${cand.indexer}-${cand.title}`" :class="{ chosen: cand.chosen, rejected: !cand.acceptable }">
              <td class="msm-rank" :class="{ gold: cand.chosen }">{{ rankLabel(cand) }}</td>
              <td class="msm-title mono" :title="cand.title">{{ cand.title }}</td>
              <td>{{ cand.indexer }}</td>
              <td class="mono">{{ cand.quality || '—' }}</td>
              <td class="num mono">{{ cand.format_score }}</td>
              <td class="num mono">{{ fmtSize(cand.size_bytes) }}</td>
              <td class="num mono">{{ fmtAge(cand.publish_date) }}</td>
              <td class="msm-why">
                <span v-if="cand.acceptable" class="msm-ok"><Icon name="check" :size="12" /> acceptable</span>
                <AppTooltip v-else-if="cand.rejections?.length" :label="whyNot(cand)">
                  <span class="msm-rej">{{ cand.rejections![0]!.code }}<template v-if="cand.rejections!.length > 1"> +{{ cand.rejections!.length - 1 }}</template></span>
                </AppTooltip>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <p class="msm-note">Dry run — nothing is sent to a download client. This run is recorded in History as run #{{ run.run_id }}.</p>
    </template>
  </AppDialog>
</template>

<style scoped>
.msm-loading,
.msm-error {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 28px 6px;
  color: var(--fg-2);
  font-size: 13px;
}
.msm-error { color: var(--bad); }

.msm-summary {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.msm-chosen { font-size: 12px; color: var(--fg-1); overflow-wrap: anywhere; }
.msm-meta { font-size: 11.5px; color: var(--fg-3); }
.msm-again { margin-left: auto; }

.msm-tablewrap { overflow-x: auto; max-height: 60vh; overflow-y: auto; border: 1px solid var(--border); border-radius: var(--r-md); }
.msm-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.msm-table th {
  position: sticky;
  top: 0;
  background: var(--bg-2);
  text-align: left;
  padding: 8px 10px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--fg-3);
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
}
.msm-table td { padding: 6px 10px; border-bottom: 1px solid var(--hair); vertical-align: top; }
.msm-table th.num, .msm-table td.num { text-align: right; }
.msm-table tr.chosen td { background: var(--gold-soft); }
.msm-table tr.rejected td { opacity: 0.62; }
.msm-rank { width: 34px; text-align: center; font-family: var(--font-mono); font-size: 11px; }
.msm-rank.gold { color: var(--gold-bright); }
.msm-title { max-width: 420px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.msm-ok { display: inline-flex; align-items: center; gap: 4px; color: var(--good); font-size: 11.5px; }
.msm-rej {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--bad);
  cursor: help;
  white-space: nowrap;
}
.msm-note { margin: 10px 2px 0; font-size: 11.5px; color: var(--fg-3); }
.mono { font-family: var(--font-mono); }
</style>
