<template>
  <div class="mt-layout">
    <LibrarySidebar
      v-if="!isPhone && !isCompact"
      :libraries="libraries"
      :active-lib="activeLib"
      :active-view="activeView"
      type-label="Movies"
      :show-browse="true"
      :show-roulette="true"
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
        :show-roulette="true"
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
    <BrowseView
      v-if="activeView === 'browse'"
      section="movie"
      class="library-main"
      :lib-title="libTitle"
      :lib-crumbs="libCrumbs"
      :ledger-cells="ledgerCells"
    />
    <RecsBrowse v-else-if="activeView === 'recommendations'" section="movie" class="library-main" />
    <RouletteView v-else-if="activeView === 'roulette'" class="library-main" />
    <div v-else ref="mainEl" class="library-main scroll" :class="{ 'has-alpha-rail': showAlphaRail }" @scroll.passive="onMainScroll">
      <!-- Library head + ledger scroll away above the sticky control bar. -->
      <LibHead :title="libTitle" :crumbs="libCrumbs" />
      <LedgerStrip v-if="ledgerCells.length" :cells="ledgerCells" canvas />

      <!-- Franchises overview — a page of its own (/movies/franchises). Reuses
           the FilterBar (sort + grid/detail/list toggle, no movie filters) and
           the same view chrome as the library; cards/rows deep-link into the
           per-franchise browse view (/movies/collection/N). -->
      <template v-if="activeView === 'franchises'">
        <FilterBar
          title="Franchises"
          hide-title
          count-label="franchises"
          :count="sortedFranchises.length"
          :sort="franchiseSort"
          :view="view"
          :filters="filters"
          :available-genres="[]"
          :available-languages="[]"
          :sort-options="FRANCHISE_SORTS"
          hide-filters
          :tile-size="tileSize"
          @sort="franchiseSort = $event"
          @view="view = $event"
          @tile-size="tileSize = $event"
        />

        <div class="lib-content">
          <div v-if="!sortedFranchises.length" class="empty-lib">
            <Icon name="film" :size="30" class="empty-icon" />
            <p>No franchises with more than one film yet.</p>
          </div>

          <!-- Non-virtualized grid: the slider drives it through the same
               --tile-min var the .grid-posters CSS sizes columns from. -->
          <div v-else-if="view === 'grid'" class="grid-posters fr-grid" :style="{ '--tile-min': `${tileSize}px` }">
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
      <!-- A–Z rail dock: FIRST child so its sticky anchor is the container's
           very top in every scroll state; the rail itself measures the
           FilterBar and hangs below it (see AlphabetRail). -->
      <div v-if="showAlphaRail" class="alpha-dock">
        <AlphabetRail :available="alphaAvailable" @jump="jumpToLetter" />
      </div>

      <FilterBar
        :title="viewTitle"
        hide-title
        :count="sorted.length"
        :sort="sort"
        :view="view"
        :filters="filters"
        :available-genres="availableGenres"
        :available-languages="availableLanguages"
        :genre-counts="genreCounts"
        :dirty="isDirty"
        :tile-size="tileSize"
        @sort="sort = $event"
        @view="view = $event"
        @update:filters="onFiltersChange"
        @save-list="saveSmartList"
        @reset="resetBrowse"
        @tile-size="tileSize = $event"
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
                  :data-prefetch-to="item.available !== false ? mediaUrl(item) : undefined"
                  :class="{ unavailable: item.available === false }"
                  draggable="true"
                  role="link"
                  :tabindex="item.available !== false ? 0 : -1"
                  @click="item.available !== false && navigateTo(mediaUrl(item))"
                  @keydown.enter="item.available !== false && navigateTo(mediaUrl(item))"
                  @keydown.space.prevent="item.available !== false && navigateTo(mediaUrl(item))"
                  @dragstart="onDragStart($event, item)"
                  @dragend="onDragEnd"
                >
                  <MediaCard
                    :idx="rowIdx * gridCols + colIdx"
                    :src="usePosterUrl(item)"
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
                :data-prefetch-to="item.available !== false ? mediaUrl(item) : undefined"
                :class="{ unavailable: item.available === false, 'browse-detail-row-phone': isPhone }"
                draggable="true"
                role="link"
                :tabindex="item.available !== false ? 0 : -1"
                @click="item.available !== false && navigateTo(mediaUrl(item))"
                @keydown.enter="item.available !== false && navigateTo(mediaUrl(item))"
                @keydown.space.prevent="item.available !== false && navigateTo(mediaUrl(item))"
                @dragstart="onDragStart($event, item)"
                @dragend="onDragEnd"
              >
                <template v-if="isPhone">
                  <div class="bdr-top">
                    <Poster :idx="index" :src="usePosterUrl(item)" style="width: 52px; height: 78px; border-radius: 4px; flex-shrink: 0" />
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
                  <Poster :idx="index" :src="usePosterUrl(item)" class-name="browse-detail-poster" :width="120" />
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
              :data-prefetch-to="mediaUrl(item)"
              :class="{ 'list-row-phone': isPhone }"
              role="link"
              tabindex="0"
              @click="navigateTo(mediaUrl(item))"
              @keydown.enter="navigateTo(mediaUrl(item))"
              @keydown.space.prevent="navigateTo(mediaUrl(item))"
            >
              <template v-if="isPhone">
                <Poster :idx="0" :src="usePosterUrl(item)" style="width: 44px; height: 66px; border-radius: 4px; flex-shrink: 0" />
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
                  <Poster :idx="0" :src="usePosterUrl(item)" style="width: 36px; height: 54px; border-radius: 4px; flex-shrink: 0" />
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
import type { LedgerCell } from '~/components/ui/LedgerStrip.vue'
import type { Crumb } from '~/components/library/LibHead.vue'
import { useCardContextItems } from '~/composables/useContextMenu'
import { useQuery, useQueryCache } from '@pinia/colada'
import {
  collectionsBrowseQuery,
  enrichedCatalogQuery,
  librariesQuery,
  movieUserStateQuery,
  userListsQuery,
} from '~/queries/catalog'

