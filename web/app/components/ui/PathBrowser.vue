<template>
  <div class="path-browser" ref="rootEl">
    <div class="path-field">
      <Icon name="folder" :size="14" class="field-icon" />
      <input
        ref="inputEl"
        :value="modelValue"
        class="field-input"
        placeholder="/path/to/media"
        @input="onType"
        @focus="onFocus"
        @keydown.enter.prevent="commitTyped"
        @keydown.escape="close"
      />
      <button
        v-if="modelValue"
        type="button"
        class="field-clear"
        @click.stop="$emit('update:modelValue', ''); close()"
        title="Clear"
      >
        <Icon name="close" :size="10" />
      </button>
    </div>

    <Transition name="drop">
      <div v-if="open_" class="drop" ref="dropEl">
        <div class="drop-crumbs">
          <button
            v-for="(crumb, i) in breadcrumbs"
            :key="i"
            class="crumb"
            @mousedown.prevent="navigate(crumb.path)"
          >
            <Icon v-if="i === 0" name="hard-drives" :size="11" />
            <span>{{ crumb.label }}</span>
          </button>
        </div>

        <div class="drop-list scroll" ref="listEl">
          <button
            v-if="parent"
            class="dir-item dir-parent"
            @mousedown.prevent="navigate(parent)"
          >
            <Icon name="back" :size="13" class="dir-icon-inline muted" />
            <span>..</span>
          </button>

          <div v-if="loading" class="drop-status">
            <Icon name="loading" :size="16" class="spinning" />
          </div>
          <template v-else>
            <button
              v-for="entry in filteredEntries"
              :key="entry.path"
              class="dir-item"
              @mousedown.prevent="navigate(entry.path)"
            >
              <Icon name="folder" :size="13" class="dir-icon-inline gold" />
              <span v-if="filterText" v-html="highlightMatch(entry.name)" />
              <span v-else>{{ entry.name }}</span>
            </button>
            <div v-if="!loading && !filteredEntries.length" class="drop-status">
              {{ filterText ? 'No matches' : 'No subdirectories' }}
            </div>
          </template>
        </div>

        <div class="drop-bar">
          <div class="bar-path">
            <Icon name="check" :size="12" class="bar-check" />
            <span>{{ currentPath }}</span>
          </div>
          <button
            type="button"
            class="bar-select"
            @mousedown.prevent="select(currentPath)"
          >
            Select
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

const open_ = ref(false)
const loading = ref(false)
const currentPath = ref('/')
const parent = ref('')
const entries = ref<{ name: string; path: string }[]>([])
const filterText = ref('')
const inputEl = ref<HTMLInputElement>()
const listEl = ref<HTMLElement>()
const rootEl = ref<HTMLElement>()
const dropEl = ref<HTMLElement>()

const filteredEntries = computed(() => {
  if (!filterText.value) return entries.value
  const q = filterText.value.toLowerCase()
  return entries.value.filter(e => e.name.toLowerCase().includes(q))
})

const breadcrumbs = computed(() => {
  const parts = currentPath.value.split('/').filter(Boolean)
  const crumbs = [{ label: '/', path: '/' }]
  let acc = ''
  for (const part of parts) {
    acc += '/' + part
    crumbs.push({ label: part, path: acc })
  }
  return crumbs
})

async function fetchDir(path: string) {
  loading.value = true
  filterText.value = ''
  try {
    const data = await apiFetch<{ path: string; parent: string; entries: { name: string; path: string }[] }>(
      `/api/fs/browse?path=${encodeURIComponent(path)}`
    )
    currentPath.value = data.path
    parent.value = data.parent
    entries.value = data.entries
    nextTick(() => { if (listEl.value) listEl.value.scrollTop = 0 })
  } catch {
    entries.value = []
  }
  loading.value = false
}

function onFocus() {
  open_.value = true
  fetchDir(props.modelValue || '/')
}

let typeTimer: ReturnType<typeof setTimeout> | null = null

function onType(e: Event) {
  const val = (e.target as HTMLInputElement).value
  emit('update:modelValue', val)

  if (typeTimer) clearTimeout(typeTimer)
  typeTimer = setTimeout(() => syncBrowserToInput(val), 200)
}

async function syncBrowserToInput(val: string) {
  if (!val || !val.startsWith('/')) return

  const endsWithSlash = val.endsWith('/')
  const clean = val.replace(/\/+$/, '') || '/'
  const parts = clean.split('/').filter(Boolean)

  if (endsWithSlash || parts.length === 0) {
    await fetchDir(clean)
    return
  }

  const parentDir = parts.length > 1 ? '/' + parts.slice(0, -1).join('/') : '/'
  const partial = parts[parts.length - 1]

  if (parentDir !== currentPath.value) {
    await fetchDir(parentDir)
  }

  const exact = entries.value.find(e => e.name === partial)
  if (exact) {
    filterText.value = ''
    await fetchDir(exact.path)
    emit('update:modelValue', exact.path)
    setCaret(exact.path.length)
    return
  }

  const matches = entries.value.filter(e => e.name.toLowerCase() === partial.toLowerCase())
  if (matches.length === 1) {
    filterText.value = ''
    const corrected = matches[0]
    await fetchDir(corrected.path)
    emit('update:modelValue', corrected.path)
    setCaret(corrected.path.length)
    return
  }

  const prefixMatches = entries.value.filter(e => e.name.toLowerCase().startsWith(partial.toLowerCase()))
  if (prefixMatches.length === 1) {
    filterText.value = ''
    const corrected = prefixMatches[0]
    await fetchDir(corrected.path)
    emit('update:modelValue', corrected.path)
    setCaret(corrected.path.length)
    return
  }

  filterText.value = partial
}

