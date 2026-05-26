<script setup lang="ts">
definePageMeta({ layout: 'settings', middleware: 'admin' })

import type { Library, LibrarySettings } from '~~/shared/types'
import type { LibraryScanProgress } from '~/composables/useEventBus'

const { $heya } = useNuxtApp()
const { confirm } = useConfirm()
const { scanProgress } = useEventBus()

const libraries = ref<Library[]>([])
const loading = ref(true)
const scanning = ref<number | null>(null)
const scanningAll = ref(false)
const flash = ref<{ kind: 'ok' | 'err' | 'warn', text: string } | null>(null)

const showAdd = ref(false)
const addError = ref('')
const newLib = ref({
  name: '', media_type: 'movie', paths: [''],
  settings: defaultSettings('movie'),
})

const editLib = ref<Library | null>(null)
const editSettings = ref<LibrarySettings>(defaultSettings('movie'))
const editPaths = ref<string[]>([])
const saveError = ref('')
const saving = ref(false)
const showEdit = computed({
  get: () => editLib.value !== null,
  set: (v: boolean) => { if (!v) editLib.value = null },
})

function defaultSettings(type: string): LibrarySettings {
  const base: LibrarySettings = {
    watch: true, preferred_language: 'en', preferred_country: 'US',
    auto_collections: false, metadata_refresh_days: 0, fetch_ratings: true,
    save_nfo: false, save_images: false,
    enable_trickplay: false, generate_thumbnails: true,
  }
  if (type === 'movie') return { ...base, auto_collections: true }
  return base
}

function mediaIcon(type: string): string {
  return type === 'movie' ? 'film' : type === 'tv' ? 'tv' : type === 'music' ? 'music' : 'book'
}

function libProgress(id: number): LibraryScanProgress | null {
  return scanProgress.value[id] ?? null
}
function libPercent(id: number): number {
  const p = scanProgress.value[id]
  if (!p || p.total === 0) return 0
  return Math.min(p.processed / p.total, 1)
}
const hasAnyProgress = computed(() => Object.keys(scanProgress.value).length > 0)

async function fetchLibraries() {
  try {
    libraries.value = await $heya('/api/libraries') ?? []
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Failed to load libraries.' }
  } finally {
    loading.value = false
  }
}

async function scanLib(id: number) {
  scanning.value = id
  try {
    await $heya('/api/libraries/{id}/scan', { method: 'POST', path: { id } })
    flash.value = { kind: 'ok', text: 'Scan queued.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Scan failed.' }
  } finally {
    scanning.value = null
  }
}

async function cancelLib(id: number) {
  try {
    await $heya('/api/libraries/{id}/scan/cancel', { method: 'POST', path: { id } })
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Cancel failed.' }
  }
}

async function scanAll() {
  scanningAll.value = true
  try {
    await Promise.all(libraries.value.map(lib =>
      $heya('/api/libraries/{id}/scan', { method: 'POST', path: { id: lib.id } }),
    ))
    flash.value = { kind: 'ok', text: 'All libraries queued for scan.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Scan-all failed.' }
  } finally {
    scanningAll.value = false
  }
}

async function cancelAll() {
  try {
    await $heya('/api/libraries/scan/cancel-all', { method: 'POST' })
    flash.value = { kind: 'ok', text: 'All running scans cancelled.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Cancel-all failed.' }
  }
}

async function forceRefreshMetadata(id: number) {
  try {
    await $heya('/api/libraries/{id}/refresh-metadata', { method: 'POST', path: { id } })
    flash.value = { kind: 'ok', text: 'Metadata refresh queued.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Refresh failed.' }
  }
}

async function forceRefreshImages(id: number) {
  try {
    await $heya('/api/libraries/{id}/refresh-images', { method: 'POST', path: { id } })
    flash.value = { kind: 'ok', text: 'Image refresh queued.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Refresh failed.' }
  }
}

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
  if (!paths.length) { addError.value = 'At least one folder is required.'; return }
  try {
    await $heya('/api/libraries', {
      method: 'POST',
      body: { name: newLib.value.name, media_type: newLib.value.media_type, paths, settings: newLib.value.settings } as any,
    })
    showAdd.value = false
    flash.value = { kind: 'ok', text: `Created library "${newLib.value.name}".` }
    await fetchLibraries()
  } catch (e: any) {
    addError.value = e?.data?.error || e?.message || 'Failed to create library.'
  }
}

