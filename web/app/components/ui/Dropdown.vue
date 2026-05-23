<script setup lang="ts">
export interface DropdownOption {
  value: string
  label: string
  meta?: string
}

const props = defineProps<{
  modelValue: string
  options: DropdownOption[]
  placeholder?: string
  ariaLabel?: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  change: [value: string]
}>()

const open = ref(false)
const triggerEl = ref<HTMLButtonElement | null>(null)
const listEl = ref<HTMLElement | null>(null)
const highlightIdx = ref(0)

const pos = ref<{ top: number | null; bottom: number | null; left: number; minWidth: number; flip: boolean }>({
  top: 0, bottom: null, left: 0, minWidth: 0, flip: false,
})

const activeOption = computed(() => props.options.find(o => o.value === props.modelValue))
const displayLabel = computed(() => activeOption.value?.label || props.placeholder || 'Select…')
const isCustom = computed(() => props.modelValue !== '' && props.modelValue != null)

function commit(value: string) {
  emit('update:modelValue', value)
  emit('change', value)
}

function selectOption(opt: DropdownOption) {
  commit(opt.value)
  closeMenu()
  triggerEl.value?.focus()
}

function openMenu() {
  if (!triggerEl.value) return
  const rect = triggerEl.value.getBoundingClientRect()
  const spaceBelow = window.innerHeight - rect.bottom
  const spaceAbove = rect.top
  const flip = spaceBelow < 240 && spaceAbove > spaceBelow

  pos.value = {
    top: flip ? null : rect.bottom + 4,
    bottom: flip ? window.innerHeight - rect.top + 4 : null,
    left: rect.left,
    minWidth: rect.width,
    flip,
  }

  const cur = props.options.findIndex(o => o.value === props.modelValue)
  highlightIdx.value = cur >= 0 ? cur : 0
  open.value = true

  nextTick(() => {
    listEl.value?.focus()
    scrollHighlightIntoView()
  })
}

function closeMenu() { open.value = false }

function toggle() {
  if (open.value) closeMenu()
  else openMenu()
}

function scrollHighlightIntoView() {
  const item = listEl.value?.querySelector<HTMLElement>(`[data-idx="${highlightIdx.value}"]`)
  item?.scrollIntoView({ block: 'nearest' })
}

function moveHighlight(delta: number) {
  if (!props.options.length) return
  const n = props.options.length
  highlightIdx.value = (highlightIdx.value + delta + n) % n
  scrollHighlightIntoView()
}

function onTriggerKey(e: KeyboardEvent) {
  if (open.value) return
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    openMenu()
  }
}

function onMenuKey(e: KeyboardEvent) {
  if (e.key === 'Escape') { e.preventDefault(); closeMenu(); triggerEl.value?.focus(); return }
  if (e.key === 'ArrowDown') { e.preventDefault(); moveHighlight(1); return }
  if (e.key === 'ArrowUp') { e.preventDefault(); moveHighlight(-1); return }
  if (e.key === 'Home') { e.preventDefault(); highlightIdx.value = 0; scrollHighlightIntoView(); return }
  if (e.key === 'End') { e.preventDefault(); highlightIdx.value = props.options.length - 1; scrollHighlightIntoView(); return }
  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault()
    const opt = props.options[highlightIdx.value]
    if (opt) selectOption(opt)
    return
  }
  // type-ahead
  if (e.key.length === 1 && /\S/.test(e.key)) {
    const ch = e.key.toLowerCase()
    const start = (highlightIdx.value + 1) % props.options.length
    for (let i = 0; i < props.options.length; i++) {
      const idx = (start + i) % props.options.length
      const lbl = props.options[idx]?.label.toLowerCase() || ''
      if (lbl.startsWith(ch)) {
        highlightIdx.value = idx
        scrollHighlightIntoView()
        break
      }
    }
  }
}

function onDocPointer(e: MouseEvent) {
  if (!open.value) return
  const t = e.target as Node
  if (triggerEl.value?.contains(t) || listEl.value?.contains(t)) return
  closeMenu()
}

function onScrollOrResize() {
  if (open.value) closeMenu()
}

onMounted(() => {
  document.addEventListener('mousedown', onDocPointer)
  window.addEventListener('resize', onScrollOrResize)
  window.addEventListener('scroll', onScrollOrResize, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onDocPointer)
  window.removeEventListener('resize', onScrollOrResize)
  window.removeEventListener('scroll', onScrollOrResize, true)
})
</script>

