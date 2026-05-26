<template>
  <div class="mf">
    <!-- Embedded Subtitles -->
    <div class="mf-card">
      <div class="mf-card-head">Embedded Subtitles</div>
      <div v-if="!embeddedSubs.length" class="sub-empty-inline">No embedded subtitle tracks</div>
      <div v-else class="sub-list">
        <div v-for="s in embeddedSubs" :key="s.index" class="sub-row">
          <span class="sub-codec-badge">{{ s.codec_name.toUpperCase() }}</span>
          <span v-if="s.language" class="sub-lang-badge">{{ s.language.toUpperCase() }}</span>
          <span v-if="s.title" class="sub-title-text">{{ s.title }}</span>
          <span v-if="s.forced" class="sub-tag sub-tag-forced">Forced</span>
          <span v-if="s.default" class="sub-tag sub-tag-default">Default</span>
          <span v-if="s.hearing_impaired" class="sub-tag sub-tag-hi">HI</span>
        </div>
      </div>
    </div>

    <!-- External Subtitle Files -->
    <div class="mf-card">
      <div class="mf-card-head">External Subtitles</div>
      <div v-if="!externalSubs.length" class="sub-empty-inline">No external subtitle files</div>
      <div v-else class="sub-list">
        <div v-for="s in externalSubs" :key="s.id" class="sub-row">
          <span v-if="s.language" class="sub-lang-badge">{{ s.language.toUpperCase() }}</span>
          <span class="sub-filename">{{ subtitleFilename(s) }}</span>
          <span class="sub-source-badge">{{ s.source }}</span>
          <span class="spacer" />
          <button class="sub-delete-btn" title="Delete" @click="deleteSubtitle(s)">
            <Icon name="trash" :size="13" />
          </button>
        </div>
      </div>
    </div>

    <!-- Search OpenSubtitles -->
    <div class="sub-actions">
      <button class="btn btn-secondary" :disabled="!osConfigured" @click="openSearch">
        <Icon name="search" :size="14" />
        Search OpenSubtitles
      </button>
      <span v-if="!osConfigured" class="sub-hint">
        Configure credentials in Settings &rarr; Providers
      </span>
    </div>

    <!-- Search Modal -->
    <AppDialog v-model="showSearch" title="Search OpenSubtitles" size="lg">
      <div class="sub-search-bar">
        <input
          v-model="searchQuery"
          type="text"
          class="sub-search-input"
          placeholder="Search or leave blank to use media title..."
          @keydown.enter="doSearch"
        />
        <input
          v-model="searchLangs"
          type="text"
          class="sub-search-langs"
          placeholder="en,da"
          title="Languages (comma-separated ISO codes)"
        />
        <button class="btn btn-primary" :disabled="searching" @click="doSearch">
          {{ searching ? 'Searching...' : 'Search' }}
        </button>
      </div>

      <div class="sub-results scroll">
        <div v-if="searching" class="sub-modal-empty">
          <Icon name="loading" :size="18" />
          Searching...
        </div>
        <div v-else-if="searched && !searchResults.length" class="sub-modal-empty">
          No subtitles found
        </div>
        <div v-for="r in searchResults" :key="r.id" class="sub-result">
          <div class="sub-result-main">
            <span class="sub-lang-badge">{{ r.attributes.language.toUpperCase() }}</span>
            <span class="sub-result-release">{{ r.attributes.release || 'Unknown release' }}</span>
            <span v-if="r.attributes.hearing_impaired" class="sub-tag sub-tag-hi">HI</span>
            <span v-if="r.attributes.foreign_parts_only" class="sub-tag sub-tag-forced">Foreign</span>
            <span v-if="r.attributes.ai_translated" class="sub-tag">AI</span>
            <span v-if="r.attributes.from_trusted" class="sub-tag sub-tag-trusted">Trusted</span>
          </div>
          <div class="sub-result-meta">
            <span class="sub-result-stat">{{ r.attributes.download_count.toLocaleString() }} downloads</span>
            <span v-if="r.attributes.ratings > 0" class="sub-result-stat">{{ r.attributes.ratings.toFixed(1) }} rating</span>
            <span class="sub-result-stat">{{ r.attributes.uploader?.name || 'Unknown' }}</span>
          </div>
          <button
            class="btn btn-ghost-sm sub-result-dl"
            :disabled="downloading === r.id"
            @click="downloadSubtitle(r)"
          >
            <Icon v-if="downloading === r.id" name="loading" :size="12" />
            <Icon v-else name="download" :size="12" />
            {{ downloading === r.id ? 'Downloading...' : 'Download' }}
          </button>
        </div>
      </div>
    </AppDialog>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  mediaId: number
  fileId?: number | null
  detail: any
}>()

