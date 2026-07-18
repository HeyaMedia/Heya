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
      :style="{ gridTemplateColumns: effectiveGrid }"
    >
      <div
        v-for="col in visibleColumns"
        :key="col.key"
        class="tl-cell"
        :class="[`tl-c-${col.kind}`, {
          'tl-align-right': col.align === 'right',
          'tl-sortable': col.sortable && sortEnabled,
          'tl-sorted': sortKey === col.key,
        }]"
        @click="onHeaderClick(col)"
      >
        <Icon v-if="col.headerIcon" :name="col.headerIcon" :size="13" />
        <template v-else>{{ col.label }}</template>
        <Icon v-if="sortKey === col.key" :name="sortDir === 'asc' ? 'chevup' : 'chevdown'" :size="10" class="tl-sort-arrow" />
      </div>
      <div v-if="pickerEnabled" class="tl-cell tl-c-picker">
        <AppMenu trigger-class="tl-picker-btn" trigger-title="Choose columns" trigger-aria-label="Choose columns">
          <template #trigger><Icon name="eq" :size="13" /></template>
          <div class="tl-picker-heading">Columns</div>
          <div class="tl-picker-list">
            <DropdownMenuItem
              v-for="col in optionalColumns"
              :key="col.key"
              class="surface-item tl-picker-item"
              @select.prevent="toggleColumn(col.key)"
            >
              <span class="tl-picker-check" :class="{ on: isColumnOn(col) }">
                <Icon name="check" :size="11" />
              </span>
              {{ col.label }}
            </DropdownMenuItem>
          </div>
        </AppMenu>
      </div>
    </div>

    <div v-if="!virtualized" class="tl-body">
      <template v-for="(t, i) in displayRows" :key="t.id">
        <!-- Optional escape hatch (W2c) for content between rows — e.g. the
             album page's disc-boundary markers. Unused by every other
             consumer; renders nothing when the slot isn't provided. -->
        <slot name="row-before" :track="t" :index="i" />
        <TrackListItem
          :track="t"
          :index="i"
          :columns="visibleColumns"
          :grid-template-columns="effectiveGrid"
          :context-items="contextItemsAt"
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
          @row-click="emit('row-click', origIdx(t, $event))"
          @open-sheet="openSheet"
        >
          <template v-for="col in columns" :key="col.key" #[`cell-${col.key}`]="slotProps">
            <slot :name="`cell-${col.key}`" v-bind="slotProps" :index="origIdx(slotProps.track, slotProps.index)" />
          </template>
        </TrackListItem>
      </template>
    </div>

    <RecycleScroller
      v-else
      class="tl-body tl-virtual"
      :items="displayRows"
      :item-size="isPhone ? 65 : 61"
      :buffer="610"
      key-field="id"
      page-mode
      emit-update
      @update="onScrollerUpdate"
      v-slot="{ item: t, index: i }"
    >
      <!-- Sparse (random-access paged) lists hand down placeholder rows for
           the stretches the scrollbar hasn't visited yet — hold the row
           footprint with a shimmer instead of mounting a dead TrackListItem. -->
      <div v-if="t.pending" class="tl-row tl-track tl-pending" :style="!isPhone ? { gridTemplateColumns: effectiveGrid } : undefined">
        <div class="tl-skel-bar" />
      </div>
      <TrackListItem
        v-else
        :track="t"
        :index="i"
        :columns="visibleColumns"
        :grid-template-columns="effectiveGrid"
        :context-items="contextItemsAt"
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
        @row-click="emit('row-click', origIdx(t, $event))"
        @open-sheet="openSheet"
      >
        <template v-for="col in columns" :key="col.key" #[`cell-${col.key}`]="slotProps">
          <slot :name="`cell-${col.key}`" v-bind="slotProps" :index="origIdx(slotProps.track, slotProps.index)" />
        </template>
      </TrackListItem>
    </RecycleScroller>

    <ActionSheet
      v-model:open="sheetOpen"
      :items="sheetTrack ? contextItemsAt(sheetTrack, sheetIndex) : []"
      :title="sheetTrack?.title"
    />
  </div>