function openEdit(lib: Library) {
  editLib.value = lib
  editSettings.value = { ...defaultSettings(lib.media_type), ...lib.settings }
  editPaths.value = [...lib.paths]
  saveError.value = ''
}

async function saveEditSettings() {
  if (!editLib.value) return
  saving.value = true
  saveError.value = ''
  try {
    const paths = editPaths.value.filter(p => p.trim())
    const pathsChanged = paths.length && paths.join(',') !== editLib.value.paths.join(',')
    if (pathsChanged && !editLib.value.sources?.paths) {
      await $heya('/api/libraries/{id}', {
        method: 'PUT',
        path: { id: editLib.value.id },
        body: { name: editLib.value.name, paths } as any,
      })
    }
    const updated = await $heya('/api/libraries/{id}/settings', {
      method: 'PUT',
      path: { id: editLib.value.id },
      body: editSettings.value as any,
    }) as Library
    const idx = libraries.value.findIndex(l => l.id === updated.id)
    if (idx >= 0) libraries.value[idx] = updated
    editLib.value = null
    flash.value = { kind: 'ok', text: 'Library updated.' }
  } catch (e: any) {
    saveError.value = e?.data?.error || e?.message || 'Failed to save.'
  } finally {
    saving.value = false
  }
}

function isEnvLocked(lib: Library): boolean {
  return !!(lib.sources?.name || lib.sources?.paths || lib.sources?.media_type)
}

function envLockTooltip(lib: Library): string {
  const envVar = lib.sources?.name?.env_var
  if (envVar) {
    const base = envVar.replace(/_NAME$/, '')
    return `Locked by ${base}_NAME / _PATHS / _TYPE — remove the env vars to delete.`
  }
  return 'Locked by environment variables.'
}

async function deleteLib(lib: Library) {
  if (isEnvLocked(lib)) return
  const ok = await confirm({
    title: `Delete "${lib.name}"?`,
    message: `Removes the library and all its media data. ${lib.paths.length} ${lib.paths.length === 1 ? 'path' : 'paths'} on disk are not touched.`,
    destructive: true,
    confirmLabel: 'Delete',
  })
  if (!ok) return
  try {
    await $heya('/api/libraries/{id}', { method: 'DELETE', path: { id: lib.id } })
    libraries.value = libraries.value.filter(l => l.id !== lib.id)
    flash.value = { kind: 'ok', text: 'Library deleted.' }
  } catch (e: any) {
    flash.value = { kind: 'err', text: e?.message ?? 'Delete failed.' }
  }
}

const totalByKind = computed(() => {
  const m: Record<string, number> = {}
  for (const l of libraries.value) m[l.media_type] = (m[l.media_type] ?? 0) + 1
  return m
})

onMounted(fetchLibraries)
</script>

