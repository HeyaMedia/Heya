<template>
  <Teleport to="body">
    <div class="modal-overlay" @click.self="$emit('close')">
      <div class="modal-panel">
        <div class="modal-header">
          <h3 class="modal-title">{{ taskName }}</h3>
          <button class="modal-close" @click="$emit('close')">
            <Icon name="close" :size="16" />
          </button>
        </div>

        <div class="modal-tabs">
          <button
            v-for="tab in tabs"
            :key="tab.key"
            class="tab-btn"
            :class="{ active: activeTab === tab.key }"
            @click="switchTab(tab.key)"
          >
            {{ tab.label }}
            <span class="tab-count">{{ tab.count }}</span>
          </button>
        </div>

        <div class="modal-body">
          <div v-if="loading && !items.length" class="loading-hint">Loading...</div>
          <div v-else-if="!items.length" class="empty-hint">
            <Icon name="check" :size="14" />
            No items{{ activeTab !== 'all' ? ' matching filter' : '' }}
          </div>
          <div v-else class="item-list">
            <div v-for="item in items" :key="item.id" class="item-row">
              <span class="item-dot" :class="item.status" />
              <div class="item-info">
                <div class="item-name">{{ item.name }}</div>
                <div class="item-path">{{ item.path }}</div>
                <div v-if="item.error" class="item-error" :title="item.error">{{ item.error }}</div>
              </div>
              <div v-if="item.detail" class="item-detail">{{ item.detail }}</div>
              <div class="item-status-label" :class="item.status">{{ item.status }}</div>
            </div>
          </div>
        </div>

        <div v-if="total > items.length || offset > 0" class="modal-footer">
          <button class="btn btn-secondary btn-sm" :disabled="offset === 0" @click="prevPage">Previous</button>
          <span class="page-info">{{ offset + 1 }}–{{ Math.min(offset + pageSize, total) }} of {{ total }}</span>
          <button class="btn btn-secondary btn-sm" :disabled="offset + pageSize >= total" @click="nextPage">Next</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
interface TaskItem {
  id: number
  name: string
  path: string
  status: string
  detail?: string
  error?: string
}

interface TaskItemsResponse {
  items: TaskItem[]
  total: number
  complete: number
  pending: number
  failed: number
}

const props = defineProps<{
  taskId: string
  taskName: string
}>()

defineEmits<{ close: [] }>()

const items = ref<TaskItem[]>([])
const total = ref(0)
const completeCount = ref(0)
const pendingCount = ref(0)
const failedCount = ref(0)
const loading = ref(false)
const activeTab = ref('all')
const offset = ref(0)
const pageSize = 50

const tabs = computed(() => {
  const base = [
    { key: 'all', label: 'All', count: total.value },
    { key: 'complete', label: 'Complete', count: completeCount.value },
    { key: 'pending', label: 'Pending', count: pendingCount.value },
  ]
  // Only show the Failed tab when there's something to look at — keeps
  // the modal uncluttered for tasks that don't track per-item failure.
  if (failedCount.value > 0) {
    base.push({ key: 'failed', label: 'Failed', count: failedCount.value })
  }
  return base
})

async function fetchItems() {
  loading.value = true
  try {
    const { $heya } = useNuxtApp()
    const query: Record<string, any> = { limit: pageSize, offset: offset.value }
    if (activeTab.value !== 'all') query.status = activeTab.value

    const res = await $heya('/api/tasks/{id}/items', {
      path: { id: props.taskId as any },
      query,
    }) as TaskItemsResponse
    items.value = res.items
    total.value = res.total
    completeCount.value = res.complete
    pendingCount.value = res.pending
    failedCount.value = res.failed ?? 0
  } catch {
    items.value = []
  }
  loading.value = false
}

function switchTab(tab: string) {
  activeTab.value = tab
  offset.value = 0
  fetchItems()
}

function prevPage() {
  offset.value = Math.max(0, offset.value - pageSize)
  fetchItems()
}

function nextPage() {
  offset.value += pageSize
  fetchItems()
}

onMounted(() => {
  fetchItems()
  document.body.style.overflow = 'hidden'
})

