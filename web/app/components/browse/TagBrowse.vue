<!--
  TagBrowse — the shared browse surface for /genre/{name} and /keyword/{name}.

  Both are flat, cross-library lists (a genre or keyword spans movies + TV),
  so they don't get the full library sidebar/FilterBar. They get the library
  *look* instead: MediaCard poster tiles, plus a lightweight control row with
  a media-type segment (only when the set is mixed) and a sort menu.

  Data is random-access paged (useVirtualCatalog + VirtualPosterGrid): the
  grid is sized to the server total up front so the page scrollbar spans the
  entire genre — grab it and jump anywhere and that page fetches on demand.
  Sorting and the type filter therefore run SERVER-side (the client never
  holds the full list); each sort/filter combination pages its own cache.
  Segment counts come from the response's type_counts, so they're exact even
  before any deep page has loaded.
-->
<template>
  <div class="scroll page-pad" style="height: 100%">
    <header class="tb-head">
      <div class="tb-eyebrow">{{ eyebrow }}</div>
      <h1 class="tb-title">{{ displayName }}</h1>
      <div v-if="!pending" class="tb-meta">
        {{ (total ?? 0).toLocaleString() }} title<span v-if="total !== 1">s</span>
      </div>
    </header>

    <div v-if="pending" class="grid-posters">
      <div v-for="i in 12" :key="i" class="grid-tile">
        <div class="poster" style="aspect-ratio: 2/3; background: var(--bg-3); animation: pulse 1.5s infinite" />
      </div>
    </div>

    <template v-else-if="(total ?? 0) > 0 || mediaFilter !== 'all'">
      <div class="tb-controls">
        <div v-if="typeSegments.length > 1" class="tb-seg">
          <button
            v-for="seg in typeSegments"
            :key="seg.value"
            :class="{ active: mediaFilter === seg.value }"
            :aria-pressed="mediaFilter === seg.value"
            @click="mediaFilter = seg.value"
          >
            {{ seg.label }}<span class="tb-seg-count">{{ seg.count }}</span>
          </button>
        </div>
        <div class="tb-controls-spacer" />
        <AppMenu trigger-class="btn-ghost-sm steer-glass" :width="200" align="end">
          <template #trigger>
            <Icon name="sort" :size="14" />
            {{ sortLabel }}
            <Icon name="chevdown" :size="10" class="tb-caret" />
          </template>
          <DropdownMenuItem
            v-for="opt in sortOptions"
            :key="opt.value"
            class="surface-item tb-sort-item"
            :class="{ active: sortMode === opt.value }"
            @select="sortMode = opt.value"
          >
            {{ opt.label }}
            <Icon v-if="sortMode === opt.value" name="check" :size="12" class="tb-sort-check" />
          </DropdownMenuItem>
        </AppMenu>
      </div>

      <VirtualPosterGrid
        v-if="(total ?? 0) > 0"
        :total="total ?? 0"
        :item-at="itemAt"
        :aspect="1.5"
        @range="ensureRange"
      >
        <template #default="{ item, index }">
          <NuxtLink :to="mediaUrl(item)" class="grid-tile card-tile">
            <MediaCard
              :idx="index"
              :src="usePosterUrl(item)"
              aspect="2/3"
              :title="item.title"
              :subtitle="subtitleFor(item)"
            />
          </NuxtLink>
        </template>
      </VirtualPosterGrid>
      <div v-else class="tb-empty">Nothing matches this filter.</div>
    </template>

    <div v-else class="tb-empty tb-empty-lg">
      No media found for this {{ kind }}.
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'
import { DropdownMenuItem } from 'reka-ui'

const props = defineProps<{
  kind: 'genre' | 'keyword'
  /** Dash-separated name straight from the route param (URL form). */
  rawName: string
}>()

// Hoisted per the useNuxtApp gotcha — never resolve $heya inside async bodies.
const { $heya } = useNuxtApp()