// Stable page key shared with the browse sub-routes registered in
// app/router.options.ts (/movies/library/:id, /movies/loved, …) so switching
// the sidebar selection reuses this component instead of remounting + refetching.
definePageMeta({ key: 'browse-movies' })

// Ambient background: this page owns a cycling movie-artwork pool with the
// content veil (see useBackground.ts) — never stuck on whatever the home
// hero showed last.
useBackground().pool('movie')

const mainEl = ref<HTMLElement | null>(null)
const gridWrap = ref<HTMLElement | null>(null)
const loading = ref(true)

const librariesData = useQuery(librariesQuery())
const listsData = useQuery(userListsQuery())
const collectionsData = useQuery(collectionsBrowseQuery())
const userStateData = useQuery(movieUserStateQuery())
const libraries = computed<Library[]>(() => (librariesData.data.value ?? []).filter(l => l.media_type === 'movie'))
const userLists = computed<UserList[]>(() => listsData.data.value ?? [])
const collections = computed<CollectionBrowse[]>(() => collectionsData.data.value ?? [])
const queryCache = useQueryCache()

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
const { view, sort, filters, activeLib, activeView, scrollTop, tileSize } = browse
const { isDirty, restoreScroll } = browse

// The Recommended landing (bare /movies) renders rails from their own queries
// and never needs the full item list, so defer the up-to-5000-item /enriched
// fetch until a grid/franchises view is actually entered.
const catalogEnabled = ref(false)
const catalogData = useQuery(() => ({
  ...enrichedCatalogQuery('movie'),
  enabled: catalogEnabled.value,
}))
const items = computed<EnrichedMediaItem[]>(() => catalogData.data.value ?? [])
const itemsLoaded = computed(() => catalogData.data.value !== undefined)
async function ensureItems() {
  if (itemsLoaded.value) return
  loading.value = true
  catalogEnabled.value = true
  try { await catalogData.refetch() } catch { /* keep persisted/last-good data */ }
  loading.value = false
}
watch(activeView, (v) => { if (v !== 'browse' && v !== 'recommendations' && v !== 'roulette') ensureItems() })

