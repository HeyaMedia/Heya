<template>
  <div>
    <div class="page-header">
      <div>
        <h2 class="page-title">Libraries</h2>
        <p class="page-desc">Manage your media folders and metadata sources</p>
      </div>
      <div class="header-actions">
        <button v-if="hasAnyProgress" class="btn btn-danger-subtle" @click="cancelAll">
          <Icon name="close" :size="14" />
          Cancel All
        </button>
        <button v-if="libraries.length" class="btn btn-secondary" @click="scanAll" :disabled="scanningAll">
          <Icon :name="scanningAll ? 'loading' : 'refresh'" :size="15" :class="{ spinning: scanningAll }" />
          {{ scanningAll ? 'Scanning…' : 'Scan All' }}
        </button>
        <button class="btn btn-primary" @click="openAdd">
          <Icon name="plus" :size="16" />
          Add Library
        </button>
      </div>
    </div>

    <div v-if="libraries.length" class="lib-list">
      <div v-for="lib in libraries" :key="lib.id" class="lib-card" :class="{ active: libProgress(lib.id) }">
        <div class="lib-card-left">
          <div class="lib-icon" :class="lib.media_type">
            <svg v-if="libProgress(lib.id)" class="progress-ring" viewBox="0 0 48 48">
              <circle class="ring-track" cx="24" cy="24" r="20" />
              <circle
                class="ring-fill"
                cx="24" cy="24" r="20"
                :stroke-dasharray="125.66"
                :stroke-dashoffset="125.66 - 125.66 * libPercent(lib.id)"
              />
            </svg>
            <Icon :name="mediaIcon(lib.media_type)" :size="20" />
          </div>
        </div>
        <div class="lib-card-center">
          <div class="lib-header">
            <span class="lib-name">{{ lib.name }}</span>
            <span class="lib-type">{{ lib.media_type }}</span>
          </div>

          <div v-if="libProgress(lib.id)" class="lib-progress">
            <div class="progress-bar-track">
              <div class="progress-bar-fill" :style="{ width: (libPercent(lib.id) * 100) + '%' }" />
            </div>
            <span class="progress-text">
              Processed {{ libProgress(lib.id)!.processed }} of {{ libProgress(lib.id)!.total }} files
              <template v-if="libProgress(lib.id)!.matched"> &middot; {{ libProgress(lib.id)!.matched }} matched</template>
            </span>
          </div>

          <div v-else class="lib-paths">
            <span v-for="(p, i) in lib.paths" :key="i" class="lib-path">
              <Icon name="folder" :size="11" />
              {{ p }}
            </span>
          </div>
          <div class="lib-tags">
            <span class="tag meta">heya.media</span>
            <span v-if="lib.settings?.fetch_ratings" class="tag rate">
              <Icon name="star" :size="9" />
              Ratings
            </span>
            <span v-if="lib.settings?.watch" class="tag watch">
              <Icon name="eye" :size="9" />
              Watching
            </span>
            <span v-if="lib.settings?.preferred_language" class="tag lang">{{ lib.settings.preferred_language.toUpperCase() }}</span>
          </div>
        </div>
        <div class="lib-card-right">
          <button class="action-btn" @click="openSettings(lib)" title="Configure">
            <Icon name="settings" :size="15" />
          </button>
          <div class="more-wrap" :ref="(el) => setMoreRef(lib.id, el as HTMLElement)">
            <button class="action-btn" @click="toggleMore(lib.id)" title="More actions">
              <Icon name="more" :size="15" />
            </button>
            <div v-if="moreOpen === lib.id" class="more-menu">
              <button class="more-option" @click="forceRefreshMetadata(lib.id)">
                <Icon name="refresh" :size="13" />
                Refresh Metadata
              </button>
              <button class="more-option" @click="forceRefreshImages(lib.id)">
                <Icon name="download" :size="13" />
                Refresh Images
              </button>
            </div>
          </div>
          <button v-if="libProgress(lib.id)" class="action-btn danger" @click="cancelLib(lib.id)" title="Cancel scan">
            <Icon name="close" :size="15" />
          </button>
          <button v-else class="action-btn" @click="scanLib(lib.id)" :disabled="scanning === lib.id" title="Scan">
            <Icon :name="scanning === lib.id ? 'loading' : 'refresh'" :size="15" :class="{ spinning: scanning === lib.id }" />
          </button>
          <button class="action-btn danger" @click="deleteLib(lib.id)" title="Remove">
            <Icon name="trash" :size="15" />
          </button>
        </div>
      </div>
    </div>

    <div v-else class="empty-state">
      <div class="empty-icon">
        <Icon name="folder" :size="32" />
      </div>
      <h3 class="empty-title">No libraries configured</h3>
      <p class="empty-desc">Add a library to start scanning your media collection</p>
      <button class="btn btn-primary" @click="openAdd">
        <Icon name="plus" :size="16" />
        Add Your First Library
      </button>
    </div>

    <!-- Add Library Modal -->
    <Teleport to="body">
      <Transition name="modal">
        <div v-if="showAdd" class="modal-overlay" @click.self="showAdd = false">
          <form class="modal-card" @submit.prevent="addLibrary">
            <div class="modal-header">
              <h2 class="modal-title">Add Library</h2>
              <button type="button" class="modal-close" @click="showAdd = false">
                <Icon name="close" :size="16" />
              </button>
            </div>

            <div class="modal-body">
              <div class="form-section">
                <div class="form-row">
                  <div class="form-group" style="flex: 1">
                    <label class="form-label">Name</label>
                    <input v-model="newLib.name" class="form-input" placeholder="My Movies" required />
                  </div>
                  <div class="form-group" style="width: 140px">
                    <label class="form-label">Type</label>
                    <div class="select-wrap">
                      <select v-model="newLib.media_type" @change="onTypeChange" class="form-select">
                        <option value="movie">Movie</option>
                        <option value="tv">TV Show</option>
                        <option value="music">Music</option>
                        <option value="book">Book</option>
                      </select>
                      <Icon name="chevdown" :size="12" class="select-icon" />
                    </div>
                  </div>
                </div>
              </div>

              <div class="form-section">
                <label class="form-label">Folders</label>
                <div class="paths-list">
                  <div v-for="(p, i) in newLib.paths" :key="i" class="path-row">
                    <LibraryPathInput
                      :model-value="p ?? ''"
                      @update:model-value="(v: string) => { newLib.paths.splice(i, 1, v) }"
                    />
                    <button v-if="newLib.paths.length > 1" type="button" class="path-remove" @click="newLib.paths.splice(i, 1)">
                      <Icon name="close" :size="12" />
                    </button>
                  </div>
                </div>
                <button type="button" class="add-path-btn" @click="newLib.paths.push('')">
                  <Icon name="plus" :size="12" />
                  Add folder
                </button>
              </div>

              <div class="modal-divider" />

              <LibrarySettingsPanel v-model="newLib.settings" :media-type="newLib.media_type" />
            </div>

            <div v-if="addError" class="modal-error">
              <Icon name="warning" :size="14" />
              {{ addError }}
            </div>

            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" @click="showAdd = false">Cancel</button>
              <button type="submit" class="btn btn-primary">
                <Icon name="plus" :size="14" />
                Add Library
              </button>
            </div>
          </form>
        </div>
      </Transition>
    </Teleport>

    <!-- Edit Settings Modal -->
    <Teleport to="body">
      <Transition name="modal">
        <div v-if="editLib" class="modal-overlay" @click.self="editLib = null">
          <div class="modal-card">
            <div class="modal-header">
              <div class="modal-header-left">
                <div class="modal-lib-icon" :class="editLib.media_type">
                  <Icon :name="mediaIcon(editLib.media_type)" :size="16" />
                </div>
                <div>
                  <h2 class="modal-title">{{ editLib.name }}</h2>
                  <span class="modal-subtitle">{{ editLib.media_type }} library</span>
                </div>
              </div>
              <button class="modal-close" @click="editLib = null">
                <Icon name="close" :size="16" />
              </button>
            </div>

            <div class="modal-body">
              <div class="form-section">
                <label class="form-label">Folders</label>
                <div class="paths-list">
                  <div v-for="(p, i) in editPaths" :key="i" class="path-row">
                    <LibraryPathInput
                      :model-value="p ?? ''"
                      @update:model-value="(v: string) => { editPaths.splice(i, 1, v) }"
                    />
                    <button v-if="editPaths.length > 1" type="button" class="path-remove" @click="editPaths.splice(i, 1)">
                      <Icon name="close" :size="12" />
                    </button>
                  </div>
                </div>
                <button type="button" class="add-path-btn" @click="editPaths.push('')">
                  <Icon name="plus" :size="12" />
                  Add folder
                </button>
              </div>

              <div class="modal-divider" />

              <LibrarySettingsPanel v-model="editSettings" :media-type="editLib.media_type" />
            </div>

            <div v-if="saveError" class="modal-error">
              <Icon name="warning" :size="14" />
              {{ saveError }}
            </div>

            <div class="modal-footer">
              <button class="btn btn-secondary" @click="editLib = null">Cancel</button>
              <button class="btn btn-primary" @click="saveSettings" :disabled="saving">
                <Icon :name="saving ? 'loading' : 'check'" :size="14" :class="{ spinning: saving }" />
                {{ saving ? 'Saving…' : 'Save Changes' }}
              </button>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import type { Library, LibrarySettings } from '~~/shared/types'