// Ambient background: genres/keywords span movies + TV — claim a mixed pool
// so the layer shows relevant art under the content veil (list pages have
// text at the very top; the open home scrim is too raw there).
useBackground().pool('movie', 'tv')

const PAGE = 100 // rows per random-access page (API caps limit at 200)

const mediaFilter = ref<'all' | string>('all')
const sortMode = ref<'title' | 'year-desc' | 'year-asc'>('title')
// Unfiltered per-type breakdown from the last response — segment labels stay
// exact regardless of which pages have loaded.
const typeCounts = ref<Record<string, number>>({})

// Genre/keyword names are used verbatim — the FE links via
// encodeURIComponent(exactName) and the API matches the exact string, so a
// dash in "Sci-Fi & Fantasy" is a real dash, not a space separator. Vue Router
// already decodes route params, so rawName is the exact name; don't re-decode
// (a literal '%' in a name would make decodeURIComponent throw).
const displayName = computed(() => props.rawName)
const eyebrow = computed(() => props.kind === 'genre' ? 'Genre' : 'Keyword')

const sortOptions = [
  { label: 'Title A→Z', value: 'title' as const },
  { label: 'Year (Newest)', value: 'year-desc' as const },
  { label: 'Year (Oldest)', value: 'year-asc' as const },
]
const sortLabel = computed(() => sortOptions.find(o => o.value === sortMode.value)?.label || 'Sort')

// Random-access catalog: each (tag, filter, sort) combination pages its own
// store, so flipping a control repaints from cache when it's been seen.
const { total, pending, itemAt, ensureRange } = useVirtualCatalog<MediaItem>(() => ({
  key: `tag:${props.kind}:${props.rawName}:${mediaFilter.value}:${sortMode.value}`,
  pageSize: PAGE,
  fetch: async (offset, limit) => {
    const query = {
      limit,
      offset,
      sort: sortMode.value,
      // mediaFilter's non-'all' values come from the server's type_counts
      // keys, which are always real media types — assert to the spec enum
      // (regenerated client tightened `type` from string).
      type: mediaFilter.value === 'all' ? undefined : mediaFilter.value as 'movie' | 'tv' | 'anime' | 'music' | 'book' | 'comic',
    }
    // Split by kind so each call keeps a literal path — the typed $heya
    // client can't infer params from a dynamic path string.
    const res = props.kind === 'genre'
      ? await $heya('/api/genres/{name}', { path: { name: props.rawName }, query }) as
        { items: MediaItem[]; total: number; type_counts?: Record<string, number> }
      : await $heya('/api/keywords/{name}', { path: { name: props.rawName }, query }) as
        { items: MediaItem[]; total: number; type_counts?: Record<string, number> }
    typeCounts.value = res.type_counts ?? {}
    return { items: res.items ?? [], total: res.total ?? 0 }
  },
}))

// One segment per media_type actually present, plus "All" — hidden entirely
// (via the >1 guard in the template) when the list is single-type.
const TYPE_PLURALS: Record<string, string> = { movie: 'Movies', tv: 'TV Shows', anime: 'Anime', book: 'Books', music: 'Music' }
const typeSegments = computed(() => {
  const counts = typeCounts.value
  const present = Object.keys(counts).sort()
  if (present.length <= 1) return []
  const all = present.reduce((s, t) => s + (counts[t] ?? 0), 0)
  return [
    { value: 'all', label: 'All', count: all },
    ...present.map(t => ({ value: t, label: TYPE_PLURALS[t] || mediaTypeLabel(t), count: counts[t]! })),
  ]
})

const mixedTypes = computed(() => typeSegments.value.length > 1)

function subtitleFor(item: MediaItem): string {
  // On a mixed-type list the type disambiguates; on a single-type list the
  // year alone reads cleaner.
  return mixedTypes.value ? `${item.year} · ${mediaTypeLabel(item.media_type)}` : item.year
}

