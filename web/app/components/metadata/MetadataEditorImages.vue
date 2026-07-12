<template>
  <div class="mf">
    <div v-for="group in groups" :key="group.type" class="mf-card">
      <div class="mei-group-head">
        <span class="mf-card-head-inline">{{ group.label }}</span>
        <span class="mei-count">{{ group.assets.length }}</span>
        <button class="mei-find-btn" @click="openFindModal(group.type)">
          <Icon name="search" :size="12" />
          Find
        </button>
      </div>
      <div class="mei-grid">
        <div v-for="asset in group.assets" :key="asset.id" class="mei-card" :class="{ 'mei-card-wide': wideTypes.has(asset.asset_type) }">
          <div class="mei-img-wrap" :style="{ aspectRatio: wideTypes.has(asset.asset_type) ? '16/9' : '2/3' }">
            <NuxtImg :src="imageUrl(asset)" class="mei-img" :width="240" :quality="80" densities="1x 2x" @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }" />
            <div v-if="asset.sort_order === 0" class="mei-primary-badge">Primary</div>
            <div class="mei-overlay">
              <button v-if="asset.sort_order !== 0" class="mei-btn" title="Set as primary" @click="setPrimary(asset)">
                <Icon name="star" :size="14" />
              </button>
              <button class="mei-btn mei-btn-danger" title="Delete" @click="deleteAsset(asset)">
                <Icon name="trash" :size="14" />
              </button>
            </div>
          </div>
          <div class="mei-meta">
            <span v-if="asset.language" class="mei-tag">{{ asset.language }}</span>
            <span class="mei-tag mei-tag-src">{{ asset.source }}</span>
          </div>
        </div>
      </div>
    </div>

    <div class="mei-upload-row">
      <label class="btn btn-ghost-sm">
        <Icon name="plus" :size="14" />
        Upload Custom
        <input type="file" accept="image/*" style="display:none" @change="uploadFile" />
      </label>
    </div>

    <!-- Find modal -->
    <AppDialog
      :model-value="!!findModalType"
      :title="findModalType ? `Find ${(typeLabels[findModalType] || findModalType)}` : ''"
      size="xl"
      content-class="mei-find-dialog"
      @update:model-value="(v) => v ? null : findModalType = null"
    >
      <div v-if="!findLoading && availableProviders.length > 1" class="mei-provider-bar">
        <button
          class="mei-provider-pill"
          :class="{ active: findProviderFilter === 'all' }"
          @click="findProviderFilter = 'all'"
        >All</button>
        <button
          v-for="prov in availableProviders"
          :key="prov"
          class="mei-provider-pill"
          :class="{ active: findProviderFilter === prov }"
          @click="findProviderFilter = prov"
        >{{ prov }}</button>
      </div>
      <div v-if="findLoading" class="mei-modal-empty">
        <Icon name="loading" :size="18" />
        Searching providers...
      </div>
      <div v-else-if="!findGrouped.length" class="mei-modal-empty">No images found</div>
      <div v-for="lang in findGrouped" :key="lang.code" class="mei-lang-group">
        <div class="mei-lang-head">
          <span class="mei-lang-label">{{ lang.label }}</span>
          <span class="mei-lang-count">{{ lang.items.length }}</span>
        </div>
        <div class="mei-lang-grid">
          <button
            v-for="(art, i) in lang.items"
            :key="i"
            type="button"
            class="mei-find-card"
            :class="{ 'mei-find-card-wide': wideTypes.has(findModalType!) }"
            :aria-label="`Add poster option ${i + 1}`"
            @click="downloadArt(art)"
          >
            <div class="mei-find-img-wrap" :style="{ aspectRatio: wideTypes.has(findModalType!) ? '16/9' : '2/3' }">
              <NuxtImg :src="art.url" :alt="`Poster option ${i + 1}`" class="mei-find-img" />
              <div v-if="art.source" class="mei-find-source">{{ art.source }}</div>
              <div class="mei-find-dl"><Icon name="plus" :size="16" /></div>
            </div>
          </button>
        </div>
      </div>
    </AppDialog>
  </div>
</template>

<script setup lang="ts">
import type { ArtworkSearchResult } from '~~/shared/types'

const props = defineProps<{
  mediaId: number
  mediaPublicId?: string | null
  detail: any
  context?: 'media' | 'season' | 'episode'
}>()

const emit = defineEmits<{ refresh: [] }>()

const findModalType = ref<string | null>(null)
const findLoading = ref(false)
const findResults = ref<ArtworkSearchResult[]>([])
const findProviderFilter = ref<string>('all')

const wideTypes = new Set(['backdrop', 'banner'])