<template>
  <div>
    <header class="sv2-page-head">
      <h2 class="sv2-page-title">Libraries</h2>
      <p class="sv2-page-desc">
        Your media folders. Each library has its own metadata defaults
        (NFO, images, refresh window, trickplay). Env-locked libraries
        carry a key badge and can't be deleted from the UI.
      </p>
    </header>

    <div class="tiles">
      <MetricTile label="Total" :value="libraries.length" icon="folder" />
      <MetricTile label="Movies" :value="totalByKind.movie ?? 0" icon="film" />
      <MetricTile label="TV"     :value="totalByKind.tv ?? 0"    icon="tv" />
      <MetricTile label="Music"  :value="totalByKind.music ?? 0" icon="music" />
      <MetricTile label="Books"  :value="totalByKind.book ?? 0"  icon="book" />
      <MetricTile
        label="Active scans"
        :value="Object.keys(scanProgress).length"
        icon="pulse"
        :tone="hasAnyProgress ? 'good' : 'neutral'"
      />
    </div>

    <SettingsSection title="Configured libraries" icon="folder">
      <template #actions>
        <button v-if="hasAnyProgress" class="sv2-btn danger" @click="cancelAll">
          <Icon name="close" :size="12" />
          Cancel all
        </button>
        <button v-if="libraries.length" class="sv2-btn ghost" :disabled="scanningAll" @click="scanAll">
          <Icon :name="scanningAll ? 'spinner' : 'refresh'" :size="12" />
          {{ scanningAll ? 'Queuing…' : 'Scan all' }}
        </button>
        <button class="sv2-btn primary" @click="openAdd">
          <Icon name="plus" :size="12" />
          Add library
        </button>
      </template>

      <div v-if="loading" class="loading-state"><Icon name="spinner" :size="14" /> Loading…</div>

      <div v-else-if="libraries.length === 0" class="empty-state">
        <div class="empty-icon"><Icon name="folder" :size="28" /></div>
        <div class="empty-title">No libraries yet</div>
        <p class="empty-desc">Point Heya at a folder of movies, TV, music, or books to get started.</p>
        <button class="sv2-btn primary" @click="openAdd">
          <Icon name="plus" :size="12" />
          Add your first library
        </button>
      </div>

      <div v-else class="lib-list">
        <div
          v-for="lib in libraries"
          :key="lib.id"
          class="lib-card"
          :class="{ scanning: libProgress(lib.id) }"
        >
          <div class="lib-left">
            <div class="lib-icon" :class="`kind-${lib.media_type}`">
              <svg v-if="libProgress(lib.id)" class="progress-ring" viewBox="0 0 48 48">
                <circle class="ring-track" cx="24" cy="24" r="20" />
                <circle
                  class="ring-fill"
                  cx="24" cy="24" r="20"
                  :stroke-dasharray="125.66"
                  :stroke-dashoffset="125.66 - 125.66 * libPercent(lib.id)"
                />
              </svg>
              <Icon :name="mediaIcon(lib.media_type)" :size="18" />
            </div>
          </div>

          <div class="lib-body">
            <div class="lib-row">
              <span class="lib-name">{{ lib.name }}</span>
              <span class="lib-type mono">{{ lib.media_type }}</span>
              <span v-if="isEnvLocked(lib)" class="env-badge" :title="envLockTooltip(lib)">
                <Icon name="key" :size="10" /> env
              </span>
            </div>

            <div v-if="libProgress(lib.id)" class="lib-progress">
              <div class="prog-track">
                <div class="prog-fill" :style="{ width: (libPercent(lib.id) * 100) + '%' }" />
              </div>
              <div class="prog-meta">
                Processed {{ libProgress(lib.id)!.processed }} / {{ libProgress(lib.id)!.total }} files
                <template v-if="libProgress(lib.id)!.matched">· {{ libProgress(lib.id)!.matched }} matched</template>
              </div>
            </div>

            <div v-else class="lib-paths">
              <span v-for="(p, i) in lib.paths" :key="i" class="lib-path mono">
                <Icon name="folder" :size="11" />
                {{ p }}
              </span>
            </div>

            <div class="lib-tags">
              <span class="tag">heya.media</span>
              <span v-if="lib.settings?.fetch_ratings" class="tag rate"><Icon name="star" :size="9" /> Ratings</span>
              <span v-if="lib.settings?.save_nfo"      class="tag nfo"><Icon name="clipboard" :size="9" /> NFO</span>
              <span v-if="lib.settings?.save_images"   class="tag img"><Icon name="image" :size="9" /> Images</span>
              <span v-if="lib.settings?.watch"         class="tag watch"><Icon name="eye" :size="9" /> Watching</span>
              <span v-if="lib.settings?.enable_trickplay" class="tag trick">Trickplay</span>
              <span v-if="lib.settings?.preferred_language" class="tag lang">{{ lib.settings.preferred_language.toUpperCase() }}</span>
            </div>
          </div>

          <div class="lib-actions">
            <button class="row-btn" :title="`Configure ${lib.name}`" @click="openEdit(lib)">
              <Icon name="settings" :size="14" />
            </button>

            <AppMenu align="end" :side-offset="8">
              <template #trigger>
                <span class="row-btn"><Icon name="more" :size="14" /></span>
              </template>
              <template #default="{ close }">
                <button class="menu-item" @click="forceRefreshMetadata(lib.id); close()">
                  <Icon name="refresh" :size="13" />
                  Refresh metadata
                </button>
                <button class="menu-item" @click="forceRefreshImages(lib.id); close()">
                  <Icon name="image" :size="13" />
                  Refresh images
                </button>
              </template>
            </AppMenu>

            <button v-if="libProgress(lib.id)" class="row-btn danger" title="Cancel scan" @click="cancelLib(lib.id)">
              <Icon name="close" :size="14" />
            </button>
            <button v-else class="row-btn" :disabled="scanning === lib.id" title="Scan now" @click="scanLib(lib.id)">
              <Icon :name="scanning === lib.id ? 'spinner' : 'refresh'" :size="14" />
            </button>

            <button
              class="row-btn danger"
              :disabled="isEnvLocked(lib)"
              :title="isEnvLocked(lib) ? envLockTooltip(lib) : `Delete ${lib.name}`"
              @click="deleteLib(lib)"
            >
              <Icon :name="isEnvLocked(lib) ? 'key' : 'trash'" :size="14" />
            </button>
          </div>
        </div>
      </div>
    </SettingsSection>

    <div v-if="flash" class="sv2-flash" :class="flash.kind">
      <Icon :name="flash.kind === 'ok' ? 'check' : 'warning'" :size="13" />
      {{ flash.text }}
    </div>

    <AppDialog v-model="showAdd" title="Add library" size="lg">
      <form class="dialog-form" @submit.prevent="addLibrary">
        <div class="form-row">
          <div class="form-field grow">
            <label class="form-label">Name</label>
            <input v-model="newLib.name" class="sv2-input" placeholder="My Movies" required />
          </div>
          <div class="form-field">
            <label class="form-label">Type</label>
            <select v-model="newLib.media_type" class="sv2-select" @change="onTypeChange">
              <option value="movie">Movie</option>
              <option value="tv">TV Show</option>
              <option value="music">Music</option>
              <option value="book">Book</option>
            </select>
          </div>
        </div>

        <div class="form-field">
          <label class="form-label">Folders</label>
          <div class="paths-list">
            <div v-for="(p, i) in newLib.paths" :key="i" class="path-row">
              <LibraryPathInput
                :model-value="p ?? ''"
                @update:model-value="(v: string) => newLib.paths.splice(i, 1, v)"
              />
              <button v-if="newLib.paths.length > 1" type="button" class="path-remove" @click="newLib.paths.splice(i, 1)">
                <Icon name="close" :size="11" />
              </button>
            </div>
          </div>
          <button type="button" class="add-path" @click="newLib.paths.push('')">
            <Icon name="plus" :size="11" /> Add folder
          </button>
        </div>

        <div class="dialog-divider" />

        <LibrarySettingsPanel v-model="newLib.settings" :media-type="newLib.media_type" />

        <div v-if="addError" class="form-error">
          <Icon name="warning" :size="13" /> {{ addError }}
        </div>
      </form>
      <template #footer="{ close }">
        <button class="sv2-btn ghost" @click="close()">Cancel</button>
        <button class="sv2-btn primary" @click="addLibrary">
          <Icon name="plus" :size="12" /> Add library
        </button>
      </template>
    </AppDialog>

    <AppDialog v-model="showEdit" :title="editLib?.name ?? 'Edit library'" :description="editLib ? `${editLib.media_type} library — paths and metadata defaults` : ''" size="lg">
      <div v-if="editLib" class="dialog-form">
        <div class="dialog-identity">
          <div class="dialog-icon" :class="`kind-${editLib.media_type}`">
            <Icon :name="mediaIcon(editLib.media_type)" :size="18" />
          </div>
          <div class="dialog-identity-text">
            <div class="dialog-identity-name">{{ editLib.name }}</div>
            <div class="dialog-identity-sub mono">{{ editLib.media_type }} · {{ editLib.paths.length }} {{ editLib.paths.length === 1 ? 'path' : 'paths' }}</div>
          </div>
        </div>

        <div class="form-field">
          <label class="form-label">
            Folders
            <span v-if="editLib.sources?.paths" class="env-badge" :title="`Locked by ${editLib.sources.paths.env_var}`">
              <Icon name="key" :size="10" /> {{ editLib.sources.paths.env_var }}
            </span>
          </label>
          <div v-if="editLib.sources?.paths" class="paths-locked">
            <div v-for="(p, i) in editPaths" :key="i" class="path-locked mono">
              <Icon name="folder" :size="11" /> {{ p }}
            </div>
          </div>
          <div v-else>
            <div class="paths-list">
              <div v-for="(p, i) in editPaths" :key="i" class="path-row">
                <LibraryPathInput
                  :model-value="p ?? ''"
                  @update:model-value="(v: string) => editPaths.splice(i, 1, v)"
                />
                <button v-if="editPaths.length > 1" type="button" class="path-remove" @click="editPaths.splice(i, 1)">
                  <Icon name="close" :size="11" />
                </button>
              </div>
            </div>
            <button type="button" class="add-path" @click="editPaths.push('')">
              <Icon name="plus" :size="11" /> Add folder
            </button>
          </div>
        </div>

        <div class="dialog-divider" />

        <LibrarySettingsPanel v-model="editSettings" :media-type="editLib.media_type" />

        <div v-if="saveError" class="form-error">
          <Icon name="warning" :size="13" /> {{ saveError }}
        </div>
      </div>

      <template #footer="{ close }">
        <button class="sv2-btn ghost" @click="close()">Cancel</button>
        <button class="sv2-btn primary" :disabled="saving" @click="saveEditSettings">
          <Icon :name="saving ? 'spinner' : 'check'" :size="12" />
          {{ saving ? 'Saving…' : 'Save changes' }}
        </button>
      </template>
    </AppDialog>
  </div>
