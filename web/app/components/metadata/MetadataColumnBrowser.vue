<template>
  <div class="mtb">
    <div class="mtb-search">
      <input v-model="search" type="text" placeholder="Filter..." class="mtb-input" />
    </div>
    <div class="mtb-tree scroll">
      <div v-if="loadingLibs" class="mtb-empty">Loading...</div>
      <div v-for="lib in libraries" :key="lib.id" class="mtb-lib">
        <div class="mtb-row mtb-lib-row" :class="{ open: expandedLibs.has(lib.id) }" @click="toggleLib(lib.id)">
          <Icon :name="expandedLibs.has(lib.id) ? 'chevdown' : 'chevright'" :size="10" class="mtb-chevron" />
          <Icon :name="libIcon(lib.media_type)" :size="14" class="mtb-type-icon" :class="lib.media_type" />
          <span class="mtb-row-label">{{ lib.name }}</span>
          <span class="mtb-row-count">{{ libCounts[lib.id] || '' }}</span>
        </div>
        <div v-if="expandedLibs.has(lib.id)" class="mtb-children">
          <div v-if="libLoading[lib.id]" class="mtb-empty mtb-indent">Loading...</div>
          <template v-for="item in filteredMedia(lib.id)" :key="item.id">
            <div
              class="mtb-row mtb-item-row"
              :class="{ active: selectedMediaId === item.id, open: expandedItems.has(item.id) }"
              @click="selectItem(item)"
            >
              <Icon
                v-if="item.media_type === 'tv'"
                :name="expandedItems.has(item.id) ? 'chevdown' : 'chevright'"
                :size="10"
                class="mtb-chevron"
                @click.stop="toggleItem(item)"
              />
              <span v-else class="mtb-chevron-spacer" />
              <img
                v-if="item.poster_path"
                :src="`/api/media/${item.id}/image/poster`"
                class="mtb-thumb"
                @error="(e: Event) => (e.target as HTMLImageElement).style.display = 'none'"
              />
              <span class="mtb-row-label">{{ item.title }}</span>
              <span class="mtb-row-year">{{ item.year }}</span>
            </div>
            <div v-if="expandedItems.has(item.id) && itemSeasons[item.id]" class="mtb-children">
              <template v-for="season in itemSeasons[item.id]" :key="season.id">
                <div
                  class="mtb-row mtb-season-row"
                  :class="{ active: selectedSeasonId === season.id && !selectedEpisodeId, open: expandedSeasons.has(season.id) }"
                  @click="clickSeason(item, season)"
                >
                  <Icon :name="expandedSeasons.has(season.id) ? 'chevdown' : 'chevright'" :size="10" class="mtb-chevron" />
                  <span class="mtb-row-label">{{ season.title || `Season ${season.season_number}` }}</span>
                  <span class="mtb-row-count">{{ (season.episodes || []).length }}</span>
                </div>
                <div v-if="expandedSeasons.has(season.id)" class="mtb-children">
                  <div
                    v-for="ep in season.episodes || []"
                    :key="ep.id"
                    class="mtb-row mtb-ep-row"
                    :class="{ active: selectedEpisodeId === ep.id }"
                    @click="clickEpisode(item.id, ep.id)"
                  >
                    <span class="mtb-ep-num">{{ ep.episode_number }}</span>
                    <span class="mtb-row-label">{{ ep.title }}</span>
                  </div>
                </div>
              </template>
            </div>
          </template>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Library, MediaItem, MediaDetail } from '~~/shared/types'

const emit = defineEmits<{
  selectMedia: [id: number]
  selectSeason: [mediaId: number, seasonId: number]
  selectEpisode: [mediaId: number, episodeId: number]
}>()

const libraries = ref<Library[]>([])
const loadingLibs = ref(true)
const search = ref('')

const expandedLibs = ref(new Set<number>())
const expandedItems = ref(new Set<number>())
const expandedSeasons = ref(new Set<number>())
const selectedMediaId = ref<number | null>(null)
const selectedSeasonId = ref<number | null>(null)
const selectedEpisodeId = ref<number | null>(null)

const libMedia = ref<Record<number, MediaItem[]>>({})
const libLoading = ref<Record<number, boolean>>({})
const libCounts = ref<Record<number, number>>({})
const itemSeasons = ref<Record<number, any[]>>({})

function libIcon(type: string) {
  switch (type) {
    case 'movie': return 'film'
    case 'tv': return 'tv'
    case 'music': return 'music'
    case 'book': return 'book'
    default: return 'folder'
  }
}

function filteredMedia(libId: number): MediaItem[] {
  const items = libMedia.value[libId] || []
  if (!search.value) return items
  const q = search.value.toLowerCase()
  return items.filter(i => i.title.toLowerCase().includes(q))
}

async function toggleLib(id: number) {
  if (expandedLibs.value.has(id)) {
    expandedLibs.value.delete(id)
    expandedLibs.value = new Set(expandedLibs.value)
    return
  }
  expandedLibs.value.add(id)
  expandedLibs.value = new Set(expandedLibs.value)

  if (!libMedia.value[id]) {
    libLoading.value[id] = true
    try {
      const { $heya } = useNuxtApp()
      const items = await $heya('/api/libraries/{id}/media', { path: { id }, query: { limit: 2000 } }) as MediaItem[]
      libMedia.value[id] = items
      libCounts.value[id] = items.length
    } catch { libMedia.value[id] = [] }
    libLoading.value[id] = false
  }
}