// A new tag starts clean — the filter belongs to the tag being browsed.
watch(() => [props.kind, props.rawName], () => {
  mediaFilter.value = 'all'
  typeCounts.value = {}
})
</script>

<style scoped>
/* Art-proof header — same recipe as SectionHeader/RecsBrowse: a blended
   --bg-1 wash behind the text (no locatable edge) plus halo text-shadows,
   so the title holds up over whatever the ambient pool is showing. */
.tb-head {
  position: relative;
  isolation: isolate;
  margin-bottom: 20px;
}
.tb-head::before {
  content: '';
  position: absolute;
  top: -44px;
  bottom: -36px;
  left: -70px;
  width: min(56%, 560px);
  z-index: -1;
  pointer-events: none;
  background: radial-gradient(ellipse 90% 75% at 30% 50%,
    color-mix(in srgb, var(--bg-1) 55%, transparent) 0%,
    color-mix(in srgb, var(--bg-1) 38%, transparent) 40%,
    color-mix(in srgb, var(--bg-1) 16%, transparent) 68%,
    transparent 92%);
  filter: blur(24px);
}
.tb-eyebrow {
  font-size: 10px; font-family: var(--font-mono); font-weight: 700;
  letter-spacing: 0.18em; text-transform: uppercase; color: var(--gold); margin-bottom: 8px;
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1);
}
.tb-title {
  font-size: 36px; font-weight: 600; letter-spacing: -0.02em; margin: 0 0 6px;
  text-shadow:
    0 1px 2px var(--bg-1),
    0 0 10px var(--bg-1),
    0 0 24px var(--bg-1);
}
.tb-meta {
  font-size: 12px; font-family: var(--font-mono);
  /* fg-1, not the muted tiers — those wash out over bright art. */
  color: var(--fg-1);
  text-shadow: 0 1px 2px var(--bg-1), 0 0 10px var(--bg-1), 0 0 24px var(--bg-1);
}

.tb-controls { display: flex; align-items: center; gap: 10px; margin-bottom: 20px; }
.tb-controls-spacer { flex: 1; }

/* Media-type segmented control — glassed so it reads over ambient art
   (the ink-wash recipe it mirrored from FilterBar vanishes on artwork). */
.tb-seg {
  display: inline-flex; gap: 2px; padding: 2px;
  background: color-mix(in oklab, var(--bg-2) 82%, transparent);
  backdrop-filter: blur(10px);
  -webkit-backdrop-filter: blur(10px);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  box-shadow: var(--shadow-el);
}
.tb-seg button {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 5px 12px; border-radius: 4px; font-size: 12px; font-weight: 500;
  color: var(--fg-2); cursor: pointer;
  transition: background 0.12s ease, color 0.12s ease;
}
.tb-seg button:hover { color: var(--fg-0); }
.tb-seg button.active { background: var(--gold-soft); color: var(--gold-bright); }
.tb-seg-count { font-family: var(--font-mono); font-size: 10px; color: var(--fg-3); }
.tb-seg button.active .tb-seg-count { color: color-mix(in srgb, var(--gold) 70%, transparent); }

.tb-caret { opacity: 0.45; margin-left: -2px; }

.tb-empty { padding: 40px 0; text-align: center; color: var(--fg-3); font-size: 14px; }
.tb-empty-lg { padding: 60px 0; }
.tb-more { padding: 24px 0 60px; text-align: center; }

@media (max-width: 720px) {
  .page-pad { padding: 20px 16px 60px; }
  .tb-title { font-size: 26px; }
  .tb-controls { flex-wrap: wrap; }
}
</style>

<style>
/* AppMenu portals the sort items out of scoped reach (docs/ui.md). */
.tb-sort-item { justify-content: space-between; }
.tb-sort-item.active { color: var(--gold); }
.tb-sort-check { color: var(--gold); }
</style>
