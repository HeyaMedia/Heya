<template>
  <AppDialog
    :model-value="show"
    :title="`Identify — ${album?.title || ''}`"
    size="lg"
    @update:model-value="(v) => v ? null : $emit('close')"
  >
    <div class="mid-search-bar">
      <input v-model="query" type="text" class="mid-input" placeholder="Search album title..." @keydown.enter="search" />
      <button class="btn btn-primary" :disabled="searching || !query.trim()" @click="search">
        {{ searching ? 'Searching...' : 'Search' }}
      </button>
    </div>
    <div class="mid-results scroll">
      <div v-if="searching" class="mid-empty">Searching heya.media albums...</div>
      <div v-else-if="searched && !results.length" class="mid-empty">No results found</div>
      <div
        v-for="r in results"
        :key="r.provider_id"
        class="mid-result"
      >
        <NuxtImg v-if="r.poster_url" :src="r.poster_url" class="mid-cover" />
        <div v-else class="mid-cover mid-cover-empty">
          <Icon name="music" :size="18" />
        </div>
        <div class="mid-info">
          <div class="mid-result-head">
            <span class="mid-result-title">{{ r.title }}</span>
            <span v-if="r.year" class="mid-result-year">{{ r.year }}</span>
          </div>
          <div class="mid-result-provider">
            <span class="mid-badge">{{ r.provider_name }}</span>
            <span class="mid-provider-id">{{ r.provider_id }}</span>
          </div>
          <div v-if="r.description" class="mid-desc">{{ r.description }}</div>
        </div>
        <button class="btn btn-secondary mid-apply-btn" @click="apply(r)">Apply</button>
      </div>
    </div>
  </AppDialog>
</template>

<script setup lang="ts">
import type { ProviderSearchResult } from '~~/shared/types'

const props = defineProps<{
  album: { id: number; title: string } | null
  show: boolean
}>()
const emit = defineEmits<{ applied: []; close: [] }>()

const query = ref('')
const searching = ref(false)
const searched = ref(false)
const results = ref<ProviderSearchResult[]>([])

const { $heya } = useNuxtApp()

watch(() => props.show, (v) => {
  if (v) {
    query.value = props.album?.title || ''
    searched.value = false
    results.value = []
    if (query.value) search()
  }
})

async function search() {
  if (!props.album || !query.value.trim()) return
  searching.value = true
  searched.value = true
  try {
    const res = await $heya('/api/music/albums/{id}/identify', {
      path: { id: props.album.id },
      query: { q: query.value },
    }) as { results: ProviderSearchResult[] }
    results.value = res.results || []
  } catch { results.value = [] }
  searching.value = false
}

async function apply(r: ProviderSearchResult) {
  if (!props.album) return
  const ok = await useConfirm().confirm({
    title: `Pin album to "${r.title}"?`,
    message: 'The album is pinned to this MusicBrainz release group and the artist re-fetches — title, year, label and cover adopt from the new record.',
    confirmLabel: 'Pin & refresh',
    destructive: true,
  })
  if (!ok) return
  try {
    await $heya('/api/music/albums/{id}/identify', {
      method: 'POST',
      path: { id: props.album.id },
      body: { provider_name: r.provider_name, provider_id: r.provider_id } as any,
    })
    emit('applied')
  } catch { /* empty */ }
}
</script>

<style scoped>
.mid-search-bar {
  display: flex; gap: 8px;
  padding-bottom: 14px; margin-bottom: 6px;
  border-bottom: 1px solid var(--border);
}
.mid-input {
  height: 36px; border: 1px solid var(--border); border-radius: var(--r-sm);
  background: var(--bg-3); color: var(--fg-0); font-size: 13px; padding: 0 10px;
  outline: none; flex: 1;
}
.mid-input:focus { border-color: var(--gold); }
.mid-results { max-height: 56vh; overflow-y: auto; }
.mid-empty {
  display: flex; align-items: center; justify-content: center;
  padding: 48px 0; color: var(--fg-3); font-size: 13px;
}
.mid-result {
  display: flex; align-items: flex-start; gap: 14px;
  padding: 14px 20px; transition: background 0.12s;
  border-bottom: 1px solid rgb(var(--ink) / 0.03);
}
.mid-result:hover { background: rgb(var(--ink) / 0.02); }
.mid-cover {
  width: 64px; height: 64px; border-radius: var(--r-sm); object-fit: cover;
  flex-shrink: 0; background: var(--bg-3);
}
.mid-cover-empty {
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-3);
}
.mid-info { flex: 1; min-width: 0; }
.mid-result-head { display: flex; align-items: baseline; gap: 8px; }
.mid-result-title { font-size: 15px; font-weight: 600; color: var(--fg-0); }
.mid-result-year { font-size: 13px; color: var(--fg-2); }
.mid-result-provider {
  display: flex; align-items: center; gap: 8px; margin-top: 4px;
}
.mid-badge {
  padding: 2px 6px; border-radius: 4px; font-size: 10px; font-weight: 700;
  text-transform: uppercase; background: rgba(100,150,230,0.15); color: rgb(100,150,230);
}
.mid-provider-id { font-size: 11px; color: var(--fg-3); font-family: var(--font-mono); }
.mid-desc {
  font-size: 12px; color: var(--fg-3); margin-top: 6px; line-height: 1.4;
  display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
}
.mid-apply-btn { flex-shrink: 0; align-self: center; }
</style>