import type { LibraryScanProgress } from '~/composables/useEventBus'

const { scanProgress } = useEventBus()

const libraries = ref<Library[]>([])
const scanning = ref<number | null>(null)
const scanningAll = ref(false)

function libProgress(id: number): LibraryScanProgress | null {
  return scanProgress.value[id] ?? null
}

function libPercent(id: number): number {
  const p = scanProgress.value[id]
  if (!p || p.total === 0) return 0
  return Math.min(p.processed / p.total, 1)
}

const hasAnyProgress = computed(() => Object.keys(scanProgress.value).length > 0)

async function scanAll() {
  scanningAll.value = true
  try {
    await Promise.all(libraries.value.map(lib => apiFetch(`/api/libraries/${lib.id}/scan`, { method: 'POST' })))
  } catch {}
  scanningAll.value = false
}

async function cancelLib(id: number) {
  try { await apiFetch(`/api/libraries/${id}/scan/cancel`, { method: 'POST' }) } catch {}
}

async function cancelAll() {
  try { await apiFetch('/api/libraries/scan/cancel-all', { method: 'POST' }) } catch {}
}

function defaultSettings(type: string): LibrarySettings {
  const base: LibrarySettings = {
    watch: true, preferred_language: 'en', preferred_country: 'US',
    auto_collections: false, metadata_refresh_days: 0, fetch_ratings: true,
    save_nfo: false, save_images: false,
    enable_trickplay: false, generate_thumbnails: true,
  }
  switch (type) {
    case 'movie': return { ...base, auto_collections: true }
    case 'tv': return { ...base }
    case 'music': return { ...base }
    case 'book': return { ...base }
    default: return base
  }
}