</template>

<script setup lang="ts">
import { DropdownMenuItem } from 'reka-ui'
import type { ContextMenuItem } from '~~/shared/types'

// Row/column types live in utils/trackListMeta.ts (a plain .ts module —
// raw tsc can't resolve type exports from an SFC); re-exported here so
// consuming pages keep their existing import path.
import type { TrackListColumn, TrackListRow } from '~/utils/trackListMeta'

export type { TrackListColumn, TrackListColumnKind, TrackListRow } from '~/utils/trackListMeta'

const props = withDefaults(defineProps<{
  tracks: TrackListRow[]
  columns: TrackListColumn[]
  /** Verbatim `grid-template-columns` value from the page being migrated.
   *  Ignored (and omissible) when every column carries a `width` — the grid
   *  is then computed from the currently-visible column set. */
  gridTemplateColumns?: string
  /** Enables the column picker: optional columns become user-toggleable and
   *  the visible set persists in localStorage under this key. */
  storageKey?: string
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
  /** Defaults to the global m:ss formatter; pass usePlayerBindings().formatTime for
   *  pages that used it today (adds h:mm:ss past an hour, "0:00" for 0). */
  durationFormatter?: (seconds: number) => string
  /** Window rows against the nearest scroll container. Intended for lists
   *  with hundreds of fixed-height rows; small/detail lists stay native. */
  virtualized?: boolean
}>(), {
  gridTemplateColumns: '',
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

const emit = defineEmits<{
  'row-click': [index: number]
  /** Rendered row range (buffer included) from the virtualized scroller —
   *  sparse pages wire this to useVirtualCatalog.ensureRange. */
  'range': [start: number, end: number]
}>()

const { isPhone, isCoarse } = useViewport()
const { onDragStart, onDragEnd } = useMusicDragDrop()

function onScrollerUpdate(start: number, end: number) {
  emit('range', start, end)
}

// ── Column picker ────────────────────────────────────────────────────
// Optional columns toggle through an AppMenu in the header. The preference
// is SITE-WIDE: one global key stores the user's explicit on/off DELTAS
// (not the absolute set), so per-page defaults still apply where a page
// declares them (playlist's "Added", loved's "Loved") while a column
// enabled on any list follows the user to every other list. Defaults
// render on both SSR and first client paint; the stored preference applies
// onMounted (localStorage isn't available server-side, and reading it
// during hydration would mismatch).
const TL_COLS_KEY = 'heya:tl-cols'
const pickerEnabled = computed(() => !!props.storageKey && !isPhone.value)
const optionalColumns = computed(() => props.columns.filter((c) => c.optional))
const userOn = ref<Set<string>>(new Set())
const userOff = ref<Set<string>>(new Set())
onMounted(() => {
  try {
    const saved = localStorage.getItem(TL_COLS_KEY)
    if (saved) {
      const { on, off } = JSON.parse(saved) as { on?: string[]; off?: string[] }
      userOn.value = new Set(on ?? [])
      userOff.value = new Set(off ?? [])
    }
  } catch { /* corrupt entry — keep defaults */ }
})
function isColumnOn(col: TrackListColumn): boolean {
  if (!col.optional) return true
  if (userOff.value.has(col.key)) return false
  return userOn.value.has(col.key) || !!col.defaultOn
}
function toggleColumn(key: string) {
  const col = props.columns.find((c) => c.key === key)
  if (!col) return
  const on = new Set(userOn.value)
  const off = new Set(userOff.value)
  if (isColumnOn(col)) {
    on.delete(key)
    off.add(key)
  } else {
    off.delete(key)
    on.add(key)
  }
  userOn.value = on
  userOff.value = off
  try { localStorage.setItem(TL_COLS_KEY, JSON.stringify({ on: [...on], off: [...off] })) } catch { /* quota */ }
}

const visibleColumns = computed(() => props.columns.filter(isColumnOn))

// ── Sorting ──────────────────────────────────────────────────────────
// Client-side, header-click, asc → desc → off. Only offered when every row
// is materialized — sorting a sparse (random-access paged) list would order
// the loaded islands and lie about the rest. Events and contextItems keep
// receiving ORIGINAL indexes (pages resolve rows by index into their own
// source arrays), so sorted views translate through an id → index map.
const sortKey = ref<string | null>(null)
const sortDir = ref<'asc' | 'desc'>('asc')
const hasPending = computed(() => props.tracks.some((t) => t.pending))
const sortEnabled = computed(() => !hasPending.value)

onMounted(() => {
  if (!props.storageKey) return
  try {
    const saved = localStorage.getItem(`heya:tl-sort:${props.storageKey}`)
    if (saved) {
      const { key, dir } = JSON.parse(saved) as { key: string | null; dir: 'asc' | 'desc' }
      sortKey.value = key
      sortDir.value = dir === 'desc' ? 'desc' : 'asc'
    }
  } catch { /* corrupt entry — keep defaults */ }
})

function onHeaderClick(col: TrackListColumn) {
  if (!col.sortable || !sortEnabled.value) return
  if (sortKey.value !== col.key) {
    sortKey.value = col.key
    sortDir.value = 'asc'
  } else if (sortDir.value === 'asc') {
    sortDir.value = 'desc'
  } else {
    sortKey.value = null
    sortDir.value = 'asc'
  }
  if (props.storageKey) {
    try { localStorage.setItem(`heya:tl-sort:${props.storageKey}`, JSON.stringify({ key: sortKey.value, dir: sortDir.value })) } catch { /* quota */ }
  }
}

// Built-in kinds sort on their natural field; meta columns bring their own
// sortValue from the registry.
function kindSortValue(col: TrackListColumn): ((r: TrackListRow) => string | number | null | undefined) | null {
  switch (col.kind) {
    case 'title': return (r) => r.title
    case 'artist': return (r) => r.artist
    case 'album': return (r) => r.album
    case 'year': return (r) => Number(r.album_year) || null
    case 'rating': return (r) => r.rating ?? 0
    case 'duration': return (r) => r.duration
    default: return null
  }
}

const displayRows = computed<TrackListRow[]>(() => {
  const key = sortKey.value
  if (!key || !sortEnabled.value) return props.tracks
  const col = props.columns.find((c) => c.key === key)
  const sv = col ? (col.sortValue ?? kindSortValue(col)) : null
  if (!col || !sv) return props.tracks
  const dir = sortDir.value === 'desc' ? -1 : 1
  return [...props.tracks].sort((a, b) => {
    const av = sv(a)
    const bv = sv(b)
    // Absent values sink to the bottom in either direction.
    if (av == null || av === '') return bv == null || bv === '' ? 0 : 1
    if (bv == null || bv === '') return -1
    if (typeof av === 'number' && typeof bv === 'number') return (av - bv) * dir
    return String(av).localeCompare(String(bv), undefined, { sensitivity: 'base', numeric: true }) * dir
  })
})

// Original index per row id — events/contextItems speak the page's indexes.
const originalIndex = computed(() => {
  const m = new Map<number, number>()
  props.tracks.forEach((t, i) => m.set(t.id, i))
  return m
})
const origIdx = (t: TrackListRow, fallback: number) => originalIndex.value.get(t.id) ?? fallback
// Wrapped builder handed to rows: translates the visual index back to the
// page's original index before delegating.
function contextItemsAt(track: TrackListRow, visualIndex: number) {
  return props.contextItems(track, origIdx(track, visualIndex))
}

// Grid: computed from the visible columns' widths when every column has one
// (picker mode); the page's literal string otherwise. The trailing 28px
// track hosts the picker button in the header — body rows auto-place their
// cells into the leading tracks and leave it empty, so alignment holds.
const effectiveGrid = computed(() => {
  const cols = visibleColumns.value
  const base = cols.every((c) => c.width) && cols.length
    ? cols.map((c) => c.width!).join(' ')
    : props.gridTemplateColumns
  return pickerEnabled.value ? `${base} 28px` : base
})

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
/* Glass surface: every consumer sits over the music shell's rotating
   ambient pool (or a detail page's claimed art), and bare rows painted
   straight onto bright artwork washed out — the album/songs pages were
   near-unreadable in light mode. One panel here fixes all of them. */
background: color-mix(in oklab, var(--bg-2) 76%, transparent);
backdrop-filter: blur(10px);
-webkit-backdrop-filter: blur(10px);
border-radius: var(--r-lg);
box-shadow: var(--shadow-el);
padding: 4px 6px 8px;

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
  /* Near-opaque (not blurred: a nested backdrop-filter under .tl's own
     one renders ~30% opaque — docs/ui.md gotcha #4) so rows scrolling
     beneath the stuck header stay masked. */
  background: color-mix(in srgb, var(--bg-1) 92%, transparent);
  color: var(--fg-2);
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
.tl-track:hover { background: rgb(var(--ink) / 0.04); }
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

/* Sparse-list placeholder row: one shimmer bar holding the title slot. */
.tl-pending { cursor: default; display: flex; align-items: center; height: 100%; }
.tl-pending:hover { background: transparent; }
.tl-skel-bar {
  height: 12px;
  width: min(46%, 320px);
  border-radius: 6px;
  background: rgb(var(--ink) / 0.08);
  animation: tl-skel-pulse 1.5s ease-in-out infinite;
}
@keyframes tl-skel-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.45; }
}

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
.tl-c-meta {
  color: var(--fg-3);
  font-family: var(--font-mono);
  font-size: 11.5px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  min-width: 0;
}
.tl-align-right { text-align: right; }

