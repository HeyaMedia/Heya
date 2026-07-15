<script setup lang="ts">
import type { Keyword } from '~~/shared/types'

defineProps<{ keywords?: Keyword[] | null }>()
</script>

<template>
  <!-- Inline, comma-separated underlined links (heya2.css `.detail-grid dd a`),
       matching the mockup + the TV page's page-local keyword rendering. Sits in
       the movie page's Details `dd`, inheriting its font/colour. -->
  <span v-if="keywords?.length" class="keywords">
    <template v-for="(k, i) in keywords" :key="k.id">
      <NuxtLink :to="`/keyword/${encodeURIComponent(k.name)}`" class="keyword-link">{{ k.name }}</NuxtLink><span v-if="i < keywords.length - 1">, </span>
    </template>
  </span>
</template>

<style scoped>
.keyword-link {
  border-bottom: 1px solid rgb(var(--ink) / 0.18);
  transition: color 0.15s, border-color 0.15s;
}
.keyword-link:hover { color: var(--tone); border-color: rgb(var(--tone-rgb) / 0.5); }
</style>
