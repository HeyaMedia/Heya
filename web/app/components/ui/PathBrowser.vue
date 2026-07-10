<!--
  PathBrowser — folder picker for local filesystem paths.

  Redesigned with a clearer mental model:
   - The trigger looks like a status-readout of the current path (not an
     editable input fighting with the dropdown).
   - The dropdown has three distinct sections — breadcrumbs (where am I?),
     a filter (narrow down this folder), the list (click to descend), plus
     a sticky footer with the selected path and an obvious confirm CTA.
   - Keyboard nav: ↑/↓ move highlight, Enter descends or confirms, Esc
     closes, Backspace climbs to parent when filter is empty.
   - Inline-editable mode: clicking the path readout flips it into a
     text input for direct path entry; Enter resolves + navigates.

  Backed by the same /api/fs/browse endpoint as before. Teleported to
  <body> so the dropdown isn't clipped by modal `overflow:hidden`.
-->
<template>
  <div class="path-browser" ref="rootEl">
    <!-- Trigger: dual-mode (readout when closed, input when typing) -->
    <div class="pb-trigger" :class="{ 'is-active': open_ }">
      <Icon name="folder" :size="14" class="pb-trigger-icon" />
      <input
        v-if="typing"
        ref="typingInputEl"
        v-model="typingValue"
        class="pb-trigger-input"
        placeholder="/path/to/media"
        @keydown.enter.prevent="commitTyped"
        @keydown.escape.prevent="cancelTyping"
        @blur="cancelTyping"
      />
      <button
        v-else
        type="button"
        class="pb-trigger-button"
        @click="openPicker"
      >
        <span v-if="modelValue" class="pb-trigger-path">{{ modelValue }}</span>
        <span v-else class="pb-trigger-placeholder">Choose a folder…</span>
        <Icon name="chevdown" :size="11" class="pb-trigger-chev" :class="{ rot: open_ }" />
      </button>
      <button
        v-if="!typing && modelValue"
        type="button"
        class="pb-trigger-mini"
        title="Edit path manually"
        aria-label="Edit path manually"
        @click="startTyping"
      >
        <Icon name="pencil" :size="11" />
      </button>
      <button
        v-if="!typing && modelValue"
        type="button"
        class="pb-trigger-mini"
        title="Clear path"
        aria-label="Clear path"
        @click="clearValue"
      >
        <Icon name="close" :size="11" />
      </button>
    </div>

    <!-- Panel rendered inline (NOT teleported). Teleporting puts the
         panel outside the modal DOM tree, which triggers the modal's
         own click-outside handler the moment you click a folder row.
         The modal has plenty of vertical space and reasonable scroll
         behaviour, so inline absolute positioning works fine. -->
      <Transition name="pb-drop">
        <div
          v-if="open_"
          ref="dropEl"
          class="pb-panel surface"
          @keydown.down.prevent="moveHighlight(1)"
          @keydown.up.prevent="moveHighlight(-1)"
          @keydown.enter.prevent="enterHighlighted"
          @keydown.escape.prevent="close"
          @keydown.backspace="onBackspace"
          tabindex="-1"
        >
          <!-- Header: breadcrumbs -->
          <div class="pb-header">
            <button
              type="button"
              class="pb-crumb pb-crumb-root"
              :class="{ active: currentPath === '/' }"
              @click="navigate('/')"
              title="Filesystem root"
            >
              <Icon name="hard-drives" :size="13" />
            </button>
            <template v-for="(crumb, i) in breadcrumbs" :key="i">
              <span class="pb-crumb-sep">/</span>
              <button
                type="button"
                class="pb-crumb"
                :class="{ active: i === breadcrumbs.length - 1 }"
                @click="navigate(crumb.path)"
              >{{ crumb.label }}</button>
            </template>
          </div>

          <!-- Filter -->
          <div class="pb-filter">
            <Icon name="search" :size="13" class="pb-filter-icon" />
            <input
              ref="filterInputEl"
              v-model="filterText"
              class="pb-filter-input"
              placeholder="Filter folders in this directory"
              @keydown.down.prevent="moveHighlight(1)"
              @keydown.up.prevent="moveHighlight(-1)"
              @keydown.enter.prevent="enterHighlighted"
              @keydown.escape.prevent="filterText ? filterText = '' : close()"
              @keydown.backspace="onBackspace"
            />
            <button
              v-if="filterText"
              type="button"
              class="pb-filter-clear"
              @click="filterText = ''"
              title="Clear filter"
            >
              <Icon name="close" :size="10" />
            </button>
          </div>

          <!-- Folder list -->
          <div class="pb-list scroll" ref="listEl">
            <!-- Parent shortcut -->
            <button
              v-if="parent"
              type="button"
              class="pb-row pb-row-parent"
              :class="{ highlighted: highlightIdx === -1 }"
              @mouseenter="highlightIdx = -1"
              @click="navigate(parent)"
            >
              <Icon name="back" :size="13" class="pb-row-icon muted" />
              <span class="pb-row-name">Parent directory</span>
              <span class="pb-row-meta">..</span>
            </button>

            <!-- Loading / empty / list -->
            <div v-if="loading" class="pb-status">
              <Icon name="loading" :size="16" class="pb-spin" />
              <span>Reading directory…</span>
            </div>
            <div v-else-if="!filteredEntries.length" class="pb-status pb-empty">
              <Icon :name="filterText ? 'search' : 'folder'" :size="18" />
              <span>{{ filterText ? `No folders match "${filterText}"` : 'No subfolders here' }}</span>
            </div>
            <button
              v-else
              v-for="(entry, idx) in filteredEntries"
              :key="entry.path"
              type="button"
              class="pb-row"
              :class="{ highlighted: idx === highlightIdx }"
              :data-idx="idx"
              @mouseenter="highlightIdx = idx"
              @click="navigate(entry.path)"
            >
              <Icon name="folder" :size="14" class="pb-row-icon gold" />
              <span class="pb-row-name" v-if="filterText" v-html="highlightMatch(entry.name)" />
              <span class="pb-row-name" v-else>{{ entry.name }}</span>
              <Icon name="chevright" :size="10" class="pb-row-arrow" />
            </button>
          </div>

          <!-- Sticky footer: current path + confirm -->
          <div class="pb-footer">
            <div class="pb-footer-path" :title="currentPath">
              <Icon name="folder" :size="11" class="pb-footer-icon" />
              <code>{{ currentPath }}</code>
            </div>
            <button
              type="button"
              class="pb-confirm"
              :disabled="!currentPath"
              @click="confirmSelection"
            >
              <Icon name="check" :size="12" />
              <span>Use this folder</span>
            </button>
          </div>
        </div>
      </Transition>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  modelValue: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

