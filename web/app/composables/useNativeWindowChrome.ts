import { readonly, ref } from 'vue'

const videoActive = ref(false)
const videoControlsVisible = ref(true)
const revealListeners = new Set<() => void>()

export function useNativeWindowChrome() {
  function enterVideo(controlsVisible = true) {
    videoActive.value = true
    videoControlsVisible.value = controlsVisible
  }

  function updateVideoControls(visible: boolean) {
    videoControlsVisible.value = visible
  }

  function leaveVideo() {
    videoActive.value = false
    videoControlsVisible.value = true
  }

  function onRevealVideoControls(listener: () => void): () => void {
    revealListeners.add(listener)
    return () => revealListeners.delete(listener)
  }

  function requestVideoControls() {
    for (const listener of [...revealListeners]) listener()
  }

  return {
    videoActive: readonly(videoActive),
    videoControlsVisible: readonly(videoControlsVisible),
    enterVideo,
    updateVideoControls,
    leaveVideo,
    onRevealVideoControls,
    requestVideoControls,
  }
}
