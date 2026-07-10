<template>
  <div class="mt-layout">
    <LibrarySidebar
      v-if="!isPhone && !isCompact"
      :libraries="libraries"
      :active-lib="activeLib"
      :active-view="null"
      type-label="Books"
      :total-count="items.length"
      :hide-footer="true"
      @select="activeLib = $event"
    />
    <!-- Section-sidebar left drawer — phone (<=720px) and the compact band
         (720.02-1200px) both open it from AppTopBar's burger
         (useSectionSidebar's shared `open` ref); the persistent sidebar above
         doesn't mount below 1200px (v-if, not CSS). -->
    <AppSheet v-if="isPhone || isCompact" side="left" v-model:open="sectionSidebar.open.value" title="Library">
      <LibrarySidebar
        variant="sheet"
        :libraries="libraries"
        :active-lib="activeLib"
        :active-view="null"
        type-label="Books"
        :total-count="items.length"
        @select="activeLib = $event; sectionSidebar.close()"
      />
    </AppSheet>
    <div class="library-main scroll">
      <LibraryToolbar
        title="Books"
        :count="sorted.length"
        :sort="sort"
        :view="view"
        @sort="sort = $event"
        @view="view = $event"
      />

      <div class="lib-content">
        <div class="book-filters lib-pad-top">
          <button
            v-for="f in KIND_FILTERS"
            :key="f.key"
            class="book-filter"
            :class="{ active: kindFilter === f.key }"
            @click="kindFilter = f.key"
          >
            {{ f.label }}
            <span>{{ kindFilterCount(f.key) }}</span>
          </button>
        </div>

        <div v-if="loading" class="grid-posters lib-pad-top">
          <div v-for="i in 12" :key="i" class="grid-tile">
            <div class="poster" style="aspect-ratio: 2/3; background: var(--bg-3)" />
          </div>
        </div>

        <div v-else-if="view === 'grid'" ref="gridWrap" class="grid-virt lib-pad">
          <RecycleScroller
            :items="gridRows"
            :item-size="rowHeight"
            key-field="key"
            page-mode
            v-slot="{ item: row, index: rowIdx }"
          >
            <div class="grid-row" :style="{ gridTemplateColumns: `repeat(${gridCols}, minmax(0, 1fr))` }">
              <AppContextMenu
                v-for="(item, colIdx) in row.items"
                :key="item.id"
                :items="bookContextItems(item)"
              >
                <div
                  class="grid-tile card-tile"
                  @click="navigateTo(mediaUrl(item))"
                >
                  <Poster :idx="rowIdx * gridCols + colIdx" :src="usePosterUrl(item)" :aspect="'2/3'" :class="{ 'poster--missing': item.available === false }">
                    <MediaMissingBadge v-if="item.available === false" />
                  </Poster>
                  <div class="book-kind-badge" :class="`kind-${bookKind(item)}`">
                    <Icon :name="bookKind(item) === 'audiobook' ? 'music' : 'book'" :size="10" />
                    {{ bookKindLabel(item) }}
                  </div>
                  <div class="grid-tile-meta">
                    <div class="grid-tile-title">{{ item.title }}</div>
                    <div class="grid-tile-sub">{{ bookMetaLine(item) }}</div>
                  </div>
                </div>
              </AppContextMenu>
            </div>
          </RecycleScroller>
        </div>

        <div v-else class="list-rows lib-pad">
          <div v-if="!isPhone" class="list-row list-row-head books-list-row">
            <div>Title</div>
            <div>Type</div>
            <div>Year</div>
            <div>Added</div>
          </div>
          <RecycleScroller
            :items="sorted"
            :item-size="isPhone ? 76 : 70"
            key-field="id"
            page-mode
            v-slot="{ item }"
          >
            <AppContextMenu :items="bookContextItems(item)">
              <div
                class="list-row books-list-row"
                :class="{ 'list-row-phone': isPhone }"
                @click="navigateTo(mediaUrl(item))"
              >
                <template v-if="isPhone">
                  <Poster :idx="0" :src="usePosterUrl(item)" style="width: 44px; height: 66px; border-radius: 4px; flex-shrink: 0" :class="{ 'poster--missing': item.available === false }" />
                  <div class="list-phone-main">
                    <div class="list-title">
                      {{ item.title }}
                      <Icon v-if="item.available === false" name="trash" :size="11" class="list-missing-icon" />
                    </div>
                    <div class="list-sub">{{ bookMetaLine(item) }}</div>
                  </div>
                </template>
                <template v-else>
                  <div class="list-title-cell">
                    <Poster :idx="0" :src="usePosterUrl(item)" style="width: 36px; height: 54px; border-radius: 4px; flex-shrink: 0" :class="{ 'poster--missing': item.available === false }" />
                    <div>
                      <div class="list-title">
                        {{ item.title }}
                        <Icon v-if="item.available === false" name="trash" :size="11" class="list-missing-icon" />
                      </div>
                      <div class="list-sub">{{ item.book_author || item.year }}</div>
                    </div>
                  </div>
                  <div>
                    <span class="book-kind-pill" :class="`kind-${bookKind(item)}`">
                      <Icon :name="bookKind(item) === 'audiobook' ? 'music' : 'book'" :size="10" />
                      {{ bookKindLabel(item) }}
                    </span>
                  </div>
                  <div>{{ item.year }}</div>
                  <div class="list-added">{{ formatDateShort(item.created_at) }}</div>
                </template>
              </div>
            </AppContextMenu>
          </RecycleScroller>
        </div>

        <div v-if="!loading && !sorted.length" class="empty-lib">
          <p>{{ items.length ? 'No books match this filter.' : 'No books found. Scan a library to discover content.' }}</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem, Library, UserList } from '~~/shared/types'

