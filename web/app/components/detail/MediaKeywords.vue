<script setup lang="ts">
import type { Keyword } from '~~/shared/types'

defineProps<{ keywords?: Keyword[] | null }>()
</script>

<template>
  <div v-if="keywords?.length" class="keywords">
    <NuxtLink
      v-for="k in keywords"
      :key="k.id"
      :to="`/keyword/${encodeURIComponent(k.name)}`"
      class="keyword-tag"
    >{{ k.name }}</NuxtLink>
  </div>
</template>

<style scoped>
.keywords { display: flex; gap: 5px; flex-wrap: wrap; margin-top: 16px; }
.keyword-tag {
  font-size: 10px; padding: 3px 10px; border-radius: 100px;
  background: var(--bg-3); color: var(--fg-2);
  font-family: var(--font-mono);
  transition: background 0.15s, color 0.15s;
}
.keyword-tag:hover { background: var(--gold-soft); color: var(--gold); }
</style>
