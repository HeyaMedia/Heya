import { defineStore } from 'pinia'
import type { ImageTone } from '~/composables/useImageTone'
import type { BackgroundClaim, BackgroundControls } from '~/composables/useBackground'

export const useBackgroundStore = defineStore('background', () => {
  const claims = ref<BackgroundClaim[]>([])
  const tone = ref<ImageTone | null>(null)
  const controls = ref<BackgroundControls>({
    mode: 'off',
    rotating: false,
    cycle: 0,
    paused: false,
    shuffleReq: 0,
    reveal: false,
    current: null,
  })

  return { claims, tone, controls }
})