// Ambient background: cycling book-artwork pool with the content veil.
useBackground().pool('book')

type BookKind = 'book' | 'audiobook'
type BookKindFilter = 'all' | BookKind

const KIND_FILTERS: { key: BookKindFilter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'book', label: 'Books' },
  { key: 'audiobook', label: 'Audiobooks' },
]

const gridWrap = ref<HTMLElement | null>(null)
const items = ref<MediaItem[]>([])
const libraries = ref<Library[]>([])
const userLists = ref<UserList[]>([])
const favoritedSet = ref<Set<number>>(new Set())
const loading = ref(true)
const activeLib = ref<number | null>(null)
const kindFilter = ref<BookKindFilter>('all')
const sort = ref('added')
const view = ref('grid')

const { isPhone, isCompact } = useViewport()
// Section-nav left drawer (phone + compact band), opened by AppTopBar's
// burger — shared singleton state (module-level ref), see useSectionSidebar.ts.
const sectionSidebar = useSectionSidebar()
const { buildItems: buildCardCtxItems } = useCardContextItems()

const scopedItems = computed(() => {
  let list = [...items.value]
  if (activeLib.value) list = list.filter(i => i.library_id === activeLib.value)
  return list
})

const sorted = computed(() => {
  let list = [...scopedItems.value]
  if (kindFilter.value !== 'all') list = list.filter(i => bookKind(i) === kindFilter.value)
  switch (sort.value) {
    case 'title': list.sort((a, b) => a.title.localeCompare(b.title)); break
    case 'year-desc': list.sort((a, b) => (b.year || '').localeCompare(a.year || '')); break
    case 'year-asc': list.sort((a, b) => (a.year || '').localeCompare(b.year || '')); break
    default: list.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
  }
  return list
})

const { cols: gridCols, rowHeight, rows: gridRows } = usePosterGrid(gridWrap, sorted)

function bookKind(item: MediaItem): BookKind {
  return item.book_format === 'audiobook' ? 'audiobook' : 'book'
}

function bookKindLabel(item: MediaItem): string {
  return bookKind(item) === 'audiobook' ? 'Audiobook' : 'Book'
}

function bookMetaLine(item: MediaItem): string {
  const parts = [item.book_author, item.year, bookKindLabel(item)].filter(Boolean)
  return parts.join(' · ')
}

function kindFilterCount(kind: BookKindFilter): number {
  if (kind === 'all') return scopedItems.value.length
  return scopedItems.value.filter(i => bookKind(i) === kind).length
}

