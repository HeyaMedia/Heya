<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  points: number[]
  stroke?: string
  fill?: string
  height?: number
  width?: number
}>(), {
  stroke: 'var(--gold)',
  fill: 'rgba(230, 185, 74, 0.10)',
  height: 28,
  width: 120,
})

const path = computed(() => {
  const pts = props.points
  if (pts.length < 2) return ''
  const min = Math.min(...pts)
  const max = Math.max(...pts)
  const range = max - min || 1
  const w = props.width
  const h = props.height
  const stepX = w / (pts.length - 1)
  return pts
    .map((y, i) => {
      const px = i * stepX
      const py = h - ((y - min) / range) * h
      return `${i === 0 ? 'M' : 'L'}${px.toFixed(1)},${py.toFixed(1)}`
    })
    .join(' ')
})

const area = computed(() => {
  if (!path.value) return ''
  return `${path.value} L${props.width},${props.height} L0,${props.height} Z`
})
</script>

<template>
  <svg
    class="sv2-spark"
    :viewBox="`0 0 ${width} ${height}`"
    preserveAspectRatio="none"
    aria-hidden="true"
  >
    <path :d="area" :fill="fill" stroke="none" />
    <path :d="path" :stroke="stroke" stroke-width="1.25" fill="none" stroke-linejoin="round" stroke-linecap="round" />
  </svg>
</template>

<style scoped>
.sv2-spark {
  width: 100%;
  display: block;
}
</style>
