<template>
  <div class="poster" :class="className" :style="{ aspectRatio: aspect || '2/3', ...style }">
    <LoadingImage
      v-if="src && !imgError"
      :src="src"
      :alt="title ?? ''"
      :width="width ?? 200"
      :quality="80"
      densities="1x 2x"
      class="absolute inset-0 w-full h-full object-cover z-0"
      loading="lazy"
      @error="imgError = true"
    />
    <template v-if="!src || imgError">
      <div class="poster-fallback" :style="{ background: fallbackTint }" />
      <div v-if="initials" class="poster-initials">{{ initials }}</div>
    </template>
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
  // Base width hint passed to the resize provider. The `densities="1x 2x"`
  // attribute on the shared image component auto-doubles it for HiDPI displays via
  // srcset, so the request you see in DevTools is either `?w={width}` on 1x
  // or `?w={width*2}` on 2x — the browser picks. Default 200 covers the
  // ~160-180px CSS grid cards. Pass a larger value for hero crops.
  width?: number
}>()

const imgError = ref(false)

watch(() => props.src, () => { imgError.value = false })

const p = computed(() => palettes[(props.idx || 0) % palettes.length]!)

// No-art tile (Heya 2.0): a faint palette-hashed tint over the poster's own
// dark --bg-3 surface (kept literal — decorative placeholder art), plus big
// mono initials derived from the title. The palette hash is retained so
// neighbouring blank tiles stay subtly distinct rather than identical.
const fallbackTint = computed(() =>
  `linear-gradient(150deg, color-mix(in srgb, ${p.value.hi} 16%, transparent) 0%, transparent 62%)`)
const initials = computed(() => {
  const t = (props.title || '').trim()
  if (!t) return ''
  const words = t.split(/\s+/).filter(Boolean)
  const chars = words.length >= 2 ? words[0]!.charAt(0) + words[1]!.charAt(0) : t.slice(0, 2)
  return chars.toUpperCase()
})
</script>