function bookContextItems(item: MediaItem) {
  return buildCardCtxItems(item, {
    favoritedSet: favoritedSet.value,
    userLists: userLists.value,
    onToggleFavorite: async (id: number, favorited: boolean) => {
      try {
        const { $heya } = useNuxtApp()
        await $heya('/api/me/favorites', {
          method: 'POST',
          body: { entity_type: 'media_item', entity_id: id } as any,
        })
        const next = new Set(favoritedSet.value)
        if (favorited) next.add(id)
        else next.delete(id)
        favoritedSet.value = next
      } catch { /* ignore */ }
    },
    onAddToList: async (listId: number, mediaId: number) => {
      try {
        const { $heya } = useNuxtApp()
        await $heya('/api/me/lists/{id}/items', {
          method: 'POST',
          path: { id: listId },
          body: { media_item_id: mediaId } as any,
        })
      } catch { /* ignore */ }
    },
  })
}

onMounted(async () => {
  const { $heya } = useNuxtApp()
  const [mediaRes, libRes, listsRes, mediaStateRes] = await Promise.allSettled([
    $heya('/api/media', { query: { type: 'book', limit: 500 } }) as Promise<MediaItem[]>,
    $heya('/api/libraries') as Promise<Library[]>,
    $heya('/api/me/lists') as Promise<UserList[]>,
    $heya('/api/me/media-state') as Promise<{ watched: number[]; favorited: number[] }>,
  ])
  if (mediaRes.status === 'fulfilled') items.value = mediaRes.value
  if (libRes.status === 'fulfilled') libraries.value = libRes.value.filter(l => l.media_type === 'book')
  if (listsRes.status === 'fulfilled') userLists.value = listsRes.value
  if (mediaStateRes.status === 'fulfilled') favoritedSet.value = new Set(mediaStateRes.value.favorited || [])
  loading.value = false
})
</script>

<style scoped>
.lib-content { min-height: 200px; }
.grid-virt { /* container for usePosterGrid; width is the source of truth */ }
.grid-row { display: grid; column-gap: 18px; padding-bottom: 22px; }
/* Was inline `style="padding: 0 32px 80px"` on grid-virt/list-rows (and
   `0 32px` on the loading skeleton) — moved to classes so phone can
   override without fighting inline style specificity. */
.lib-pad { padding: 0 32px 80px; }
.lib-pad-top { padding: 0 32px; }
.empty-lib { padding: 80px 32px; text-align: center; color: var(--fg-2); font-size: 15px; }
.list-missing-icon { color: var(--bad); vertical-align: -1px; margin-left: 4px; }
.book-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 18px;
}
.book-filter {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--bg-2);
  color: var(--fg-2);
  font-size: 12px;
}
.book-filter:hover { color: var(--fg-0); border-color: var(--border-strong); }
.book-filter.active { color: rgb(140, 220, 180); border-color: rgba(140, 220, 180, 0.35); background: rgba(140, 220, 180, 0.08); }
.book-filter span { font-family: var(--font-mono); font-size: 10px; color: var(--fg-3); }
.book-filter.active span { color: rgb(140, 220, 180); }
.card-tile { position: relative; }
.book-kind-badge,
.book-kind-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: rgba(0,0,0,0.58);
  color: var(--fg-1);
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 650;
  white-space: nowrap;
}
.book-kind-badge {
  position: absolute;
  left: 8px;
  top: 8px;
  padding: 3px 7px;
  backdrop-filter: blur(8px);
}
.book-kind-pill {
  padding: 3px 8px;
  background: rgb(var(--ink) / 0.04);
}
.book-kind-badge.kind-audiobook,
.book-kind-pill.kind-audiobook {
  color: rgb(200, 140, 255);
  border-color: rgba(200, 140, 255, 0.30);
  background: rgba(200, 140, 255, 0.10);
}
.book-kind-badge.kind-book,
.book-kind-pill.kind-book {
  color: rgb(140, 220, 180);
  border-color: rgba(140, 220, 180, 0.30);
  background: rgba(140, 220, 180, 0.10);
}
.books-list-row {
  grid-template-columns: minmax(0, 2.8fr) 110px 80px 120px;
}

/* ── Phone (<=720px) ─────────────────────────────────────────────────
   Grid gap/padding here must track usePosterGrid.ts's phone constants
   (MIN_CARD_PHONE/COL_GAP_PHONE/ROW_GAP_PHONE). */
@media (max-width: 720px) {
  .lib-pad { padding: 0 12px 90px; }
  .lib-pad-top { padding: 0 12px; }
  .book-filters { margin-bottom: 14px; }
  .grid-row { column-gap: 10px; padding-bottom: 14px; }

  .list-row-phone {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px;
  }
  .list-phone-main { flex: 1; min-width: 0; }
}
</style>
