// useSleepTimer — pause playback after a countdown, or at the end of the
// current track. State is module-level so the popover control and the ticking
// (driven once by that control) stay in sync. The "end of track" case is honored
// by usePlayer.handleEnded via the shared Pinia player domain.

import { storeToRefs } from 'pinia'
import { usePlayerStore } from '~/composables/usePlayer'

export function useSleepTimer() {
  const { sleepAtTrackEnd: atTrackEnd, sleepDeadline: deadline, sleepNowTick: nowTick } = storeToRefs(usePlayerStore())

  const remainingMs = computed(() =>
    deadline.value != null ? Math.max(0, deadline.value - (nowTick.value || Date.now())) : 0,
  )
  const timed = computed(() => deadline.value != null)
  const active = computed(() => deadline.value != null || atTrackEnd.value)

  function setMinutes(min: number) {
    atTrackEnd.value = false
    deadline.value = Date.now() + min * 60_000
    nowTick.value = Date.now()
  }
  function setEndOfTrack() {
    deadline.value = null
    atTrackEnd.value = true
  }
  function cancel() {
    deadline.value = null
    atTrackEnd.value = false
  }

  // Called once per second by the control. Updates the reactive clock and fires
  // onExpire (→ pause) when a timed sleep elapses.
  function tick(onExpire: () => void) {
    nowTick.value = Date.now()
    if (deadline.value != null && nowTick.value >= deadline.value) {
      deadline.value = null
      onExpire()
    }
  }

  return { remainingMs, timed, active, atTrackEnd, setMinutes, setEndOfTrack, cancel, tick }
}
