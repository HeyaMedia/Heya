<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { useQuery, defineQueryOptions } from '@pinia/colada'
import type { ManagerQueueView, ManagerQueueItemView } from '~~/shared/api/types.gen'

const queueQuery = defineQueryOptions(() => ({
  key: ['manager', 'queue'],
  query: async () => {
    const { $heya } = useNuxtApp()
    return await $heya('/api/manager/queue') as ManagerQueueView
  },
  staleTime: 1000 * 10,
  meta: { prefetch: 'none', sensitivity: 'private' },
}))

const { data, asyncStatus, error, refetch } = useQuery(queueQuery)

// The queue is live — poll while the page is visible.
const poll = setInterval(() => {
  if (document.visibilityState === 'visible') refetch()
}, 15000)
onUnmounted(() => clearInterval(poll))

const active = computed(() => (data.value?.items ?? []).filter(i => !i.history))
const finished = computed(() => (data.value?.items ?? []).filter(i => i.history))

const VERDICT_META: Record<string, { label: string, state: 'ok' | 'warn' | 'error' | 'idle' }> = {
  would_accept: { label: 'Heya agrees', state: 'ok' },
  would_reject: { label: 'Heya would reject', state: 'error' },
  unknown_identity: { label: 'unknown to Heya', state: 'idle' },
  unmonitored: { label: 'not monitored', state: 'idle' },
  no_profile: { label: 'no profile', state: 'warn' },
}

function pct(item: ManagerQueueItemView): number {
  return Math.min(100, Math.max(0, item.percentage))
}

