<template>
  <div class="qp-wrap" ref="wrap">
    <button
      class="qp-chip"
      :class="{ open }"
      @click.stop="toggle"
      :title="files.length > 1 ? `${files.length} formats available` : ''"
    >
      <span>{{ chipLabel(files[0]) }}</span>
      <Icon v-if="files.length > 1" name="chevdown" :size="10" />
    </button>
    <Teleport to="body">
      <div v-if="open" ref="menuEl" class="qp-menu" :style="menuStyle">
        <div class="qp-menu-head">Quality</div>
        <button
          v-for="f in files"
          :key="f.id"
          class="qp-menu-item"
          :class="{ active: f.id === selectedId }"
          @click.stop="pick(f)"
        >
          <div class="qp-menu-main">
            <div class="qp-menu-format">{{ chipLabel(f) }}</div>
            <div class="qp-menu-meta">
              <span v-if="f.bitrate_kbps > 0">{{ f.bitrate_kbps }} kbps</span>
              <span v-if="f.sample_rate_hz > 0">{{ Math.round(f.sample_rate_hz / 1000) }} kHz</span>
              <span v-if="f.channels > 0">{{ f.channels }}ch</span>
              <span v-if="f.size_bytes > 0">{{ formatBytes(f.size_bytes) }}</span>
            </div>
          </div>
          <Icon v-if="f.id === selectedId" name="check" :size="12" />
        </button>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import type { TrackFile } from '~~/shared/types'

const props = defineProps<{
  files: TrackFile[]
  selectedId?: number
}>()

const emit = defineEmits<{ pick: [TrackFile] }>()

const open = ref(false)
const wrap = ref<HTMLElement | null>(null)
const menuEl = ref<HTMLElement | null>(null)
const menuStyle = ref<Record<string, string>>({})

function toggle() {
  if (props.files.length <= 1) return
  open.value = !open.value
  if (open.value) nextTick(positionMenu)
}

function positionMenu() {
  if (!wrap.value) return
  const rect = wrap.value.getBoundingClientRect()
  menuStyle.value = {
    top: `${rect.bottom + window.scrollY + 4}px`,
    left: `${rect.left + window.scrollX}px`,
  }
}

function pick(f: TrackFile) {
  emit('pick', f)
  open.value = false
}

function chipLabel(f?: TrackFile): string {
  if (!f) return ''
  const parts: string[] = []
  if (f.format) parts.push(f.format.toUpperCase())
  if (f.bit_depth > 0 && f.sample_rate_hz > 0) {
    parts.push(`${f.bit_depth}/${Math.round(f.sample_rate_hz / 1000)}`)
  } else if (f.bitrate_kbps > 0) {
    parts.push(`${f.bitrate_kbps}`)
  }
  return parts.join(' ')
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function onDocClick(e: MouseEvent) {
  if (!open.value) return
  const t = e.target as Node
  if (wrap.value?.contains(t)) return
  if (menuEl.value?.contains(t)) return
  open.value = false
}

onMounted(() => {
  document.addEventListener('click', onDocClick)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
})
</script>

<style scoped>
.qp-wrap { display: inline-flex; }
.qp-chip {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-3);
  padding: 2px 5px;
  border-radius: 3px;
  background: transparent;
  border: 1px solid transparent;
  cursor: pointer;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}
.qp-chip:hover, .qp-chip.open {
  background: var(--bg-3);
  color: var(--fg-1);
  border-color: var(--border);
}

.qp-menu {
  position: absolute;
  z-index: 100;
  min-width: 220px;
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  padding: 4px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.45);
}
.qp-menu-head {
  font-size: 10px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--fg-3);
  padding: 8px 10px 4px;
}
.qp-menu-item {
  display: flex;
  align-items: center;
  width: 100%;
  padding: 8px 10px;
  background: transparent;
  border: 0;
  border-radius: var(--r-sm);
  text-align: left;
  cursor: pointer;
  color: var(--fg-1);
}
.qp-menu-item:hover { background: rgba(255,255,255,0.05); }
.qp-menu-item.active { color: var(--gold); }
.qp-menu-main { flex: 1; min-width: 0; }
.qp-menu-format {
  font-size: 12px;
  font-family: var(--font-mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.qp-menu-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 10px;
  color: var(--fg-3);
  margin-top: 2px;
}
</style>
