<template>
  <AppDialog
    :model-value="show"
    title="Identify"
    size="lg"
    @update:model-value="(v) => v ? null : $emit('close')"
  >
    <div class="mid-search-bar">
      <input v-model="query" type="text" class="mid-input" placeholder="Search title or enter an IMDb / TMDB / TVDB / TVmaze ID or URL..." @keydown.enter="search" />
      <input v-if="!isDirectLookup" v-model="year" type="text" class="mid-input mid-year" placeholder="Year" maxlength="4" @keydown.enter="search" />
      <button class="btn btn-primary" :disabled="searching || !query.trim()" @click="search">
        {{ searching ? (isDirectLookup ? 'Looking up...' : 'Searching...') : (isDirectLookup ? 'Look up' : 'Search') }}
      </button>
    </div>
    <div class="mid-results scroll">
      <div v-if="searching" class="mid-empty">Searching Heya...</div>
      <div v-else-if="searched && !results.length" class="mid-empty">No results found</div>
      <div
        v-for="r in results"
        :key="r.provider_id"
        class="mid-result"
      >
        <LoadingImage v-if="r.poster_url" :src="r.poster_url" class="mid-poster" />
        <div v-else class="mid-poster mid-poster-empty" />
        <div class="mid-info">
          <div class="mid-result-head">
            <span class="mid-result-title">{{ r.title }}</span>
            <span v-if="r.year" class="mid-result-year">{{ r.year }}</span>
          </div>
          <div class="mid-result-provider">
            <span class="mid-badge">Heya</span>
            <span class="mid-provider-id">{{ resultIdentity(r) }}</span>
          </div>
          <div v-if="r.description" class="mid-desc">{{ r.description }}</div>
        </div>
        <button class="btn btn-secondary mid-apply-btn" :disabled="applyingId === r.provider_id" @click="apply(r)">
          {{ applyingId === r.provider_id ? 'Applying...' : 'Apply' }}
        </button>
      </div>
    </div>
  </AppDialog>
</template>

<script setup lang="ts">
import type { ProviderSearchResult } from '~~/shared/types'

const props = defineProps<{ mediaId: number; detail: any; show: boolean }>()
const emit = defineEmits<{ applied: []; close: [] }>()

const query = ref(props.detail?.media_item?.title || '')
const year = ref(props.detail?.media_item?.year || '')
const searching = ref(false)
const searched = ref(false)
const results = ref<ProviderSearchResult[]>([])
const applyingId = ref('')
const { toast } = useToast()

const isDirectLookup = computed(() => {
  const q = query.value.trim()
  return /^https?:\/\//i.test(q)
    || /^heya(_[a-z]+)?:/i.test(q)
    || /^(?:imdb:)?tt\d+$/i.test(q)
    || /^(?:tmdb|tvdb|tvmaze):\d+$/i.test(q)
})

watch(() => props.show, (v) => {
  if (v) {
    query.value = props.detail?.media_item?.title || ''
    year.value = props.detail?.media_item?.year || ''
    searched.value = false
    results.value = []
  }
})

async function search() {
  if (!query.value.trim()) return
  searching.value = true
  searched.value = true
  try {
    const { $heya } = useNuxtApp()
    const q: Record<string, any> = { q: query.value }
    if (year.value && !isDirectLookup.value) q.year = year.value
    const res = await $heya('/api/media/{id}/identify', {
      path: { id: props.mediaId },
      query: q,
    }) as { results: ProviderSearchResult[] }
    results.value = res.results || []
  } catch (error) {
    results.value = []
    toast.err(apiErrorMessage(error, 'Heya search failed'), { duration: 7000 })
  }
  searching.value = false
}

async function apply(r: ProviderSearchResult) {
  const ok = await useConfirm().confirm({
    title: `Replace metadata with "${r.title}"?`,
    message: 'This changes the canonical Heya identity and refreshes the metadata attached to it.',
    confirmLabel: 'Replace',
    destructive: true,
  })
  if (!ok) return
  applyingId.value = r.provider_id
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/media/{id}/identify', {
      method: 'POST',
      path: { id: props.mediaId },
      body: { provider_name: r.provider_name, provider_id: r.provider_id } as any,
    })
    emit('applied')
  } catch (error) {
    toast.err(apiErrorMessage(error, 'Could not apply the selected Heya identity'), { duration: 7000 })
  } finally {
    applyingId.value = ''
  }
}

function resultIdentity(result: ProviderSearchResult): string {
  const value = (result as any).heya_slug || result.provider_id || ''
  const uuid = value.match(/[0-9a-f]{8}-[0-9a-f-]{27,}/i)?.[0]
  return uuid || 'Canonical result'
}
</script>

<style scoped>
/* AppDialog supplies overlay/panel/header chrome — only the layout
   for the search bar + results list inside the body lives here. */
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
.mid-year { max-width: 80px; flex: none; }
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
.mid-poster {
  width: 56px; height: 84px; border-radius: var(--r-sm); object-fit: cover;
  flex-shrink: 0; background: var(--bg-3);
}
.mid-poster-empty { display: flex; align-items: center; justify-content: center; }
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
  display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
}
.mid-apply-btn { flex-shrink: 0; align-self: center; }
</style>