interface DirEntry { name: string; path: string }

const open_ = ref(false)
const loading = ref(false)
const currentPath = ref('/')
const parent = ref('')
const entries = ref<DirEntry[]>([])
const filterText = ref('')
const highlightIdx = ref(-1)
const typing = ref(false)
const typingValue = ref('')

const rootEl = ref<HTMLElement>()
const dropEl = ref<HTMLElement>()
const listEl = ref<HTMLElement>()
const filterInputEl = ref<HTMLInputElement>()
const typingInputEl = ref<HTMLInputElement>()

// Hoist $heya out of fetchDir — calling useNuxtApp inside an async fn
// triggered while the Vue instance isn't the active one can silently
// hang the request. (We were burned by exactly this pattern earlier
// with cardCtxOpts in the movies/tv pages.)
const { $heya } = useNuxtApp()

const filteredEntries = computed(() => {
  if (!filterText.value) return entries.value
  const q = filterText.value.toLowerCase()
  return entries.value.filter(e => e.name.toLowerCase().includes(q))
})

const breadcrumbs = computed(() => {
  const parts = currentPath.value.split('/').filter(Boolean)
  let acc = ''
  return parts.map(part => {
    acc += '/' + part
    return { label: part, path: acc }
  })
})

async function fetchDir(path: string) {
  loading.value = true
  highlightIdx.value = -1
  try {
    const data = await $heya('/api/fs/browse', { query: { path } }) as { path: string; parent: string; entries: DirEntry[] | null }
    currentPath.value = data.path
    parent.value = data.parent
    // The API returns `entries: null` (not `[]`) for empty directories.
    // Without the coalesce the `entries.value` ref becomes null and the
    // filteredEntries computed throws on .filter, hanging the panel in
    // its loading state.
    entries.value = data.entries ?? []
    nextTick(() => { if (listEl.value) listEl.value.scrollTop = 0 })
  } catch {
    entries.value = []
  }
  loading.value = false
}

function openPicker() {
  open_.value = true
  fetchDir(props.modelValue || '/')
  nextTick(() => filterInputEl.value?.focus())
}

function close() {
  open_.value = false
  filterText.value = ''
}

function navigate(path: string) {
  fetchDir(path)
  filterText.value = ''
  highlightIdx.value = -1
  nextTick(() => filterInputEl.value?.focus())
}

function confirmSelection() {
  emit('update:modelValue', currentPath.value)
  close()
}

function clearValue() {
  emit('update:modelValue', '')
}

// Inline-edit mode: lets power-users paste / type a full path directly.
function startTyping() {
  typing.value = true
  typingValue.value = props.modelValue
  nextTick(() => {
    typingInputEl.value?.focus()
    typingInputEl.value?.select()
  })
}
function cancelTyping() { typing.value = false }
function commitTyped() {
  const v = typingValue.value.trim()
  if (v) emit('update:modelValue', v)
  typing.value = false
  if (open_.value) fetchDir(v || '/')
}

