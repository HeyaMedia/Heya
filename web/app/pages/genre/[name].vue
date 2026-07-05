<template>
  <div class="scroll page-pad" style="height: 100%">
    <header class="genre-head">
      <div class="genre-eyebrow">Genre</div>
      <h1 class="genre-title">{{ displayName }}</h1>
      <div v-if="!loading" class="genre-meta">
        {{ total.toLocaleString() }} title<span v-if="total !== 1">s</span>
      </div>
    </header>

    <div v-if="loading" class="grid-posters">
      <div v-for="i in 12" :key="i" class="grid-tile">
        <div class="poster" style="aspect-ratio: 2/3; background: var(--bg-3); animation: pulse 1.5s infinite" />
      </div>
    </div>

    <div v-else-if="items.length" ref="gridWrap" class="grid-virt">
      <RecycleScroller
        :items="gridRows"
        :item-size="rowHeight"
        key-field="key"
        page-mode
        v-slot="{ item: row, index: rowIdx }"
      >
        <div class="grid-row" :style="{ gridTemplateColumns: `repeat(${gridCols}, 1fr)` }">
          <NuxtLink
            v-for="(item, colIdx) in row.items"
            :key="item.id"
            :to="mediaUrl(item)"
            class="grid-tile card-tile"
          >
            <Poster :idx="rowIdx * gridCols + colIdx" :src="usePosterUrl(item.id)" aspect="2/3" :title="item.title" />
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ item.title }}</div>
              <div class="grid-tile-sub">{{ item.year }} · {{ mediaTypeLabel(item.media_type) }}</div>
            </div>
          </NuxtLink>
        </div>
      </RecycleScroller>
    </div>

    <div v-if="!loading && total > items.length" class="load-more-wrap">
      <button class="btn btn-secondary" @click="loadMore" :disabled="loadingMore">
        {{ loadingMore ? 'Loading…' : `Show more (${total - items.length} remaining)` }}
      </button>
    </div>

    <div v-if="!loading && !items.length" class="genre-empty">
      No media found with this genre.
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'

const route = useRoute()
const name = computed(() => route.params.name as string)
const displayName = computed(() => decodeURIComponent(name.value).replace(/-/g, ' '))

const gridWrap = ref<HTMLElement | null>(null)
const items = ref<MediaItem[]>([])
const total = ref(0)

const { cols: gridCols, rowHeight, rows: gridRows } = usePosterGrid(gridWrap, items)
const loading = ref(true)
const loadingMore = ref(false)
const PAGE_SIZE = 60

async function fetchGenre(reset = true) {
  if (reset) {
    items.value = []
    total.value = 0
  }
  const offset = reset ? 0 : items.value.length
  const { $heya } = useNuxtApp()
  const res = await $heya('/api/genres/{name}', {
    path: { name: name.value },
    query: { limit: PAGE_SIZE, offset },
  }) as { genre: string; items: MediaItem[]; total: number }
  if (reset) {
    items.value = res.items || []
  } else {
    items.value = items.value.concat(res.items || [])
  }
  total.value = res.total || 0
}

async function loadMore() {
  loadingMore.value = true
  await fetchGenre(false)
  loadingMore.value = false
}

onMounted(async () => {
  await fetchGenre()
  loading.value = false
})

watch(name, async () => {
  loading.value = true
  await fetchGenre()
  loading.value = false
})
</script>

<style scoped>
.genre-head { margin-bottom: 28px; }
.genre-eyebrow {
  font-size: 10px; font-family: var(--font-mono); font-weight: 700;
  letter-spacing: 0.18em; text-transform: uppercase; color: var(--gold); margin-bottom: 8px;
}
.genre-title { font-size: 36px; font-weight: 600; letter-spacing: -0.02em; margin: 0 0 6px; }
.genre-meta { font-size: 12px; font-family: var(--font-mono); color: var(--fg-3); }
.genre-empty { padding: 60px 0; text-align: center; color: var(--fg-3); font-size: 14px; }
.load-more-wrap { text-align: center; padding: 24px 0 80px; }
.grid-virt { /* container for usePosterGrid */ }
.grid-row { display: grid; column-gap: 18px; padding-bottom: 22px; }

/* Phone: 16px side padding per the established grid-page pattern (this
   overrides heya.css's global .page-pad, which only tightens at 1100px).
   `.grid-row`'s gap/padding here must track usePosterGrid.ts's phone
   constants (MIN_CARD_PHONE/COL_GAP_PHONE/ROW_GAP_PHONE, landed via the
   W3b library-pages package) — same pairing movies/tv/books index.vue use
   — so the JS column math and the actual rendered gap agree. */
@media (max-width: 720px) {
  .page-pad { padding: 20px 16px 60px; }
  .genre-title { font-size: 26px; }
  .grid-row { column-gap: 10px; padding-bottom: 14px; }
}
</style>
