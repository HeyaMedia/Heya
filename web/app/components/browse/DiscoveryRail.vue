<!--
  DiscoveryRail — one server-ranked rail on a Movies/TV browse landing
  ("Top Unwatched", "Starring X", "More Horror", …). The bundle endpoint
  (/api/me/recommended/{section}) supplies the 24-item head; the first
  load-more flips `started` and the per-rail pager endpoint continues from
  that offset, so rails scroll on for as long as the query has rows.
-->
<template>
  <ContentRow
    :title="rail.title"
    :memory-key="rail.key"
    :subtitle="rail.subtitle"
    :items="allItems"
    :context-items="contextItems"
    :has-more="hasMore"
    :loading-more="loadingMore"
    :more="showAllTo ? 'Show all' : undefined"
    @tile="$emit('tile', $event)"
    @more="showAllTo && navigateTo(showAllTo)"
    @load-more="onLoadMore"
  />
</template>

<script setup lang="ts">
import type { ContextMenuItem, MediaItem } from '~~/shared/types'
import { useInfiniteQuery } from '@pinia/colada'
import { DISCOVERY_PAGE, discoveryRailInfinite, type Rail, type RailItem } from '~/queries/rails'

const props = defineProps<{
  section: 'movie' | 'tv'
  rail: Rail
  contextItems?: (item: MediaItem) => ContextMenuItem[]
}>()

defineEmits<{ tile: [item: MediaItem] }>()

// The pager stays dormant (enabled: false) until the user actually reaches
// the end of the bundle head — most rails never get scrolled that deep and
// shouldn't cost a request.
const started = ref(false)
const pager = useInfiniteQuery(() => ({
  ...discoveryRailInfinite({
    section: props.section,
    railKey: props.rail.key,
    baseline: props.rail.baseline,
    baselineId: props.rail.baseline_id,
    startOffset: props.rail.items.length,
  }),
  enabled: started.value,
}))

// Head (bundle) + pager pages, deduped by id — a live-refresh refetch can
// shift the ranking between the two sources and a repeated tile would break
// the v-for key.
const allItems = computed<MediaItem[]>(() => {
  const out: RailItem[] = [...props.rail.items]
  const seen = new Set(out.map(i => i.id))
  for (const page of pager.data.value?.pages ?? []) {
    for (const it of page.items) {
      if (seen.has(it.id)) continue
      seen.add(it.id)
      out.push(it)
    }
  }
  return out as unknown as MediaItem[]
})

const loadingMore = computed(() => pager.asyncStatus.value === 'loading')
// Before the pager starts, a full bundle head implies more rows behind it.
const hasMore = computed(() =>
  started.value
    ? pager.hasNextPage.value || loadingMore.value
    : props.rail.items.length >= DISCOVERY_PAGE)

function onLoadMore() {
  if (!started.value) {
    started.value = true // enabling the query fetches its first page
    return
  }
  if (pager.asyncStatus.value !== 'loading' && pager.hasNextPage.value) void pager.loadNextPage()
}

// "Show all" deep-links to the grid (seeded sort/filter via query params —
// see useBrowseState), the person page, or the genre page. Rails with no
// browsable equivalent (rediscover, TMDB recommended) just scroll forever.
const showAllTo = computed(() => {
  const base = props.section === 'movie' ? '/movies' : '/tv'
  switch (props.rail.key) {
    case 'by-actor':
      return props.rail.baseline_id ? `/person/${props.rail.baseline_id}` : undefined
    case 'more-genre':
      return props.rail.baseline ? `/genre/${encodeURIComponent(props.rail.baseline)}` : undefined
    case 'recently-released':
      return `${base}/all?sort=year-desc`
    case 'top-unwatched':
      return `${base}/all?sort=rating&watched=unwatched`
    case 'top-rated':
      return `${base}/all?sort=rating`
    default:
      return undefined
  }
})
</script>