</template>

<style scoped>
.sv2-page-head { margin-bottom: 28px; }
.sv2-page-title { font-size: 26px; font-weight: 600; letter-spacing: -0.02em; margin: 0; }
.sv2-page-desc { margin: 6px 0 0; font-size: 13px; color: var(--fg-3); line-height: 1.55; }

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 8px;
  margin-bottom: 28px;
}

.loading-state, .empty-state {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--fg-3); font-size: 12.5px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.empty-state {
  flex-direction: column;
  padding: 36px 18px;
  text-align: center;
}
.empty-icon {
  width: 56px; height: 56px;
  border-radius: var(--r-md);
  background: var(--bg-3);
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-3);
  margin-bottom: 6px;
}
.empty-title { font-size: 14px; font-weight: 600; color: var(--fg-1); }
.empty-desc { margin: 0 0 10px; font-size: 12.5px; color: var(--fg-3); line-height: 1.4; }

.lib-list { display: flex; flex-direction: column; gap: 8px; }
.lib-card {
  display: flex; align-items: flex-start; gap: 14px;
  padding: 14px 16px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  transition: border-color 0.15s ease;
}
.lib-card:hover { border-color: var(--border-strong); }
.lib-card.scanning { border-color: var(--gold); background: rgba(230, 185, 74, 0.04); }

