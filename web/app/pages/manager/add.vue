<script setup lang="ts">
definePageMeta({ layout: 'manager', middleware: 'admin' })

import { useQuery } from '@pinia/colada'
import { librariesQuery } from '~/queries/catalog'
import { managerQualityProfilesQuery } from '~/queries/manager'
import type { ManagerLookupResult } from '~~/shared/api/types.gen'

const { $heya } = useNuxtApp()

const librariesData = useQuery(librariesQuery)
const libraries = computed(() => librariesData.data.value ?? [])
const profilesData = useQuery(managerQualityProfilesQuery)

const libraryId = ref<number>(0)
const query = ref('')
const results = ref<ManagerLookupResult[]>([])
const searching = ref(false)
const searchError = ref('')
const searched = ref(false)

watchEffect(() => {
  if (!libraryId.value && libraries.value.length) libraryId.value = libraries.value[0]!.id
})

const selectedLibrary = computed(() => libraries.value.find(l => l.id === libraryId.value))
const domainProfiles = computed(() => {
  const mediaType = selectedLibrary.value?.media_type
  const domain = mediaType === 'anime' ? 'tv' : mediaType
  return (profilesData.data.value ?? []).filter(p => p.domain === domain)
})

async function search() {
  if (!query.value.trim() || !libraryId.value) return
  searching.value = true
  searchError.value = ''
  try {
    results.value = await $heya('/api/manager/lookup', {
      query: { library_id: libraryId.value, q: query.value.trim() },
    }) as ManagerLookupResult[]
    searched.value = true
  } catch (e: any) {
    searchError.value = e?.data?.detail ?? 'Search failed'
  } finally {
    searching.value = false
  }
}

// Add dialog.
const addTarget = ref<ManagerLookupResult | null>(null)
const addMonitored = ref(true)
const addProfileId = ref<number>(0)
const adding = ref(false)
const addError = ref('')
const dialogOpen = computed({
  get: () => addTarget.value !== null,
  set: (open: boolean) => { if (!open) addTarget.value = null },
})

function openAdd(result: ManagerLookupResult) {
  addTarget.value = result
  addError.value = ''
  addMonitored.value = true
  addProfileId.value = domainProfiles.value[0]?.id ?? 0
}

async function confirmAdd() {
  const target = addTarget.value
  if (!target) return
  adding.value = true
  addError.value = ''
  try {
    const result = await $heya('/api/manager/add', {
      method: 'POST',
      body: {
        library_id: libraryId.value,
        title: target.title,
        year: target.year,
        description: target.description,
        poster_url: target.poster_url,
        heya_slug: target.heya_slug,
        external_ids: target.external_ids,
        monitored: addMonitored.value,
        quality_profile_id: addProfileId.value || undefined,
      },
    }) as { media_item_id: number, library_id: number }
    addTarget.value = null
    navigateTo(`/manager/library/${result.library_id}/${result.media_item_id}`)
  } catch (e: any) {
    addError.value = e?.data?.detail ?? 'Add failed'
  } finally {
    adding.value = false
  }
}
</script>

