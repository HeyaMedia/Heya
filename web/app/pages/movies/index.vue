<template>
  <div class="mt-layout">
    <LibrarySidebar
      :libraries="libraries"
      :active-lib="activeLib"
      type-label="Movies"
      :total-count="items.length"
      @select="activeLib = $event"
    />
    <div class="library-main scroll">
      <LibraryToolbar
        title="Movies"
        :count="items.length"
        :sort="sort"
        :view="view"
        @sort="sort = $event"
        @view="view = $event"
      />

      <div class="lib-content">
        <div v-if="loading" class="grid-posters" style="padding: 0 32px">
          <div v-for="i in 12" :key="i" class="grid-tile">
            <div class="poster" style="aspect-ratio: 2/3; background: var(--bg-3); animation: pulse 1.5s infinite" />
          </div>
        </div>

        <div v-else-if="view === 'grid'" class="grid-posters" style="padding: 0 32px 80px">
          <div
            v-for="(item, i) in sorted"
            :key="item.id"
            class="grid-tile card-tile"
            :class="{ unavailable: item.available === false }"
            @click="item.available !== false && navigateTo(mediaUrl(item))"
          >
            <div style="position: relative">
              <Poster :idx="i" :src="usePosterUrl(item.id)" :aspect="'2/3'" />
              <div v-if="item.available === false" class="missing-badge">Missing</div>
            </div>
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ item.title }}</div>
              <div class="grid-tile-sub">{{ item.year }}</div>
            </div>
          </div>
        </div>

        <div v-else class="list-rows" style="padding: 0 32px 80px">
          <div class="list-row list-row-head">
            <div>Title</div>
            <div>Year</div>
            <div>Added</div>
          </div>
          <div
            v-for="item in sorted"
            :key="item.id"
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
        </div>

        <div v-if="!loading && !items.length" class="empty-lib">
          <p>No movies found. Scan a library to discover content.</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem, Library } from '~~/shared/types'


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

function formatDate(d: string) {
  if (!d) return ''
  return new Date(d).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

onMounted(async () => {
  const [mediaRes, libRes] = await Promise.allSettled([
    apiFetch<MediaItem[]>('/api/media?type=movie&limit=500'),
    apiFetch<Library[]>('/api/libraries'),
  ])
  if (mediaRes.status === 'fulfilled') items.value = mediaRes.value
  if (libRes.status === 'fulfilled') libraries.value = libRes.value.filter(l => l.media_type === 'movie')
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
</style>
