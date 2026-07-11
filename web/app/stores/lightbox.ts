import { defineStore } from 'pinia'

export const useLightboxStore = defineStore('lightbox', () => {
  const isOpen = ref(false)
  const images = ref<string[]>([])
  const index = ref(0)
  const currentSrc = computed(() => images.value[index.value] || '')
  const hasNext = computed(() => index.value < images.value.length - 1)
  const hasPrev = computed(() => index.value > 0)
  const total = computed(() => images.value.length)

  function open(src: string | string[], startIndex = 0) {
    images.value = Array.isArray(src) ? src : [src]
    index.value = Math.max(0, Math.min(startIndex, images.value.length - 1))
    isOpen.value = true
  }

  function close() { isOpen.value = false }
  function next() { if (hasNext.value) index.value++ }
  function prev() { if (hasPrev.value) index.value-- }

  return { isOpen, images, index, currentSrc, hasNext, hasPrev, total, open, close, next, prev }
})