// Keyboard navigation in the dropdown.
function moveHighlight(delta: number) {
  const n = filteredEntries.value.length
  if (!n) return
  // -1 means "parent" row; with no parent it's a no-op.
  const hasParent = !!parent.value
  const min = hasParent ? -1 : 0
  let next = highlightIdx.value + delta
  if (next < min) next = n - 1
  if (next >= n) next = min
  highlightIdx.value = next
  scrollHighlightIntoView()
}
function scrollHighlightIntoView() {
  if (highlightIdx.value < 0) return
  const el = listEl.value?.querySelector<HTMLElement>(`[data-idx="${highlightIdx.value}"]`)
  el?.scrollIntoView({ block: 'nearest' })
}
function enterHighlighted() {
  if (highlightIdx.value === -1) {
    if (parent.value) navigate(parent.value)
    else confirmSelection()
    return
  }
  const entry = filteredEntries.value[highlightIdx.value]
  if (entry) navigate(entry.path)
}
function onBackspace(e: KeyboardEvent) {
  // Backspace from an empty filter climbs to parent — like a file manager.
  if (filterText.value === '' && parent.value) {
    e.preventDefault()
    navigate(parent.value)
  }
}

function highlightMatch(name: string): string {
  const q = filterText.value.toLowerCase()
  const idx = name.toLowerCase().indexOf(q)
  if (idx === -1) return name
  return `${name.slice(0, idx)}<b class="pb-hl">${name.slice(idx, idx + q.length)}</b>${name.slice(idx + q.length)}`
}

onClickOutside(rootEl, () => { if (open_.value) close() }, { ignore: [dropEl] })

// Close the dropdown when the bound value is cleared externally.
watch(() => props.modelValue, (v) => {
  if (!v && open_.value) close()
})
</script>

<style scoped>
.path-browser { position: relative; flex: 1; min-width: 0; }

/* ── Trigger ──────────────────────────────────────────────────────── */
.pb-trigger {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  height: 40px;
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 0 8px 0 12px;
  transition: border-color 0.12s;
}
.pb-trigger:hover { border-color: var(--border-strong); }
.pb-trigger.is-active { border-color: var(--gold); }

.pb-trigger-icon { color: var(--gold); flex-shrink: 0; }

.pb-trigger-button {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: 12px;
  text-align: left;
  color: var(--fg-0);
}
.pb-trigger-path {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pb-trigger-placeholder { color: var(--fg-3); font-family: var(--font-sans); flex: 1; }
.pb-trigger-chev {
  color: var(--fg-3);
  flex-shrink: 0;
  transition: transform 0.2s;
}
.pb-trigger-chev.rot { transform: rotate(180deg); color: var(--gold); }

.pb-trigger-input {
  flex: 1;
  height: 100%;
  background: transparent;
  border: 0;
  outline: none;
  color: var(--fg-0);
  font-family: var(--font-mono);
  font-size: 12px;
  padding: 0;
}
.pb-trigger-input::placeholder { color: var(--fg-3); font-family: var(--font-sans); }

.pb-trigger-mini {
  width: 24px;
  height: 24px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  border-radius: var(--r-xs);
  flex-shrink: 0;
  transition: color 0.1s, background 0.1s;
}
.pb-trigger-mini:hover { color: var(--fg-0); background: rgb(var(--ink) / 0.06); }
</style>

<!--
  Panel content is teleported to <body>; its rules have to live unscoped
  to reach the rendered element.
-->
<style>
.pb-panel {
  /* Inline (not teleported) so the panel lives inside the modal's DOM
     tree and the modal's click-outside doesn't close on every row click. */
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  right: 0;
  z-index: 100;
  display: flex;
  flex-direction: column;
  max-height: 460px;
  overflow: hidden;
  padding: 0;
}

/* ── Breadcrumbs ─────────────────────────────────────────────── */
.pb-header {
  display: flex;
  align-items: center;
  gap: 1px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
  overflow-x: auto;
  scrollbar-width: none;
}
.pb-header::-webkit-scrollbar { display: none; }

.pb-crumb {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 8px;
  border-radius: var(--r-xs);
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--fg-2);
  white-space: nowrap;
  flex-shrink: 0;
  background: transparent;
  border: 0;
  cursor: pointer;
  transition: color 0.1s, background 0.1s;
}
.pb-crumb:hover { color: var(--fg-0); background: rgb(var(--ink) / 0.06); }
.pb-crumb.active { color: var(--gold); font-weight: 600; }
.pb-crumb-root { color: var(--fg-3); padding-left: 6px; padding-right: 6px; }
.pb-crumb-root.active { color: var(--gold); }
.pb-crumb-sep { color: var(--fg-4); font-family: var(--font-mono); font-size: 12px; user-select: none; }