function mediaIcon(type: string) {
  return type === 'movie' ? 'film' : type === 'tv' ? 'tv' : type === 'music' ? 'music' : 'book'
}

const showAdd = ref(false)
const addError = ref('')
const newLib = ref({
  name: '', media_type: 'movie', paths: [''],
  settings: defaultSettings('movie'),
})

function openAdd() {
  newLib.value = { name: '', media_type: 'movie', paths: [''], settings: defaultSettings('movie') }
  addError.value = ''
  showAdd.value = true
}

function onTypeChange() {
  newLib.value.settings = defaultSettings(newLib.value.media_type)
}

async function addLibrary() {
  addError.value = ''
  const paths = newLib.value.paths.filter(p => p.trim())
  if (!paths.length) { addError.value = 'At least one folder is required'; return }
  try {
    await apiFetch('/api/libraries', {
      method: 'POST',
      body: JSON.stringify({ name: newLib.value.name, media_type: newLib.value.media_type, paths, settings: newLib.value.settings }),
    })
    showAdd.value = false
    await fetchLibraries()
  } catch (e: any) {
    addError.value = e?.data?.error || 'Failed to create library'
  }
}

const editLib = ref<Library | null>(null)
const editSettings = ref<LibrarySettings>(defaultSettings('movie'))
const editPaths = ref<string[]>([])
const saveError = ref('')
const saving = ref(false)