const emit = defineEmits<{ refresh: [] }>()

interface StreamInfo {
  index: number
  codec_name: string
  codec_type: string
  language?: string
  title?: string
  default: boolean
  forced: boolean
  hearing_impaired?: boolean
}

interface FileInfo {
  id: number
  streams?: StreamInfo[]
}

const embeddedSubs = ref<StreamInfo[]>([])
const osConfigured = ref(false)
const showSearch = ref(false)
const searchQuery = ref('')
const searchLangs = ref('en')
const searching = ref(false)
const searched = ref(false)
const searchResults = ref<any[]>([])
const downloading = ref<string | null>(null)

const externalSubs = computed(() => {
  return (props.detail?.assets || []).filter((a: any) => a.asset_type === 'subtitle')
})

function subtitleFilename(s: any): string {
  if (!s.local_path) return 'Unknown'
  const parts = s.local_path.split('/')
  return parts[parts.length - 1]
}

async function fetchEmbeddedSubs() {
  try {
    const { $heya } = useNuxtApp()
    const files = await $heya('/api/media/{id}/files', {
      path: { id: props.mediaId },
    }) as FileInfo[]
    const target = props.fileId ? files.find(f => f.id === props.fileId) : files[0]
    if (target?.streams) {
      embeddedSubs.value = target.streams.filter(s => s.codec_type === 'subtitle')
    }
  } catch {
    embeddedSubs.value = []
  }
}

async function checkOSConfigured() {
  try {
    const { $heya } = useNuxtApp()
    const res = await $heya('/api/system-settings/{key}', {
      path: { key: 'opensubtitles' },
    }) as { key: string; value: any }
    osConfigured.value = !!(res.value?.api_key && res.value?.username)
  } catch {
    osConfigured.value = false
  }
}

function openSearch() {
  showSearch.value = true
  searched.value = false
  searchResults.value = []
}

async function doSearch() {
  searching.value = true
  searched.value = true
  try {
    const { $heya } = useNuxtApp()
    const query: Record<string, any> = { media_id: props.mediaId }
    if (searchQuery.value) query.query = searchQuery.value
    if (searchLangs.value) query.languages = searchLangs.value
    const res = await $heya('/api/opensubtitles/search', { query }) as { data: any[] }
    searchResults.value = res.data || []
  } catch {
    searchResults.value = []
  }
  searching.value = false
}

async function downloadSubtitle(r: any) {
  if (!r.attributes?.files?.length) return
  downloading.value = r.id
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/opensubtitles/download', {
      method: 'POST',
      body: {
        media_item_id: props.mediaId,
        file_id: r.attributes.files[0].file_id,
        language: r.attributes.language,
        file_name: r.attributes.files[0].file_name,
      } as any,
    })
    emit('refresh')
  } catch { /* empty */ }
  downloading.value = null
}

async function deleteSubtitle(s: any) {
  const ok = await useConfirm().confirm({
    title: 'Delete subtitle?',
    confirmLabel: 'Delete',
    destructive: true,
  })
  if (!ok) return
  try {
    const { $heya } = useNuxtApp()
    await $heya('/api/media/{id}/assets/{asset_id}', {
      method: 'DELETE',
      path: { id: props.mediaId, asset_id: s.id },
    })
    emit('refresh')
  } catch { /* empty */ }
}

