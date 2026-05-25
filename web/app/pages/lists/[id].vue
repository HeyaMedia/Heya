<template>
  <div class="scroll" style="height: 100%">
    <div class="page-pad" style="max-width: 1200px">
      <div v-if="loading" style="padding: 40px 0">
        <div style="height: 32px; width: 200px; background: var(--bg-3); border-radius: var(--r-sm)" />
      </div>

      <template v-else-if="list">
        <div class="list-header">
          <div>
            <h1 class="list-name">{{ list.name }}</h1>
            <p v-if="list.description" class="list-desc">{{ list.description }}</p>
            <div class="list-meta">{{ items.length }} item{{ items.length !== 1 ? 's' : '' }}</div>
          </div>
          <button class="btn btn-secondary btn-sm" @click="confirmDelete">
            <Icon name="trash" :size="14" /> Delete List
          </button>
        </div>

        <div v-if="items.length" class="grid-posters" style="padding-bottom: 80px">
          <div
            v-for="(item, i) in items"
            :key="item.id"
            class="grid-tile card-tile"
            @click="navigateTo(mediaUrl(item))"
          >
            <Poster :idx="i" :src="usePosterUrl(item.id)" aspect="2/3" />
            <div class="grid-tile-meta">
              <div class="grid-tile-title">{{ item.title }}</div>
              <div class="grid-tile-sub">{{ item.year }}</div>
            </div>
          </div>
        </div>

        <div v-else class="empty-list">
          <p>This list is empty. Add items from any media detail page.</p>
        </div>
      </template>

      <div v-else class="empty-list">
        <p>List not found.</p>
        <button class="btn btn-secondary" style="margin-top: 16px" @click="$router.back()">Go back</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { MediaItem } from '~~/shared/types'

const route = useRoute()
const listId = computed(() => route.params.id as string)

interface UserList {
  id: number
  name: string
  description: string
}

const list = ref<UserList | null>(null)
const items = ref<MediaItem[]>([])
const loading = ref(true)

async function confirmDelete() {
  if (!list.value) return
  if (!confirm(`Delete "${list.value.name}"? This cannot be undone.`)) return
  const { $heya } = useNuxtApp()
  await $heya('/api/me/lists/{id}', {
    method: 'DELETE',
    path: { id: list.value.id },
  })
  navigateTo('/')
}

onMounted(async () => {
  try {
    const { $heya } = useNuxtApp()
    const res = await $heya('/api/me/lists/{id}', {
      path: { id: Number(listId.value) },
    }) as { list: UserList; items: MediaItem[] }
    list.value = res.list
    items.value = res.items || []
  } catch { /* empty */ }
  loading.value = false
})
</script>

<style scoped>
.list-header {
  display: flex; align-items: flex-start; justify-content: space-between;
  gap: 24px; padding: 32px 0 24px;
  border-bottom: 1px solid var(--border); margin-bottom: 24px;
}
.list-name { font-size: 28px; font-weight: 700; letter-spacing: -0.02em; margin: 0; }
.list-desc { font-size: 14px; color: var(--fg-2); margin-top: 4px; max-width: 600px; }
.list-meta { font-size: 12px; color: var(--fg-3); font-family: var(--font-mono); margin-top: 8px; }
.empty-list { padding: 80px 0; text-align: center; color: var(--fg-2); font-size: 15px; }
.btn-sm { padding: 6px 14px; font-size: 12px; }
</style>
