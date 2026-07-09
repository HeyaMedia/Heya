<template>
  <div class="mt-layout">
    <LibrarySidebar
      v-if="!isPhone && !isCompact"
      :libraries="libraries"
      :active-lib="activeLib"
      :active-view="activeView"
      type-label="Movies"
      :show-browse="true"
      :total-count="items.length"
      :loved-count="favoritedSet.size"
      :user-lists="userLists"
      :collections="displayCollections"
      :drag-over-list-id="dragState.overListId"
      @select="activeLib = $event; activeView = null"
      @view="activeView = $event"
      @list-drop="onListDrop"
      @list-dragover="onListDragOver"
      @list-dragleave="onListDragLeave"
    />
    <!-- Section-sidebar left drawer — phone (<=720px) and the compact band
         (720.02-1200px) both open it from AppTopBar's burger
         (useSectionSidebar's shared `open` ref); the persistent 240px sidebar
         above doesn't mount below 1200px (v-if, not CSS). Same component,
         `variant="sheet"` — see LibrarySidebar.vue for why this one could just
         add a variant instead of MusicSidebar's (W1c) flat re-listing
         workaround. -->
    <AppSheet v-if="isPhone || isCompact" side="left" v-model:open="sectionSidebar.open.value" title="Library">
      <LibrarySidebar
        variant="sheet"
        :libraries="libraries"
        :active-lib="activeLib"
        :active-view="activeView"
        type-label="Movies"
      :show-browse="true"
        :total-count="items.length"
        :loved-count="favoritedSet.size"
        :user-lists="userLists"
        :collections="displayCollections"
        :drag-over-list-id="dragState.overListId"
        @select="activeLib = $event; activeView = null; sectionSidebar.close()"
        @view="activeView = $event; sectionSidebar.close()"
        @list-drop="onListDrop"
        @list-dragover="onListDragOver"
        @list-dragleave="onListDragLeave"
      />
    </AppSheet>
    <!-- Recommended landing (bare /movies). Its own scroll container; the flat
         grid + franchises live in the sibling main below. -->
    <BrowseView v-if="activeView === 'browse'" section="movie" class="library-main" />
    <RecsBrowse v-else-if="activeView === 'recommendations'" section="movie" class="library-main" />
    <div v-else ref="mainEl" class="library-main scroll" @scroll.passive="onMainScroll">
      <!-- Franchises overview — a page of its own (/movies/franchises). Reuses
           the FilterBar (sort + grid/detail/list toggle, no movie filters) and
           the same view chrome as the library; cards/rows deep-link into the
           per-franchise browse view (/movies/collection/N). -->
      <template v-if="activeView === 'franchises'">
        <FilterBar
          title="Franchises"
          count-label="franchises"
          :count="sortedFranchises.length"
          :sort="franchiseSort"
          :view="view"
          :filters="filters"
          :available-genres="[]"
          :available-languages="[]"
          :sort-options="FRANCHISE_SORTS"
          hide-filters
          @sort="franchiseSort = $event"
          @view="view = $event"
        />

        <div class="lib-content">
          <div v-if="!sortedFranchises.length" class="empty-lib">
            <Icon name="film" :size="30" class="empty-icon" />
            <p>No franchises with more than one film yet.</p>
          </div>

          <div v-else-if="view === 'grid'" class="grid-posters fr-grid">
            <NuxtLink
              v-for="(c, i) in sortedFranchises"
              :key="c.id"
              :to="`/collection/${c.id}`"
              class="grid-tile card-tile"
            >
              <MediaCard
                :idx="i"
                :src="c.poster_path"
                aspect="2/3"
                :title="franchiseLabel(c.name)"
                :subtitle="`${c.movie_count} films`"
              />
            </NuxtLink>
          </div>

          <div v-else-if="view === 'detail'" class="fr-detail lib-pad">
            <NuxtLink
              v-for="(c, i) in sortedFranchises"
              :key="c.id"
              :to="`/collection/${c.id}`"
              class="browse-detail-row"
            >
              <Poster :idx="i" :src="c.poster_path" class-name="browse-detail-poster" :width="104" />
              <div class="browse-detail-body">
                <div class="browse-detail-title"><span>{{ franchiseLabel(c.name) }}</span></div>
                <div class="browse-detail-meta">
                  <span>{{ c.movie_count }} films</span>
                  <span v-if="c.added">Added {{ formatDateShort(c.added) }}</span>
                </div>
              </div>
            </NuxtLink>
          </div>

          <div v-else class="list-rows lib-pad">
            <div v-if="!isPhone" class="list-row list-row-head fr-list-row">
              <div>Franchise</div>
              <div>Films</div>
              <div>Added</div>
            </div>
            <NuxtLink
              v-for="c in sortedFranchises"
              :key="c.id"
              :to="`/collection/${c.id}`"
              class="list-row fr-list-row"
              :class="{ 'list-row-phone': isPhone }"
            >
              <div class="list-title-cell">
                <Poster :idx="0" :src="c.poster_path" style="width: 36px; height: 54px; border-radius: 4px; flex-shrink: 0" />
                <div>
                  <div class="list-title">{{ franchiseLabel(c.name) }}</div>
                  <div class="list-sub">{{ c.movie_count }} films<span v-if="isPhone && c.added"> · {{ formatDateShort(c.added) }}</span></div>
                </div>
              </div>
              <div v-if="!isPhone">{{ c.movie_count }}</div>
              <div v-if="!isPhone" class="list-added">{{ c.added ? formatDateShort(c.added) : '–' }}</div>
            </NuxtLink>
          </div>
        </div>
      </template>

      <template v-else>
      <FilterBar
        :title="viewTitle"
        :count="sorted.length"
        :sort="sort"
        :view="view"
        :filters="filters"
        :available-genres="availableGenres"
        :available-languages="availableLanguages"
        :genre-counts="genreCounts"
        :dirty="isDirty"
        @sort="sort = $event"
        @view="view = $event"
        @update:filters="onFiltersChange"
        @save-list="saveSmartList"
        @reset="resetBrowse"
      />

      <div class="lib-content">
        <div v-if="loading" class="grid-posters lib-pad-top">
          <div v-for="i in 12" :key="i" class="grid-tile">
            <div class="poster" style="aspect-ratio: 2/3; background: var(--bg-3); animation: pulse 1.5s infinite" />
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
                :items="ctxItemsFor(item)"
              >
                <div
                  class="grid-tile card-tile"
                  :class="{ unavailable: item.available === false }"
                  draggable="true"
                  @click="item.available !== false && navigateTo(mediaUrl(item))"
                  @dragstart="onDragStart($event, item)"
                  @dragend="onDragEnd"
                >
                  <MediaCard
                    :idx="rowIdx * gridCols + colIdx"
                    :src="usePosterUrl(item.id)"
                    aspect="2/3"
                    :title="item.title"
                    :title-to="mediaUrl(item)"
                    :subtitle="item.year + (item.rating ? ` · ${item.rating.toFixed(1)}★` : '')"
                    :missing="item.available === false"
                  >
                    <template #badges>
                      <div v-if="item.resolution" class="res-badge">{{ item.resolution === '4k' ? '4K' : item.resolution }}</div>
                      <div v-if="isWatched(item.id)" class="watched-badge"><Icon name="check" :size="10" /></div>
                      <div v-if="isFavorited(item.id)" class="fav-badge"><Icon name="heartfill" :size="10" /></div>
                    </template>
                  </MediaCard>
                </div>
              </AppContextMenu>
            </div>
          </RecycleScroller>
        </div>

        <div v-else-if="view === 'detail'" class="detail-virt lib-pad">
          <RecycleScroller
            :items="sorted"
            :item-size="isPhone ? 132 : 188"
            key-field="id"
            page-mode
            v-slot="{ item, index }"
          >
            <AppContextMenu :items="ctxItemsFor(item)">
              <div
                class="browse-detail-row"
                :class="{ unavailable: item.available === false, 'browse-detail-row-phone': isPhone }"
                draggable="true"
                @click="item.available !== false && navigateTo(mediaUrl(item))"
                @dragstart="onDragStart($event, item)"
                @dragend="onDragEnd"
              >
                <template v-if="isPhone">
                  <div class="bdr-top">
                    <Poster :idx="index" :src="usePosterUrl(item.id)" style="width: 52px; height: 78px; border-radius: 4px; flex-shrink: 0" />
                    <div class="bdr-top-text">
                      <div class="bdr-title">
                        {{ item.title }}
                        <Icon v-if="isWatched(item.id)" name="check" :size="12" style="color: var(--good); flex-shrink: 0" />
                        <Icon v-if="isFavorited(item.id)" name="heartfill" :size="12" style="color: var(--bad); flex-shrink: 0" />
                      </div>
                      <div class="bdr-meta">
                        <span>{{ item.year }}</span>
                        <span v-if="item.rating" class="star"><Icon name="star" :size="10" weight="fill" />{{ item.rating.toFixed(1) }}</span>
                        <span v-if="item.resolution" class="browse-detail-res">{{ item.resolution === '4k' ? '4K' : item.resolution }}</span>
                      </div>
                    </div>
                    <button type="button" class="bdr-more" aria-label="More actions" @click.stop="openListSheet(item)">
                      <Icon name="more" :size="18" />
                    </button>
                  </div>
                  <div v-if="item.genres?.length" class="browse-detail-genres">
                    <span v-for="g in item.genres.slice(0, 3)" :key="g" class="chip">{{ g }}</span>
                  </div>
                </template>
                <template v-else>
                  <Poster :idx="index" :src="usePosterUrl(item.id)" class-name="browse-detail-poster" :width="120" />
                  <div class="browse-detail-body">
                    <div class="browse-detail-title">
                      <span>{{ item.title }}</span>
                      <Icon v-if="isWatched(item.id)" name="check" :size="14" style="color: var(--good); flex-shrink: 0" />
                      <Icon v-if="isFavorited(item.id)" name="heartfill" :size="14" style="color: var(--bad); flex-shrink: 0" />
                    </div>
                    <div class="browse-detail-meta">
                      <span>{{ item.year }}</span>
                      <span v-if="item.runtime_minutes">{{ fmtRuntime(item.runtime_minutes) }}</span>
                      <span v-if="item.rating" class="star"><Icon name="star" :size="11" weight="fill" />{{ item.rating.toFixed(1) }}</span>
                      <span v-if="item.resolution" class="browse-detail-res">{{ item.resolution === '4k' ? '4K' : item.resolution }}</span>
                    </div>
                    <div v-if="item.genres?.length" class="browse-detail-genres">
                      <span v-for="g in item.genres.slice(0, 4)" :key="g" class="chip">{{ g }}</span>
                    </div>
                    <p v-if="item.description" class="browse-detail-overview">{{ item.description }}</p>
                  </div>
                </template>
              </div>
            </AppContextMenu>
          </RecycleScroller>
        </div>

        <div v-else class="list-rows lib-pad">
          <div v-if="!isPhone" class="list-row list-row-head">
            <div>Title</div>
            <div>Year</div>
            <div>Rating</div>
            <div>Genre</div>
            <div>Added</div>
          </div>
          <RecycleScroller
            :items="sorted"
            :item-size="isPhone ? 76 : 70"
            key-field="id"
            page-mode
            v-slot="{ item }"
          >
            <AppContextMenu :items="ctxItemsFor(item)">
            <div
              class="list-row"
              :class="{ 'list-row-phone': isPhone }"
              @click="navigateTo(mediaUrl(item))"
            >
              <template v-if="isPhone">
                <Poster :idx="0" :src="usePosterUrl(item.id)" style="width: 44px; height: 66px; border-radius: 4px; flex-shrink: 0" />
                <div class="list-phone-main">
                  <div class="list-title">
                    {{ item.title }}
                    <Icon v-if="isWatched(item.id)" name="check" :size="12" style="color: var(--good); margin-left: 4px" />
                    <Icon v-if="isFavorited(item.id)" name="heartfill" :size="12" style="color: var(--bad); margin-left: 2px" />
                  </div>
                  <div class="list-sub">{{ item.year }}<span v-if="item.rating"> · {{ item.rating.toFixed(1) }}★</span></div>
                </div>
                <button type="button" class="list-phone-more" aria-label="More actions" @click.stop="openListSheet(item)">
                  <Icon name="more" :size="18" />
                </button>
              </template>
              <template v-else>
                <div class="list-title-cell">
                  <Poster :idx="0" :src="usePosterUrl(item.id)" style="width: 36px; height: 54px; border-radius: 4px; flex-shrink: 0" />
                  <div>
                    <div class="list-title">
                      {{ item.title }}
                      <Icon v-if="isWatched(item.id)" name="check" :size="12" style="color: var(--good); margin-left: 4px" />
                      <Icon v-if="isFavorited(item.id)" name="heartfill" :size="12" style="color: var(--bad); margin-left: 2px" />
                    </div>
                    <div class="list-sub">{{ item.year }}</div>
                  </div>
                </div>
                <div>{{ item.year }}</div>
                <div>{{ item.rating ? item.rating.toFixed(1) : '–' }}</div>
                <div class="list-genres">{{ (item.genres || []).slice(0, 2).join(', ') }}</div>
                <div class="list-added">{{ formatDateShort(item.created_at) }}</div>
              </template>
            </div>
            </AppContextMenu>
          </RecycleScroller>
        </div>

        <div v-if="!loading && !items.length" class="empty-lib">
          <Icon name="film" :size="30" class="empty-icon" />
          <p>No movies found. Scan a library to discover content.</p>
        </div>
        <div v-else-if="!loading && !sorted.length" class="empty-lib">
          <Icon name="filter" :size="30" class="empty-icon" />
          <p>Nothing matches the current filters.</p>
          <button v-if="isDirty" class="btn btn-secondary" @click="resetBrowse">Reset filters</button>
        </div>
      </div>
      </template>
    </div>

    <!-- Phone: list/detail rows get an always-visible "..." button (grid
         tiles rely on AppContextMenu's native long-press instead — see
         MediaCard findings in the W3b report). Shares ctxItemsFor with the
         AppContextMenu each row is already wrapped in. -->
    <ActionSheet
      v-model:open="listSheetOpen"
      :items="listSheetItem ? ctxItemsFor(listSheetItem) : []"
      :title="listSheetItem?.title"
    />
  </div>
</template>

<script setup lang="ts">
import type { EnrichedMediaItem, Library, UserList, FilterState, CollectionBrowse } from '~~/shared/types'
import { useCardContextItems } from '~/composables/useContextMenu'

// Stable page key shared with the browse sub-routes registered in
// app/router.options.ts (/movies/library/:id, /movies/loved, …) so switching
// the sidebar selection reuses this component instead of remounting + refetching.
definePageMeta({ key: 'browse-movies' })

const mainEl = ref<HTMLElement | null>(null)
const gridWrap = ref<HTMLElement | null>(null)
const items = ref<EnrichedMediaItem[]>([])
const libraries = ref<Library[]>([])
const userLists = ref<UserList[]>([])
const collections = ref<CollectionBrowse[]>([])
const loading = ref(true)

const { isPhone, isCompact } = useViewport()
// Section-nav left drawer (phone + compact band), opened by AppTopBar's
// burger — shared singleton state (module-level ref), see useSectionSidebar.ts.
const sectionSidebar = useSectionSidebar()

// Phone-only "..." action sheet for list/detail rows — see ActionSheet usage
// at the bottom of the template. Grid tiles don't need this: AppContextMenu
// already opens on long-press (docs/responsive-plan.md W0b).
const listSheetOpen = ref(false)
const listSheetItem = ref<EnrichedMediaItem | null>(null)
function openListSheet(item: EnrichedMediaItem) {
  listSheetItem.value = item
  listSheetOpen.value = true
}

// View mode, sort, filters, sidebar selection and scroll offset all persist —
// navigating into a movie and back restores the page exactly as it was.
const browse = useBrowseState('movies', { browseDefault: true })
const { view, sort, filters, activeLib, activeView, scrollTop } = browse
const { isDirty, restoreScroll } = browse

// The Recommended landing (bare /movies) renders rails from their own queries
// and never needs the full item list, so defer the up-to-5000-item /enriched
// fetch until a grid/franchises view is actually entered.
const itemsLoaded = ref(false)
async function ensureItems() {
  if (itemsLoaded.value) return
  loading.value = true
  await loadItems()
  itemsLoaded.value = true
  loading.value = false
}
watch(activeView, (v) => { if (v !== 'browse' && v !== 'recommendations') ensureItems() })

const favoritedSet = ref<Set<number>>(new Set())
const watchedSet = ref<Set<number>>(new Set())
function isWatched(id: number) { return watchedSet.value.has(id) }
function isFavorited(id: number) { return favoritedSet.value.has(id) }

const personMediaIds = ref<Set<number>>(new Set())
const studioMediaIds = ref<Set<number>>(new Set())

const listItems = ref<Set<number>>(new Set())

const { buildItems: buildCardCtxItems } = useCardContextItems()
const { dragState, onDragStart, onDragEnd, onListDragOver, onListDragLeave, onListDrop } = useDragDrop()

// Action handlers shared across grid + list views. Reactive sets are read
// at item-build time so each render reflects the latest watched/favorited
// state; reka only mounts the menu on right-click so this is cheap.
const { $heya } = useNuxtApp()
const invalidateContinueWatching = useInvalidateContinueWatching()
const cardCtxOpts = computed(() => {
  return {
    watchedSet: watchedSet.value,
    favoritedSet: favoritedSet.value,
    userLists: userLists.value,
    onToggleWatched: async (id: number, watched: boolean) => {
      try {
        await $heya('/api/me/watched/media/{id}', {
          method: 'POST',
          path: { id },
          body: { watched } as any,
        })
        if (watched) watchedSet.value.add(id)
        else watchedSet.value.delete(id)
        watchedSet.value = new Set(watchedSet.value)
        invalidateContinueWatching()
      } catch { /* ignore */ }
    },
    onToggleFavorite: async (id: number, favorited: boolean) => {
      try {
        await $heya('/api/me/favorites', {
          method: 'POST',
          body: { entity_type: 'media_item', entity_id: id } as any,
        })
        if (favorited) favoritedSet.value.add(id)
        else favoritedSet.value.delete(id)
        favoritedSet.value = new Set(favoritedSet.value)
      } catch { /* ignore */ }
    },
    onAddToList: async (listId: number, mediaId: number) => {
      try {
        await $heya('/api/me/lists/{id}/items', {
          method: 'POST',
          path: { id: listId },
          body: { media_item_id: mediaId } as any,
        })
      } catch { /* ignore */ }
    },
  }
})

function ctxItemsFor(item: EnrichedMediaItem) {
  return buildCardCtxItems(item, cardCtxOpts.value)
}

// Franchises surfaced in the sidebar row + /movies/franchises grid: only
// collections we own ≥2 films from (a lone film isn't a "franchise"). The full
// `collections` list stays the source of truth for title lookup + selection
// revalidation, so deep links to a 1-film collection still resolve.
const displayCollections = computed(() => collections.value.filter(c => c.movie_count >= 2))

// Franchises browse: the /movies/franchises view reuses the FilterBar + the
// grid/detail/list view modes, so it needs its own sort. "Recently Added" is
// derived client-side from the already-loaded movies — a franchise's date is
// the newest created_at among the films we own from it — so no extra fetch.
const FRANCHISE_SORTS = [
  { label: 'Name A→Z', value: 'name' },
  { label: 'Recently Added', value: 'added' },
  { label: 'Most Films', value: 'films-desc' },
  { label: 'Fewest Films', value: 'films-asc' },
]
const franchiseSort = ref('name')

const franchiseRows = computed(() => {
  const addedStr = new Map<number, string>()
  const addedTs = new Map<number, number>()
  for (const it of items.value) {
    const cid = it.collection_id
    if (cid == null || !it.created_at) continue
    const t = new Date(it.created_at).getTime()
    if (t > (addedTs.get(cid) ?? -1)) { addedTs.set(cid, t); addedStr.set(cid, it.created_at) }
  }
  return displayCollections.value.map(c => ({
    ...c,
    added: addedStr.get(c.id) ?? '',
    addedTs: addedTs.get(c.id) ?? 0,
  }))
})

const sortedFranchises = computed(() => {
  const rows = [...franchiseRows.value]
  switch (franchiseSort.value) {
    case 'films-desc': rows.sort((a, b) => b.movie_count - a.movie_count); break
    case 'films-asc': rows.sort((a, b) => a.movie_count - b.movie_count); break
    case 'added': rows.sort((a, b) => b.addedTs - a.addedTs); break
    default: rows.sort((a, b) => franchiseLabel(a.name).localeCompare(franchiseLabel(b.name)))
  }
  return rows
})

const viewTitle = computed(() => {
  if (activeView.value === 'loved') return 'Loved Movies'
  if (activeView.value === 'franchises') return 'Franchises'
  if (activeView.value?.startsWith('list-')) {
    const list = userLists.value.find(l => `list-${l.id}` === activeView.value)
    return list?.name || 'List'
  }
  return 'Movies'
})

const availableGenres = computed(() => extractAvailableGenres(items.value))
const availableLanguages = computed(() => extractLanguages(items.value))

const genreCounts = computed(() => {
  const counts: Record<string, number> = {}
  for (const item of items.value) {
    for (const g of item.genres || []) counts[g] = (counts[g] || 0) + 1
  }
  return counts
})

watch(activeView, (v) => { syncActiveView(v) })

async function syncActiveView(v: string | null) {
  if (!v) { listItems.value = new Set(); return }
  if (v.startsWith('list-')) {
    const listId = v.replace('list-', '')
    const list = userLists.value.find(l => String(l.id) === listId)
    if (list?.list_type === 'smart' && list.filter_json) {
      filters.value = { ...defaultFilters(), ...list.filter_json }
      listItems.value = new Set()
      return
    }
    try {
      const { $heya } = useNuxtApp()
      const res = await $heya('/api/me/lists/{id}', {
        path: { id: Number(listId) },
      }) as { items: any[] }
      listItems.value = new Set((res.items || []).map((i: any) => i.id))
    } catch { listItems.value = new Set() }
  }
}

const filtered = computed(() => {
  let list = [...items.value]
  if (activeView.value === 'loved') {
    list = list.filter(i => favoritedSet.value.has(i.id))
  } else if (activeView.value?.startsWith('list-')) {
    const listId = activeView.value.replace('list-', '')
    const userList = userLists.value.find(l => String(l.id) === listId)
    if (userList?.list_type === 'smart') {
      // Smart list: filters are applied below
    } else {
      list = list.filter(i => listItems.value.has(i.id))
    }
  } else if (activeLib.value) {
    list = list.filter(i => i.library_id === activeLib.value)
  }
  return applyFilters(list, filters.value, watchedSet.value, personMediaIds.value, studioMediaIds.value)
})

const sorted = computed(() => {
  const list = [...filtered.value]
  switch (sort.value) {
    case 'title': list.sort((a, b) => (a.sort_title || a.title).localeCompare(b.sort_title || b.title)); break
    case 'year-desc': list.sort((a, b) => (b.year || '').localeCompare(a.year || '')); break
    case 'year-asc': list.sort((a, b) => (a.year || '').localeCompare(b.year || '')); break
    case 'rating': list.sort((a, b) => (b.rating || 0) - (a.rating || 0)); break
    default: list.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
  }
  return list
})

const { cols: gridCols, rowHeight, rows: gridRows } = usePosterGrid(gridWrap, sorted)

function onMainScroll() {
  if (mainEl.value) scrollTop.value = mainEl.value.scrollTop
}

function resetBrowse() {
  browse.reset()
  personMediaIds.value = new Set()
  studioMediaIds.value = new Set()
}

async function onFiltersChange(f: FilterState) {
  filters.value = f
  const { $heya } = useNuxtApp()
  if (f.personIds.length > 0) {
    try {
      const ids = await $heya('/api/people/media-ids', {
        method: 'POST',
        body: { person_ids: f.personIds } as any,
      }) as number[]
      personMediaIds.value = new Set(ids)
    } catch {
      // Fallback: search cast directly
      personMediaIds.value = new Set()
    }
  } else {
    personMediaIds.value = new Set()
  }
  if (f.studioIds.length > 0) {
    try {
      const ids = await $heya('/api/studios/media-ids', {
        method: 'POST',
        body: { company_ids: f.studioIds } as any,
      }) as number[]
      studioMediaIds.value = new Set(ids)
    } catch {
      studioMediaIds.value = new Set()
    }
  } else {
    studioMediaIds.value = new Set()
  }
}

async function saveSmartList() {
  const name = prompt('Smart list name:')
  if (!name?.trim()) return
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/me/lists', {
      method: 'POST',
      body: {
        name: name.trim(),
        list_type: 'smart',
        filter_json: filters.value,
        media_type: 'movie',
      } as any,
    })
    await loadLists()
  } catch { /* ignore */ }
}

