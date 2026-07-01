<template>
  <div class="mt-layout">
    <LibrarySidebar
      :libraries="libraries"
      :active-lib="activeLib"
      :active-view="activeView"
      type-label="Shows"
      :total-count="items.length"
      :loved-count="favoritedSet.size"
      :user-lists="userLists"
      :drag-over-list-id="dragState.overListId"
      @select="activeLib = $event; activeView = null"
      @view="activeView = $event"
      @list-drop="onListDrop"
      @list-dragover="onListDragOver"
      @list-dragleave="onListDragLeave"
    />
    <div ref="mainEl" class="library-main scroll" @scroll.passive="onMainScroll">
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
                    :subtitle="item.year + (item.rating ? ` · ${item.rating.toFixed(1)}★` : '')"
                    :missing="item.available === false"
                  >
                    <template #badges>
                      <div v-if="item.resolution" class="res-badge">{{ item.resolution === '4k' ? '4K' : item.resolution }}</div>
                      <div v-if="isFullyWatched(item.id)" class="watched-badge"><Icon name="check" :size="10" /></div>
                      <div v-else-if="unwatchedCount(item.id) > 0 && unwatchedCount(item.id) < (showStates.get(item.id)?.total || 0)" class="unwatched-badge">{{ unwatchedCount(item.id) }}</div>
                      <div v-if="isFavorited(item.id)" class="fav-badge"><Icon name="heartfill" :size="10" /></div>
                    </template>
                  </MediaCard>
                </div>
              </AppContextMenu>
            </div>
          </RecycleScroller>
        </div>

        <div v-else-if="view === 'detail'" class="detail-virt" style="padding: 0 32px 80px">
          <RecycleScroller
            :items="sorted"
            :item-size="188"
            key-field="id"
            page-mode
            v-slot="{ item, index }"
          >
            <AppContextMenu :items="ctxItemsFor(item)">
              <div
                class="browse-detail-row"
                :class="{ unavailable: item.available === false }"
                draggable="true"
                @click="item.available !== false && navigateTo(mediaUrl(item))"
                @dragstart="onDragStart($event, item)"
                @dragend="onDragEnd"
              >
                <Poster :idx="index" :src="usePosterUrl(item.id)" class-name="browse-detail-poster" :width="120" />
                <div class="browse-detail-body">
                  <div class="browse-detail-title">
                    <span>{{ item.title }}</span>
                    <Icon v-if="isFullyWatched(item.id)" name="check" :size="14" style="color: var(--good); flex-shrink: 0" />
                    <Icon v-if="isFavorited(item.id)" name="heartfill" :size="14" style="color: var(--bad); flex-shrink: 0" />
                  </div>
                  <div class="browse-detail-meta">
                    <span>{{ item.year }}</span>
                    <span v-if="item.number_of_seasons">{{ item.number_of_seasons }} {{ item.number_of_seasons === 1 ? 'season' : 'seasons' }}</span>
                    <span v-if="item.number_of_episodes">{{ item.number_of_episodes }} eps</span>
                    <span v-if="item.rating" class="star"><Icon name="star" :size="11" weight="fill" />{{ item.rating.toFixed(1) }}</span>
                    <span v-if="item.resolution" class="browse-detail-res">{{ item.resolution === '4k' ? '4K' : item.resolution }}</span>
                    <span v-if="unwatchedCount(item.id) > 0" class="browse-detail-unseen">{{ unwatchedCount(item.id) }} unseen</span>
                  </div>
                  <div v-if="item.genres?.length" class="browse-detail-genres">
                    <span v-for="g in item.genres.slice(0, 4)" :key="g" class="chip">{{ g }}</span>
                  </div>
                  <p v-if="item.description" class="browse-detail-overview">{{ item.description }}</p>
                </div>
              </div>
            </AppContextMenu>
          </RecycleScroller>
        </div>

        <div v-else class="list-rows" style="padding: 0 32px 80px">
          <div class="list-row list-row-head">
            <div>Title</div>
            <div>Year</div>
            <div>Rating</div>
            <div>Status</div>
            <div>Added</div>
          </div>
          <RecycleScroller
            :items="sorted"
            :item-size="70"
            key-field="id"
            page-mode
            v-slot="{ item }"
          >
            <AppContextMenu :items="ctxItemsFor(item)">
            <div
              class="list-row"
              @click="navigateTo(mediaUrl(item))"
            >
              <div class="list-title-cell">
                <Poster :idx="0" :src="usePosterUrl(item.id)" style="width: 36px; height: 54px; border-radius: 4px; flex-shrink: 0" />
                <div>
                  <div class="list-title">
                    {{ item.title }}
                    <Icon v-if="isFullyWatched(item.id)" name="check" :size="12" style="color: var(--good); margin-left: 4px" />
                    <Icon v-if="isFavorited(item.id)" name="heartfill" :size="12" style="color: var(--bad); margin-left: 2px" />
                  </div>
                  <div class="list-sub">{{ item.year }}</div>
                </div>
              </div>
              <div>{{ item.year }}</div>
              <div>{{ item.rating ? item.rating.toFixed(1) : '–' }}</div>
              <div class="list-status">{{ item.status || '–' }}</div>
              <div class="list-added">{{ formatDate(item.created_at) }}</div>
            </div>
            </AppContextMenu>
          </RecycleScroller>
        </div>

        <div v-if="!loading && !items.length" class="empty-lib">
          <Icon name="tv" :size="30" class="empty-icon" />
          <p>No TV shows found. Scan a library to discover content.</p>
        </div>
        <div v-else-if="!loading && !sorted.length" class="empty-lib">
          <Icon name="filter" :size="30" class="empty-icon" />
          <p>Nothing matches the current filters.</p>
          <button v-if="isDirty" class="btn btn-secondary" @click="resetBrowse">Reset filters</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { EnrichedMediaItem, Library, UserList, FilterState } from '~~/shared/types'
