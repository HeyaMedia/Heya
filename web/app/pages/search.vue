<template>
  <div class="scroll page-pad" style="height: 100%">
    <h1 style="font-size: 30px; font-weight: 600; margin-bottom: 24px">
      Search results for "{{ query }}"
    </h1>

    <div v-if="loading" style="color: var(--fg-2); font-size: 14px">Searching…</div>

    <div v-else-if="results.length">
      <div class="grid-posters" style="margin-bottom: 40px">
        <div
          v-for="(item, i) in results"
          :key="item.id"
          class="grid-tile card-tile"
          @click="navigateTo(mediaUrl(item))"
        >
          <Poster :idx="i" :src="usePosterUrl(item.id)" :aspect="item.media_type === 'music' ? '1/1' : '2/3'" />
          <div class="grid-tile-meta">
            <div class="grid-tile-title">{{ item.title }}</div>
            <div class="grid-tile-sub">
              <span style="text-transform: uppercase; font-size: 10px; color: var(--gold)">{{ item.media_type }}</span>
              &middot; {{ item.year }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-else style="text-align: center; padding: 60px 0; color: var(--fg-2)">
      <p style="font-size: 16px">No results found</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'


const route = useRoute()
const query = computed(() => (route.query.q as string) || '')
const results = ref<MediaItem[]>([])
const loading = ref(true)

watch(query, async (q) => {
  if (!q) { results.value = []; loading.value = false; return }
  loading.value = true
  try {
    results.value = await apiFetch<MediaItem[]>(`/api/search?q=${encodeURIComponent(q)}`)
  } catch { results.value = [] }
  loading.value = false
}, { immediate: true })
</script>