async function loadLists() {
  try {
    const { $heya } = useNuxtApp()
    userLists.value = await $heya('/api/me/lists') as UserList[]
  } catch { /* ignore */ }
}

function fmtRuntime(min: number) {
  const h = Math.floor(min / 60)
  return h ? `${h}h ${min % 60}m` : `${min}m`
}

// Pulled out of onMounted so useLiveRefresh (below) can re-run just the
// media list on a live media.added/media.updated event, without refetching
// libraries/lists/collections/watch-state too. Errors are swallowed —
// leaving `items` at its last-good value beats blanking the grid on a
// background refresh hiccup (matches the original Promise.allSettled
// fire-and-forget-on-reject behavior).
async function loadItems() {
  try {
    const { $heya } = useNuxtApp()
    // /api/media/enriched wraps results in `{ movies, tv, type }` since the
    // API rewrite — unwrap the relevant branch.
    const res = await $heya('/api/media/enriched', { query: { type: 'movie', limit: 5000 } }) as { movies: EnrichedMediaItem[] | null }
    items.value = res.movies ?? []
  } catch { /* keep the last-good list */ }
}

// This page has no vue-query cache to invalidate — data lands in a plain
// ref via loadItems() — so useLiveRefresh's `refetch` escape hatch drives
// it directly instead of a `keys` invalidation.
useLiveRefresh([
  // Only refetch the grid once it's actually been loaded — on the Recommended
  // landing the item list is deferred and BrowseView refreshes its own rails.
  { events: ['media.added', 'media.updated'], filter: byMediaType('movie'), refetch: () => { if (itemsLoaded.value) loadItems() } },
])

