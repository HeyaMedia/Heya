<template>
  <div class="card p-4">
    <div class="text-xs font-medium uppercase tracking-wider" :class="colorClass">{{ label }}</div>
    <div class="mt-1 text-2xl font-semibold text-gray-100">{{ value }}</div>
    <div v-if="subtitle" class="mt-0.5 text-xs text-gray-500">{{ subtitle }}</div>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  label: string
  value: string | number
  subtitle?: string
  color?: string
}>()

const colorClass = computed(() => {
  const colors: Record<string, string> = {
    movie: 'text-heya-movie',
    tv: 'text-heya-tv',
    music: 'text-heya-music',
    book: 'text-heya-book',
    primary: 'text-heya-primary',
  }
  return colors[props.color || 'primary'] || 'text-gray-400'
})
</script>