const assets = computed(() => props.detail?.assets || [])
const mediaImageKey = computed(() => useMediaImageKey({ id: props.mediaId, public_id: props.mediaPublicId }))

const typeLabels = computed<Record<string, string>>(() => {
  if (props.context === 'episode') {
    return { ...baseTypeLabels, backdrop: 'Episode Image' }
  }
  return baseTypeLabels
})

const baseTypeLabels: Record<string, string> = {
  poster: 'Posters', backdrop: 'Backdrops', logo: 'Logos',
  art: 'Art', banner: 'Banners', thumb: 'Thumbnails', disc: 'Disc Art',
  clearart: 'Clear Art',
}

const langLabels: Record<string, string> = {
  en: 'English', ja: 'Japanese', ko: 'Korean', zh: 'Chinese',
  de: 'German', fr: 'French', es: 'Spanish', it: 'Italian',
  pt: 'Portuguese', ru: 'Russian', nl: 'Dutch', sv: 'Swedish',
  pl: 'Polish', da: 'Danish', no: 'Norwegian', fi: 'Finnish',
  cs: 'Czech', hu: 'Hungarian', ro: 'Romanian', tr: 'Turkish',
  ar: 'Arabic', he: 'Hebrew', th: 'Thai', uk: 'Ukrainian',
  '': 'No Language', '00': 'No Language',
}

const nonImageTypes = new Set(['subtitle', 'lyrics', 'nfo'])

const groups = computed(() => {
  const map = new Map<string, any[]>()
  for (const a of assets.value) {
    const t = a.asset_type
    if (nonImageTypes.has(t)) continue
    if (!map.has(t)) map.set(t, [])
    map.get(t)!.push(a)
  }
  return Array.from(map.entries())
    .map(([type, items]) => ({ type, label: typeLabels.value[type] || type, assets: items }))
    .filter(g => g.assets.length > 0)
})

const availableProviders = computed(() => {
  const providers = new Set<string>()
  for (const art of findResults.value) {
    if (art.source) providers.add(art.source)
  }
  return Array.from(providers)
})

const findGrouped = computed(() => {
  const filtered = findProviderFilter.value === 'all'
    ? findResults.value
    : findResults.value.filter(a => a.source === findProviderFilter.value)
  const map = new Map<string, ArtworkSearchResult[]>()
  for (const art of filtered) {
    const lang = art.language || ''
    if (!map.has(lang)) map.set(lang, [])
    map.get(lang)!.push(art)
  }
  return Array.from(map.entries())
    .sort(([a], [b]) => {
      if (a === 'en') return -1
      if (b === 'en') return 1
      if (a === '' || a === '00') return 1
      if (b === '' || b === '00') return 1
      return a.localeCompare(b)
    })
    .map(([code, items]) => ({
      code,
      label: langLabels[code] || code.toUpperCase(),
      items,
    }))
})

function imageUrl(asset: any) {
  if (asset.local_path && !asset.local_path.startsWith('http')) {
    return `/api/media/${mediaImageKey.value}/image/${asset.asset_type}?sort=${asset.sort_order}`
  }
  return asset.remote_url || asset.local_path
}

async function openFindModal(type: string) {
  findModalType.value = type
  findLoading.value = true
  findResults.value = []
  findProviderFilter.value = 'all'
  try {
    const { $heya } = useNuxtApp()
    const res = await $heya('/api/media/{id}/assets/search', {
      path: { id: props.mediaId },
      query: { type: type as any },
    }) as { results: ArtworkSearchResult[] }
    findResults.value = res.results || []
  } catch { findResults.value = [] }
  findLoading.value = false
}

async function setPrimary(asset: any) {
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/media/{id}/assets/{asset_id}/primary', {
      method: 'PUT',
      path: { id: props.mediaId, asset_id: asset.id },
    })
    emit('refresh')
  } catch { /* empty */ }
}

async function deleteAsset(asset: any) {
  const ok = await useConfirm().confirm({
    title: 'Delete image?',
    confirmLabel: 'Delete',
    destructive: true,
  })
  if (!ok) return
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/media/{id}/assets/{asset_id}', {
      method: 'DELETE',
      path: { id: props.mediaId, asset_id: asset.id },
    })
    emit('refresh')
  } catch { /* empty */ }
}

async function downloadArt(art: ArtworkSearchResult) {
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/media/{id}/assets/download', {
      method: 'POST',
      path: { id: props.mediaId },
      body: { url: art.url, asset_type: art.asset_type || findModalType.value } as any,
    })
    emit('refresh')
  } catch { /* empty */ }
}