onMounted(async () => {
  const { $heya } = useNuxtApp()
  const [libRes, stateRes, listsRes, colRes] = await Promise.allSettled([
    $heya('/api/libraries') as Promise<Library[]>,
    fetchUserState('movies'),
    $heya('/api/me/lists') as Promise<UserList[]>,
    $heya('/api/collections/browse') as Promise<CollectionBrowse[]>,
  ])
  if (libRes.status === 'fulfilled') libraries.value = libRes.value.filter(l => l.media_type === 'movie')
  if (stateRes.status === 'fulfilled') {
    favoritedSet.value = new Set(stateRes.value.favorited || [])
    watchedSet.value = new Set(stateRes.value.watched || [])
  }
  if (listsRes.status === 'fulfilled') userLists.value = listsRes.value
  if (colRes.status === 'fulfilled') collections.value = colRes.value ?? []

  // Grid/franchises need the full item list; the Recommended landing doesn't.
  if (activeView.value !== 'browse' && activeView.value !== 'recommendations') await ensureItems()
  loading.value = false

  // Re-validate the persisted sidebar selection against fresh data — a
  // deleted library/list/franchise would otherwise filter everything away.
  if (activeLib.value !== null && !libraries.value.some(l => l.id === activeLib.value)) activeLib.value = null
  if (activeView.value?.startsWith('list-') && !userLists.value.some(l => `list-${l.id}` === activeView.value)) activeView.value = null

  // Restored person/studio filters need their media-id sets refetched, and a
  // restored list view needs its items loaded, before scroll can be restored.
  if (filters.value.personIds.length || filters.value.studioIds.length) await onFiltersChange(filters.value)
  if (activeView.value) await syncActiveView(activeView.value)

  await nextTick()
  restoreScroll(mainEl.value)
})
</script>

