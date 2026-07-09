<!--
  TrackList — shared desktop track table + phone touch-row, extracted from
  four hand-rolled tables (music/songs.vue, music/loved.vue,
  music/my/favorites.vue, music/browse/[kind]/[key].vue — see docs/ui.md
  "Shared App primitives" / docs/responsive-plan.md W2a).

  Dumb by design: no data fetching, no query state. The page owns its query,
  maps its rows to `TrackListRow`, and hands them down along with a
  `columns` config that reproduces its EXACT current desktop grid (same
  `gridTemplateColumns` string, same column set/order) — that's the pixel-
  parity contract. Structural differences between the four legacy tables
  (art column vs inline art, linked vs plain artist/album text, sticky vs
  static header, presence of a rating/year column) are covered by column
  `kind`/flags below. The handful of pure numeric deltas that don't fit a
  flag (art box size, index font size, header padding, hover tint, radius)
  are expected to be layered on by the consuming page via a small scoped
  `:deep(.tl-...)` override block — see music/loved.vue, music/my/favorites.vue,
  music/browse/[kind]/[key].vue for the pattern. TrackList's own baseline CSS
  matches music/songs.vue (the reference implementation).

  Phone (<=720px, via useViewport().isPhone): each row collapses to a 2-line
  touch layout — 44px art (or the display index, if the page has no artwork
  at all) — title over "artist · album" as plain text (never nested links;
  a nested <a> inside a tappable row stole the row's own tap target at phone
  width, a bug found in W1c testing) — duration — and an always-visible 44px
  "..." button that opens the same context-menu items in an ActionSheet.
  Row tap (anywhere but the button) emits `row-click`. Long-press still opens
  the AppContextMenu popper — reka's ContextMenuTrigger already wraps the
  whole row regardless of which layout is showing.

  W2c additions: the `cell-${key}` named slot for `kind: 'custom'` columns
  (arbitrary per-cell content — e.g. the album page's TrackQualityPicker) and
  the optional `#row-before` slot (content rendered before a given row — the
  album page's disc-boundary markers) both pre-date W2c's own edits and are
  additive: neither is used by songs/loved/favorites/browse, so they render
  as no-ops there. Several W2c pages also keep their own hand-rolled header
  row and pass `:show-header="false"` rather than fight TrackList's sticky,
  solid-background header for pixel parity with a pre-existing layout — see
  music/artist/[slug]/[album].vue and music/mix/[slug].vue.
-->
<template>
  <div class="tl">
    <div
      v-if="showHeader && !isPhone"
      class="tl-row tl-head"
      :style="{ gridTemplateColumns }"
    >
      <div
        v-for="col in columns"
        :key="col.key"
        class="tl-cell"
        :class="`tl-c-${col.kind}`"
      >
        <Icon v-if="col.headerIcon" :name="col.headerIcon" :size="13" />
        <template v-else>{{ col.label }}</template>
      </div>
    </div>

    <div v-if="!virtualized" class="tl-body">
      <template v-for="(t, i) in tracks" :key="t.id">
        <!-- Optional escape hatch (W2c) for content between rows — e.g. the
             album page's disc-boundary markers. Unused by every other
             consumer; renders nothing when the slot isn't provided. -->
        <slot name="row-before" :track="t" :index="i" />
        <TrackListItem
          :track="t"
          :index="i"
          :columns="columns"
          :grid-template-columns="gridTemplateColumns"
          :context-items="contextItems"
          :active="isActive(t)"
          :playing="playing"
          :is-phone="isPhone"
          :is-coarse="isCoarse"
          :has-art="hasArt"
          :vu-meter-in="vuMeterIn"
          :display-index="displayIndex"
          :on-rating-change="onRatingChange"
          :art-play-icon-size="artPlayIconSize"
          :duration-formatter="durationFormatter"
          :on-drag-start="onDragStart"
          :on-drag-end="onDragEnd"
          @row-click="emit('row-click', $event)"
          @open-sheet="openSheet"
        >
          <template v-for="col in columns" :key="col.key" #[`cell-${col.key}`]="slotProps">
            <slot :name="`cell-${col.key}`" v-bind="slotProps" />
          </template>
        </TrackListItem>
      </template>
    </div>

    <RecycleScroller
      v-else
      class="tl-body tl-virtual"
      :items="tracks"
      :item-size="isPhone ? 65 : 61"
      :buffer="610"
      key-field="id"
      page-mode
      v-slot="{ item: t, index: i }"
    >
      <TrackListItem
        :track="t"
        :index="i"
        :columns="columns"
        :grid-template-columns="gridTemplateColumns"
        :context-items="contextItems"
        :active="isActive(t)"
        :playing="playing"
        :is-phone="isPhone"
        :is-coarse="isCoarse"
        :has-art="hasArt"
        :vu-meter-in="vuMeterIn"
        :display-index="displayIndex"
        :on-rating-change="onRatingChange"
        :art-play-icon-size="artPlayIconSize"
        :duration-formatter="durationFormatter"
        :on-drag-start="onDragStart"
        :on-drag-end="onDragEnd"
        @row-click="emit('row-click', $event)"
        @open-sheet="openSheet"
      >
        <template v-for="col in columns" :key="col.key" #[`cell-${col.key}`]="slotProps">
          <slot :name="`cell-${col.key}`" v-bind="slotProps" />
        </template>
      </TrackListItem>
    </RecycleScroller>

    <ActionSheet
      v-model:open="sheetOpen"
      :items="sheetTrack ? contextItems(sheetTrack, sheetIndex) : []"
      :title="sheetTrack?.title"
    />
  </div>
</template>

<script setup lang="ts">
import type { ContextMenuItem } from '~~/shared/types'

// Minimal row shape TrackList needs. Pages map their (differently-shaped)
// query rows into this before handing them down — see the `tlRows`
// computed in each migrated page for the adapter.
export interface TrackListRow {
  id: number
  title: string
  artist: string
  artist_slug?: string
  album: string
  album_slug?: string
  album_year?: string | number | null
  duration: number
  /** false = file removed from disk; row dims and stops accepting clicks. */
  available?: boolean
  poster?: string | null
  /** 0..10 half-star scale; only rendered when a 'rating' column is present. */
  rating?: number
  /** Phone-only quality label ("FLAC 24/96", "MP3 320") — see utils/trackQuality.ts.
   *  Rendered under the duration in the phone row; omitted entirely when absent. */
  quality?: string | null
}

export type TrackListColumnKind =
  | 'index' | 'art' | 'title' | 'album' | 'year' | 'rating' | 'duration' | 'custom'

export interface TrackListColumn {
  key: string
  kind: TrackListColumnKind
  /** Header cell text. Ignored when `headerIcon` is set. */
  label?: string
  /** Header cell renders this icon instead of `label` (songs.vue's clock-over-duration). */
  headerIcon?: string
  /** 'title' only — render a thumb ahead of the text (browse's combined art+title cell). */
  inlineArt?: boolean
  inlineArtSize?: number
  /** 'title' only — how the line under the title renders. */
  subtitle?: 'artist-link' | 'artist-plain' | 'artist-album-year' | 'none'
  /** 'album' only — NuxtLink (default) vs plain text. */
  linkAlbum?: boolean
}

const props = withDefaults(defineProps<{
  tracks: TrackListRow[]
  columns: TrackListColumn[]
  /** Verbatim `grid-template-columns` value from the page being migrated. */
  gridTemplateColumns: string
  contextItems: (track: TrackListRow, index: number) => ContextMenuItem[]
  /** Drives `.tl-active`/`.playing` tint + the VU meter swap. Omit (or leave
   *  null) to never highlight a row — matches pages that don't track it today. */
  activeTrackId?: number | null
  playing?: boolean
  showHeader?: boolean
  /** Which column swaps in the VU meter for the active row. 'none' (default)
   *  matches pages that only tint text/background. */
  vuMeterIn?: 'art' | 'title' | 'none'
  displayIndex?: (index: number) => number | string
  onRatingChange?: (id: number, value: number) => void
  /** Icon size for the art column's hover-play / missing glyph. */
  artPlayIconSize?: number
  /** Defaults to the global m:ss formatter; pass usePlayer().formatTime for
   *  pages that used it today (adds h:mm:ss past an hour, "0:00" for 0). */
  durationFormatter?: (seconds: number) => string
  /** Window rows against the nearest scroll container. Intended for lists
   *  with hundreds of fixed-height rows; small/detail lists stay native. */
  virtualized?: boolean
}>(), {
  activeTrackId: null,
  playing: false,
  showHeader: true,
  vuMeterIn: 'none',
  // NOTE: Function-typed props are compiler-inferred as runtime `type:
  // Function`, so Vue's resolvePropValue uses this value AS-IS instead of
  // calling it as a factory (that factory-call path is only taken when
  // `opt.type !== Function`) — do not wrap these in an extra `() => ...`.
  displayIndex: (i: number) => i + 1,
  artPlayIconSize: 14,
  durationFormatter: formatDuration,
  virtualized: false,
})

const emit = defineEmits<{ 'row-click': [index: number] }>()

const { isPhone, isCoarse } = useViewport()
const { onDragStart, onDragEnd } = useMusicDragDrop()

const hasArt = computed(() => props.columns.some((c) => c.kind === 'art' || (c.kind === 'title' && c.inlineArt)))

function isActive(t: TrackListRow) {
  return props.activeTrackId != null && t.id === props.activeTrackId
}

// --- Phone action sheet ------------------------------------------------
const sheetOpen = ref(false)
const sheetTrack = ref<TrackListRow | null>(null)
const sheetIndex = ref(-1)

function openSheet(t: TrackListRow, i: number) {
  sheetTrack.value = t
  sheetIndex.value = i
  sheetOpen.value = true
}
</script>

<style>
/* Baseline matches music/songs.vue exactly — it's the reference
   implementation. The other migrated pages layer a small `:deep()`
   override block on top for their numeric deltas (art size, index font
   size, hover tint, header sticky-ness, ...). */

.tl {

.tl-row {
  display: grid;
  column-gap: 12px;
  align-items: center;
}
.tl-head {
  position: sticky;
  top: 0;
  z-index: 4;
  padding: 8px 10px;
  background: var(--bg-1);
  color: var(--fg-3);
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  border-bottom: 1px solid var(--border);
}

.tl-body { display: flex; flex-direction: column; gap: 1px; }
.tl-virtual { display: block; overflow: visible; }

.tl-track {
  padding: 6px 10px;
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.15s;
}
.tl-track:hover { background: rgba(255, 255, 255, 0.04); }
/* `.tl-body` qualifier bumps specificity to (0,4,0) — strictly more than any
   page-level `:deep(.tl-track:hover)` override, which compiles to a
   descendant selector at (0,3,0) (`[data-v-x] .tl-track:hover`). Without
   this, a page override and this rule tie on specificity and whichever
   stylesheet loads later wins, which can make a still-hovered active row
   lose its gold tint (found while migrating music/browse/[kind]/[key].vue). */
.tl-body .tl-track.tl-active { background: var(--gold-soft); }
.tl-track.tl-active .tl-title { color: var(--gold); }
.tl-track.tl-active .tl-c-index { color: var(--gold); }
.tl-track.tl-missing { opacity: 0.5; cursor: default; }
.tl-track.tl-missing:hover { background: transparent; }

/* Drag affordance — fine-pointer only, doesn't touch the resting cursor on
   touch devices (which don't drag; long-press opens the context menu). */
@media (pointer: fine) {
  .tl-track:not(.tl-missing) { cursor: grab; }
  .tl-track:not(.tl-missing):active { cursor: grabbing; }
}

.tl-c-index { text-align: right; color: var(--fg-3); font-family: var(--font-mono); font-size: 12px; }
.tl-c-year { color: var(--fg-3); font-family: var(--font-mono); font-size: 12px; }
.tl-c-duration { text-align: right; color: var(--fg-3); font-family: var(--font-mono); font-size: 12px; }
.tl-c-rating { display: flex; align-items: center; }

.tl-c-art {
  width: 48px;
  height: 48px;
  position: relative;
  border-radius: 4px;
  overflow: hidden;
  background: var(--bg-3);
  justify-self: center;
}
.tl-c-art img { width: 100%; height: 100%; object-fit: cover; display: block; }
.tl-art-play {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  opacity: 0;
  transition: opacity 0.15s;
}
.tl-track:hover .tl-art-play { opacity: 1; }
.tl-art-missing {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0, 0, 0, 0.55);
  color: #d96b6b;
}

.tl-c-title { min-width: 0; }
.tl-title-inline-art { display: flex; align-items: center; gap: 12px; }
.tl-title-thumb { border-radius: 4px; flex-shrink: 0; }
.tl-title-text { min-width: 0; }
.tl-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--fg-0);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.tl-artist { color: var(--fg-3); }
.tl-artist-link {
  font-size: 12px;
  text-decoration: none;
  display: inline-block;
  margin-top: 1px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  max-width: 100%;
}
.tl-artist-link:hover { color: var(--fg-1); text-decoration: underline; }
.tl-artist-plain { font-size: 11px; margin-top: 2px; }
.tl-artist-combo {
  font-size: 12px;
  margin-top: 2px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

.tl-c-album { min-width: 0; }
.tl-album-link {
  font-size: 13px;
  color: var(--fg-2);
  text-decoration: none;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  display: block;
}
.tl-album-link:hover { color: var(--fg-0); text-decoration: underline; }
.tl-album-plain { cursor: default; }

/* ── Phone (<=720px) ─────────────────────────────────────────────────
   Structural (branches on isPhone in JS, not CSS, so hidden art/desktop
   markup never mounts and never fetches images it won't show) — see
   docs/responsive-plan.md W2a. */
.tl-phone-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 8px;
}
.tl-phone-thumb {
  width: 44px; height: 44px;
  flex-shrink: 0;
  border-radius: 4px;
  overflow: hidden;
  background: var(--bg-3);
  display: flex; align-items: center; justify-content: center;
}
.tl-phone-thumb img { width: 100%; height: 100%; object-fit: cover; display: block; }
.tl-phone-idx { font-family: var(--font-mono); font-size: 13px; color: var(--fg-3); }
.tl-phone-main { flex: 1; min-width: 0; }
.tl-phone-title { font-size: 14px; }
.tl-phone-sub {
  font-size: 12px;
  color: var(--fg-3);
  margin-top: 2px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.tl-phone-right {
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 2px;
}
.tl-phone-dur {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--fg-3);
}
.tl-phone-quality {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--fg-3);
  letter-spacing: 0.02em;
}
.tl-phone-more {
  flex-shrink: 0;
  width: 44px; height: 44px;
  display: flex; align-items: center; justify-content: center;
  background: transparent;
  border: 0;
  border-radius: var(--r-sm);
  color: var(--fg-2);
  cursor: pointer;
}
.tl-phone-more:active { background: rgba(255, 255, 255, 0.06); color: var(--fg-0); }
}
</style>