function openSettings(lib: Library) {
  editLib.value = lib
  editSettings.value = {
    ...defaultSettings(lib.media_type),
    ...lib.settings,
  }
  editPaths.value = [...lib.paths]
  saveError.value = ''
}

async function saveSettings() {
  if (!editLib.value) return
  saving.value = true
  saveError.value = ''
  try {
    const paths = editPaths.value.filter(p => p.trim())
    if (paths.length && paths.join(',') !== editLib.value.paths.join(',')) {
      await apiFetch(`/api/libraries/${editLib.value.id}`, {
        method: 'PUT',
        body: JSON.stringify({ name: editLib.value.name, paths }),
      })
    }
    const updated = await apiFetch<Library>(`/api/libraries/${editLib.value.id}/settings`, {
      method: 'PUT',
      body: JSON.stringify(editSettings.value),
    })
    const idx = libraries.value.findIndex(l => l.id === updated.id)
    if (idx >= 0) libraries.value[idx] = updated
    editLib.value = null
  } catch (e: any) {
    saveError.value = e?.data?.error || 'Failed to save'
  }
  saving.value = false
}

const moreOpen = ref<number | null>(null)
const moreRefs: Record<number, HTMLElement> = {}

function setMoreRef(id: number, el: HTMLElement) {
  if (el) moreRefs[id] = el
}

function toggleMore(id: number) {
  moreOpen.value = moreOpen.value === id ? null : id
}

async function forceRefreshMetadata(id: number) {
  moreOpen.value = null
  try { await apiFetch(`/api/libraries/${id}/refresh-metadata`, { method: 'POST' }) } catch {}
}

async function forceRefreshImages(id: number) {
  moreOpen.value = null
  try { await apiFetch(`/api/libraries/${id}/refresh-images`, { method: 'POST' }) } catch {}
}

async function scanLib(id: number) {
  scanning.value = id
  try { await apiFetch(`/api/libraries/${id}/scan`, { method: 'POST' }) } catch {}
  scanning.value = null
}

async function deleteLib(id: number) {
  if (!confirm('Delete this library and all its media data?')) return
  try {
    await apiFetch(`/api/libraries/${id}`, { method: 'DELETE' })
    libraries.value = libraries.value.filter(l => l.id !== id)
  } catch {}
}

async function fetchLibraries() {
  try { libraries.value = await apiFetch<Library[]>('/api/libraries') } catch {}
}

onMounted(() => {
  fetchLibraries()
  document.addEventListener('click', (e) => {
    if (moreOpen.value !== null) {
      const el = moreRefs[moreOpen.value]
      if (el && !el.contains(e.target as Node)) moreOpen.value = null
    }
  })
})
</script>

<style scoped>
.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 28px;
}
.page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.page-desc { font-size: 13px; color: var(--fg-3); margin: 6px 0 0; }
.header-actions { display: flex; gap: 8px; }

/* Library list */
.lib-list { display: flex; flex-direction: column; gap: 8px; }

.lib-card {
  display: flex;
  align-items: center;
  gap: 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-lg);
  padding: 18px 20px;
  transition: border-color 0.15s ease;
}

.lib-card:hover { border-color: var(--border-strong); }
.lib-card.active { border-color: var(--gold-deep); }

.lib-card-left { flex-shrink: 0; }

.lib-icon {
  width: 44px;
  height: 44px;
  border-radius: var(--r-md);
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--gold-soft);
  color: var(--gold);
}
.lib-icon.tv { background: rgba(140, 160, 255, 0.12); color: rgb(140, 160, 255); }
.lib-icon.music { background: rgba(200, 140, 255, 0.12); color: rgb(200, 140, 255); }
.lib-icon.book { background: rgba(140, 220, 180, 0.12); color: rgb(140, 220, 180); }

