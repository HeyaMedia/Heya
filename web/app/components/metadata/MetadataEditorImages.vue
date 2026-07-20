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
      <div v-if="pendingSelection?.assetType === group.type" class="mei-progress" role="status" aria-live="polite">
        <Icon name="loading" :size="14" />
        <span>Applying selected {{ singularTypeLabel(group.type).toLowerCase() }}… It will update everywhere as soon as Heya finishes downloading it.</span>
      </div>
      <div class="mei-grid">
        <div v-if="!group.assets.length" class="mei-empty-slot">No image selected</div>
        <div v-for="(asset, assetIndex) in group.assets" :key="asset.id" class="mei-card" :class="{ 'mei-card-wide': wideTypes.has(asset.asset_type), 'is-pending': asset._pending }">
          <div class="mei-img-wrap" :style="{ aspectRatio: wideTypes.has(asset.asset_type) ? '16/9' : '2/3' }">
            <LoadingImage :src="asset._pending ? asset.remote_url : imageUrl(asset)" :persistent="!!asset._pending" class="mei-img" :width="240" :quality="80" densities="1x 2x" @error="(e: Event | string) => { if (typeof e !== 'string') (e.target as HTMLImageElement).style.display = 'none' }" />
            <div v-if="asset._pending" class="mei-primary-badge"><Icon name="loading" :size="11" /> Applying</div>
            <div v-else-if="assetIndex === 0" class="mei-primary-badge">Primary</div>
            <div v-if="!asset._pending" class="mei-overlay">
              <button v-if="assetIndex !== 0" class="mei-btn" title="Set as primary" @click="setPrimary(asset)">
                <Icon :name="busyAssetId === asset.id ? 'loading' : 'star'" :size="14" />
              </button>
              <button class="mei-btn mei-btn-danger" title="Delete" @click="deleteAsset(asset)">
                <Icon :name="busyAssetId === asset.id ? 'loading' : 'trash'" :size="14" />
              </button>
            </div>
          </div>
          <div class="mei-meta">
            <span v-if="asset.language" class="mei-tag">{{ asset.language }}</span>
            <span class="mei-tag mei-tag-src">{{ assetSourceLabel(asset) }}</span>
          </div>
        </div>
      </div>
    </div>

    <div class="mei-upload-row">
      <select v-model="uploadType" class="mei-upload-select" aria-label="Custom image type">
        <option v-for="type in allowedTypes" :key="type" :value="type">{{ singularTypeLabel(type) }}</option>
      </select>
      <label class="btn btn-ghost-sm">
        <Icon name="plus" :size="14" />
        Upload Custom
        <input type="file" accept="image/jpeg,image/png,image/webp,.jpg,.jpeg,.png,.webp" style="display:none" @change="uploadFile" />
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
      <div v-if="findLoading" class="mei-modal-empty">
        <Icon name="loading" :size="18" />
        Searching Heya...
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
            :class="{ 'mei-find-card-wide': wideTypes.has(findModalType!), 'is-disabled': !!pendingSelection }"
            :disabled="!!pendingSelection"
            :aria-label="`Add poster option ${i + 1}`"
            @click="downloadArt(art)"
          >
            <div class="mei-find-img-wrap" :style="{ aspectRatio: wideTypes.has(findModalType!) ? '16/9' : '2/3' }">
              <LoadingImage :src="art.url" :persistent="true" :alt="`Poster option ${i + 1}`" class="mei-find-img" />
              <div class="mei-find-source">Heya</div>
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
import { withAuthHeaders } from '~/composables/useAuth'

const props = defineProps<{
  mediaId: number
  mediaPublicId?: string | null
  detail: any
  context?: 'media' | 'season' | 'episode'
  assetLabel?: string
}>()

interface ArtworkActivity {
  assetType: string
  url: string
}

const emit = defineEmits<{
  refresh: []
  pending: [activity: ArtworkActivity | null]
  ready: [activity: ArtworkActivity]
}>()

const findModalType = ref<string | null>(null)
const findLoading = ref(false)
const findResults = ref<ArtworkSearchResult[]>([])
const uploadType = ref('poster')
const pendingSelection = ref<(ArtworkActivity & { label: string, startedAt: number }) | null>(null)
const busyAssetId = ref<number | null>(null)
let pendingGeneration = 0
const { toast } = useToast()

const wideTypes = new Set(['backdrop', 'banner', 'still'])

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
  still: 'Episode Image',
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

