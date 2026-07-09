<template>
  <AppDialog
    :model-value="show"
    title="Search match"
    size="lg"
    @update:model-value="(v) => v ? null : $emit('close')"
  >
    <div class="ssd-search-bar">
      <input v-model="query" type="text" class="ssd-input" :placeholder="isURL ? 'Paste a Heya / TMDB / IMDb URL...' : 'Search title or paste a URL...'" @keydown.enter="search" />
      <input v-if="!isURL" v-model="year" type="text" class="ssd-input ssd-year" placeholder="Year" maxlength="4" @keydown.enter="search" />
      <button class="btn btn-primary" :disabled="searching || !query.trim()" @click="search">
        {{ searching ? (isURL ? 'Looking up...' : 'Searching...') : (isURL ? 'Look up' : 'Search') }}
      </button>
    </div>
    <div class="ssd-results scroll">
      <div v-if="searching" class="ssd-empty">Searching providers...</div>
      <div v-else-if="searched && !results.length" class="ssd-empty">No results found</div>
      <div
        v-for="r in results"
        :key="r.provider_id"
        class="ssd-result"
      >
        <NuxtImg v-if="r.poster_url" :src="r.poster_url" class="ssd-poster" />
        <div v-else class="ssd-poster ssd-poster-empty" />
        <div class="ssd-info">
          <div class="ssd-result-head">
            <span class="ssd-result-title">{{ r.title }}</span>
            <span v-if="r.year" class="ssd-result-year">{{ r.year }}</span>
          </div>
          <div class="ssd-result-provider">
            <span class="ssd-badge">{{ r.provider_name }}</span>
            <span class="ssd-provider-id">{{ r.provider_id }}</span>
          </div>
          <div v-if="r.description" class="ssd-desc">{{ r.description }}</div>
        </div>
        <button class="btn btn-secondary ssd-apply-btn" :disabled="assigning" @click="assign(r)">
          {{ assigning ? 'Matching...' : 'Use this' }}
        </button>
      </div>
    </div>
  </AppDialog>
</template>

<script setup lang="ts">
import type { ProviderSearchResult } from '~~/shared/types'

type ScannerSearchResult = ProviderSearchResult & {
  confidence?: number
  external_ids?: Record<string, string>
  heya_slug?: string
}

const props = defineProps<{
  libraryId: number
  identity: { id: number; title: string; year?: string } | null
  show: boolean
}>()
const emit = defineEmits<{ applied: [title: string]; close: [] }>()

const { $heya } = useNuxtApp()

const query = ref('')
const year = ref('')
const searching = ref(false)
const searched = ref(false)
const assigning = ref(false)
const results = ref<ScannerSearchResult[]>([])

const isURL = computed(() => {
  const q = query.value.trim()
  return /^https?:\/\//i.test(q) || /^heya(_[a-z]+)?:/i.test(q)
})

watch(() => props.show, (v) => {
  if (v) {
    query.value = props.identity?.title || ''
    year.value = props.identity?.year || ''
    searched.value = false
    results.value = []
    if (query.value.trim()) search()
  }
})

async function search() {
  if (!query.value.trim() || !props.identity) return
  searching.value = true
  searched.value = true
  try {
    const heya = $heya as any
    const q: Record<string, any> = { q: query.value }
    if (year.value && !isURL.value) q.year = year.value
    const res = await heya('/api/libraries/{id}/scanner/identities/{identity_id}/search', {
      path: { id: props.libraryId, identity_id: props.identity.id },
      query: q,
    }) as { results: ScannerSearchResult[] }
    results.value = res.results || []
  } catch { results.value = [] }
  searching.value = false
}

async function assign(r: ScannerSearchResult) {
  if (!props.identity) return
  const ok = await useConfirm().confirm({
    title: `Match as "${r.title}"?`,
    message: `${props.identity.title || 'This identity'} will be re-identified as ${r.title}${r.year ? ` (${r.year})` : ''} from ${r.provider_name}.`,
    confirmLabel: 'Match',
  })
  if (!ok) return
  assigning.value = true
  try {
    const heya = $heya as any
    await heya('/api/libraries/{id}/scanner/identities/{identity_id}/assign', {
      method: 'POST',
      path: { id: props.libraryId, identity_id: props.identity.id },
      body: {
        provider_name: r.provider_name,
        provider_id: r.provider_id,
        title: r.title,
        year: r.year || undefined,
        description: r.description || undefined,
        poster_url: r.poster_url || undefined,
        heya_slug: r.heya_slug || undefined,
        confidence: r.confidence || undefined,
        external_ids: r.external_ids && Object.keys(r.external_ids).length ? r.external_ids : undefined,
      } as any,
    })
    emit('applied', r.title)
  } catch { /* parent surfaces errors via refresh; keep dialog open */ }
  assigning.value = false
}
</script>

<style scoped>
/* AppDialog supplies overlay/panel/header chrome — only the layout
   for the search bar + results list inside the body lives here. */
.ssd-search-bar {
  display: flex; gap: 8px;
  padding-bottom: 14px; margin-bottom: 6px;
  border-bottom: 1px solid var(--border);
}
.ssd-input {
  height: 36px; border: 1px solid var(--border); border-radius: var(--r-sm);
  background: var(--bg-3); color: var(--fg-0); font-size: 13px; padding: 0 10px;
  outline: none; flex: 1;
}
.ssd-input:focus { border-color: var(--gold); }
.ssd-year { max-width: 80px; flex: none; }
.ssd-results { max-height: 56vh; overflow-y: auto; }
.ssd-empty {
  display: flex; align-items: center; justify-content: center;
  padding: 48px 0; color: var(--fg-3); font-size: 13px;
}
.ssd-result {
  display: flex; align-items: flex-start; gap: 14px;
  padding: 14px 20px; transition: background 0.12s;
  border-bottom: 1px solid rgba(255,255,255,0.03);
}
.ssd-result:hover { background: rgba(255,255,255,0.02); }
.ssd-poster {
  width: 56px; height: 84px; border-radius: var(--r-sm); object-fit: cover;
  flex-shrink: 0; background: var(--bg-3);
}
.ssd-poster-empty { display: flex; align-items: center; justify-content: center; }
.ssd-info { flex: 1; min-width: 0; }
.ssd-result-head { display: flex; align-items: baseline; gap: 8px; }
.ssd-result-title { font-size: 15px; font-weight: 600; color: var(--fg-0); }
.ssd-result-year { font-size: 13px; color: var(--fg-2); }
.ssd-result-provider {
  display: flex; align-items: center; gap: 8px; margin-top: 4px;
}
.ssd-badge {
  padding: 2px 6px; border-radius: 4px; font-size: 10px; font-weight: 700;
  text-transform: uppercase; background: rgba(100,150,230,0.15); color: rgb(100,150,230);
}
.ssd-provider-id { font-size: 11px; color: var(--fg-3); font-family: var(--font-mono); }
.ssd-desc {
  font-size: 12px; color: var(--fg-3); margin-top: 6px; line-height: 1.4;
  display: -webkit-box; -webkit-line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden;
}
.ssd-apply-btn { flex-shrink: 0; align-self: center; }
</style>
