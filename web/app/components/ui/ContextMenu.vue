<template>
  <Teleport to="body">
    <div
      v-if="visible"
      class="ctx-backdrop"
      @click="$emit('close')"
      @contextmenu.prevent="$emit('close')"
    />
    <div
      v-if="visible"
      ref="menuEl"
      class="ctx-menu"
      :style="{ top: posY + 'px', left: posX + 'px' }"
    >
      <template v-for="(item, i) in items" :key="i">
        <div v-if="item.separator" class="ctx-sep" />
        <div
          v-else
          class="ctx-item"
          :class="{ disabled: item.disabled, 'has-sub': item.submenu?.length }"
          @click="!item.disabled && !item.submenu?.length && handleAction(item)"
          @mouseenter="item.submenu?.length ? openSub = i : openSub = -1"
        >
          <Icon v-if="item.icon" :name="item.icon" :size="14" class="ctx-icon" />
          <span>{{ item.label }}</span>
          <Icon v-if="item.submenu?.length" name="chevright" :size="10" class="ctx-arrow" />

          <!-- Submenu -->
          <div v-if="item.submenu?.length && openSub === i" class="ctx-submenu">
            <div
              v-for="(sub, j) in item.submenu"
              :key="j"
              class="ctx-item"
              :class="{ disabled: sub.disabled }"
              @click.stop="!sub.disabled && handleAction(sub)"
            >
              <Icon v-if="sub.icon" :name="sub.icon" :size="14" class="ctx-icon" />
              <span>{{ sub.label }}</span>
            </div>
          </div>
        </div>
      </template>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import type { ContextMenuItem } from '~~/shared/types'

const props = defineProps<{
  items: ContextMenuItem[]
  x: number
  y: number
  visible: boolean
}>()

const emit = defineEmits<{ close: [] }>()

const menuEl = ref<HTMLElement>()
const openSub = ref(-1)
const posX = ref(0)
const posY = ref(0)

watch(() => props.visible, (v) => {
  if (v) {
    openSub.value = -1
    nextTick(() => adjustPosition())
  }
})

watch(() => [props.x, props.y], () => {
  if (props.visible) nextTick(() => adjustPosition())
})

function adjustPosition() {
  posX.value = props.x
  posY.value = props.y
  if (!menuEl.value) return
  const rect = menuEl.value.getBoundingClientRect()
  const vw = window.innerWidth
  const vh = window.innerHeight
  if (posX.value + rect.width > vw - 8) posX.value = vw - rect.width - 8
  if (posY.value + rect.height > vh - 8) posY.value = vh - rect.height - 8
  if (posX.value < 8) posX.value = 8
  if (posY.value < 8) posY.value = 8
}

function handleAction(item: ContextMenuItem) {
  item.action?.()
  emit('close')
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

onMounted(() => document.addEventListener('keydown', onKeydown))
onUnmounted(() => document.removeEventListener('keydown', onKeydown))
</script>

<style scoped>
.ctx-backdrop {
  position: fixed; inset: 0; z-index: 999;
}
.ctx-menu {
  position: fixed; z-index: 1000;
  min-width: 200px; max-width: 280px;
  background: var(--bg-3); border: 1px solid var(--border-strong);
  border-radius: var(--r-md); padding: 4px;
  box-shadow: var(--shadow-2);
}
.ctx-item {
  display: flex; align-items: center; gap: 8px;
  padding: 7px 10px; font-size: 13px;
  border-radius: var(--r-sm); cursor: pointer;
  color: var(--fg-1); position: relative;
}
.ctx-item:hover { background: rgba(255,255,255,0.06); }
.ctx-item.disabled { opacity: 0.35; cursor: default; }
.ctx-item.disabled:hover { background: none; }
.ctx-icon { flex-shrink: 0; opacity: 0.6; }
.ctx-arrow { margin-left: auto; opacity: 0.4; }
.ctx-sep { height: 1px; background: var(--border); margin: 4px 6px; }

.ctx-submenu {
  position: absolute; left: 100%; top: -4px;
  min-width: 180px; background: var(--bg-3);
  border: 1px solid var(--border-strong);
  border-radius: var(--r-md); padding: 4px;
  box-shadow: var(--shadow-2);
}
</style>