.lib-left { flex-shrink: 0; }
.lib-icon {
  width: 38px; height: 38px;
  border-radius: var(--r-sm);
  background: var(--bg-0);
  color: var(--gold);
  display: flex; align-items: center; justify-content: center;
  position: relative;
  flex-shrink: 0;
}
.lib-icon.kind-tv    { color: rgb(140, 160, 255); background: rgba(140, 160, 255, 0.10); }
.lib-icon.kind-music { color: rgb(200, 140, 255); background: rgba(200, 140, 255, 0.10); }
.lib-icon.kind-book  { color: rgb(140, 220, 180); background: rgba(140, 220, 180, 0.10); }

.progress-ring {
  position: absolute; inset: -4px;
  width: calc(100% + 8px); height: calc(100% + 8px);
  transform: rotate(-90deg);
  pointer-events: none;
}
.ring-track { fill: none; stroke: rgba(255,255,255,0.06); stroke-width: 3; }
.ring-fill  { fill: none; stroke: var(--gold); stroke-width: 3; stroke-linecap: round; transition: stroke-dashoffset 0.4s ease; }

.lib-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
.lib-row { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; }
.lib-name { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.lib-type {
  font-family: var(--font-mono); font-size: 10px;
  text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--fg-3);
}
.env-badge {
  display: inline-flex; align-items: center; gap: 3px;
  padding: 1px 8px;
  border-radius: 999px;
  background: var(--gold-soft); color: var(--gold);
  font-family: var(--font-mono);
  font-size: 9px; font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: lowercase;
}