const allowedTypes = computed(() => {
  if (props.context === 'season') return ['poster']
  if (props.context === 'episode') return ['still']
  const mediaType = props.detail?.media_item?.media_type
  if (mediaType === 'book') return ['poster', 'backdrop']
  return ['poster', 'backdrop', 'logo', 'clearart', 'banner', 'thumb', 'disc']
})

const groups = computed(() => {
  const map = new Map<string, any[]>()
  for (const a of assets.value) {
    const t = a.asset_type
    if (nonImageTypes.has(t)) continue
    if (!map.has(t)) map.set(t, [])
    map.get(t)!.push(a)
  }
  return allowedTypes.value.map(type => {
    const groupAssets = [...(map.get(type) || [])]
    if (pendingSelection.value?.assetType === type) {
      groupAssets.unshift({
        id: `pending-${pendingSelection.value.startedAt}`,
        asset_type: type,
        remote_url: pendingSelection.value.url,
        source: 'remote',
        language: '',
        sort_order: 0,
        _pending: true,
      })
    }
    return { type, label: typeLabels.value[type] || type, assets: groupAssets }
  })
})

const findGrouped = computed(() => {
  const map = new Map<string, ArtworkSearchResult[]>()
  for (const art of findResults.value) {
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
  const key = mediaImageKey.value
  if (!key) return ''
  const params = new URLSearchParams({ sort: String(asset.sort_order) })
  if (asset.label) params.set('label', asset.label)
  if (asset.content_hash) params.set('content', asset.content_hash.slice(0, 16))
  return withMediaImageRevision(`/api/media/${key}/image/${asset.asset_type}?${params}`, {
    id: props.mediaId,
    public_id: props.mediaPublicId,
  })
}

function assetSourceLabel(asset: any) {
  if (asset.source === 'local') return 'Local'
  if (asset.source === 'custom') return 'Custom'
  return 'Heya'
}

function singularTypeLabel(type: string) {
  const label = typeLabels.value[type] || type
  return label.endsWith('s') ? label.slice(0, -1) : label
}

async function openFindModal(type: string) {
  findModalType.value = type
  findLoading.value = true
  findResults.value = []
  try {
    const { $heya } = useNuxtApp()
    const res = await $heya('/api/media/{id}/assets/search', {
      path: { id: props.mediaId },
      query: { type: type as any },
    }) as { results: ArtworkSearchResult[] }
    findResults.value = res.results || []
  } catch (error) {
    findResults.value = []
    toast.err(apiErrorMessage(error, 'Could not search Heya artwork'), { duration: 7000 })
  }
  findLoading.value = false
}

async function setPrimary(asset: any) {
  if (busyAssetId.value != null) return
  busyAssetId.value = asset.id
  const previewURL = imageUrl(asset)
  emit('pending', { assetType: asset.asset_type, url: previewURL })
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/media/{id}/assets/{asset_id}/primary', {
      method: 'PUT',
      path: { id: props.mediaId, asset_id: asset.id },
    })
    emit('ready', { assetType: asset.asset_type, url: previewURL })
    emit('refresh')
    toast.ok('Primary image updated')
  } catch (error) {
    emit('pending', null)
    toast.err(apiErrorMessage(error, 'Could not set the primary image'), { duration: 7000 })
  } finally {
    busyAssetId.value = null
  }
}

async function deleteAsset(asset: any) {
  const ok = await useConfirm().confirm({
    title: 'Delete image?',
    confirmLabel: 'Delete',
    destructive: true,
  })
  if (!ok) return
  if (busyAssetId.value != null) return
  busyAssetId.value = asset.id
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/media/{id}/assets/{asset_id}', {
      method: 'DELETE',
      path: { id: props.mediaId, asset_id: asset.id },
    })
    emit('ready', { assetType: asset.asset_type, url: '' })
    emit('refresh')
    toast.ok('Image deleted')
  } catch (error) {
    toast.err(apiErrorMessage(error, 'Could not delete the image'), { duration: 7000 })
  } finally {
    busyAssetId.value = null
  }
}

async function downloadArt(art: ArtworkSearchResult) {
  if (pendingSelection.value) return
  const activity = {
    assetType: art.asset_type || findModalType.value || 'poster',
    url: art.url,
    label: props.assetLabel || '',
    startedAt: Date.now(),
  }
  pendingSelection.value = activity
  emit('pending', activity)
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/media/{id}/assets/download', {
      method: 'POST',
      path: { id: props.mediaId },
      body: { url: art.url, asset_type: art.asset_type || findModalType.value, label: props.assetLabel || '' } as any,
    })
    findModalType.value = null
    toast.info('Selected image is downloading; this page will update automatically')
    void waitForDownloadedAsset(activity, ++pendingGeneration)
  } catch (error) {
    pendingSelection.value = null
    emit('pending', null)
    toast.err(apiErrorMessage(error, 'Could not queue the image download'), { duration: 7000 })
  }
}

