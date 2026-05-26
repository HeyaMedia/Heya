<template>
  <div class="mt-layout">
    <LibrarySidebar
      :libraries="libraries"
      :active-lib="activeLib"
      :active-view="null"
      type-label="Books"
      :total-count="items.length"
      @select="activeLib = $event"
    />
    <div class="library-main scroll">
      <LibraryToolbar
        title="Books"
        :count="items.length"
        :sort="sort"
        :view="view"
        @sort="sort = $event"
        @view="view = $event"
      />

      <div class="lib-content">
        <div v-if="loading" class="grid-posters" style="padding: 0 32px">
          <div v-for="i in 12" :key="i" class="grid-tile">
            <div class="poster" style="aspect-ratio: 2/3; background: var(--bg-3)" />
          </div>
        </div>

        <div v-else-if="view === 'grid'" ref="gridWrap" class="grid-virt" style="padding: 0 32px 80px">
          <RecycleScroller
            :items="gridRows"
            :item-size="rowHeight"
            key-field="key"
            page-mode
            v-slot="{ item: row, index: rowIdx }"
          >
            <div class="grid-row" :style="{ gridTemplateColumns: `repeat(${gridCols}, 1fr)` }">
              <div
                v-for="(item, colIdx) in row.items"
                :key="item.id"
                class="grid-tile card-tile"
                @click="navigateTo(mediaUrl(item))"
              >
                <Poster :idx="rowIdx * gridCols + colIdx" :src="usePosterUrl(item.id)" :aspect="'2/3'" />
                <div class="grid-tile-meta">
                  <div class="grid-tile-title">{{ item.title }}</div>
                  <div class="grid-tile-sub">{{ item.year }}</div>
                </div>
              </div>
            </div>
          </RecycleScroller>
        </div>

        <div v-else class="list-rows" style="padding: 0 32px 80px">
          <div class="list-row list-row-head">
            <div>Title</div>
            <div>Year</div>
            <div>Added</div>
          </div>
          <RecycleScroller
            :items="sorted"
            :item-size="70"
            key-field="id"
            page-mode
            v-slot="{ item }"
          >
            <div
              class="list-row"
              @click="navigateTo(mediaUrl(item))"
            >
              <div class="list-title-cell">
                <Poster :idx="0" :src="usePosterUrl(item.id)" style="width: 36px; height: 54px; border-radius: 4px; flex-shrink: 0" />
                <div>
                  <div class="list-title">{{ item.title }}</div>
                  <div class="list-sub">{{ item.year }}</div>
                </div>
              </div>
              <div>{{ item.year }}</div>
              <div class="list-added">{{ formatDate(item.created_at) }}</div>
            </div>
          </RecycleScroller>
        </div>

        <div v-if="!loading && !items.length" class="empty-lib">
          <p>No books found. Scan a library to discover content.</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem, Library } from '~~/shared/types'


const gridWrap = ref<HTMLElement | null>(null)
const items = ref<MediaItem[]>([])
const libraries = ref<Library[]>([])
const loading = ref(true)
const activeLib = ref<number | null>(null)
const sort = ref('added')
const view = ref('grid')

const sorted = computed(() => {
  let list = [...items.value]
  if (activeLib.value) list = list.filter(i => i.library_id === activeLib.value)
  switch (sort.value) {
    case 'title': list.sort((a, b) => a.title.localeCompare(b.title)); break
    case 'year-desc': list.sort((a, b) => (b.year || '').localeCompare(a.year || '')); break
    case 'year-asc': list.sort((a, b) => (a.year || '').localeCompare(b.year || '')); break
    default: list.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
  }
  return list
})

const { cols: gridCols, rowHeight, rows: gridRows } = usePosterGrid(gridWrap, sorted)

function formatDate(d: string) {
  if (!d) return ''
  return new Date(d).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

onMounted(async () => {
  const { $heya } = useNuxtApp()
  const [mediaRes, libRes] = await Promise.allSettled([
    $heya('/api/media', { query: { type: 'book', limit: 500 } }) as Promise<MediaItem[]>,
    $heya('/api/libraries') as Promise<Library[]>,
  ])
  if (mediaRes.status === 'fulfilled') items.value = mediaRes.value
  if (libRes.status === 'fulfilled') libraries.value = libRes.value.filter(l => l.media_type === 'book')
  loading.value = false
})
</script>

<style scoped>
.lib-content { min-height: 200px; }
.grid-virt { /* container for usePosterGrid; width is the source of truth */ }
.grid-row { display: grid; column-gap: 18px; padding-bottom: 22px; }
.empty-lib { padding: 80px 32px; text-align: center; color: var(--fg-2); font-size: 15px; }
</style>