function fmtSize(mb: number): string {
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${Math.round(mb)} MB`
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Queue"
      icon="cloud-download"
      eyebrow="Manager · Dry run"
      description="Everything the download clients are working on — grabbed by your live arr setup — with Heya's shadow verdict beside each: would this pipeline have taken the same release?"
    />

    <div v-if="error" class="q-empty">
      <Icon name="warning" :size="14" /> Couldn't reach the download clients.
      <button type="button" class="mgr-btn" @click="refetch()">Retry</button>
    </div>
    <div v-else-if="asyncStatus === 'loading' && !data" class="q-empty">
      <span class="mgr-loading" /> Loading queues…
    </div>

    <template v-else-if="data">
      <div v-for="err in data.errors ?? []" :key="err" class="mgr-flash err">{{ err }}</div>

      <div class="q-table" role="table" aria-label="Active downloads">
        <div class="q-head">
          <span>Release</span>
          <span>Heya verdict</span>
          <span class="q-col-progress">Progress</span>
          <span>Status</span>
        </div>
        <div v-for="item in active" :key="`${item.client}-${item.nzo_id}`" class="q-row">
          <div class="q-release">
            <div class="q-title mono" :title="item.name">{{ item.name }}</div>
            <div class="q-sub">
              <template v-if="item.matched_title">
                <NuxtLink v-if="item.matched_item_id" :to="`/manager/library/${item.matched_library}/${item.matched_item_id}`" class="q-matchlink">{{ item.matched_title }}</NuxtLink>
                <template v-else>{{ item.matched_title }}</template> ·
              </template>
              {{ item.client }} · {{ item.category || 'no category' }}
            </div>
          </div>
          <div class="q-verdict">
            <AppTooltip v-if="item.verdict_detail" :label="item.verdict_detail">
              <StatusBadge :state="VERDICT_META[item.verdict]?.state ?? 'idle'">{{ VERDICT_META[item.verdict]?.label ?? item.verdict }}</StatusBadge>
            </AppTooltip>
            <StatusBadge v-else :state="VERDICT_META[item.verdict]?.state ?? 'idle'">{{ VERDICT_META[item.verdict]?.label ?? item.verdict }}</StatusBadge>
          </div>
          <div class="q-progress">
            <div class="q-bar"><div class="q-bar-fill" :style="{ width: `${pct(item)}%` }" /></div>
            <span class="q-progress-text mono">{{ fmtSize(item.size_mb - item.size_left_mb) }} / {{ fmtSize(item.size_mb) }}<template v-if="item.time_left"> · {{ item.time_left }}</template></span>
          </div>
          <StatusBadge :state="item.status.toLowerCase() === 'downloading' ? 'ok' : 'idle'">{{ item.status.toLowerCase() }}</StatusBadge>
        </div>
      </div>
      <div v-if="active.length === 0" class="q-empty">
        <Icon name="info" :size="14" /> Nothing downloading right now.
      </div>

      <template v-if="finished.length">
        <h2 class="q-section">Recently completed</h2>
        <div class="q-table">
          <div v-for="item in finished" :key="`${item.client}-${item.nzo_id}`" class="q-row done">
            <div class="q-release">
              <div class="q-title mono" :title="item.name">{{ item.name }}</div>
              <div class="q-sub">
                <template v-if="item.matched_title">
                  <NuxtLink v-if="item.matched_item_id" :to="`/manager/library/${item.matched_library}/${item.matched_item_id}`" class="q-matchlink">{{ item.matched_title }}</NuxtLink>
                  <template v-else>{{ item.matched_title }}</template> ·
                </template>
                {{ item.client }} · {{ fmtSize(item.size_mb) }}
                <span v-if="item.fail_message" class="q-fail">· {{ item.fail_message }}</span>
              </div>
            </div>
            <div class="q-verdict">
              <AppTooltip v-if="item.verdict_detail" :label="item.verdict_detail">
                <StatusBadge :state="VERDICT_META[item.verdict]?.state ?? 'idle'">{{ VERDICT_META[item.verdict]?.label ?? item.verdict }}</StatusBadge>
              </AppTooltip>
              <StatusBadge v-else :state="VERDICT_META[item.verdict]?.state ?? 'idle'">{{ VERDICT_META[item.verdict]?.label ?? item.verdict }}</StatusBadge>
            </div>
            <span class="q-when mono">{{ item.completed_at ? new Date(item.completed_at * 1000).toLocaleString() : '' }}</span>
            <StatusBadge :state="item.status.toLowerCase() === 'completed' ? 'ok' : item.status.toLowerCase() === 'failed' ? 'error' : 'idle'">{{ item.status.toLowerCase() }}</StatusBadge>
          </div>
        </div>
      </template>
    </template>
  </div>
</template>

<style scoped>
.q-table { display: flex; flex-direction: column; gap: 6px; }
.q-head,
.q-row {
  display: grid;
  grid-template-columns: minmax(260px, 2.4fr) 150px minmax(180px, 1.2fr) 110px;
  gap: 14px;
  align-items: center;
}
.q-head {
  padding: 0 14px 6px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.q-row {
  padding: 12px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.q-row.done { opacity: 0.85; }

.q-release { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
.q-title { font-size: 12.5px; color: var(--fg-0); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.q-sub { font-size: 11.5px; color: var(--fg-2); }
.q-matchlink { color: var(--gold-bright); text-decoration: none; }
.q-fail { color: var(--bad); }

.q-progress { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.q-bar { height: 5px; border-radius: 999px; background: rgb(var(--ink) / 0.1); overflow: hidden; }
.q-bar-fill { height: 100%; border-radius: 999px; background: var(--gold); transition: width 0.4s; }
.q-progress-text { font-size: 10.5px; color: var(--fg-3); }
.q-when { font-size: 11px; color: var(--fg-3); }

.q-section {
  font-family: var(--font-display);
  font-variation-settings: "wdth" 100;
  font-size: 17px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--fg-0);
  margin: 24px 0 12px;
}

.q-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
  margin-top: 6px;
}
.mono { font-family: var(--font-mono); }

@media (max-width: 960px) {
  .q-head { display: none; }
  .q-row { grid-template-columns: minmax(0, 1fr) auto; row-gap: 8px; }
  .q-progress, .q-when { grid-column: 1 / -1; }
}
</style>