async function toggleItem(item: MediaItem) {
  if (expandedItems.value.has(item.id)) {
    expandedItems.value.delete(item.id)
    expandedItems.value = new Set(expandedItems.value)
    return
  }
  expandedItems.value.add(item.id)
  expandedItems.value = new Set(expandedItems.value)

  if (!itemSeasons.value[item.id] && item.media_type === 'tv') {
    try {
      const { $heya } = useNuxtApp()
      const detail = await $heya('/api/media/{id}', { path: { id: String(item.id) } }) as MediaDetail
      itemSeasons.value[item.id] = (detail as any).seasons || []
    } catch { itemSeasons.value[item.id] = [] }
  }
}

function selectItem(item: MediaItem) {
  selectedMediaId.value = item.id
  selectedSeasonId.value = null
  selectedEpisodeId.value = null
  emit('selectMedia', item.id)
  if (item.media_type === 'tv' && !expandedItems.value.has(item.id)) {
    toggleItem(item)
  }
}

function clickSeason(item: MediaItem, season: any) {
  selectedMediaId.value = item.id
  selectedSeasonId.value = season.id
  selectedEpisodeId.value = null
  if (!expandedSeasons.value.has(season.id)) {
    expandedSeasons.value.add(season.id)
  } else {
    expandedSeasons.value.delete(season.id)
  }
  expandedSeasons.value = new Set(expandedSeasons.value)
  emit('selectSeason', item.id, season.id)
}

function clickEpisode(mediaId: number, episodeId: number) {
  selectedMediaId.value = mediaId
  selectedEpisodeId.value = episodeId
  emit('selectEpisode', mediaId, episodeId)
}

onMounted(async () => {
  try {
    const { $heya } = useNuxtApp()
    libraries.value = await $heya('/api/libraries') as Library[]
  } catch { /* empty */ }
  loadingLibs.value = false
})
</script>

<style scoped>
.mtb {
  width: 280px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  height: 100%;
  background: var(--bg-2);
  border-right: 1px solid var(--border);
}
.mtb-search { padding: 10px 10px 6px; }
.mtb-input {
  width: 100%; height: 30px; border: 1px solid var(--border);
  border-radius: var(--r-sm); background: var(--bg-3); color: var(--fg-1);
  font-size: 12px; padding: 0 8px; outline: none;
}
.mtb-input:focus { border-color: var(--gold); }
.mtb-tree {
  flex: 1; overflow-y: auto; padding: 4px 0;
}
.mtb-empty { padding: 16px 14px; font-size: 11px; color: var(--fg-3); }
.mtb-indent { padding-left: 28px; }

.mtb-row {
  display: flex; align-items: center; gap: 6px;
  padding: 5px 10px; cursor: pointer; transition: background 0.1s;
  position: relative; min-height: 28px;
}
.mtb-row:hover { background: rgba(255,255,255,0.03); }
.mtb-row.active {
  background: var(--gold-soft);
}
.mtb-row.active::before {
  content: ''; position: absolute; left: 0; top: 3px; bottom: 3px;
  width: 3px; border-radius: 2px; background: var(--gold);
}

.mtb-chevron { color: var(--fg-3); flex-shrink: 0; width: 14px; }
.mtb-chevron-spacer { width: 14px; flex-shrink: 0; }

.mtb-type-icon { flex-shrink: 0; color: var(--fg-3); }
.mtb-type-icon.movie { color: var(--gold); }
.mtb-type-icon.tv { color: rgb(100,150,230); }
.mtb-type-icon.music { color: rgb(180,100,230); }
.mtb-type-icon.book { color: rgb(74,180,130); }

.mtb-row-label {
  flex: 1; min-width: 0; font-size: 12px; font-weight: 500; color: var(--fg-1);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.mtb-row.active .mtb-row-label { color: var(--gold-bright); }
.mtb-row-count {
  font-size: 10px; color: var(--fg-3); font-family: var(--font-mono); flex-shrink: 0;
}
.mtb-row-year { font-size: 10px; color: var(--fg-3); flex-shrink: 0; }

.mtb-lib-row .mtb-row-label { font-weight: 600; font-size: 12px; }

.mtb-children { padding-left: 12px; }

.mtb-thumb {
  width: 20px; height: 30px; border-radius: 2px; object-fit: cover;
  flex-shrink: 0; background: var(--bg-3);
}

.mtb-item-row { padding-left: 22px; }
.mtb-season-row { padding-left: 34px; }
.mtb-season-row .mtb-row-label { font-size: 11px; color: var(--fg-2); }
.mtb-ep-row { padding-left: 48px; }
.mtb-ep-row .mtb-row-label { font-size: 11px; color: var(--fg-2); }
.mtb-ep-num {
  font-size: 10px; font-family: var(--font-mono); color: var(--fg-3);
  width: 20px; text-align: right; flex-shrink: 0;
}
</style>
