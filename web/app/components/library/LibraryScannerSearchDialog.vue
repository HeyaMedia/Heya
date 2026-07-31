<template>
  <AppDialog
    :model-value="show"
    title="Search match"
    size="lg"
    @update:model-value="(v) => v ? null : $emit('close')"
  >
    <div class="ssd-search-bar">
      <input v-model="query" type="text" class="ssd-input" placeholder="Search title or enter an IMDb / TMDB / TVDB / TVmaze ID or URL..." @keydown.enter="search" />
      <input v-if="!isDirectLookup" v-model="year" type="text" class="ssd-input ssd-year" placeholder="Year" maxlength="4" @keydown.enter="search" />
      <button class="btn btn-primary" :disabled="searching || !query.trim()" @click="search">
        {{ searching ? (isDirectLookup ? 'Looking up...' : 'Searching...') : (isDirectLookup ? 'Look up' : 'Search') }}
      </button>
    </div>
    <div class="ssd-results scroll">
      <div v-if="searching" class="ssd-empty">Searching providers...</div>
      <div v-else-if="searched && !results.length" class="ssd-empty">No results found</div>
      <MetadataCandidateCard
        v-for="r in results"
        :key="r.provider_id"
        :result="r"
        compact
      >
        <template #actions>
          <button class="btn btn-secondary ssd-apply-btn" :disabled="assigning" @click="assign(r)">
            {{ assigning ? 'Matching...' : 'Use this' }}
          </button>
        </template>
      </MetadataCandidateCard>
    </div>
  </AppDialog>
</template>

<script setup lang="ts">
import type { SearchResult } from '~~/shared/api/types.gen'

type ScannerSearchResult = SearchResult

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
    if (year.value && !isDirectLookup.value) q.year = year.value
    const res = await heya('/api/libraries/{id}/scanner/identities/{identity_id}/search', {
      path: { id: props.libraryId, identity_id: props.identity.id },
      query: q,
    }) as { results: ScannerSearchResult[] }
    results.value = res.results || []
  } catch (error) {
    results.value = []
    toast.err(apiErrorMessage(error, 'Metadata lookup failed'), { duration: 7000 })
  }
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
.ssd-apply-btn { flex-shrink: 0; align-self: center; }
</style>
