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
    <div class="library-main scroll">
      <FilterBar
        :title="viewTitle"
        :count="sorted.length"
        :sort="sort"
        :view="view"
        :filters="filters"
        :available-genres="availableGenres"
        :available-languages="availableLanguages"
        @sort="sort = $event"
        @view="view = $event"
        @update:filters="onFiltersChange"
        @save-list="saveSmartList"
      />

      <div class="lib-content">
        <div v-if="loading" class="grid-posters" style="padding: 0 32px">
          <div v-for="i in 12" :key="i" class="grid-tile">
            <div class="poster" style="aspect-ratio: 2/3; background: var(--bg-3)" />
          </div>
        </div>

        <div v-else-if="view === 'grid'" class="grid-posters" style="padding: 0 32px 80px">
          <div
            v-for="(item, i) in sorted"
            :key="item.id"
            class="grid-tile card-tile"
            :class="{ unavailable: item.available === false }"
            draggable="true"
            @click="item.available !== false && navigateTo(mediaUrl(item))"
            @contextmenu.prevent="openContextMenu($event, item)"
            @dragstart="onDragStart($event, item)"
            @dragend="onDragEnd"
          >
            <div style="position: relative">
              <Poster :idx="i" :src="usePosterUrl(item.id)" :aspect="'2/3'" />
              <div v-if="item.available === false" class="missing-badge">Missing</div>
              <div v-if="isFullyWatched(item.id)" class="watched-badge"><Icon name="check" :size="10" /></div>
              <div v-else-if="unwatchedCount(item.id) > 0 && unwatchedCount(item.id) < (showStates.get(item.id)?.total || 0)" class="unwatched-badge">{{ unwatchedCount(item.id) }}</div>
              <div v-if="isFavorited(item.id)" class="fav-badge"><Icon name="heartfill" :size="10" /></div>
              <div v-if="item.resolution" class="res-badge">{{ item.resolution === '4k' ? '4K' : item.resolution }}</div>
            </div>
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ item.title }}</div>
              <div class="grid-tile-sub">{{ item.year }}<template v-if="item.rating"> · {{ item.rating.toFixed(1) }}★</template></div>
            </div>
          </div>
        </div>

        <div v-else class="list-rows" style="padding: 0 32px 80px">
          <div class="list-row list-row-head">
            <div>Title</div>
            <div>Year</div>
            <div>Rating</div>
            <div>Status</div>
            <div>Added</div>
          </div>
          <div
            v-for="item in sorted"
            :key="item.id"
            class="list-row"
            @click="navigateTo(mediaUrl(item))"
            @contextmenu.prevent="openContextMenu($event, item)"
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
        </div>

        <div v-if="!loading && !items.length" class="empty-lib">
          <p>No TV shows found. Scan a library to discover content.</p>
        </div>
      </div>
    </div>

    <ContextMenu
      :items="menuState.items"
      :x="menuState.x"
      :y="menuState.y"
      :visible="menuState.visible"
      @close="closeMenu"
    />
  </div>
</template>

<script setup lang="ts">
import type { EnrichedMediaItem, Library, UserList, FilterState } from '~~/shared/types'

const items = ref<EnrichedMediaItem[]>([])
const libraries = ref<Library[]>([])
const userLists = ref<UserList[]>([])
const loading = ref(true)
const activeLib = ref<number | null>(null)
const activeView = ref<string | null>(null)
const sort = ref('title')
const view = ref('grid')
const filters = ref<FilterState>(defaultFilters())

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

const { menuState, showMenu, closeMenu } = useContextMenu()
const { dragState, onDragStart, onDragEnd, onListDragOver, onListDragLeave, onListDrop } = useDragDrop()

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

watch(activeView, async (v) => {
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
})

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

function openContextMenu(event: MouseEvent, item: EnrichedMediaItem) {
  const { $heya } = useNuxtApp()
  showMenu(event, item, {
    watchedSet: watchedSet.value,
    favoritedSet: favoritedSet.value,
    userLists: userLists.value,
    onToggleWatched: async (id, watched) => {
      try {
        await $heya('/api/me/watched/media/{id}', {
          method: 'POST',
          path: { id },
          body: { watched } as any,
        })
      } catch { /* ignore */ }
    },
    onToggleFavorite: async (id, favorited) => {
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
    onAddToList: async (listId, mediaId) => {
      try {
        await $heya('/api/me/lists/{id}/items', {
          method: 'POST',
          path: { id: listId },
          body: { media_item_id: mediaId } as any,
        })
      } catch { /* ignore */ }
    },
  })
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
})
</script>

<style scoped>
.lib-content { min-height: 200px; }
.empty-lib { padding: 80px 32px; text-align: center; color: var(--fg-2); font-size: 15px; }
.unavailable { opacity: 0.4; cursor: default !important; }
.unavailable:hover .grid-tile-title { color: inherit !important; }
.missing-badge {
  position: absolute; top: 8px; right: 8px;
  font-size: 9px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.08em;
  padding: 3px 8px; border-radius: 100px;
  background: rgba(217,107,107,0.85); color: #fff;
}
.watched-badge {
  position: absolute; bottom: 8px; right: 8px;
  width: 24px; height: 24px; border-radius: var(--r-sm);
  background: rgba(0,0,0,0.65); color: var(--good);
  display: flex; align-items: center; justify-content: center;
}
.unwatched-badge {
  position: absolute; bottom: 8px; right: 8px;
  min-width: 22px; height: 22px; padding: 0 6px;
  border-radius: var(--r-sm); font-size: 11px; font-weight: 700;
  font-family: var(--font-mono);
  background: rgba(0,0,0,0.65); color: var(--gold);
  display: flex; align-items: center; justify-content: center;
}
.fav-badge {
  position: absolute; bottom: 8px; left: 8px;
  width: 24px; height: 24px; border-radius: var(--r-sm);
  background: rgba(0,0,0,0.65); color: var(--bad);
  display: flex; align-items: center; justify-content: center;
}
.res-badge {
  position: absolute; top: 8px; left: 8px;
  font-size: 9px; font-weight: 700; font-family: var(--font-mono);
  text-transform: uppercase; letter-spacing: 0.06em;
  padding: 2px 6px; border-radius: 4px;
  background: rgba(0,0,0,0.6); color: var(--gold);
}
.list-status { font-size: 12px; color: var(--fg-3); }
</style>