async function waitForDownloadedAsset(activity: ArtworkActivity & { label: string, startedAt: number }, generation: number) {
  const { $heya } = useNuxtApp()
  let attempt = 0
  while (generation === pendingGeneration && Date.now() - activity.startedAt < 24 * 60 * 60 * 1000) {
    await new Promise(resolve => setTimeout(resolve, Math.min(1500 + attempt++ * 250, 5000)))
    if (generation !== pendingGeneration) return
    try {
      const current = await $heya('/api/media/{id}', { path: { id: String(props.mediaId) } }) as any
      const landed = (current.assets || []).some((asset: any) =>
        asset.asset_type === activity.assetType
        && asset.remote_url === activity.url
        && !!asset.local_path
        && (!activity.label || asset.label === activity.label),
      )
      if (!landed) continue
      pendingSelection.value = null
      emit('ready', activity)
      emit('refresh')
      toast.ok(`${singularTypeLabel(activity.assetType)} updated everywhere`)
      return
    } catch {
      // Keep waiting through temporary network/server restarts. The visible
      // pending card is the status indicator; no reload is required.
    }
  }
  if (generation === pendingGeneration) {
    pendingSelection.value = null
    emit('pending', null)
    toast.err('The image is still unavailable after 24 hours. You can select it again to retry.', { duration: 10000 })
  }
}

async function uploadFile(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  const acceptedTypes = new Set(['image/jpeg', 'image/png', 'image/webp'])
  // Some browsers leave File.type empty (notably for files selected from
  // uncommon filesystem providers). The server validates the decoded bytes,
  // so only reject MIME types when the browser actually supplied one.
  if (file.type && !acceptedTypes.has(file.type)) {
    toast.err('Choose a JPEG, PNG, or WebP image')
    input.value = ''
    return
  }
  if (file.size > 25 * 1024 * 1024) {
    toast.err('Images must be 25 MiB or smaller')
    input.value = ''
    return
  }

  const form = new FormData()
  form.append('file', file)
  form.append('asset_type', uploadType.value)
  if (props.assetLabel) form.append('label', props.assetLabel)

  // Multipart upload — stays on raw $fetch. $heya / openapi-fetch insist on
  // JSON bodies, and the spec doesn't model the multipart shape anyway.
  try {
    const url = `/api/media/${props.mediaId}/assets/upload`
    await $fetch(url, {
      method: 'POST',
      body: form,
      headers: withAuthHeaders(url),
    })
    emit('ready', { assetType: uploadType.value, url: '' })
    emit('refresh')
    toast.ok('Custom image uploaded')
  } catch (error) {
    toast.err(apiErrorMessage(error, 'Could not upload the custom image'), { duration: 7000 })
  }
  input.value = ''
}

watch(allowedTypes, (types) => {
  if (!types.includes(uploadType.value)) uploadType.value = types[0] || 'poster'
}, { immediate: true })

onBeforeUnmount(() => { pendingGeneration++ })
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
.mei-progress {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: -4px 0 14px;
  padding: 9px 11px;
  border: 1px solid color-mix(in srgb, var(--gold) 35%, var(--border));
  border-radius: var(--r-sm);
  background: var(--gold-soft);
  color: var(--gold-bright);
  font-size: 11px;
  line-height: 1.4;
}
.mei-empty-slot {
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 150px;
  min-height: 76px;
  padding: 12px;
  border: 1px dashed var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-3);
  font-size: 12px;
}

.mei-card {
  position: relative;
  width: 100px;
}
.mei-card-wide {
  width: 180px;
}
.mei-card.is-pending .mei-img-wrap {
  box-shadow: 0 0 0 1px var(--gold), 0 0 24px color-mix(in srgb, var(--gold) 20%, transparent);
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
  display: inline-flex;
  align-items: center;
  gap: 3px;
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
.mei-upload-select {
  height: 34px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--fg-1);
  padding: 0 10px;
  font-size: 12px;
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
.mei-find-card.is-disabled {
  cursor: wait;
  opacity: 0.45;
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