const favoritedSet = ref<Set<number>>(new Set())
const watchedSet = ref<Set<number>>(new Set())
await Promise.allSettled([
  waitForQuery(librariesData),
  waitForQuery(listsData),
  waitForQuery(collectionsData),
  waitForQuery(userStateData),
])
favoritedSet.value = new Set(userStateData.data.value?.favorited ?? [])
watchedSet.value = new Set(userStateData.data.value?.watched ?? [])
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
        queryCache.setQueryData(['me', 'state', 'movies'], current => {
          const state = (current as { favorited?: number[]; watched?: number[] } | undefined) ?? {}
          return { ...state, watched: [...watchedSet.value] }
        })
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
        queryCache.setQueryData(['me', 'state', 'movies'], current => {
          const state = (current as { favorited?: number[]; watched?: number[] } | undefined) ?? {}
          return { ...state, favorited: [...favoritedSet.value] }
        })
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

// ── Library head (Heya 2.0 lib-head) ────────────────────────────────────
// Archivo title + mono breadcrumb. The bare Browse landing keeps the "Movies"
// wordmark; every grid view names itself (All / Loved / a library / a list).
const libTitle = computed(() => {
  if (activeView.value === 'browse') return 'Movies'
  if (activeLib.value) return libraries.value.find(l => l.id === activeLib.value)?.name || 'Movies'
  if (activeView.value === null) return 'All Movies'
  return viewTitle.value
})
const libCrumbs = computed<Crumb[]>(() => {
  if (activeView.value === 'browse') return [{ label: 'Library' }, { label: 'Film' }]
  const c: Crumb[] = [{ label: 'Movies', to: '/movies' }]
  if (activeLib.value) c.push({ label: 'Library' })
  else if (activeView.value === 'loved') c.push({ label: 'Loved' })
  else if (activeView.value === 'franchises') c.push({ label: 'Franchises' })
  else if (activeView.value?.startsWith('list-')) c.push({ label: 'My List' })
  else c.push({ label: 'All movies' })
  return c
})

// Signature ledger — user-facing facts derived from the loaded catalog +
// watch/favorite state (never ops telemetry). Empty until the catalog resolves,
// so the strip fades in rather than flashing zeros.
const ledgerCells = computed<LedgerCell[]>(() => {
  if (!itemsLoaded.value) return []
  const list = items.value
  const total = list.length
  if (!total) return []
  const watched = list.filter(i => watchedSet.value.has(i.id)).length
  const runtime = list.reduce((s, i) => s + (i.runtime_minutes || 0), 0)
  const years = list.map(i => parseInt(i.year || '', 10)).filter(y => !Number.isNaN(y))
  const weekAgo = Date.now() - 7 * 24 * 3600 * 1000
  const addedWeek = list.filter(i => i.created_at && new Date(i.created_at).getTime() > weekAgo).length
  const cells: LedgerCell[] = [
    { k: 'Films', v: String(total) },
    { k: 'Unwatched', v: String(total - watched), tone: true },
    { k: 'Loved', v: String(favoritedSet.value.size) },
  ]
  if (runtime > 0) cells.push({ k: 'Runtime', v: fmtHoursTotal(runtime), sub: 'total' })
  if (years.length) cells.push({ k: 'Span', v: `${Math.min(...years)}–${Math.max(...years)}` })
  if (addedWeek > 0) cells.push({ k: 'Added', v: String(addedWeek), sub: 'this week', tone: true })
  return cells
})