onUnmounted(() => {
  document.body.style.overflow = ''
})
</script>

<style scoped>
.modal-overlay {
  position: fixed; inset: 0; z-index: 1000;
  background: rgba(0, 0, 0, 0.6); backdrop-filter: blur(4px);
  display: flex; align-items: center; justify-content: center;
}
.modal-panel {
  background: var(--bg-2); border: 1px solid var(--border);
  border-radius: var(--r-lg); width: 720px; max-width: 90vw;
  max-height: 80vh; display: flex; flex-direction: column;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.4);
}

.modal-header {
  display: flex; align-items: center; justify-content: space-between;
  padding: 18px 22px; border-bottom: 1px solid var(--border);
}
.modal-title { font-size: 16px; font-weight: 600; margin: 0; }
.modal-close {
  width: 32px; height: 32px; border-radius: var(--r-sm);
  display: flex; align-items: center; justify-content: center;
  color: var(--fg-3); transition: all 0.12s ease;
}
.modal-close:hover { color: var(--fg-0); background: rgba(255,255,255,0.06); }

.modal-tabs {
  display: flex; gap: 2px; padding: 10px 22px; border-bottom: 1px solid var(--border);
}
.tab-btn {
  display: flex; align-items: center; gap: 6px;
  padding: 6px 14px; border-radius: 100px;
  font-size: 12px; font-weight: 500; color: var(--fg-3);
  background: transparent; border: 1px solid transparent;
  cursor: pointer; transition: all 0.12s ease;
}
.tab-btn:hover { color: var(--fg-1); background: rgba(255,255,255,0.04); }
.tab-btn.active { color: var(--gold); background: var(--gold-soft); border-color: rgba(230,185,74,0.2); }
.tab-count {
  font-family: var(--font-mono); font-size: 11px; font-weight: 700;
}

.modal-body {
  flex: 1; overflow-y: auto; padding: 0;
}

.loading-hint, .empty-hint {
  display: flex; align-items: center; justify-content: center; gap: 8px;
  color: var(--fg-3); font-size: 13px; padding: 40px 22px;
}

.item-list { }
.item-row {
  display: flex; align-items: flex-start; gap: 10px;
  padding: 10px 22px; border-bottom: 1px solid var(--border);
  transition: background 0.1s ease;
}
.item-row:last-child { border-bottom: none; }
.item-row:hover { background: rgba(255,255,255,0.02); }

.item-dot {
  width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; margin-top: 5px;
}
.item-dot.complete { background: var(--good); }
.item-dot.pending { background: var(--gold); }
.item-dot.failed { background: var(--bad, #d6594a); }

.item-info { flex: 1; min-width: 0; }
.item-name { font-size: 13px; font-weight: 500; color: var(--fg-1); }
.item-path {
  font-size: 11px; font-family: var(--font-mono); color: var(--fg-4);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; margin-top: 1px;
}

.item-detail {
  font-size: 11px; font-family: var(--font-mono); color: var(--fg-3);
  white-space: nowrap; flex-shrink: 0; text-transform: capitalize;
}

.item-status-label {
  font-size: 10px; font-weight: 600; font-family: var(--font-mono);
  padding: 2px 8px; border-radius: 100px; text-transform: uppercase;
  flex-shrink: 0; letter-spacing: 0.04em;
}
.item-status-label.complete { background: rgba(100,200,140,0.12); color: var(--good); }
.item-status-label.pending { background: var(--gold-soft); color: var(--gold); }
.item-status-label.failed { background: rgba(214,89,74,0.14); color: var(--bad, #d6594a); }

.item-error {
  font-size: 11px; font-family: var(--font-mono); color: var(--bad, #d6594a);
  margin-top: 3px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

.modal-footer {
  display: flex; align-items: center; justify-content: center; gap: 12px;
  padding: 12px 22px; border-top: 1px solid var(--border);
}
.page-info { font-size: 11px; color: var(--fg-3); font-family: var(--font-mono); }
.btn-sm { height: 30px; padding: 0 12px; font-size: 11px; }
</style>