.lib-paths { display: flex; flex-wrap: wrap; gap: 4px 12px; }
.lib-path {
  display: inline-flex; align-items: center; gap: 5px;
  font-size: 11px; color: var(--fg-3);
}
.mono { font-family: var(--font-mono); }

.lib-progress { display: flex; flex-direction: column; gap: 4px; }
.prog-track {
  height: 4px; border-radius: 2px;
  background: rgba(255,255,255,0.06); overflow: hidden;
}
.prog-fill { height: 100%; background: var(--gold); transition: width 0.4s ease; }
.prog-meta { font-family: var(--font-mono); font-size: 11px; color: var(--fg-2); }

.lib-tags { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 2px; }
.tag {
  display: inline-flex; align-items: center; gap: 3px;
  padding: 2px 8px; border-radius: 999px;
  font-family: var(--font-mono); font-size: 9px; font-weight: 600;
  letter-spacing: 0.04em;
  background: rgba(255,255,255,0.04); color: var(--fg-3);
}
.tag.rate  { background: rgba(255, 180, 100, 0.10); color: rgb(255, 180, 100); }
.tag.nfo   { background: rgba(200, 200, 255, 0.08); color: rgb(180, 180, 230); }
.tag.img   { background: rgba(140, 220, 180, 0.10); color: rgb(140, 220, 180); }
.tag.watch { background: rgba(111, 191, 124, 0.10); color: var(--good); }
.tag.trick { background: rgba(200, 140, 255, 0.10); color: rgb(200, 140, 255); }
.tag.lang  { background: rgba(140, 160, 255, 0.10); color: rgb(140, 160, 255); }

