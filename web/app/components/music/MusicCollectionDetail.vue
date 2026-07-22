<!--
  Shared collection-detail scaffold for generated mixes, user playlists,
  and Loved Songs. The page owns only its query and collection-specific
  actions; this component owns the visual contract: hero, ledger seam,
  empty/loading state, section heading, and TrackList.
-->
<template>
  <div class="mcd">
    <MusicCollectionHero
      :kind="kind"
      :title="title"
      :description="description"
      :images="images"
      :backdrop="backdrop"
      @image="emit('image', $event)"
    >
      <template #stats><slot name="stats" /></template>
      <template #actions><slot name="actions" /></template>
    </MusicCollectionHero>

    <LedgerStrip
      v-if="ledgerCells.length || ledgerPending"
      :cells="ledgerCells"
      :pending="ledgerPending"
    />

    <div v-if="tracksPending" class="mcd-loading page-pad">
      <slot name="loading">Loading tracks…</slot>
    </div>

    <section v-else-if="!tracks.length" class="mcd-empty page-pad">
      <slot name="empty" />
    </section>

    <section v-else class="mcd-tracks page-pad">
      <header class="mcd-heading">
        <div>
          <h2>{{ tracksTitle }}</h2>
          <span v-if="tracksMeta">{{ tracksMeta }}</span>
        </div>
        <slot name="track-tools" />
      </header>

      <TrackList
        :tracks="tracks"
        :columns="columns"
        :grid-template-columns="gridTemplateColumns"
        :storage-key="storageKey"
        :context-items="contextItems"
        :active-track-id="activeTrackId"
        :playing="playing"
        :show-header="showHeader"
        :vu-meter-in="vuMeterIn"
        :display-index="displayIndex"
        :on-rating-change="onRatingChange"
        :art-play-icon-size="artPlayIconSize"
        :duration-formatter="durationFormatter"
        :virtualized="virtualized"
        @row-click="emit('row-click', $event)"
        @range="onRange"
      >
        <template #cell-remove="slotProps">
          <slot name="cell-remove" v-bind="slotProps" />
        </template>
      </TrackList>
    </section>
  </div>
</template>

<script setup lang="ts">
import type { ContextMenuItem } from '~~/shared/types'
import type { TrackListColumn, TrackListRow } from '~/utils/trackListMeta'
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'

withDefaults(defineProps<{
  kind: string
  title: string
  description?: string
  images?: string[]
  backdrop?: string | null
  ledgerCells: LedgerCell[]
  ledgerPending?: boolean
  tracks: TrackListRow[]
  tracksPending?: boolean
  tracksTitle?: string
  tracksMeta?: string
  columns: TrackListColumn[]
  gridTemplateColumns?: string
  storageKey?: string
  contextItems: (track: TrackListRow, index: number) => ContextMenuItem[]
  activeTrackId?: number | null
  playing?: boolean
  showHeader?: boolean
  vuMeterIn?: 'art' | 'title' | 'none'
  displayIndex?: (index: number) => number | string
  onRatingChange?: (id: number, value: number) => void
  artPlayIconSize?: number
  durationFormatter?: (seconds: number) => string
  virtualized?: boolean
}>(), {
  images: () => [],
  backdrop: null,
  ledgerPending: false,
  tracksPending: false,
  tracksTitle: 'Tracks',
  tracksMeta: '',
  gridTemplateColumns: '',
  activeTrackId: null,
  playing: false,
  showHeader: true,
  vuMeterIn: 'art',
  displayIndex: (i: number) => i + 1,
  artPlayIconSize: 14,
  durationFormatter: formatDuration,
  virtualized: false,
})

const emit = defineEmits<{
  image: [src: string | null]
  'row-click': [index: number]
  range: [start: number, end: number]
}>()

function onRange(start: number, end: number) {
  emit('range', start, end)
}
</script>

<style scoped>
.mcd { padding-bottom: 80px; }

.mcd-tracks { padding-top: 24px; }
.mcd-heading {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 16px;
  padding: 0 4px 14px;
}
.mcd-heading > div { min-width: 0; }
.mcd-heading h2 {
  margin: 0;
  color: var(--fg-0);
  font: 650 12px var(--font-mono);
  letter-spacing: 0.22em;
  text-transform: uppercase;
}
.mcd-heading span {
  display: block;
  margin-top: 4px;
  color: var(--tone, var(--gold));
  font: 500 10px var(--font-mono);
  letter-spacing: 0.06em;
}

.mcd-loading { color: var(--fg-3); font-size: 13px; padding-top: 40px; text-align: center; }
.mcd-empty { padding-top: 40px; }

@media (max-width: 720px) {
  .mcd-tracks { padding-top: 22px; }
  .mcd-heading { padding-inline: 2px; }
}
</style>