watch(() => props.mediaId, () => {
  if (props.mediaId) {
    fetchEmbeddedSubs()
    checkOSConfigured()
  }
}, { immediate: true })
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

.mf-card-head {
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-2);
  margin-bottom: 16px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border);
}

.sub-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.sub-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-radius: var(--r-sm);
  transition: background 0.12s;
}
.sub-row:hover {
  background: rgba(255, 255, 255, 0.03);
}

.sub-codec-badge {
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--gold-soft);
  color: var(--gold-bright);
  font-size: 10px;
  font-weight: 700;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
}

.sub-lang-badge {
  padding: 2px 6px;
  border-radius: 4px;
  background: rgba(96, 165, 250, 0.12);
  color: rgb(96, 165, 250);
  font-size: 10px;
  font-weight: 700;
  font-family: var(--font-mono);
  letter-spacing: 0.04em;
}

.sub-title-text {
  font-size: 12px;
  color: var(--fg-1);
}

.sub-filename {
  font-size: 12px;
  color: var(--fg-1);
  font-family: var(--font-mono);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sub-source-badge {
  padding: 2px 6px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.05);
  color: var(--fg-3);
  font-size: 9px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.sub-tag {
  padding: 1px 5px;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.06);
  color: var(--fg-3);
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.sub-tag-forced { background: rgba(217, 107, 107, 0.15); color: var(--bad); }
.sub-tag-default { background: rgba(74, 222, 128, 0.12); color: var(--good); }
.sub-tag-hi { background: rgba(96, 165, 250, 0.12); color: rgb(96, 165, 250); }
.sub-tag-trusted { background: var(--gold-soft); color: var(--gold-bright); }

.spacer { flex: 1; }

.sub-delete-btn {
  width: 28px;
  height: 28px;
  border-radius: var(--r-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  transition: all 0.12s;
}
.sub-delete-btn:hover {
  background: rgba(217, 107, 107, 0.12);
  color: var(--bad);
}

.sub-empty-inline {
  font-size: 13px;
  color: var(--fg-3);
  padding: 8px 0;
}

.sub-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.sub-hint {
  font-size: 11px;
  color: var(--fg-4);
}

/* Search modal */
/* AppDialog supplies overlay/panel/header chrome; rules below are
   layout-only for the search bar + results list inside the body. */
.sub-search-bar {
  display: flex;
  gap: 8px;
  padding-bottom: 14px;
  margin-bottom: 6px;
  border-bottom: 1px solid var(--border);
}

.sub-search-input {
  flex: 1;
  height: 38px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--fg-0);
  font-size: 13px;
  padding: 0 12px;
  outline: none;
}
.sub-search-input:focus { border-color: var(--gold); }

.sub-search-langs {
  width: 80px;
  height: 38px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--fg-0);
  font-size: 13px;
  font-family: var(--font-mono);
  padding: 0 10px;
  outline: none;
  text-align: center;
}
.sub-search-langs:focus { border-color: var(--gold); }

.sub-results {
  /* AppDialog body already scrolls; let this region grow to fill it. */
  max-height: 56vh;
  overflow-y: auto;
}

.sub-modal-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 48px 0;
  color: var(--fg-3);
  font-size: 13px;
}

.sub-result {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 20px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.03);
  transition: background 0.12s;
}
.sub-result:hover {
  background: rgba(255, 255, 255, 0.02);
}

.sub-result-main {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
  flex-wrap: wrap;
}

.sub-result-release {
  font-size: 13px;
  color: var(--fg-0);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sub-result-meta {
  display: flex;
  gap: 12px;
  flex-shrink: 0;
}

.sub-result-stat {
  font-size: 10px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  white-space: nowrap;
}

.sub-result-dl {
  flex-shrink: 0;
}
</style>