<style scoped>
.lib-content { min-height: 200px; padding-top: 16px; }

/* Franchises overview (activeView === 'franchises') — reuses the global
   .grid-posters / .browse-detail-row / .list-row chrome so it matches the
   library views; only the franchise-specific column shape + link resets live
   here. */
.fr-grid { padding: 0 32px 80px; }
.fr-detail { display: flex; flex-direction: column; gap: 4px; }
/* Franchise list columns: name | films | added (the global .list-row is a
   5-column movie layout). Scoped, so it outranks the global rule. */
.fr-list-row { grid-template-columns: minmax(0, 3fr) 0.6fr 1fr; }
/* Detail/list rows are NuxtLinks here (movie rows are click-divs) — drop the
   anchor underline. */
.fr-detail .browse-detail-row,
.list-rows .fr-list-row { text-decoration: none; }
.grid-virt { /* container for usePosterGrid; width is the source of truth */ }
.grid-row { display: grid; column-gap: 18px; padding-bottom: 22px; }
/* Was inline `style="padding: 0 32px 80px"` on grid-virt/detail-virt/list-rows
   (and `0 32px` on the loading skeleton) — moved to classes so phone can
   override without fighting inline style specificity. */
.lib-pad { padding: 0 32px 80px; }
.lib-pad-top { padding: 0 32px; }
.empty-lib {
  display: flex; flex-direction: column; align-items: center; gap: 14px;
  padding: 90px 32px; text-align: center; color: var(--fg-2); font-size: 15px;
}
.empty-lib p { margin: 0; }
.empty-icon { opacity: 0.35; }
.unavailable { opacity: 0.4; cursor: default !important; }
/* Badges injected through MediaCard's slot stay in the parent's scope
   (Vue 3 keeps slotted content under the slot owner's data-v attribute).
   They absolutely position inside the Poster — the closest positioned
   ancestor — and sit above the gradient via z-index. Stack watched + fav
   in the top-right so they don't fight the title overlay at the bottom. */