.lib-card-center { flex: 1; min-width: 0; }

/* Progress ring on icon */
.lib-icon { position: relative; }
.progress-ring {
  position: absolute;
  inset: -4px;
  width: calc(100% + 8px);
  height: calc(100% + 8px);
  transform: rotate(-90deg);
}
.ring-track {
  fill: none;
  stroke: rgba(255,255,255,0.06);
  stroke-width: 3;
}
.ring-fill {
  fill: none;
  stroke: var(--gold);
  stroke-width: 3;
  stroke-linecap: round;
  transition: stroke-dashoffset 0.4s ease;
}

/* Progress bar + text */
.lib-progress { margin-bottom: 8px; }
.progress-bar-track {
  height: 3px;
  background: rgba(255,255,255,0.06);
  border-radius: 2px;
  overflow: hidden;
  margin-bottom: 6px;
}
.progress-bar-fill {
  height: 100%;
  background: var(--gold);
  border-radius: 2px;
  transition: width 0.4s ease;
}
.progress-text {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-2);
}

.lib-header { display: flex; align-items: baseline; gap: 8px; margin-bottom: 4px; }
.lib-name { font-size: 15px; font-weight: 600; }
.lib-type {
  font-size: 10px;
  font-weight: 600;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--fg-3);
}

.lib-paths { display: flex; flex-wrap: wrap; gap: 4px 12px; margin-bottom: 8px; }
.lib-path {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
}

.lib-tags { display: flex; flex-wrap: wrap; gap: 4px; }
.tag {
  font-size: 9px;
  font-weight: 600;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  padding: 2px 8px;
  border-radius: 100px;
  display: inline-flex;
  align-items: center;
  gap: 3px;
}
.tag.meta { background: rgba(255, 255, 255, 0.05); color: var(--fg-2); }
.tag.art { background: rgba(140, 160, 255, 0.1); color: rgb(140, 160, 255); }
.tag.rate { background: rgba(255, 180, 100, 0.1); color: rgb(255, 180, 100); }
.tag.watch { background: rgba(111, 191, 124, 0.1); color: var(--good); }
.tag.lang { background: rgba(255, 255, 255, 0.05); color: var(--fg-2); }

.lib-card-right {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.action-btn {
  width: 34px;
  height: 34px;
  border-radius: var(--r-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  border: 1px solid transparent;
  transition: all 0.15s ease;
}

.action-btn:hover {
  color: var(--fg-0);
  background: rgba(255, 255, 255, 0.06);
  border-color: var(--border);
}

.action-btn.danger:hover {
  color: var(--bad);
  background: rgba(217, 107, 107, 0.08);
  border-color: rgba(217, 107, 107, 0.2);
}

.action-btn:disabled { opacity: 0.5; cursor: not-allowed; }

.more-wrap { position: relative; }
.more-menu {
  position: absolute; top: calc(100% + 6px); right: 0; z-index: 20;
  min-width: 190px;
  background: var(--bg-3); border: 1px solid var(--border-strong);
  border-radius: var(--r-md); padding: 4px;
  box-shadow: var(--shadow-2);
}
.more-option {
  display: flex; align-items: center; gap: 8px;
  width: 100%; padding: 8px 12px;
  font-size: 13px; color: var(--fg-1);
  border-radius: var(--r-sm); cursor: pointer;
  transition: background 0.12s;
}
.more-option:hover {
  background: rgba(255,255,255,0.06);
}

@keyframes spin { to { transform: rotate(360deg); } }
.spinning { animation: spin 0.8s linear infinite; }

/* Empty state */
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 64px 0;
}

.empty-icon {
  width: 64px;
  height: 64px;
  border-radius: var(--r-lg);
  background: var(--bg-3);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  margin-bottom: 16px;
}

.empty-title { font-size: 16px; font-weight: 600; margin: 0 0 6px; }
.empty-desc { font-size: 13px; color: var(--fg-3); margin: 0 0 20px; }

/* Modal */
.modal-overlay {
  position: fixed;
  inset: 0;
  z-index: 100;
  background: rgba(0, 0, 0, 0.65);
  backdrop-filter: blur(12px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}

.modal-card {
  width: 100%;
  max-width: 860px;
  background: var(--bg-2);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-xl);
  display: flex;
  flex-direction: column;
  max-height: 90vh;
  box-shadow: var(--shadow-3);
}

.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 24px 28px 0;
}

