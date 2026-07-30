<script setup lang="ts">
// One evaluated release, in full: the complete release name (wrapping, never
// truncated), every matched custom-format label, and the evaluation facts.
// Shared by the interactive search modal and the History expansion.
type FormatHit = { id?: number, name?: string, score?: number }
type Rejection = { code: string, message: string }

const props = defineProps<{
  title: string
  indexer: string
  quality?: string
  score: number
  sizeBytes: number
  publishDate?: string
  breakdown?: FormatHit[]
  rejections?: Rejection[]
  acceptable: boolean
  chosen: boolean
  selectionRank?: number
}>()

const rankLabel = computed(() => {
  if (props.chosen) return '★'
  if (props.acceptable && props.selectionRank) return `#${props.selectionRank}`
  return ''
})

function fmtSize(bytes: number): string {
  if (bytes >= 1024 ** 3) return `${(bytes / 1024 ** 3).toFixed(1)} GB`
  if (bytes >= 1024 ** 2) return `${(bytes / 1024 ** 2).toFixed(0)} MB`
  return bytes > 0 ? `${bytes} B` : '—'
}

const age = computed(() => {
  if (!props.publishDate) return ''
  const hours = (Date.now() - new Date(props.publishDate).getTime()) / 36e5
  if (hours < 1) return '<1h'
  if (hours < 24) return `${Math.round(hours)}h`
  return `${Math.round(hours / 24)}d`
})

const labels = computed(() => (props.breakdown ?? []).filter(hit => hit.name))
</script>

<template>
  <div class="mcr" :class="{ chosen, rejected: !acceptable }">
    <span class="mcr-rank" :class="{ gold: chosen }">{{ rankLabel }}</span>
    <div class="mcr-body">
      <div class="mcr-title mono">{{ title }}</div>
      <div class="mcr-meta">
        <span class="mcr-fact">{{ indexer }}</span>
        <span v-if="quality" class="mcr-fact mono">{{ quality }}</span>
        <span class="mcr-fact mono">score {{ score }}</span>
        <span class="mcr-fact mono">{{ fmtSize(sizeBytes) }}</span>
        <span v-if="age" class="mcr-fact mono">{{ age }}</span>
        <span v-if="acceptable" class="mcr-ok"><Icon name="check" :size="11" /> acceptable</span>
      </div>
      <div v-if="labels.length" class="mcr-labels">
        <span
          v-for="hit in labels" :key="hit.id ?? hit.name" class="mcr-label"
          :class="{ pos: (hit.score ?? 0) > 0, neg: (hit.score ?? 0) < 0 }"
        >
          {{ hit.name }}<template v-if="hit.score"> {{ hit.score! > 0 ? '+' : '' }}{{ hit.score }}</template>
        </span>
      </div>
      <div v-if="!acceptable && rejections?.length" class="mcr-rejections">
        <span v-for="rej in rejections" :key="rej.code + rej.message" class="mcr-rej">
          <span class="mcr-rej-code mono">{{ rej.code }}</span> {{ rej.message }}
        </span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.mcr {
  display: flex;
  gap: 10px;
  padding: 9px 12px;
  border-bottom: 1px solid var(--hair);
}
.mcr:last-child { border-bottom: 0; }
.mcr.chosen { background: var(--gold-soft); }
.mcr.rejected { opacity: 0.68; }

.mcr-rank {
  flex-shrink: 0;
  width: 30px;
  text-align: center;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-2);
  padding-top: 1px;
}
.mcr-rank.gold { color: var(--gold-bright); }

.mcr-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
.mcr-title {
  font-size: 12px;
  color: var(--fg-0);
  overflow-wrap: anywhere;
  line-height: 1.45;
}
.mcr-meta { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.mcr-fact { font-size: 11px; color: var(--fg-2); }
.mcr-ok { display: inline-flex; align-items: center; gap: 4px; color: var(--good); font-size: 11px; }

.mcr-labels { display: flex; gap: 5px; flex-wrap: wrap; }
.mcr-label {
  display: inline-flex;
  padding: 1px 7px;
  border-radius: 999px;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  background: rgb(var(--ink) / 0.05);
  border: 1px solid var(--border);
  color: var(--fg-2);
  white-space: nowrap;
}
.mcr-label.pos {
  color: var(--good);
  background: color-mix(in srgb, var(--good) 9%, transparent);
  border-color: color-mix(in srgb, var(--good) 28%, transparent);
}
.mcr-label.neg {
  color: var(--bad);
  background: color-mix(in srgb, var(--bad) 9%, transparent);
  border-color: color-mix(in srgb, var(--bad) 28%, transparent);
}

.mcr-rejections { display: flex; flex-direction: column; gap: 2px; }
.mcr-rej { font-size: 11px; color: var(--fg-3); overflow-wrap: anywhere; }
.mcr-rej-code { color: var(--bad); font-size: 10px; }
.mono { font-family: var(--font-mono); }
</style>