/* Sortable headers — pointer + hover ink; the active column carries a
   direction arrow inline after its label. */
.tl-head .tl-cell.tl-sortable { cursor: pointer; user-select: none; }
.tl-head .tl-cell.tl-sortable:hover { color: var(--fg-0); }
.tl-head .tl-cell.tl-sorted { color: var(--gold); }
.tl-sort-arrow { vertical-align: -1px; margin-left: 3px; }

/* Column picker — trailing 28px header track. The button is header-only;
   body rows leave the track empty so column alignment holds. */
.tl-c-picker { display: flex; justify-content: flex-end; }

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
  background: rgba(0, 0, 0, 0.55); /* on artwork — stays literal */
  color: #fff; /* on artwork — stays literal */
  opacity: 0;
  transition: opacity 0.15s;
}
.tl-track:hover .tl-art-play { opacity: 1; }
.tl-art-missing {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  background: rgba(0, 0, 0, 0.55); /* on artwork — stays literal */
  color: var(--bad);
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
/* Credit tail past the primary artist (" feat. B") — dimmer than the link text. */
.tl-feat { color: var(--fg-3); opacity: 0.85; }
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
.tl-phone-more:active { background: rgb(var(--ink) / 0.06); color: var(--fg-0); }

.tl-picker-btn {
  width: 24px; height: 24px;
  display: inline-flex; align-items: center; justify-content: center;
  background: transparent;
  border: 0;
  border-radius: var(--r-sm);
  color: var(--fg-3);
  cursor: pointer;
  transition: color 0.15s, background 0.15s;
}
.tl-picker-btn:hover,
.tl-picker-btn[data-state="open"] { background: rgb(var(--ink) / 0.06); color: var(--fg-0); }
}

/* Picker menu content is portaled out of .tl (docs/ui.md gotcha #2) — these
   stay top-level. */
.tl-picker-heading {
  padding: 6px 10px 4px;
  font: 600 10px var(--font-mono);
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: var(--fg-3);
}
.tl-picker-item { gap: 8px; }
/* 19 optional columns overflow shorter viewports — scroll the list, keep
   the heading pinned. */
.tl-picker-list { max-height: min(56vh, 460px); overflow-y: auto; }
.tl-picker-check {
  width: 15px; height: 15px;
  display: inline-flex; align-items: center; justify-content: center;
  border-radius: 4px;
  border: 1px solid var(--border-strong);
  color: transparent;
  flex-shrink: 0;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.tl-picker-check.on {
  background: var(--gold);
  border-color: var(--gold);
  color: var(--bg-0);
}
</style>
