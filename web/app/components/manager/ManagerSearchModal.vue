<script setup lang="ts">
// Interactive shadow search: run the full decision pipeline for one item
// and show every candidate with its verdict — what would be grabbed, what
// was rejected, and exactly why. Dry-run: nothing is ever sent to a client.
import type { ManagerSearchRunView } from '~/queries/manager'

const props = defineProps<{
  mediaItemId: number
  title: string
  /** TV: narrow the search to one season or one episode. */
  season?: number
  episodeId?: number
  scopeLabel?: string
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
watch(() => [props.season, props.episodeId], () => { run.value = null })

async function search() {
  loading.value = true
  error.value = ''
  run.value = null
  try {
    run.value = await $heya(`/api/manager/media/${props.mediaItemId}/search`, {
      method: 'POST',
      query: {
        season: props.episodeId == null ? props.season : undefined,
        episode_id: props.episodeId,
      },
    }) as ManagerSearchRunView
  } catch (e: any) {
    error.value = e?.data?.detail ?? e?.message ?? 'Search failed'
  } finally {
    loading.value = false
  }
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
  <AppDialog v-model="open" :title="`Search · ${title}${scopeLabel ? ' ' + scopeLabel : ''}`" size="xl">
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

      <div class="msm-list">
        <ManagerCandidateRow
          v-for="cand in run.candidates ?? []" :key="`${cand.indexer}-${cand.title}`"
          :title="cand.title" :indexer="cand.indexer" :quality="cand.quality"
          :score="cand.format_score" :size-bytes="cand.size_bytes"
          :publish-date="cand.publish_date" :breakdown="cand.format_breakdown ?? []"
          :rejections="cand.rejections ?? []" :acceptable="cand.acceptable"
          :chosen="cand.chosen" :selection-rank="cand.selection_rank"
        />
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

.msm-list { max-height: 62vh; overflow-y: auto; border: 1px solid var(--border); border-radius: var(--r-md); }
.msm-note { margin: 10px 2px 0; font-size: 11.5px; color: var(--fg-3); }
.mono { font-family: var(--font-mono); }
</style>
