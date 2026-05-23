<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="show" class="mid-overlay" @click.self="$emit('close')">
        <div class="mid-modal">
          <div class="mid-head">
            <h3 class="mid-title">Identify</h3>
            <button class="mid-close" @click="$emit('close')"><Icon name="x" :size="16" /></button>
          </div>
          <div class="mid-search-bar">
            <input v-model="query" type="text" class="mid-input" :placeholder="isURL ? 'Paste a Heya / TMDB / IMDb URL...' : 'Search title or paste a URL...'" @keydown.enter="search" />
            <input v-if="!isURL" v-model="year" type="text" class="mid-input mid-year" placeholder="Year" maxlength="4" @keydown.enter="search" />
            <button class="btn btn-primary" :disabled="searching || !query.trim()" @click="search">
              {{ searching ? (isURL ? 'Looking up...' : 'Searching...') : (isURL ? 'Look up' : 'Search') }}
            </button>
          </div>
          <div class="mid-body scroll">
            <div v-if="searching" class="mid-empty">Searching providers...</div>
            <div v-else-if="searched && !results.length" class="mid-empty">No results found</div>
            <div
              v-for="r in results"
              :key="r.provider_id"
              class="mid-result"
            >
              <img v-if="r.poster_url" :src="r.poster_url" class="mid-poster" />
              <div v-else class="mid-poster mid-poster-empty" />
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
        </div>
      </div>
    </Transition>
  </Teleport>
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

const isURL = computed(() => {
  const q = query.value.trim()
  return /^https?:\/\//i.test(q) || /^heya(_[a-z]+)?:/i.test(q)
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
    const params = new URLSearchParams({ q: query.value })
    if (year.value) params.set('year', year.value)
    const res = await apiFetch<{ results: ProviderSearchResult[] }>(
      `/api/media/${props.mediaId}/identify?${params}`
    )
    results.value = res.results || []
  } catch { results.value = [] }
  searching.value = false
}

async function apply(r: ProviderSearchResult) {
  if (!confirm(`Replace all metadata with "${r.title}" from ${r.provider_name}? This will overwrite current metadata.`)) return
  try {
    await apiFetch(`/api/media/${props.mediaId}/identify`, {
      method: 'POST',
      body: JSON.stringify({ provider_name: r.provider_name, provider_id: r.provider_id }),
    })
    emit('applied')
  } catch { /* empty */ }
}
</script>

<style scoped>
.mid-overlay {
  position: fixed; inset: 0; z-index: 1100;
  background: rgba(0,0,0,0.7); backdrop-filter: blur(4px);
  display: flex; align-items: center; justify-content: center;
}
.mid-modal {
  width: 90vw; max-width: 700px; max-height: 80vh;
  background: var(--bg-2); border: 1px solid var(--border);
  border-radius: var(--r-lg); display: flex; flex-direction: column;
  overflow: hidden; box-shadow: var(--shadow-3);
}
.mid-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 16px 20px; border-bottom: 1px solid var(--border); flex-shrink: 0;
}
.mid-title { font-size: 16px; font-weight: 600; color: var(--fg-0); margin: 0; }
.mid-close {
  width: 32px; height: 32px; border-radius: 50%; border: none;
  background: rgba(255,255,255,0.06); color: var(--fg-2); cursor: pointer;
  display: flex; align-items: center; justify-content: center;
}
.mid-close:hover { background: rgba(255,255,255,0.12); color: var(--fg-0); }
.mid-search-bar {
  display: flex; gap: 8px; padding: 16px 20px; border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.mid-input {
  height: 36px; border: 1px solid var(--border); border-radius: var(--r-sm);
  background: var(--bg-3); color: var(--fg-0); font-size: 13px; padding: 0 10px;
  outline: none; flex: 1;
}
.mid-input:focus { border-color: var(--gold); }
.mid-year { max-width: 80px; flex: none; }
.mid-body { flex: 1; overflow-y: auto; padding: 8px 0; }
.mid-empty {
  display: flex; align-items: center; justify-content: center;
  padding: 48px 0; color: var(--fg-3); font-size: 13px;
}
.mid-result {
  display: flex; align-items: flex-start; gap: 14px;
  padding: 14px 20px; transition: background 0.12s;
  border-bottom: 1px solid rgba(255,255,255,0.03);
}
.mid-result:hover { background: rgba(255,255,255,0.02); }
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
.modal-enter-active, .modal-leave-active { transition: all 0.2s ease; }
.modal-enter-from, .modal-leave-to { opacity: 0; }
</style>