<template>
  <div>
    <SettingsContextHero
      title="Add new"
      icon="plus"
      eyebrow="Manager · Dry run"
      description="Search the metadata catalog and add movies, series, or artists to a library — monitored and fileless until releases arrive. Hidden from the public apps until real files land."
    />

    <form class="add-bar" @submit.prevent="search">
      <select v-model.number="libraryId" class="add-select" aria-label="Target library">
        <option v-for="lib in libraries" :key="lib.id" :value="lib.id">{{ lib.name }}</option>
      </select>
      <input
        v-model="query" type="search" class="add-input"
        :placeholder="`Search ${selectedLibrary?.media_type ?? ''} titles…`"
        aria-label="Search query"
      >
      <button type="submit" class="mgr-btn gold" :disabled="searching || !query.trim()">
        <span v-if="searching" class="mgr-spin" />
        <Icon v-else name="search" :size="14" />
        Search
      </button>
    </form>

    <div v-if="searchError" class="mgr-flash err">{{ searchError }}</div>

    <div v-if="results.length" class="add-grid">
      <div v-for="result in results" :key="`${result.provider_name}-${result.provider_id}`" class="add-card">
        <NuxtImg
          v-if="result.poster_url"
          :src="result.poster_url" class="add-poster" width="120" loading="lazy" :alt="''"
        />
        <div v-else class="add-poster empty" aria-hidden="true"><Icon name="image" :size="20" /></div>
        <div class="add-body">
          <div class="add-title">{{ result.title }}<span v-if="result.year" class="add-year"> ({{ result.year }})</span></div>
          <p v-if="result.description" class="add-overview">{{ result.description }}</p>
          <div class="add-actions">
            <NuxtLink
              v-if="result.already_in_library"
              :to="`/manager/library/${libraryId}/${result.existing_item_id}`"
              class="mgr-btn"
            ><Icon name="check" :size="13" /> In library</NuxtLink>
            <button v-else type="button" class="mgr-btn gold" @click="openAdd(result)">
              <Icon name="plus" :size="13" /> Add
            </button>
          </div>
        </div>
      </div>
    </div>
    <div v-else-if="searched && !searching" class="add-empty">
      <Icon name="info" :size="14" /> No catalog matches for “{{ query }}”.
    </div>

    <AppDialog v-model="dialogOpen" :title="`Add ${addTarget?.title ?? ''}`">
      <div v-if="addTarget" class="add-form">
        <label class="add-field">
          <span>Quality profile</span>
          <select v-model.number="addProfileId" class="add-select">
            <option :value="0">No profile</option>
            <option v-for="p in domainProfiles" :key="p.id" :value="p.id">{{ p.name }}</option>
          </select>
        </label>
        <label class="add-check">
          <input v-model="addMonitored" type="checkbox">
          <span>Monitor for releases</span>
        </label>
        <p class="add-note">Dry run — the pipeline records what it would grab, and the item stays hidden from public browse until files actually land.</p>
        <div v-if="addError" class="mgr-flash err">{{ addError }}</div>
        <div class="add-form-actions">
          <button type="button" class="mgr-btn" @click="addTarget = null">Cancel</button>
          <button type="button" class="mgr-btn gold" :disabled="adding" @click="confirmAdd">
            {{ adding ? 'Adding…' : 'Add to library' }}
          </button>
        </div>
      </div>
    </AppDialog>
  </div>
</template>

<style scoped>
.add-bar {
  display: flex;
  gap: 10px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}
.add-select {
  height: 36px;
  padding: 0 10px;
  border-radius: var(--r-sm);
  background: var(--bg-2);
  border: 1px solid var(--border);
  color: var(--fg-1);
  font-size: 13px;
}
.add-input {
  flex: 1;
  min-width: 220px;
  height: 36px;
  padding: 0 12px;
  border-radius: var(--r-sm);
  background: var(--bg-2);
  border: 1px solid var(--border);
  color: var(--fg-0);
  font-size: 13.5px;
}
.add-input:focus { outline: none; border-color: color-mix(in srgb, var(--gold) 50%, var(--border)); }

.add-grid { display: flex; flex-direction: column; gap: 10px; }
.add-card {
  display: flex;
  gap: 14px;
  padding: 14px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.add-poster {
  width: 68px;
  aspect-ratio: 2/3;
  object-fit: cover;
  border-radius: var(--r-sm);
  flex-shrink: 0;
}
.add-poster.empty {
  display: flex; align-items: center; justify-content: center;
  background: rgb(var(--ink) / 0.06);
  color: var(--fg-3);
}
.add-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
.add-title { font-size: 14.5px; font-weight: 600; color: var(--fg-0); }
.add-year { color: var(--fg-2); font-weight: 400; }
.add-overview {
  margin: 0;
  font-size: 12.5px;
  color: var(--fg-2);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.add-actions { display: flex; gap: 8px; margin-top: auto; }

.add-empty {
  display: flex; align-items: center; gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
}

.add-form { display: flex; flex-direction: column; gap: 14px; }
.add-field { display: flex; flex-direction: column; gap: 6px; font-size: 12.5px; color: var(--fg-1); }
.add-check { display: flex; align-items: center; gap: 8px; font-size: 13px; color: var(--fg-1); }
.add-note { margin: 0; font-size: 11.5px; color: var(--fg-3); }
.add-form-actions { display: flex; justify-content: flex-end; gap: 8px; }
</style>
