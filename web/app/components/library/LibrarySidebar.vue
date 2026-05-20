<template>
  <aside class="lib-sidebar scroll">
    <div class="lib-section">
      <div class="section-title" style="padding: 0 14px; margin-bottom: 10px">Libraries</div>
      <div
        class="lib-item"
        :class="{ active: !activeLib && !activeView }"
        @click="selectLib(null)"
      >
        <Icon name="folder" :size="16" />
        <span>All {{ typeLabel }}</span>
        <span class="count">{{ totalCount }}</span>
      </div>
      <div
        v-for="lib in libraries"
        :key="lib.id"
        class="lib-item"
        :class="{ active: activeLib === lib.id && !activeView }"
        @click="selectLib(lib.id)"
      >
        <Icon name="folder" :size="16" />
        <span>{{ lib.name }}</span>
      </div>
    </div>

    <div class="lib-section" style="margin-top: 24px">
      <div class="section-title" style="padding: 0 14px; margin-bottom: 10px">Collections</div>
      <div
        class="lib-item"
        :class="{ active: activeView === 'loved' }"
        @click="$emit('view', 'loved')"
      >
        <Icon name="heartfill" :size="16" style="color: var(--bad)" />
        <span>Loved</span>
        <span v-if="lovedCount > 0" class="count">{{ lovedCount }}</span>
      </div>

      <div class="lib-item lists-toggle" @click="listsExpanded = !listsExpanded">
        <Icon name="list" :size="16" />
        <span>My Lists</span>
        <Icon :name="listsExpanded ? 'chevdown' : 'chevright'" :size="10" class="expand-icon" />
      </div>
      <template v-if="listsExpanded">
        <div
          v-for="l in userLists"
          :key="l.id"
          class="lib-item lib-item-nested"
          :class="{ active: activeView === `list-${l.id}` }"
          @click="$emit('view', `list-${l.id}`)"
        >
          <span>{{ l.name }}</span>
        </div>
        <div class="lib-item lib-item-nested lib-item-action" @click="createList">
          <Icon name="plus" :size="12" />
          <span>New List</span>
        </div>
      </template>
    </div>

    <div class="lib-footer">
      <div class="lib-footer-text">{{ totalCount }} titles</div>
    </div>
  </aside>
</template>

<script setup lang="ts">
import type { Library } from '~~/shared/types'

const props = defineProps<{
  libraries: Library[]
  activeLib: number | null
  activeView: string | null
  typeLabel: string
  totalCount: number
  lovedCount?: number
}>()

const emit = defineEmits<{
  select: [id: number | null]
  view: [view: string]
}>()

const listsExpanded = ref(false)
const userLists = ref<{ id: number; name: string }[]>([])

function selectLib(id: number | null) {
  emit('select', id)
}

async function createList() {
  const name = prompt('List name:')
  if (!name?.trim()) return
  try {
    await apiFetch('/api/lists', { method: 'POST', body: JSON.stringify({ name: name.trim() }) })
    await loadLists()
  } catch { /* empty */ }
}

async function loadLists() {
  try {
    userLists.value = await apiFetch<{ id: number; name: string }[]>('/api/lists')
  } catch { /* empty */ }
}

onMounted(() => { loadLists() })
</script>

<style scoped>
.lib-sidebar {
  width: 240px;
  flex-shrink: 0;
  background: var(--bg-2);
  border-right: 1px solid var(--border);
  padding: 20px 10px;
  display: flex;
  flex-direction: column;
  height: 100%;
}
.lib-section { display: flex; flex-direction: column; }
.lib-footer {
  margin-top: auto;
  padding: 16px 14px 0;
  border-top: 1px solid var(--border);
}
.lib-footer-text {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-3);
}
.lists-toggle { cursor: pointer; }
.expand-icon { margin-left: auto; opacity: 0.4; }
.lib-item-nested { padding-left: 38px; }
.lib-item-action { color: var(--fg-3); font-size: 12px; }
.lib-item-action:hover { color: var(--gold); }
</style>