.watched-badge, .fav-badge, .res-badge {
  position: absolute;
  z-index: 3;
  display: flex; align-items: center; justify-content: center;
  font-family: var(--font-mono);
  font-weight: 700;
}
.res-badge {
  top: 8px; left: 8px;
  font-size: 9px; text-transform: uppercase; letter-spacing: 0.06em;
  padding: 3px 7px; border-radius: 4px;
  background: rgba(0,0,0,0.6); backdrop-filter: blur(6px);
  color: var(--gold);
}
.watched-badge {
  top: 8px; right: 8px;
  width: 22px; height: 22px; border-radius: 50%;
  background: rgba(0,0,0,0.6); backdrop-filter: blur(6px);
  color: var(--good);
}
.fav-badge {
  top: 8px; right: 36px;
  width: 22px; height: 22px; border-radius: 50%;
  background: rgba(0,0,0,0.6); backdrop-filter: blur(6px);
  color: var(--bad);
}
.list-genres { font-size: 12px; color: var(--fg-3); max-width: 160px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* ── Phone (<=720px) ─────────────────────────────────────────────────
   Grid gap/padding here must track usePosterGrid.ts's phone constants
   (MIN_CARD_PHONE/COL_GAP_PHONE/ROW_GAP_PHONE) — the JS column math and the
   actual rendered gap have to agree or RecycleScroller misjudges row
   height. List/detail rows collapse to the same stacked-card + "..." sheet
   language as TrackList's phone rows (docs/responsive-plan.md W2a/W3b). */
@media (max-width: 720px) {
  .lib-pad { padding: 0 12px 90px; }
  .lib-pad-top { padding: 0 12px; }
  .fr-head { padding: 14px 12px 10px; }
  .fr-grid { padding: 0 12px 90px; }
  .grid-row { column-gap: 10px; padding-bottom: 14px; }

  .list-row-phone {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 8px;
  }
  .list-phone-main { flex: 1; min-width: 0; }
  .list-phone-more {
    flex-shrink: 0;
    width: 44px; height: 44px;
    display: flex; align-items: center; justify-content: center;
    background: transparent; border: 0; border-radius: var(--r-sm);
    color: var(--fg-2); cursor: pointer;
  }
  .list-phone-more:active { background: rgba(255, 255, 255, 0.06); color: var(--fg-0); }

  /* Detail view collapses to the same header row as list, plus a genre-chip
     row underneath — the overview paragraph drops to keep row height sane
     in a virtualized, fixed-item-size list. */
  .browse-detail-row-phone { flex-direction: column; align-items: stretch; gap: 8px; padding: 10px 8px; }
  .bdr-top { display: flex; align-items: center; gap: 12px; }
  .bdr-top-text { flex: 1; min-width: 0; }
  .bdr-title {
    display: flex; align-items: center; gap: 4px;
    font-size: 14px; font-weight: 500; color: var(--fg-0);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .bdr-meta {
    display: flex; align-items: center; gap: 8px; margin-top: 3px;
    font-size: 11px; color: var(--fg-3);
  }
  .bdr-meta .star { color: var(--gold); display: inline-flex; align-items: center; gap: 3px; }
  .bdr-more {
    flex-shrink: 0;
    width: 44px; height: 44px;
    display: flex; align-items: center; justify-content: center;
    background: transparent; border: 0; border-radius: var(--r-sm);
    color: var(--fg-2); cursor: pointer;
  }
  .bdr-more:active { background: rgba(255, 255, 255, 0.06); color: var(--fg-0); }
  .browse-detail-row-phone .browse-detail-genres { margin-top: 0; max-height: none; }
}
</style>