<template>
  <div class="dd">
    <button
      ref="triggerEl"
      type="button"
      class="dd-trigger"
      :class="{ 'is-open': open, 'is-custom': isCustom }"
      :aria-haspopup="'listbox'"
      :aria-expanded="open"
      :aria-label="ariaLabel"
      @click="toggle"
      @keydown="onTriggerKey"
    >
      <span class="dd-label">{{ displayLabel }}</span>
      <span class="dd-chev" :class="{ 'is-rot': open }">
        <Icon name="chevdown" :size="12" />
      </span>
    </button>

    <Teleport to="body">
      <Transition :name="pos.flip ? 'dd-up' : 'dd-down'">
        <div
          v-if="open"
          ref="listEl"
          role="listbox"
          tabindex="-1"
          class="dd-menu"
          :class="{ 'is-flip': pos.flip }"
          :style="{
            top: pos.top !== null ? pos.top + 'px' : 'auto',
            bottom: pos.bottom !== null ? pos.bottom + 'px' : 'auto',
            left: pos.left + 'px',
            minWidth: pos.minWidth + 'px',
          }"
          @keydown="onMenuKey"
        >
          <button
            v-for="(opt, idx) in options"
            :key="opt.value"
            type="button"
            role="option"
            class="dd-item"
            :class="{
              'is-active': opt.value === modelValue,
              'is-highlight': idx === highlightIdx,
            }"
            :data-idx="idx"
            :aria-selected="opt.value === modelValue"
            @click="selectOption(opt)"
            @mouseenter="highlightIdx = idx"
          >
            <span class="dd-item-label">{{ opt.label }}</span>
            <span v-if="opt.meta" class="dd-item-meta">{{ opt.meta }}</span>
            <Icon v-if="opt.value === modelValue" name="check" :size="13" class="dd-item-check" />
          </button>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.dd { position: relative; min-width: 0; }

/* ── Trigger ───────────────────────────────── */
.dd-trigger {
  display: flex; align-items: center; gap: 8px;
  width: 100%;
  padding: 7px 10px 7px 12px;
  font-size: 12px; font-weight: 500;
  font-family: inherit;
  color: rgba(255,255,255,0.85);
  background: rgba(255,255,255,0.06);
  border: 1px solid rgba(255,255,255,0.1);
  border-radius: var(--r-sm);
  cursor: pointer;
  outline: none;
  text-align: left;
  transition: background 0.12s, border-color 0.12s, color 0.12s;
}
.dd-trigger:hover {
  background: rgba(255,255,255,0.1);
  border-color: rgba(255,255,255,0.22);
  color: #fff;
}
.dd-trigger.is-open {
  border-color: var(--gold);
  background: rgba(255,255,255,0.08);
}
.dd-trigger.is-custom {
  color: var(--gold);
  border-color: rgba(251,191,36,0.35);
  background: rgba(251,191,36,0.08);
}
.dd-trigger.is-custom.is-open {
  border-color: var(--gold);
  background: rgba(251,191,36,0.12);
}
.dd-trigger:focus-visible {
  outline: 2px solid rgba(251,191,36,0.4);
  outline-offset: 2px;
}

.dd-label {
  flex: 1; min-width: 0;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.dd-chev {
  flex-shrink: 0; display: inline-flex; align-items: center;
  opacity: 0.7; transition: opacity 0.12s, transform 0.18s ease;
}
.dd-trigger:hover .dd-chev,
.dd-trigger.is-open .dd-chev { opacity: 1; }
.dd-chev.is-rot { transform: rotate(180deg); }

/* ── Menu (teleported) ─────────────────────── */
.dd-menu {
  position: fixed;
  z-index: 9000;
  max-height: 280px; overflow-y: auto;
  padding: 4px;
  background: var(--bg-2);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-md);
  box-shadow: 0 12px 40px rgba(0,0,0,0.55), 0 0 0 1px rgba(255,255,255,0.02);
  outline: none;
  scrollbar-width: thin;
  scrollbar-color: rgba(255,255,255,0.2) transparent;
}
.dd-menu::-webkit-scrollbar { width: 8px; }
.dd-menu::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.15); border-radius: 4px; }

.dd-item {
  display: flex; align-items: center; gap: 8px;
  width: 100%;
  padding: 7px 10px;
  font-size: 12px; font-weight: 500;
  font-family: inherit;
  color: var(--fg-1);
  background: transparent;
  border: none;
  border-radius: var(--r-xs);
  cursor: pointer;
  outline: none;
  text-align: left;
  transition: background 0.1s, color 0.1s;
}
.dd-item.is-highlight {
  background: rgba(255,255,255,0.06);
  color: #fff;
}
.dd-item.is-active {
  color: var(--gold);
  font-weight: 600;
}
.dd-item.is-active.is-highlight {
  background: var(--gold-soft);
}
.dd-item-label {
  flex: 1; min-width: 0;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.dd-item-meta {
  font-size: 10px; font-family: var(--font-mono);
  color: var(--fg-4); flex-shrink: 0;
}
.dd-item.is-active .dd-item-meta { color: var(--gold); opacity: 0.65; }
.dd-item-check { color: var(--gold); flex-shrink: 0; }

/* ── Open / close transitions ──────────────── */
.dd-down-enter-active, .dd-down-leave-active,
.dd-up-enter-active, .dd-up-leave-active {
  transition: opacity 0.14s ease, transform 0.14s ease;
}
.dd-down-enter-from, .dd-down-leave-to {
  opacity: 0; transform: translateY(-4px);
}
.dd-up-enter-from, .dd-up-leave-to {
  opacity: 0; transform: translateY(4px);
}
</style>
