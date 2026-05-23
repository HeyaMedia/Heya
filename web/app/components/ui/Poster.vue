<template>
  <div class="poster" :class="className" :style="{ aspectRatio: aspect || '2/3', ...style }">
    <img
      v-if="src && !imgError"
      :src="src"
      :alt="title"
      class="absolute inset-0 w-full h-full object-cover z-0"
      loading="lazy"
      @error="imgError = true"
    />
    <template v-if="!src || imgError">
      <div class="poster-gradient" :style="{ background: gradient }" />
      <div class="poster-texture" />
    </template>
    <div v-if="title && (!src || imgError)" class="poster-title">{{ title }}</div>
    <div v-if="label || kind" class="poster-label">{{ label || kind }}</div>
    <slot />
  </div>
</template>

<script setup lang="ts">
const palettes = [
  { bg: '#1a1a2e', mid: '#2d3561', hi: '#4a4e8a' },
  { bg: '#1e1a0d', mid: '#3a2f17', hi: '#5c4b2a' },
  { bg: '#0d1a1a', mid: '#173a3a', hi: '#2a5c5c' },
  { bg: '#1a0d1a', mid: '#3a173a', hi: '#5c2a5c' },
  { bg: '#1a1a0d', mid: '#3a3a17', hi: '#5c5c2a' },
  { bg: '#0d0d1a', mid: '#17173a', hi: '#2a2a5c' },
  { bg: '#1a0d0d', mid: '#3a1717', hi: '#5c2a2a' },
  { bg: '#0d1a0d', mid: '#173a17', hi: '#2a5c2a' },
  { bg: '#15121e', mid: '#2e2545', hi: '#4a3d6e' },
  { bg: '#1e1512', mid: '#452e25', hi: '#6e4a3d' },
  { bg: '#121e15', mid: '#254530', hi: '#3d6e50' },
  { bg: '#1e1218', mid: '#45252e', hi: '#6e3d4a' },
]

const props = defineProps<{
  idx?: number
  title?: string
  kind?: string
  label?: string
  src?: string | null
  aspect?: string
  className?: string
  style?: Record<string, string>
}>()

const imgError = ref(false)

watch(() => props.src, () => { imgError.value = false })

const p = computed(() => palettes[(props.idx || 0) % palettes.length]!)
const gradient = computed(() => `linear-gradient(135deg, ${p.value.bg} 0%, ${p.value.mid} 55%, ${p.value.hi} 100%)`)
</script>