function fmtHoursTotal(mins: number): string {
  const h = Math.round(mins / 60)
  return h >= 1 ? `${h}h` : `${mins}m`
}

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
      await onFiltersChange({ ...defaultFilters(), ...list.filter_json })
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

const { cols: gridCols, rowHeight, rows: gridRows } = usePosterGrid(gridWrap, sorted, { minCard: () => tileSize.value })

// ── Alphabet rail ──────────────────────────────────────────────────────
// First-character buckets over the title-sorted list; digits/symbols pool
// under '#'. Jumping forces title sort (the rail is meaningless against
// other orders), then scrolls the virtualized grid to the letter's first
// row, clearing the sticky FilterBar.
function alphaKey(item: { sort_title?: string; title: string }): string {
  const c = (item.sort_title || item.title || '').trim().charAt(0).toUpperCase()
  return c >= 'A' && c <= 'Z' ? c : '#'
}
const alphaAvailable = computed(() => [...new Set(sorted.value.map(alphaKey))])
const showAlphaRail = computed(() => view.value === 'grid' && sorted.value.length > 30)

function jumpToLetter(letter: string) {
  if (sort.value !== 'title') sort.value = 'title'
  nextTick(() => {
    const idx = sorted.value.findIndex(i => alphaKey(i) === letter)
    const main = mainEl.value
    const wrap = gridWrap.value
    if (idx < 0 || !main || !wrap) return
    const row = Math.floor(idx / gridCols.value)
    const barH = main.querySelector('.filter-bar')?.getBoundingClientRect().height ?? 0
    const top = wrap.getBoundingClientRect().top - main.getBoundingClientRect().top
      + main.scrollTop + row * rowHeight.value - barH - 10
    main.scrollTo({ top: Math.max(0, top) })
  })
}

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

const { promptText } = usePrompt()

async function saveSmartList() {
  const name = await promptText({
    title: 'Save as smart list',
    label: 'Smart list name',
    placeholder: 'Unwatched sci-fi',
    confirmLabel: 'Save list',
  })
  if (!name?.trim()) return
  const { toast } = useToast()
  try {
    const { $heya } = useNuxtApp()
    const created = await $heya('/api/me/lists', {
      method: 'POST',
      body: {
        name: name.trim(),
        description: '',
        list_type: 'smart',
        filter_json: filters.value,
        media_type: 'movie',
      } as any,
    }) as UserList
    await loadLists()
    activeView.value = `list-${created.id}`
    toast.ok(`Saved “${created.name}” in My Lists.`)
  } catch (e: any) {
    toast.err(e?.data?.detail || e?.message || 'Could not save the smart list.')
  }
}

async function loadLists() {
  try { await listsData.refetch() } catch { /* keep the last-good list */ }
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
useLiveRefresh([
  // Only refetch the grid once it's actually been loaded — on the Recommended
  // landing the item list is deferred and BrowseView refreshes its own rails.
  { events: ['media.added', 'media.updated'], filter: byMediaType('movie'), keys: [['media', 'catalog', 'movie']] },
])

onMounted(async () => {
  // Grid/franchises block on the full item list; the Browse/Recommendations/
  // Roulette landings render their own rails immediately. On the Browse landing
  // we still kick off the catalog fetch (unawaited) so the lib-head ledger +
  // sidebar counts fill in — the rails never wait on it.
  const needsItems = activeView.value !== 'browse' && activeView.value !== 'recommendations' && activeView.value !== 'roulette'
  if (needsItems) await ensureItems()
  else if (activeView.value === 'browse') void ensureItems()
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
  .list-phone-more:active { background: rgb(var(--ink) / 0.06); color: var(--fg-0); }

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
  .bdr-more:active { background: rgb(var(--ink) / 0.06); color: var(--fg-0); }
  .browse-detail-row-phone .browse-detail-genres { margin-top: 0; max-height: none; }
}
</style>