function setCaret(pos: number) {
  nextTick(() => {
    if (inputEl.value) {
      inputEl.value.setSelectionRange(pos, pos)
    }
  })
}

function highlightMatch(name: string): string {
  if (!filterText.value) return name
  const idx = name.toLowerCase().indexOf(filterText.value.toLowerCase())
  if (idx === -1) return name
  const before = name.slice(0, idx)
  const match = name.slice(idx, idx + filterText.value.length)
  const after = name.slice(idx + filterText.value.length)
  return `${before}<b class="hl">${match}</b>${after}`
}

function commitTyped() {
  if (props.modelValue) {
    syncBrowserToInput(props.modelValue)
  }
}

function navigate(path: string) {
  fetchDir(path)
  emit('update:modelValue', path)
  inputEl.value?.focus()
}

function select(path: string) {
  if (!path) return
  emit('update:modelValue', path)
  close()
}

function close() {
  open_.value = false
}

function onClickOutside(e: MouseEvent) {
  if (!rootEl.value?.contains(e.target as Node)) {
    close()
  }
}

onMounted(() => document.addEventListener('mousedown', onClickOutside))
onUnmounted(() => document.removeEventListener('mousedown', onClickOutside))
</script>

<style scoped>
.path-browser {
  flex: 1;
  position: relative;
}

/* Input field */
.path-field {
  position: relative;
  display: flex;
  align-items: center;
}

.field-icon {
  position: absolute;
  left: 12px;
  color: var(--gold);
  pointer-events: none;
  z-index: 1;
}

.field-input {
  width: 100%;
  height: 40px;
  background: var(--bg-3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 0 36px;
  color: var(--fg-0);
  font-size: 13px;
  font-family: var(--font-mono);
  outline: none;
  transition: border-color 0.12s;
}
.field-input:focus { border-color: var(--gold); }
.field-input::placeholder { color: var(--fg-3); font-family: var(--font-sans); }

.field-clear {
  position: absolute;
  right: 8px;
  width: 24px;
  height: 24px;
  border-radius: var(--r-xs);
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--fg-3);
  transition: all 0.1s;
}
.field-clear:hover { color: var(--fg-1); background: rgba(255,255,255,0.06); }

/* Dropdown */
.drop {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  right: 0;
  z-index: 120;
  background: var(--bg-2);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-lg);
  box-shadow: var(--shadow-3);
  display: flex;
  flex-direction: column;
  max-height: 380px;
  overflow: hidden;
}

/* Breadcrumbs */
.drop-crumbs {
  display: flex;
  align-items: center;
  padding: 8px 8px 4px;
  overflow-x: auto;
  scrollbar-width: none;
  border-bottom: 1px solid var(--border);
}
.drop-crumbs::-webkit-scrollbar { display: none; }

.crumb {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 4px 8px;
  border-radius: var(--r-xs);
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  white-space: nowrap;
  flex-shrink: 0;
  transition: all 0.1s;
}
.crumb:hover { color: var(--fg-1); background: rgba(255,255,255,0.04); }
.crumb:last-child { color: var(--fg-0); font-weight: 600; }
.crumb + .crumb::before {
  content: '/';
  margin-right: 3px;
  color: var(--fg-4);
  font-weight: 400;
}

/* Directory list */
.drop-list {
  flex: 1;
  overflow-y: auto;
  padding: 4px;
  min-height: 80px;
  max-height: 260px;
}

.dir-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 7px 10px;
  border-radius: var(--r-sm);
  font-size: 13px;
  color: var(--fg-1);
  text-align: left;
  transition: background 0.08s;
}
.dir-item:hover { background: rgba(255,255,255,0.04); }

.dir-parent {
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  margin-bottom: 2px;
}
.dir-parent:hover { color: var(--fg-2); }

.dir-icon-inline { flex-shrink: 0; }
.dir-icon-inline.gold { color: var(--gold); }
.dir-icon-inline.muted { color: var(--fg-3); }

.dir-item :deep(.hl) { color: var(--gold); font-weight: 600; }

.drop-status {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 28px 0;
  font-size: 12px;
  color: var(--fg-3);
}

/* Bottom select bar */
.drop-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 10px;
  border-top: 1px solid var(--border);
  background: var(--bg-3);
  border-radius: 0 0 var(--r-lg) var(--r-lg);
}

.bar-path {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
  font-family: var(--font-mono);
  color: var(--fg-3);
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.bar-check { color: var(--good); flex-shrink: 0; }

.bar-select {
  height: 28px;
  padding: 0 14px;
  border-radius: var(--r-sm);
  background: var(--gold);
  color: var(--bg-0);
  font-size: 11px;
  font-weight: 600;
  flex-shrink: 0;
  transition: background 0.12s;
}
.bar-select:hover { background: var(--gold-bright); }

/* Animations */
@keyframes spin { to { transform: rotate(360deg); } }
.spinning { animation: spin 0.8s linear infinite; }

.drop-enter-active { transition: opacity 0.12s ease, transform 0.12s cubic-bezier(0.22, 1, 0.36, 1); }
.drop-leave-active { transition: opacity 0.08s ease, transform 0.08s ease; }
.drop-enter-from { opacity: 0; transform: translateY(-4px) scaleY(0.98); }
.drop-leave-to { opacity: 0; transform: translateY(-2px); }
</style>