.lib-actions { display: flex; gap: 4px; flex-shrink: 0; }
.row-btn {
  width: 30px; height: 30px;
  border-radius: var(--r-sm);
  display: inline-flex; align-items: center; justify-content: center;
  color: var(--fg-3); border: 1px solid transparent;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.row-btn:hover:not(:disabled) {
  color: var(--fg-0);
  background: rgba(255,255,255,0.06);
  border-color: var(--border);
}
.row-btn.danger:hover:not(:disabled) {
  color: var(--bad);
  background: rgba(217,107,107,0.10);
  border-color: rgba(217,107,107,0.25);
}
.row-btn:disabled { opacity: 0.4; cursor: not-allowed; }

.menu-item {
  display: flex; align-items: center; gap: 8px;
  width: 100%; padding: 8px 12px;
  font-size: 12.5px; color: var(--fg-1);
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: background 0.12s;
}
.menu-item:hover { background: rgba(255,255,255,0.06); }

/* dialog inner */
.dialog-identity {
  display: flex; align-items: center; gap: 12px;
  padding: 12px 14px;
  background: var(--bg-1);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
}
.dialog-icon {
  width: 34px; height: 34px;
  border-radius: var(--r-sm);
  background: var(--gold-soft); color: var(--gold);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
}
.dialog-icon.kind-tv    { color: rgb(140, 160, 255); background: rgba(140, 160, 255, 0.10); }
.dialog-icon.kind-music { color: rgb(200, 140, 255); background: rgba(200, 140, 255, 0.10); }
.dialog-icon.kind-book  { color: rgb(140, 220, 180); background: rgba(140, 220, 180, 0.10); }
.dialog-identity-text { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.dialog-identity-name { font-size: 14px; font-weight: 600; color: var(--fg-0); }
.dialog-identity-sub { font-size: 11px; color: var(--fg-3); text-transform: capitalize; }

.dialog-form {
  display: flex; flex-direction: column; gap: 14px;
}
.form-row { display: flex; gap: 12px; }
.form-field { display: flex; flex-direction: column; gap: 6px; }
.form-field.grow { flex: 1; }
.form-label {
  font-family: var(--font-mono);
  font-size: 10px; font-weight: 700;
  text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--fg-3);
}
.form-label .env-badge { margin-left: 6px; }

.sv2-input, .sv2-select {
  background: var(--bg-0);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  color: var(--fg-0);
  font-size: 13px;
  padding: 9px 12px;
  outline: none;
  transition: border-color 0.12s;
}
.sv2-input:focus, .sv2-select:focus { border-color: var(--gold); }
.sv2-select { cursor: pointer; min-width: 130px; }

.paths-list { display: flex; flex-direction: column; gap: 6px; }
.path-row { display: flex; gap: 6px; align-items: center; }
.path-remove {
  width: 36px; height: 36px;
  border-radius: var(--r-sm);
  border: 1px solid var(--border);
  color: var(--fg-3);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
  transition: color 0.12s, border-color 0.12s, background 0.12s;
}
.path-remove:hover { color: var(--bad); border-color: rgba(217,107,107,0.30); background: rgba(217,107,107,0.06); }

.add-path {
  display: inline-flex; align-items: center; gap: 5px;
  margin-top: 8px;
  font-family: var(--font-mono); font-size: 11px;
  color: var(--fg-2);
  padding: 4px 6px;
  border-radius: var(--r-xs);
  cursor: pointer;
  transition: color 0.12s, background 0.12s;
}
.add-path:hover { color: var(--gold); background: var(--gold-soft); }

.paths-locked { display: flex; flex-direction: column; gap: 4px; }
.path-locked {
  display: flex; align-items: center; gap: 8px;
  padding: 10px 12px;
  background: var(--bg-3);
  border: 1px dashed var(--border);
  border-radius: var(--r-sm);
  font-size: 12px; color: var(--fg-2);
}
.dialog-divider { height: 1px; background: var(--border); margin: 6px 0; }

.form-error {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 12px;
  background: rgba(217,107,107,0.10);
  border: 1px solid rgba(217,107,107,0.25);
  border-radius: var(--r-sm);
  color: var(--bad);
  font-size: 12px;
}

.sv2-btn {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 7px 12px;
  border-radius: var(--r-sm);
  font-size: 12px; font-weight: 500;
  cursor: pointer;
  transition: background 0.12s, color 0.12s, border-color 0.12s;
}
.sv2-btn.primary { background: var(--gold); color: #1a1408; }
.sv2-btn.primary:hover:not(:disabled) { background: var(--gold-deep); }
.sv2-btn.ghost { border: 1px solid var(--border); background: var(--bg-2); color: var(--fg-2); }
.sv2-btn.ghost:hover:not(:disabled) { border-color: var(--border-strong); color: var(--fg-0); }
.sv2-btn.danger {
  border: 1px solid rgba(217,107,107,0.30);
  background: rgba(217,107,107,0.06);
  color: var(--bad);
}
.sv2-btn.danger:hover:not(:disabled) { background: rgba(217,107,107,0.12); }
.sv2-btn:disabled { opacity: 0.5; cursor: not-allowed; }

.sv2-flash {
  margin-top: 16px;
  padding: 10px 14px;
  border-radius: var(--r-sm);
  font-size: 12px;
  display: flex; align-items: center; gap: 8px;
}
.sv2-flash.ok   { background: rgba(111,191,124,0.10); border: 1px solid rgba(111,191,124,0.25); color: var(--good); }
.sv2-flash.warn { background: rgba(230,185,74,0.10); border: 1px solid rgba(230,185,74,0.30); color: var(--gold); }
.sv2-flash.err  { background: rgba(217,107,107,0.10); border: 1px solid rgba(217,107,107,0.30); color: var(--bad); }
</style>