/* ── Filter row ──────────────────────────────────────────────── */
.pb-filter {
  position: relative;
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
}
.pb-filter-icon {
  position: absolute;
  left: 22px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--fg-3);
  pointer-events: none;
}
.pb-filter-input {
  width: 100%;
  height: 30px;
  background: rgb(var(--ink) / 0.04);
  border: 1px solid rgb(var(--ink) / 0.06);
  border-radius: var(--r-sm);
  padding: 0 28px 0 32px;
  color: var(--fg-0);
  font-size: 12px;
  outline: none;
  transition: border-color 0.12s, background 0.12s;
}
.pb-filter-input:focus {
  border-color: color-mix(in srgb, var(--gold) 40%, transparent);
  background: rgb(var(--ink) / 0.06);
}
.pb-filter-input::placeholder { color: var(--fg-3); }

.pb-filter-clear {
  position: absolute;
  right: 18px;
  top: 50%;
  transform: translateY(-50%);
  width: 20px;
  height: 20px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--r-xs);
  color: var(--fg-3);
  transition: color 0.1s, background 0.1s;
}
.pb-filter-clear:hover { color: var(--fg-0); background: rgb(var(--ink) / 0.06); }

/* ── List ────────────────────────────────────────────────────── */
.pb-list {
  flex: 1;
  min-height: 120px;
  max-height: 280px;
  overflow-y: auto;
  padding: 4px;
}

.pb-row {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 10px;
  background: transparent;
  border: 0;
  border-radius: var(--r-sm);
  font-size: 13px;
  color: var(--fg-1);
  text-align: left;
  cursor: pointer;
  transition: background 0.08s, color 0.08s;
}
.pb-row:hover,
.pb-row.highlighted { background: rgb(var(--ink) / 0.05); color: var(--fg-0); }

.pb-row-parent { color: var(--fg-3); font-family: var(--font-mono); font-size: 12px; }
.pb-row-parent .pb-row-meta { margin-left: auto; color: var(--fg-4); }
.pb-row-parent:hover { color: var(--fg-1); }

.pb-row-icon { flex-shrink: 0; }
.pb-row-icon.gold { color: var(--gold); }
.pb-row-icon.muted { color: var(--fg-3); }

.pb-row-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.pb-hl { color: var(--gold); font-weight: 600; }

.pb-row-arrow {
  color: var(--fg-4);
  opacity: 0;
  flex-shrink: 0;
  transition: opacity 0.1s, color 0.1s;
}
.pb-row:hover .pb-row-arrow,
.pb-row.highlighted .pb-row-arrow { opacity: 1; color: var(--fg-2); }

/* ── Status states (loading / empty) ─────────────────────────── */
.pb-status {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 32px 16px;
  color: var(--fg-3);
  font-size: 12px;
  text-align: center;
}
.pb-empty {
  /* Slightly more visual breathing room for the empty state, since it's
     the only feedback the user gets when nothing matches. */
  padding: 40px 16px;
}
@keyframes pb-spin { to { transform: rotate(360deg); } }
.pb-spin { animation: pb-spin 0.8s linear infinite; color: var(--gold); }

/* ── Footer ─────────────────────────────────────────────────── */
.pb-footer {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-top: 1px solid var(--border);
  background: rgb(var(--ink) / 0.02);
}

.pb-footer-path {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  color: var(--fg-2);
  overflow: hidden;
}
.pb-footer-path code {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-1);
  background: transparent;
  padding: 0;
}
.pb-footer-icon { color: var(--fg-3); flex-shrink: 0; }

.pb-confirm {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 14px;
  border-radius: var(--r-sm);
  background: var(--gold);
  color: var(--accent-ink);
  font-size: 12px;
  font-weight: 600;
  border: 0;
  cursor: pointer;
  flex-shrink: 0;
  transition: background 0.12s, transform 0.08s;
}
.pb-confirm:hover { background: var(--gold-bright, var(--gold)); }
.pb-confirm:active { transform: scale(0.98); }
.pb-confirm:disabled { opacity: 0.5; cursor: not-allowed; }

/* ── Transitions ─────────────────────────────────────────────── */
.pb-drop-enter-active { transition: opacity 0.14s ease, transform 0.14s cubic-bezier(0.16, 1, 0.3, 1); }
.pb-drop-leave-active { transition: opacity 0.1s ease, transform 0.1s ease; }
.pb-drop-enter-from { opacity: 0; transform: translateY(-4px) scale(0.99); }
.pb-drop-leave-to { opacity: 0; transform: translateY(-2px); }
</style>