async function uploadFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  const form = new FormData()
  form.append('file', file)
  form.append('asset_type', 'poster')

  // Multipart upload — stays on raw $fetch. $heya / openapi-fetch insist on
  // JSON bodies, and the spec doesn't model the multipart shape anyway.
  try {
    const { token } = useAuth()
    await $fetch(`/api/media/${props.mediaId}/assets/upload`, {
      method: 'POST',
      body: form,
      headers: token.value ? { Authorization: `Bearer ${token.value}` } : {},
    })
    emit('refresh')
  } catch { /* empty */ }
  input.value = ''
}
</script>

<style scoped>
.mf {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.mf-card {
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 20px;
}

.mei-group-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}

.mf-card-head-inline {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-2);
}

.mei-count {
  font-size: 10px;
  color: var(--fg-3);
  font-family: var(--font-mono);
}

.mei-find-btn {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  border-radius: var(--r-sm);
  border: 1px solid var(--border);
  background: var(--bg-3);
  color: var(--fg-2);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.12s;
}
.mei-find-btn:hover {
  border-color: var(--gold);
  color: var(--gold-bright);
  background: var(--gold-soft);
}

.mei-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.mei-card {
  position: relative;
  width: 100px;
}
.mei-card-wide {
  width: 180px;
}

.mei-img-wrap {
  position: relative;
  border-radius: var(--r-sm);
  overflow: hidden;
  background: var(--bg-3);
}

.mei-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.mei-primary-badge {
  position: absolute;
  top: 4px;
  left: 4px;
  padding: 1px 6px;
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  background: var(--gold);
  color: var(--bg-0);
  border-radius: 4px;
}

.mei-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: rgba(0, 0, 0, 0.6); /* on artwork — stays literal */
  opacity: 0;
  transition: opacity 0.15s;
}
.mei-card:hover .mei-overlay {
  opacity: 1;
}

.mei-btn {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  border: none;
  background: rgba(255, 255, 255, 0.15); /* on artwork — stays literal */
  color: var(--fg-0);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.12s;
}
.mei-btn:hover {
  background: rgba(255, 255, 255, 0.25); /* on artwork — stays literal */
}
.mei-btn-danger:hover {
  background: rgba(217, 107, 107, 0.4); /* on artwork — stays literal */
}

.mei-meta {
  display: flex;
  gap: 4px;
  margin-top: 4px;
}

.mei-tag {
  font-size: 9px;
  padding: 1px 5px;
  border-radius: 3px;
  background: rgb(var(--ink) / 0.06);
  color: var(--fg-2);
  text-transform: uppercase;
}
.mei-tag-src {
  color: var(--fg-3);
}

.mei-upload-row {
  display: flex;
  gap: 8px;
}

/* AppDialog supplies overlay/panel/header chrome — the rules below
   style the contents (provider pills, language groups, image cards). */

.mei-modal-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 48px 0;
  color: var(--fg-3);
  font-size: 13px;
}

.mei-provider-bar {
  display: flex;
  gap: 6px;
  padding: 12px 20px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.mei-provider-pill {
  padding: 4px 12px;
  border-radius: 12px;
  border: 1px solid var(--border);
  background: transparent;
  color: var(--fg-2);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.12s;
}
.mei-provider-pill:hover {
  border-color: var(--fg-3);
  color: var(--fg-1);
}
.mei-provider-pill.active {
  background: var(--gold-soft);
  border-color: var(--gold);
  color: var(--gold-bright);
}

.mei-lang-group {
  margin-bottom: 24px;
}
.mei-lang-group:last-child {
  margin-bottom: 0;
}

.mei-lang-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--border);
}

.mei-lang-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--fg-1);
}

.mei-lang-count {
  font-size: 10px;
  color: var(--fg-3);
  font-family: var(--font-mono);
}

.mei-lang-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.mei-find-card {
  width: 100px;
  cursor: pointer;
  position: relative;
}
.mei-find-card-wide {
  width: 180px;
}

.mei-find-img-wrap {
  position: relative;
  border-radius: var(--r-sm);
  overflow: hidden;
  background: var(--bg-3);
}

.mei-find-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.mei-find-source {
  position: absolute;
  bottom: 4px;
  right: 4px;
  padding: 1px 5px;
  font-size: 8px;
  font-weight: 700;
  text-transform: uppercase;
  background: rgba(0, 0, 0, 0.7); /* on artwork — stays literal */
  color: var(--fg-2);
  border-radius: 3px;
  pointer-events: none;
}

.mei-find-dl {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.55); /* on artwork — stays literal */
  color: var(--gold-bright);
  opacity: 0;
  transition: opacity 0.15s;
}
.mei-find-card:hover .mei-find-dl {
  opacity: 1;
}

</style>