import { useCardContextItems } from '~/composables/useContextMenu'

const mainEl = ref<HTMLElement | null>(null)
const gridWrap = ref<HTMLElement | null>(null)
const items = ref<EnrichedMediaItem[]>([])
const libraries = ref<Library[]>([])
const userLists = ref<UserList[]>([])
const loading = ref(true)

// View mode, sort, filters, sidebar selection and scroll offset all persist —
// navigating into a show and back restores the page exactly as it was.
const browse = useBrowseState('tv')
const { view, sort, filters, activeLib, activeView, scrollTop } = browse
const { isDirty, restoreScroll } = browse

const showStates = ref<Map<number, { total: number; watched: number }>>(new Map())
const favoritedSet = ref<Set<number>>(new Set())
const watchedSet = ref<Set<number>>(new Set())

function isFullyWatched(id: number) {
  const s = showStates.value.get(id)
  return !!s && s.total > 0 && s.watched >= s.total
}
function unwatchedCount(id: number) {
  const s = showStates.value.get(id)
  if (!s || s.total === 0) return 0
  return s.total - s.watched
}
function isFavorited(id: number) { return favoritedSet.value.has(id) }

const personMediaIds = ref<Set<number>>(new Set())
const studioMediaIds = ref<Set<number>>(new Set())
const listItems = ref<Set<number>>(new Set())

const { buildItems: buildCardCtxItems } = useCardContextItems()
const { dragState, onDragStart, onDragEnd, onListDragOver, onListDragLeave, onListDrop } = useDragDrop()

const { $heya } = useNuxtApp()
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

const viewTitle = computed(() => {
  if (activeView.value === 'loved') return 'Loved Shows'
  if (activeView.value?.startsWith('list-')) {
    const list = userLists.value.find(l => `list-${l.id}` === activeView.value)
    return list?.name || 'List'
  }
  return 'TV Shows'
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
    if (userList?.list_type !== 'smart') {
      list = list.filter(i => listItems.value.has(i.id))
    }
  } else if (activeLib.value) {
    list = list.filter(i => i.library_id === activeLib.value)
  }
  // For TV, "watched" means fully watched
  const fullWatchedSet = new Set(
    [...showStates.value.entries()]
      .filter(([, s]) => s.total > 0 && s.watched >= s.total)
      .map(([id]) => id)
  )
  return applyFilters(list, filters.value, fullWatchedSet, personMediaIds.value, studioMediaIds.value)
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
    } catch { personMediaIds.value = new Set() }
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
    } catch { studioMediaIds.value = new Set() }
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
        media_type: 'tv',
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

function formatDate(d: string) {
  if (!d) return ''
  return new Date(d).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

onMounted(async () => {
  const { $heya } = useNuxtApp()
  const [mediaRes, libRes, stateRes, listsRes] = await Promise.allSettled([
    // /api/media/enriched wraps results in `{ movies, tv, type }` since the
    // API rewrite — unwrap the relevant branch.
    $heya('/api/media/enriched', { query: { type: 'tv', limit: 5000 } }) as Promise<{ tv: EnrichedMediaItem[] | null }>,
    $heya('/api/libraries') as Promise<Library[]>,
    fetchUserState('series'),
    $heya('/api/me/lists') as Promise<UserList[]>,
  ])
  if (mediaRes.status === 'fulfilled') items.value = mediaRes.value.tv ?? []
  if (libRes.status === 'fulfilled') libraries.value = libRes.value.filter(l => l.media_type === 'tv')
  if (stateRes.status === 'fulfilled') {
    const st = stateRes.value
    for (const s of (st.shows || [])) {
      showStates.value.set(s.media_item_id, { total: s.total_episodes, watched: s.watched_episodes })
    }
    favoritedSet.value = new Set(st.favorited || [])
  }
  if (listsRes.status === 'fulfilled') userLists.value = listsRes.value
  loading.value = false

  // Re-validate the persisted sidebar selection against fresh data — a
  // deleted library/list would otherwise filter everything away.
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
.grid-virt { /* container for usePosterGrid; width is the source of truth */ }
.grid-row { display: grid; column-gap: 18px; padding-bottom: 22px; }
.empty-lib {
  display: flex; flex-direction: column; align-items: center; gap: 14px;
  padding: 90px 32px; text-align: center; color: var(--fg-2); font-size: 15px;
}
.empty-lib p { margin: 0; }
.empty-icon { opacity: 0.35; }
.unavailable { opacity: 0.4; cursor: default !important; }
/* Badges injected through MediaCard's slot stay in the parent's scope.
   They absolutely position inside the Poster (closest positioned ancestor)
   and sit above the gradient via z-index. Stack the status icons in the
   top-right so the bottom title overlay stays clean. */
.watched-badge, .unwatched-badge, .fav-badge, .res-badge {
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
.watched-badge, .unwatched-badge {
  top: 8px; right: 8px;
  min-width: 22px; height: 22px;
  background: rgba(0,0,0,0.6); backdrop-filter: blur(6px);
}
.watched-badge { width: 22px; border-radius: 50%; color: var(--good); }
.unwatched-badge {
  padding: 0 7px; border-radius: 999px; font-size: 11px; color: var(--gold);
}
.fav-badge {
  top: 8px; right: 36px;
  width: 22px; height: 22px; border-radius: 50%;
  background: rgba(0,0,0,0.6); backdrop-filter: blur(6px);
  color: var(--bad);
}
.list-status { font-size: 12px; color: var(--fg-3); }
.browse-detail-unseen { color: var(--gold); font-size: 11px; }
</style>
