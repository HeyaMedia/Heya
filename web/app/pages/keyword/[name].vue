<template>
  <div class="scroll page-pad" style="height: 100%">
    <header class="genre-head">
      <div class="genre-eyebrow">Keyword</div>
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

    <div v-else-if="items.length" class="grid-posters">
      <NuxtLink
        v-for="(item, i) in items"
        :key="item.id"
        :to="mediaUrl(item)"
        class="grid-tile card-tile"
      >
        <Poster :idx="i" :src="usePosterUrl(item.id)" aspect="2/3" :title="item.title" />
        <div class="grid-tile-meta">
          <div class="grid-tile-title">{{ item.title }}</div>
          <div class="grid-tile-sub">{{ item.year }} · {{ mediaTypeLabel(item.media_type) }}</div>
        </div>
      </NuxtLink>
    </div>

    <div v-if="!loading && total > items.length" class="load-more-wrap">
      <button class="btn btn-secondary" @click="loadMore" :disabled="loadingMore">
        {{ loadingMore ? 'Loading…' : `Show more (${total - items.length} remaining)` }}
      </button>
    </div>

    <div v-if="!loading && !items.length" class="genre-empty">
      No media found with this keyword.
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'

const route = useRoute()
const name = computed(() => route.params.name as string)
const displayName = computed(() => decodeURIComponent(name.value).replace(/-/g, ' '))

const items = ref<MediaItem[]>([])
const total = ref(0)
const loading = ref(true)
const loadingMore = ref(false)
const PAGE_SIZE = 60

async function fetchKeyword(reset = true) {
  if (reset) {
    items.value = []
    total.value = 0
  }
  const offset = reset ? 0 : items.value.length
  const { $heya } = useNuxtApp()
  const res = await $heya('/api/keywords/{name}', {
    path: { name: name.value },
    query: { limit: PAGE_SIZE, offset },
  }) as { keyword: string; items: MediaItem[]; total: number }
  if (reset) {
    items.value = res.items || []
  } else {
    items.value = items.value.concat(res.items || [])
  }
  total.value = res.total || 0
}

async function loadMore() {
  loadingMore.value = true
  await fetchKeyword(false)
  loadingMore.value = false
}

onMounted(async () => {
  await fetchKeyword()
  loading.value = false
})

watch(name, async () => {
  loading.value = true
  await fetchKeyword()
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
</style>