.modal-header-left { display: flex; align-items: center; gap: 12px; }

.modal-lib-icon {
  width: 36px;
  height: 36px;
  border-radius: var(--r-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--gold-soft);
  color: var(--gold);
}
.modal-lib-icon.tv { background: rgba(140, 160, 255, 0.12); color: rgb(140, 160, 255); }
.modal-lib-icon.music { background: rgba(200, 140, 255, 0.12); color: rgb(200, 140, 255); }
.modal-lib-icon.book { background: rgba(140, 220, 180, 0.12); color: rgb(140, 220, 180); }

.modal-title { font-size: 18px; font-weight: 600; margin: 0; }
.modal-subtitle {
  font-size: 11px;
  color: var(--fg-3);
  font-family: var(--font-mono);
  text-transform: capitalize;
}

.modal-close {
  width: 32px;
  height: 32px;
  border-radius: var(--r-sm);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  transition: all 0.12s ease;
}
.modal-close:hover { background: rgba(255, 255, 255, 0.08); color: var(--fg-0); }

.modal-body {
  padding: 20px 28px;
  overflow-y: auto;
  flex: 1;
}

.modal-divider {
  height: 1px;
  background: var(--border);
  margin: 20px 0;
}

.modal-error {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 0 28px;
  padding: 10px 14px;
  background: rgba(217, 107, 107, 0.08);
  border: 1px solid rgba(217, 107, 107, 0.2);
  border-radius: var(--r-md);
  font-size: 13px;
  color: var(--bad);
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 16px 28px 24px;
  border-top: 1px solid var(--border);
}

/* Form elements */
.form-section { margin-bottom: 18px; }
.form-row { display: flex; gap: 12px; }
.form-group { display: flex; flex-direction: column; }

.form-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-2);
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  margin-bottom: 6px;
}

.form-input {
  height: 40px;
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 0 14px;
  color: var(--fg-0);
  font-size: 14px;
  outline: none;
  transition: border-color 0.12s ease;
  width: 100%;
}
.form-input:focus { border-color: var(--gold); }
.form-input::placeholder { color: var(--fg-3); }

.select-wrap {
  position: relative;
}
.form-select {
  width: 100%;
  height: 40px;
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 0 32px 0 14px;
  color: var(--fg-0);
  font-size: 14px;
  appearance: none;
  cursor: pointer;
}
.form-select:focus { border-color: var(--gold); outline: none; }
.select-icon {
  position: absolute;
  right: 12px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--fg-3);
  pointer-events: none;
}

.paths-list { display: flex; flex-direction: column; gap: 6px; }
.path-row { display: flex; gap: 6px; align-items: center; }
.path-remove {
  width: 34px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  color: var(--fg-3);
  transition: all 0.12s ease;
  flex-shrink: 0;
}
.path-remove:hover { color: var(--bad); border-color: rgba(217, 107, 107, 0.3); }

.add-path-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
  font-size: 12px;
  color: var(--fg-2);
  font-family: var(--font-mono);
  padding: 4px 0;
  transition: color 0.12s ease;
}
.add-path-btn:hover { color: var(--gold); }

/* Modal transition */
.modal-enter-active { transition: opacity 0.2s ease; }
.modal-enter-active .modal-card { transition: transform 0.2s ease, opacity 0.2s ease; }
.modal-leave-active { transition: opacity 0.15s ease; }
.modal-leave-active .modal-card { transition: transform 0.15s ease, opacity 0.15s ease; }
.modal-enter-from { opacity: 0; }
.modal-enter-from .modal-card { transform: scale(0.96) translateY(8px); opacity: 0; }
.modal-leave-to { opacity: 0; }
.modal-leave-to .modal-card { transform: scale(0.98); opacity: 0; }
</style>
