<script setup lang="ts">
// Entity accountability: the decisions ledger slice for one media item —
// every evaluation run that touched it, its verdict, and what would have
// been grabbed.
import { useQuery } from '@pinia/colada'
import { managerItemDecisionsQuery } from '~/queries/manager'

const props = defineProps<{ mediaItemId: number }>()

const { data, asyncStatus } = useQuery(() => managerItemDecisionsQuery(props.mediaItemId))

const VERDICT_META: Record<string, { label: string, state: 'ok' | 'warn' | 'error' | 'idle' }> = {
  would_grab: { label: 'would grab', state: 'ok' },
  already_satisfied: { label: 'satisfied', state: 'idle' },
  no_acceptable_candidate: { label: 'nothing acceptable', state: 'error' },
  comparison_uncertain: { label: 'uncertain', state: 'warn' },
  configuration_error: { label: 'config error', state: 'error' },
}

function fmtWhen(iso: string): string {
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

function fmtSize(bytes: number): string {
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`
  return `${(bytes / 1024 ** 2).toFixed(0)} MB`
}

function unitLabel(d: { target_kind: string, season_number?: number, episode_number?: number, album_title?: string }): string {
  if (d.target_kind === 'episode' && d.season_number != null && d.episode_number != null) {
    return `S${String(d.season_number).padStart(2, '0')}E${String(d.episode_number).padStart(2, '0')}`
  }
  if (d.target_kind === 'season' && d.season_number != null) return `Season ${d.season_number}`
  return ''
}
</script>

<template>
  <section v-if="(data?.decisions?.length ?? 0) > 0 || asyncStatus === 'loading'" class="mdp">
    <h2 class="mdp-head">Acquisition decisions</h2>
    <div v-if="asyncStatus === 'loading' && !data" class="mdp-loading"><span class="mgr-loading" /> Loading decisions…</div>
    <ul v-else class="mdp-list">
      <li v-for="d in data?.decisions ?? []" :key="d.id" class="mdp-row">
        <StatusBadge :state="VERDICT_META[d.verdict]?.state ?? 'idle'">{{ VERDICT_META[d.verdict]?.label ?? d.verdict }}</StatusBadge>
        <div class="mdp-body">
          <div class="mdp-line">
            <span v-if="unitLabel(d)" class="mdp-unit mono">{{ unitLabel(d) }}</span>
            <span v-if="d.chosen_title" class="mdp-chosen mono">{{ d.chosen_title }}</span>
            <span v-else class="mdp-none">no release selected</span>
          </div>
          <div v-if="d.chosen_title" class="mdp-facts">
            <span v-if="d.chosen_quality" class="mdp-fact mono">{{ d.chosen_quality }}</span>
            <span class="mdp-fact mono">score {{ d.chosen_score ?? 0 }}</span>
            <span v-if="d.chosen_size_bytes" class="mdp-fact mono">{{ fmtSize(d.chosen_size_bytes) }}</span>
            <template v-for="hit in ((d.chosen_breakdown as any[]) ?? [])" :key="hit.name">
              <span class="mdp-cf" :class="{ pos: (hit.score ?? 0) > 0, neg: (hit.score ?? 0) < 0 }">
                {{ hit.name }}<template v-if="hit.score"> {{ hit.score > 0 ? '+' : '' }}{{ hit.score }}</template>
              </span>
            </template>
          </div>
          <div class="mdp-sub">{{ fmtWhen(d.decided_at) }} · {{ d.run_kind }}<template v-if="d.profile_name"> · {{ d.profile_name }}</template></div>
        </div>
        <NuxtLink :to="`/manager/history?run=${d.run_id}`" class="mdp-link mono">run #{{ d.run_id }}</NuxtLink>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.mdp { margin-top: 26px; }
.mdp-head {
  font-family: var(--font-display);
  font-variation-settings: "wdth" 100;
  font-size: 17px;
  font-weight: 700;
  letter-spacing: -0.01em;
  color: var(--fg-0);
  margin: 0 0 12px;
}
.mdp-loading { display: flex; align-items: center; gap: 8px; color: var(--fg-3); font-size: 12.5px; }
.mdp-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.mdp-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.mdp-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.mdp-line { display: flex; align-items: center; gap: 8px; min-width: 0; }
.mdp-unit { font-size: 11px; color: var(--fg-2); flex-shrink: 0; }
.mdp-chosen { font-size: 12px; color: var(--fg-1); overflow-wrap: anywhere; }
.mdp-facts { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.mdp-fact { font-size: 10.5px; color: var(--fg-2); }
.mdp-cf {
  display: inline-flex;
  padding: 0 6px;
  border-radius: 999px;
  font-family: var(--font-mono);
  font-size: 9.5px;
  font-weight: 600;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  white-space: nowrap;
}
.mdp-cf.pos { color: var(--good); border-color: color-mix(in srgb, var(--good) 28%, transparent); }
.mdp-cf.neg { color: var(--bad); border-color: color-mix(in srgb, var(--bad) 28%, transparent); }
.mdp-none { font-size: 12px; color: var(--fg-3); }
.mdp-sub { font-size: 11px; color: var(--fg-3); }
.mdp-link { flex-shrink: 0; font-size: 11px; color: var(--fg-3); text-decoration: none; }
.mdp-link:hover { color: var(--gold-bright); }
.mono { font-family: var(--font-mono); }
</style>
