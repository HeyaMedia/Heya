<template>
  <aside class="lib-sidebar scroll">
    <div class="lib-section">
      <div class="section-title" style="padding: 0 14px; margin-bottom: 10px">Libraries</div>
      <div
        class="lib-item"
        :class="{ active: !activeLib }"
        @click="$emit('select', null)"
      >
        <Icon name="folder" :size="16" />
        <span>All {{ typeLabel }}</span>
        <span class="count">{{ totalCount }}</span>
      </div>
      <div
        v-for="lib in libraries"
        :key="lib.id"
        class="lib-item"
        :class="{ active: activeLib === lib.id }"
        @click="$emit('select', lib.id)"
      >
        <Icon name="folder" :size="16" />
        <span>{{ lib.name }}</span>
      </div>
    </div>

    <div class="lib-section" style="margin-top: 24px">
      <div class="section-title" style="padding: 0 14px; margin-bottom: 10px">Collections</div>
      <div class="lib-item">
        <Icon name="heart" :size="16" />
        <span>Loved</span>
      </div>
      <div class="lib-item">
        <Icon name="bookmark" :size="16" />
        <span>My List</span>
      </div>
      <div class="lib-item">
        <Icon name="download" :size="16" />
        <span>Downloaded</span>
      </div>
    </div>

    <div class="lib-footer">
      <div class="lib-footer-text">{{ totalCount }} titles</div>
    </div>
  </aside>
</template>

<script setup lang="ts">
import type { Library } from '~~/shared/types'

defineProps<{
  libraries: Library[]
  activeLib: number | null
  typeLabel: string
  totalCount: number
}>()

defineEmits<{
  select: [id: number | null]
}>()
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
</style>
